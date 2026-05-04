package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, err := extractVersion(entry.Name())
		if err != nil {
			return fmt.Errorf("extract version from %s: %w", entry.Name(), err)
		}

		var existing int
		row := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version WHERE version = ?`, version)
		if err := row.Scan(&existing); err != nil {
			return fmt.Errorf("check schema_version for %d: %w", version, err)
		}

		if existing > 0 {
			continue
		}

		if err := applyMigrationFile(ctx, db, migrationFS, entry.Name(), version); err != nil {
			return err
		}
	}

	return nil
}

func extractVersion(filename string) (int, error) {
	// ファイル名の先頭4桁が version 番号 (e.g. "0001_init.sql" → 1)
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("unexpected filename format: %s", filename)
	}

	return strconv.Atoi(parts[0])
}

func applyMigrationFile(ctx context.Context, db *sql.DB, fsys fs.FS, name string, version int) error {
	data, err := fs.ReadFile(fsys, "migrations/"+name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for migration %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("exec migration %s: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_version(version, applied_at) VALUES (?, ?)`,
		version, time.Now().UnixNano(),
	); err != nil {
		return fmt.Errorf("record schema_version for %d: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}
