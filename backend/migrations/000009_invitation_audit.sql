ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_event_type_check;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_event_type_check CHECK (
    event_type IN ('workspace.created','membership.changed','connection.created',
        'connection.credential_rotated','connection.disabled','operation.accepted',
        'operation.completed','operation.failed','operation.reconciled','auth.login','auth.logout','invitation.created')
);
ALTER FUNCTION public.valid_audit_metadata(text,jsonb) RENAME TO valid_audit_metadata_v2;
CREATE FUNCTION public.valid_audit_metadata(kind text, metadata jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN kind='invitation.created' THEN
        metadata - ARRAY['role']='{}'::jsonb AND metadata ? 'role'
        AND jsonb_typeof(metadata->'role')='string'
        AND metadata->>'role' IN ('viewer','operator','admin','owner')
        ELSE public.valid_audit_metadata_v2(kind,metadata) END;
$$;
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_metadata_allowlist;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_metadata_allowlist
    CHECK (public.valid_audit_metadata(event_type,metadata));
