CREATE TABLE public.resource_locks (
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('resource','creation')),
    resource_type text NOT NULL CHECK (resource_type='compute.server'),
    scope text NOT NULL CHECK (length(scope) BETWEEN 1 AND 128),
    native_id text NOT NULL CHECK (length(native_id) BETWEEN 1 AND 512),
    operation_id uuid NOT NULL,
    fencing_version bigint GENERATED ALWAYS AS IDENTITY,
    PRIMARY KEY(workspace_id,connection_id,target_kind,resource_type,scope,native_id),
    UNIQUE(workspace_id,operation_id),
    FOREIGN KEY(workspace_id,connection_id,operation_id)
        REFERENCES public.operations(workspace_id,connection_id,id)
);
