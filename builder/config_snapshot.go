package builder

import "slices"

// snapshotAgentConfig 隔离构建配置的可变容器，宿主资源与函数仍按引用共享。
func snapshotAgentConfig(config *AgentConfig) *AgentConfig {
	copy := *config
	copy.Reasoning = config.Reasoning.Clone()
	copy.OutputValidators = slices.Clone(config.OutputValidators)
	copy.Offloads = slices.Clone(config.Offloads)
	copy.ReturnDirectTools = slices.Clone(config.ReturnDirectTools)
	copy.PreStep = slices.Clone(config.PreStep)
	copy.RequestHooks = slices.Clone(config.RequestHooks)
	copy.RequestErrorHooks = slices.Clone(config.RequestErrorHooks)
	copy.TurnStopping = slices.Clone(config.TurnStopping)
	copy.PromptSections = slices.Clone(config.PromptSections)
	copy.PromptContexts = slices.Clone(config.PromptContexts)
	return &copy
}
