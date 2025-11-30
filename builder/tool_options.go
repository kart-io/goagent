package builder

import "github.com/kart-io/goagent/interfaces"

// Tool 配置方法
// 本文件包含 AgentBuilder 的工具配置方法

// WithTools 添加工具到 Agent
func (b *AgentBuilder[C, S]) WithTools(tools ...interfaces.Tool) *AgentBuilder[C, S] {
	b.tools = append(b.tools, tools...)
	return b
}
