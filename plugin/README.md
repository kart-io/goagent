# plugin

Agent 能力包：在 Go 中静态组合工具、提示词片段、循环钩子、中间件、回调与进程级资源，由 `Host` 管理启停，经 `Install` 装进任意 `builder.AgentBuilder`。

| 文件 | 内容 |
|---|---|
| `plugin.go` | `Plugin`、`Starter`、`Dependent`、`PreStep`、`Contribution` |
| `resources.go` | `Env`、`Resources`、`Lookup` |
| `host.go` | `Host`：依赖排序、启动回滚、停止 |
| `install.go` | `Install`、`Only`、`Dispatch` |

使用方式与规则见 [插件指南](../docs/guides/PLUGIN_SYSTEM_GUIDE.md)。
