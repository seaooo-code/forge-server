create table if not exists applications (
    id text primary key,
    slug text not null unique,
    name text not null,
    status text not null check (status in ('running', 'maintenance', 'offline')),
    platform text not null,
    owner text not null,
    owner_email text not null default '',
    owner_avatar_url text not null default '',
    owner_department_ids text[] not null default '{}',
    description text not null,
    module_count integer not null default 0 check (module_count >= 0),
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists application_environment_bindings (
    application_id text not null references applications(id) on delete cascade,
    environment_code text not null references environments(code) on delete restrict,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    primary key (application_id, environment_code)
);

create table if not exists application_members (
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

create index if not exists idx_applications_status on applications(status);
create index if not exists idx_application_environment_bindings_environment
    on application_environment_bindings(environment_code);
create index if not exists idx_application_members_application
    on application_members(application_id);
