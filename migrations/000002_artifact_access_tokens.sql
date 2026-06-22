create table if not exists artifact_access_tokens (
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

create index if not exists idx_artifact_access_tokens_status
    on artifact_access_tokens(status);
create index if not exists idx_artifact_access_tokens_created_by
    on artifact_access_tokens(created_by_user_id);
create index if not exists idx_artifact_access_tokens_expires_at
    on artifact_access_tokens(expires_at)
    where expires_at is not null;
