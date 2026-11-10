// Package remote 通过官方 A2A 客户端发现和调用外部 Agent。
package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Connect 使用宿主 HTTP client 完成能力发现和传输；调用方负责 Destroy。
// 返回官方客户端，直接支持流式发送、重订阅、查询与取消。
// 只使用 Agent Card 中与发现地址同源的接口：宿主 client 可能带凭证，不能被远端声明的地址引向其他主机；
// 接口部署在其他源时，由宿主自行校验 Agent Card 后用 a2aclient.NewFromCard 连接。
func Connect(ctx context.Context, endpoint string, client *http.Client) (*a2aclient.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "A2A 发现地址必须是 HTTP(S) URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	card, err := agentcard.NewResolver(client).Resolve(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	card.SupportedInterfaces = slices.DeleteFunc(card.SupportedInterfaces, func(i *a2a.AgentInterface) bool {
		target, err := url.Parse(i.URL)
		return err != nil || target.Scheme != u.Scheme || !strings.EqualFold(target.Host, u.Host)
	})
	if len(card.SupportedInterfaces) == 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "A2A Agent Card 没有与发现地址同源的接口")
	}
	return a2aclient.NewFromCard(ctx, card, a2aclient.WithDefaultsDisabled(), a2aclient.WithJSONRPCTransport(client), a2aclient.WithRESTTransport(client))
}

type agentTool struct{ *tools.BaseTool }

func (*agentTool) DurableOperationPolicy() durable.OperationPolicy {
	return durable.OperationPolicy{MaxAttempts: 1}
}

// interruptibleAgentTool 在持久运行中把远端的等待输入转为 durable 等待。
type interruptibleAgentTool struct {
	*tools.BaseTool
	version string
}

func (t *interruptibleAgentTool) InterruptibleVersion() (string, bool) { return t.version, true }

// maxInputRounds 限制单次发送的连续输入轮数。
const maxInputRounds = 64

// Option 配置 Tool。
type Option func(*options)

type options struct{ durableInput *string }

// WithDurableInput 让 send 在持久运行中以 durable 等待处理远端的 input-required：每次发送记入日志，
// 任务等待输入时以 durable.AwaitInput 暂停，Continuation 的 Info 是该任务的 JSON；宿主以任意 JSON 恢复后，
// 该输入作为下一条消息发往同一任务，消息 ID 由原 message_id 派生，已完成的发送回放而不重发。
// version 是持久 intent 版本（tools.InterruptibleTool），不能为空；此时工具不再声明持久策略。
// 不在持久运行中时 send 照旧返回等待输入的任务。
func WithDurableInput(version string) Option {
	return func(o *options) { o.durableInput = &version }
}

// Tool 将 send/get/cancel 暴露为一个工具；人工回复使用 task_id 和结构化 input。
// send 等到完成或等待输入后返回完整任务；不会自动替用户作审批决定。
func Tool(client *a2aclient.Client, name, description string, opts ...Option) (interfaces.Tool, error) {
	if client == nil || name == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "A2A 客户端和工具名不能为空")
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.durableInput != nil && *o.durableInput == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "A2A 持久输入需要版本")
	}
	schema := `{"type":"object","properties":{"action":{"type":"string","enum":["send","get","cancel"]},"message_id":{"type":"string"},"context_id":{"type":"string"},"task_id":{"type":"string"},"input":{}},"required":["action"],"additionalProperties":false}`
	t := tools.NewBaseTool(name, description, schema, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if in == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "A2A 工具输入不能为空")
		}
		raw, err := json.Marshal(in.Args)
		if err != nil {
			return nil, err
		}
		var args struct {
			Action    string          `json:"action"`
			MessageID string          `json:"message_id"`
			ContextID string          `json:"context_id"`
			TaskID    string          `json:"task_id"`
			Input     json.RawMessage `json:"input"`
		}
		if err = json.Unmarshal(raw, &args, json.Strict); err != nil {
			return nil, err
		}
		var result any
		switch args.Action {
		case "get":
			if args.TaskID == "" {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "A2A 查询和取消需要 task_id")
			}
			result, err = client.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.TaskID(args.TaskID)})
		case "cancel":
			if args.TaskID == "" {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "A2A 查询和取消需要 task_id")
			}
			result, err = client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: a2a.TaskID(args.TaskID)})
		case "send":
			if args.MessageID == "" || len(args.Input) == 0 {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "A2A 发送需要稳定 message_id 和 input")
			}
			send := func(ctx context.Context, messageID, contextID string, taskID a2a.TaskID, input json.RawMessage) (a2a.SendMessageResult, error) {
				var value any
				if err := json.Unmarshal(input, &value, json.UseNumber); err != nil {
					return nil, err
				}
				part := a2a.NewDataPart(value)
				if text, ok := value.(string); ok {
					part = a2a.NewTextPart(text)
				}
				return client.SendMessage(ctx, &a2a.SendMessageRequest{Message: &a2a.Message{ID: messageID, Role: a2a.MessageRoleUser, ContextID: contextID, TaskID: taskID, Parts: a2a.ContentParts{part}}})
			}
			if o.durableInput != nil && durable.CanWait(ctx) {
				result, err = sendDurable(ctx, send, args.MessageID, args.ContextID, a2a.TaskID(args.TaskID), args.Input)
				break
			}
			result, err = send(ctx, args.MessageID, args.ContextID, a2a.TaskID(args.TaskID), args.Input)
		default:
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "未知 A2A 操作")
		}
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: result}, nil
	})
	if o.durableInput != nil {
		return &interruptibleAgentTool{BaseTool: t, version: *o.durableInput}, nil
	}
	return &agentTool{t}, nil
}

type sendFunc func(ctx context.Context, messageID, contextID string, taskID a2a.TaskID, input json.RawMessage) (a2a.SendMessageResult, error)

// sendDurable 把每次发送记为子操作，任务等待输入时以 durable.AwaitInput 暂停；恢复时重新进入，
// 已完成的发送与已收到的输入从日志回放。发送结果未知时（MaxAttempts 1）交由宿主处理，不盲目重发。
// 返回最后一次发送结果的 JSON。
func sendDurable(ctx context.Context, send sendFunc, messageID, contextID string, taskID a2a.TaskID, input json.RawMessage) (json.RawMessage, error) {
	base := messageID
	for step := 0; ; step++ {
		intent, err := json.Marshal(map[string]any{"message_id": messageID, "context_id": contextID, "task_id": taskID, "input": input})
		if err != nil {
			return nil, err
		}
		record, err := durable.Do(ctx, fmt.Sprintf("a2a/send/%d", step), intent, durable.OperationPolicy{MaxAttempts: 1}, func(ctx context.Context) (json.RawMessage, error) {
			result, err := send(ctx, messageID, contextID, taskID, input)
			if err != nil {
				return nil, err
			}
			return json.Marshal(result)
		})
		if err != nil {
			return nil, err
		}
		var task struct {
			ID        a2a.TaskID     `json:"id"`
			ContextID string         `json:"contextId"`
			Status    a2a.TaskStatus `json:"status"`
		}
		if err = json.Unmarshal(record.Value, &task); err != nil || task.Status.State != a2a.TaskStateInputRequired {
			return record.Value, nil
		}
		if step >= maxInputRounds {
			return nil, agentErrors.Newf(agentErrors.CodeToolExecution, "A2A 输入轮数超过 %d", maxInputRounds)
		}
		if input, err = durable.AwaitInput(ctx, fmt.Sprintf("a2a/input/%d", step), record.Value, nil); err != nil {
			return nil, err
		}
		messageID, contextID, taskID = fmt.Sprintf("%s/input/%d", base, step), task.ContextID, task.ID
	}
}
