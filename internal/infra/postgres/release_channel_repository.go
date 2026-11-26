package postgres

import (
	"context"
	"errors"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReleaseChannelRepository struct {
	pool *pgxpool.Pool
}

func NewReleaseChannelRepository(pool *pgxpool.Pool) *ReleaseChannelRepository {
	return &ReleaseChannelRepository{pool: pool}
}

const releaseChannelSelectSQL = `
	select
		ch.application_id,
		ch.code,
		ch.name,
		ch.description,
		(
			select count(*)::integer
			from release_records rr
			join modules m on m.id = rr.module_id
			where m.application_id = ch.application_id
				and rr.channel = ch.code
				and rr.status = 'active'
		),
		coalesce(ch.created_by_user_id, ''),
		ch.created_at,
		ch.updated_at
	from application_channels ch
	join applications a on a.id = ch.application_id
`

func (r *ReleaseChannelRepository) ListReleaseChannels(ctx context.Context, applicationSlug string) ([]domain.ReleaseChannel, error) {
	if _, err := applicationIDBySlug(ctx, r.pool, applicationSlug); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, releaseChannelSelectSQL+`
		where a.slug = $1
		order by ch.created_at, ch.code
	`, applicationSlug)
	if err != nil {
		return nil, fmt.Errorf("query release channels: %w", err)
	}
	defer rows.Close()

	channels := []domain.ReleaseChannel{}
	for rows.Next() {
		channel, err := scanReleaseChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release channels: %w", err)
	}
	return channels, nil
}

func (r *ReleaseChannelRepository) CreateReleaseChannel(ctx context.Context, applicationSlug string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error) {
	applicationID, err := applicationIDBySlug(ctx, r.pool, applicationSlug)
	if err != nil {
		return domain.ReleaseChannel{}, err
	}
	if _, err := r.pool.Exec(ctx, `
		insert into application_channels (application_id, code, name, description, created_by_user_id)
		values ($1, $2, $3, $4, nullif($5, ''))
	`, applicationID, input.Code, input.Name, input.Description, input.CreatedByUserID); err != nil {
		switch {
		case isUniqueViolation(err):
			return domain.ReleaseChannel{}, domain.ErrReleaseChannelExists
		case isCheckViolation(err), isForeignKeyViolation(err):
			return domain.ReleaseChannel{}, domain.ErrInvalidReleaseChannelInput
		}
		return domain.ReleaseChannel{}, fmt.Errorf("insert release channel: %w", err)
	}
	return r.getReleaseChannel(ctx, applicationSlug, input.Code)
}

func (r *ReleaseChannelRepository) UpdateReleaseChannel(ctx context.Context, applicationSlug string, code string, input domain.ReleaseChannelInput) (domain.ReleaseChannel, error) {
	// slug 全平台唯一，先在当前租户内确认应用存在，后面的语句就可以直接按 slug 操作。
	if _, err := applicationIDBySlug(ctx, r.pool, applicationSlug); err != nil {
		return domain.ReleaseChannel{}, err
	}
	tag, err := r.pool.Exec(ctx, `
		update application_channels ch
		set name = $3, description = $4, updated_at = now()
		from applications a
		where a.id = ch.application_id
			and a.slug = $1
			and ch.code = $2
	`, applicationSlug, code, input.Name, input.Description)
	if err != nil {
		if isCheckViolation(err) {
			return domain.ReleaseChannel{}, domain.ErrInvalidReleaseChannelInput
		}
		return domain.ReleaseChannel{}, fmt.Errorf("update release channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := applicationIDBySlug(ctx, r.pool, applicationSlug); err != nil {
			return domain.ReleaseChannel{}, err
		}
		return domain.ReleaseChannel{}, domain.ErrReleaseChannelNotFound
	}
	return r.getReleaseChannel(ctx, applicationSlug, code)
}

// DeleteReleaseChannel 只在渠道没有生效中的发布时删除。锁住渠道行，与创建发布时的 for share 互斥，避免删除和发布并发。
func (r *ReleaseChannelRepository) DeleteReleaseChannel(ctx context.Context, applicationSlug string, code string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete release channel: %w", err)
	}
	defer rollbackMigration(ctx, tx)

	if _, err := applicationIDBySlug(ctx, tx, applicationSlug); err != nil {
		return err
	}
	var applicationID string
	if err := tx.QueryRow(ctx, `
		select ch.application_id
		from application_channels ch
		join applications a on a.id = ch.application_id
		where a.slug = $1 and ch.code = $2
		for update of ch
	`, applicationSlug, code).Scan(&applicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := applicationIDBySlug(ctx, tx, applicationSlug); err != nil {
				return err
			}
			return domain.ErrReleaseChannelNotFound
		}
		return fmt.Errorf("query release channel for deletion: %w", err)
	}
	var inUse bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from release_records rr
			join modules m on m.id = rr.module_id
			where m.application_id = $1
				and rr.channel = $2
				and rr.status = 'active'
		)
	`, applicationID, code).Scan(&inUse); err != nil {
		return fmt.Errorf("query release channel usage: %w", err)
	}
	if inUse {
		return domain.ErrReleaseChannelInUse
	}
	if _, err := tx.Exec(ctx, `delete from application_channels where application_id = $1 and code = $2`, applicationID, code); err != nil {
		return fmt.Errorf("delete release channel: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete release channel: %w", err)
	}
	return nil
}

func (r *ReleaseChannelRepository) getReleaseChannel(ctx context.Context, applicationSlug string, code string) (domain.ReleaseChannel, error) {
	channel, err := scanReleaseChannel(r.pool.QueryRow(ctx, releaseChannelSelectSQL+`
		where a.slug = $1 and ch.code = $2
	`, applicationSlug, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReleaseChannel{}, domain.ErrReleaseChannelNotFound
	}
	return channel, err
}

func scanReleaseChannel(row pgx.Row) (domain.ReleaseChannel, error) {
	var channel domain.ReleaseChannel
	if err := row.Scan(
		&channel.ApplicationID,
		&channel.Code,
		&channel.Name,
		&channel.Description,
		&channel.ActiveReleases,
		&channel.CreatedByUserID,
		&channel.CreatedAt,
		&channel.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReleaseChannel{}, err
		}
		return domain.ReleaseChannel{}, fmt.Errorf("scan release channel: %w", err)
	}
	return channel, nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// applicationIDBySlug 在当前租户范围内按 slug 查应用 ID。
func applicationIDBySlug(ctx context.Context, querier rowQuerier, slug string) (string, error) {
	tenantID, err := tenantFilter(ctx)
	if err != nil {
		return "", err
	}
	var id string
	if err := querier.QueryRow(ctx, `
		select id from applications where slug = $1 and ($2::text = '' or tenant_id = $2)
	`, slug, tenantID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrApplicationNotFound
		}
		return "", fmt.Errorf("query application by slug: %w", err)
	}
	return id, nil
}
