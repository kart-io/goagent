package agentbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/evaluation"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// Case uses fixed Evidence or Lookup.Orders; neither means a task without tools.
type Case struct {
	ID       string      `json:"id"`
	Question string      `json:"question"`
	Answer   string      `json:"answer"`
	Evidence string      `json:"evidence,omitempty"`
	Lookup   *LookupCase `json:"lookup,omitempty"`
}

type Config struct {
	// Provider selects openai, deepseek or gemini-openai; empty defaults to openai.
	Provider string `json:"provider,omitempty"`
	// DirectTools 仅用于工具结果已是完整答案的独立题集，显式启用。
	DirectTools bool              `json:"direct_tools"`
	APIKey      string            `json:"-"`
	BaseURL     string            `json:"-"`
	Budget      evaluation.Budget `json:"budget"`
	Timeout     time.Duration     `json:"timeout"`
}

type Result struct {
	LookupBackend       string                  `json:"lookup_backend,omitempty"`
	LookupMisses        int32                   `json:"lookup_misses,omitempty"`
	ToolArgumentsPassed *bool                   `json:"tool_arguments_passed,omitempty"`
	ActualToolArguments []json.RawMessage       `json:"actual_tool_arguments,omitempty"`
	Framework           string                  `json:"framework"`
	CaseID              string                  `json:"case_id"`
	Answer              string                  `json:"answer"`
	Success             bool                    `json:"success"`
	Error               string                  `json:"error,omitempty"`
	DurationMS          float64                 `json:"duration_ms"`
	FirstTextMS         *float64                `json:"first_text_ms,omitempty"`
	ToolCalls           int32                   `json:"tool_calls"`
	Measurements        evaluation.Measurements `json:"measurements"`
	UsageComplete       bool                    `json:"usage_complete"`
}

// SystemInstruction is shared by framework comparison executors.
const SystemInstruction = "完成用户任务。若存在 lookup 工具，必须先调用该工具，不能仅回复将要查询。该无参数工具每题只需查询一次，收到成功结果后不要重复查询。若用户要求原样返回工具文本，成功工具返回的非空正文就是完整答案，保留所有字符、语言和换行，不翻译、不加引号或 Markdown，不将合法状态值改成 UNKNOWN。若用户要求事实提取或计算，则依据工具结果回答，只有无法从资料得出所问事实时才输出 UNKNOWN。最终只输出答案本身，不要解释。"

// LookupDescription describes the identical evidence tool for each framework.
const LookupDescription = "查询本题的参考资料，无需参数。"

// lookup 返回本题的工具资料，并记录实际调用次数。
type lookup struct {
	evidence   string
	calls      atomic.Int32
	parameters json.RawMessage
	mu         sync.Mutex
	arguments  []json.RawMessage
	orders     *OrderLookupBackend
	misses     atomic.Int32
}

func (t *lookup) Name() string { return "lookup" }
func (t *lookup) Description() string {
	if t.orders != nil {
		return "在当前会话授权范围查询订单，仅接受 order_id；未找到或无权限统一返回 NOT_FOUND。"
	}
	if len(t.parameters) > 0 {
		return "查询本题的参考资料，请根据用户问题填写工具参数。"
	}
	return LookupDescription
}
func (t *lookup) ArgsSchema() string {
	if len(t.parameters) > 0 {
		return string(t.parameters)
	}
	return `{"type":"object","properties":{}}`
}
func (t *lookup) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.calls.Add(1)
	if len(t.parameters) > 0 {
		if input == nil {
			return nil, fmt.Errorf("missing tool input")
		}
		data, err := json.Marshal(input.Args)
		if err != nil {
			return nil, err
		}
		t.mu.Lock()
		t.arguments = append(t.arguments, data)
		t.mu.Unlock()
	}
	if t.orders != nil {
		if input == nil {
			return nil, fmt.Errorf("missing tool input")
		}
		text, missed, err := t.orders.query(ctx, input.Args)
		if err != nil {
			return nil, err
		}
		if missed {
			t.misses.Add(1)
		}
		return &interfaces.ToolOutput{Result: text, Success: true}, nil
	}
	return &interfaces.ToolOutput{Result: t.evidence, Success: true}, nil
}

// Run 执行一个 GoAgent 评测任务。失败也保留结果；首次文本时间不等同于工具参数首帧时间。
func Run(parent context.Context, c Case, config Config) Result {
	t := &lookup{evidence: c.Evidence}
	if c.Lookup != nil {
		t.parameters = c.Lookup.Parameters
		t.orders = c.Lookup.Orders
	}
	result := RunWithExecutor(parent, c, config, "goagent", func(ctx context.Context, baseURL string, cfg Config, c Case, onText, onFinal func(string)) (int32, error) {
		err := runGoAgent(ctx, baseURL, cfg, c, t, onText, onFinal)
		return t.calls.Load(), err
	})
	if hasLookupTool(c) {
		result.LookupBackend = "fixed_evidence"
		if t.orders != nil {
			result.LookupBackend = "tenant_orders"
			result.LookupMisses = t.misses.Load()
		}
	}
	if c.Lookup != nil {
		t.mu.Lock()
		result.ActualToolArguments = append([]json.RawMessage(nil), t.arguments...)
		t.mu.Unlock()
		passed := len(result.ActualToolArguments) == 1 && equalArguments(c.Lookup.ExpectedArguments, result.ActualToolArguments[0])
		result.ToolArgumentsPassed = &passed
	}
	return result
}

// Executor runs a framework through the supplied metered BaseURL. It reports
// transient text and the authoritative final answer separately, plus tool calls.
// Executors must honor ctx and finish all requests before returning.
type Executor func(context.Context, string, Config, Case, func(string), func(string)) (int32, error)

// RunWithExecutor shares validation, timeout, metering and exact-match scoring.
// Only the supplied framework executor changes between comparison runs.
func RunWithExecutor(parent context.Context, c Case, config Config, framework string, execute Executor) Result {
	result := Result{Framework: framework, CaseID: c.ID}
	if execute == nil || framework == "" {
		result.Error = "framework and executor are required"
		return result
	}
	if err := ValidateLookupCase(c); err != nil {
		result.Error = err.Error()
		return result
	}
	if config.Provider != "" && config.Provider != "openai" && config.Provider != "deepseek" && config.Provider != "gemini-openai" {
		result.Error = "provider must be openai, deepseek or gemini-openai"
		return result
	}
	if config.APIKey == "" || config.BaseURL == "" || config.Timeout <= 0 || config.Budget.MaxCalls <= 0 || config.Budget.MaxOutputTokens <= 0 || config.Budget.Model == "" || c.ID == "" || c.Question == "" || c.Answer == "" {
		result.Error = "题目、凭据、地址、模型、超时和正数预算不能为空"
		return result
	}
	meter := &Meter{Endpoint: config.BaseURL, Budget: config.Budget}
	proxy := httptest.NewServer(meter)
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(parent, config.Timeout)
	defer cancel()
	start := time.Now()
	var text strings.Builder
	onText := func(delta string) {
		if delta == "" {
			return
		}
		if result.FirstTextMS == nil {
			ms := float64(time.Since(start).Microseconds()) / 1000
			result.FirstTextMS = &ms
		}
		text.WriteString(delta)
	}
	finalSeen := false
	toolCalls, runErr := execute(ctx, proxy.URL, config, c, onText, func(final string) { finalSeen = true; text.Reset(); text.WriteString(final) })
	if runErr == nil {
		runErr = ctx.Err()
	}
	if runErr == nil && !finalSeen {
		runErr = io.ErrUnexpectedEOF
	}
	cancel()
	proxy.Close()
	result.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	result.Answer = strings.TrimSpace(text.String())
	result.ToolCalls = toolCalls
	result.Measurements = meter.Measurements()
	result.UsageComplete = result.Measurements.Calls > 0 && result.Measurements.Calls == result.Measurements.UsageReports
	result.Success = runErr == nil && result.Answer == c.Answer && (!hasLookupTool(c) || result.ToolCalls > 0)
	if runErr != nil {
		result.Error = runErr.Error()
	}
	return result
}

func runGoAgent(ctx context.Context, baseURL string, cfg Config, c Case, t *lookup, onText func(string), onFinal func(string)) error {
	options := []llm.ClientOption{llm.WithAPIKey(cfg.APIKey), llm.WithBaseURL(baseURL), llm.WithModel(cfg.Budget.Model), llm.WithMaxTokens(cfg.Budget.MaxOutputTokens), llm.WithTemperature(cfg.Budget.Temperature), llm.WithTimeout(cfg.Timeout)}
	var client llm.Client
	var err error
	if cfg.Provider == "deepseek" {
		client, err = providers.NewDeepSeekWithOptions(options...)
	} else {
		// gemini-openai uses Google's compatibility endpoint, not GeminiProvider.
		client, err = providers.NewOpenAIWithOptions(options...)
	}
	if err != nil {
		return err
	}
	instruction := SystemInstruction
	if c.Lookup != nil {
		instruction = strings.Replace(instruction, "该无参数工具每题只需查询一次", "根据用户问题填写工具参数，每题只需查询一次", 1)
	}
	b := builder.NewSimpleBuilder(client).WithSystemPrompt(instruction).WithMaxIterations(cfg.Budget.MaxCalls).WithMaxTokens(cfg.Budget.MaxOutputTokens).WithTemperature(cfg.Budget.Temperature).WithTimeout(cfg.Timeout)
	if hasLookupTool(c) {
		b.WithTools(t)
		if cfg.DirectTools {
			b.WithReturnDirectTools(t.Name())
		}
	}
	agent, err := b.Build()
	if err != nil {
		return err
	}
	stream, err := agent.AsRunnable().Stream(ctx, c.Question)
	if err != nil {
		return err
	}
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Event != nil {
			onText(chunk.Event.Delta)
		}
		if chunk.Done {
			if chunk.Data == nil {
				return fmt.Errorf("最终输出为空")
			}
			onFinal(fmt.Sprint(chunk.Data.Result))
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return io.ErrUnexpectedEOF
}
