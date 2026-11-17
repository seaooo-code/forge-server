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

create table global_role_assignments (
    user_id text not null references users(id) on delete cascade,
    role text not null check (role in ('SuperAdmin', 'PlatformAdmin', 'ReleaseAdmin', 'Auditor')),
    created_by_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now(),
    primary key (user_id, role)
);

create index idx_global_role_assignments_role
    on global_role_assignments(role);

create table artifact_access_tokens (
    id text primary key,
    name text not null,
    token_hash text not null unique,
    token_preview text not null,
    status text not null check (status in ('active', 'disabled')),
    created_by_user_id text not null references users(id) on delete restrict,
    expires_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_artifact_access_tokens_status
    on artifact_access_tokens(status);
create index idx_artifact_access_tokens_created_by
    on artifact_access_tokens(created_by_user_id);
create index idx_artifact_access_tokens_expires_at
    on artifact_access_tokens(expires_at)
    where expires_at is not null;

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
    user_id text not null references users(id) on delete cascade,
    role text not null check (role in ('Owner', 'Developer', 'QA', 'Release')),
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (application_id, user_id)
);

create index idx_application_members_application
    on application_members(application_id);
create index idx_application_members_user
    on application_members(user_id);

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

create table code_source_connections (
    id text primary key,
    provider text not null check (provider in ('github', 'gitlab')),
    name text not null,
    web_base_url text not null,
    api_base_url text not null,
    external_account text not null default '',
    external_id text not null default '',
    credential_encrypted text not null default '',
    credential_preview text not null default '',
    credential_expires_at timestamptz,
    webhook_secret_encrypted text not null default '',
    status text not null check (status in ('active', 'disabled', 'deleted')),
    created_by_user_id text not null references users(id) on delete restrict,
    last_verified_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index idx_code_source_connections_external
    on code_source_connections(provider, api_base_url, external_id)
    where external_id <> '' and status <> 'deleted';

create index idx_code_source_connections_provider_status
    on code_source_connections(provider, status);

create index idx_code_source_connections_created_by
    on code_source_connections(created_by_user_id);

create table code_source_repositories (
    connection_id text not null references code_source_connections(id) on delete cascade,
    external_repository_id text not null,
    full_name text not null,
    web_url text not null default '',
    clone_url text not null default '',
    default_branch text not null default '',
    private boolean not null default false,
    description text not null default '',
    last_synced_at timestamptz not null default now(),
    primary key (connection_id, external_repository_id)
);

create index idx_code_source_repositories_full_name
    on code_source_repositories(connection_id, full_name);

create table build_adapters (
    id text primary key,
    name text not null,
    description text not null default '',
    dockerfile_template text not null,
    -- {相对路径: 模板内容}，渲染后放入构建上下文的 .forge/ 目录。
    files jsonb not null default '{}',
    -- [{key, label, description, required, default}]
    parameters jsonb not null default '[]',
    -- 每次修改模板、文件或参数定义时 +1，构建记录据此追溯所用版本。
    version integer not null default 1 check (version > 0),
    status text not null check (status in ('active', 'disabled')),
    created_by_user_id text not null references users(id) on delete restrict,
    updated_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_build_adapters_status
    on build_adapters(status);
create index idx_build_adapters_created_by
    on build_adapters(created_by_user_id);
create index idx_build_adapters_updated_by
    on build_adapters(updated_by_user_id);

create table modules (
    id text primary key,
    name text not null,
    kind text not null check (kind in ('Common', 'Feature')),
    common_module_id text references modules(id) on delete restrict,
    application_id text not null references applications(id) on delete restrict,
    owner_user_id text not null references users(id) on delete restrict,
    code_source_connection_id text not null references code_source_connections(id) on delete restrict,
    external_repository_id text not null,
    repository_full_name text not null,
    repository_web_url text not null default '',
    repository_clone_url text not null default '',
    default_branch text not null default '',
    dockerfile_path text not null default 'Dockerfile',
    build_context text not null default '.',
    build_mode text not null default 'dockerfile' check (build_mode in ('dockerfile', 'adapter')),
    build_adapter_id text references build_adapters(id) on delete restrict,
    build_adapter_params jsonb not null default '{}',
    auto_build_branches text[] not null default '{}',
    auto_build_environment_code text references environments(code) on delete set null,
    auto_preview_release boolean not null default false,
    auto_preview_ttl_hours integer not null default 168 check (auto_preview_ttl_hours > 0),
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (
        (kind = 'Common' and common_module_id is null)
        or (kind = 'Feature' and common_module_id is not null and common_module_id <> id)
    ),
    constraint modules_build_adapter_check check (
        (build_mode = 'dockerfile' and build_adapter_id is null)
        or (build_mode = 'adapter' and build_adapter_id is not null)
    )
);

create index idx_modules_application
    on modules(application_id);

-- 只约束 dockerfile 模式：adapter 模式下同一仓库可以用不同适配器参数绑定多个模块。
create unique index modules_repository_dockerfile_unique
    on modules(code_source_connection_id, external_repository_id, dockerfile_path)
    where build_mode = 'dockerfile';

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
    on modules(repository_full_name);

create index idx_modules_build_adapter
    on modules(build_adapter_id)
    where build_adapter_id is not null;

create index idx_modules_auto_build_environment
    on modules(auto_build_environment_code)
    where auto_build_environment_code is not null;

create table module_build_variables (
    module_id text not null references modules(id) on delete cascade,
    key text not null check (key ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    value_encrypted text not null,
    is_secret boolean not null default false,
    environment_code text references environments(code) on delete cascade,
    updated_by_user_id text not null references users(id) on delete restrict,
    updated_at timestamptz not null default now(),
    unique nulls not distinct (module_id, key, environment_code)
);

create index idx_module_build_variables_environment
    on module_build_variables(environment_code)
    where environment_code is not null;

create index idx_module_build_variables_updated_by
    on module_build_variables(updated_by_user_id);

create table build_records (
    id uuid primary key,
    module_id text not null references modules(id) on delete restrict,
    -- 模块内单调递增的构建号，创建构建记录时在事务内按模块加锁分配。
    number bigint not null check (number > 0),
    environment_code text not null references environments(code) on delete restrict,
    storage_configuration_id text not null references storage_configurations(id) on delete restrict,
    branch text not null default '',
    commit_sha text not null default '',
    status text not null check (status in ('pending', 'running', 'succeeded', 'failed')),
    trigger_type text not null default 'manual' check (trigger_type in ('manual', 'webhook')),
    trigger_id text not null default '',
    -- 手动构建记录当前登录用户；webhook 构建记录代码源连接的创建人。历史数据为空。
    created_by_user_id text references users(id) on delete set null,
    runtime_version text not null default '',
    platforms text[] not null default '{}',
    -- 适配器构建记录所用适配器、版本与渲染后 Dockerfile 的 sha256；Dockerfile 构建为空。
    build_adapter_id text,
    build_adapter_version integer,
    rendered_dockerfile_sha256 text not null default '',
    -- 构建时注入到构建上下文的依赖构建：[{moduleId, runtimeVersion, buildRecordId}]。
    -- Feature 模块注入所属 Common 模块在当前环境下生效中的默认路由产物，其他构建为空数组。
    dependency_builds jsonb not null default '[]',
    worker_id text not null default '',
    lease_expires_at timestamptz,
    artifact_key text not null default '',
    artifact_url text not null default '',
    manifest_key text not null default '',
    file_count integer not null default 0 check (file_count >= 0),
    total_size_bytes bigint not null default 0 check (total_size_bytes >= 0),
    error_message text not null default '',
    queued_at timestamptz not null default now(),
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (status <> 'succeeded' or (commit_sha <> '' and runtime_version <> '' and cardinality(platforms) > 0)),
    constraint build_records_module_number_unique unique (module_id, number)
);

create index idx_build_records_status_queue
    on build_records(status, queued_at);

create index idx_build_records_module
    on build_records(module_id);

create index idx_build_records_environment
    on build_records(environment_code);

create index idx_build_records_storage_configuration
    on build_records(storage_configuration_id);

create index idx_build_records_trigger
    on build_records(trigger_type, trigger_id);

create index idx_build_records_commit
    on build_records(module_id, environment_code, commit_sha);

create index idx_build_records_created_by_user
    on build_records(created_by_user_id);

create table build_log_chunks (
    build_record_id uuid not null references build_records(id) on delete cascade,
    seq integer not null,
    content text not null,
    created_at timestamptz not null default now(),
    primary key (build_record_id, seq)
);

create index idx_build_log_chunks_created_at
    on build_log_chunks(created_at);

create table release_records (
    id uuid primary key,
    module_id text not null references modules(id) on delete restrict,
    environment_code text not null references environments(code) on delete restrict,
    build_record_id uuid not null references build_records(id) on delete restrict,
    release_type text not null check (release_type in ('full', 'gray')),
    gray_strategy text not null default '',
    gray_percentage integer not null default 0,
    expires_at timestamptz,
    status text not null check (status in ('active', 'inactive')),
    artifact_key text not null,
    artifact_url text not null default '',
    description text not null default '',
    created_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (
        (release_type = 'full' and gray_strategy = '' and gray_percentage = 0 and expires_at is null)
        or (release_type = 'gray' and gray_strategy = 'targeted' and gray_percentage = 0 and expires_at is not null)
        or (release_type = 'gray' and gray_strategy = 'percentage' and gray_percentage between 0 and 99 and expires_at is null)
    )
);

create index idx_release_records_module_environment
    on release_records(module_id, environment_code, created_at desc);

create index idx_release_records_build
    on release_records(build_record_id);

create index idx_release_records_created_by
    on release_records(created_by_user_id);

create index idx_release_records_targeted_expiry
    on release_records(expires_at)
    where release_type = 'gray' and gray_strategy = 'targeted' and status = 'active';

create table release_routes (
    id text primary key,
    module_id text not null references modules(id) on delete restrict,
    environment_code text not null references environments(code) on delete restrict,
    release_id uuid not null references release_records(id) on delete restrict,
    runtime_version text not null,
    route_type text not null check (route_type in ('default', 'identifier', 'percentage')),
    identifier text not null default '',
    status text not null check (status in ('active', 'inactive')),
    created_by_user_id text not null references users(id) on delete restrict,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (
        (route_type = 'default' and identifier = '')
        or (route_type = 'identifier' and identifier <> '')
        or (route_type = 'percentage' and identifier = '')
    )
);

create unique index idx_release_routes_default_active
    on release_routes(module_id, environment_code, runtime_version)
    where route_type = 'default' and status = 'active';

create unique index idx_release_routes_identifier_active
    on release_routes(module_id, environment_code, runtime_version, identifier)
    where route_type = 'identifier' and status = 'active';

create unique index idx_release_routes_percentage_active
    on release_routes(module_id, environment_code, runtime_version)
    where route_type = 'percentage' and status = 'active';

create index idx_release_routes_release
    on release_routes(release_id);

create index idx_release_routes_created_by
    on release_routes(created_by_user_id);

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
