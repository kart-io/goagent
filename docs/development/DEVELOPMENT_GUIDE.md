# GoAgent 开发指南

## 环境

```bash
go version            # go.mod 最低 Go 1.27.0，开发工具链 Go 1.27.3
git clone https://github.com/kart-io/goagent.git && cd goagent
go mod download
go build ./...
```

`make lint` 需要 golangci-lint v2（`.golangci.yml` 为 `version: "2"`），未安装或主版本不符时会自动安装 v2.15.0；`make fmt` 会按需安装 `gofumpt` 与 `gci`。

Makefile 从 `go.mod` 的 `toolchain` 声明选择开发工具链，并清除传给子命令的 `GOROOT`，避免 Homebrew 的 `go` 与旧 GVM 标准库混用。首次运行可能下载工具链；不改 shell 配置。直接运行 Go 命令时，若遇到 `compile: version ... does not match go tool version ...`，使用 `env -u GOROOT GOTOOLCHAIN=go1.27.3 go ...`，或修正自己的 Go 环境。可通过显式设置 `GOTOOLCHAIN` 验证其他受支持版本。

## 常用命令

| 命令 | 作用 |
|------|------|
| `make build` | 编译检查主模块全部库和示例包，不生成统一应用二进制 |
| `make build-all` | 关闭 CGO，检查 Linux amd64/arm64、Windows amd64、macOS amd64/arm64 的编译；不代表目标平台运行验证 |
| `make run` | 运行无需密钥的 `examples/execution-scope` 离线宿主示例 |
| `make run EXAMPLE=./examples/workflow` | 运行指定示例；需要模型的示例须另行配置密钥 |
| `make test` | `GO_TEST_MODE=true go test -v -race -timeout 120s ./...` |
| `make test-short` | 带 `-short` 的测试 |
| `make test-frameworkbench` | 使用只读依赖运行独立性能评测模块的竞态契约测试；不执行计时基准 |
| `make test-contracts` | 执行、模型流、持久恢复、审批、预算与工具的完整竞态契约测试 |
| `make compatibility` | 用当前源码验证相邻关联仓库；可设置 `COMPAT_ROOT` 与新的 `COMPAT_OUT` |
| `make test-integration` | `-tags=integration` 的测试 |
| `make coverage` | 生成 `coverage/coverage.out`、执行覆盖率门禁并生成 `coverage/coverage.html` |
| `make bench` | `go test -bench=. -benchmem ./...` |
| `make fmt` | `gofumpt -w .`，`gci` 按标准库 → 第三方 → `github.com/kart-io/goagent` 分组 |
| `make vet` / `make lint` | `go vet ./...` / `golangci-lint run ./...` |
| `bash scripts/verify_imports.sh` | 导入层级检查，见 [导入层级说明](../architecture/IMPORT_LAYERING.md) |
| `./scripts/check_coverage.sh` | 默认读取 `coverage/coverage.out`，可传入文件路径；按语句数检查整体、执行核心模块及关键文件，详见[测试规范](TESTING_BEST_PRACTICES.md) |
| `bash scripts/provider_regression.sh [REPORT.jsonl]` | 离线 Provider/Builder 契约与竞态回归，见 [持续回归](../guides/PROVIDER_REGRESSION.md) |

CI（`.github/workflows/`）运行带 `-race` 的全量测试、导入检查与 golangci-lint；PR 流程另跑覆盖率门禁。

公共契约改动还须执行 `make test-frameworkbench` 与 `make compatibility`。后者要求三个关联仓库位于同一个父目录，自动包含嵌套模块；使用临时 modfile，不修改关联仓库的 `go.mod`、`go.sum` 或 `go.work`。报告中必须核对 `core_directory` 指向本次源码，并逐项检查 `skipped_tests`：没有配置 Postgres 等测试服务时，编译和离线测试通过不能代替实库验收。报告目录必须新建，避免覆盖历史证据。

本仓库作为 Go 库交付，应用通过 `go get github.com/kart-io/goagent@<version>` 引入。旧 Makefile 中假设统一应用二进制的 `install`、`uninstall`、`docker-*`、`init` 目标已移除；`make version` 使用 `git describe --tags --always --dirty` 显示库版本。需要分发某个示例时，显式指定包与产物路径，例如 `go build -o /tmp/execution-scope ./examples/execution-scope`。

## 项目结构

包职责与依赖图见 [架构概述](../architecture/ARCHITECTURE.md)。Preset 与编码工具在 `github.com/kart-io/goagent-harness`，大部分业务示例在 `github.com/kart-io/goagent-examples`，不要在本仓库重新引入。

## 代码约定

- 注释、错误信息与文档使用简体中文，标识符用英文。
- 错误使用 `github.com/kart-io/goagent/errors`：

```go
import agentErrors "github.com/kart-io/goagent/errors"

func validate(input map[string]any) error {
	if input == nil {
		return agentErrors.NewInvalidInputError("my_component", "input", "input cannot be nil")
	}
	return nil
}

func wrap(err error) error {
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to do something")
	}
	return nil
}

func isInput(err error) bool { return agentErrors.IsCode(err, agentErrors.CodeInvalidInput) }
```

- 会调用宿主代码的同步入口用 `core.CallSafely` / `core.RecoverPanic` 把 panic 转为错误，见 [Panic 处理](PANIC_HANDLING.md)。
- 所有阻塞操作接受并遵守 `context.Context`；工具须遵守 `ToolInput.Context` 的期限。
- 不做向后兼容层：删除的 API 直接删除，迁移写进 CHANGELOG。

## 扩展开发

### 新工具

实现 `interfaces.Tool`，或用 `tools.NewFunctionTool` / `NewTypedFunctionTool`。工具注册到 `tools.Registry`，经 `tools.NewDispatcher` 交给 Builder（`WithTools(d.Tools()...)`）；Dispatcher 负责 Schema 校验、授权、管线与持久记录，工具本身不必处理这些。

```go
type WeatherArgs struct {
	City string `json:"city"`
}

type Weather struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_c"`
}

func weatherDispatcher() (*tools.Dispatcher, error) {
	weather := tools.NewTypedFunctionTool("get_weather", "查询城市天气",
		`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
		func(ctx context.Context, in WeatherArgs) (Weather, error) {
			return Weather{City: in.City, TempC: 21}, nil
		})
	reg := tools.NewRegistry()
	if err := reg.Register(weather); err != nil {
		return nil, err
	}
	return tools.NewDispatcher(reg, tools.DispatchConfig{ReturnArgumentErrors: true})
}
```

可选能力接口：`interfaces.ValidatableTool`（自定义校验）、`tools.ReconcilableTool`（崩溃后查询效果是否已生效）、`tools.DispatchTimeoutProvider`（自定义超时）、`tools.DurablePolicyProvider` / `DurableIntentVersioner`（持久策略与意图版本）。工具的契约见 [工具 API](../api/TOOL_API.md)。

### 新 LLM Provider

在 `llm/providers` 中实现 `llm.Client`（`CompleteStream` 与 `Provider`）；要用于原生工具循环还须实现 `llm.ToolStreamClient`（参考 `openai_stream.go`、`deepseek_stream.go`、`gemini_native.go`）。构造函数沿用 `NewXxxWithOptions(opts ...llm.ClientOption)` 并复用 `common.BaseProvider`：

```go
// 正式接入时把常量与环境变量名加到 llm/constants。
const ProviderMy constants.Provider = "myprovider"

type MyClient struct {
	*common.BaseProvider
	*common.ProviderCapabilities
}

func NewMyWithOptions(opts ...llm.ClientOption) (*MyClient, error) {
	base := common.NewBaseProvider(opts...)
	base.ApplyProviderDefaults(ProviderMy, "https://api.example.com/v1", "my-model", "MY_BASE_URL", "MY_MODEL")
	if err := base.EnsureAPIKey("MY_API_KEY", ProviderMy); err != nil {
		return nil, err
	}
	return &MyClient{
		BaseProvider:         base,
		ProviderCapabilities: common.NewProviderCapabilities(llm.CapabilityCompletion, llm.CapabilityChat),
	}, nil
}

func (c *MyClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	// 用 c.NewHTTPClient(...) 发请求，模型、温度等取 c.GetModel(req.Model)、c.GetTemperature(req.Temperature)
	// 只有同步接口时，把完整结果作为一个终止块返回。
	content := "..."
	out := make(chan *llm.StreamChunk, 1)
	out <- &llm.StreamChunk{Delta: content, Content: content, FinishReason: "stop", Done: true}
	close(out)
	return out, nil
}

func (c *MyClient) Provider() constants.Provider { return ProviderMy }
```

调用方用 `llm.Complete(ctx, client, req)` / `llm.Chat(ctx, client, messages)` 获取完整响应。

能力声明与契约测试见 [Provider 能力](../guides/PROVIDER_CAPABILITIES.md) 和 `llm/providers/capability_contract_test.go`。

### 新 Agent 中间件

实现 `core/middleware.Middleware`（`Name`/`OnBefore`/`OnAfter`/`OnError`），通常嵌入 `BaseMiddleware` 只覆盖需要的方法，再用 `WithMiddleware` 注册：

```go
type TenantMiddleware struct {
	*middleware.BaseMiddleware
	tenant string
}

func NewTenantMiddleware(tenant string) *TenantMiddleware {
	return &TenantMiddleware{BaseMiddleware: middleware.NewBaseMiddleware("tenant"), tenant: tenant}
}

func (m *TenantMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	req.Metadata["tenant"] = m.tenant
	return req, nil
}

func useMiddleware(client llm.Client) (*builder.SimpleAgent, error) {
	return builder.NewSimpleBuilder(client).WithMiddleware(NewTenantMiddleware("acme")).Build()
}
```

这里的 `middleware` 是 `github.com/kart-io/goagent/core/middleware`。改变模型请求、历史或工具调用的行为优先用循环钩子（`WithPreStep`、`WithRequestHook`、`WithRequestError`、`WithTurnStopping`）或 `tools.Pipeline`，它们的持久与恢复语义见 [扩展契约](../guides/EXTENSION_CONTRACT.md)。

## 调试

- `WithVerbose(true)` 打开 Builder 的详细输出。
- 用 `core/observe.WithObserver` 接收 agent/model/tool 事件；`devtools.NewRecorder` 记录事件，`devtools.WriteReport` 生成图报告。
- 持久运行可用 `durable.InspectOperation`、`durable.DiagnoseOperations`、`Runner.Diagnose` / `DurableGraph.Diagnose` 只读诊断；会话日志可用 `testing/replay` 回放。

## 提交

- 提交信息使用约定式格式（`feat`、`fix`、`docs`、`refactor`、`test`、`chore` 等），正文用中文说明原因。
- 提交前运行 `make fmt`、`make vet`、`make lint`、`bash scripts/verify_imports.sh`、`make test`；破坏性变更在 CHANGELOG 中写明迁移方式。

## 相关文档

- [测试最佳实践](TESTING_BEST_PRACTICES.md)
- [Panic 处理指南](PANIC_HANDLING.md)
- [架构概述](../architecture/ARCHITECTURE.md)
- [API 参考](../api/CORE_API.md)
