-- Runtime may read/lock the gate; only the installation migration identity can
-- deliberately close/reopen it. Fresh installations also start closed.
CREATE TABLE public.dispatch_recovery_gate (
 singleton boolean PRIMARY KEY CHECK (singleton),
 open boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 changed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO public.dispatch_recovery_gate(singleton) VALUES(true);
