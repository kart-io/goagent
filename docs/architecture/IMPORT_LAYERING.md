# GoAgent 导入层级与检查范围

职责分层见 [架构概述](ARCHITECTURE.md)。本页只描述 [scripts/apicheck](../../scripts/apicheck/main.go) 当前真正执行的规则；概念图不是所有 import 组合的静态证明。Go 编译器另行检查循环依赖。

检查读取生产 Go 文件的 AST，排除测试、隐藏目录与 vendor，不从注释或字符串推断 import。

| 来源 | 检查器限制 |
| --- | --- |
| 所有包 | 禁止导入独立 `github.com/kart-io/goagent-harness`；非 examples 禁止导入当前模块 examples |
| `interfaces`、`errors` | 禁止导入其他当前模块包 |
| `utils` | 只允许当前模块的 utils 子包及叶子 errors |
| `cache` | 只允许 cache 子包和既有 utils/json |
| `core` | 禁止导入 agents、tools、middleware、parsers、builder |
| `builder` | 禁止导入顶层 middleware、agents |
| `tools` | 禁止导入 agents |
| `parsers` | 禁止导入 agents、顶层 middleware |
| 基础与执行机制包 | 禁止反向导入顶层 execution 装配入口，具体来源集合见检查器 |

`core/middleware` 与顶层 `middleware` 是不同路径；Builder 消费前者的执行协议。`execution` 是宿主装配薄层，不同于承载状态与存储环境的 `core/execution.Runtime`；examples、evaluation、cmd 可消费装配入口。

```bash
bash scripts/verify_imports.sh
# 扫描旧接口/入口候选，不进行自动迁移
go run ./scripts/apicheck -mode migration -root .
```

迁移扫描不会做完整类型推导，可能误报或漏报。新编排使用 `core.Runnable`、Builder.AsRunnable 和 workflow；保留兼容接口并不代表推荐新业务继续依赖旧路径。
