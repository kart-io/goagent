// Package remote 通过官方 A2A 客户端发现和调用外部 Agent。
package remote

import (
	"context"
	"net/http"
	"net/url"

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
	return a2aclient.NewFromCard(ctx, card, a2aclient.WithDefaultsDisabled(), a2aclient.WithJSONRPCTransport(client), a2aclient.WithRESTTransport(client))
}

type agentTool struct{ *tools.BaseTool }

func (*agentTool) DurableOperationPolicy() durable.OperationPolicy {
	return durable.OperationPolicy{MaxAttempts: 1}
}

// Tool 将 send/get/cancel 暴露为一个工具；人工回复使用 task_id 和结构化 input。
// send 等到完成或等待输入后返回完整任务；不会自动替用户作审批决定。
func Tool(client *a2aclient.Client, name, description string) (interfaces.Tool, error) {
	if client == nil || name == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "A2A 客户端和工具名不能为空")
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
			var value any
			if err = json.Unmarshal(args.Input, &value, json.UseNumber); err != nil {
				return nil, err
			}
			part := a2a.NewDataPart(value)
			if text, ok := value.(string); ok {
				part = a2a.NewTextPart(text)
			}
			result, err = client.SendMessage(ctx, &a2a.SendMessageRequest{Message: &a2a.Message{ID: args.MessageID, Role: a2a.MessageRoleUser, ContextID: args.ContextID, TaskID: a2a.TaskID(args.TaskID), Parts: a2a.ContentParts{part}}})
		default:
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "未知 A2A 操作")
		}
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: result}, nil
	})
	return &agentTool{t}, nil
}
