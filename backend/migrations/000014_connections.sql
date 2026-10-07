CREATE TABLE public.connections (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES public.workspaces(id) ON DELETE RESTRICT,
    provider text NOT NULL CHECK (length(provider) BETWEEN 1 AND 64),
    api_family text NOT NULL CHECK (length(api_family) BETWEEN 1 AND 64),
    account_identity text NOT NULL CHECK (length(account_identity) BETWEEN 1 AND 512),
    region_policy text[] NOT NULL DEFAULT '{}',
    label text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 100),
    status text NOT NULL DEFAULT 'disabled' CHECK (status IN ('active', 'disabled')),
    active_credential_version bigint CHECK (active_credential_version > 0),
    permissions_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    CHECK (status <> 'active' OR active_credential_version IS NOT NULL),
    CHECK (cardinality(region_policy) <= 100)
);

CREATE TABLE public.credential_versions (
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 1 AND 65536),
    nonce bytea NOT NULL CHECK (octet_length(nonce) BETWEEN 1 AND 64),
    wrapped_data_key bytea NOT NULL CHECK (octet_length(wrapped_data_key) BETWEEN 1 AND 1024),
    wrapping_nonce bytea NOT NULL CHECK (octet_length(wrapping_nonce) BETWEEN 1 AND 64),
    encryption_format integer NOT NULL CHECK (encryption_format > 0),
    master_key_id text NOT NULL CHECK (length(master_key_id) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz,
    PRIMARY KEY (workspace_id, connection_id, version),
    FOREIGN KEY (workspace_id, connection_id) REFERENCES public.connections(workspace_id, id) ON DELETE RESTRICT,
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

-- Deferred for atomic connection + credential creation in a future save service.
ALTER TABLE public.connections ADD CONSTRAINT connection_active_credential_fk
    FOREIGN KEY (workspace_id, id, active_credential_version)
    REFERENCES public.credential_versions(workspace_id, connection_id, version)
    DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION public.preserve_connection_binding() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.id, NEW.workspace_id, NEW.provider, NEW.api_family, NEW.account_identity)
       IS DISTINCT FROM (OLD.id, OLD.workspace_id, OLD.provider, OLD.api_family, OLD.account_identity) THEN
        RAISE EXCEPTION 'connection binding is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER connection_binding_immutable BEFORE UPDATE ON public.connections
    FOR EACH ROW EXECUTE FUNCTION public.preserve_connection_binding();

-- Rewrapping may change only the key envelope; ciphertext remains bound to its identity.
CREATE FUNCTION public.preserve_credential_binding() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.workspace_id, NEW.connection_id, NEW.version, NEW.ciphertext, NEW.nonce, NEW.encryption_format, NEW.created_at)
       IS DISTINCT FROM (OLD.workspace_id, OLD.connection_id, OLD.version, OLD.ciphertext, OLD.nonce, OLD.encryption_format, OLD.created_at)
       OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'credential binding is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER credential_binding_immutable BEFORE UPDATE ON public.credential_versions
    FOR EACH ROW EXECUTE FUNCTION public.preserve_credential_binding();
