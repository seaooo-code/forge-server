package postgres

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"forge-server/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

const passwordCommandTimeout = 15 * time.Second

// resolvePassword 执行 POSTGRES_PASSWORD_COMMAND 并返回标准输出（去掉首尾空白）作为数据库密码。
// 命令的标准错误只在失败时截取一小段放进错误信息，命令不得向标准错误输出密码。
func resolvePassword(ctx context.Context, command []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, passwordCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, command[0], command[1:]...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			detail := strings.TrimSpace(string(exitErr.Stderr))
			if len(detail) > 200 {
				detail = detail[:200]
			}
			return "", fmt.Errorf("run postgres password command %q: %w: %s", command[0], err, detail)
		}
		return "", fmt.Errorf("run postgres password command %q: %w", command[0], err)
	}

	password := strings.TrimSpace(string(output))
	if password == "" {
		return "", fmt.Errorf("postgres password command %q printed an empty password", command[0])
	}
	return password, nil
}

func NewPool(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	if len(cfg.PasswordCommand) > 0 {
		password, err := resolvePassword(ctx, cfg.PasswordCommand)
		if err != nil {
			return nil, err
		}
		poolConfig.ConnConfig.Password = password
	}

	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
