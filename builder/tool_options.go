package builder

import (
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Tool 配置方法
// 本文件包含 AgentBuilder 的工具配置方法

// WithTools 添加工具到 Agent
func (b *AgentBuilder[S]) WithTools(tools ...interfaces.Tool) *AgentBuilder[S] {
	b.tools = append(b.tools, tools...)
	return b
}

// WithDispatcher 由一个派发器提供完整工具集，不能再混用 WithTools。
func (b *AgentBuilder[S]) WithDispatcher(d *tools.Dispatcher) *AgentBuilder[S] {
	b.dispatcher = d
	b.dispatcherSet = true
	return b
}
