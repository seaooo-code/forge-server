create table if not exists feishu_users (
    union_id text primary key,
    open_id text not null default '',
    user_id text not null default '',
    name text not null default '',
    en_name text not null default '',
    avatar_url text not null default '',
    avatar_thumb text not null default '',
    avatar_middle text not null default '',
    avatar_big text not null default '',
    email text not null default '',
    enterprise_email text not null default '',
    mobile text not null default '',
    tenant_key text not null default '',
    employee_no text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    last_login_at timestamptz not null default now()
);

create index if not exists idx_feishu_users_open_id on feishu_users(open_id);
create index if not exists idx_feishu_users_user_id on feishu_users(user_id);
create index if not exists idx_feishu_users_tenant_key on feishu_users(tenant_key);
