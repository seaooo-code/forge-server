-- 租户隔离与用户身份，设计见 docs/tenants.md。
-- 一次性结构转换：只在旧表 feishu_tenants 存在时执行。旧版本进程不能和新结构共存，部署前先停掉所有旧实例。
do $$
declare
    default_tenant text;
    fk record;
begin
    if to_regclass('public.feishu_tenants') is null then
        return;
    end if;

    -- 1. 租户与飞书登录配置
    create table tenants (
        id text primary key,
        name text not null,
        status text not null check (status in ('active', 'disabled')),
        sort_order integer not null default 0,
        created_at timestamptz not null default now(),
        updated_at timestamptz not null default now()
    );

    -- 每个租户一个飞书自建应用。app_secret 为 secretbox 密文。
    create table tenant_feishu_apps (
        tenant_id text primary key references tenants(id) on delete cascade,
        app_id text not null unique,
        app_secret text not null,
        created_at timestamptz not null default now(),
        updated_at timestamptz not null default now()
    );

    insert into tenants (id, name, status, sort_order, created_at, updated_at)
    select id, name, status, sort_order, created_at, updated_at from feishu_tenants;
    insert into tenant_feishu_apps (tenant_id, app_id, app_secret, created_at, updated_at)
    select id, app_id, app_secret, created_at, updated_at from feishu_tenants;

    select id into default_tenant from tenants order by sort_order, created_at, id limit 1;
    if default_tenant is null and (
        exists (select 1 from users)
        or exists (select 1 from storage_configurations)
        or exists (select 1 from code_source_connections)
        or exists (select 1 from build_adapters)
        or exists (select 1 from artifact_access_tokens)
        or exists (select 1 from code_source_app_configs)
    ) then
        raise exception 'tenant migration: business data exists but there is no feishu tenant to own it';
    end if;

    -- 2. 用户归属租户：按最近一次会话的租户，没有会话的归默认租户。
    alter table users add column tenant_id text references tenants(id) on delete restrict;
    update users u
    set tenant_id = coalesce((
        select s.feishu_tenant_id
        from feishu_auth_sessions s
        where u.feishu_union_id <> '' and s.union_id = u.feishu_union_id
        order by s.last_seen_at desc
        limit 1
    ), default_tenant);
    alter table users alter column tenant_id set not null;
    alter table users add constraint users_tenant_id_id_key unique (tenant_id, id);

    -- 3. 重新生成用户 ID，改写所有引用用户的外键列。
    create temp table user_id_map on commit drop as
    select id as old_id, 'user-' || substr(md5(random()::text || clock_timestamp()::text || id), 1, 16) as new_id
    from users;

    create temp table user_fk_defs on commit drop as
    select
        c.conrelid::regclass::text as table_name,
        c.conname,
        pg_get_constraintdef(c.oid) as definition,
        a.attname as column_name
    from pg_constraint c
    join pg_attribute a on a.attrelid = c.conrelid and a.attnum = c.conkey[1]
    where c.contype = 'f' and c.confrelid = 'users'::regclass;

    for fk in select * from user_fk_defs loop
        execute format('alter table %s drop constraint %I', fk.table_name, fk.conname);
        execute format(
            'update %s t set %I = m.new_id from user_id_map m where t.%I = m.old_id',
            fk.table_name, fk.column_name, fk.column_name
        );
    end loop;
    update users u set id = m.new_id from user_id_map m where u.id = m.old_id;
    for fk in select * from user_fk_defs loop
        execute format('alter table %s add constraint %I %s', fk.table_name, fk.conname, fk.definition);
    end loop;

    -- 4. 用户身份：飞书身份的 issuer 是飞书 App ID，subject 是 open_id。
    create table user_identities (
        id text primary key,
        user_id text not null references users(id) on delete cascade,
        provider text not null check (provider in ('feishu')),
        issuer text not null,
        subject text not null,
        -- 登录方式自己的资料，例如飞书的 unionId、userId、enName。
        profile jsonb not null default '{}',
        created_at timestamptz not null default now(),
        updated_at timestamptz not null default now(),
        last_login_at timestamptz,
        unique (provider, issuer, subject)
    );
    create index idx_user_identities_user on user_identities(user_id);

    insert into user_identities (id, user_id, provider, issuer, subject, profile, last_login_at)
    select
        'identity-' || substr(md5(random()::text || clock_timestamp()::text || u.id), 1, 16),
        u.id,
        'feishu',
        a.app_id,
        coalesce(nullif(u.feishu_open_id, ''), m.old_id),
        jsonb_strip_nulls(jsonb_build_object(
            'unionId', nullif(u.feishu_union_id, ''),
            'userId', coalesce(nullif(u.feishu_user_id, ''), nullif(f.user_id, '')),
            'enName', nullif(f.en_name, ''),
            'employeeNo', nullif(f.employee_no, ''),
            'tenantKey', nullif(f.tenant_key, '')
        )),
        f.last_login_at
    from users u
    join user_id_map m on m.new_id = u.id
    join tenant_feishu_apps a on a.tenant_id = u.tenant_id
    left join feishu_users f on u.feishu_union_id <> '' and f.union_id = u.feishu_union_id;

    alter table users drop column feishu_union_id;
    alter table users drop column feishu_open_id;
    alter table users drop column feishu_user_id;
    alter table users drop column department_ids;

    -- 5. 会话：所有旧会话作废，用户重新登录。
    drop table feishu_auth_sessions;
    drop table feishu_users;
    drop table feishu_tenants;

    create table auth_sessions (
        session_token_hash text primary key,
        user_id text not null references users(id) on delete cascade,
        identity_id text not null references user_identities(id) on delete cascade,
        -- 会话当前所在的租户。普通用户恒等于所属租户，SuperAdmin 可以切换。
        active_tenant_id text not null references tenants(id) on delete cascade,
        -- 登录方式签发的 token，飞书登录用它搜索通讯录、刷新会话。
        provider_access_token text not null default '',
        provider_access_token_expires_at timestamptz not null,
        provider_refresh_token text not null default '',
        provider_refresh_token_expires_at timestamptz,
        provider_token_type text not null default '',
        provider_scope text not null default '',
        created_at timestamptz not null default now(),
        updated_at timestamptz not null default now(),
        last_seen_at timestamptz not null default now()
    );
    create index idx_auth_sessions_user on auth_sessions(user_id);
    create index idx_auth_sessions_active_tenant on auth_sessions(active_tenant_id);

    -- 6. 角色：SuperAdmin 是平台级，其余角色只在用户所属租户内生效。
    alter table global_role_assignments rename to user_roles;
    alter table user_roles drop constraint global_role_assignments_role_check;
    update user_roles set role = 'TenantAdmin' where role = 'PlatformAdmin';
    alter table user_roles add constraint user_roles_role_check
        check (role in ('SuperAdmin', 'TenantAdmin', 'ReleaseAdmin', 'Auditor'));
    alter table user_roles rename constraint global_role_assignments_pkey to user_roles_pkey;
    alter table user_roles rename constraint global_role_assignments_user_id_fkey to user_roles_user_id_fkey;
    alter table user_roles rename constraint global_role_assignments_created_by_user_id_fkey to user_roles_created_by_user_id_fkey;
    alter index idx_global_role_assignments_role rename to idx_user_roles_role;

    -- 7. 根表归属租户
    alter table storage_configurations add column tenant_id text references tenants(id) on delete restrict;
    update storage_configurations set tenant_id = default_tenant;
    alter table storage_configurations alter column tenant_id set not null;
    alter table storage_configurations add constraint storage_configurations_tenant_id_id_key unique (tenant_id, id);
    alter table storage_configurations drop constraint storage_configurations_owner_user_id_fkey;
    alter table storage_configurations add constraint storage_configurations_owner_user_id_fkey
        foreign key (tenant_id, owner_user_id) references users(tenant_id, id) on delete restrict;

    alter table applications add column tenant_id text references tenants(id) on delete restrict;
    update applications set tenant_id = default_tenant;
    alter table applications alter column tenant_id set not null;
    alter table applications add constraint applications_tenant_id_id_key unique (tenant_id, id);
    alter table applications drop constraint applications_owner_user_id_fkey;
    alter table applications add constraint applications_owner_user_id_fkey
        foreign key (tenant_id, owner_user_id) references users(tenant_id, id) on delete restrict;

    alter table code_source_connections add column tenant_id text references tenants(id) on delete restrict;
    update code_source_connections set tenant_id = default_tenant;
    alter table code_source_connections alter column tenant_id set not null;
    alter table code_source_connections add constraint code_source_connections_tenant_id_id_key unique (tenant_id, id);
    drop index idx_code_source_connections_external;
    create unique index idx_code_source_connections_external
        on code_source_connections(tenant_id, provider, api_base_url, external_id)
        where external_id <> '' and status <> 'deleted';

    alter table code_source_app_configs add column tenant_id text references tenants(id) on delete cascade;
    update code_source_app_configs set tenant_id = default_tenant;
    alter table code_source_app_configs alter column tenant_id set not null;
    alter table code_source_app_configs drop constraint code_source_app_configs_pkey;
    alter table code_source_app_configs add primary key (tenant_id, provider);

    alter table build_adapters add column tenant_id text references tenants(id) on delete restrict;
    update build_adapters set tenant_id = default_tenant;
    alter table build_adapters alter column tenant_id set not null;
    alter table build_adapters add constraint build_adapters_tenant_id_id_key unique (tenant_id, id);

    alter table artifact_access_tokens add column tenant_id text references tenants(id) on delete restrict;
    update artifact_access_tokens set tenant_id = default_tenant;
    alter table artifact_access_tokens alter column tenant_id set not null;
    create index idx_artifact_access_tokens_tenant on artifact_access_tokens(tenant_id);

    -- 8. 环境：新主键 id，编码在租户内唯一；子表的 environment_code 换成 environment_id。
    alter table environments add column tenant_id text references tenants(id) on delete restrict;
    update environments set tenant_id = default_tenant;
    alter table environments alter column tenant_id set not null;
    alter table environments add column id text;
    update environments set id = 'env-' || substr(md5(random()::text || clock_timestamp()::text || code), 1, 16);
    alter table environments alter column id set not null;

    alter table application_environment_bindings drop constraint application_environment_bindings_environment_code_fkey;
    alter table build_records drop constraint build_records_environment_code_fkey;
    alter table module_build_variables drop constraint module_build_variables_environment_code_fkey;
    alter table modules drop constraint modules_auto_build_environment_code_fkey;
    alter table release_health_policies drop constraint release_health_policies_environment_code_fkey;
    alter table release_records drop constraint release_records_environment_code_fkey;
    alter table release_routes drop constraint release_routes_environment_code_fkey;
    alter table test_plans drop constraint test_plans_environment_code_fkey;
    alter table user_test_accounts drop constraint user_test_accounts_environment_code_fkey;

    -- 统计表没有外键，环境已删除的历史行直接丢弃。
    delete from artifact_pull_daily_stats where environment_code not in (select code from environments);
    delete from release_health_buckets where environment_code not in (select code from environments);

    update application_environment_bindings t set environment_code = e.id from environments e where t.environment_code = e.code;
    update build_records t set environment_code = e.id from environments e where t.environment_code = e.code;
    update module_build_variables t set environment_code = e.id from environments e where t.environment_code = e.code;
    update modules t set auto_build_environment_code = e.id from environments e where t.auto_build_environment_code = e.code;
    update release_health_policies t set environment_code = e.id from environments e where t.environment_code = e.code;
    update release_records t set environment_code = e.id from environments e where t.environment_code = e.code;
    update release_routes t set environment_code = e.id from environments e where t.environment_code = e.code;
    update test_plans t set environment_code = e.id from environments e where t.environment_code = e.code;
    update user_test_accounts t set environment_code = e.id from environments e where t.environment_code = e.code;
    update artifact_pull_daily_stats t set environment_code = e.id from environments e where t.environment_code = e.code;
    update release_health_buckets t set environment_code = e.id from environments e where t.environment_code = e.code;

    alter table application_environment_bindings rename column environment_code to environment_id;
    alter table build_records rename column environment_code to environment_id;
    alter table module_build_variables rename column environment_code to environment_id;
    alter table modules rename column auto_build_environment_code to auto_build_environment_id;
    alter table release_health_policies rename column environment_code to environment_id;
    alter table release_records rename column environment_code to environment_id;
    alter table release_routes rename column environment_code to environment_id;
    alter table test_plans rename column environment_code to environment_id;
    alter table user_test_accounts rename column environment_code to environment_id;
    alter table artifact_pull_daily_stats rename column environment_code to environment_id;
    alter table release_health_buckets rename column environment_code to environment_id;
    alter table module_build_variables rename constraint module_build_variables_module_id_key_environment_code_key
        to module_build_variables_module_id_key_environment_id_key;

    alter table environments drop constraint environments_pkey;
    alter table environments add primary key (id);
    alter table environments add constraint environments_tenant_id_code_key unique (tenant_id, code);
    alter table environments add constraint environments_tenant_id_id_key unique (tenant_id, id);
    alter table environments drop constraint environments_storage_configuration_id_fkey;
    alter table environments add constraint environments_storage_configuration_id_fkey
        foreign key (tenant_id, storage_configuration_id) references storage_configurations(tenant_id, id) on delete restrict;

    -- 应用和环境的绑定冗余 tenant_id，保证两者同租户。
    alter table application_environment_bindings add column tenant_id text;
    update application_environment_bindings t set tenant_id = a.tenant_id from applications a where a.id = t.application_id;
    alter table application_environment_bindings alter column tenant_id set not null;
    alter table application_environment_bindings drop constraint application_environment_bindings_application_id_fkey;
    alter table application_environment_bindings add constraint application_environment_bindings_application_id_fkey
        foreign key (tenant_id, application_id) references applications(tenant_id, id) on delete cascade;
    alter table application_environment_bindings add constraint application_environment_bindings_environment_id_fkey
        foreign key (tenant_id, environment_id) references environments(tenant_id, id) on delete restrict;

    alter table build_records add constraint build_records_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete restrict;
    alter table module_build_variables add constraint module_build_variables_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete cascade;
    alter table release_health_policies add constraint release_health_policies_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete cascade;
    alter table release_records add constraint release_records_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete restrict;
    alter table release_routes add constraint release_routes_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete restrict;
    alter table test_plans add constraint test_plans_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete restrict;
    alter table user_test_accounts add constraint user_test_accounts_environment_id_fkey
        foreign key (environment_id) references environments(id) on delete cascade;

    -- 9. 模块冗余 tenant_id，所有根表引用都限定在同一租户。
    alter table modules add column tenant_id text;
    update modules t set tenant_id = a.tenant_id from applications a where a.id = t.application_id;
    alter table modules alter column tenant_id set not null;
    create index idx_modules_tenant on modules(tenant_id);

    alter table modules drop constraint modules_application_id_fkey;
    alter table modules add constraint modules_application_id_fkey
        foreign key (tenant_id, application_id) references applications(tenant_id, id) on delete restrict;
    alter table modules drop constraint modules_owner_user_id_fkey;
    alter table modules add constraint modules_owner_user_id_fkey
        foreign key (tenant_id, owner_user_id) references users(tenant_id, id) on delete restrict;
    alter table modules drop constraint modules_code_source_connection_id_fkey;
    alter table modules add constraint modules_code_source_connection_id_fkey
        foreign key (tenant_id, code_source_connection_id) references code_source_connections(tenant_id, id) on delete restrict;
    alter table modules drop constraint modules_build_adapter_id_fkey;
    alter table modules add constraint modules_build_adapter_id_fkey
        foreign key (tenant_id, build_adapter_id) references build_adapters(tenant_id, id) on delete restrict;
    alter table modules add constraint modules_auto_build_environment_id_fkey
        foreign key (tenant_id, auto_build_environment_id) references environments(tenant_id, id)
        on delete set null (auto_build_environment_id);
end
$$;
