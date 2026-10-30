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
- **条件写入**：`WriteIntent.CreateIfAbsent` 要求文件不存在（`ErrExists`），`IfVersion` 要求版本一致（`ErrStale`）；检查与写入在本进程内原子。`Perm` 为 0 时保留原权限，新文件为 0644。
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
