ALTER TABLE public.workspaces
    ADD COLUMN queue_limit integer NOT NULL DEFAULT 1000 CHECK (queue_limit BETWEEN 1 AND 1000),
    ADD COLUMN audit_retention_days integer NOT NULL DEFAULT 180 CHECK (audit_retention_days BETWEEN 180 AND 3650);
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_event_type_check;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_event_type_check CHECK (
    event_type IN ('workspace.created','membership.changed','connection.created',
        'connection.credential_rotated','connection.disabled','operation.accepted',
        'operation.completed','operation.failed','operation.reconciled','auth.login','auth.logout',
        'invitation.created','workspace.settings_changed')
);
ALTER FUNCTION public.valid_audit_metadata(text,jsonb) RENAME TO valid_audit_metadata_v3;
CREATE FUNCTION public.valid_audit_metadata(kind text, metadata jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN kind='workspace.settings_changed' THEN
        metadata - ARRAY['policy_version']='{}'::jsonb AND metadata ? 'policy_version'
        AND jsonb_typeof(metadata->'policy_version')='string'
        AND metadata->>'policy_version' ~ '^[1-9][0-9]{0,18}$'
        ELSE public.valid_audit_metadata_v3(kind,metadata) END;
$$;
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_metadata_allowlist;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_metadata_allowlist
    CHECK (public.valid_audit_metadata(event_type,metadata));
