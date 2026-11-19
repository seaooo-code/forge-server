-- 新增构建方式 gitlab_ci：worker 在代码源上触发 pipeline，取回指定 job 的 artifacts。
-- 已经执行过 000001 的数据库通过本文件升级，所有语句可重复执行。

-- gitlab_ci 模式：产出构建产物的 job 名，以及 job artifacts 中产物根目录的相对路径。其他模式为空串。
alter table modules
    add column if not exists ci_job text not null default '',
    add column if not exists ci_artifact_path text not null default '';

alter table modules drop constraint if exists modules_build_mode_check;
alter table modules add constraint modules_build_mode_check
    check (build_mode in ('dockerfile', 'adapter', 'gitlab_ci'));

alter table modules drop constraint if exists modules_build_adapter_check;
alter table modules add constraint modules_build_adapter_check check (
    (build_mode in ('dockerfile', 'gitlab_ci') and build_adapter_id is null)
    or (build_mode = 'adapter' and build_adapter_id is not null)
);

alter table modules drop constraint if exists modules_gitlab_ci_check;
alter table modules add constraint modules_gitlab_ci_check check (
    (build_mode = 'gitlab_ci' and ci_job <> '' and ci_artifact_path <> '')
    or (build_mode <> 'gitlab_ci' and ci_job = '' and ci_artifact_path = '')
);

-- gitlab_ci 模式：同一仓库可以用不同的 job 绑定多个模块。
create unique index if not exists modules_repository_gitlab_ci_unique
    on modules(code_source_connection_id, external_repository_id, ci_job)
    where build_mode = 'gitlab_ci';

-- gitlab_ci 构建在代码源上创建的 pipeline。创建后立即写回，worker 被接管后据此续接而不重复触发。
alter table build_records
    add column if not exists ci_pipeline_id bigint,
    add column if not exists ci_pipeline_url text not null default '';
