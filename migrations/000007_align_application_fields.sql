alter table applications
    add column if not exists owner_department_ids text[] not null default '{}';

alter table applications
    drop column if exists owner_team,
    drop column if exists repository_group,
    drop column if exists default_branch,
    drop column if exists bundle_namespace,
    drop column if exists release_policy;
