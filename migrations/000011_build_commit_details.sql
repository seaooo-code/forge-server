-- 构建使用的提交详情：构建成功时由 worker 按 commit SHA 向代码源查询并写入，查询失败时保持为空。
-- 所有语句可重复执行。只新增带默认值的列，旧版本进程不读写它们。
alter table build_records add column if not exists commit_title text not null default '';
alter table build_records add column if not exists commit_author_name text not null default '';
alter table build_records add column if not exists committed_at timestamptz;
