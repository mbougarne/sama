CREATE TABLE public.invitations (
    token_digest bytea PRIMARY KEY CHECK (octet_length(token_digest)=32),
    workspace_id uuid NOT NULL REFERENCES public.workspaces(id) ON DELETE RESTRICT,
    inviter_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    inviter_version bigint NOT NULL CHECK (inviter_version>0),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 2048),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    role text NOT NULL CHECK (role IN ('viewer','operator','admin','owner')),
    expires_at timestamptz NOT NULL
);
CREATE INDEX invitations_workspace_expiry_idx ON public.invitations(workspace_id,expires_at);
