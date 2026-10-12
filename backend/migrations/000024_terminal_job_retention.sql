-- Unknown legacy completion times remain NULL and cannot be pruned by age.
ALTER TABLE public.jobs ADD COLUMN completed_at timestamptz;
ALTER TABLE public.jobs ADD CHECK (completed_at IS NULL OR completed_at>=created_at);
CREATE INDEX jobs_terminal_retention ON public.jobs(completed_at,id)
    WHERE state IN ('succeeded','failed','cancelled');
