// Package main implements the FlowForge database migration tool.
//
// It manages schema migrations for the PostgreSQL database, tracking applied
// versions in a schema_migrations table and supporting up, down, status, and
// create operations.
//
// Usage:
//
//	flowforge-migrate up             # apply all pending migrations
//	flowforge-migrate down [N]       # rollback the last N migrations (default 1)
//	flowforge-migrate status         # show migration status
//	flowforge-migrate create <name>  # scaffold a new migration
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ---------------------------------------------------------------------------
// Build-time variables
// ---------------------------------------------------------------------------

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// ---------------------------------------------------------------------------
// Migration Types
// ---------------------------------------------------------------------------

// Migration represents a single database schema migration.
type Migration struct {
	Version   int
	Name      string
	UpSQL     string
	DownSQL   string
	AppliedAt *time.Time // nil if not yet applied
}

// ---------------------------------------------------------------------------
// Hardcoded Migrations
// ---------------------------------------------------------------------------

// migrations is the ordered list of all known migrations. In production this
// could be loaded from embedded SQL files; here they are defined inline so
// the binary is self-contained.
var migrations = []*Migration{
	{
		Version: 1,
		Name:    "create_initial_schema",
		UpSQL: `
-- Enable UUID generation.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- -----------------------------------------------------------------------
-- workflows
-- -----------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS workflows (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    definition  JSONB NOT NULL DEFAULT '{}',
    version     INTEGER NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_workflows_name ON workflows (name);
CREATE INDEX idx_workflows_status ON workflows (status);
CREATE INDEX idx_workflows_created_at ON workflows (created_at DESC);

-- -----------------------------------------------------------------------
-- workflow_versions
-- -----------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS workflow_versions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    definition  JSONB NOT NULL DEFAULT '{}',
    hash        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (workflow_id, version)
);

CREATE INDEX idx_workflow_versions_workflow_id ON workflow_versions (workflow_id);
CREATE INDEX idx_workflow_versions_hash ON workflow_versions (hash);

-- -----------------------------------------------------------------------
-- runs
-- -----------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS runs (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id  UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending',
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    input        JSONB NOT NULL DEFAULT '{}',
    output       JSONB,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_runs_workflow_id ON runs (workflow_id);
CREATE INDEX idx_runs_status ON runs (status);
CREATE INDEX idx_runs_created_at ON runs (created_at DESC);
CREATE INDEX idx_runs_workflow_status ON runs (workflow_id, status);

-- -----------------------------------------------------------------------
-- task_runs
-- -----------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_runs (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    run_id       UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    task_id      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    attempt      INTEGER NOT NULL DEFAULT 1,
    worker_id    TEXT,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    output       JSONB,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_task_runs_run_id ON task_runs (run_id);
CREATE INDEX idx_task_runs_status ON task_runs (status);
CREATE INDEX idx_task_runs_task_id ON task_runs (task_id);
CREATE INDEX idx_task_runs_run_task ON task_runs (run_id, task_id);

-- -----------------------------------------------------------------------
-- api_keys
-- -----------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS api_keys (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name       TEXT NOT NULL,
    key_hash   TEXT NOT NULL UNIQUE,
    role       TEXT NOT NULL DEFAULT 'read',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash);
CREATE INDEX idx_api_keys_role ON api_keys (role);
CREATE INDEX idx_api_keys_expires_at ON api_keys (expires_at) WHERE expires_at IS NOT NULL;
`,
		DownSQL: `
DROP TABLE IF EXISTS api_keys CASCADE;
DROP TABLE IF EXISTS task_runs CASCADE;
DROP TABLE IF EXISTS runs CASCADE;
DROP TABLE IF EXISTS workflow_versions CASCADE;
DROP TABLE IF EXISTS workflows CASCADE;
`,
	},
	{
		Version: 2,
		Name:    "add_workflow_metadata",
		UpSQL: `
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]';
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT '';
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS schedule TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_workflows_owner ON workflows (owner) WHERE owner != '';

-- Add a trigger to automatically update updated_at on workflows.
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_workflows_updated_at
    BEFORE UPDATE ON workflows
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
`,
		DownSQL: `
DROP TRIGGER IF EXISTS trigger_workflows_updated_at ON workflows;
DROP FUNCTION IF EXISTS update_updated_at_column();

ALTER TABLE workflows DROP COLUMN IF EXISTS schedule;
ALTER TABLE workflows DROP COLUMN IF EXISTS owner;
ALTER TABLE workflows DROP COLUMN IF EXISTS tags;
`,
	},
	{
		Version: 3,
		Name:    "add_run_metrics",
		UpSQL: `
ALTER TABLE runs ADD COLUMN IF NOT EXISTS duration_ms BIGINT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS task_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE task_runs ADD COLUMN IF NOT EXISTS duration_ms BIGINT;
ALTER TABLE task_runs ADD COLUMN IF NOT EXISTS input JSONB;

CREATE INDEX idx_runs_duration ON runs (duration_ms) WHERE duration_ms IS NOT NULL;
`,
		DownSQL: `
DROP INDEX IF EXISTS idx_runs_duration;

ALTER TABLE task_runs DROP COLUMN IF EXISTS input;
ALTER TABLE task_runs DROP COLUMN IF EXISTS duration_ms;

ALTER TABLE runs DROP COLUMN IF EXISTS retry_count;
ALTER TABLE runs DROP COLUMN IF EXISTS task_count;
ALTER TABLE runs DROP COLUMN IF EXISTS duration_ms;
`,
	},
}

// ---------------------------------------------------------------------------
// Migrator
// ---------------------------------------------------------------------------

// Migrator manages database schema migrations.
type Migrator struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewMigrator creates a new Migrator.
func NewMigrator(db *sql.DB, logger *zap.Logger) *Migrator {
	return &Migrator{db: db, logger: logger}
}

// ensureMigrationsTable creates the schema_migrations tracking table if it
// does not already exist.
func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			name        TEXT NOT NULL,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	_, err := m.db.ExecContext(ctx, query)
	return err
}

// appliedVersions returns the set of already-applied migration versions.
func (m *Migrator) appliedVersions(ctx context.Context) (map[int]time.Time, error) {
	rows, err := m.db.QueryContext(ctx,
		"SELECT version, applied_at FROM schema_migrations ORDER BY version",
	)
	if err != nil {
		return nil, fmt.Errorf("query applied versions: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]time.Time)
	for rows.Next() {
		var ver int
		var at time.Time
		if err := rows.Scan(&ver, &at); err != nil {
			return nil, fmt.Errorf("scan applied version: %w", err)
		}
		applied[ver] = at
	}
	return applied, rows.Err()
}

// Up applies all pending migrations in order.
func (m *Migrator) Up(ctx context.Context) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}

	var count int
	for _, mig := range migrations {
		if _, ok := applied[mig.Version]; ok {
			continue
		}

		m.logger.Info("applying migration",
			zap.Int("version", mig.Version),
			zap.String("name", mig.Name),
		)

		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", mig.Version, err)
		}

		if _, err := tx.ExecContext(ctx, mig.UpSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s) failed: %w", mig.Version, mig.Name, err)
		}

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
			mig.Version, mig.Name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", mig.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", mig.Version, err)
		}

		m.logger.Info("migration applied",
			zap.Int("version", mig.Version),
			zap.String("name", mig.Name),
		)
		count++
	}

	if count == 0 {
		m.logger.Info("database is up to date, no migrations to apply")
	} else {
		m.logger.Info("migrations applied", zap.Int("count", count))
	}
	return nil
}

// Down rolls back the last n applied migrations (default 1).
func (m *Migrator) Down(ctx context.Context, n int) error {
	if n <= 0 {
		n = 1
	}

	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}

	// Build a reverse-ordered list of applied migrations.
	var toRollback []*Migration
	for i := len(migrations) - 1; i >= 0; i-- {
		if _, ok := applied[migrations[i].Version]; ok {
			toRollback = append(toRollback, migrations[i])
		}
		if len(toRollback) >= n {
			break
		}
	}

	if len(toRollback) == 0 {
		m.logger.Info("no migrations to roll back")
		return nil
	}

	for _, mig := range toRollback {
		m.logger.Info("rolling back migration",
			zap.Int("version", mig.Version),
			zap.String("name", mig.Name),
		)

		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for rollback %d: %w", mig.Version, err)
		}

		if _, err := tx.ExecContext(ctx, mig.DownSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rollback %d (%s) failed: %w", mig.Version, mig.Name, err)
		}

		if _, err := tx.ExecContext(ctx,
			"DELETE FROM schema_migrations WHERE version = $1",
			mig.Version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("remove migration record %d: %w", mig.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit rollback %d: %w", mig.Version, err)
		}

		m.logger.Info("migration rolled back",
			zap.Int("version", mig.Version),
			zap.String("name", mig.Name),
		)
	}

	return nil
}

// Status prints the state of each known migration.
func (m *Migrator) Status(ctx context.Context) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("%-10s %-35s %-10s %s\n", "VERSION", "NAME", "STATUS", "APPLIED AT")
	fmt.Printf("%-10s %-35s %-10s %s\n",
		strings.Repeat("-", 10),
		strings.Repeat("-", 35),
		strings.Repeat("-", 10),
		strings.Repeat("-", 25),
	)

	for _, mig := range migrations {
		status := "pending"
		appliedAt := ""
		if at, ok := applied[mig.Version]; ok {
			status = "applied"
			appliedAt = at.Format(time.RFC3339)
		}
		fmt.Printf("%-10d %-35s %-10s %s\n", mig.Version, mig.Name, status, appliedAt)
	}

	return nil
}

// Create scaffolds a new migration with the given name. It prints the
// version number and name to stdout.
func Create(name string) error {
	if name == "" {
		return fmt.Errorf("migration name is required")
	}

	// Determine next version.
	nextVersion := 1
	if len(migrations) > 0 {
		nextVersion = migrations[len(migrations)-1].Version + 1
	}

	// Sanitize the name for use in a filename.
	safeName := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")

	// Write template SQL files.
	migrationsDir := "migrations"
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return fmt.Errorf("create migrations directory: %w", err)
	}

	prefix := fmt.Sprintf("%04d_%s", nextVersion, safeName)
	upFile := filepath.Join(migrationsDir, prefix+".up.sql")
	downFile := filepath.Join(migrationsDir, prefix+".down.sql")

	upContent := fmt.Sprintf("-- Migration: %s (version %d)\n-- Created at: %s\n\n-- Write your UP migration SQL here.\n",
		safeName, nextVersion, time.Now().UTC().Format(time.RFC3339))
	downContent := fmt.Sprintf("-- Migration: %s (version %d)\n-- Created at: %s\n\n-- Write your DOWN migration SQL here.\n",
		safeName, nextVersion, time.Now().UTC().Format(time.RFC3339))

	if err := os.WriteFile(upFile, []byte(upContent), 0o644); err != nil {
		return fmt.Errorf("write up migration: %w", err)
	}
	if err := os.WriteFile(downFile, []byte(downContent), 0o644); err != nil {
		return fmt.Errorf("write down migration: %w", err)
	}

	fmt.Printf("Created migration %d: %s\n", nextVersion, safeName)
	fmt.Printf("  Up:   %s\n", upFile)
	fmt.Printf("  Down: %s\n", downFile)
	fmt.Println()
	fmt.Println("Remember to add the migration to the migrations slice in cmd/migrate/main.go")
	fmt.Println("after writing the SQL, so it will be applied by the migrate tool.")
	return nil
}

// ---------------------------------------------------------------------------
// Logger
// ---------------------------------------------------------------------------

func newLogger(level string) (*zap.Logger, error) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zapcore.InfoLevel
	}

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(lvl),
		Development: false,
		Encoding:    "console",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      zapcore.OmitKey,
			MessageKey:     "msg",
			StacktraceKey:  zapcore.OmitKey,
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.CapitalColorLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return cfg.Build()
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func usage() {
	fmt.Fprintf(os.Stderr, `FlowForge Database Migration Tool (version %s)

Usage:
  flowforge-migrate <command> [args]

Commands:
  up              Apply all pending migrations
  down [N]        Roll back the last N migrations (default: 1)
  status          Show migration status
  create <name>   Create a new migration scaffold

Environment Variables:
  DATABASE_URL    PostgreSQL connection string (required for up/down/status)
  LOG_LEVEL       Log level: debug, info, warn, error (default: info)

`, version)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	command := os.Args[1]

	// "create" doesn't need a database connection.
	if command == "create" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: migration name is required")
			fmt.Fprintln(os.Stderr, "Usage: flowforge-migrate create <name>")
			os.Exit(1)
		}
		name := strings.Join(os.Args[2:], "_")
		if err := Create(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// All other commands require a database connection.
	logLevel := envOrDefault("LOG_LEVEL", "info")
	logger, err := newLogger(logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	databaseURL := envOrDefault("DATABASE_URL", "")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "Error: DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	logger.Info("connecting to database")
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		logger.Fatal("failed to open database", zap.Error(err))
	}
	defer db.Close()

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		logger.Fatal("failed to ping database", zap.Error(err))
	}
	logger.Info("database connection established")

	migrator := NewMigrator(db, logger)

	switch command {
	case "up":
		if err := migrator.Up(ctx); err != nil {
			logger.Fatal("migration up failed", zap.Error(err))
		}

	case "down":
		n := 1
		if len(os.Args) >= 3 {
			n, err = strconv.Atoi(os.Args[2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid number %q\n", os.Args[2])
				os.Exit(1)
			}
		}
		if err := migrator.Down(ctx, n); err != nil {
			logger.Fatal("migration down failed", zap.Error(err))
		}

	case "status":
		if err := migrator.Status(ctx); err != nil {
			logger.Fatal("migration status failed", zap.Error(err))
		}

	case "help", "-h", "--help":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n", command)
		usage()
		os.Exit(1)
	}
}
