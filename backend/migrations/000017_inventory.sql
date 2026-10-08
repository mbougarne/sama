CREATE TABLE public.sync_runs (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    resource_type text NOT NULL CHECK (resource_type='compute.server'),
    scope text NOT NULL CHECK (length(scope) BETWEEN 1 AND 128),
    generation uuid NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed','cancelled')),
    page_count integer NOT NULL DEFAULT 0 CHECK (page_count>=0),
    started_at timestamptz,
    completed_at timestamptz,
    FOREIGN KEY(workspace_id,connection_id) REFERENCES public.connections(workspace_id,id),
    UNIQUE(workspace_id,connection_id,id),
    UNIQUE(workspace_id,connection_id,resource_type,scope,generation)
);

CREATE TABLE public.resources (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    api_family text NOT NULL CHECK (length(api_family) BETWEEN 1 AND 64),
    resource_type text NOT NULL CHECK (resource_type='compute.server'),
    scope text NOT NULL CHECK (length(scope) BETWEEN 1 AND 128),
    native_id text NOT NULL CHECK (length(native_id) BETWEEN 1 AND 512),
    native_status text NOT NULL CHECK (length(native_status)<=128),
    status text NOT NULL CHECK (status IN ('unknown','running','stopped','pending','failed')),
    name text NOT NULL CHECK (length(name)<=200),
    details_version integer NOT NULL CHECK (details_version=1),
    details jsonb NOT NULL CHECK (jsonb_typeof(details)='object' AND octet_length(details::text)<=65536
        AND details - ARRAY['region','machine_type']='{}'::jsonb
        AND (NOT(details ? 'region') OR jsonb_typeof(details->'region')='string')
        AND (NOT(details ? 'machine_type') OR jsonb_typeof(details->'machine_type')='string')),
    observed_at timestamptz NOT NULL,
    generation uuid NOT NULL,
    tombstoned_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    FOREIGN KEY(workspace_id,connection_id) REFERENCES public.connections(workspace_id,id),
    FOREIGN KEY(workspace_id,connection_id,resource_type,scope,generation)
        REFERENCES public.sync_runs(workspace_id,connection_id,resource_type,scope,generation),
    UNIQUE(workspace_id,id),
    UNIQUE(workspace_id,connection_id,api_family,resource_type,scope,native_id)
);
CREATE INDEX resources_workspace_type_id ON public.resources(workspace_id,resource_type,id);
