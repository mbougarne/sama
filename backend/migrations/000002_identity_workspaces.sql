CREATE TABLE public.users (
    id uuid PRIMARY KEY,
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 2048),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    display_name text NOT NULL DEFAULT '' CHECK (length(display_name) <= 200),
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (issuer, subject)
);

CREATE TABLE public.workspaces (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    policy_version bigint NOT NULL DEFAULT 1 CHECK (policy_version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    closed_at timestamptz
);

CREATE TABLE public.memberships (
    workspace_id uuid NOT NULL REFERENCES public.workspaces(id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    role text NOT NULL CHECK (role IN ('viewer', 'operator', 'admin', 'owner')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX memberships_user_workspace_idx ON public.memberships (user_id, workspace_id);

-- Membership removal must preserve both actor identity and workspace evidence.
ALTER TABLE public.audit_events
    ADD CONSTRAINT audit_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES public.workspaces(id) ON DELETE RESTRICT,
    ADD CONSTRAINT audit_actor_fk FOREIGN KEY (actor_id)
        REFERENCES public.users(id) ON DELETE RESTRICT;
