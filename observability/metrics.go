package observability

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics Agent 监控指标
type Metrics struct {
	// Agent 执行指标
	AgentExecutions *prometheus.CounterVec
	AgentDuration   *prometheus.HistogramVec
	AgentErrors     *prometheus.CounterVec

	// 工具调用指标
	ToolCalls    *prometheus.CounterVec
	ToolDuration *prometheus.HistogramVec
	ToolErrors   *prometheus.CounterVec

	// 分布式协调指标
	RemoteAgentCalls    *prometheus.CounterVec
	RemoteAgentDuration *prometheus.HistogramVec
	RemoteAgentErrors   *prometheus.CounterVec

	// 服务实例指标
	ServiceInstances *prometheus.GaugeVec
	HealthyInstances *prometheus.GaugeVec

	// 并发执行指标
	ConcurrentExecutions prometheus.Gauge
}

var (
	metricsInstance *Metrics
	metricsOnce     sync.Once
)

// GetMetrics 获取指标实例
func GetMetrics() *Metrics {
	metricsOnce.Do(func() {
		metricsInstance = &Metrics{
			// Agent 执行指标
			AgentExecutions: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "agent_executions_total",
					Help: "Total number of agent executions",
				},
				[]string{"agent_name", "service", "status"},
			),
			AgentDuration: promauto.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "agent_execution_duration_seconds",
					Help:    "Agent execution duration in seconds",
					Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
				},
				[]string{"agent_name", "service"},
			),
			AgentErrors: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "agent_errors_total",
					Help: "Total number of agent execution errors",
				},
				[]string{"agent_name", "service", "error_type"},
			),

			// 工具调用指标
			ToolCalls: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "tool_calls_total",
					Help: "Total number of tool calls",
				},
				[]string{"tool_name", "agent_name", "status"},
			),
			ToolDuration: promauto.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "tool_call_duration_seconds",
					Help:    "Tool call duration in seconds",
					Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
				},
				[]string{"tool_name", "agent_name"},
			),
			ToolErrors: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "tool_errors_total",
					Help: "Total number of tool call errors",
				},
				[]string{"tool_name", "agent_name", "error_type"},
			),

			// 分布式协调指标
			RemoteAgentCalls: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "remote_agent_calls_total",
					Help: "Total number of remote agent calls",
				},
				[]string{"service", "agent_name", "status"},
			),
			RemoteAgentDuration: promauto.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "remote_agent_call_duration_seconds",
					Help:    "Remote agent call duration in seconds",
					Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
				},
				[]string{"service", "agent_name"},
			),
			RemoteAgentErrors: promauto.NewCounterVec(
				prometheus.CounterOpts{
					Name: "remote_agent_errors_total",
					Help: "Total number of remote agent errors",
				},
				[]string{"service", "agent_name", "error_type"},
			),

			// 服务实例指标
			ServiceInstances: promauto.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "service_instances_total",
					Help: "Total number of registered service instances",
				},
				[]string{"service"},
			),
			HealthyInstances: promauto.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "healthy_instances_total",
					Help: "Number of healthy service instances",
				},
				[]string{"service"},
			),

			// 并发执行指标
			ConcurrentExecutions: promauto.NewGauge(
				prometheus.GaugeOpts{
					Name: "concurrent_executions",
					Help: "Current number of concurrent agent executions",
				},
			),
		}
	})

	return metricsInstance
}

// RecordAgentExecution 记录 Agent 执行
func RecordAgentExecution(agentName, service, status string, duration time.Duration) {
	m := GetMetrics()
	m.AgentExecutions.WithLabelValues(agentName, service, status).Inc()
	m.AgentDuration.WithLabelValues(agentName, service).Observe(duration.Seconds())
}

// RecordAgentError 记录 Agent 错误
func RecordAgentError(agentName, service, errorType string) {
	m := GetMetrics()
	m.AgentErrors.WithLabelValues(agentName, service, errorType).Inc()
}

// RecordToolCall 记录工具调用
func RecordToolCall(toolName, agentName, status string, duration time.Duration) {
	m := GetMetrics()
	m.ToolCalls.WithLabelValues(toolName, agentName, status).Inc()
	m.ToolDuration.WithLabelValues(toolName, agentName).Observe(duration.Seconds())
}

// RecordToolError 记录工具错误
func RecordToolError(toolName, agentName, errorType string) {
	m := GetMetrics()
	m.ToolErrors.WithLabelValues(toolName, agentName, errorType).Inc()
}

// RecordRemoteAgentCall 记录远程 Agent 调用
func RecordRemoteAgentCall(service, agentName, status string, duration time.Duration) {
	m := GetMetrics()
	m.RemoteAgentCalls.WithLabelValues(service, agentName, status).Inc()
	m.RemoteAgentDuration.WithLabelValues(service, agentName).Observe(duration.Seconds())
}

// RecordRemoteAgentError 记录远程 Agent 错误
func RecordRemoteAgentError(service, agentName, errorType string) {
	m := GetMetrics()
	m.RemoteAgentErrors.WithLabelValues(service, agentName, errorType).Inc()
}

// UpdateServiceInstances 更新服务实例数量
func UpdateServiceInstances(service string, total, healthy int) {
	m := GetMetrics()
	m.ServiceInstances.WithLabelValues(service).Set(float64(total))
	m.HealthyInstances.WithLabelValues(service).Set(float64(healthy))
}

// IncrementConcurrentExecutions 增加并发执行数
func IncrementConcurrentExecutions() {
	m := GetMetrics()
	m.ConcurrentExecutions.Inc()
}

// DecrementConcurrentExecutions 减少并发执行数
func DecrementConcurrentExecutions() {
	m := GetMetrics()
	m.ConcurrentExecutions.Dec()
}
