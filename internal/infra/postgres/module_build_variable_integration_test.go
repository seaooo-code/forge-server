package postgres

import (
	"context"
	"errors"
	"testing"

	"forge-server/internal/domain"
)

// 创建模块时一起提交的构建变量和模块在同一个事务里写入；任何一条变量写入失败，模块也不会留下。
func TestIntegrationCreateModuleWithBuildVariables(t *testing.T) {
	pool := newIntegrationPool(t)
	commonModuleID, environment := seedIntegrationModule(t, pool)
	ctx := context.Background()

	var applicationID, ownerUserID, connectionID, repositoryID string
	var moduleCount int
	if err := pool.QueryRow(ctx, `
		select m.application_id, m.owner_user_id, m.code_source_connection_id, m.external_repository_id, a.module_count
		from modules m join applications a on a.id = m.application_id
		where m.id = $1
	`, commonModuleID).Scan(&applicationID, &ownerUserID, &connectionID, &repositoryID, &moduleCount); err != nil {
		t.Fatal(err)
	}

	modules := NewModuleRepository(pool)
	featureInput := func(id string, variables ...domain.ModuleBuildVariable) domain.ModuleInput {
		return domain.ModuleInput{
			ApplicationID:  applicationID,
			CommonModuleID: commonModuleID,
			ID:             id,
			Kind:           domain.ModuleKindFeature,
			Name:           id,
			Owner:          "tester",
			OwnerUserID:    ownerUserID,
			Repository:     domain.ModuleRepository{ConnectionID: connectionID, ExternalRepositoryID: repositoryID, FullName: "demo/repo"},
			BuildConfig: domain.ModuleBuildConfig{
				BuildMode:      domain.BuildModeGitLabCI,
				CIJob:          "forge_build_" + id,
				CIArtifactPath: "dist",
				DockerfilePath: domain.DefaultModuleDockerfilePath,
				BuildContext:   domain.DefaultModuleBuildContext,
			},
			BuildVariables: variables,
		}
	}
	variable := func(key string, encrypted string, environmentCode string, secret bool) domain.ModuleBuildVariable {
		return domain.ModuleBuildVariable{Key: key, ValueEncrypted: encrypted, EnvironmentCode: environmentCode, IsSecret: secret, UpdatedByUserID: ownerUserID}
	}

	created := commonModuleID + "-with-vars"
	if _, err := modules.CreateModule(ctx, featureInput(created,
		variable("YRN_KIND", "enc:business", "", false),
		variable("YRN_KIND", "enc:common", environment, false),
		variable("NPM_TOKEN", "enc:token", "", true),
	)); err != nil {
		t.Fatalf("create module with build variables: %v", err)
	}
	saved, err := NewBuildVariableRepository(pool).ListModuleBuildVariables(ctx, created)
	if err != nil {
		t.Fatalf("list build variables: %v", err)
	}
	if len(saved) != 3 {
		t.Fatalf("expected 3 build variables, got %#v", saved)
	}
	for _, item := range saved {
		if item.ModuleID != created || item.UpdatedByUserID != ownerUserID || item.ValueEncrypted == "" {
			t.Fatalf("unexpected saved build variable: %#v", item)
		}
	}

	// 第二条变量的环境不存在：整个创建回滚，模块和第一条变量都不会留下，应用的模块数也不变。
	rolledBack := commonModuleID + "-rollback"
	_, err = modules.CreateModule(ctx, featureInput(rolledBack,
		variable("YRN_KIND", "enc:business", "", false),
		variable("YRN_KIND", "enc:common", "missing-env", false),
	))
	if !errors.Is(err, domain.ErrEnvironmentNotFound) {
		t.Fatalf("expected environment not found, got %v", err)
	}
	if _, err := modules.GetModule(ctx, rolledBack); !errors.Is(err, domain.ErrModuleNotFound) {
		t.Fatalf("module must be rolled back with its build variables, got %v", err)
	}
	var orphanVariables, currentCount int
	if err := pool.QueryRow(ctx, `select count(*) from module_build_variables where module_id = $1`, rolledBack).Scan(&orphanVariables); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select module_count from applications where id = $1`, applicationID).Scan(&currentCount); err != nil {
		t.Fatal(err)
	}
	if orphanVariables != 0 || currentCount != moduleCount+1 {
		t.Fatalf("expected no orphan variables and module count %d, got %d variables, count %d", moduleCount+1, orphanVariables, currentCount)
	}
}
