package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newScratchDatabasePool 在测试实例上建一个空库并返回它的连接池，测试结束后删除。
func newScratchDatabasePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin := newIntegrationPool(t)
	name := fmt.Sprintf("forge_migration_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `create database `+name); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `drop database if exists `+name+` with (force)`); err != nil {
			t.Errorf("drop scratch database: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(os.Getenv("FORGE_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// API、worker 同时启动时都会执行迁移：并发执行必须全部成功，每个迁移只记录一次。
func TestIntegrationRunMigrationsConcurrently(t *testing.T) {
	pool := newScratchDatabasePool(t)
	ctx := context.Background()

	const runners = 8
	errs := make(chan error, runners)
	var wg sync.WaitGroup
	for range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- RunMigrations(ctx, pool, "../../../migrations")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrations: %v", err)
		}
	}

	files, err := migrationFiles(os.DirFS("../../../migrations"))
	if err != nil {
		t.Fatalf("migration files: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&count); err != nil || count != len(files) {
		t.Fatalf("expected %d recorded migrations, got %d (err %v)", len(files), count, err)
	}
}

// 已经执行过基线迁移的库（例如 staging）再启动时，只补执行新增的迁移。
func TestIntegrationRunMigrationsUpgradesExistingDatabase(t *testing.T) {
	pool := newScratchDatabasePool(t)
	ctx := context.Background()

	baseline, err := os.ReadFile("../../../migrations/000001_init_schema.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if err := runMigrations(ctx, pool, mapFS("000001_init_schema.sql", baseline)); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	var hasColumn bool
	query := `select exists(select 1 from information_schema.columns where table_name = 'modules' and column_name = 'ci_job')`
	if err := pool.QueryRow(ctx, query).Scan(&hasColumn); err != nil || hasColumn {
		t.Fatalf("baseline must not have modules.ci_job yet: has=%v err=%v", hasColumn, err)
	}

	if err := RunMigrations(ctx, pool, "../../../migrations"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := pool.QueryRow(ctx, query).Scan(&hasColumn); err != nil || !hasColumn {
		t.Fatalf("expected modules.ci_job after upgrade: has=%v err=%v", hasColumn, err)
	}
	// 再执行一次是空操作。
	if err := RunMigrations(ctx, pool, "../../../migrations"); err != nil {
		t.Fatalf("rerun: %v", err)
	}
}

func mapFS(name string, content []byte) fstest.MapFS {
	return fstest.MapFS{name: {Data: content}}
}
