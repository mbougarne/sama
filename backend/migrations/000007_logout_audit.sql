ALTER TABLE public.sessions ADD COLUMN audit_workspace_id uuid REFERENCES public.workspaces(id) ON DELETE RESTRICT;
UPDATE public.sessions s SET audit_workspace_id = COALESCE(
    (SELECT m.workspace_id FROM public.memberships m WHERE m.user_id=s.user_id ORDER BY m.workspace_id LIMIT 1),
    (SELECT a.workspace_id FROM public.audit_events a WHERE a.actor_id=s.user_id ORDER BY a.created_at DESC LIMIT 1)
);
-- Revoke any legacy session without a retained audit scope.
DELETE FROM public.sessions WHERE audit_workspace_id IS NULL;
ALTER TABLE public.sessions ALTER COLUMN audit_workspace_id SET NOT NULL;

ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_event_type_check;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_event_type_check CHECK (
    event_type IN ('workspace.created', 'membership.changed', 'connection.created',
        'connection.credential_rotated', 'connection.disabled', 'operation.accepted',
        'operation.completed', 'operation.failed', 'operation.reconciled', 'auth.login', 'auth.logout')
);
ALTER FUNCTION public.valid_audit_metadata(text,jsonb) RENAME TO valid_audit_metadata_v1;
CREATE FUNCTION public.valid_audit_metadata(kind text, metadata jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN kind='auth.logout' THEN metadata='{}'::jsonb
        ELSE public.valid_audit_metadata_v1(kind,metadata) END;
$$;
ALTER TABLE public.audit_events DROP CONSTRAINT audit_events_metadata_allowlist;
ALTER TABLE public.audit_events ADD CONSTRAINT audit_events_metadata_allowlist
    CHECK (public.valid_audit_metadata(event_type, metadata));
