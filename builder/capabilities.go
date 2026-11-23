package builder

import (
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// nativeToolLoop 返回实际执行工具循环的客户端；零工具仍走纯聊天。
func nativeToolLoop(client llm.Client, tools int) llm.ToolStreamClient {
	native, _ := client.(llm.ToolStreamClient)
	if tools == 0 {
		return nil
	}
	return native
}

// validateLoopCapabilities 在初始化或访问宿主资源前拒绝会被执行模式忽略的配置。
func (b *AgentBuilder[S]) validateLoopCapabilities(toolset []interfaces.Tool) error {
	if nativeToolLoop(b.llmClient, len(toolset)) != nil {
		return nil
	}
	var unavailable []string
	for _, feature := range []struct {
		name    string
		enabled bool
	}{
		{"PreStep", len(b.config.PreStep) > 0},
		{"RequestHook", len(b.config.RequestHooks) > 0},
		{"TurnStopping", len(b.config.TurnStopping) > 0},
		{"PromptSections", len(b.config.PromptSections) > 0},
		{"PromptContexts", len(b.config.PromptContexts) > 0},
	} {
		if feature.enabled {
			unavailable = append(unavailable, feature.name)
		}
	}
	if len(unavailable) > 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "%s需要ToolStreamClient和至少一个工具，当前配置没有原生工具循环", strings.Join(unavailable, "、")).WithComponent("builder").WithOperation("validate_loop_capabilities")
	}
	return nil
}
