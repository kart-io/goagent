// Package einocompare isolates Eino dependencies from the GoAgent module.
package einocompare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	bench "github.com/kart-io/goagent/evaluation/agentbench"
)

// Run changes only the framework executor; metering and scoring stay shared.
func Run(ctx context.Context, framework string, c bench.Case, cfg bench.Config) bench.Result {
	switch framework {
	case "goagent":
		return bench.Run(ctx, c, cfg)
	case "eino":
		return bench.RunWithExecutor(ctx, c, cfg, "eino", execute)
	default:
		return bench.Result{Framework: framework, CaseID: c.ID, Error: "unknown framework: " + framework}
	}
}

type evidenceTool struct {
	evidence string
	calls    atomic.Int32
}

func (t *evidenceTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "lookup", Desc: bench.LookupDescription, ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}
func (t *evidenceTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(args), &values); err != nil || values == nil {
		return "", fmt.Errorf("lookup requires a JSON object")
	}
	t.calls.Add(1)
	return t.evidence, nil
}

func execute(ctx context.Context, baseURL string, cfg bench.Config, c bench.Case, onText, onFinal func(string)) (int32, error) {
	temperature := float32(cfg.Budget.Temperature)
	model, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{APIKey: cfg.APIKey, BaseURL: baseURL, Model: cfg.Budget.Model, MaxTokens: &cfg.Budget.MaxOutputTokens, Temperature: &temperature, Timeout: cfg.Timeout})
	if err != nil {
		return 0, err
	}
	lookup := &evidenceTool{evidence: c.Evidence}
	toolsConfig := adk.ToolsConfig{}
	if c.Evidence != "" {
		toolsConfig.ToolsNodeConfig = compose.ToolsNodeConfig{Tools: []tool.BaseTool{lookup}}
		if cfg.DirectTools {
			toolsConfig.ReturnDirectly = map[string]bool{"lookup": true}
		}
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: "comparison", Description: "Shared evaluation task", Instruction: bench.SystemInstruction, Model: model, ToolsConfig: toolsConfig, MaxIterations: cfg.Budget.MaxCalls})
	if err != nil {
		return 0, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	iter := runner.Query(ctx, c.Question)
	finalSeen := false
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return lookup.calls.Load(), event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		output := event.Output.MessageOutput
		message, err := consumeMessage(output, onText)
		if err != nil {
			return lookup.calls.Load(), err
		}
		if message == nil {
			continue
		}
		// EOF/[DONE] only closes the transport. Match GoAgent's requirement
		// for a completed model turn, including turns that request tools.
		// Direct tool results are not model messages and have no finish reason.
		if message.Role == schema.Assistant && (message.ResponseMeta == nil || message.ResponseMeta.FinishReason == "") {
			return lookup.calls.Load(), io.ErrUnexpectedEOF
		}
		if meta := message.ResponseMeta; meta != nil && (meta.FinishReason == "length" || meta.FinishReason == "content_filter") {
			return lookup.calls.Load(), fmt.Errorf("incomplete model output: %s", meta.FinishReason)
		}
		if message.Role == schema.Assistant && len(message.ToolCalls) == 0 {
			if !output.IsStreaming {
				onText(message.Content)
			}
			onFinal(message.Content)
			finalSeen = true
		} else if message.Role == schema.Tool && cfg.DirectTools && lookup.calls.Load() == 1 {
			onText(message.Content)
			onFinal(message.Content)
			finalSeen = true
		}
	}
	if err := ctx.Err(); err != nil {
		return lookup.calls.Load(), err
	}
	if !finalSeen {
		return lookup.calls.Load(), io.ErrUnexpectedEOF
	}
	return lookup.calls.Load(), nil
}

func consumeMessage(output *adk.MessageVariant, onText func(string)) (*schema.Message, error) {
	if !output.IsStreaming {
		return output.Message, nil
	}
	if output.MessageStream == nil {
		return nil, fmt.Errorf("missing message stream")
	}
	defer output.MessageStream.Close()
	var chunks []*schema.Message
	for {
		chunk, err := output.MessageStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			continue
		}
		chunks = append(chunks, chunk)
		if output.Role == schema.Assistant {
			onText(chunk.Content)
		}
	}
	if len(chunks) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return schema.ConcatMessages(chunks)
}
