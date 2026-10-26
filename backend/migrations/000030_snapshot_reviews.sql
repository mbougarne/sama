-- Syntax/storage support does not enable an action: production review/mutation
-- factories and the separately qualified switch intersection remain closed.
ALTER TABLE public.action_reviews DROP CONSTRAINT action_reviews_action_check;
ALTER TABLE public.action_reviews ADD CHECK (action IN (
    'compute.server.power_on','compute.server.shutdown','compute.server.power_off',
    'compute.server.reboot','compute.server.snapshot_create','storage.snapshot.delete'
));

ALTER TABLE public.operations DROP CONSTRAINT operations_provider_reference_check;
ALTER TABLE public.operations ADD CHECK (provider_reference IS NULL OR
    (action IN ('compute.server.snapshot_create','storage.snapshot.delete')
        AND provider_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}(/[A-Za-z0-9][A-Za-z0-9._:-]{0,127}){0,2}$')
    OR (action NOT IN ('compute.server.snapshot_create','storage.snapshot.delete')
        AND provider_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'));

-- Vocabulary only: no connection is granted or marked verified by this migration.
ALTER TABLE public.connections DROP CONSTRAINT connections_verified_capabilities_check;
ALTER TABLE public.connections ADD CHECK (verified_capabilities <@ ARRAY[
    'compute.server.read','compute.server.power_on','compute.server.shutdown',
    'compute.server.power_off','compute.server.reboot','compute.server.snapshot_create',
    'storage.snapshot.read','storage.snapshot.delete']::text[]
    AND cardinality(verified_capabilities)<=8);
