package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BuildAdapterRepository struct {
	pool *pgxpool.Pool
}

func NewBuildAdapterRepository(pool *pgxpool.Pool) *BuildAdapterRepository {
	return &BuildAdapterRepository{pool: pool}
}

const buildAdapterSelectSQL = `
	select
		ba.id,
		ba.name,
		ba.description,
		ba.dockerfile_template,
		ba.files,
		ba.parameters,
		ba.version,
		ba.status,
		(select count(*)::integer from modules m where m.build_adapter_id = ba.id) as module_count,
		ba.created_by_user_id,
		coalesce(cu.name, ''),
		coalesce(cu.avatar_url, ''),
		ba.updated_by_user_id,
		coalesce(uu.name, ''),
		coalesce(uu.avatar_url, ''),
		ba.created_at,
		ba.updated_at
	from build_adapters ba
	left join users cu on cu.id = ba.created_by_user_id
	left join users uu on uu.id = ba.updated_by_user_id`

func (r *BuildAdapterRepository) ListBuildAdapters(ctx context.Context, filters domain.BuildAdapterFilters) ([]domain.BuildAdapter, error) {
	rows, err := r.pool.Query(ctx, buildAdapterSelectSQL+`
		where ($1 = '' or ba.status = $1)
		order by ba.name asc, ba.created_at asc
	`, string(filters.Status))
	if err != nil {
		return nil, fmt.Errorf("query build adapters: %w", err)
	}
	defer rows.Close()

	adapters := make([]domain.BuildAdapter, 0)
	for rows.Next() {
		adapter, err := scanBuildAdapter(rows)
		if err != nil {
			return nil, err
		}
		adapters = append(adapters, adapter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate build adapters: %w", err)
	}
	return adapters, nil
}

func (r *BuildAdapterRepository) GetBuildAdapter(ctx context.Context, id string) (domain.BuildAdapter, error) {
	return scanBuildAdapter(r.pool.QueryRow(ctx, buildAdapterSelectSQL+` where ba.id = $1`, id))
}

func (r *BuildAdapterRepository) CreateBuildAdapter(ctx context.Context, input domain.BuildAdapterInput) (domain.BuildAdapter, error) {
	files, parameters, err := encodeBuildAdapterJSON(input)
	if err != nil {
		return domain.BuildAdapter{}, err
	}
	id := newBuildAdapterID()
	if _, err := r.pool.Exec(ctx, `
		insert into build_adapters (
			id, name, description, dockerfile_template, files, parameters,
			version, status, created_by_user_id, updated_by_user_id
		) values ($1, $2, $3, $4, $5::jsonb, $6::jsonb, 1, 'active', $7, $7)
	`, id, input.Name, input.Description, input.DockerfileTemplate, files, parameters, input.UserID); err != nil {
		if isForeignKeyViolation(err) {
			return domain.BuildAdapter{}, domain.ErrInvalidBuildAdapterInput
		}
		return domain.BuildAdapter{}, fmt.Errorf("insert build adapter: %w", err)
	}
	return r.GetBuildAdapter(ctx, id)
}

// UpdateBuildAdapter 用 expectedVersion 做乐观锁：版本已被其他请求修改时返回 ErrBuildAdapterVersionConflict。
func (r *BuildAdapterRepository) UpdateBuildAdapter(ctx context.Context, id string, expectedVersion int, input domain.BuildAdapterInput) (domain.BuildAdapter, error) {
	files, parameters, err := encodeBuildAdapterJSON(input)
	if err != nil {
		return domain.BuildAdapter{}, err
	}
	tag, err := r.pool.Exec(ctx, `
		update build_adapters set
			name = $3,
			description = $4,
			dockerfile_template = $5,
			files = $6::jsonb,
			parameters = $7::jsonb,
			version = version + 1,
			updated_by_user_id = $8,
			updated_at = now()
		where id = $1 and version = $2
	`, id, expectedVersion, input.Name, input.Description, input.DockerfileTemplate, files, parameters, input.UserID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return domain.BuildAdapter{}, domain.ErrInvalidBuildAdapterInput
		}
		return domain.BuildAdapter{}, fmt.Errorf("update build adapter: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetBuildAdapter(ctx, id); err != nil {
			return domain.BuildAdapter{}, err
		}
		return domain.BuildAdapter{}, domain.ErrBuildAdapterVersionConflict
	}
	return r.GetBuildAdapter(ctx, id)
}

// SetBuildAdapterStatus 只切换启停状态，不修改版本号：版本只追踪模板、文件与参数定义。
func (r *BuildAdapterRepository) SetBuildAdapterStatus(ctx context.Context, id string, status domain.BuildAdapterStatus, userID string) (domain.BuildAdapter, error) {
	tag, err := r.pool.Exec(ctx, `
		update build_adapters set
			status = $2,
			updated_by_user_id = $3,
			updated_at = now()
		where id = $1
	`, id, string(status), userID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return domain.BuildAdapter{}, domain.ErrInvalidBuildAdapterInput
		}
		return domain.BuildAdapter{}, fmt.Errorf("update build adapter status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.BuildAdapter{}, domain.ErrBuildAdapterNotFound
	}
	return r.GetBuildAdapter(ctx, id)
}

func scanBuildAdapter(row pgx.Row) (domain.BuildAdapter, error) {
	var adapter domain.BuildAdapter
	var files, parameters []byte
	var createdByName, createdByAvatar, updatedByName, updatedByAvatar string
	if err := row.Scan(
		&adapter.ID,
		&adapter.Name,
		&adapter.Description,
		&adapter.DockerfileTemplate,
		&files,
		&parameters,
		&adapter.Version,
		&adapter.Status,
		&adapter.ModuleCount,
		&adapter.CreatedByUserID,
		&createdByName,
		&createdByAvatar,
		&adapter.UpdatedByUserID,
		&updatedByName,
		&updatedByAvatar,
		&adapter.CreatedAt,
		&adapter.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.BuildAdapter{}, domain.ErrBuildAdapterNotFound
		}
		return domain.BuildAdapter{}, fmt.Errorf("scan build adapter: %w", err)
	}
	adapter.Files = map[string]string{}
	if err := json.Unmarshal(files, &adapter.Files); err != nil {
		return domain.BuildAdapter{}, fmt.Errorf("decode build adapter files: %w", err)
	}
	adapter.Parameters = []domain.BuildAdapterParameter{}
	if err := json.Unmarshal(parameters, &adapter.Parameters); err != nil {
		return domain.BuildAdapter{}, fmt.Errorf("decode build adapter parameters: %w", err)
	}
	adapter.CreatedBy = &domain.UserSummary{ID: adapter.CreatedByUserID, Name: createdByName, AvatarURL: createdByAvatar}
	adapter.UpdatedBy = &domain.UserSummary{ID: adapter.UpdatedByUserID, Name: updatedByName, AvatarURL: updatedByAvatar}
	return adapter, nil
}

func encodeBuildAdapterJSON(input domain.BuildAdapterInput) (string, string, error) {
	files := input.Files
	if files == nil {
		files = map[string]string{}
	}
	parameters := input.Parameters
	if parameters == nil {
		parameters = []domain.BuildAdapterParameter{}
	}
	filesJSON, err := json.Marshal(files)
	if err != nil {
		return "", "", fmt.Errorf("encode build adapter files: %w", err)
	}
	parametersJSON, err := json.Marshal(parameters)
	if err != nil {
		return "", "", fmt.Errorf("encode build adapter parameters: %w", err)
	}
	return string(filesJSON), string(parametersJSON), nil
}

func newBuildAdapterID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "build-adapter-" + hex.EncodeToString(value[:])
	}
	return "build-adapter-" + time.Now().UTC().Format("20060102150405.000000000")
}
