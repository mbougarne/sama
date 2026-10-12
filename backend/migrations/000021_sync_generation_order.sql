-- Database-assigned ordering is independent of UUID clocks and random bits.
-- Admission already serializes scopes under the workspace lock.
ALTER TABLE public.sync_runs ADD COLUMN generation_number bigint GENERATED ALWAYS AS IDENTITY;
CREATE UNIQUE INDEX sync_runs_generation_number ON public.sync_runs(generation_number);
