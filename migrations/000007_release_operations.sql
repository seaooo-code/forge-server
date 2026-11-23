-- 发布运维：发布操作记录，以及按天聚合的客户端拉包统计。只新增表，旧版本进程不读写它们。

-- 发布操作记录：创建、调整灰度比例、转为全量、被新发布替换、停用。
create table if not exists release_events (
    id bigserial primary key,
    release_id uuid not null references release_records(id) on delete cascade,
    kind text not null,
    detail jsonb not null default '{}',
    actor_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now()
);
create index if not exists idx_release_events_release on release_events(release_id, created_at desc);

-- 客户端拉包统计：manifest 请求（含 304）里每个模块的解析结果，按天累加。
-- outcome 是来源（testPlan、percentage、full）或 missing:{原因}；缺包时 build_record_id 为空。
-- 统计数据不加外键，模块或构建删除后历史计数仍保留。
create table if not exists artifact_pull_daily_stats (
    day date not null,
    application_id text not null,
    environment_code text not null,
    module_id text not null,
    runtime_version text not null,
    platform text not null,
    outcome text not null,
    build_record_id text not null default '',
    requests bigint not null default 0,
    updated_at timestamptz not null default now(),
    primary key (day, application_id, environment_code, module_id, runtime_version, platform, outcome, build_record_id)
);
create index if not exists idx_artifact_pull_daily_stats_module on artifact_pull_daily_stats(module_id, environment_code, day desc);
create index if not exists idx_artifact_pull_daily_stats_build on artifact_pull_daily_stats(build_record_id, day desc) where build_record_id <> '';
