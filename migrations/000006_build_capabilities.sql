-- 构建产物在 forge.json 里声明的能力等级（provides / requires），发布和拉包时据此校验 Common 与 Feature 模块是否兼容。
-- 只加带默认值的列，旧版本进程写入构建记录时不受影响。
alter table build_records add column if not exists provided_capabilities jsonb not null default '{}'::jsonb;
alter table build_records add column if not exists required_capabilities jsonb not null default '{}'::jsonb;
