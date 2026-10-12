ALTER TABLE public.resources ADD UNIQUE(workspace_id,connection_id,id);

CREATE TABLE public.action_reviews (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    actor_id uuid NOT NULL REFERENCES public.users(id),
    connection_id uuid NOT NULL,
    resource_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('compute.server.power_on','compute.server.shutdown','compute.server.power_off','compute.server.reboot')),
    intent_version integer NOT NULL CHECK (intent_version=1),
    intent jsonb NOT NULL CHECK (intent=jsonb_build_object('action',action,'parameters','{}'::jsonb)),
    intent_hash bytea NOT NULL CHECK (octet_length(intent_hash)=32),
    precondition_hash bytea NOT NULL CHECK (octet_length(precondition_hash)=32),
    credential_version bigint NOT NULL CHECK (credential_version>0),
    policy_version bigint NOT NULL CHECK (policy_version>0),
    high_impact boolean NOT NULL,
    target_name text NOT NULL CHECK (length(target_name)<=200),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    consumed_by uuid,
    FOREIGN KEY(workspace_id,connection_id,resource_id) REFERENCES public.resources(workspace_id,connection_id,id),
    FOREIGN KEY(workspace_id,connection_id,credential_version) REFERENCES public.credential_versions(workspace_id,connection_id,version),
    UNIQUE(workspace_id,connection_id,id),
    CHECK (expires_at>created_at AND expires_at<=created_at+interval '5 minutes 5 seconds')
);

CREATE TABLE public.operations (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    actor_id uuid NOT NULL REFERENCES public.users(id),
    connection_id uuid NOT NULL,
    resource_id uuid NOT NULL,
    action text NOT NULL,
    intent_version integer NOT NULL CHECK (intent_version=1),
    intent jsonb NOT NULL CHECK (intent=jsonb_build_object('action',action,'parameters','{}'::jsonb)),
    intent_hash bytea NOT NULL CHECK (octet_length(intent_hash)=32),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    review_id uuid NOT NULL,
    credential_version bigint NOT NULL,
    policy_version bigint NOT NULL,
    state text NOT NULL CHECK (state IN ('queued','dispatching','waiting_provider','reconciling','unknown','succeeded','failed','cancelled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    event_sequence bigint NOT NULL DEFAULT 0 CHECK (event_sequence>=0),
    deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(workspace_id,connection_id,resource_id) REFERENCES public.resources(workspace_id,connection_id,id),
    FOREIGN KEY(workspace_id,connection_id,review_id) REFERENCES public.action_reviews(workspace_id,connection_id,id),
    FOREIGN KEY(workspace_id,connection_id,credential_version) REFERENCES public.credential_versions(workspace_id,connection_id,version),
    UNIQUE(workspace_id,id),
    UNIQUE(workspace_id,connection_id,id),
    UNIQUE(workspace_id,actor_id,idempotency_key),
    UNIQUE(workspace_id,review_id),
    CHECK (deadline>created_at)
);
ALTER TABLE public.action_reviews ADD FOREIGN KEY(workspace_id,connection_id,consumed_by)
    REFERENCES public.operations(workspace_id,connection_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE public.operation_events (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    sequence bigint NOT NULL CHECK (sequence>0),
    from_state text NOT NULL,
    to_state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(workspace_id,operation_id) REFERENCES public.operations(workspace_id,id),
    UNIQUE(workspace_id,operation_id,sequence)
);

CREATE FUNCTION public.preserve_operation_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.workspace_id,NEW.actor_id,NEW.connection_id,NEW.resource_id,NEW.action,
        NEW.intent_version,NEW.intent,NEW.intent_hash,NEW.idempotency_key,NEW.review_id,
        NEW.credential_version,NEW.policy_version,NEW.deadline)
        IS DISTINCT FROM ROW(OLD.id,OLD.workspace_id,OLD.actor_id,OLD.connection_id,OLD.resource_id,OLD.action,
        OLD.intent_version,OLD.intent,OLD.intent_hash,OLD.idempotency_key,OLD.review_id,
        OLD.credential_version,OLD.policy_version,OLD.deadline) THEN
        RAISE EXCEPTION 'operation intent is immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER preserve_operation_intent BEFORE UPDATE ON public.operations
    FOR EACH ROW EXECUTE FUNCTION public.preserve_operation_intent();

ALTER TABLE public.jobs ADD COLUMN operation_id uuid;
ALTER TABLE public.jobs ADD FOREIGN KEY(workspace_id,connection_id,operation_id)
    REFERENCES public.operations(workspace_id,connection_id,id);
ALTER TABLE public.jobs DROP CONSTRAINT jobs_type_check;
ALTER TABLE public.jobs ADD CHECK (type IN ('connection_refresh','operation_dispatch'));
DO $$
DECLARE payload_constraint name;
BEGIN
    SELECT c.conname INTO STRICT payload_constraint FROM pg_catalog.pg_constraint c
    JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attname='payload_version'
    WHERE c.conrelid='public.jobs'::regclass AND c.contype='c' AND a.attnum=ANY(c.conkey);
    EXECUTE format('ALTER TABLE public.jobs DROP CONSTRAINT %I',payload_constraint);
END;
$$;
ALTER TABLE public.jobs ADD CHECK (
    (type='connection_refresh' AND operation_id IS NULL AND payload_version=1 AND sync_id IS NULL
        AND payload=jsonb_build_object('connection_id',connection_id::text))
    OR (type='connection_refresh' AND operation_id IS NULL AND payload_version=2 AND sync_id IS NOT NULL
        AND payload=jsonb_build_object('connection_id',connection_id::text,'sync_id',sync_id::text))
    OR (type='operation_dispatch' AND payload_version=3 AND sync_id IS NULL AND operation_id IS NOT NULL
        AND payload=jsonb_build_object('connection_id',connection_id::text,'operation_id',operation_id::text))
);
CREATE UNIQUE INDEX jobs_operation ON public.jobs(operation_id) WHERE operation_id IS NOT NULL;
CREATE INDEX action_reviews_expiry ON public.action_reviews(expires_at) WHERE consumed_by IS NULL;
CREATE INDEX operations_history ON public.operations(workspace_id,created_at DESC,id DESC);

CREATE FUNCTION public.preserve_action_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (to_jsonb(NEW)-'consumed_by') IS DISTINCT FROM (to_jsonb(OLD)-'consumed_by')
        OR (OLD.consumed_by IS NOT NULL AND NEW.consumed_by IS DISTINCT FROM OLD.consumed_by) THEN
        RAISE EXCEPTION 'action review is immutable and consumed once';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER preserve_action_review BEFORE UPDATE ON public.action_reviews
    FOR EACH ROW EXECUTE FUNCTION public.preserve_action_review();
