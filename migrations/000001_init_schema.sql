create table users (
    id text primary key,
    name text not null,
    email text not null default '',
    avatar_url text not null default '',
    department_ids text[] not null default '{}',
    feishu_union_id text not null default '',
    feishu_open_id text not null default '',
    feishu_user_id text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_users_name on users(name);
create index idx_users_feishu_union_id
    on users(feishu_union_id)
    where feishu_union_id <> '';
create index idx_users_feishu_user_id
    on users(feishu_user_id)
    where feishu_user_id <> '';

create table storage_configurations (
    id text primary key,
    name text not null,
    adapter text not null check (adapter in ('S3')),
    bucket text not null,
    endpoint text not null,
    region text not null,
    owner_user_id text not null references users(id) on delete restrict,
    public_base_url text not null default '',
    access_key_id text not null,
    secret_access_key text not null,
    status text not null check (status in ('healthy', 'error', 'disabled')),
    last_verified_at timestamptz,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_storage_configurations_status
    on storage_configurations(status);
create index idx_storage_configurations_adapter
    on storage_configurations(adapter);
create index idx_storage_configurations_owner_user
    on storage_configurations(owner_user_id);

create table environments (
    code text primary key,
    name text not null,
    status text not null check (status in ('healthy', 'gray')),
    tone text not null check (tone in ('amber', 'blue', 'emerald', 'rose', 'sky')),
    description text not null,
    storage_configuration_id text not null references storage_configurations(id) on delete restrict,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_environments_storage_configuration
    on environments(storage_configuration_id);

create table applications (
    id text primary key,
    slug text not null unique,
    name text not null,
    status text not null check (status in ('running', 'maintenance', 'offline')),
    platform text not null,
    owner_user_id text not null references users(id) on delete restrict,
    description text not null,
    module_count integer not null default 0 check (module_count >= 0),
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_applications_status on applications(status);
create index idx_applications_owner_user
    on applications(owner_user_id);

create table application_environment_bindings (
    application_id text not null references applications(id) on delete cascade,
    environment_code text not null references environments(code) on delete restrict,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    primary key (application_id, environment_code)
);

create index idx_application_environment_bindings_environment
    on application_environment_bindings(environment_code);

create table application_members (
    id bigserial primary key,
    application_id text not null references applications(id) on delete cascade,
    member_key text not null,
    name text not null,
    role text not null check (role in ('Owner', 'Developer', 'QA', 'Release')),
    team text not null,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (application_id, member_key)
);

create index idx_application_members_application
    on application_members(application_id);

create table feishu_users (
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

create index idx_feishu_users_open_id on feishu_users(open_id);
create index idx_feishu_users_user_id on feishu_users(user_id);
create index idx_feishu_users_tenant_key on feishu_users(tenant_key);

create table feishu_auth_sessions (
    session_token_hash text primary key,
    union_id text not null references feishu_users(union_id) on delete cascade,
    access_token text not null,
    access_token_expires_at timestamptz not null,
    refresh_token text not null default '',
    refresh_token_expires_at timestamptz,
    token_type text not null default '',
    scope text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    last_seen_at timestamptz not null default now()
);

create index idx_feishu_auth_sessions_union_id
    on feishu_auth_sessions(union_id);
create index idx_feishu_auth_sessions_refresh_expiry
    on feishu_auth_sessions(refresh_token_expires_at);
