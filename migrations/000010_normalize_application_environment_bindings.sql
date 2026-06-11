alter table application_environment_bindings
    drop column if exists deployment_policy,
    drop column if exists current_combination,
    drop column if exists bundle_count,
    drop column if exists traffic_label,
    drop column if exists status,
    drop column if exists updated_at;
