create table if not exists feishu_auth_sessions (
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

create index if not exists idx_feishu_auth_sessions_union_id
    on feishu_auth_sessions(union_id);

create index if not exists idx_feishu_auth_sessions_refresh_expiry
    on feishu_auth_sessions(refresh_token_expires_at);
