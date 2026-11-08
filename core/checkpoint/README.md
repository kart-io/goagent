# 检查点复制与恢复边界

复制配置使用 `constants.CheckpointReplicationModeSync` / `constants.CheckpointReplicationModeAsync`，原 `checkpoint.ReplicationMode*` 入口已删除。默认健康检查间隔由 `constants.DefaultCheckpointHealthCheckInterval` 定义（30 秒），切换次数由 `constants.DefaultCheckpointMaxFailoverAttempts` 定义（3 次）；默认采用异步复制，已有配置值和恢复协议保持不变。

`Checkpointer` 只有无条件 `Save/Load/Delete`，没有版本比较、事务切换或 fencing。`DistributedCheckpointer` 适用于单写者的检查点复制；多个实例共同写同一会话不具有隔离保证。需要权威的持久运行日志时使用 `core/durable.Store` 及满足其契约的后端。

主库正常时，实例按顺序执行主库提交与复制入队。一个消费者按顺序应用异步任务，`Save` 和 `Delete` 使用相同顺序。副库应用与健康切换共享屏障；已切到副库后不再向自己复制，尚未应用的旧主库任务会被丢弃，避免覆盖副库上已经确认的新写入或复活删除。同一实例的前台存储调用串行，异步复制的副库 I/O 不占用前台锁；切换须等待正在进行的副库写入结束。这是无版本后端的保守吞吐边界。

复制是尽力而为：异步队列满会丢弃复制任务，副库失败不会撤销主库成功写入，同步复制的副库失败也不会令主库回滚。自动故障切换可能读取落后的副库，无法承诺零 RPO。`GetStatus` 只报告本地健康、切换与队列信息，不能证明副库完整或跨进程权威归属。

不提供自动切回配置或执行路径。主库健康不代表已同步故障期间的更新、新建与删除；当前后端接口无法建立可靠追平屏障。状态中的 `auto_failback_supported` 为 `false`。

旧配置须删除 `FailbackDelay` 字段（包括原值 0），不保留兼容参数。故障切换后，停止并协调所有写入者，使用后端自己的备份、恢复或事务同步能力，校验副库的全部数据及删除结果，确认主库追平后才重新以主库构造实例。禁止仅因主库 `Ping` 成功而重建实例；进程重启也需要宿主保留并核对权威后端归属。框架不自动执行这项迁移。

`Close` 停止接受新操作、排空可应用的复制任务并等待后台协程退出；不会关闭宿主注入的后端。调用方必须保证后端调用最终返回，后台复制目前不提供独立 I/O 超时。
