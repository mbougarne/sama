CREATE UNIQUE INDEX sync_runs_pending_scope ON public.sync_runs(workspace_id,connection_id,resource_type,scope)
    WHERE status IN ('pending','running');
ALTER TABLE public.jobs ADD COLUMN sync_id uuid;
ALTER TABLE public.jobs ADD FOREIGN KEY(workspace_id,connection_id,sync_id)
    REFERENCES public.sync_runs(workspace_id,connection_id,id);
ALTER TABLE public.jobs DROP CONSTRAINT jobs_payload_version_check;
-- PostgreSQL assigns this multi-column check a table-level generated name.
-- Locate it by its payload column rather than relying on that generated name.
DO $$
DECLARE
    payload_constraint name;
BEGIN
    SELECT c.conname INTO STRICT payload_constraint
    FROM pg_catalog.pg_constraint c
    JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attname='payload'
    WHERE c.conrelid='public.jobs'::regclass AND c.contype='c' AND a.attnum=ANY(c.conkey);
    EXECUTE format('ALTER TABLE public.jobs DROP CONSTRAINT %I', payload_constraint);
END;
$$;
ALTER TABLE public.jobs ADD CHECK (
    (payload_version=1 AND sync_id IS NULL AND payload=jsonb_build_object('connection_id',connection_id::text))
    OR (payload_version=2 AND sync_id IS NOT NULL
        AND payload=jsonb_build_object('connection_id',connection_id::text,'sync_id',sync_id::text))
);
ALTER TABLE public.jobs ADD CHECK (jsonb_typeof(payload)='object' AND octet_length(payload::text)<=1024);
