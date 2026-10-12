-- Legacy syncs have no proven actor and cannot dispatch until explicitly replaced.
ALTER TABLE public.sync_runs ADD COLUMN actor_id uuid REFERENCES public.users(id);
ALTER TABLE public.sync_runs ADD COLUMN scan_complete boolean NOT NULL DEFAULT false;
ALTER TABLE public.sync_runs ADD COLUMN credential_version bigint;
ALTER TABLE public.sync_runs ADD COLUMN failure_code text
    CHECK (failure_code IN ('permission_denied','provider_unavailable','provider_rate_limited',
        'provider_response_invalid','sync_limit_reached'));

-- Copy the allowlisted resource checks, but not public identity/uniqueness indexes.
CREATE TABLE public.inventory_staging (LIKE public.resources INCLUDING CONSTRAINTS);
ALTER TABLE public.inventory_staging ADD PRIMARY KEY (workspace_id,connection_id,generation,native_id);
ALTER TABLE public.inventory_staging ADD FOREIGN KEY (workspace_id,connection_id,resource_type,scope,generation)
    REFERENCES public.sync_runs(workspace_id,connection_id,resource_type,scope,generation);
ALTER TABLE public.inventory_staging ADD CHECK (tombstoned_at IS NULL AND version=1);
