CREATE TABLE public.audit_events (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    actor_kind text NOT NULL CHECK (actor_kind IN ('user', 'system', 'installation')),
    actor_id uuid,
    CHECK ((actor_kind = 'user' AND actor_id IS NOT NULL) OR (actor_kind <> 'user' AND actor_id IS NULL)),
    event_type text NOT NULL CHECK (event_type IN (
        'workspace.created',
        'membership.changed',
        'connection.created',
        'connection.credential_rotated',
        'connection.disabled',
        'operation.accepted',
        'operation.completed',
        'operation.failed',
        'operation.reconciled'
    )),
    request_id text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (
        jsonb_typeof(metadata) = 'object' AND octet_length(metadata::text) <= 512
    ),
    CONSTRAINT audit_events_metadata_allowlist CHECK (
        CASE
            WHEN event_type = 'workspace.created' THEN metadata = '{}'::jsonb
            WHEN event_type = 'membership.changed' THEN
                metadata - ARRAY['member_id', 'role'] = '{}'::jsonb
                AND (NOT (metadata ? 'member_id') OR (
                    jsonb_typeof(metadata->'member_id') = 'string'
                    AND metadata->>'member_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                ))
                AND (NOT (metadata ? 'role') OR (
                    jsonb_typeof(metadata->'role') = 'string'
                    AND metadata->>'role' IN ('viewer', 'operator', 'admin', 'owner')
                ))
            WHEN event_type IN ('connection.created', 'connection.credential_rotated', 'connection.disabled') THEN
                metadata - ARRAY['connection_id'] = '{}'::jsonb
                AND (NOT (metadata ? 'connection_id') OR (
                    jsonb_typeof(metadata->'connection_id') = 'string'
                    AND metadata->>'connection_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                ))
            WHEN event_type IN ('operation.accepted', 'operation.completed') THEN
                metadata - ARRAY['operation_id', 'resource_id'] = '{}'::jsonb
                AND (NOT (metadata ? 'operation_id') OR (
                    jsonb_typeof(metadata->'operation_id') = 'string'
                    AND metadata->>'operation_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                ))
                AND (NOT (metadata ? 'resource_id') OR (
                    jsonb_typeof(metadata->'resource_id') = 'string'
                    AND metadata->>'resource_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                ))
            WHEN event_type IN ('operation.failed', 'operation.reconciled') THEN
                metadata - ARRAY['operation_id', 'resource_id', 'reason_code'] = '{}'::jsonb
                AND (NOT (metadata ? 'operation_id') OR (
                    jsonb_typeof(metadata->'operation_id') = 'string'
                    AND metadata->>'operation_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                ))
                AND (NOT (metadata ? 'resource_id') OR (
                    jsonb_typeof(metadata->'resource_id') = 'string'
                    AND metadata->>'resource_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                ))
                AND (NOT (metadata ? 'reason_code') OR (
                    jsonb_typeof(metadata->'reason_code') = 'string'
                    AND metadata->>'reason_code' ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'
                ))
            ELSE false
        END
    ),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX audit_events_workspace_created_id_idx
    ON public.audit_events (workspace_id, created_at DESC, id DESC);
