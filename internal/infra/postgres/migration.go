package postgres

import (
	"context"
	"errors"
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

// migrationLockKey 是迁移期间持有的 advisory lock 的键。API、worker 和 forge-migrate 每次部署都可能同时启动，
// 加锁后只有一个进程执行迁移，其余进程等它完成后看到迁移已记录而跳过。
const migrationLockKey int64 = 0x666f72676d696772

func runMigrations(ctx context.Context, pool *pgxpool.Pool, migrationsFS fs.FS) (err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, migrationLockKey); err != nil {
		conn.Release()
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		// 解锁失败时不能把仍持有锁的连接放回连接池，直接关闭它，锁随会话结束释放。
		if _, unlockErr := conn.Exec(context.Background(), `select pg_advisory_unlock($1)`, migrationLockKey); unlockErr != nil {
			_ = conn.Hijack().Close(context.Background())
			err = errors.Join(err, fmt.Errorf("unlock migrations: %w", unlockErr))
			return
		}
		conn.Release()
	}()

	if _, err := conn.Exec(ctx, schemaMigrationsTableSQL); err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}

	files, err := migrationFiles(migrationsFS)
	if err != nil {
		return err
	}

	for _, file := range files {
		if err := runMigration(ctx, conn, migrationsFS, file); err != nil {
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

func runMigration(ctx context.Context, conn *pgxpool.Conn, migrationsFS fs.FS, file string) error {
	sql, err := fs.ReadFile(migrationsFS, file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", file, err)
	}

	tx, err := conn.Begin(ctx)
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

	if _, err := tx.Exec(ctx, `
		insert into schema_migrations (version) values ($1)
		on conflict (version) do update set applied_at = now()
	`, file); err != nil {
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
