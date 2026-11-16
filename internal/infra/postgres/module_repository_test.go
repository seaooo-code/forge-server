package postgres

import (
	"errors"
	"testing"

	"forge-server/internal/domain"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestModuleUniqueViolationError(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{
			name:       "module primary key",
			constraint: "modules_pkey",
			want:       domain.ErrModuleIDConflict,
		},
		{
			name:       "common module application",
			constraint: "idx_modules_common_application_unique",
			want:       domain.ErrModuleCommonConflict,
		},
		{
			name:       "module repository",
			constraint: "modules_repository_dockerfile_unique",
			want:       domain.ErrModuleRepositoryConflict,
		},
		{
			name:       "unknown module unique constraint",
			constraint: "unexpected_unique_constraint",
			want:       domain.ErrModuleIDConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := moduleUniqueViolationError(&pgconn.PgError{ConstraintName: tt.constraint})
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}
