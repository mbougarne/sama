-- Version 1 contains only a connection reference. A later version must add
-- operation/sync references with their owning composite foreign keys.
CREATE TABLE public.jobs (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    type text NOT NULL CHECK (type='connection_refresh'),
    payload_version integer NOT NULL CHECK (payload_version=1),
    payload jsonb NOT NULL CHECK (
        jsonb_typeof(payload)='object' AND octet_length(payload::text)<=1024
        AND payload=jsonb_build_object('connection_id',connection_id::text)
    ),
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','succeeded','failed','cancelled')),
    next_run_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_owner uuid,
    lease_token uuid,
    lease_expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts>=0 AND attempts<=100),
    deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (workspace_id,connection_id) REFERENCES public.connections(workspace_id,id),
    UNIQUE(workspace_id,id),
    CHECK (deadline>created_at AND next_run_at<=deadline),
    CHECK ((lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
        OR (lease_owner IS NOT NULL AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CHECK (state<>'running' OR lease_token IS NOT NULL)
);
CREATE INDEX jobs_due ON public.jobs(next_run_at,id) WHERE state='queued';
CREATE INDEX jobs_expired_leases ON public.jobs(lease_expires_at) WHERE state='running';
