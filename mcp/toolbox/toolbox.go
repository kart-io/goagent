package toolbox

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
)

// StandardToolBox 标准工具箱实现
type StandardToolBox struct {
	// 工具注册表
	registry core.ToolRegistry

	// 工具执行器
	executor core.ToolExecutor

	// 工具验证器
	validator core.ToolValidator

	// 权限管理器
	permissionManager *PermissionManager

	// 统计信息
	stats      *core.ToolBoxStatistics
	statsMutex sync.RWMutex

	// 调用历史
	callHistory      []*core.ToolCallResult
	callHistoryMutex sync.RWMutex
	maxHistorySize   int
}

// NewStandardToolBox 创建标准工具箱
func NewStandardToolBox() *StandardToolBox {
	return &StandardToolBox{
		registry:          NewMemoryRegistry(),
		executor:          NewStandardExecutor(),
		validator:         NewJSONSchemaValidator(),
		permissionManager: NewPermissionManager(),
		stats: &core.ToolBoxStatistics{
			ToolUsage:     make(map[string]int64),
			CategoryUsage: make(map[string]int64),
		},
		callHistory:    make([]*core.ToolCallResult, 0, 100),
		maxHistorySize: 100,
	}
}

// Register 注册工具
func (tb *StandardToolBox) Register(tool core.Tool) error {
	// 验证工具 Schema
	if err := tb.validator.ValidateSchema(tool.Schema()); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeToolValidation, "invalid tool schema").
			WithComponent("standard_toolbox").
			WithOperation("register").
			WithContext("tool_name", tool.Name())
	}

	// 注册到注册表
	if err := tb.registry.Register(tool); err != nil {
		return err
	}

	// 更新统计
	tb.statsMutex.Lock()
	tb.stats.TotalTools = tb.registry.Count()
	tb.statsMutex.Unlock()

	return nil
}

// Unregister 注销工具
func (tb *StandardToolBox) Unregister(name string) error {
	if err := tb.registry.Unregister(name); err != nil {
		return err
	}

	// 更新统计
	tb.statsMutex.Lock()
	tb.stats.TotalTools = tb.registry.Count()
	delete(tb.stats.ToolUsage, name)
	tb.statsMutex.Unlock()

	return nil
}

// Get 获取工具
func (tb *StandardToolBox) Get(name string) (core.Tool, error) {
	tool, exists := tb.registry.Get(name)
	if !exists {
		return nil, &core.ErrToolNotFound{ToolName: name}
	}
	return tool, nil
}

// List 列出所有工具
func (tb *StandardToolBox) List() []core.Tool {
	return tb.registry.List()
}

// ListByCategory 按分类列出工具
func (tb *StandardToolBox) ListByCategory(category string) []core.Tool {
	tools := tb.registry.List()
	result := make([]core.Tool, 0)

	for _, tool := range tools {
		if tool.Category() == category {
			result = append(result, tool)
		}
	}

	return result
}

// Search 搜索工具
func (tb *StandardToolBox) Search(query string) []core.Tool {
	tools := tb.registry.List()
	result := make([]core.Tool, 0)
	query = strings.ToLower(query)

	for _, tool := range tools {
		name := strings.ToLower(tool.Name())
		desc := strings.ToLower(tool.Description())

		if strings.Contains(name, query) || strings.Contains(desc, query) {
			result = append(result, tool)
		}
	}

	return result
}

// Execute 执行工具
func (tb *StandardToolBox) Execute(ctx context.Context, call *core.ToolCall) (*core.ToolCallResult, error) {
	startTime := time.Now()

	// 验证调用
	if err := tb.Validate(call); err != nil {
		return nil, err
	}

	// 检查权限
	if call.UserID != "" {
		allowed, err := tb.HasPermission(call.UserID, call.ToolName)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, &core.ErrPermissionDenied{
				UserID:   call.UserID,
				ToolName: call.ToolName,
				Reason:   "user not authorized for this tool",
			}
		}
	}

	// 获取工具
	tool, err := tb.Get(call.ToolName)
	if err != nil {
		return nil, err
	}

	// 生成调用 ID
	if call.ID == "" {
		call.ID = uuid.New().String()
	}

	// 执行工具
	result, err := tb.executor.Execute(ctx, tool, call)
	if err != nil {
		tb.recordFailure(call.ToolName, tool.Category())
		return nil, err
	}

	// 记录成功
	tb.recordSuccess(call.ToolName, tool.Category(), result.Duration)

	// 创建调用结果
	callResult := &core.ToolCallResult{
		Call:        call,
		Result:      result,
		ExecutedAt:  startTime,
		CompletedAt: time.Now(),
	}

	// 保存到历史
	tb.addToHistory(callResult)

	return callResult, nil
}

// ExecuteBatch 批量执行工具
func (tb *StandardToolBox) ExecuteBatch(ctx context.Context, calls []*core.ToolCall) ([]*core.ToolCallResult, error) {
	results := make([]*core.ToolCallResult, len(calls))
	errors := make([]error, len(calls))

	// 并发执行
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c *core.ToolCall) {
			defer wg.Done()
			result, err := tb.Execute(ctx, c)
			results[idx] = result
			errors[idx] = err
		}(i, call)
	}

	wg.Wait()

	// 检查错误
	for _, err := range errors {
		if err != nil {
			return results, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "batch execution had errors").
				WithComponent("standard_toolbox").
				WithOperation("execute_batch").
				WithContext("total_calls", len(calls))
		}
	}

	return results, nil
}

// GetMetadata 获取工具元数据
func (tb *StandardToolBox) GetMetadata(name string) (*core.ToolMetadata, error) {
	tool, err := tb.Get(name)
	if err != nil {
		return nil, err
	}

	return &core.ToolMetadata{
		Name:         tool.Name(),
		Description:  tool.Description(),
		Category:     tool.Category(),
		Schema:       tool.Schema(),
		RequiresAuth: tool.RequiresAuth(),
		IsDangerous:  tool.IsDangerous(),
	}, nil
}

// ListMetadata 列出所有工具元数据
func (tb *StandardToolBox) ListMetadata() []*core.ToolMetadata {
	tools := tb.List()
	metadata := make([]*core.ToolMetadata, len(tools))

	for i, tool := range tools {
		metadata[i] = &core.ToolMetadata{
			Name:         tool.Name(),
			Description:  tool.Description(),
			Category:     tool.Category(),
			Schema:       tool.Schema(),
			RequiresAuth: tool.RequiresAuth(),
			IsDangerous:  tool.IsDangerous(),
		}
	}

	return metadata
}

// Validate 验证工具调用
func (tb *StandardToolBox) Validate(call *core.ToolCall) error {
	// 检查工具是否存在
	tool, err := tb.Get(call.ToolName)
	if err != nil {
		return err
	}

	// 验证输入参数
	if err := tb.validator.ValidateInput(tool.Schema(), call.Input); err != nil {
		return err
	}

	return nil
}

// HasPermission 检查权限
func (tb *StandardToolBox) HasPermission(userID string, toolName string) (bool, error) {
	return tb.permissionManager.HasPermission(userID, toolName)
}

// Statistics 获取统计信息
func (tb *StandardToolBox) Statistics() *core.ToolBoxStatistics {
	tb.statsMutex.RLock()
	defer tb.statsMutex.RUnlock()

	// 深拷贝统计信息
	stats := &core.ToolBoxStatistics{
		TotalTools:      tb.stats.TotalTools,
		TotalCalls:      tb.stats.TotalCalls,
		SuccessfulCalls: tb.stats.SuccessfulCalls,
		FailedCalls:     tb.stats.FailedCalls,
		AverageLatency:  tb.stats.AverageLatency,
		ToolUsage:       make(map[string]int64),
		CategoryUsage:   make(map[string]int64),
	}

	for k, v := range tb.stats.ToolUsage {
		stats.ToolUsage[k] = v
	}
	for k, v := range tb.stats.CategoryUsage {
		stats.CategoryUsage[k] = v
	}

	return stats
}

// GetCallHistory 获取调用历史
func (tb *StandardToolBox) GetCallHistory() []*core.ToolCallResult {
	tb.callHistoryMutex.RLock()
	defer tb.callHistoryMutex.RUnlock()

	history := make([]*core.ToolCallResult, len(tb.callHistory))
	copy(history, tb.callHistory)
	return history
}

// ClearHistory 清空历史
func (tb *StandardToolBox) ClearHistory() {
	tb.callHistoryMutex.Lock()
	defer tb.callHistoryMutex.Unlock()

	tb.callHistory = make([]*core.ToolCallResult, 0, tb.maxHistorySize)
}

// recordSuccess 记录成功调用
func (tb *StandardToolBox) recordSuccess(toolName, category string, duration time.Duration) {
	tb.statsMutex.Lock()
	defer tb.statsMutex.Unlock()

	tb.stats.TotalCalls++
	tb.stats.SuccessfulCalls++
	tb.stats.ToolUsage[toolName]++
	tb.stats.CategoryUsage[category]++

	// 更新平均延迟
	total := tb.stats.TotalCalls
	oldAvg := tb.stats.AverageLatency
	tb.stats.AverageLatency = (oldAvg*float64(total-1) + float64(duration.Milliseconds())) / float64(total)
}

// recordFailure 记录失败调用
func (tb *StandardToolBox) recordFailure(toolName, category string) {
	tb.statsMutex.Lock()
	defer tb.statsMutex.Unlock()

	tb.stats.TotalCalls++
	tb.stats.FailedCalls++
	tb.stats.ToolUsage[toolName]++
	tb.stats.CategoryUsage[category]++
}

// addToHistory 添加到历史
func (tb *StandardToolBox) addToHistory(result *core.ToolCallResult) {
	tb.callHistoryMutex.Lock()
	defer tb.callHistoryMutex.Unlock()

	tb.callHistory = append(tb.callHistory, result)

	// 限制历史大小
	if len(tb.callHistory) > tb.maxHistorySize {
		tb.callHistory = tb.callHistory[1:]
	}
}

// SetPermissionManager 设置权限管理器
func (tb *StandardToolBox) SetPermissionManager(pm *PermissionManager) {
	tb.permissionManager = pm
}

// SetMaxHistorySize 设置最大历史大小
func (tb *StandardToolBox) SetMaxHistorySize(size int) {
	tb.maxHistorySize = size
}
