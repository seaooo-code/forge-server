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

create table code_source_app_configs (
    provider text primary key check (provider in ('github')),
    provider_app_id text not null,
    app_slug text not null,
    name text not null default '',
    html_url text not null default '',
    private_key text not null,
    webhook_secret text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table code_source_installations (
    id bigserial primary key,
    provider text not null check (provider in ('github')),
    owner_union_id text not null,
    provider_installation_id bigint not null,
    account_login text not null default '',
    account_type text not null default '',
    repository_selection text not null default '',
    status text not null check (status in ('active', 'deleted')),
    installed_at timestamptz not null default now(),
    deleted_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (provider, owner_union_id, provider_installation_id)
);

create index idx_code_source_installations_provider_owner_status
    on code_source_installations(provider, owner_union_id, status);

create table code_source_repositories (
    provider text not null check (provider in ('github')),
    owner_union_id text not null,
    provider_repository_id bigint not null,
    provider_installation_id bigint not null,
    owner text not null,
    name text not null,
    full_name text not null,
    private boolean not null default false,
    html_url text not null default '',
    clone_url text not null default '',
    ssh_url text not null default '',
    default_branch text not null default '',
    description text not null default '',
    repository_updated_at timestamptz,
    last_synced_at timestamptz not null default now(),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (provider, owner_union_id, provider_repository_id),
    foreign key (provider, owner_union_id, provider_installation_id)
        references code_source_installations(provider, owner_union_id, provider_installation_id)
        on delete cascade
);

create index idx_code_source_repositories_installation
    on code_source_repositories(provider, owner_union_id, provider_installation_id);

create index idx_code_source_repositories_full_name
    on code_source_repositories(provider, owner_union_id, full_name);

create table modules (
    id text primary key,
    name text not null,
    kind text not null check (kind in ('Common', 'Feature')),
    common_module_id text references modules(id) on delete restrict,
    application_id text not null references applications(id) on delete restrict,
    owner_user_id text not null references users(id) on delete restrict,
    repository_provider text not null check (repository_provider in ('github')),
    provider_repository_id bigint not null,
    provider_installation_id bigint not null default 0,
    repository_full_name text not null,
    repository_html_url text not null default '',
    repository_clone_url text not null default '',
    repository_ssh_url text not null default '',
    default_branch text not null default '',
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (
        (kind = 'Common' and common_module_id is null)
        or (kind = 'Feature' and common_module_id is not null and common_module_id <> id)
    ),
    unique (repository_provider, provider_repository_id)
);

create index idx_modules_application
    on modules(application_id);

create unique index idx_modules_common_application_unique
    on modules(application_id)
    where kind = 'Common';

create index idx_modules_common_module
    on modules(common_module_id);

create index idx_modules_owner_user
    on modules(owner_user_id);

create index idx_modules_kind
    on modules(kind);

create index idx_modules_repository_full_name
    on modules(repository_provider, repository_full_name);

create index idx_modules_repository_installation
    on modules(repository_provider, provider_installation_id);

create function ensure_module_common_binding()
returns trigger as $$
begin
    if new.kind = 'Feature' then
        if not exists (
            select 1
            from modules common_module
            where common_module.id = new.common_module_id
              and common_module.kind = 'Common'
              and common_module.application_id = new.application_id
        ) then
            raise exception 'feature module common_module_id must point to a Common module of the same application'
                using errcode = '23514';
        end if;
    end if;

    if exists (
        select 1
        from modules feature_module
        where feature_module.common_module_id = new.id
          and (new.kind <> 'Common' or feature_module.application_id <> new.application_id)
    ) then
        raise exception 'module with feature dependents must remain a Common module of the same application'
            using errcode = '23514';
    end if;

    return new;
end;
$$ language plpgsql;

create trigger trg_modules_common_binding
before insert or update of kind, application_id, common_module_id
on modules
for each row execute function ensure_module_common_binding();
