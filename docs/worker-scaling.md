# worker 扩缩容

worker 从 PostgreSQL 里的 `build_records` 表领取构建，多个 worker 之间不需要额外的协调服务。扩容就是多起 worker 进程或提高单进程并发，缩容就是给 worker 发 SIGTERM。本文说明保证扩缩容安全的机制、配置和指标。

## 1. 领取与租约

- 领取：`select … where status = 'pending' or (status = 'running' and lease_expires_at < now()) order by queued_at for update skip locked limit 1`。多个 worker 同时领取时不会拿到同一个构建。
- 租约：领取时把 `worker_id` 写成持有者 ID，把 `lease_expires_at` 设为 `now() + FORGE_WORKER_LEASE_DURATION`（默认 2 分钟）。执行期间每 1/3 租约时长续租一次。
- 接管：worker 宕机或断网后不再续租，租约到期的构建会被其他 worker 当作待领取构建重新执行，最长等待一个租约时长。
- 续租时数据库暂时出错只记警告，下一次心跳再试；租约还没过期，构建不受影响。续租发现构建已不归自己所有（已被接管或已结束）时，立即取消正在执行的构建，结果不写回。

## 2. 归属校验

写回结果（成功、失败）和放回队列都带 `where worker_id = <持有者> and status = 'running'`。如果原持有者的租约已过期、构建已被别的 worker 接管，原持有者的写入会被拒绝（`ErrBuildLeaseLost`），只记一条警告，不会覆盖新一次执行的结果。

- `FORGE_WORKER_CONCURRENCY > 1` 时，每个执行槽位的持有者 ID 是 `{FORGE_WORKER_ID}#{序号}`，所以同一个进程里另一个槽位接管了过期租约时，校验同样生效。`FORGE_WORKER_ID` 不能包含 `#`。
- 多个 worker 进程**不能使用相同的 `FORGE_WORKER_ID`**，否则归属校验无法区分它们。留空时取 `forge-worker-{hostname}`，同一台机器起多个进程时必须显式设置。
- 被接管的构建会从头重新执行：构建日志追加在同一条构建记录下，产物写到同一个对象路径（按构建记录 ID 区分），后一次的结果为准。

## 3. 退出与排空

1. 收到第一次 SIGTERM 或 SIGINT：停止领取新构建，等待进行中的构建完成并写回结果，最多等 `FORGE_WORKER_SHUTDOWN_GRACE`（默认 10 分钟）。
2. 宽限期到了还没完成：中断这些构建，**放回队列**（`status = 'pending'`，`queued_at` 不变，排在队首），由其他 worker 重新执行，不标记失败。
3. 排空期间再收到一次信号：立即退出。没来得及放回队列的构建在租约过期后被其他 worker 接管。

worker 启动的 git、docker 子进程运行在独立的进程组里，所以终端 Ctrl-C 或按进程组发送的信号只到达 worker，不会直接中止进行中的构建。用 systemd 托管时要设置 `KillMode=mixed`：默认的 `control-group` 会把 SIGTERM 发给 cgroup 里的所有进程，构建会被立即中止。

部署平台的终止宽限期（例如容器的 terminationGracePeriodSeconds）应**大于** `FORGE_WORKER_SHUTDOWN_GRACE`，否则平台会在 worker 放回队列之前强制杀掉进程；这种情况下构建要等租约过期才会被接管。

构建超时（`FORGE_WORKER_BUILD_TIMEOUT`）与退出无关：超时的构建标记为失败。

## 4. 并发

- `FORGE_WORKER_CONCURRENCY`（默认 1，最大 64）是单个进程同时执行的构建数。每个槽位独立领取，执行完一个构建后立即尝试领取下一个，队列为空时按 `FORGE_WORKER_POLL_INTERVAL` 轮询。
- 同一台机器上的槽位共享 Docker daemon、BuildKit 缓存和磁盘。构建没有单独的 CPU 和内存限制，并发数要按单个构建的峰值占用估算。例如 YRN 适配器给 Node 设置了 `--max-old-space-size=8192`，单个构建最多可能用到约 8 GB 内存。
- 一个进程跑多个槽位和多个进程各跑一个槽位效果相同；前者部署简单，后者故障隔离更好。

## 5. 指标

`GET /metrics`（API 进程）以 Prometheus 文本格式输出构建队列的即时状态：

| 指标 | 说明 |
| --- | --- |
| `forge_build_queue_waiting` | 等待领取的构建数，包括租约已过期、等待接管的构建 |
| `forge_build_queue_oldest_wait_seconds` | 等待最久的构建已排队的秒数，队列为空时为 0 |
| `forge_build_running` | 租约有效、正在执行的构建数 |
| `forge_build_workers_active` | 持有有效租约的 worker 进程数（按 `#` 前的 worker ID 去重），空闲的 worker 不计入 |

- 配置了 `FORGE_METRICS_TOKEN` 时要求 `Authorization: Bearer <令牌>`；没配置时不鉴权，与 `/healthz` 一样只应在内网暴露。指标只包含计数，不含模块或分支信息。
- 扩缩容建议：`forge_build_queue_oldest_wait_seconds` 持续超过可接受的排队时长（例如 5 分钟）时扩容；`forge_build_queue_waiting` 为 0 且 `forge_build_running` 远小于总槽位数时缩容。缩容依赖第 3 节的排空，不会让构建失败。

## 6. 尚未覆盖

- 自动伸缩本身：需要部署平台按上面的指标增减 worker 副本。公司容器平台是否支持按自定义指标伸缩、是否允许 worker 访问 Docker，尚未确认。
- 公平调度：队列是全局先进先出，某个应用集中提交时其他应用要排队。
- 资源隔离：构建之间不限制 CPU 和内存。
- 失败重试上限：导致 worker 崩溃的构建会在租约过期后被反复接管执行，目前没有次数上限。
