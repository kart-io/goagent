package toolkits

import (
	"context"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/compute"
	"github.com/kart-io/goagent/tools/http"
	"github.com/kart-io/goagent/tools/search"
	"github.com/kart-io/goagent/tools/shell"
)

// Toolkit 工具集接口
//
// 管理一组相关的工具
type Toolkit interface {
	// GetTools 获取所有工具
	GetTools() []interfaces.Tool

	// GetToolByName 根据名称获取工具
	GetToolByName(name string) (interfaces.Tool, error)

	// GetToolNames 获取所有工具名称
	GetToolNames() []string
}

// BaseToolkit 基础工具集实现
type BaseToolkit struct {
	tools    []interfaces.Tool
	toolsMap map[string]interfaces.Tool
	mu       sync.RWMutex
}

// NewBaseToolkit 创建基础工具集
func NewBaseToolkit(toolList ...interfaces.Tool) *BaseToolkit {
	toolkit := &BaseToolkit{
		tools:    toolList,
		toolsMap: make(map[string]interfaces.Tool),
	}

	for _, tool := range toolList {
		toolkit.toolsMap[tool.Name()] = tool
	}

	return toolkit
}

// GetTools 获取所有工具
func (t *BaseToolkit) GetTools() []interfaces.Tool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tools
}

// GetToolByName 根据名称获取工具
func (t *BaseToolkit) GetToolByName(name string) (interfaces.Tool, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tool, ok := t.toolsMap[name]
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
			WithComponent("base_toolkit").
			WithOperation("get_tool_by_name").
			WithContext("tool_name", name)
	}

	return tool, nil
}

// GetToolNames 获取所有工具名称
func (t *BaseToolkit) GetToolNames() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	names := make([]string, 0, len(t.tools))
	for _, tool := range t.tools {
		names = append(names, tool.Name())
	}

	return names
}

// AddTool 添加工具
func (t *BaseToolkit) AddTool(tool interfaces.Tool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tools = append(t.tools, tool)
	t.toolsMap[tool.Name()] = tool
}

// RemoveTool 移除工具
func (t *BaseToolkit) RemoveTool(name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.toolsMap[name]; !ok {
		return agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
			WithComponent("base_toolkit").
			WithOperation("remove_tool").
			WithContext("tool_name", name)
	}

	delete(t.toolsMap, name)

	// 从 slice 中移除
	for i, tool := range t.tools {
		if tool.Name() == name {
			t.tools = append(t.tools[:i], t.tools[i+1:]...)
			break
		}
	}

	return nil
}

// MergeTool 合并另一个工具集
func (t *BaseToolkit) MergeTool(other Toolkit) {
	for _, tool := range other.GetTools() {
		t.AddTool(tool)
	}
}

// StandardToolkit 标准工具集
//
// 包含常用的基础工具
type StandardToolkit struct {
	*BaseToolkit
}

// NewStandardToolkit 创建标准工具集
func NewStandardToolkit() *StandardToolkit {
	toolList := []interfaces.Tool{
		compute.NewCalculatorTool(),
		search.NewSearchTool(search.NewMockSearchEngine()),
	}

	return &StandardToolkit{
		BaseToolkit: NewBaseToolkit(toolList...),
	}
}

// DevelopmentToolkit 开发工具集
//
// 包含开发常用的工具（Shell、API 等）
type DevelopmentToolkit struct {
	*BaseToolkit
}

// NewDevelopmentToolkit 创建开发工具集
func NewDevelopmentToolkit() *DevelopmentToolkit {
	// 安全的命令白名单
	safeCommands := []string{
		"ls", "pwd", "echo", "cat", "grep", "find",
		"git", "curl", "wget",
		"uname", "hostname", "whoami", "date",
	}

	toolList := []interfaces.Tool{
		shell.NewShellTool(safeCommands, 0),
		http.NewAPITool("", 0, nil),
		compute.NewCalculatorTool(),
	}

	return &DevelopmentToolkit{
		BaseToolkit: NewBaseToolkit(toolList...),
	}
}

// ToolkitBuilder 工具集构建器
type ToolkitBuilder struct {
	tools []interfaces.Tool
}

// NewToolkitBuilder 创建工具集构建器
func NewToolkitBuilder() *ToolkitBuilder {
	return &ToolkitBuilder{
		tools: []interfaces.Tool{},
	}
}

// AddTool 添加单个工具
func (b *ToolkitBuilder) AddTool(tool interfaces.Tool) *ToolkitBuilder {
	b.tools = append(b.tools, tool)
	return b
}

// AddTools 批量添加工具
func (b *ToolkitBuilder) AddTools(tools ...interfaces.Tool) *ToolkitBuilder {
	b.tools = append(b.tools, tools...)
	return b
}

// AddToolkit 添加整个工具集
func (b *ToolkitBuilder) AddToolkit(toolkit Toolkit) *ToolkitBuilder {
	b.tools = append(b.tools, toolkit.GetTools()...)
	return b
}

// WithCalculator 添加计算器工具
func (b *ToolkitBuilder) WithCalculator() *ToolkitBuilder {
	b.tools = append(b.tools, compute.NewCalculatorTool())
	return b
}

// WithSearch 添加搜索工具
func (b *ToolkitBuilder) WithSearch(engine search.SearchEngine) *ToolkitBuilder {
	if engine == nil {
		engine = search.NewMockSearchEngine()
	}
	b.tools = append(b.tools, search.NewSearchTool(engine))
	return b
}

// WithShell 添加 Shell 工具
func (b *ToolkitBuilder) WithShell(allowedCommands []string) *ToolkitBuilder {
	b.tools = append(b.tools, shell.NewShellTool(allowedCommands, 0))
	return b
}

// WithAPI 添加 API 工具
func (b *ToolkitBuilder) WithAPI(baseURL string, headers map[string]string) *ToolkitBuilder {
	b.tools = append(b.tools, http.NewAPITool(baseURL, 0, headers))
	return b
}

// Build 构建工具集
func (b *ToolkitBuilder) Build() Toolkit {
	return NewBaseToolkit(b.tools...)
}

// ToolRegistry 工具注册表
//
// 全局工具管理器，支持注册和发现工具
type ToolRegistry struct {
	tools map[string]interfaces.Tool
	mu    sync.RWMutex
}

// NewToolRegistry 创建工具注册表
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]interfaces.Tool),
	}
}

// Register 注册工具
func (r *ToolRegistry) Register(tool interfaces.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[tool.Name()]; exists {
		return agentErrors.New(agentErrors.CodeToolValidation, "tool already registered").
			WithComponent("tool_registry").
			WithOperation("register").
			WithContext("tool_name", tool.Name())
	}

	r.tools[tool.Name()] = tool
	return nil
}

// Unregister 注销工具
func (r *ToolRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
			WithComponent("tool_registry").
			WithOperation("unregister").
			WithContext("tool_name", name)
	}

	delete(r.tools, name)
	return nil
}

// Get 获取工具
func (r *ToolRegistry) Get(name string) (interfaces.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
			WithComponent("tool_registry").
			WithOperation("get").
			WithContext("tool_name", name)
	}

	return tool, nil
}

// List 列出所有工具
func (r *ToolRegistry) List() []interfaces.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	toolList := make([]interfaces.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		toolList = append(toolList, tool)
	}

	return toolList
}

// CreateToolkit 从注册表创建工具集
func (r *ToolRegistry) CreateToolkit(names ...string) (Toolkit, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	toolList := make([]interfaces.Tool, 0, len(names))
	for _, name := range names {
		tool, ok := r.tools[name]
		if !ok {
			return nil, agentErrors.New(agentErrors.CodeToolNotFound, "tool not found").
				WithComponent("tool_registry").
				WithOperation("create_toolkit").
				WithContext("tool_name", name)
		}
		toolList = append(toolList, tool)
	}

	return NewBaseToolkit(toolList...), nil
}

// 全局工具注册表
var defaultRegistry = NewToolRegistry()

// RegisterTool 注册工具到全局注册表
func RegisterTool(tool interfaces.Tool) error {
	return defaultRegistry.Register(tool)
}

// GetTool 从全局注册表获取工具
func GetTool(name string) (interfaces.Tool, error) {
	return defaultRegistry.Get(name)
}

// ListTools 列出全局注册表中的所有工具
func ListTools() []interfaces.Tool {
	return defaultRegistry.List()
}

// ToolkitExecutor 工具包执行器
//
// 提供工具执行的辅助功能
type ToolkitExecutor struct {
	toolkit  Toolkit
	parallel bool // 是否并行执行
}

// NewToolkitExecutor 创建工具包执行器
func NewToolkitExecutor(toolkit Toolkit) *ToolkitExecutor {
	return &ToolkitExecutor{
		toolkit:  toolkit,
		parallel: false,
	}
}

// WithParallel 设置并行执行
func (e *ToolkitExecutor) WithParallel(parallel bool) *ToolkitExecutor {
	e.parallel = parallel
	return e
}

// Execute 执行单个工具
func (e *ToolkitExecutor) Execute(ctx context.Context, toolName string, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	tool, err := e.toolkit.GetToolByName(toolName)
	if err != nil {
		return nil, err
	}

	return tool.Invoke(ctx, input)
}

// ExecuteMultiple 执行多个工具
func (e *ToolkitExecutor) ExecuteMultiple(ctx context.Context, requests map[string]*interfaces.ToolInput) (map[string]*interfaces.ToolOutput, error) {
	if e.parallel {
		return e.executeParallel(ctx, requests)
	}
	return e.executeSequential(ctx, requests)
}

// executeSequential 顺序执行
func (e *ToolkitExecutor) executeSequential(ctx context.Context, requests map[string]*interfaces.ToolInput) (map[string]*interfaces.ToolOutput, error) {
	results := make(map[string]*interfaces.ToolOutput)

	for toolName, input := range requests {
		output, err := e.Execute(ctx, toolName, input)
		if err != nil {
			return results, err
		}
		results[toolName] = output
	}

	return results, nil
}

// executeParallel 并行执行
func (e *ToolkitExecutor) executeParallel(ctx context.Context, requests map[string]*interfaces.ToolInput) (map[string]*interfaces.ToolOutput, error) {
	type result struct {
		toolName string
		output   *interfaces.ToolOutput
		err      error
	}

	resultsChan := make(chan result, len(requests))

	for toolName, input := range requests {
		go func(name string, inp *interfaces.ToolInput) {
			output, err := e.Execute(ctx, name, inp)
			resultsChan <- result{
				toolName: name,
				output:   output,
				err:      err,
			}
		}(toolName, input)
	}

	results := make(map[string]*interfaces.ToolOutput)
	var firstError error

	for i := 0; i < len(requests); i++ {
		select {
		case res := <-resultsChan:
			if res.err != nil && firstError == nil {
				firstError = res.err
			}
			results[res.toolName] = res.output
		case <-ctx.Done():
			return results, ctx.Err()
		}
	}

	return results, firstError
}
