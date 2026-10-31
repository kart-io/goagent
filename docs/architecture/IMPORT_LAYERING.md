# GoAgent 导入层级说明

## 检查器

`scripts/verify_imports.sh` 调用 `go run ./scripts/apicheck -mode imports -root .`：用 Go AST 读取仓库内全部非测试 `.go` 文件（跳过 `_test.go`、隐藏目录、`vendor`、`node_modules`，包含 `examples/`）的 import，任一违规即以非零码退出。规则在 `scripts/apicheck/main.go` 的 `forbidden` 中，回归测试在 `scripts/apicheck/main_test.go`。CI（`ci.yml`、`pr.yml`、`release.yml`）都会运行该脚本；`--strict` 只影响 `-mode migration`，导入模式始终在违规时失败。

规则按“源文件所在的顶层目录”与“被导入的 GoAgent 包”判断。下表是检查器**实际执行**的全部规则，表外的组合不检查，循环依赖由 Go 编译器拒绝。

## 规则

| 源目录 | 禁止导入 |
|--------|----------|
| 任意（含 `examples/`） | `github.com/kart-io/goagent-harness` 及其子包 |
| `interfaces/`、`errors/` | 任何 GoAgent 内部包 |
| `utils/` | 除 `utils/...` 与 `errors` 以外的内部包 |
| `cache/` | 除 `cache/...` 与 `utils/json` 以外的内部包 |
| `core/` | `tools`、`middleware`、`parsers`、`builder`（顶层目录，`core/middleware` 不受影响） |
| `builder/` | `middleware` |
| `parsers/` | `middleware` |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness` | 顶层 `execution` |
| 非 `examples/` | `examples/...` |

说明：

- 顶层 `execution/` 是宿主装配薄层（依赖 `builder`、`core`、`core/budget`、`core/durable`、`core/observe`、`tools`），只有表中未列出的目录（如 `examples`、`evaluation`、`mcp`、`testing`）可以导入；它不替代 `core/execution`。
- Harness 已迁到独立仓库 `github.com/kart-io/goagent-harness`，它依赖 GoAgent，反向导入被禁止。本仓库的 `harness/env` 是不依赖内部包的通用环境接口。迁移见 [Harness 迁移指南](../guides/HARNESS_MIGRATION.md)。

## 当前依赖图

`go list` 得到的实际内部依赖（不含 `examples/`、测试与 `cmd/`）见 [架构概述](ARCHITECTURE.md#包依赖)。基础叶子包为 `errors`、`interfaces`、`utils/json`、`llm/constants`、`harness/env`。

## 运行

```bash
bash scripts/verify_imports.sh      # 期望输出：imports: 0 findings
go test ./scripts/apicheck/         # 规则回归测试
```

新增或调整边界时，同时修改 `forbidden` 与 `main_test.go` 的用例，再更新本表。

## 导入顺序

`make fmt` 用 `gofumpt` 格式化，并用 `gci` 按“标准库 → 第三方 → `github.com/kart-io/goagent`”分组排序。

## 相关文档

- [架构概述](ARCHITECTURE.md)
- [测试最佳实践](../development/TESTING_BEST_PRACTICES.md)
