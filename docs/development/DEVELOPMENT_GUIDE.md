# GoAgent 开发指南

## 环境

```bash
go version            # 需要 Go 1.25（go.mod: go 1.25.0）
git clone https://github.com/kart-io/goagent.git && cd goagent
go mod download
go build ./...
```

`make lint` 需要 golangci-lint v2（`.golangci.yml` 为 `version: "2"`），未安装或版本不符时会自动安装 v2.6.2；`make fmt` 会按需安装 `gofumpt` 与 `gci`。

## 常用命令

| 命令 | 作用 |
|------|------|
| `make test` | `GO_TEST_MODE=true go test -v -race -timeout 120s ./...` |
| `make test-short` | 带 `-short` 的测试 |
| `make test-integration` | `-tags=integration` 的测试 |
| `make coverage` | 生成 `coverage/coverage.out` 与 `coverage/coverage.html` |
| `make bench` | `go test -bench=. -benchmem ./...` |
| `make fmt` | `gofumpt -w .`，`gci` 按标准库 → 第三方 → `github.com/kart-io/goagent` 分组 |
| `make vet` / `make lint` | `go vet ./...` / `golangci-lint run ./...` |
| `bash scripts/verify_imports.sh` | 导入层级检查，见 [导入层级说明](../architecture/IMPORT_LAYERING.md) |
| `./scripts/check_coverage.sh` | 读取已有的 `coverage.out`，整体覆盖率（不含示例）须 ≥ 45%，并检查少数关键文件 |
| `bash scripts/provider_regression.sh [REPORT.jsonl]` | 离线 Provider/Builder 契约与竞态回归，见 [持续回归](../guides/PROVIDER_REGRESSION.md) |

CI（`.github/workflows/`）运行带 `-race` 的全量测试、导入检查与 golangci-lint；PR 流程另跑覆盖率门禁。

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

在 `llm/providers` 中实现 `llm.Client`；要用于原生工具循环还须实现 `llm.StreamClient` 与 `llm.ToolStreamClient`（参考 `openai_stream.go`、`deepseek_stream.go`、`gemini_native.go`）。构造函数沿用 `NewXxxWithOptions(opts ...llm.ClientOption)` 并复用 `common.BaseProvider`：

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

func (c *MyClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 用 c.NewHTTPClient(...) 发请求，模型、温度等取 c.GetModel(req.Model)、c.GetTemperature(req.Temperature)
	return &llm.CompletionResponse{Content: "...", Model: c.GetModel(req.Model), Provider: string(ProviderMy)}, nil
}

func (c *MyClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return c.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (c *MyClient) Provider() constants.Provider { return ProviderMy }

func (c *MyClient) IsAvailable() bool { return c.Config.APIKey != "" }
```

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
