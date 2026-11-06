// path: internal/store/postgres.go
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/crnji/forge-ci/internal/artifacts"
	"github.com/crnji/forge-ci/internal/billing"
	"github.com/crnji/forge-ci/internal/logs"
	"github.com/crnji/forge-ci/internal/secrets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store, billing.BillingRepository, secrets.SecretStore,
// and logs.LogStore backed by a PostgreSQL database pool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a new Postgres backed storage instance.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// AutoMigrate applies schema initialization tables if they do not exist.
func (p *PostgresStore) AutoMigrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			repo_url TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			branch TEXT NOT NULL DEFAULT 'main',
			status TEXT NOT NULL DEFAULT 'pending',
			trigger TEXT NOT NULL DEFAULT 'manual',
			error_message TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			duration_ms BIGINT DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS steps (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
			step_id TEXT NOT NULL,
			name TEXT NOT NULL,
			image TEXT NOT NULL,
			command TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			exit_code INTEGER,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			duration_ms BIGINT DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_steps_run_id ON steps(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_steps_run_step ON steps(run_id, step_id);`,
		`CREATE TABLE IF NOT EXISTS logs (
			id BIGSERIAL PRIMARY KEY,
			run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
			step_id TEXT NOT NULL,
			stream TEXT NOT NULL DEFAULT 'stdout',
			line TEXT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_run_step_id ON logs(run_id, step_id, id);`,
		`CREATE TABLE IF NOT EXISTS secrets (
			name TEXT PRIMARY KEY,
			encrypted_value BYTEA NOT NULL,
			nonce BYTEA NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
			step_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			s3_key TEXT NOT NULL,
			size_bytes BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_run_id ON artifacts(run_id);`,
		`CREATE TABLE IF NOT EXISTS billing_credits (
			user_id TEXT PRIMARY KEY,
			balance_kes NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
			currency TEXT NOT NULL DEFAULT 'KES',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS mpesa_transactions (
			checkout_request_id TEXT PRIMARY KEY,
			merchant_request_id TEXT NOT NULL,
			phone_number TEXT NOT NULL,
			amount_kes NUMERIC(10, 2) NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			mpesa_receipt_number TEXT,
			result_code INTEGER,
			result_desc TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mpesa_phone ON mpesa_transactions(phone_number);`,
		`CREATE INDEX IF NOT EXISTS idx_mpesa_status ON mpesa_transactions(status);`,
	}

	for _, q := range queries {
		if _, err := p.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("automigrate exec query failed: %w (sql: %s)", err, q)
		}
	}
	return nil
}

// ==============================================================================
// Store: Runs
// ==============================================================================

func (p *PostgresStore) CreateRun(ctx context.Context, run RunRecord) error {
	query := `INSERT INTO runs (id, repo_url, commit_sha, branch, status, trigger, error_message, created_at, started_at, completed_at, duration_ms)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := p.pool.Exec(ctx, query,
		run.ID, run.RepoURL, run.CommitSHA, run.Branch, run.Status, run.Trigger,
		run.ErrorMessage, run.CreatedAt, run.StartedAt, run.CompletedAt, run.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("creating run: %w", err)
	}
	return nil
}

func (p *PostgresStore) GetRun(ctx context.Context, id string) (*RunRecord, error) {
	query := `SELECT id, repo_url, commit_sha, branch, status, trigger, COALESCE(error_message, ''), created_at, started_at, completed_at, duration_ms
			  FROM runs WHERE id = $1`
	var run RunRecord
	err := p.pool.QueryRow(ctx, query, id).Scan(
		&run.ID, &run.RepoURL, &run.CommitSHA, &run.Branch, &run.Status, &run.Trigger,
		&run.ErrorMessage, &run.CreatedAt, &run.StartedAt, &run.CompletedAt, &run.DurationMs,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("run %q not found", id)
		}
		return nil, fmt.Errorf("querying run %q: %w", id, err)
	}

	steps, err := p.GetStepsByRunID(ctx, id)
	if err == nil {
		run.Steps = steps
	}

	return &run, nil
}

func (p *PostgresStore) ListRuns(ctx context.Context, limit, offset int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, repo_url, commit_sha, branch, status, trigger, COALESCE(error_message, ''), created_at, started_at, completed_at, duration_ms
			  FROM runs ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := p.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	defer rows.Close()

	var runs []RunRecord
	for rows.Next() {
		var r RunRecord
		if err := rows.Scan(
			&r.ID, &r.RepoURL, &r.CommitSHA, &r.Branch, &r.Status, &r.Trigger,
			&r.ErrorMessage, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.DurationMs,
		); err != nil {
			return nil, fmt.Errorf("scanning run row: %w", err)
		}
		runs = append(runs, r)
	}

	return runs, nil
}

func (p *PostgresStore) UpdateRunStatus(ctx context.Context, id, status, errorMessage string, startedAt, completedAt *time.Time, durationMs int64) error {
	query := `UPDATE runs
			  SET status = $2,
			      error_message = CASE WHEN $3 <> '' THEN $3 ELSE error_message END,
			      started_at = COALESCE($4, started_at),
			      completed_at = COALESCE($5, completed_at),
			      duration_ms = CASE WHEN $6 > 0 THEN $6 ELSE duration_ms END
			  WHERE id = $1`
	res, err := p.pool.Exec(ctx, query, id, status, errorMessage, startedAt, completedAt, durationMs)
	if err != nil {
		return fmt.Errorf("updating run %q: %w", id, err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("run %q not found", id)
	}
	return nil
}

// ==============================================================================
// Store: Steps
// ==============================================================================

func (p *PostgresStore) CreateStep(ctx context.Context, step StepRecord) error {
	query := `INSERT INTO steps (id, run_id, step_id, name, image, command, status, exit_code, started_at, completed_at, duration_ms, created_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err := p.pool.Exec(ctx, query,
		step.ID, step.RunID, step.StepID, step.Name, step.Image, step.Command,
		step.Status, step.ExitCode, step.StartedAt, step.CompletedAt, step.DurationMs, step.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("creating step: %w", err)
	}
	return nil
}

func (p *PostgresStore) GetStepsByRunID(ctx context.Context, runID string) ([]StepRecord, error) {
	query := `SELECT id, run_id, step_id, name, image, command, status, exit_code, started_at, completed_at, duration_ms, created_at
			  FROM steps WHERE run_id = $1 ORDER BY created_at ASC`
	rows, err := p.pool.Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("querying steps for run %q: %w", runID, err)
	}
	defer rows.Close()

	var steps []StepRecord
	for rows.Next() {
		var s StepRecord
		if err := rows.Scan(
			&s.ID, &s.RunID, &s.StepID, &s.Name, &s.Image, &s.Command,
			&s.Status, &s.ExitCode, &s.StartedAt, &s.CompletedAt, &s.DurationMs, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning step row: %w", err)
		}
		steps = append(steps, s)
	}
	return steps, nil
}

func (p *PostgresStore) UpdateStepStatus(ctx context.Context, id, status string, exitCode *int, startedAt, completedAt *time.Time, durationMs int64) error {
	query := `UPDATE steps
			  SET status = $2,
			      exit_code = COALESCE($3, exit_code),
			      started_at = COALESCE($4, started_at),
			      completed_at = COALESCE($5, completed_at),
			      duration_ms = CASE WHEN $6 > 0 THEN $6 ELSE duration_ms END
			  WHERE id = $1 OR step_id = $1`
	res, err := p.pool.Exec(ctx, query, id, status, exitCode, startedAt, completedAt, durationMs)
	if err != nil {
		return fmt.Errorf("updating step %q: %w", id, err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("step %q not found", id)
	}
	return nil
}

// ==============================================================================
// Store: Artifacts
// ==============================================================================

func (p *PostgresStore) CreateArtifact(ctx context.Context, record artifacts.ArtifactRecord) error {
	query := `INSERT INTO artifacts (id, run_id, step_id, file_path, s3_key, size_bytes, created_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := p.pool.Exec(ctx, query,
		record.ID, record.RunID, record.StepID, record.FilePath, record.S3Key, record.SizeBytes, record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("creating artifact: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListArtifactsByRun(ctx context.Context, runID string) ([]artifacts.ArtifactRecord, error) {
	query := `SELECT id, run_id, step_id, file_path, s3_key, size_bytes, created_at
			  FROM artifacts WHERE run_id = $1 ORDER BY created_at ASC`
	rows, err := p.pool.Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("querying artifacts for run %q: %w", runID, err)
	}
	defer rows.Close()

	var out []artifacts.ArtifactRecord
	for rows.Next() {
		var a artifacts.ArtifactRecord
		if err := rows.Scan(&a.ID, &a.RunID, &a.StepID, &a.FilePath, &a.S3Key, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning artifact row: %w", err)
		}
		out = append(out, a)
	}
	return out, nil
}

func (p *PostgresStore) GetArtifact(ctx context.Context, id string) (*artifacts.ArtifactRecord, error) {
	query := `SELECT id, run_id, step_id, file_path, s3_key, size_bytes, created_at
			  FROM artifacts WHERE id = $1`
	var a artifacts.ArtifactRecord
	err := p.pool.QueryRow(ctx, query, id).Scan(&a.ID, &a.RunID, &a.StepID, &a.FilePath, &a.S3Key, &a.SizeBytes, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("artifact %q not found", id)
		}
		return nil, fmt.Errorf("querying artifact %q: %w", id, err)
	}
	return &a, nil
}

// ==============================================================================
// billing.BillingRepository Implementation
// ==============================================================================

func (p *PostgresStore) GetBalance(ctx context.Context, userID string) (float64, error) {
	query := `SELECT balance_kes FROM billing_credits WHERE user_id = $1`
	var balance float64
	err := p.pool.QueryRow(ctx, query, userID).Scan(&balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0.00, nil
		}
		return 0.00, fmt.Errorf("fetching balance: %w", err)
	}
	return balance, nil
}

func (p *PostgresStore) AddCredits(ctx context.Context, userID string, amountKES float64) (float64, error) {
	query := `INSERT INTO billing_credits (user_id, balance_kes, currency, updated_at)
			  VALUES ($1, $2, 'KES', NOW())
			  ON CONFLICT (user_id) DO UPDATE
			  SET balance_kes = billing_credits.balance_kes + EXCLUDED.balance_kes,
			      updated_at = NOW()
			  RETURNING balance_kes`
	var newBal float64
	err := p.pool.QueryRow(ctx, query, userID, amountKES).Scan(&newBal)
	if err != nil {
		return 0.00, fmt.Errorf("adding credits: %w", err)
	}
	return newBal, nil
}

func (p *PostgresStore) DeductCredits(ctx context.Context, userID string, amountKES float64) (float64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0.00, fmt.Errorf("beginning credit deduction tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current float64
	query := `SELECT balance_kes FROM billing_credits WHERE user_id = $1 FOR UPDATE`
	err = tx.QueryRow(ctx, query, userID).Scan(&current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0.00, fmt.Errorf("insufficient credits: balance 0.00 KES is below charge of %.2f KES", amountKES)
		}
		return 0.00, fmt.Errorf("querying locked balance: %w", err)
	}

	if current < amountKES {
		return current, fmt.Errorf("insufficient credits: balance %.2f KES is below charge of %.2f KES", current, amountKES)
	}

	updateQuery := `UPDATE billing_credits SET balance_kes = balance_kes - $2, updated_at = NOW() WHERE user_id = $1 RETURNING balance_kes`
	var newBal float64
	if err := tx.QueryRow(ctx, updateQuery, userID, amountKES).Scan(&newBal); err != nil {
		return 0.00, fmt.Errorf("updating deducted balance: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0.00, fmt.Errorf("committing credit deduction tx: %w", err)
	}

	return newBal, nil
}

func (p *PostgresStore) CreateTransaction(ctx context.Context, tx billing.TransactionRecord) error {
	query := `INSERT INTO mpesa_transactions (checkout_request_id, merchant_request_id, phone_number, amount_kes, status, result_code, result_desc, created_at, updated_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := p.pool.Exec(ctx, query,
		tx.CheckoutRequestID, tx.MerchantRequestID, tx.PhoneNumber, tx.AmountKES,
		tx.Status, tx.ResultCode, tx.ResultDesc, tx.CreatedAt, tx.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("creating mpesa transaction record: %w", err)
	}
	return nil
}

func (p *PostgresStore) UpdateTransaction(ctx context.Context, checkoutID, status, receipt string, resultCode int, resultDesc string) error {
	query := `UPDATE mpesa_transactions
			  SET status = $2,
			      mpesa_receipt_number = CASE WHEN $3 <> '' THEN $3 ELSE mpesa_receipt_number END,
			      result_code = $4,
			      result_desc = $5,
			      updated_at = NOW()
			  WHERE checkout_request_id = $1`
	res, err := p.pool.Exec(ctx, query, checkoutID, status, receipt, resultCode, resultDesc)
	if err != nil {
		return fmt.Errorf("updating mpesa transaction %q: %w", checkoutID, err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("mpesa transaction %q not found", checkoutID)
	}
	return nil
}

func (p *PostgresStore) GetTransaction(ctx context.Context, checkoutID string) (*billing.TransactionRecord, error) {
	query := `SELECT checkout_request_id, merchant_request_id, phone_number, amount_kes, status, COALESCE(mpesa_receipt_number, ''), result_code, COALESCE(result_desc, ''), created_at, updated_at
			  FROM mpesa_transactions WHERE checkout_request_id = $1`
	var tx billing.TransactionRecord
	err := p.pool.QueryRow(ctx, query, checkoutID).Scan(
		&tx.CheckoutRequestID, &tx.MerchantRequestID, &tx.PhoneNumber, &tx.AmountKES,
		&tx.Status, &tx.MpesaReceipt, &tx.ResultCode, &tx.ResultDesc, &tx.CreatedAt, &tx.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("mpesa transaction %q not found", checkoutID)
		}
		return nil, fmt.Errorf("querying mpesa transaction %q: %w", checkoutID, err)
	}
	return &tx, nil
}

// ==============================================================================
// secrets.SecretStore Implementation
// ==============================================================================

func (p *PostgresStore) SaveSecret(ctx context.Context, record secrets.SecretRecord) error {
	query := `INSERT INTO secrets (name, encrypted_value, nonce, created_at, updated_at)
			  VALUES ($1, $2, $3, $4, $5)
			  ON CONFLICT (name) DO UPDATE
			  SET encrypted_value = EXCLUDED.encrypted_value,
			      nonce = EXCLUDED.nonce,
			      updated_at = EXCLUDED.updated_at`
	_, err := p.pool.Exec(ctx, query, record.Name, record.EncryptedValue, record.Nonce, record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return fmt.Errorf("saving secret %q: %w", record.Name, err)
	}
	return nil
}

func (p *PostgresStore) GetSecret(ctx context.Context, name string) (*secrets.SecretRecord, error) {
	query := `SELECT name, encrypted_value, nonce, created_at, updated_at FROM secrets WHERE name = $1`
	var rec secrets.SecretRecord
	err := p.pool.QueryRow(ctx, query, name).Scan(&rec.Name, &rec.EncryptedValue, &rec.Nonce, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("secret %q not found", name)
		}
		return nil, fmt.Errorf("querying secret %q: %w", name, err)
	}
	return &rec, nil
}

func (p *PostgresStore) DeleteSecret(ctx context.Context, name string) error {
	query := `DELETE FROM secrets WHERE name = $1`
	res, err := p.pool.Exec(ctx, query, name)
	if err != nil {
		return fmt.Errorf("deleting secret %q: %w", name, err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("secret %q not found", name)
	}
	return nil
}

func (p *PostgresStore) ListSecretNames(ctx context.Context) ([]string, error) {
	query := `SELECT name FROM secrets ORDER BY name ASC`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing secret names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning secret name: %w", err)
		}
		names = append(names, name)
	}
	return names, nil
}

// ==============================================================================
// logs.LogStore Implementation
// ==============================================================================

func (p *PostgresStore) InsertLogBatch(ctx context.Context, entries []logs.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `INSERT INTO logs (run_id, step_id, stream, line, timestamp) VALUES ($1, $2, $3, $4, $5)`
	for _, entry := range entries {
		batch.Queue(query, entry.RunID, entry.StepID, entry.Stream, entry.Line, entry.Timestamp)
	}

	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(entries); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("executing batch log insert at index %d: %w", i, err)
		}
	}
	return nil
}

