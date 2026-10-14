ALTER TABLE public.audit_events ADD UNIQUE(workspace_id,id);
CREATE TABLE public.operation_resolutions (
 id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES public.users(id),
 audit_id uuid NOT NULL,
 residual_risk_acknowledged boolean NOT NULL CHECK (residual_risk_acknowledged),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(workspace_id,operation_id),
 FOREIGN KEY(workspace_id,operation_id) REFERENCES public.operations(workspace_id,id),
 FOREIGN KEY(workspace_id,audit_id) REFERENCES public.audit_events(workspace_id,id)
);
