-- These facts are written only after validation by a qualified read adapter.
ALTER TABLE public.connections ADD COLUMN verified_capabilities text[] NOT NULL DEFAULT '{}'
    CHECK (verified_capabilities <@ ARRAY['compute.server.read','compute.server.power_on',
        'compute.server.shutdown','compute.server.power_off','compute.server.reboot']::text[]
        AND cardinality(verified_capabilities)<=5);
