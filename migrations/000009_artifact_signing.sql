-- 包签名：每个应用一对 Ed25519 密钥，worker 构建结束时签名产物文件列表，manifest 下发签名，见 docs/artifact-signing.md。
-- 所有语句可重复执行。只新增表和列，旧版本进程不读写它们。

create table if not exists application_signing_keys (
    id uuid primary key,
    application_id text not null references applications(id) on delete cascade,
    -- 公钥 sha256 的前 16 个十六进制字符，客户端据此挑选公钥。
    key_id text not null unique check (key_id ~ '^[0-9a-f]{16}$'),
    algorithm text not null check (algorithm = 'ed25519'),
    -- base64 编码的原始 32 字节公钥。
    public_key text not null,
    -- secretbox 加密的私钥种子，任何接口都不返回。
    private_key_ciphertext text not null,
    status text not null check (status in ('active', 'retired')),
    created_by_user_id text references users(id) on delete set null,
    created_at timestamptz not null default now(),
    retired_at timestamptz
);

-- 一个应用同一时间最多一把 active 密钥。
create unique index if not exists idx_application_signing_keys_active
    on application_signing_keys(application_id)
    where status = 'active';

-- 打开后，创建发布和回滚拒绝没有签名的构建。
alter table applications add column if not exists require_signed_releases boolean not null default false;

-- 构建的签名：签名所用密钥、被签名的 JSON 原文和 base64 签名，未签名时都为空字符串。
alter table build_records add column if not exists signing_key_id text not null default '';
alter table build_records add column if not exists signed_payload text not null default '';
alter table build_records add column if not exists signature text not null default '';

create index if not exists idx_build_records_signing_key
    on build_records(signing_key_id)
    where signing_key_id <> '';
