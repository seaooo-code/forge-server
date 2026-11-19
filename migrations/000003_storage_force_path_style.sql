-- 对象存储的寻址方式：false 把 Bucket 拼进域名（bucket.endpoint，AWS 默认），
-- true 把 Bucket 放在路径里（endpoint/bucket）。FDS 内网域名、MinIO 等没有 Bucket 子域名的服务需要打开。
-- 已有配置默认 false，行为不变。
alter table storage_configurations
    add column if not exists force_path_style boolean not null default false;
