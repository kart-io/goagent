package builder

import (
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Tool 配置方法
// 本文件包含 AgentBuilder 的工具配置方法

// WithTools adds tools to the agent.
func (b *AgentBuilder[S]) WithTools(tools ...interfaces.Tool) *AgentBuilder[S] {
	b.tools = append(b.tools, tools...)
	return b
}

// WithDispatcher provides the complete tool set from one dispatcher; it cannot
// be combined with WithTools.
func (b *AgentBuilder[S]) WithDispatcher(d *tools.Dispatcher) *AgentBuilder[S] {
	b.dispatcher = d
	b.dispatcherSet = true
	return b
}
