package audit

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/002_create_audit_events.sql
var migration002SQL string

//go:embed migrations/003_add_downstream_credential_ref.sql
var migration003SQL string

// PostgresStore implements Store backed by PostgreSQL via pgx/v5.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore initializes a connection pool to PostgreSQL.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	poolCfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("audit: parse postgres config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("audit: connect postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("audit: ping postgres: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

// NewPostgresStoreWithPool wraps an existing pgxpool.Pool.
func NewPostgresStoreWithPool(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Migrate applies the schema migration for audit_events.
func (s *PostgresStore) Migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("audit: begin migration tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint:errcheck

	if _, err := tx.Exec(ctx, migration002SQL); err != nil {
		return fmt.Errorf("audit: apply 002_create_audit_events: %w", err)
	}

	if _, err := tx.Exec(ctx, migration003SQL); err != nil {
		return fmt.Errorf("audit: apply 003_add_downstream_credential_ref: %w", err)
	}

	_, err = tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(128) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		INSERT INTO schema_migrations (version)
		VALUES ('002_create_audit_events')
		ON CONFLICT (version) DO NOTHING;
		INSERT INTO schema_migrations (version)
		VALUES ('003_add_downstream_credential_ref')
		ON CONFLICT (version) DO NOTHING;
	`)
	if err != nil {
		return fmt.Errorf("audit: record migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("audit: commit migration tx: %w", err)
	}

	return nil
}

// AppendDecision persists an authorization decision into audit_events within an advisory-locked transaction.
func (s *PostgresStore) AppendDecision(ctx context.Context, record DecisionRecord) (*StoredRecord, error) {
	if record.WorkspaceID == "" {
		return nil, fmt.Errorf("audit: workspace_id is required")
	}

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC().Truncate(time.Microsecond)
	} else {
		record.Timestamp = record.Timestamp.Truncate(time.Microsecond)
	}

	if record.EventType == "" {
		record.EventType = EventTypeDecision
	}

	rolesJSON, err := json.Marshal(record.PrincipalRoles)
	if err != nil {
		return nil, fmt.Errorf("audit: marshal principal_roles: %w", err)
	}

	argsJSON, err := json.Marshal(record.RedactedArguments)
	if err != nil {
		return nil, fmt.Errorf("audit: marshal redacted_arguments: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit: begin append tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint:errcheck

	// Advisory lock keyed by workspace_id hash ensures sequential sequence numbering per workspace without table locks
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", record.WorkspaceID); err != nil {
		return nil, fmt.Errorf("audit: acquire workspace lock: %w", err)
	}

	insertSQL := `
		INSERT INTO audit_events (
			workspace_id,
			sequence_number,
			execution_id,
			timestamp,
			event_type,
			decision,
			reason,
			principal_agent_id,
			principal_roles,
			principal_on_behalf_of,
			tool_backend_id,
			tool_name,
			tool_risk,
			policy_version,
			policy_hash,
			redacted_arguments,
			downstream_credential_ref,
			canonical_payload,
			prev_hash,
			row_hash
		) VALUES (
			$1::text,
			COALESCE((SELECT MAX(sequence_number) FROM audit_events WHERE workspace_id = $1::text), 0) + 1,
			$2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		)
		RETURNING id, sequence_number, timestamp
	`

	var stored StoredRecord
	err = tx.QueryRow(ctx, insertSQL,
		record.WorkspaceID,
		record.ExecutionID,
		record.Timestamp,
		record.EventType,
		record.Decision,
		record.Reason,
		record.PrincipalAgentID,
		rolesJSON,
		record.PrincipalOnBehalfOf,
		record.ToolBackendID,
		record.ToolName,
		record.ToolRisk,
		record.PolicyVersion,
		record.PolicyHash,
		argsJSON,
		record.DownstreamCredentialRef,
		record.CanonicalPayload,
		record.PrevHash,
		record.RowHash,
	).Scan(&stored.ID, &stored.SequenceNumber, &stored.Timestamp)

	if err != nil {
		return nil, fmt.Errorf("audit: insert audit_event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("audit: commit append tx: %w", err)
	}

	stored.WorkspaceID = record.WorkspaceID
	stored.ExecutionID = record.ExecutionID
	stored.EventType = record.EventType
	stored.Decision = record.Decision
	stored.Reason = record.Reason
	stored.PrincipalAgentID = record.PrincipalAgentID
	stored.PrincipalRoles = record.PrincipalRoles
	stored.PrincipalOnBehalfOf = record.PrincipalOnBehalfOf
	stored.ToolBackendID = record.ToolBackendID
	stored.ToolName = record.ToolName
	stored.ToolRisk = record.ToolRisk
	stored.PolicyVersion = record.PolicyVersion
	stored.PolicyHash = record.PolicyHash
	stored.RedactedArguments = record.RedactedArguments
	stored.DownstreamCredentialRef = record.DownstreamCredentialRef
	stored.CanonicalPayload = record.CanonicalPayload
	stored.PrevHash = record.PrevHash
	stored.RowHash = record.RowHash

	return &stored, nil
}

// AppendMutation persists a policy mutation event.
func (s *PostgresStore) AppendMutation(ctx context.Context, event auditevents.MutationEvent, prevHash string) (*StoredRecord, error) {
	rec := DecisionRecord{
		WorkspaceID:       event.WorkspaceID,
		ExecutionID:       event.CorrelationID,
		Timestamp:         event.Timestamp,
		EventType:         EventTypeMutation,
		Decision:          "MUTATION",
		Reason:            string(event.Action),
		PrincipalAgentID:  event.OperatorID,
		PolicyVersion:     event.NewVersion,
		PolicyHash:        event.PreviousVersion,
		PrevHash:          prevHash,
		RedactedArguments: make(map[string]string),
	}

	return s.AppendDecision(ctx, rec)
}

const selectFields = `
	id, workspace_id, sequence_number, execution_id, timestamp, event_type,
	decision, reason, principal_agent_id, principal_roles, principal_on_behalf_of,
	tool_backend_id, tool_name, tool_risk, policy_version, policy_hash,
	redacted_arguments, downstream_credential_ref, canonical_payload, prev_hash, row_hash
`

func scanRow(row pgx.Row) (*StoredRecord, error) {
	var r StoredRecord
	var rolesJSON, argsJSON []byte

	err := row.Scan(
		&r.ID,
		&r.WorkspaceID,
		&r.SequenceNumber,
		&r.ExecutionID,
		&r.Timestamp,
		&r.EventType,
		&r.Decision,
		&r.Reason,
		&r.PrincipalAgentID,
		&rolesJSON,
		&r.PrincipalOnBehalfOf,
		&r.ToolBackendID,
		&r.ToolName,
		&r.ToolRisk,
		&r.PolicyVersion,
		&r.PolicyHash,
		&argsJSON,
		&r.DownstreamCredentialRef,
		&r.CanonicalPayload,
		&r.PrevHash,
		&r.RowHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("audit: scan row: %w", err)
	}

	if len(rolesJSON) > 0 {
		_ = json.Unmarshal(rolesJSON, &r.PrincipalRoles)
	}
	if len(argsJSON) > 0 {
		_ = json.Unmarshal(argsJSON, &r.RedactedArguments)
	}

	return &r, nil
}

// GetLatestRecord returns the most recent record for the specified workspace.
func (s *PostgresStore) GetLatestRecord(ctx context.Context, workspaceID string) (*StoredRecord, error) {
	query := fmt.Sprintf(`SELECT %s FROM audit_events WHERE workspace_id = $1 ORDER BY sequence_number DESC LIMIT 1`, selectFields)
	row := s.pool.QueryRow(ctx, query, workspaceID)
	return scanRow(row)
}

// ListRecords returns up to limit records for a workspace in descending sequence order.
// When limit <= 0, all records for the workspace are returned.
func (s *PostgresStore) ListRecords(ctx context.Context, workspaceID string, limit int) ([]StoredRecord, error) {
	var rows pgx.Rows
	var err error
	if limit <= 0 {
		query := fmt.Sprintf(`SELECT %s FROM audit_events WHERE workspace_id = $1 ORDER BY sequence_number DESC`, selectFields)
		rows, err = s.pool.Query(ctx, query, workspaceID)
	} else {
		query := fmt.Sprintf(`SELECT %s FROM audit_events WHERE workspace_id = $1 ORDER BY sequence_number DESC LIMIT $2`, selectFields)
		rows, err = s.pool.Query(ctx, query, workspaceID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("audit: query list records: %w", err)
	}
	defer rows.Close()
	return scanRecordRows(rows)
}

// ListRecordsBefore returns up to limit records for a workspace with sequence numbers strictly
// less than beforeSequence, in descending sequence order — the cursor-paginated counterpart to
// ListRecords, added for G7's audit-query read API.
func (s *PostgresStore) ListRecordsBefore(ctx context.Context, workspaceID string, beforeSequence int64, limit int) ([]StoredRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf(`SELECT %s FROM audit_events WHERE workspace_id = $1 AND sequence_number < $2 ORDER BY sequence_number DESC LIMIT $3`, selectFields)
	rows, err := s.pool.Query(ctx, query, workspaceID, beforeSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: query list records before: %w", err)
	}
	defer rows.Close()
	return scanRecordRows(rows)
}

// scanRecordRows scans every row of an already-executed audit_events query into StoredRecord
// values. Shared by ListRecords and ListRecordsBefore, whose row shape is identical — only the
// WHERE/LIMIT clause differs between them.
func scanRecordRows(rows pgx.Rows) ([]StoredRecord, error) {
	var records []StoredRecord
	for rows.Next() {
		var r StoredRecord
		var rolesJSON, argsJSON []byte
		err := rows.Scan(
			&r.ID,
			&r.WorkspaceID,
			&r.SequenceNumber,
			&r.ExecutionID,
			&r.Timestamp,
			&r.EventType,
			&r.Decision,
			&r.Reason,
			&r.PrincipalAgentID,
			&rolesJSON,
			&r.PrincipalOnBehalfOf,
			&r.ToolBackendID,
			&r.ToolName,
			&r.ToolRisk,
			&r.PolicyVersion,
			&r.PolicyHash,
			&argsJSON,
			&r.DownstreamCredentialRef,
			&r.CanonicalPayload,
			&r.PrevHash,
			&r.RowHash,
		)
		if err != nil {
			return nil, fmt.Errorf("audit: scan list row: %w", err)
		}
		if len(rolesJSON) > 0 {
			_ = json.Unmarshal(rolesJSON, &r.PrincipalRoles)
		}
		if len(argsJSON) > 0 {
			_ = json.Unmarshal(argsJSON, &r.RedactedArguments)
		}
		records = append(records, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: list rows error: %w", err)
	}

	if records == nil {
		records = []StoredRecord{}
	}

	return records, nil
}

// GetRecordBySequence retrieves a specific record by workspace and sequence number.
func (s *PostgresStore) GetRecordBySequence(ctx context.Context, workspaceID string, seq int64) (*StoredRecord, error) {
	query := fmt.Sprintf(`SELECT %s FROM audit_events WHERE workspace_id = $1 AND sequence_number = $2`, selectFields)
	row := s.pool.QueryRow(ctx, query, workspaceID, seq)
	return scanRow(row)
}

// Ping checks database connectivity.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close closes the connection pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}
