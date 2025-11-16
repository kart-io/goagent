package toolbox

import (
	"fmt"
	"sync"

	"github.com/kart-io/goagent/mcp/core"
)

// MemoryRegistry 内存工具注册表
type MemoryRegistry struct {
	tools map[string]core.Tool
	mutex sync.RWMutex
}

// NewMemoryRegistry 创建内存注册表
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		tools: make(map[string]core.Tool),
	}
}

// Register 注册工具
func (r *MemoryRegistry) Register(tool core.Tool) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return &core.ErrToolAlreadyExists{ToolName: name}
	}

	r.tools[name] = tool
	return nil
}

// Unregister 注销工具
func (r *MemoryRegistry) Unregister(name string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.tools[name]; !exists {
		return &core.ErrToolNotFound{ToolName: name}
	}

	delete(r.tools, name)
	return nil
}

// Get 获取工具
func (r *MemoryRegistry) Get(name string) (core.Tool, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tool, exists := r.tools[name]
	return tool, exists
}

// List 列出所有工具
func (r *MemoryRegistry) List() []core.Tool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tools := make([]core.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}

	return tools
}

// Exists 检查工具是否存在
func (r *MemoryRegistry) Exists(name string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	_, exists := r.tools[name]
	return exists
}

// Count 工具数量
func (r *MemoryRegistry) Count() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return len(r.tools)
}

// Clear 清空注册表
func (r *MemoryRegistry) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.tools = make(map[string]core.Tool)
}

// GetByCategory 按分类获取工具
func (r *MemoryRegistry) GetByCategory(category string) []core.Tool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tools := make([]core.Tool, 0)
	for _, tool := range r.tools {
		if tool.Category() == category {
			tools = append(tools, tool)
		}
	}

	return tools
}

// ListNames 列出所有工具名称
func (r *MemoryRegistry) ListNames() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}

	return names
}

// ListCategories 列出所有分类
func (r *MemoryRegistry) ListCategories() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	categoryMap := make(map[string]bool)
	for _, tool := range r.tools {
		categoryMap[tool.Category()] = true
	}

	categories := make([]string, 0, len(categoryMap))
	for category := range categoryMap {
		categories = append(categories, category)
	}

	return categories
}

// Update 更新工具
func (r *MemoryRegistry) Update(tool core.Tool) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; !exists {
		return &core.ErrToolNotFound{ToolName: name}
	}

	r.tools[name] = tool
	return nil
}

// RegisterBatch 批量注册工具
func (r *MemoryRegistry) RegisterBatch(tools []core.Tool) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// 先检查是否有重复
	for _, tool := range tools {
		name := tool.Name()
		if _, exists := r.tools[name]; exists {
			return fmt.Errorf("tool already exists: %s", name)
		}
	}

	// 批量注册
	for _, tool := range tools {
		r.tools[tool.Name()] = tool
	}

	return nil
}
