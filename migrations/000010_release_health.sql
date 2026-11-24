-- 发布健康：客户端上报启动结果，按发布统计启动失败率，比例灰度不健康时自动停用，见 docs/release-health.md。
-- 所有语句可重复执行。只新增表，旧版本进程不读写它们。

-- 5 分钟一个桶。users 是该桶里去重后的用户数（没有 userId 的事件不计），events 是事件总数。
-- 统计数据不加外键，模块、构建或发布删除后历史计数仍保留。
create table if not exists release_health_buckets (
    bucket_start timestamptz not null,
    application_id text not null,
    environment_code text not null,
    module_id text not null,
    build_record_id text not null,
    release_id text not null default '',
    channel text not null default '',
    platform text not null,
    outcome text not null,
    reason text not null default '',
    users bigint not null default 0,
    events bigint not null default 0,
    updated_at timestamptz not null default now(),
    primary key (bucket_start, application_id, environment_code, module_id, build_record_id, release_id, channel, platform, outcome, reason)
);
create index if not exists idx_release_health_buckets_release
    on release_health_buckets(release_id, bucket_start)
    where release_id <> '';

-- 每个应用、环境一条健康策略，没有记录时等同于关闭。
create table if not exists release_health_policies (
    application_id text not null references applications(id) on delete cascade,
    environment_code text not null references environments(code) on delete cascade,
    enabled boolean not null default false,
    window_minutes integer not null default 30 check (window_minutes between 5 and 1440),
    min_users integer not null default 50 check (min_users between 1 and 1000000),
    max_failure_rate double precision not null default 0.05 check (max_failure_rate > 0 and max_failure_rate <= 1),
    min_delta double precision not null default 0.02 check (min_delta >= 0 and min_delta <= 1),
    auto_stop_gray boolean not null default true,
    updated_by_user_id text references users(id) on delete set null,
    updated_at timestamptz not null default now(),
    primary key (application_id, environment_code)
);

-- 每条生效发布最近一次的判定结果，用来识别状态变化和在发布列表上展示。
create table if not exists release_health (
    release_id uuid primary key references release_records(id) on delete cascade,
    status text not null check (status in ('no_reports', 'insufficient_data', 'healthy', 'unhealthy')),
    launched_users bigint not null default 0,
    failed_users bigint not null default 0,
    failure_rate double precision not null default 0,
    baseline_failure_rate double precision,
    evaluated_at timestamptz not null default now()
);
