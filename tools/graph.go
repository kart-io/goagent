package tools

import (
	"fmt"
	"sync"
)

// ToolGraph 工具依赖图
//
// 管理工具之间的依赖关系，支持 DAG (有向无环图) 执行
type ToolGraph struct {
	// nodes 节点映射
	nodes map[string]*ToolNode

	// edges 边集合 (from -> to)
	edges map[string][]string

	// inDegree 入度统计
	inDegree map[string]int

	// mu 读写锁
	mu sync.RWMutex
}

// ToolNode 工具节点
type ToolNode struct {
	// ID 节点标识符
	ID string

	// Tool 工具实例
	Tool Tool

	// Input 输入参数
	Input *ToolInput

	// Dependencies 依赖的节点 ID 列表
	Dependencies []string

	// Metadata 元数据
	Metadata map[string]interface{}
}

// NewToolGraph 创建工具依赖图
func NewToolGraph() *ToolGraph {
	return &ToolGraph{
		nodes:    make(map[string]*ToolNode),
		edges:    make(map[string][]string),
		inDegree: make(map[string]int),
	}
}

// AddNode 添加节点
func (g *ToolGraph) AddNode(node *ToolNode) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}

	if node.ID == "" {
		return fmt.Errorf("node ID is required")
	}

	if node.Tool == nil {
		return fmt.Errorf("node tool is nil")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// 检查节点是否已存在
	if _, exists := g.nodes[node.ID]; exists {
		return fmt.Errorf("node with ID %s already exists", node.ID)
	}

	g.nodes[node.ID] = node
	g.inDegree[node.ID] = 0

	return nil
}

// AddEdge 添加边（依赖关系）
//
// from 依赖于 to（即 from 必须在 to 之后执行）
func (g *ToolGraph) AddEdge(from, to string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 检查节点是否存在
	if _, exists := g.nodes[from]; !exists {
		return fmt.Errorf("node %s does not exist", from)
	}
	if _, exists := g.nodes[to]; !exists {
		return fmt.Errorf("node %s does not exist", to)
	}

	// 检查是否会形成环
	if g.wouldCreateCycle(from, to) {
		return fmt.Errorf("adding edge %s -> %s would create a cycle", from, to)
	}

	// 添加边
	g.edges[to] = append(g.edges[to], from)
	g.inDegree[from]++

	// 更新节点的依赖列表
	if g.nodes[from] != nil {
		g.nodes[from].Dependencies = append(g.nodes[from].Dependencies, to)
	}

	return nil
}

// RemoveNode 移除节点
func (g *ToolGraph) RemoveNode(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.nodes[id]; !exists {
		return fmt.Errorf("node %s does not exist", id)
	}

	// 移除所有相关的边
	delete(g.nodes, id)
	delete(g.edges, id)
	delete(g.inDegree, id)

	// 移除指向此节点的边
	for nodeID, deps := range g.edges {
		newDeps := make([]string, 0)
		for _, dep := range deps {
			if dep != id {
				newDeps = append(newDeps, dep)
			} else {
				g.inDegree[nodeID]--
			}
		}
		g.edges[nodeID] = newDeps
	}

	return nil
}

// GetNode 获取节点
func (g *ToolGraph) GetNode(id string) *ToolNode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[id]
}

// GetNodes 获取所有节点
func (g *ToolGraph) GetNodes() []*ToolNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]*ToolNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// TopologicalSort 拓扑排序
//
// 返回执行顺序（节点 ID 列表）
func (g *ToolGraph) TopologicalSort() ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 复制入度图（避免修改原图）
	inDegree := make(map[string]int)
	for k, v := range g.inDegree {
		inDegree[k] = v
	}

	// 找出所有入度为 0 的节点
	queue := make([]string, 0)
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}

	// 拓扑排序
	result := make([]string, 0, len(g.nodes))

	for len(queue) > 0 {
		// 取出队首节点
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// 减少所有依赖当前节点的节点的入度
		for _, dependent := range g.edges[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// 检查是否所有节点都被访问
	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("graph contains cycle, cannot perform topological sort")
	}

	return result, nil
}

// wouldCreateCycle 检查添加边是否会形成环
func (g *ToolGraph) wouldCreateCycle(from, to string) bool {
	// 使用 DFS 检查从 from 是否能到达 to
	visited := make(map[string]bool)
	return g.dfs(from, to, visited)
}

// dfs 深度优先搜索
func (g *ToolGraph) dfs(current, target string, visited map[string]bool) bool {
	if current == target {
		return true
	}

	if visited[current] {
		return false
	}

	visited[current] = true

	// 访问所有邻接节点
	for _, neighbor := range g.edges[current] {
		if g.dfs(neighbor, target, visited) {
			return true
		}
	}

	return false
}

// Validate 验证图的有效性
func (g *ToolGraph) Validate() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 检查是否有环
	_, err := g.TopologicalSort()
	if err != nil {
		return fmt.Errorf("graph validation failed: %w", err)
	}

	// 检查所有依赖是否存在
	for nodeID, node := range g.nodes {
		for _, depID := range node.Dependencies {
			if _, exists := g.nodes[depID]; !exists {
				return fmt.Errorf("node %s depends on non-existent node %s", nodeID, depID)
			}
		}
	}

	return nil
}

// Clear 清空图
func (g *ToolGraph) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes = make(map[string]*ToolNode)
	g.edges = make(map[string][]string)
	g.inDegree = make(map[string]int)
}

// Size 返回节点数量
func (g *ToolGraph) Size() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// HasNode 检查节点是否存在
func (g *ToolGraph) HasNode(id string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, exists := g.nodes[id]
	return exists
}

// GetDependencies 获取节点的所有依赖
func (g *ToolGraph) GetDependencies(id string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	node := g.nodes[id]
	if node == nil {
		return []string{}
	}

	deps := make([]string, len(node.Dependencies))
	copy(deps, node.Dependencies)
	return deps
}

// GetDependents 获取依赖某个节点的所有节点
func (g *ToolGraph) GetDependents(id string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	dependents := make([]string, 0)
	for nodeID, node := range g.nodes {
		for _, depID := range node.Dependencies {
			if depID == id {
				dependents = append(dependents, nodeID)
				break
			}
		}
	}

	return dependents
}

// Clone 克隆图
func (g *ToolGraph) Clone() *ToolGraph {
	g.mu.RLock()
	defer g.mu.RUnlock()

	newGraph := NewToolGraph()

	// 复制节点
	for id, node := range g.nodes {
		newNode := &ToolNode{
			ID:           node.ID,
			Tool:         node.Tool,
			Input:        node.Input,
			Dependencies: make([]string, len(node.Dependencies)),
			Metadata:     make(map[string]interface{}),
		}
		copy(newNode.Dependencies, node.Dependencies)
		for k, v := range node.Metadata {
			newNode.Metadata[k] = v
		}
		newGraph.nodes[id] = newNode
	}

	// 复制边
	for from, tos := range g.edges {
		newGraph.edges[from] = make([]string, len(tos))
		copy(newGraph.edges[from], tos)
	}

	// 复制入度
	for id, degree := range g.inDegree {
		newGraph.inDegree[id] = degree
	}

	return newGraph
}

// ToolGraphBuilder 工具图构建器
type ToolGraphBuilder struct {
	graph *ToolGraph
}

// NewToolGraphBuilder 创建工具图构建器
func NewToolGraphBuilder() *ToolGraphBuilder {
	return &ToolGraphBuilder{
		graph: NewToolGraph(),
	}
}

// AddTool 添加工具
func (b *ToolGraphBuilder) AddTool(id string, tool Tool, input *ToolInput) *ToolGraphBuilder {
	node := &ToolNode{
		ID:           id,
		Tool:         tool,
		Input:        input,
		Dependencies: []string{},
		Metadata:     make(map[string]interface{}),
	}
	_ = b.graph.AddNode(node)
	return b
}

// AddDependency 添加依赖
func (b *ToolGraphBuilder) AddDependency(from, to string) *ToolGraphBuilder {
	_ = b.graph.AddEdge(from, to)
	return b
}

// Build 构建图
func (b *ToolGraphBuilder) Build() (*ToolGraph, error) {
	if err := b.graph.Validate(); err != nil {
		return nil, err
	}
	return b.graph, nil
}
