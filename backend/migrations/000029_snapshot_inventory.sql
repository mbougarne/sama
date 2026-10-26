-- Snapshot metadata shares resource identity and generation fencing, with a
-- separate typed allowlist. Existing server details remain unchanged.
CREATE FUNCTION public.valid_inventory_details(kind text, details jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE snapshot jsonb; key text; value jsonb;
BEGIN
    IF jsonb_typeof(details) <> 'object' OR octet_length(details::text)>65536 THEN RETURN false; END IF;
    IF kind='compute.server' THEN
        RETURN details-ARRAY['region','machine_type']='{}'::jsonb
            AND (NOT(details ? 'region') OR jsonb_typeof(details->'region')='string')
            AND (NOT(details ? 'machine_type') OR jsonb_typeof(details->'machine_type')='string');
    END IF;
    snapshot := details->'snapshot';
    IF kind<>'storage.snapshot' OR details-ARRAY['snapshot']<>'{}'::jsonb
        OR snapshot IS NULL OR jsonb_typeof(snapshot)<>'object' THEN RETURN false; END IF;
    FOR key,value IN SELECT * FROM jsonb_each(snapshot) LOOP
        IF key IN ('source_native_id','source_type','status_reason','native_created_at','creation_time_reason') THEN
            IF jsonb_typeof(value)<>'string' OR length(value#>>'{}')>512 THEN RETURN false; END IF;
        ELSIF key='created_at' THEN
            IF value<>'null'::jsonb AND (jsonb_typeof(value)<>'string'
                OR length(value#>>'{}')>64 OR (value#>>'{}') !~ '^\d{4}-\d{2}-\d{2}T') THEN RETURN false; END IF;
        ELSIF key IN ('size_bytes','size_gb','minimum_disk_gb') THEN
            IF value<>'null'::jsonb THEN
                IF jsonb_typeof(value)<>'number' THEN RETURN false; END IF;
                IF (value#>>'{}')::numeric<0 THEN RETURN false; END IF;
                IF key='size_bytes' AND ((value#>>'{}')::numeric<>trunc((value#>>'{}')::numeric)
                    OR (value#>>'{}')::numeric>9223372036854775807) THEN RETURN false; END IF;
            END IF;
        ELSIF key='regions' THEN
            IF jsonb_typeof(value)<>'array' THEN RETURN false; END IF;
            IF jsonb_array_length(value)>100 OR EXISTS(SELECT 1 FROM jsonb_array_elements(value) r
                WHERE jsonb_typeof(r)<>'string' OR length(r#>>'{}') NOT BETWEEN 1 AND 128) THEN RETURN false; END IF;
        ELSE RETURN false;
        END IF;
    END LOOP;
    RETURN COALESCE(snapshot->>'source_type','') IN ('','compute.server','storage.volume')
        AND (COALESCE(snapshot->>'source_type','')='')=(COALESCE(snapshot->>'source_native_id','')='');
END;
$$;

DO $$
DECLARE relation text; constraint_name text;
BEGIN
    FOREACH relation IN ARRAY ARRAY['resources','inventory_staging'] LOOP
        FOR constraint_name IN SELECT c.conname FROM pg_constraint c
            WHERE c.conrelid=('public.'||relation)::regclass AND c.contype='c'
            AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.conrelid
                AND a.attnum=ANY(c.conkey) AND a.attname IN ('resource_type','status','details'))
        LOOP
            EXECUTE format('ALTER TABLE public.%I DROP CONSTRAINT %I',relation,constraint_name);
        END LOOP;
        EXECUTE format('ALTER TABLE public.%I ADD CHECK (resource_type IN (''compute.server'',''storage.snapshot''))',relation);
        EXECUTE format('ALTER TABLE public.%I ADD CHECK (
            (resource_type=''compute.server'' AND status IN (''unknown'',''running'',''stopped'',''pending'',''failed''))
            OR (resource_type=''storage.snapshot'' AND status IN (''unknown'',''available'',''pending'',''failed'',''deleted'')))',relation);
        EXECUTE format('ALTER TABLE public.%I ADD CHECK (public.valid_inventory_details(resource_type,details))',relation);
    END LOOP;
END;
$$;
ALTER TABLE public.sync_runs DROP CONSTRAINT sync_runs_resource_type_check;
ALTER TABLE public.sync_runs ADD CHECK (resource_type IN ('compute.server','storage.snapshot'));
ALTER TABLE public.resource_locks DROP CONSTRAINT resource_locks_resource_type_check;
ALTER TABLE public.resource_locks ADD CHECK (resource_type IN ('compute.server','storage.snapshot'));
