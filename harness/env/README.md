# harness/env — 执行环境能力接口

Agent 工具访问文件与执行命令的统一接口。工具只依赖接口，替换实现即可把全部工具整体迁移到容器或远程沙箱。

| 接口 | 作用 | 实现 |
|---|---|---|
| `FileSystem` | 工作区内的 `Resolve`/`Stat`/`ReadFile`/`WriteFile`/`Remove`/`Walk`，路径为相对工作区根目录、以 `/` 分隔 | `LocalFS` |
| `Executor` | 在工作区内执行命令，声明 `Capabilities`（Remote、Container、Network） | `LocalExecutor`、`DockerExecutor` |

包装器（叠加在任意 `FileSystem` 上）：

| 包装器 | 作用 |
|---|---|
| `Observed` | 先读后改：修改已存在的文件前必须 `ReadFile` 过，写入以读到的版本为条件（`ErrNotObserved`、`ErrStale`）；`Peek` 读取但不记录 |
| `Checkpointed` | 首次写入或删除前保存原状态，`Changes()` 列出改动，`Revert(ctx, paths...)` 回滚 |

## 约定

- **路径限定**：`LocalFS` 每次操作都重新解析路径并检查仍在工作区内，路径已被换成指向工作区外的符号链接时返回 `ErrOutsideWorkspace`；解析与打开之间仍有被替换的窗口，不能防御并发篡改工作区的进程。`Remove`（含两个包装器）删除链接本身。
- **条件写入**：`WriteIntent.CreateIfAbsent` 要求文件不存在（`ErrExists`），`IfVersion` 要求版本一致（`ErrStale`）。同进程所有 `LocalFS` 实例按解析后的绝对路径协调写入与删除，包括不同实例和重叠根目录。`IfVersion` 的检查与写入只对这些协作调用原子；直接 `os.WriteFile`、编辑器、命令执行器及其他进程不参与协调，版本比较只能发现检查前的修改。需要跨进程 CAS 时应使用提供该协调协议的 `FileSystem` 实现。
- **文件发布**：Unix 本地文件系统上，在目标同目录写临时文件、设置权限、同步并关闭后，以 rename 替换目标；`CreateIfAbsent` 通过硬链接原子发布，即使外部进程抢先创建目标也不会覆盖它。发布前失败不截断旧文件，已打开的读者仍可读完整旧版本；发布后同步父目录。目录同步等发布后操作失败时会返回 error，但新内容可能已经可见，应重新读取确认。新建的多层父目录不提供整棵目录树的断电持久性保证。底层文件系统须支持相应的 rename、硬链接和同步语义。
- **替换语义**：保留或显式设置权限位（`Perm` 为 0 时保留原权限，新文件为 0644），符号链接仍写入解析后的目标；替换更换 inode，其他硬链接仍指向旧内容，不保留原文件的所有者、ACL 与扩展属性。
- **版本**：内容的 SHA-256，内容相同则版本相同。
- **命令**：非零退出返回 `Result` 与 nil；未能启动或 ctx 结束才返回 error，`Result` 可带已产生的输出。
  - `LocalExecutor`：Unix 上命令在独立进程组运行，结束或超时后结束整个进程组（`setsid` 脱离进程组的进程除外）；命令退出后遗留的后台进程最多再等 1 秒输出。
  - `DockerExecutor`：一次性容器，工作区挂载到 `/workspace`，默认 `--network none`，以宿主机 uid:gid 运行并设 `HOME=/tmp`，超时或取消时删除容器。
- **回滚**：`Checkpointed` 只记录经它写入的改动；写入失败不留记录；新建后又删除的文件不再列入改动；回滚经底层 `FileSystem`，因此同样受路径限定，文件被换成指向工作区外的链接时该文件回滚失败，其余文件照常。

## 测试

```bash
go test ./harness/env/
GOAGENT_ENV_DOCKER_IMAGE=alpine:3 go test -run Docker ./harness/env/
```
