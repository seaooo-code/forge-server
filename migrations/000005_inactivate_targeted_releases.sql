-- 删除按 identifier 定向灰度的第一步：提测单取代了定向灰度，把仍在生效的定向发布和 identifier 路由标为失效。
-- 列和约束保留，下一次发布再删除（见 AGENTS.md「删除列要拆成两次发布」）。可重复执行。
with inactivated_routes as (
    update release_routes
    set status = 'inactive', updated_at = now()
    where route_type = 'identifier'
        and status = 'active'
    returning release_id
)
update release_records rr
set status = 'inactive', updated_at = now()
where rr.status = 'active'
    and (
        rr.id in (select release_id from inactivated_routes)
        or (rr.release_type = 'gray' and rr.gray_strategy = 'targeted')
    );
