CREATE TABLE public.connection_grants (
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    user_id uuid NOT NULL,
    actions text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (workspace_id, connection_id, user_id),
    FOREIGN KEY (workspace_id, connection_id) REFERENCES public.connections(workspace_id, id),
    FOREIGN KEY (workspace_id, user_id) REFERENCES public.memberships(workspace_id, user_id) ON DELETE CASCADE,
    CHECK (actions <@ ARRAY['read','refresh','operate','high_impact']::text[] AND cardinality(actions) <= 4)
);

ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_event_type_check;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_event_type_check CHECK (
    event_type IN ('workspace.created','membership.changed','connection.created',
        'connection.credential_rotated','connection.disabled','operation.accepted',
        'operation.completed','operation.failed','operation.reconciled','auth.login','auth.logout',
        'invitation.created','workspace.settings_changed','connection.grants_changed','connection.grants_denied')
);
ALTER FUNCTION public.valid_audit_metadata(text,jsonb) RENAME TO valid_audit_metadata_v4;
CREATE FUNCTION public.valid_audit_metadata(kind text, metadata jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN kind IN ('connection.grants_changed','connection.grants_denied') THEN
        metadata ?& ARRAY['member_id','connection_id']
        AND metadata - ARRAY['member_id','connection_id']='{}'::jsonb
        AND public.valid_audit_metadata_v4('membership.changed',jsonb_build_object('member_id',metadata->'member_id'))
        AND public.valid_audit_metadata_v4('connection.disabled',jsonb_build_object('connection_id',metadata->'connection_id'))
        ELSE public.valid_audit_metadata_v4(kind,metadata) END;
$$;
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_metadata_allowlist;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_metadata_allowlist
    CHECK (public.valid_audit_metadata(event_type,metadata));
