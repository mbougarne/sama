ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_event_type_check;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_event_type_check CHECK (
    event_type IN ('workspace.created', 'membership.changed', 'connection.created',
        'connection.credential_rotated', 'connection.disabled', 'operation.accepted',
        'operation.completed', 'operation.failed', 'operation.reconciled', 'auth.login')
);
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_metadata_allowlist;
-- Preserve all earlier allowlists in a reusable immutable validation function.
CREATE FUNCTION public.valid_audit_metadata(kind text, metadata jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN kind IN ('workspace.created', 'auth.login') THEN metadata = '{}'::jsonb
        WHEN kind = 'membership.changed' THEN
            metadata - ARRAY['member_id','role'] = '{}'::jsonb
            AND (NOT (metadata ? 'role') OR metadata->>'role' IN ('viewer','operator','admin','owner'))
            AND (NOT (metadata ? 'member_id') OR metadata->>'member_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
        WHEN kind IN ('connection.created','connection.credential_rotated','connection.disabled') THEN
            metadata - ARRAY['connection_id'] = '{}'::jsonb
            AND (NOT (metadata ? 'connection_id') OR metadata->>'connection_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
        WHEN kind IN ('operation.accepted','operation.completed','operation.failed','operation.reconciled') THEN
            metadata - CASE WHEN kind IN ('operation.failed','operation.reconciled') THEN ARRAY['operation_id','resource_id','reason_code'] ELSE ARRAY['operation_id','resource_id'] END = '{}'::jsonb
            AND (NOT (metadata ? 'operation_id') OR metadata->>'operation_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
            AND (NOT (metadata ? 'resource_id') OR metadata->>'resource_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
            AND (NOT (metadata ? 'reason_code') OR metadata->>'reason_code' ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$')
        ELSE false
    END AND NOT EXISTS (SELECT 1 FROM jsonb_each(metadata) e WHERE jsonb_typeof(e.value) <> 'string');
$$;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_metadata_allowlist
    CHECK (public.valid_audit_metadata(event_type, metadata));
