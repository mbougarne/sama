CREATE TABLE public.login_challenges (
    state_digest bytea PRIMARY KEY CHECK (octet_length(state_digest) = 32),
    browser_digest bytea NOT NULL CHECK (octet_length(browser_digest) = 32),
    nonce text NOT NULL CHECK (length(nonce) = 43),
    verifier text NOT NULL CHECK (length(verifier) = 43),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX login_challenges_created_idx ON public.login_challenges (created_at);
CREATE INDEX login_challenges_expiry_idx ON public.login_challenges (expires_at);

-- Issuance accounting survives successful/failed challenge consumption.
CREATE TABLE public.login_initiation_budget (
    singleton boolean PRIMARY KEY CHECK (singleton),
    window_started_at timestamptz NOT NULL,
    issued integer NOT NULL CHECK (issued BETWEEN 1 AND 30)
);
