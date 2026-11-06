package tools

import "time"

// DispatchGovernance 是不含回调和资源引用的配置快照；不代表具体业务调用已获授权。
type DispatchGovernance struct {
	ToolNames                                          []string
	AuthorizationConfigured, PipelineConfigured        bool
	MaxConcurrency, MaxAdmissionConcurrency, MaxQueued int
	Timeout                                            time.Duration
}

// Governance 返回当前派发器的独立配置快照。
func (d *Dispatcher) Governance() DispatchGovernance {
	if d == nil {
		return DispatchGovernance{}
	}
	return DispatchGovernance{ToolNames: append([]string(nil), d.names...), AuthorizationConfigured: d.config.Authorize != nil, PipelineConfigured: d.config.Pipeline != nil, MaxConcurrency: d.config.MaxConcurrency, MaxAdmissionConcurrency: d.config.MaxAdmissionConcurrency, MaxQueued: d.config.MaxQueued, Timeout: d.config.Timeout}
}
