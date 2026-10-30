# tools/coding — 编码 Agent 工作区工具

为 AI 编码 Agent 提供一组面向模型的工具：读写与精确编辑文件、glob/grep 检索、执行命令、git 操作、待办清单。文件与命令分别经 [`harness/env`](../../harness/env/README.md) 的 `FileSystem` 与 `Executor` 访问；权限、审批、结果溢出等横切能力由 `tools.Pipeline` 承担，对任何工具都生效。

```go
ws, err := coding.NewWorkspace("/path/to/repo",
    coding.WithExecutor(env.DockerExecutor{Root: "/path/to/repo", Image: "golang:1.25"}), // 省略时在宿主机执行
    coding.WithLinter(func(path string) []string { ... }),                                 // 可选：编辑后检查
)

store, _ := spill.New(artifactDir, 100_000) // 可选：超长结果写入宿主目录（tools/spill）
registry := tools.NewRegistry()
for _, t := range append(ws.Tools(), store.ReadTool()) {
    _ = registry.Register(t)
}
dispatcher, _ := tools.NewDispatcher(registry, tools.DispatchConfig{
    ReturnArgumentErrors: true,
    Pipeline: &tools.Pipeline{
        Pre:      []tools.PreHook{coding.DefaultPolicy().Hook()},
        Approver: approver, // 命令行提示、IM 卡片……
        Post:     []tools.PostHook{store.Hook()},
    },
})
instructions, _ := ws.ProjectInstructions(ctx) // AGENTS.md、CLAUDE.md，拼进系统提示词

agent, _ := builder.NewSimpleBuilder(client).
    WithSessionID("session-1"). // 会话级批准按会话隔离
    WithTools(dispatcher.Tools()...).
    WithMaxIterations(30).
    Build()
```

也可以用声明式配置组合上述各部分（工作区、沙箱、工具子集、权限、溢出），见 [`harness/preset`](../../harness/preset/README.md)。完整示例见 [examples/tools/09-coding-agent](../../examples/tools/09-coding-agent)：Agent 在临时仓库里运行测试、定位 bug、编辑代码并提交。

## 工具

| 名称 | 作用 |
|---|---|
| `read_file` | 带行号读取文本文件，支持 `offset`/`limit` 分段 |
| `write_file` | 创建或整体覆盖文件，自动创建父目录 |
| `edit_file` | 把唯一出现的 `old_string` 替换为 `new_string`，`replace_all` 替换全部 |
| `multi_edit` | 对同一文件按顺序应用多处替换，全部成功才写回 |
| `glob` | 按 glob 找文件，`**` 匹配任意层目录，按修改时间从新到旧排序 |
| `grep` | RE2 正则搜索内容，`include` 过滤文件；搜索不算读取 |
| `run_command` | `sh -c` 执行命令，返回退出码与合并输出 |
| `git_status` / `git_diff` / `git_log` / `git_commit` | 常用 git 操作 |
| `todo_write` | 模型维护的待办清单，宿主经 `ws.Todos()` 读取展示 |

名称常量为 `coding.ToolReadFile` 等。

## 权限与审批

`coding.DefaultPolicy()` 返回 [`tools/policy`](../policy/policy.go) 规则：放行只读、检索、待办和文件编辑，`run_command`、`git_commit` 需要审批。审批人选择"本会话允许"时：

- `run_command` 收窄为命令前缀（首词，第二个词是子命令时一并取，如 `go test`）；含 shell 控制符、引号或反斜杠的命令只能单次批准；
- `git_commit` 可整工具放行。

前缀规则是便利而非隔离：放行 `go test` 等于放行它的全部参数（`go test -exec` 可执行任意程序，测试代码本身也由模型编写）；拒绝规则可被 `/bin/rm`、`env rm`、`sh -c` 绕过。需要隔离时使用 `env.DockerExecutor`。审批的执行顺序、超时、请求合并见 `tools.Pipeline` 的文档。

## 行为约定

- **路径限定**：由 `env.LocalFS` 保证，每次操作都重新解析路径，越界或经符号链接指向工作区外时返回参数错误。
- **先读后改**：由 `env.Observed` 保证：修改已存在的文件前必须用 `read_file` 读过，写入以读到的版本为条件，读后被外部改动则拒绝；自己写入的内容视为已读。同一工作区的编辑串行执行，同一轮并发编辑同一文件不会丢失改动。
- **保护 `.git`**：写入类工具拒绝 `.git` 目录内的路径；git 工具固定关闭 `core.fsmonitor` 与 hooks，`git_diff` 不调用外部 diff 和 textconv。代价是 `git_commit` 不运行仓库的 pre-commit 等钩子，需要时让模型用 `run_command` 显式执行检查。
- **编辑后检查**：`WithLinter` 为文件返回检查命令；命令非零退出时撤销本次写入（新建的文件被删除），检查输出以 `tools.Rejected` 交给模型。
- **结果分类**：参数不合法是参数错误（开启 `ReturnArgumentErrors` 时交还模型）；文件不存在、无权限、未读先改、读后被改、`old_string` 找不到或不唯一、git 非零退出以 `tools.Rejected` 交给模型；只有 I/O 失败、命令无法启动、调用方 ctx 结束才返回 error 中止运行。
- **命令结果**：`run_command` 的非零退出码和超时都是正常结果（`exit_code`、`timed_out`）；命令结束后遗留的后台进程被结束，不能用它启动常驻服务。输出超过上限时保留首尾各一半。命令工具与写入类工具（可能运行 linter）向 Dispatcher 声明比命令超时多 10 秒的时限。
- **文件限制**：大于 10MB 或前 8000 字节含 NUL 的文件不作为文本处理；`glob`/`grep` 跳过 `.git` 目录和符号链接文件。

## 检查点与回滚

写入类工具经 `env.Checkpointed` 在首次修改某个文件前保存原始状态。`ws.Changes()` 列出改动过的文件，`ws.Revert(ctx, paths...)` 恢复原内容与权限位、删除新建的文件，不传参数时回滚全部；回滚后的文件须重新读取才能再修改。`run_command` 对文件的改动不在记录内，需要时用 git 查看。

## 结果溢出

由 [`tools/spill`](../spill/spill.go) 提供：`spill.New(dir, maxBytes)` 的 `Hook()` 把任何工具超长的成功结果写入宿主目录 `dir`（不在工作区内），交给模型的结果替换为 1500 字符预览与 `spill_path`；模型用 `ReadTool()` 提供的 `read_spill` 分段读取。写入失败时只返回预览并说明原因，不中止运行。

## 测试

```bash
go test ./tools/coding/ ./tools/policy/ ./harness/env/
# 可选：Docker 测试，镜像需含 sh
GOAGENT_CODING_DOCKER_IMAGE=alpine:3 go test -run Docker ./tools/coding/
GOAGENT_ENV_DOCKER_IMAGE=alpine:3 go test -run Docker ./harness/env/
```
