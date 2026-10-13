-- Optional opaque provider action identity; never a URL, token or raw body.
ALTER TABLE public.operations ADD COLUMN provider_reference text
    CHECK (provider_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$');
