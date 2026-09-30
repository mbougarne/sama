CREATE TABLE public.sessions (
    token_digest bytea PRIMARY KEY CHECK (octet_length(token_digest) = 32),
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    csrf_digest bytea NOT NULL CHECK (octet_length(csrf_digest) = 32),
    authenticated_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    CHECK (created_at <= last_seen_at AND last_seen_at < absolute_expires_at),
    CHECK (idle_expires_at <= absolute_expires_at)
);
CREATE INDEX sessions_expiry_idx ON public.sessions (idle_expires_at);
CREATE INDEX sessions_user_idx ON public.sessions (user_id);
