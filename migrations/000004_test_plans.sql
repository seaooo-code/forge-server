-- 提测单：一个需求一张，包含「模块 + 分支 → 构建」条目和一组客户端 userId 成员，见 docs/test-plans.md。
-- 所有语句可重复执行。

-- 环境级开关：只有开启的环境里，提测单才会在 manifest 解析中生效。默认关闭。
alter table environments
    add column if not exists allow_test_plans boolean not null default false;

create table if not exists test_plans (
    id uuid primary key,
    application_id text not null references applications(id) on delete cascade,
    environment_code text not null references environments(code) on delete restrict,
    name text not null,
    link text not null default '',
    status text not null check (status in ('open', 'closed')),
    expires_at timestamptz not null,
    created_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    closed_at timestamptz
);

-- 条目必须绑定模块的一个分支；同一模块的同一分支只能属于一张进行中的提测单（服务层在事务里校验）。
-- mode = follow 时，该分支在同环境下构建成功后条目自动换成新构建；pinned 时构建不变。
create table if not exists test_plan_items (
    test_plan_id uuid not null references test_plans(id) on delete cascade,
    module_id text not null references modules(id) on delete cascade,
    branch text not null check (branch <> ''),
    mode text not null default 'follow' check (mode in ('follow', 'pinned')),
    build_record_id uuid not null references build_records(id) on delete restrict,
    updated_by_user_id text references users(id) on delete set null,
    updated_at timestamptz not null default now(),
    primary key (test_plan_id, module_id)
);

-- 成员是客户端上报的 userId（例如登录账号 ID），不是 Forge 用户。
-- 同一 userId 不能同时属于两张都包含同一模块的进行中提测单（服务层在事务里校验）。
create table if not exists test_plan_members (
    test_plan_id uuid not null references test_plans(id) on delete cascade,
    user_id text not null,
    note text not null default '',
    added_by_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now(),
    primary key (test_plan_id, user_id)
);

-- 控制台用户保存的常用测试账号，按环境区分：不同环境的账号体系可能不同，数值可能重叠。
create table if not exists user_test_accounts (
    owner_user_id text not null references users(id) on delete cascade,
    environment_code text not null references environments(code) on delete cascade,
    user_id text not null,
    note text not null default '',
    created_at timestamptz not null default now(),
    primary key (owner_user_id, environment_code, user_id)
);

-- 提测单操作记录：条目和成员的增删、因冲突被移出、关闭、延长、跟随分支自动更新。
create table if not exists test_plan_events (
    id bigserial primary key,
    test_plan_id uuid not null references test_plans(id) on delete cascade,
    kind text not null,
    detail jsonb not null default '{}',
    actor_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now()
);

create index if not exists idx_test_plan_members_user on test_plan_members(user_id);
create index if not exists idx_test_plan_items_module_branch on test_plan_items(module_id, branch);
create index if not exists idx_test_plan_items_build on test_plan_items(build_record_id);
create index if not exists idx_test_plans_open
    on test_plans(application_id, environment_code, expires_at)
    where status = 'open';
create index if not exists idx_test_plan_events_plan on test_plan_events(test_plan_id, created_at desc);
