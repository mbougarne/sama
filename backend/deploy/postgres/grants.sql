-- Apply after the migration command to a dedicated Sama database, as an administrator.
-- Supply identifiers with psql -v database_name=... -v migration_role=... -v runtime_role=...
REVOKE CREATE, TEMPORARY ON DATABASE :"database_name" FROM PUBLIC;
REVOKE CREATE, TEMPORARY ON DATABASE :"database_name" FROM :"runtime_role";
GRANT CONNECT ON DATABASE :"database_name" TO :"migration_role";
GRANT CONNECT ON DATABASE :"database_name" TO :"runtime_role";

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO :"migration_role";
REVOKE ALL ON SCHEMA public FROM :"runtime_role";
GRANT USAGE ON SCHEMA public TO :"runtime_role";

ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public
    REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :"runtime_role";

REVOKE ALL ON TABLE public.sama_schema_migrations FROM PUBLIC, :"runtime_role";
REVOKE ALL ON TABLE public.audit_events FROM PUBLIC, :"runtime_role";
GRANT SELECT, INSERT ON TABLE public.audit_events TO :"runtime_role";
