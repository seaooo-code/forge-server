create table if not exists environments (
    code text primary key,
    short_code text not null unique,
    name text not null,
    status text not null check (status in ('healthy', 'gray')),
    tone text not null check (tone in ('amber', 'blue', 'emerald', 'rose', 'sky')),
    description text not null,
    current_combination text not null,
    traffic_label text not null,
    endpoint text not null,
    cdn_domain text not null,
    branch_policy text not null,
    release_policy text not null,
    current_release text not null,
    bundle_count integer not null check (bundle_count >= 0),
    total_size text not null,
    health_score text not null,
    sort_order integer not null unique,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists environment_guardrails (
    id bigserial primary key,
    environment_code text not null references environments(code) on delete cascade,
    guardrail_key text not null,
    label text not null,
    value text not null,
    status text not null check (status in ('通过', '关注')),
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (environment_code, guardrail_key)
);

create table if not exists environment_timeline_events (
    id bigserial primary key,
    environment_code text not null references environments(code) on delete cascade,
    event_key text not null,
    display_time text not null,
    action text not null,
    operator text not null,
    status text not null check (status in ('成功', '进行中', '等待')),
    detail text not null,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (environment_code, event_key)
);

insert into environments (
    code,
    short_code,
    name,
    status,
    tone,
    description,
    current_combination,
    traffic_label,
    endpoint,
    cdn_domain,
    branch_policy,
    release_policy,
    current_release,
    bundle_count,
    total_size,
    health_score,
    sort_order,
    updated_at
) values
    (
        'dev',
        'DEV',
        '开发环境',
        'healthy',
        'sky',
        '开发分支自动构建滚动部署',
        'dev-rolling-20260609.2',
        '-',
        'https://rn-dev.internal.mi.com',
        'rn-cdn-dev.xiaomi.com',
        'feature/* 自动接入，主干每日同步',
        '流水线成功后自动部署',
        '自动滚动更新',
        12,
        '4.1 MB',
        '99.6%',
        10,
        '2026-06-09 14:32:18+08'
    ),
    (
        'staging',
        'STAGING',
        '联调环境',
        'healthy',
        'blue',
        '多模块联调，接入测试账号与模拟数据',
        'staging-combo-20260608.5',
        '研发内网',
        'https://rn-staging.internal.mi.com',
        'rn-cdn-staging.xiaomi.com',
        'feature/* 与 release/* 联调',
        '模块负责人确认后更新',
        '联调验证版本',
        12,
        '4.0 MB',
        '99.7%',
        20,
        '2026-06-09 13:32:18+08'
    ),
    (
        'preview',
        'PREVIEW',
        '预览环境',
        'healthy',
        'amber',
        '产品验收与版本预览，灰度前体验验证',
        'preview-combo-20260609.1',
        '体验账号',
        'https://rn-preview.internal.mi.com',
        'rn-cdn-preview.xiaomi.com',
        'release/* 与验收分支双轨',
        '产品验收通过后同步',
        '体验验收版本',
        12,
        '3.9 MB',
        '99.8%',
        30,
        '2026-06-09 11:32:18+08'
    ),
    (
        'preProduct',
        'PREPRODUCT',
        '预生产环境',
        'healthy',
        'emerald',
        '线上配置与真实链路，生产前最后验证',
        'preproduct-combo-20260609.1',
        '10K 内测',
        'https://rn-preproduct.internal.mi.com',
        'rn-cdn-preproduct.xiaomi.com',
        'release/* 只读锁定',
        '发布管理员审批后更新',
        'RT-20260610',
        12,
        '3.9 MB',
        '99.9%',
        40,
        '2026-06-09 14:04:18+08'
    ),
    (
        'product',
        'PRODUCT',
        '生产环境',
        'gray',
        'rose',
        '正式环境，受发布单约束',
        'product-combo-20260609.0',
        '2.4M 在线',
        'https://rn.xiaomi.com',
        'rn-cdn.xiaomi.com',
        'prod tag 锁定，不允许直接写入',
        '发布单审批 + 灰度推进',
        'RT-20260611',
        12,
        '3.9 MB',
        '99.8%',
        50,
        '2026-06-09 14:20:18+08'
    )
on conflict (code) do update set
    short_code = excluded.short_code,
    name = excluded.name,
    status = excluded.status,
    tone = excluded.tone,
    description = excluded.description,
    current_combination = excluded.current_combination,
    traffic_label = excluded.traffic_label,
    endpoint = excluded.endpoint,
    cdn_domain = excluded.cdn_domain,
    branch_policy = excluded.branch_policy,
    release_policy = excluded.release_policy,
    current_release = excluded.current_release,
    bundle_count = excluded.bundle_count,
    total_size = excluded.total_size,
    health_score = excluded.health_score,
    sort_order = excluded.sort_order,
    updated_at = excluded.updated_at;

insert into environment_guardrails (
    environment_code,
    guardrail_key,
    label,
    value,
    status,
    sort_order
)
select
    environment_code,
    guardrail_key,
    label,
    value,
    status,
    sort_order
from (
    values
        ('dev', 'dependency', '依赖检查', 'Common Bundle 与 Feature Bundle 版本兼容', '通过', 10),
        ('dev', 'size', '体积阈值', '全部 Bundle 未超过阈值', '通过', 20),
        ('dev', 'approval', '发布准入', '开发环境允许自动滚动，无需审批', '关注', 30),
        ('dev', 'cdn', 'CDN 同步', '多 Region 缓存命中正常', '通过', 40),
        ('staging', 'dependency', '依赖检查', 'Common Bundle 与 Feature Bundle 版本兼容', '通过', 10),
        ('staging', 'size', '体积阈值', '全部 Bundle 未超过阈值', '通过', 20),
        ('staging', 'approval', '发布准入', '审批链路完整', '通过', 30),
        ('staging', 'cdn', 'CDN 同步', '多 Region 缓存命中正常', '通过', 40),
        ('preview', 'dependency', '依赖检查', 'Common Bundle 与 Feature Bundle 版本兼容', '通过', 10),
        ('preview', 'size', '体积阈值', '全部 Bundle 未超过阈值', '通过', 20),
        ('preview', 'approval', '发布准入', '审批链路完整', '通过', 30),
        ('preview', 'cdn', 'CDN 同步', '多 Region 缓存命中正常', '通过', 40),
        ('preProduct', 'dependency', '依赖检查', 'Common Bundle 与 Feature Bundle 版本兼容', '通过', 10),
        ('preProduct', 'size', '体积阈值', '全部 Bundle 未超过阈值', '通过', 20),
        ('preProduct', 'approval', '发布准入', '审批链路完整', '通过', 30),
        ('preProduct', 'cdn', 'CDN 同步', '多 Region 缓存命中正常', '通过', 40),
        ('product', 'dependency', '依赖检查', 'Common Bundle 与 Feature Bundle 版本兼容', '通过', 10),
        ('product', 'size', '体积阈值', 'vehicle 665 KB，接近 700 KB 阈值', '关注', 20),
        ('product', 'approval', '发布准入', '审批链路完整', '通过', 30),
        ('product', 'cdn', 'CDN 同步', '多 Region 缓存命中正常', '通过', 40)
) as seed(environment_code, guardrail_key, label, value, status, sort_order)
on conflict (environment_code, guardrail_key) do update set
    label = excluded.label,
    value = excluded.value,
    status = excluded.status,
    sort_order = excluded.sort_order,
    updated_at = now();

insert into environment_timeline_events (
    environment_code,
    event_key,
    display_time,
    action,
    operator,
    status,
    detail,
    sort_order
)
select
    environment_code,
    event_key,
    display_time,
    action,
    operator,
    status,
    detail,
    sort_order
from (
    values
        ('dev', 'sync', '14:32:18', '同步 Combination', 'auto.builder', '成功', 'dev-rolling-20260609.2 已写入环境配置', 10),
        ('dev', 'cdn', '14:33:04', '刷新 CDN', 'release.bot', '成功', '缓存刷新完成，命中率恢复到基线', 20),
        ('dev', 'health', '14:35:20', '健康检查', 'system', '成功', 'JS 错误率、白屏率、Crash Free 均通过', 30),
        ('dev', 'notify', '14:36:10', '通知订阅人', 'release-notify', '等待', '飞书机器人、钉钉群、邮件订阅已排队', 40),
        ('staging', 'sync', '14:32:18', '同步 Combination', 'release.bot', '成功', 'staging-combo-20260608.5 已写入环境配置', 10),
        ('staging', 'cdn', '14:33:04', '刷新 CDN', 'release.bot', '成功', '缓存刷新完成，命中率恢复到基线', 20),
        ('staging', 'health', '14:35:20', '健康检查', 'system', '成功', 'JS 错误率、白屏率、Crash Free 均通过', 30),
        ('staging', 'notify', '14:36:10', '通知订阅人', 'release-notify', '等待', '飞书机器人、钉钉群、邮件订阅已排队', 40),
        ('preview', 'sync', '14:32:18', '同步 Combination', 'release.bot', '成功', 'preview-combo-20260609.1 已写入环境配置', 10),
        ('preview', 'cdn', '14:33:04', '刷新 CDN', 'release.bot', '成功', '缓存刷新完成，命中率恢复到基线', 20),
        ('preview', 'health', '14:35:20', '健康检查', 'system', '成功', 'JS 错误率、白屏率、Crash Free 均通过', 30),
        ('preview', 'notify', '14:36:10', '通知订阅人', 'release-notify', '等待', '飞书机器人、钉钉群、邮件订阅已排队', 40),
        ('preProduct', 'sync', '14:32:18', '同步 Combination', 'release.bot', '成功', 'preproduct-combo-20260609.1 已写入环境配置', 10),
        ('preProduct', 'cdn', '14:33:04', '刷新 CDN', 'release.bot', '成功', '缓存刷新完成，命中率恢复到基线', 20),
        ('preProduct', 'health', '14:35:20', '健康检查', 'system', '成功', 'JS 错误率、白屏率、Crash Free 均通过', 30),
        ('preProduct', 'notify', '14:36:10', '通知订阅人', 'release-notify', '等待', '飞书机器人、钉钉群、邮件订阅已排队', 40),
        ('product', 'sync', '14:32:18', '同步 Combination', 'release.bot', '进行中', 'product-combo-20260609.0 已写入环境配置', 10),
        ('product', 'cdn', '14:33:04', '刷新 CDN', 'release.bot', '成功', '缓存刷新完成，命中率恢复到基线', 20),
        ('product', 'health', '14:35:20', '健康检查', 'system', '成功', 'JS 错误率、白屏率、Crash Free 均通过', 30),
        ('product', 'notify', '14:36:10', '通知订阅人', 'release-notify', '等待', '飞书机器人、钉钉群、邮件订阅已排队', 40)
) as seed(environment_code, event_key, display_time, action, operator, status, detail, sort_order)
on conflict (environment_code, event_key) do update set
    display_time = excluded.display_time,
    action = excluded.action,
    operator = excluded.operator,
    status = excluded.status,
    detail = excluded.detail,
    sort_order = excluded.sort_order,
    updated_at = now();

create index if not exists idx_environment_guardrails_environment_order
    on environment_guardrails(environment_code, sort_order);

create index if not exists idx_environment_timeline_events_environment_order
    on environment_timeline_events(environment_code, sort_order);
