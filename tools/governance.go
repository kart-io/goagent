package tools

import "time"

// DispatchGovernance is a configuration snapshot without callbacks or resource
// references; it does not mean any specific business call is authorized.
type DispatchGovernance struct {
	ToolNames                                          []string
	AuthorizationConfigured, PipelineConfigured        bool
	MaxConcurrency, MaxAdmissionConcurrency, MaxQueued int
	Timeout                                            time.Duration
}

// Governance returns an independent configuration snapshot of the dispatcher.
func (d *Dispatcher) Governance() DispatchGovernance {
	if d == nil {
		return DispatchGovernance{}
	}
	return DispatchGovernance{ToolNames: append([]string(nil), d.names...), AuthorizationConfigured: d.config.Authorize != nil, PipelineConfigured: d.config.Pipeline != nil, MaxConcurrency: d.config.MaxConcurrency, MaxAdmissionConcurrency: d.config.MaxAdmissionConcurrency, MaxQueued: d.config.MaxQueued, Timeout: d.config.Timeout}
}
