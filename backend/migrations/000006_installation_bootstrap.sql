CREATE TABLE public.installation_bootstrap (
    singleton boolean PRIMARY KEY CHECK (singleton),
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    workspace_id uuid NOT NULL REFERENCES public.workspaces(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
