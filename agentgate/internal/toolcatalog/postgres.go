package toolcatalog

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

//go:embed migrations/004_create_tool_catalog.sql
var migration004SQL string

// PostgresStore implements Store using PostgreSQL via pgx/v5.
type PostgresStore struct {
	pool *pgxpool.Pool
}

type storedArg struct {
	Name     string          `json:"name"`
	Type     argdecl.ArgType `json:"type"`
	Required bool            `json:"required"`
}

// NewPostgresStore establishes a connection pool to PostgreSQL.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	poolCfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("toolcatalog: parse postgres config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("toolcatalog: connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("toolcatalog: ping postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

// Migrate applies the (idempotent) catalog schema.
func (s *PostgresStore) Migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("toolcatalog: begin migration tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint:errcheck

	if _, err := tx.Exec(ctx, migration004SQL); err != nil {
		return fmt.Errorf("toolcatalog: apply 004_create_tool_catalog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("toolcatalog: commit migration tx: %w", err)
	}
	return nil
}

const selectCols = `backend_id, tool_name, risk, schema_json, arguments, description, updated_at, updated_by`

func scanDef(row pgx.Row) (ToolDefinition, error) {
	var d ToolDefinition
	var risk string
	var schema, args []byte
	if err := row.Scan(&d.BackendID, &d.ToolName, &risk, &schema, &args, &d.Description, &d.UpdatedAt, &d.UpdatedBy); err != nil {
		return ToolDefinition{}, err
	}
	d.Risk = toolregistry.RiskLevel(risk)
	d.SchemaJSON = json.RawMessage(schema)
	var stored []storedArg
	if err := json.Unmarshal(args, &stored); err != nil {
		return ToolDefinition{}, fmt.Errorf("decode arguments: %w", err)
	}
	if len(stored) > 0 {
		d.Arguments = make([]argdecl.Declaration, len(stored))
		for i, a := range stored {
			d.Arguments[i] = argdecl.Declaration{Name: a.Name, Type: a.Type, Required: a.Required}
		}
	}
	d.UpdatedAt = d.UpdatedAt.UTC()
	return d, nil
}

func (s *PostgresStore) List(ctx context.Context) ([]ToolDefinition, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+selectCols+` FROM tool_definitions ORDER BY backend_id, tool_name`)
	if err != nil {
		return nil, fmt.Errorf("toolcatalog: list: %w", err)
	}
	defer rows.Close()
	out := []ToolDefinition{}
	for rows.Next() {
		d, err := scanDef(rows)
		if err != nil {
			return nil, fmt.Errorf("toolcatalog: scan: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolcatalog: rows: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) Get(ctx context.Context, backendID, toolName string) (*ToolDefinition, error) {
	d, err := scanDef(s.pool.QueryRow(ctx,
		`SELECT `+selectCols+` FROM tool_definitions WHERE backend_id = $1 AND tool_name = $2`, backendID, toolName))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("toolcatalog: get: %w", err)
	}
	return &d, nil
}

func (s *PostgresStore) Upsert(ctx context.Context, def ToolDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if def.UpdatedAt.IsZero() {
		def.UpdatedAt = time.Now().UTC()
	}
	stored := make([]storedArg, len(def.Arguments))
	for i, a := range def.Arguments {
		stored[i] = storedArg{Name: a.Name, Type: a.Type, Required: a.Required}
	}
	args, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("toolcatalog: encode arguments: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tool_definitions (backend_id, tool_name, risk, schema_json, arguments, description, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (backend_id, tool_name) DO UPDATE SET
			risk = EXCLUDED.risk,
			schema_json = EXCLUDED.schema_json,
			arguments = EXCLUDED.arguments,
			description = EXCLUDED.description,
			updated_at = EXCLUDED.updated_at,
			updated_by = EXCLUDED.updated_by`,
		def.BackendID, def.ToolName, string(def.Risk), []byte(def.SchemaJSON), args, def.Description, def.UpdatedAt, def.UpdatedBy)
	if err != nil {
		return fmt.Errorf("toolcatalog: upsert: %w", err)
	}
	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, backendID, toolName string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM tool_definitions WHERE backend_id = $1 AND tool_name = $2`, backendID, toolName); err != nil {
		return fmt.Errorf("toolcatalog: delete: %w", err)
	}
	return nil
}

func (s *PostgresStore) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}
