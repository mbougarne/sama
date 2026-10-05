ALTER TABLE public.login_challenges ADD COLUMN session_digest bytea
    CHECK (session_digest IS NULL OR octet_length(session_digest)=32);
ALTER TABLE public.login_challenges ADD CHECK (session_digest IS NULL OR invitation_digest IS NULL);
