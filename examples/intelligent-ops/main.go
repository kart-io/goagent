// Package main 演示智能运维 Agent 分层治理体系
//
// 本示例基于《智能运维 Agent 演进与实践体系》设计方案，实现了三层 Agent 架构：
//   - Layer 1: 自动化执行 (SOPAutomationAgent) - 处理确定性故障
//   - Layer 2: 巡检感知 (InspectionAgent) - 非 AI 驱动的数据采集
//   - Layer 3: AI 根因分析 (RootCauseAnalysisAgent) - 复杂故障推理
//
// 演进路径：标准化 (SOP) -> 数据驱动 (Data) -> 智能化 (AI Analysis)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"
	loggercore "github.com/kart-io/logger/core"
)

// =============================================================================
// 核心类型定义
// =============================================================================

// AlertSeverity 告警级别
type AlertSeverity string

const (
	SeverityCritical AlertSeverity = "critical"
	SeverityHigh     AlertSeverity = "high"
	SeverityMedium   AlertSeverity = "medium"
	SeverityLow      AlertSeverity = "low"
)

// AlertType 告警类型
type AlertType string

const (
	AlertTypeDiskFull     AlertType = "disk_full"
	AlertTypeCPUHigh      AlertType = "cpu_high"
	AlertTypeMemoryHigh   AlertType = "memory_high"
	AlertTypePodCrash     AlertType = "pod_crash"
	AlertTypeServiceError AlertType = "service_error"
	AlertTypeNetworkIssue AlertType = "network_issue"
	AlertTypeUnknown      AlertType = "unknown"
)

// Alert 运维告警
type Alert struct {
	ID          string                 `json:"id"`
	Type        AlertType              `json:"type"`
	Severity    AlertSeverity          `json:"severity"`
	Source      string                 `json:"source"`
	Message     string                 `json:"message"`
	Timestamp   time.Time              `json:"timestamp"`
	Labels      map[string]string      `json:"labels,omitempty"`
	Annotations map[string]string      `json:"annotations,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// SOP 标准作业程序
type SOP struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	AlertType   AlertType         `json:"alert_type"`
	Steps       []SOPStep         `json:"steps"`
	Rollback    []SOPStep         `json:"rollback,omitempty"`
	Timeout     time.Duration     `json:"timeout"`
	AutoExecute bool              `json:"auto_execute"`
	Tags        []string          `json:"tags,omitempty"`
	Conditions  map[string]string `json:"conditions,omitempty"`
}

// SOPStep SOP 执行步骤
type SOPStep struct {
	Order      int                    `json:"order"`
	Name       string                 `json:"name"`
	Action     string                 `json:"action"`
	Command    string                 `json:"command,omitempty"`
	Script     string                 `json:"script,omitempty"`
	Timeout    time.Duration          `json:"timeout"`
	RetryCount int                    `json:"retry_count"`
	Params     map[string]interface{} `json:"params,omitempty"`
	OnFailure  string                 `json:"on_failure,omitempty"`
}

// InspectionResult 巡检结果
type InspectionResult struct {
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status"`
	Data      map[string]interface{} `json:"data"`
	Anomalies []Anomaly              `json:"anomalies,omitempty"`
}

// Anomaly 异常点
type Anomaly struct {
	Type        string  `json:"type"`
	Component   string  `json:"component"`
	Description string  `json:"description"`
	Severity    string  `json:"severity"`
	Value       float64 `json:"value,omitempty"`
	Threshold   float64 `json:"threshold,omitempty"`
}

// RootCauseResult 根因分析结果
type RootCauseResult struct {
	RootCause     string                 `json:"root_cause"`
	Confidence    float64                `json:"confidence"`
	Evidence      []string               `json:"evidence"`
	RelatedAlerts []string               `json:"related_alerts,omitempty"`
	Suggestions   []string               `json:"suggestions"`
	Analysis      string                 `json:"analysis"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// OpsResult 运维处理结果
type OpsResult struct {
	AlertID   string                 `json:"alert_id"`
	Status    string                 `json:"status"`
	Layer     string                 `json:"layer"`
	Handler   string                 `json:"handler"`
	Actions   []ActionRecord         `json:"actions"`
	Duration  time.Duration          `json:"duration"`
	RootCause *RootCauseResult       `json:"root_cause,omitempty"`
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ActionRecord 操作记录
type ActionRecord struct {
	Timestamp time.Time     `json:"timestamp"`
	Action    string        `json:"action"`
	Result    string        `json:"result"`
	Success   bool          `json:"success"`
	Duration  time.Duration `json:"duration"`
}

// =============================================================================
// 简单日志实现
// =============================================================================

type simpleLogger struct {
	prefix string
}

func newLogger(prefix string) *simpleLogger {
	return &simpleLogger{prefix: prefix}
}

func (l *simpleLogger) log(level, msg string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	if len(args) > 0 {
		fmt.Printf("[%s] %s [%s] %s %v\n", timestamp, level, l.prefix, msg, args)
	} else {
		fmt.Printf("[%s] %s [%s] %s\n", timestamp, level, l.prefix, msg)
	}
}

func (l *simpleLogger) Debug(args ...interface{}) { l.log("DEBUG", fmt.Sprint(args...)) }
func (l *simpleLogger) Info(args ...interface{})  { l.log("INFO ", fmt.Sprint(args...)) }
func (l *simpleLogger) Warn(args ...interface{})  { l.log("WARN ", fmt.Sprint(args...)) }
func (l *simpleLogger) Error(args ...interface{}) { l.log("ERROR", fmt.Sprint(args...)) }
func (l *simpleLogger) Fatal(args ...interface{}) { l.log("FATAL", fmt.Sprint(args...)); os.Exit(1) }
func (l *simpleLogger) Debugf(template string, args ...interface{}) {
	l.log("DEBUG", fmt.Sprintf(template, args...))
}
func (l *simpleLogger) Infof(template string, args ...interface{}) {
	l.log("INFO ", fmt.Sprintf(template, args...))
}
func (l *simpleLogger) Warnf(template string, args ...interface{}) {
	l.log("WARN ", fmt.Sprintf(template, args...))
}
func (l *simpleLogger) Errorf(template string, args ...interface{}) {
	l.log("ERROR", fmt.Sprintf(template, args...))
}
func (l *simpleLogger) Fatalf(template string, args ...interface{}) {
	l.log("FATAL", fmt.Sprintf(template, args...))
	os.Exit(1)
}
func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {
	l.log("DEBUG", msg, keysAndValues...)
}
func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{}) {
	l.log("INFO ", msg, keysAndValues...)
}
func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{}) {
	l.log("WARN ", msg, keysAndValues...)
}
func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {
	l.log("ERROR", msg, keysAndValues...)
}
func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	l.log("FATAL", msg, keysAndValues...)
	os.Exit(1)
}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Flush() error                              { return nil }

// =============================================================================
// Layer 1: SOP 自动化执行 Agent
// =============================================================================

// SOPAutomationAgent 自动化执行 Agent
// 处理确定性故障，基于 SOP 执行标准化流程
type SOPAutomationAgent struct {
	*multiagent.BaseCollaborativeAgent
	sopRegistry map[AlertType]*SOP
	llmClient   llm.Client
	logger      *simpleLogger
	mu          sync.RWMutex
}

// NewSOPAutomationAgent 创建 SOP 自动化执行 Agent
func NewSOPAutomationAgent(
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
) *SOPAutomationAgent {
	agent := &SOPAutomationAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(
			"sop-automation",
			"自动化执行 Agent - 处理确定性故障",
			multiagent.RoleWorker,
			system,
		),
		sopRegistry: make(map[AlertType]*SOP),
		llmClient:   llmClient,
		logger:      newLogger("SOP-Agent"),
	}

	// 注册预定义的 SOP
	agent.registerDefaultSOPs()

	return agent
}

// registerDefaultSOPs 注册默认的 SOP
func (a *SOPAutomationAgent) registerDefaultSOPs() {
	// SOP-001: 磁盘清理
	a.RegisterSOP(&SOP{
		ID:          "SOP-001",
		Name:        "磁盘空间清理",
		Description: "自动清理磁盘空间，包括日志轮转和临时文件清理",
		AlertType:   AlertTypeDiskFull,
		AutoExecute: true,
		Timeout:     5 * time.Minute,
		Steps: []SOPStep{
			{
				Order:      1,
				Name:       "检查磁盘使用情况",
				Action:     "check_disk",
				Command:    "df -h",
				Timeout:    30 * time.Second,
				RetryCount: 1,
			},
			{
				Order:      2,
				Name:       "清理过期日志",
				Action:     "cleanup_logs",
				Command:    "find /var/log -name '*.log' -mtime +7 -delete",
				Timeout:    2 * time.Minute,
				RetryCount: 2,
				OnFailure:  "continue",
			},
			{
				Order:      3,
				Name:       "清理临时文件",
				Action:     "cleanup_temp",
				Command:    "rm -rf /tmp/*",
				Timeout:    1 * time.Minute,
				RetryCount: 1,
				OnFailure:  "continue",
			},
			{
				Order:      4,
				Name:       "验证清理结果",
				Action:     "verify_cleanup",
				Command:    "df -h",
				Timeout:    30 * time.Second,
				RetryCount: 1,
			},
		},
	})

	// SOP-002: Pod 重启
	a.RegisterSOP(&SOP{
		ID:          "SOP-002",
		Name:        "Pod 重启恢复",
		Description: "自动重启崩溃的 Pod",
		AlertType:   AlertTypePodCrash,
		AutoExecute: true,
		Timeout:     3 * time.Minute,
		Steps: []SOPStep{
			{
				Order:      1,
				Name:       "获取 Pod 状态",
				Action:     "get_pod_status",
				Command:    "kubectl get pod ${POD_NAME} -n ${NAMESPACE}",
				Timeout:    30 * time.Second,
				RetryCount: 2,
			},
			{
				Order:      2,
				Name:       "删除异常 Pod",
				Action:     "delete_pod",
				Command:    "kubectl delete pod ${POD_NAME} -n ${NAMESPACE}",
				Timeout:    1 * time.Minute,
				RetryCount: 2,
			},
			{
				Order:      3,
				Name:       "等待 Pod 重建",
				Action:     "wait_pod_ready",
				Command:    "kubectl wait --for=condition=ready pod -l app=${APP_NAME} -n ${NAMESPACE} --timeout=60s",
				Timeout:    90 * time.Second,
				RetryCount: 1,
			},
		},
	})

	// SOP-003: 内存清理
	a.RegisterSOP(&SOP{
		ID:          "SOP-003",
		Name:        "内存清理",
		Description: "清理系统缓存释放内存",
		AlertType:   AlertTypeMemoryHigh,
		AutoExecute: true,
		Timeout:     2 * time.Minute,
		Steps: []SOPStep{
			{
				Order:      1,
				Name:       "检查内存使用",
				Action:     "check_memory",
				Command:    "free -h",
				Timeout:    30 * time.Second,
				RetryCount: 1,
			},
			{
				Order:      2,
				Name:       "清理系统缓存",
				Action:     "clear_cache",
				Command:    "sync && echo 3 > /proc/sys/vm/drop_caches",
				Timeout:    30 * time.Second,
				RetryCount: 1,
			},
			{
				Order:      3,
				Name:       "验证内存释放",
				Action:     "verify_memory",
				Command:    "free -h",
				Timeout:    30 * time.Second,
				RetryCount: 1,
			},
		},
	})
}

// RegisterSOP 注册 SOP
func (a *SOPAutomationAgent) RegisterSOP(sop *SOP) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sopRegistry[sop.AlertType] = sop
	a.logger.Infof("已注册 SOP: %s (%s)", sop.ID, sop.Name)
}

// GetSOP 获取 SOP
func (a *SOPAutomationAgent) GetSOP(alertType AlertType) (*SOP, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	sop, exists := a.sopRegistry[alertType]
	return sop, exists
}

// CanHandle 判断是否能处理该告警
func (a *SOPAutomationAgent) CanHandle(alert *Alert) bool {
	_, exists := a.GetSOP(alert.Type)
	return exists
}

// Collaborate 执行协作任务
func (a *SOPAutomationAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	startTime := time.Now()
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Subtask:   task.Input,
		Status:    multiagent.TaskStatusExecuting,
		StartTime: startTime,
	}

	// 解析告警
	alert, err := a.parseAlert(task.Input)
	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("解析告警失败: %w", err)
	}

	a.logger.Infof("开始处理告警: %s (%s)", alert.ID, alert.Type)

	// 获取对应的 SOP
	sop, exists := a.GetSOP(alert.Type)
	if !exists {
		assignment.Status = multiagent.TaskStatusFailed
		assignment.Result = &OpsResult{
			AlertID: alert.ID,
			Status:  "no_sop",
			Layer:   "layer1",
			Handler: a.Name(),
			Message: fmt.Sprintf("未找到告警类型 %s 对应的 SOP", alert.Type),
		}
		return assignment, nil
	}

	// 使用 LLM 确认意图（可选）
	if a.llmClient != nil {
		confirmed, err := a.confirmIntent(ctx, alert, sop)
		if err != nil {
			a.logger.Warnf("LLM 意图确认失败，继续执行: %v", err)
		} else if !confirmed {
			a.logger.Warnf("LLM 建议不执行 SOP，转交人工处理")
			assignment.Status = multiagent.TaskStatusCompleted
			assignment.Result = &OpsResult{
				AlertID: alert.ID,
				Status:  "escalated",
				Layer:   "layer1",
				Handler: a.Name(),
				Message: "LLM 建议转交人工处理",
			}
			return assignment, nil
		}
	}

	// 执行 SOP
	result := a.executeSOP(ctx, alert, sop)
	result.Duration = time.Since(startTime)

	assignment.Status = multiagent.TaskStatusCompleted
	assignment.Result = result
	assignment.EndTime = time.Now()

	return assignment, nil
}

// parseAlert 解析告警
func (a *SOPAutomationAgent) parseAlert(input interface{}) (*Alert, error) {
	switch v := input.(type) {
	case *Alert:
		return v, nil
	case Alert:
		return &v, nil
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var alert Alert
		if err := json.Unmarshal(data, &alert); err != nil {
			return nil, err
		}
		return &alert, nil
	default:
		return nil, fmt.Errorf("不支持的输入类型: %T", input)
	}
}

// confirmIntent 使用 LLM 确认执行意图
func (a *SOPAutomationAgent) confirmIntent(ctx context.Context, alert *Alert, sop *SOP) (bool, error) {
	prompt := fmt.Sprintf(`你是一个运维自动化助手。请分析以下告警并确认是否应该自动执行对应的 SOP。

告警信息:
- ID: %s
- 类型: %s
- 级别: %s
- 消息: %s

对应 SOP:
- 名称: %s
- 描述: %s
- 步骤数: %d

请回答 "YES" 如果应该自动执行，"NO" 如果需要人工介入。只需回答 YES 或 NO。`,
		alert.ID, alert.Type, alert.Severity, alert.Message,
		sop.Name, sop.Description, len(sop.Steps))

	resp, err := a.llmClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "你是一个专业的运维自动化助手，负责判断告警是否适合自动处理。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   10,
		Temperature: 0.1,
	})
	if err != nil {
		return false, err
	}

	return strings.Contains(strings.ToUpper(resp.Content), "YES"), nil
}

// executeSOP 执行 SOP
func (a *SOPAutomationAgent) executeSOP(ctx context.Context, alert *Alert, sop *SOP) *OpsResult {
	result := &OpsResult{
		AlertID: alert.ID,
		Status:  "executing",
		Layer:   "layer1",
		Handler: a.Name(),
		Actions: make([]ActionRecord, 0, len(sop.Steps)),
	}

	a.logger.Infof("执行 SOP: %s (%s)", sop.ID, sop.Name)

	for _, step := range sop.Steps {
		a.logger.Infof("  步骤 %d: %s", step.Order, step.Name)

		stepStart := time.Now()
		action := ActionRecord{
			Timestamp: stepStart,
			Action:    step.Name,
		}

		// 模拟执行（实际环境中应执行真实命令）
		success := a.simulateStepExecution(step)

		action.Duration = time.Since(stepStart)
		action.Success = success
		if success {
			action.Result = "执行成功"
			a.logger.Infof("    ✓ %s 完成", step.Name)
		} else {
			action.Result = "执行失败"
			a.logger.Warnf("    ✗ %s 失败", step.Name)

			if step.OnFailure != "continue" {
				result.Status = "failed"
				result.Message = fmt.Sprintf("步骤 %d (%s) 执行失败", step.Order, step.Name)
				result.Actions = append(result.Actions, action)
				return result
			}
		}

		result.Actions = append(result.Actions, action)
	}

	result.Status = "success"
	result.Message = fmt.Sprintf("SOP %s 执行成功，共 %d 个步骤", sop.ID, len(sop.Steps))

	return result
}

// simulateStepExecution 模拟步骤执行
func (a *SOPAutomationAgent) simulateStepExecution(step SOPStep) bool {
	// 模拟执行延迟
	time.Sleep(100 * time.Millisecond)
	// 模拟 95% 成功率
	return rand.Float64() < 0.95
}

// =============================================================================
// Layer 2: 巡检感知 Agent
// =============================================================================

// InspectionAgent 巡检感知 Agent
// 非 AI 驱动的数据采集和风险感知
type InspectionAgent struct {
	*multiagent.BaseCollaborativeAgent
	dataSources []DataSource
	logger      *simpleLogger
	mu          sync.RWMutex
}

// DataSource 数据源接口
type DataSource interface {
	Name() string
	Type() string
	Collect(ctx context.Context) (*InspectionResult, error)
}

// NewInspectionAgent 创建巡检感知 Agent
func NewInspectionAgent(system *multiagent.MultiAgentSystem) *InspectionAgent {
	agent := &InspectionAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(
			"inspection",
			"巡检感知 Agent - 数据采集与风险感知",
			multiagent.RoleWorker,
			system,
		),
		dataSources: make([]DataSource, 0),
		logger:      newLogger("Inspection"),
	}

	// 注册默认数据源
	agent.RegisterDataSource(&PrometheusDataSource{})
	agent.RegisterDataSource(&KubernetesDataSource{})
	agent.RegisterDataSource(&LogDataSource{})

	return agent
}

// RegisterDataSource 注册数据源
func (a *InspectionAgent) RegisterDataSource(ds DataSource) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dataSources = append(a.dataSources, ds)
	a.logger.Infof("已注册数据源: %s (%s)", ds.Name(), ds.Type())
}

// Collaborate 执行协作任务
func (a *InspectionAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	startTime := time.Now()
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Subtask:   task.Input,
		Status:    multiagent.TaskStatusExecuting,
		StartTime: startTime,
	}

	a.logger.Info("开始执行巡检数据采集")

	// 并行采集所有数据源
	a.mu.RLock()
	sources := make([]DataSource, len(a.dataSources))
	copy(sources, a.dataSources)
	a.mu.RUnlock()

	results := make([]*InspectionResult, 0, len(sources))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, ds := range sources {
		wg.Add(1)
		go func(source DataSource) {
			defer wg.Done()

			result, err := source.Collect(ctx)
			if err != nil {
				a.logger.Warnf("数据源 %s 采集失败: %v", source.Name(), err)
				return
			}

			mu.Lock()
			results = append(results, result)
			mu.Unlock()

			a.logger.Infof("  ✓ %s 采集完成", source.Name())
		}(ds)
	}

	wg.Wait()

	// 汇总分析
	summary := a.analyzeResults(results)

	assignment.Status = multiagent.TaskStatusCompleted
	assignment.Result = summary
	assignment.EndTime = time.Now()

	return assignment, nil
}

// analyzeResults 分析巡检结果
func (a *InspectionAgent) analyzeResults(results []*InspectionResult) map[string]interface{} {
	summary := map[string]interface{}{
		"timestamp":    time.Now(),
		"source_count": len(results),
		"results":      results,
		"anomalies":    make([]Anomaly, 0),
		"status":       "healthy",
	}

	// 汇总所有异常
	allAnomalies := make([]Anomaly, 0)
	for _, result := range results {
		allAnomalies = append(allAnomalies, result.Anomalies...)
	}
	summary["anomalies"] = allAnomalies

	// 判断整体状态
	if len(allAnomalies) > 0 {
		hasCritical := false
		for _, anomaly := range allAnomalies {
			if anomaly.Severity == "critical" || anomaly.Severity == "high" {
				hasCritical = true
				break
			}
		}
		if hasCritical {
			summary["status"] = "critical"
		} else {
			summary["status"] = "warning"
		}
	}

	return summary
}

// =============================================================================
// 数据源实现
// =============================================================================

// PrometheusDataSource Prometheus 数据源
type PrometheusDataSource struct{}

func (ds *PrometheusDataSource) Name() string { return "prometheus" }
func (ds *PrometheusDataSource) Type() string { return "metrics" }

func (ds *PrometheusDataSource) Collect(ctx context.Context) (*InspectionResult, error) {
	// 模拟 Prometheus 数据采集
	time.Sleep(50 * time.Millisecond)

	cpuUsage := 45.0 + rand.Float64()*30
	memUsage := 60.0 + rand.Float64()*25
	diskUsage := 70.0 + rand.Float64()*20

	result := &InspectionResult{
		Source:    "prometheus",
		Timestamp: time.Now(),
		Type:      "metrics",
		Status:    "ok",
		Data: map[string]interface{}{
			"cpu_usage_percent":    cpuUsage,
			"memory_usage_percent": memUsage,
			"disk_usage_percent":   diskUsage,
			"request_latency_p99":  150 + rand.Float64()*100,
			"error_rate":           0.01 + rand.Float64()*0.02,
		},
		Anomalies: make([]Anomaly, 0),
	}

	// 检测异常
	if cpuUsage > 80 {
		result.Anomalies = append(result.Anomalies, Anomaly{
			Type:        "high_cpu",
			Component:   "system",
			Description: "CPU 使用率过高",
			Severity:    "high",
			Value:       cpuUsage,
			Threshold:   80,
		})
	}
	if memUsage > 85 {
		result.Anomalies = append(result.Anomalies, Anomaly{
			Type:        "high_memory",
			Component:   "system",
			Description: "内存使用率过高",
			Severity:    "high",
			Value:       memUsage,
			Threshold:   85,
		})
	}
	if diskUsage > 90 {
		result.Anomalies = append(result.Anomalies, Anomaly{
			Type:        "high_disk",
			Component:   "system",
			Description: "磁盘使用率过高",
			Severity:    "critical",
			Value:       diskUsage,
			Threshold:   90,
		})
	}

	return result, nil
}

// KubernetesDataSource Kubernetes 数据源
type KubernetesDataSource struct{}

func (ds *KubernetesDataSource) Name() string { return "kubernetes" }
func (ds *KubernetesDataSource) Type() string { return "events" }

func (ds *KubernetesDataSource) Collect(ctx context.Context) (*InspectionResult, error) {
	// 模拟 Kubernetes 数据采集
	time.Sleep(50 * time.Millisecond)

	podCount := 10 + rand.Intn(5)
	readyPods := podCount - rand.Intn(2)

	result := &InspectionResult{
		Source:    "kubernetes",
		Timestamp: time.Now(),
		Type:      "events",
		Status:    "ok",
		Data: map[string]interface{}{
			"total_pods":   podCount,
			"ready_pods":   readyPods,
			"pending_pods": podCount - readyPods,
			"node_count":   3,
			"namespace":    "production",
			"recent_events": []string{
				"Pod scheduled successfully",
				"Container started",
				"Readiness probe succeeded",
			},
		},
		Anomalies: make([]Anomaly, 0),
	}

	// 检测 Pod 异常
	if readyPods < podCount {
		result.Anomalies = append(result.Anomalies, Anomaly{
			Type:        "pod_not_ready",
			Component:   "kubernetes",
			Description: fmt.Sprintf("%d 个 Pod 未就绪", podCount-readyPods),
			Severity:    "medium",
		})
	}

	return result, nil
}

// LogDataSource 日志数据源
type LogDataSource struct{}

func (ds *LogDataSource) Name() string { return "logs" }
func (ds *LogDataSource) Type() string { return "logs" }

func (ds *LogDataSource) Collect(ctx context.Context) (*InspectionResult, error) {
	// 模拟日志数据采集
	time.Sleep(50 * time.Millisecond)

	errorCount := rand.Intn(10)
	warnCount := rand.Intn(20)

	result := &InspectionResult{
		Source:    "logs",
		Timestamp: time.Now(),
		Type:      "logs",
		Status:    "ok",
		Data: map[string]interface{}{
			"error_count":   errorCount,
			"warning_count": warnCount,
			"info_count":    100 + rand.Intn(50),
			"sample_errors": []string{
				"[ERROR] Connection timeout to database",
				"[ERROR] Request validation failed",
			},
		},
		Anomalies: make([]Anomaly, 0),
	}

	// 检测错误激增
	if errorCount > 5 {
		result.Anomalies = append(result.Anomalies, Anomaly{
			Type:        "error_spike",
			Component:   "application",
			Description: fmt.Sprintf("错误日志激增: %d 条", errorCount),
			Severity:    "high",
			Value:       float64(errorCount),
			Threshold:   5,
		})
	}

	return result, nil
}

// =============================================================================
// Layer 3: AI 根因分析 Agent
// =============================================================================

// RootCauseAnalysisAgent AI 根因分析 Agent
// 处理复杂故障的 LLM 推理分析
type RootCauseAnalysisAgent struct {
	*multiagent.BaseCollaborativeAgent
	llmClient       llm.Client
	inspectionAgent *InspectionAgent
	logger          *simpleLogger
}

// NewRootCauseAnalysisAgent 创建 AI 根因分析 Agent
func NewRootCauseAnalysisAgent(
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
	inspectionAgent *InspectionAgent,
) *RootCauseAnalysisAgent {
	return &RootCauseAnalysisAgent{
		BaseCollaborativeAgent: multiagent.NewBaseCollaborativeAgent(
			"root-cause-analysis",
			"AI 根因分析 Agent - 复杂故障推理",
			multiagent.RoleSpecialist,
			system,
		),
		llmClient:       llmClient,
		inspectionAgent: inspectionAgent,
		logger:          newLogger("RCA-Agent"),
	}
}

// Collaborate 执行协作任务
func (a *RootCauseAnalysisAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	startTime := time.Now()
	assignment := &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Subtask:   task.Input,
		Status:    multiagent.TaskStatusExecuting,
		StartTime: startTime,
	}

	// 解析告警
	alert, err := a.parseAlert(task.Input)
	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("解析告警失败: %w", err)
	}

	a.logger.Infof("开始 AI 根因分析: %s", alert.ID)

	// Step 1: 获取巡检数据
	a.logger.Info("  Step 1: 获取巡检数据...")
	inspectionData, err := a.collectInspectionData(ctx)
	if err != nil {
		a.logger.Warnf("  获取巡检数据失败: %v", err)
	}

	// Step 2: 构建分析上下文
	a.logger.Info("  Step 2: 构建分析上下文...")
	analysisContext := a.buildAnalysisContext(alert, inspectionData)

	// Step 3: 调用 LLM 进行根因分析
	a.logger.Info("  Step 3: 执行 AI 根因分析...")
	result, err := a.analyzeRootCause(ctx, analysisContext)
	if err != nil {
		assignment.Status = multiagent.TaskStatusFailed
		return assignment, fmt.Errorf("根因分析失败: %w", err)
	}

	// 构建最终结果
	opsResult := &OpsResult{
		AlertID:   alert.ID,
		Status:    "analyzed",
		Layer:     "layer3",
		Handler:   a.Name(),
		RootCause: result,
		Duration:  time.Since(startTime),
		Message:   "AI 根因分析完成",
		Metadata: map[string]interface{}{
			"inspection_data": inspectionData,
		},
	}

	assignment.Status = multiagent.TaskStatusCompleted
	assignment.Result = opsResult
	assignment.EndTime = time.Now()

	a.logger.Infof("  ✓ 分析完成，根因: %s (置信度: %.1f%%)", result.RootCause, result.Confidence*100)

	return assignment, nil
}

// parseAlert 解析告警
func (a *RootCauseAnalysisAgent) parseAlert(input interface{}) (*Alert, error) {
	switch v := input.(type) {
	case *Alert:
		return v, nil
	case Alert:
		return &v, nil
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var alert Alert
		if err := json.Unmarshal(data, &alert); err != nil {
			return nil, err
		}
		return &alert, nil
	default:
		return nil, fmt.Errorf("不支持的输入类型: %T", input)
	}
}

// collectInspectionData 收集巡检数据
func (a *RootCauseAnalysisAgent) collectInspectionData(ctx context.Context) (map[string]interface{}, error) {
	if a.inspectionAgent == nil {
		return nil, fmt.Errorf("巡检 Agent 未配置")
	}

	task := &multiagent.CollaborativeTask{
		ID:   "inspection-for-rca",
		Name: "根因分析巡检",
		Type: multiagent.CollaborationTypeParallel,
	}

	assignment, err := a.inspectionAgent.Collaborate(ctx, task)
	if err != nil {
		return nil, err
	}

	if result, ok := assignment.Result.(map[string]interface{}); ok {
		return result, nil
	}

	return nil, fmt.Errorf("巡检结果格式异常")
}

// buildAnalysisContext 构建分析上下文
func (a *RootCauseAnalysisAgent) buildAnalysisContext(alert *Alert, inspectionData map[string]interface{}) string {
	var sb strings.Builder

	sb.WriteString("## 告警信息\n")
	sb.WriteString(fmt.Sprintf("- 告警 ID: %s\n", alert.ID))
	sb.WriteString(fmt.Sprintf("- 类型: %s\n", alert.Type))
	sb.WriteString(fmt.Sprintf("- 级别: %s\n", alert.Severity))
	sb.WriteString(fmt.Sprintf("- 来源: %s\n", alert.Source))
	sb.WriteString(fmt.Sprintf("- 消息: %s\n", alert.Message))
	sb.WriteString(fmt.Sprintf("- 时间: %s\n", alert.Timestamp.Format(time.RFC3339)))

	if inspectionData != nil {
		sb.WriteString("\n## 巡检数据\n")
		data, _ := json.MarshalIndent(inspectionData, "", "  ")
		sb.WriteString(string(data))
	}

	return sb.String()
}

// analyzeRootCause 调用 LLM 分析根因
func (a *RootCauseAnalysisAgent) analyzeRootCause(ctx context.Context, analysisContext string) (*RootCauseResult, error) {
	if a.llmClient == nil {
		// 如果没有 LLM，返回模拟结果
		return a.simulateRootCauseAnalysis(), nil
	}

	prompt := fmt.Sprintf(`你是一个专业的运维根因分析专家。请根据以下告警信息和系统巡检数据，分析故障的根本原因。

%s

请按以下 JSON 格式输出分析结果：
{
  "root_cause": "根本原因的简短描述",
  "confidence": 0.85,
  "evidence": ["证据1", "证据2"],
  "suggestions": ["建议1", "建议2"],
  "analysis": "详细的分析过程说明"
}

只输出 JSON，不要有其他内容。`, analysisContext)

	resp, err := a.llmClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "你是一个专业的运维根因分析专家，擅长从多维度数据中定位故障根本原因。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   1000,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}

	// 解析 LLM 响应
	var result RootCauseResult
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		// 如果解析失败，尝试提取关键信息
		result = RootCauseResult{
			RootCause:   "需要进一步分析",
			Confidence:  0.5,
			Analysis:    resp.Content,
			Suggestions: []string{"建议收集更多日志信息", "建议检查相关服务状态"},
		}
	}

	return &result, nil
}

// simulateRootCauseAnalysis 模拟根因分析
func (a *RootCauseAnalysisAgent) simulateRootCauseAnalysis() *RootCauseResult {
	causes := []struct {
		cause       string
		confidence  float64
		evidence    []string
		suggestions []string
		analysis    string
	}{
		{
			cause:       "数据库连接池耗尽",
			confidence:  0.85,
			evidence:    []string{"数据库连接数达到上限", "请求队列积压", "响应延迟显著增加"},
			suggestions: []string{"增加连接池大小", "优化慢查询", "检查是否存在连接泄漏"},
			analysis:    "根据巡检数据，数据库连接数已达到配置上限，同时观察到请求延迟明显增加，日志中出现大量 connection timeout 错误。这表明数据库连接池已耗尽，导致新请求无法获取连接。",
		},
		{
			cause:       "上游服务 A 未透传用户上下文",
			confidence:  0.78,
			evidence:    []string{"服务 B 参数校验失败", "user_id 字段为空", "调用链路显示 A->B"},
			suggestions: []string{"检查服务 A 的请求透传逻辑", "添加参数校验日志", "配置链路追踪"},
			analysis:    "分析调用链路发现，服务 B 收到的请求中 user_id 字段为空，而该字段应由上游服务 A 透传。检查服务 A 的代码发现，在某些异常分支中未正确传递上下文。",
		},
		{
			cause:       "内存泄漏导致 OOM",
			confidence:  0.92,
			evidence:    []string{"内存使用持续增长", "GC 频率异常升高", "最终触发 OOM Killer"},
			suggestions: []string{"分析内存 dump", "检查对象引用", "添加内存监控告警"},
			analysis:    "监控数据显示内存使用率在过去 24 小时内持续上升，GC 频率从每分钟 2 次增加到 15 次。结合日志中的 OutOfMemoryError，确认存在内存泄漏问题。",
		},
	}

	selected := causes[rand.Intn(len(causes))]

	return &RootCauseResult{
		RootCause:   selected.cause,
		Confidence:  selected.confidence,
		Evidence:    selected.evidence,
		Suggestions: selected.suggestions,
		Analysis:    selected.analysis,
	}
}

// =============================================================================
// 运维协调器
// =============================================================================

// OpsCoordinator 运维协调器
// 负责路由分发故障到不同层级的 Agent
type OpsCoordinator struct {
	system          *multiagent.MultiAgentSystem
	sopAgent        *SOPAutomationAgent
	inspectionAgent *InspectionAgent
	rcaAgent        *RootCauseAnalysisAgent
	logger          *simpleLogger
}

// NewOpsCoordinator 创建运维协调器
func NewOpsCoordinator(
	system *multiagent.MultiAgentSystem,
	llmClient llm.Client,
) *OpsCoordinator {
	// 创建各层 Agent
	inspectionAgent := NewInspectionAgent(system)
	sopAgent := NewSOPAutomationAgent(system, llmClient)
	rcaAgent := NewRootCauseAnalysisAgent(system, llmClient, inspectionAgent)

	coordinator := &OpsCoordinator{
		system:          system,
		sopAgent:        sopAgent,
		inspectionAgent: inspectionAgent,
		rcaAgent:        rcaAgent,
		logger:          newLogger("Coordinator"),
	}

	// 注册 Agent 到系统
	_ = system.RegisterAgent("sop-automation", sopAgent)
	_ = system.RegisterAgent("inspection", inspectionAgent)
	_ = system.RegisterAgent("root-cause-analysis", rcaAgent)

	return coordinator
}

// HandleAlert 处理告警
func (c *OpsCoordinator) HandleAlert(ctx context.Context, alert *Alert) (*OpsResult, error) {
	c.logger.Infof("════════════════════════════════════════")
	c.logger.Infof("收到告警: %s", alert.ID)
	c.logger.Infof("类型: %s, 级别: %s", alert.Type, alert.Severity)
	c.logger.Infof("消息: %s", alert.Message)
	c.logger.Infof("════════════════════════════════════════")

	startTime := time.Now()

	// 决策：选择处理层级
	layer := c.decideLayer(alert)
	c.logger.Infof("路由决策: %s", layer)

	var result *OpsResult
	var err error

	switch layer {
	case "layer1":
		// Layer 1: SOP 自动化执行
		c.logger.Info("→ 转交 Layer 1: SOP 自动化执行")
		result, err = c.handleWithSOP(ctx, alert)

	case "layer2":
		// Layer 2: 巡检感知（通常作为 Layer 3 的前置）
		c.logger.Info("→ 转交 Layer 2: 巡检感知")
		result, err = c.handleWithInspection(ctx, alert)

	case "layer3":
		// Layer 3: AI 根因分析
		c.logger.Info("→ 转交 Layer 3: AI 根因分析")
		result, err = c.handleWithRCA(ctx, alert)

	default:
		// 未知类型，转交人工
		result = &OpsResult{
			AlertID: alert.ID,
			Status:  "escalated",
			Layer:   "manual",
			Message: "未识别的告警类型，已转交人工处理",
		}
	}

	if result != nil {
		result.Duration = time.Since(startTime)
	}

	return result, err
}

// decideLayer 决策处理层级
func (c *OpsCoordinator) decideLayer(alert *Alert) string {
	// 规则 1: 如果有对应的 SOP，使用 Layer 1
	if c.sopAgent.CanHandle(alert) {
		return "layer1"
	}

	// 规则 2: 服务错误等复杂问题，使用 Layer 3
	if alert.Type == AlertTypeServiceError || alert.Type == AlertTypeNetworkIssue {
		return "layer3"
	}

	// 规则 3: 高优先级未知问题，使用 Layer 3
	if alert.Severity == SeverityCritical && alert.Type == AlertTypeUnknown {
		return "layer3"
	}

	// 规则 4: 其他情况，先巡检再分析
	return "layer2"
}

// handleWithSOP 使用 SOP 处理
func (c *OpsCoordinator) handleWithSOP(ctx context.Context, alert *Alert) (*OpsResult, error) {
	task := &multiagent.CollaborativeTask{
		ID:          fmt.Sprintf("sop-task-%s", alert.ID),
		Name:        "SOP 自动化执行",
		Type:        multiagent.CollaborationTypeSequential,
		Input:       alert,
		Assignments: make(map[string]multiagent.Assignment),
	}

	assignment, err := c.sopAgent.Collaborate(ctx, task)
	if err != nil {
		return nil, err
	}

	if result, ok := assignment.Result.(*OpsResult); ok {
		return result, nil
	}

	return nil, fmt.Errorf("SOP 执行结果格式异常")
}

// handleWithInspection 使用巡检处理
func (c *OpsCoordinator) handleWithInspection(ctx context.Context, alert *Alert) (*OpsResult, error) {
	task := &multiagent.CollaborativeTask{
		ID:          fmt.Sprintf("inspection-task-%s", alert.ID),
		Name:        "巡检数据采集",
		Type:        multiagent.CollaborationTypeParallel,
		Input:       alert,
		Assignments: make(map[string]multiagent.Assignment),
	}

	assignment, err := c.inspectionAgent.Collaborate(ctx, task)
	if err != nil {
		return nil, err
	}

	inspectionData, ok := assignment.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("巡检结果格式异常")
	}

	// 根据巡检结果决定是否需要进一步分析
	status, _ := inspectionData["status"].(string)
	if status == "critical" || status == "warning" {
		c.logger.Info("  巡检发现异常，转交 AI 分析")
		return c.handleWithRCA(ctx, alert)
	}

	return &OpsResult{
		AlertID: alert.ID,
		Status:  "healthy",
		Layer:   "layer2",
		Handler: c.inspectionAgent.Name(),
		Message: "巡检完成，系统状态正常",
		Metadata: map[string]interface{}{
			"inspection_data": inspectionData,
		},
	}, nil
}

// handleWithRCA 使用 AI 根因分析处理
func (c *OpsCoordinator) handleWithRCA(ctx context.Context, alert *Alert) (*OpsResult, error) {
	task := &multiagent.CollaborativeTask{
		ID:          fmt.Sprintf("rca-task-%s", alert.ID),
		Name:        "AI 根因分析",
		Type:        multiagent.CollaborationTypeSequential,
		Input:       alert,
		Assignments: make(map[string]multiagent.Assignment),
	}

	assignment, err := c.rcaAgent.Collaborate(ctx, task)
	if err != nil {
		return nil, err
	}

	if result, ok := assignment.Result.(*OpsResult); ok {
		return result, nil
	}

	return nil, fmt.Errorf("根因分析结果格式异常")
}

// =============================================================================
// Mock LLM 客户端
// =============================================================================

// MockLLMClient Mock LLM 客户端
type MockLLMClient struct{}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 模拟延迟
	time.Sleep(100 * time.Millisecond)

	// 根据 prompt 内容返回不同响应
	if len(req.Messages) > 0 {
		content := req.Messages[len(req.Messages)-1].Content

		if strings.Contains(content, "YES 或 NO") {
			return &llm.CompletionResponse{
				Content:  "YES",
				Model:    "mock-model",
				Provider: "mock",
			}, nil
		}

		if strings.Contains(content, "root_cause") {
			return &llm.CompletionResponse{
				Content: `{
  "root_cause": "数据库连接池耗尽导致请求超时",
  "confidence": 0.87,
  "evidence": ["数据库连接数达到上限", "请求队列积压", "响应延迟显著增加"],
  "suggestions": ["增加连接池大小", "优化慢查询", "添加连接泄漏检测"],
  "analysis": "根据巡检数据分析，数据库连接数已达到配置上限（100/100），同时观察到请求 P99 延迟从 150ms 增加到 2500ms。日志中出现大量 connection timeout 错误。综合以上证据，确认根本原因是数据库连接池耗尽。"
}`,
				Model:    "mock-model",
				Provider: "mock",
			}, nil
		}
	}

	return &llm.CompletionResponse{
		Content:  "Mock response",
		Model:    "mock-model",
		Provider: "mock",
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

// =============================================================================
// 创建 LLM 客户端
// =============================================================================

func createLLMClient() (llm.Client, error) {
	// 尝试使用 DeepSeek
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		return providers.NewDeepSeekWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("deepseek-chat"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.3),
		)
	}

	// 尝试使用 Kimi
	if apiKey := os.Getenv("KIMI_API_KEY"); apiKey != "" {
		return providers.NewKimiWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("moonshot-v1-8k"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.3),
		)
	}

	// 尝试使用 OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		return providers.NewOpenAIWithOptions(
			llm.WithAPIKey(apiKey),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.3),
		)
	}

	return nil, fmt.Errorf("未找到可用的 LLM 提供商")
}

// =============================================================================
// 主函数
// =============================================================================

func main() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                     智能运维 Agent 分层治理体系                             ║")
	fmt.Println("║         Intelligent Ops Agent Framework - Layered Governance               ║")
	fmt.Println("╠════════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Layer 1: 自动化执行 (SOP)     - 处理确定性故障                             ║")
	fmt.Println("║  Layer 2: 巡检感知 (Data)     - 数据采集与风险感知                          ║")
	fmt.Println("║  Layer 3: AI 根因分析 (AI)    - 复杂故障推理定位                            ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 初始化随机数种子
	// rand.Seed(time.Now().UnixNano()) // Go 1.20+ automatically seeds global random source

	// 创建 LLM 客户端
	llmClient, err := createLLMClient()
	if err != nil {
		fmt.Printf("⚠️  警告: 无法创建真实 LLM 客户端: %v\n", err)
		fmt.Println("    将使用 Mock LLM 客户端进行演示")
		fmt.Println()
		llmClient = &MockLLMClient{}
	} else {
		fmt.Printf("✓ 已连接 LLM 提供商: %s\n", llmClient.Provider())
		fmt.Println()
	}

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 创建多智能体系统
	logger := newLogger("System")
	system := multiagent.NewMultiAgentSystem(logger, multiagent.WithMaxAgents(10))
	defer func() {
		if err := system.Close(); err != nil {
			fmt.Printf("Error closing system: %v\n", err)
		}
	}()

	// 创建运维协调器
	coordinator := NewOpsCoordinator(system, llmClient)

	// 演示场景
	fmt.Println("【场景演示】")
	fmt.Println()

	// 场景 1: 磁盘告警 -> Layer 1 (SOP)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("【场景 1】磁盘空间告警 - 预期路由到 Layer 1 (SOP 自动化)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	alert1 := &Alert{
		ID:        "ALERT-001",
		Type:      AlertTypeDiskFull,
		Severity:  SeverityHigh,
		Source:    "prometheus",
		Message:   "/var/log 磁盘使用率达到 92%",
		Timestamp: time.Now(),
		Labels: map[string]string{
			"host":      "web-server-01",
			"partition": "/var/log",
		},
	}
	result1, err := coordinator.HandleAlert(ctx, alert1)
	if err != nil {
		fmt.Printf("处理失败: %v\n", err)
	} else {
		printResult(result1)
	}
	fmt.Println()

	// 场景 2: 服务错误 -> Layer 3 (AI 分析)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("【场景 2】服务接口错误 - 预期路由到 Layer 3 (AI 根因分析)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	alert2 := &Alert{
		ID:        "ALERT-002",
		Type:      AlertTypeServiceError,
		Severity:  SeverityCritical,
		Source:    "istio",
		Message:   "推荐服务 /api/v1/recommend 接口报错 500，错误率 15%",
		Timestamp: time.Now(),
		Labels: map[string]string{
			"service":   "recommendation-svc",
			"namespace": "production",
			"endpoint":  "/api/v1/recommend",
		},
		Annotations: map[string]string{
			"error_message": "Internal Server Error: parameter validation failed",
		},
	}
	result2, err := coordinator.HandleAlert(ctx, alert2)
	if err != nil {
		fmt.Printf("处理失败: %v\n", err)
	} else {
		printResult(result2)
	}
	fmt.Println()

	// 场景 3: Pod 崩溃 -> Layer 1 (SOP)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("【场景 3】Pod CrashLoopBackOff - 预期路由到 Layer 1 (SOP)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	alert3 := &Alert{
		ID:        "ALERT-003",
		Type:      AlertTypePodCrash,
		Severity:  SeverityHigh,
		Source:    "kubernetes",
		Message:   "Pod payment-worker-7d9f5c6b8-x2k4m CrashLoopBackOff",
		Timestamp: time.Now(),
		Labels: map[string]string{
			"pod":       "payment-worker-7d9f5c6b8-x2k4m",
			"namespace": "production",
			"app":       "payment-worker",
		},
	}
	result3, err := coordinator.HandleAlert(ctx, alert3)
	if err != nil {
		fmt.Printf("处理失败: %v\n", err)
	} else {
		printResult(result3)
	}
	fmt.Println()

	// 场景 4: 未知告警 -> Layer 2 (巡检)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("【场景 4】未知类型告警 - 预期路由到 Layer 2 (巡检感知)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	alert4 := &Alert{
		ID:        "ALERT-004",
		Type:      AlertTypeUnknown,
		Severity:  SeverityMedium,
		Source:    "custom-monitor",
		Message:   "检测到异常流量模式",
		Timestamp: time.Now(),
	}
	result4, err := coordinator.HandleAlert(ctx, alert4)
	if err != nil {
		fmt.Printf("处理失败: %v\n", err)
	} else {
		printResult(result4)
	}

	// 打印总结
	printSummary()
}

// printResult 打印处理结果
func printResult(result *OpsResult) {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────")
	fmt.Printf("│ 处理结果\n")
	fmt.Println("├─────────────────────────────────────────────────────────")
	fmt.Printf("│ 告警 ID:  %s\n", result.AlertID)
	fmt.Printf("│ 状态:     %s\n", result.Status)
	fmt.Printf("│ 处理层:   %s\n", result.Layer)
	fmt.Printf("│ 处理者:   %s\n", result.Handler)
	fmt.Printf("│ 耗时:     %v\n", result.Duration)
	fmt.Printf("│ 消息:     %s\n", result.Message)

	if len(result.Actions) > 0 {
		fmt.Println("├─────────────────────────────────────────────────────────")
		fmt.Println("│ 执行动作:")
		for i, action := range result.Actions {
			status := "✓"
			if !action.Success {
				status = "✗"
			}
			fmt.Printf("│   %d. [%s] %s (%v)\n", i+1, status, action.Action, action.Duration)
		}
	}

	if result.RootCause != nil {
		fmt.Println("├─────────────────────────────────────────────────────────")
		fmt.Println("│ 根因分析结果:")
		fmt.Printf("│   根本原因: %s\n", result.RootCause.RootCause)
		fmt.Printf("│   置信度:   %.1f%%\n", result.RootCause.Confidence*100)
		fmt.Println("│   证据:")
		for _, e := range result.RootCause.Evidence {
			fmt.Printf("│     - %s\n", e)
		}
		fmt.Println("│   建议:")
		for _, s := range result.RootCause.Suggestions {
			fmt.Printf("│     - %s\n", s)
		}
	}

	fmt.Println("└─────────────────────────────────────────────────────────")
}

// printSummary 打印总结
func printSummary() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                              演示完成                                       ║")
	fmt.Println("╠════════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                            ║")
	fmt.Println("║  智能运维 Agent 分层治理架构核心要点:                                        ║")
	fmt.Println("║                                                                            ║")
	fmt.Println("║  ┌──────────────┬──────────────────────────────────────────────────────┐   ║")
	fmt.Println("║  │    层级      │                      职责                            │   ║")
	fmt.Println("║  ├──────────────┼──────────────────────────────────────────────────────┤   ║")
	fmt.Println("║  │ Layer 1      │ SOP 自动化执行 - 处理 60% 的确定性故障               │   ║")
	fmt.Println("║  │ (Automation) │ LLM 识别意图 -> 匹配 SOP -> 执行脚本 -> 通知         │   ║")
	fmt.Println("║  ├──────────────┼──────────────────────────────────────────────────────┤   ║")
	fmt.Println("║  │ Layer 2      │ 巡检感知 - 数据采集与风险感知 (非 AI)                │   ║")
	fmt.Println("║  │ (Inspection) │ 整合 Prometheus/K8s/Logs -> 统一数据协议 (ops-mcp)   │   ║")
	fmt.Println("║  ├──────────────┼──────────────────────────────────────────────────────┤   ║")
	fmt.Println("║  │ Layer 3      │ AI 根因分析 - 处理 15% 的复杂故障                    │   ║")
	fmt.Println("║  │ (Analysis)   │ LLM 获取巡检数据 -> 关联分析 -> 推理定位根因         │   ║")
	fmt.Println("║  └──────────────┴──────────────────────────────────────────────────────┘   ║")
	fmt.Println("║                                                                            ║")
	fmt.Println("║  演进路径: 标准化 (SOP) → 数据驱动 (Data) → 智能化 (AI)                    ║")
	fmt.Println("║                                                                            ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("使用真实 LLM 运行:")
	fmt.Println("  export DEEPSEEK_API_KEY=\"your-api-key\"")
	fmt.Println("  export KIMI_API_KEY=\"your-api-key\"")
	fmt.Println("  export OPENAI_API_KEY=\"your-api-key\"")
	fmt.Println("  go run main.go")
	fmt.Println()
}
