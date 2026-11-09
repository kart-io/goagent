# 不可变 AgentRevision 示例

从仓库根目录运行：

```sh
go run ./examples/agent-revisions
```

本示例会通过真实的 `go build` 编译并执行仓库内受信任的 Go 程序，分别生成 v1、v2 两个版本。编译设置 60 秒超时并禁用网络下载；请预先准备好仓库要求的 Go 工具链。示例不调用模型，也不使用 Docker。

每个版本的二进制字节和提示词一起封存为不可变修订。`Runtime.Artifact` 指向归档中的 `bin/agent`，其文件字节经 SHA-256 校验，必须与运行时 `Digest` 一致。

在三次执行开始之前，示例先删除原始编译目录，关闭并重新打开归档，再通过 `ArchivedFactory` 和按完整运行时身份精确匹配的 registry，依次执行 v1、v2、v1。loader 使用 `Root.Chmod(artifact, 0500)` 显式授予归档程序执行权限。整个执行过程均依赖重开后的归档，验证旧程序和旧提示词仍保持原样。

三次执行结果分别为：

```text
program-version=v1 prompt=旧版提示词
program-version=v2 prompt=新版提示词
program-version=v1 prompt=旧版提示词
```

两个版本的修订标识非空且不同。此示例中的 `Inspect` 返回 `absent` 仅为演示约定，不是生产恢复协议；会话端到端行为由 [`management/revision_test.go`](../../management/revision_test.go) 验证。

验证此示例：

```sh
go test ./examples/agent-revisions
```

测试只执行一次完整流程，断言真实程序输出与修订标识，并单独检查预取消的上下文返回 `context.Canceled`。
