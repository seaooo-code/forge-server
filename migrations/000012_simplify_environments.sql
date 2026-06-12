drop table if exists environment_timeline_events;
drop table if exists environment_guardrails;

alter table environments
    drop column if exists current_combination,
    drop column if exists endpoint,
    drop column if exists cdn_domain,
    drop column if exists branch_policy,
    drop column if exists release_policy,
    drop column if exists current_release,
    drop column if exists bundle_count,
    drop column if exists total_size,
    drop column if exists health_score;
