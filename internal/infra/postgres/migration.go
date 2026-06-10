package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaMigrationsTableSQL = `
create table if not exists schema_migrations (
    version text primary key,
    applied_at timestamptz not null default now()
);
`

func RunMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	if dir == "" {
		dir = "migrations"
	}

	migrationsFS := os.DirFS(dir)
	if _, err := fs.Stat(migrationsFS, "."); err != nil {
		return fmt.Errorf("stat migrations dir %s: %w", dir, err)
	}

	return runMigrations(ctx, pool, migrationsFS)
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, migrationsFS fs.FS) error {
	if _, err := pool.Exec(ctx, schemaMigrationsTableSQL); err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}

	files, err := migrationFiles(migrationsFS)
	if err != nil {
		return err
	}

	for _, file := range files {
		if err := runMigration(ctx, pool, migrationsFS, file); err != nil {
			return err
		}
	}

	return nil
}

func migrationFiles(migrationsFS fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	return files, nil
}

func runMigration(ctx context.Context, pool *pgxpool.Pool, migrationsFS fs.FS, file string) error {
	sql, err := fs.ReadFile(migrationsFS, file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", file, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", file, err)
	}
	defer rollbackMigration(ctx, tx)

	var exists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from schema_migrations where version = $1)`, file).Scan(&exists); err != nil {
		return fmt.Errorf("check migration %s: %w", file, err)
	}
	if exists {
		return nil
	}

	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("execute migration %s: %w", file, err)
	}

	if _, err := tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, file); err != nil {
		return fmt.Errorf("record migration %s: %w", file, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", file, err)
	}

	return nil
}

func rollbackMigration(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
