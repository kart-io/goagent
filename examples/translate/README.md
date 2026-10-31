# 智能翻译系统 (Multi-Agent Translation System)

用两个 Agent 协作：先检测输入文本的语言，再翻译成简体中文，并用 `core.CostTrackingCallback` 统计 token 与成本。

## 结构

| 文件 | 说明 |
| --- | --- |
| `main.go` | 批量示例：对 9 条内置文本（英、法、日、西、德、俄、中及两段长文本）依次检测并翻译，最后输出总 token 与总成本 |
| `interactive/main.go` | 单条示例：从命令行参数（或标准输入）读取一段文本并翻译 |
| `run.sh` | 检查 `DEEPSEEK_API_KEY` 后执行 `go run main.go` |
| `test.sh` | 不调用模型：检查文件存在并编译两个程序 |

## 工作流程

1. **语言检测 Agent**：系统提示要求只返回中文语言名称（如“英语”“法语”）。
2. **翻译 Agent**：系统提示要求只返回简体中文译文；检测结果为“中文”时跳过翻译，直接返回原文。

两个 Agent 用同一个 DeepSeek 客户端（`deepseek-chat`，temperature 0.3，max tokens 2000）构建，并共享一个 token 追踪回调：

```go
pricing := map[string]float64{
    "deepseek-chat": 0.21 / 1_000_000, // 输入/输出单价的简化平均值
}
tokenTracker := core.NewCostTrackingCallback(pricing)

agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt(systemPrompt).
    WithState(state.NewAgentState()).
    WithCallbacks(tokenTracker).
    Build()
```

每一步的 token 用量通过前后两次 `tokenTracker.GetTotalTokens()` 的差值计算，结果写入 `TranslationResult` 的 `DetectionTokens`、`TranslationTokens`、`TokensUsed`；成本由 `GetTotalCost()` 给出。

## 运行

需要 `DEEPSEEK_API_KEY`。

```bash
export DEEPSEEK_API_KEY="your-api-key"
cd examples/translate
go run main.go        # 或 ./run.sh
./test.sh             # 只编译验证，不调用模型

cd interactive
go run main.go "Hello, how are you today?"
```

交互示例未传参数时用 `fmt.Scanln` 读取标准输入，只会读到第一个空白前的内容，多词文本请通过命令行参数传入。

每条文本输出检测语言、翻译结果和三项 token 计数；交互示例另外输出预估成本。
