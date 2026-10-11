-- AgentGate tool definition catalog. Idempotent.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(64) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tool_definitions (
    backend_id  text NOT NULL,
    tool_name   text NOT NULL,
    risk        text NOT NULL,
    schema_json jsonb NOT NULL,
    arguments   jsonb NOT NULL,
    description text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL,
    updated_by  text NOT NULL,
    PRIMARY KEY (backend_id, tool_name),
    CONSTRAINT tool_definitions_risk_check CHECK (risk IN ('read','write','destructive'))
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'agentgate_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON tool_definitions TO agentgate_app;
    END IF;
END
$$;

INSERT INTO schema_migrations (version)
VALUES ('004_create_tool_catalog')
ON CONFLICT (version) DO NOTHING;
