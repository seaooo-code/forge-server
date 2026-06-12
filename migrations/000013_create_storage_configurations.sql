create table if not exists storage_configurations (
    id text primary key,
    name text not null,
    adapter text not null check (adapter in ('S3')),
    bucket text not null,
    endpoint text not null,
    region text not null default '',
    owner text not null default '',
    public_base_url text not null default '',
    access_key_id text not null,
    secret_access_key text not null,
    status text not null check (status in ('healthy', 'error', 'disabled')),
    last_verified_at timestamptz,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index if not exists idx_storage_configurations_status
    on storage_configurations(status);

create index if not exists idx_storage_configurations_adapter
    on storage_configurations(adapter);
