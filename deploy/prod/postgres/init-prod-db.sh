#!/bin/sh
set -e

# Read passwords from Docker secrets or environment variables
APP_PASSWORD=$(cat /run/secrets/app_password 2>/dev/null || echo "${AGENTGATE_APP_PASSWORD:-agentgate-app-prod-pw}")
MIGRATOR_PASSWORD=$(cat /run/secrets/migrator_password 2>/dev/null || echo "${AGENTGATE_MIGRATOR_PASSWORD:-agentgate-migrator-prod-pw}")
KEYCLOAK_PASSWORD=$(cat /run/secrets/keycloak_db_password 2>/dev/null || echo "${KEYCLOAK_DB_PASSWORD:-keycloak-db-prod-pw}")

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- 1. Create Keycloak database and user
    DO \$\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'keycloak') THEN
            CREATE USER keycloak WITH ENCRYPTED PASSWORD '$KEYCLOAK_PASSWORD';
        END IF;
    END
    \$\$;

    SELECT 'CREATE DATABASE keycloak OWNER keycloak'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'keycloak')\gexec

    GRANT ALL PRIVILEGES ON DATABASE keycloak TO keycloak;

    -- 2. Create migrator user for agentgate schema migrations
    DO \$\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'agentgate_migrator') THEN
            CREATE USER agentgate_migrator WITH ENCRYPTED PASSWORD '$MIGRATOR_PASSWORD';
        END IF;
    END
    \$\$;

    GRANT CONNECT ON DATABASE "$POSTGRES_DB" TO agentgate_migrator;
    GRANT ALL ON SCHEMA public TO agentgate_migrator;

    -- 3. Create restricted runtime user for agentgate application (G5 privilege separation)
    DO \$\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'agentgate_app') THEN
            CREATE USER agentgate_app WITH ENCRYPTED PASSWORD '$APP_PASSWORD';
        END IF;
    END
    \$\$;

    GRANT CONNECT ON DATABASE "$POSTGRES_DB" TO agentgate_app;
    GRANT USAGE ON SCHEMA public TO agentgate_app;

    -- 4. Default privileges: migrator creates tables, app user has runtime access
    ALTER DEFAULT PRIVILEGES FOR ROLE agentgate_migrator IN SCHEMA public
        GRANT SELECT, INSERT, UPDATE ON TABLES TO agentgate_app;

    ALTER DEFAULT PRIVILEGES FOR ROLE agentgate_migrator IN SCHEMA public
        GRANT USAGE, SELECT ON SEQUENCES TO agentgate_app;

    -- 5. Immutability boundary: prevent UPDATE/DELETE on audit_events
    CREATE OR REPLACE FUNCTION enforce_audit_immutability() RETURNS event_trigger AS \$\$
    BEGIN
        IF EXISTS (SELECT FROM pg_tables WHERE schemaname = 'public' AND tablename = 'audit_events') THEN
            REVOKE UPDATE, DELETE ON TABLE audit_events FROM agentgate_app;
        END IF;
    END;
    \$\$ LANGUAGE plpgsql;

    DROP EVENT TRIGGER IF EXISTS audit_immutability_trigger;
    CREATE EVENT TRIGGER audit_immutability_trigger ON ddl_command_end
        WHEN TAG IN ('CREATE TABLE')
        EXECUTE FUNCTION enforce_audit_immutability();
EOSQL
