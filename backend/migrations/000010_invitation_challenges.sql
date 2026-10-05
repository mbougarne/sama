ALTER TABLE public.login_challenges ADD COLUMN invitation_digest bytea
    CHECK (invitation_digest IS NULL OR octet_length(invitation_digest)=32);
