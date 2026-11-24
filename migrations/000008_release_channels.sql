-- 发布渠道：同一环境里给发布加一层范围，客户端拉包时上报 channel，见 docs/channels.md。
-- 所有语句可重复执行。部署完成前不要创建渠道：旧实例不认识 channel 列，会把渠道路由当成默认路由解析。

-- 渠道属于应用，在所有环境通用。默认渠道是空字符串，不存在这张表里。
create table if not exists application_channels (
    application_id text not null references applications(id) on delete cascade,
    code text not null check (code ~ '^[a-z][a-z0-9-]{0,31}$'),
    name text not null check (name <> ''),
    description text not null default '',
    created_by_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (application_id, code)
);

-- 发布和路由所属的渠道，默认渠道为空字符串。不加外键：删除渠道前要求没有生效中的发布，历史记录保留原值。
alter table release_records add column if not exists channel text not null default '';
alter table release_routes add column if not exists channel text not null default '';

-- 生效路由的唯一约束加上渠道：同一模块、环境、运行时版本下，每个渠道各有一条全量和一条比例灰度。
create unique index if not exists idx_release_routes_default_active_channel
    on release_routes(module_id, environment_code, runtime_version, channel)
    where route_type = 'default' and status = 'active';
create unique index if not exists idx_release_routes_percentage_active_channel
    on release_routes(module_id, environment_code, runtime_version, channel)
    where route_type = 'percentage' and status = 'active';
drop index if exists idx_release_routes_default_active;
drop index if exists idx_release_routes_percentage_active;

create index if not exists idx_release_records_channel
    on release_records(channel)
    where channel <> '';

-- 拉包统计加渠道维度，记录的是解析后实际使用的渠道（未注册的渠道记为默认渠道）。
-- 换主键后旧实例的 on conflict 对不上，部署窗口内旧实例的统计写入会失败。
alter table artifact_pull_daily_stats add column if not exists channel text not null default '';
alter table artifact_pull_daily_stats drop constraint if exists artifact_pull_daily_stats_pkey;
alter table artifact_pull_daily_stats add constraint artifact_pull_daily_stats_pkey
    primary key (day, application_id, environment_code, module_id, runtime_version, platform, channel, outcome, build_record_id);
