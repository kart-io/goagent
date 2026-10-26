package agentbench

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/evaluation"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// Case 的 Evidence 是查询工具结果；空值表示无工具任务。
type Case struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Evidence string `json:"evidence,omitempty"`
}

type Config struct {
	// DirectTools 仅用于工具结果已是完整答案的独立题集，显式启用。
	DirectTools bool              `json:"direct_tools"`
	APIKey      string            `json:"-"`
	BaseURL     string            `json:"-"`
	Budget      evaluation.Budget `json:"budget"`
	Timeout     time.Duration     `json:"timeout"`
}

type Result struct {
	Framework     string                  `json:"framework"`
	CaseID        string                  `json:"case_id"`
	Answer        string                  `json:"answer"`
	Success       bool                    `json:"success"`
	Error         string                  `json:"error,omitempty"`
	DurationMS    float64                 `json:"duration_ms"`
	FirstTextMS   *float64                `json:"first_text_ms,omitempty"`
	ToolCalls     int32                   `json:"tool_calls"`
	Measurements  evaluation.Measurements `json:"measurements"`
	UsageComplete bool                    `json:"usage_complete"`
}

const instruction = "完成用户任务。若存在 lookup 工具，必须先调用该工具，不能仅回复将要查询。该无参数工具每题只需查询一次，收到成功结果后不要重复查询。若用户要求原样返回工具文本，成功工具返回的非空正文就是完整答案，保留所有字符、语言和换行，不翻译、不加引号或 Markdown，不将合法状态值改成 UNKNOWN。若用户要求事实提取或计算，则依据工具结果回答，只有无法从资料得出所问事实时才输出 UNKNOWN。最终只输出答案本身，不要解释。"
const toolDescription = "查询本题的参考资料，无需参数。"

// lookup 返回本题的工具资料，并记录实际调用次数。
type lookup struct {
	evidence string
	calls    atomic.Int32
}

func (t *lookup) Name() string        { return "lookup" }
func (t *lookup) Description() string { return toolDescription }
func (t *lookup) ArgsSchema() string  { return `{"type":"object","properties":{}}` }
func (t *lookup) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.calls.Add(1)
	return &interfaces.ToolOutput{Result: t.evidence, Success: true}, nil
}

// Run 执行一个 GoAgent 评测任务。失败也保留结果；首次文本时间不等同于工具参数首帧时间。
func Run(parent context.Context, c Case, config Config) Result {
	result := Result{Framework: "goagent", CaseID: c.ID}
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
	lookupTool := &lookup{evidence: c.Evidence}
	runErr := runGoAgent(ctx, proxy.URL, config, c, lookupTool, onText, func(final string) { text.Reset(); text.WriteString(final) })
	cancel()
	proxy.Close()
	result.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	result.Answer = strings.TrimSpace(text.String())
	result.ToolCalls = lookupTool.calls.Load()
	result.Measurements = meter.Measurements()
	result.UsageComplete = result.Measurements.Calls > 0 && result.Measurements.Calls == result.Measurements.UsageReports
	result.Success = runErr == nil && result.Answer == c.Answer && (c.Evidence == "" || result.ToolCalls > 0)
	if runErr != nil {
		result.Error = runErr.Error()
	}
	return result
}

func runGoAgent(ctx context.Context, baseURL string, cfg Config, c Case, t *lookup, onText func(string), onFinal func(string)) error {
	client, err := providers.NewOpenAIWithOptions(llm.WithAPIKey(cfg.APIKey), llm.WithBaseURL(baseURL), llm.WithModel(cfg.Budget.Model), llm.WithMaxTokens(cfg.Budget.MaxOutputTokens), llm.WithTemperature(cfg.Budget.Temperature))
	if err != nil {
		return err
	}
	b := builder.NewSimpleBuilder(client).WithSystemPrompt(instruction).WithMaxIterations(cfg.Budget.MaxCalls).WithMaxTokens(cfg.Budget.MaxOutputTokens).WithTemperature(cfg.Budget.Temperature).WithTimeout(cfg.Timeout)
	if c.Evidence != "" {
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
