package postgres

import (
	"context"
	"testing"

	"forge-server/internal/domain"
)

// 路径风格开关：存储配置可以保存和修改，构建记录读出所属存储的设置，worker 上传和读取产物都按它寻址。
func TestIntegrationStorageForcePathStyle(t *testing.T) {
	pool := newIntegrationPool(t)
	tenantID, userID := newIntegrationTenant(t, pool)
	ctx := domain.WithTenant(context.Background(), tenantID)
	repository := NewStorageRepository(pool)

	input := domain.StorageConfigurationInput{
		Name:            "fds",
		Adapter:         domain.StorageAdapterS3,
		Bucket:          "forge-test",
		Endpoint:        "https://cnbj3-fusion-fds.api.xiaomi.net",
		Region:          "us-east-1",
		OwnerUserID:     userID,
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		Status:          domain.StorageStatusHealthy,
		ForcePathStyle:  true,
	}
	created, err := repository.CreateStorageConfiguration(ctx, input)
	if err != nil {
		t.Fatalf("create storage configuration: %v", err)
	}
	if !created.ForcePathStyle {
		t.Fatal("expected force_path_style to be stored on create")
	}

	input.ForcePathStyle = false
	updated, err := repository.UpdateStorageConfiguration(ctx, created.ID, input)
	if err != nil {
		t.Fatalf("update storage configuration: %v", err)
	}
	if updated.ForcePathStyle {
		t.Fatal("expected force_path_style to be cleared on update")
	}

	moduleID, environment := seedIntegrationModule(t, pool)
	builds := NewBuildRepository(pool)
	// 构建属于种子模块所在的租户，和上面的存储配置不是同一个租户。
	buildCtx := moduleTenantCtx(t, pool, moduleID)
	record, err := builds.CreateBuildRecord(buildCtx, domain.BuildRecordInput{ModuleID: moduleID, Environment: environment, TriggerType: domain.BuildTriggerTypeManual})
	if err != nil {
		t.Fatalf("create build: %v", err)
	}
	if record.StorageForcePathStyle {
		t.Fatal("expected the seeded storage to default to virtual-hosted style")
	}
	if _, err := pool.Exec(buildCtx, `update storage_configurations set force_path_style = true where id = $1`, record.StorageConfigurationID); err != nil {
		t.Fatalf("enable force_path_style: %v", err)
	}
	reloaded, err := builds.GetBuildRecord(buildCtx, record.ID)
	if err != nil {
		t.Fatalf("get build: %v", err)
	}
	if !reloaded.StorageForcePathStyle {
		t.Fatal("expected the build record to carry the storage force_path_style setting")
	}
}
