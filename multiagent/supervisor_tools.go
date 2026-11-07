package multiagent

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/builder"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Member 是 SupervisorTools 的一个成员 Agent。Version 随成员的行为、提示词或工具集变化而提升。
type Member struct {
	Name, Description, Version string
	Factory                    AgentFactory
	Resumable                  bool
	Timeout                    time.Duration // 0 继承 Dispatcher 默认；< 0 构造报错；只在经 Dispatcher 调用时生效
}

const memberArgsSchema = `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}`

// SupervisorTools 把每个成员包装成模型可调用的工具：参数为 {"task": string}，每次调用新建会话。
// 宿主把返回的工具注册到 Supervisor Agent 的 Dispatcher，再经 Dispatcher.Tools() 交给 Agent；
// 调用时 ctx 必须带根预算，持久运行中必须是持久预算。
func SupervisorTools(members ...Member) ([]interfaces.Tool, error) {
	if len(members) == 0 {
		return nil, agentErrors.NewInvalidConfigError("multiagent", "members", "SupervisorTools 需要至少一个成员")
	}
	seen := map[string]bool{}
	out := make([]interfaces.Tool, 0, len(members))
	for _, m := range members {
		switch {
		case m.Name == "" || m.Description == "" || m.Version == "" || m.Factory == nil:
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "成员 %q 需要 Name、Description、Version 与 Factory", m.Name).WithComponent("multiagent").WithOperation("supervisor_tools")
		case seen[m.Name]:
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "成员名 %q 重复", m.Name).WithComponent("multiagent").WithOperation("supervisor_tools")
		case m.Timeout < 0:
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "成员 %q 的 Timeout 不能为负", m.Name).WithComponent("multiagent").WithOperation("supervisor_tools")
		}
		seen[m.Name] = true
		tool, err := tools.NewRunnableToolFactory(m.Factory, tools.RunnableToolConfig[any, *builder.AgentOutput]{
			Name: m.Name, Description: m.Description, ArgsSchema: memberArgsSchema, Version: m.Version,
			Decode: decodeTask, Encode: encodeAgentResult,
			ModelInvoking: true, Resumable: m.Resumable, Timeout: m.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("成员 %q: %w", m.Name, err)
		}
		out = append(out, tool)
	}
	return out, nil
}

// decodeTask 只做类型断言：Schema 已在 Decode 前保证 task 存在且为字符串。
func decodeTask(args map[string]any) (any, error) {
	task, ok := args["task"].(string)
	if !ok {
		return nil, &tools.ArgumentError{Message: "task 必须是字符串"}
	}
	return task, nil
}

func encodeAgentResult(o *builder.AgentOutput) (any, error) {
	if o == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidOutput, "成员 Agent 没有返回输出").WithComponent("multiagent").WithOperation("encode_agent_result")
	}
	return resultText(o.Result)
}
