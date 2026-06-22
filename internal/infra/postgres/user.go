package postgres

import (
	"context"
	"fmt"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgconn"
)

type userExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func upsertUser(ctx context.Context, executor userExecutor, user domain.UserInput) error {
	departmentIDs := user.DepartmentIDs
	if departmentIDs == nil {
		departmentIDs = []string{}
	}

	if _, err := executor.Exec(ctx, `
		insert into users (
			id,
			name,
			email,
			avatar_url,
			department_ids,
			feishu_union_id,
			feishu_open_id,
			feishu_user_id
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
			on conflict (id) do update set
				name = excluded.name,
				email = excluded.email,
				avatar_url = excluded.avatar_url,
				department_ids = excluded.department_ids,
				feishu_union_id = excluded.feishu_union_id,
				feishu_open_id = excluded.feishu_open_id,
				feishu_user_id = excluded.feishu_user_id,
				updated_at = now()
	`,
		user.ID,
		user.Name,
		user.Email,
		user.AvatarURL,
		departmentIDs,
		user.FeishuUnionID,
		user.FeishuOpenID,
		user.FeishuUserID,
	); err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}
