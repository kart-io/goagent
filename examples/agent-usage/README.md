# Token 统计案例已迁移

案例代码统一放在 [kart-io/goagent-examples/agent-usage](https://github.com/kart-io/goagent-examples/tree/main/agent-usage)。本目录只保留迁移说明。

在 goagent-examples 根目录执行：

```sh
make work
go test -race ./agent-usage/...
go run ./agent-usage/task
go run ./agent-usage/agent -mode child
```

真实模型入口为 agent-usage/live，多平台、回放和错误边界入口为 agent-usage/cases。历史评测日志中的 examples/agent-usage 路径是迁移前的执行位置；当前代码与命令以案例仓库 README 为准。核心设计和既有验证证据保留在 GoAgent docs 中。
