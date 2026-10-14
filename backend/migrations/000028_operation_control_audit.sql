ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_event_type_check;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_event_type_check CHECK (
 event_type IN ('workspace.created','membership.changed','connection.created',
 'connection.credential_rotated','connection.disabled','operation.accepted',
 'operation.completed','operation.failed','operation.reconciled','auth.login','auth.logout',
 'invitation.created','workspace.settings_changed','connection.grants_changed','connection.grants_denied',
 'operation.cancelled','operation.reservation_released')
);
ALTER FUNCTION public.valid_audit_metadata(text,jsonb) RENAME TO valid_audit_metadata_v5;
CREATE FUNCTION public.valid_audit_metadata(kind text,metadata jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
 SELECT public.valid_audit_metadata_v5(
 CASE WHEN kind IN ('operation.cancelled','operation.reservation_released') THEN 'operation.reconciled' ELSE kind END,metadata);
$$;
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_metadata_allowlist;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_metadata_allowlist
 CHECK (public.valid_audit_metadata(event_type,metadata));
