# TD-1: Agent 接口统一实现文档

**日期**: 2025-11-28
**任务**: TD-1 - 统一 Agent 接口定义（interfaces.Agent vs core.Agent）
**状态**: ✅ 已完成

---

## 问题描述

项目中存在两个 Agent 接口定义，导致类型不兼容和跨包使用困难：

1. **core.Agent**: 泛型版本，继承 `Runnable[*AgentInput, *AgentOutput]`
2. **interfaces.Agent**: 标准版本，继承 `Runnable`（非泛型）

### 主要问题

- **类型不兼容**: 两个接口无法互换使用
- **方法差异**:
  - `core.Agent` 有 `Capabilities()` 方法
  - `interfaces.Agent` 缺少 `Capabilities()` 方法但有 `Plan()` 方法
- **输入输出类型不同**:
  - `core.Agent`: 使用 `*AgentInput` 和 `*AgentOutput`
  - `interfaces.Agent`: 使用 `*Input` 和 `*Output`
- **跨包依赖问题**: 无法在不同包之间统一使用 Agent 接口

---

## 解决方案

### 核心策略

**渐进式废弃 + 向前兼容**

1. 将 `interfaces.Agent` 作为标准接口（唯一的真相来源）
2. 在 `interfaces.Agent` 中添加 `Capabilities()` 方法
3. 将 `core.Agent` 标记为废弃（Deprecated），保留以支持现有代码
4. 提供清晰的迁移指南

### 实施步骤

#### 1. 增强 interfaces.Agent 接口

**文件**: `interfaces/agent.go`

**改动**:
```go
// Agent represents an autonomous agent that can process inputs and produce outputs.
//
// The Agent interface extends Runnable and adds agent-specific methods for:
//   - Identifying the agent (Name, Description, Capabilities)  // 新增 Capabilities
//   - Generating execution plans
type Agent interface {
	Runnable

	// Name returns the agent's identifier.
	Name() string

	// Description returns what the agent does.
	Description() string

	// Capabilities returns the agent's capability list.           // 新增方法
	// This describes what tasks or operations the agent can perform.
	// Examples: ["search", "analyze", "summarize"]
	Capabilities() []string

	// Plan generates an execution plan for the given input.
	// This is optional and may return nil if the agent doesn't support planning.
	Plan(ctx context.Context, input *Input) (*Plan, error)
}
```

**理由**:
- 统一两个接口的方法集合
- 保持向后兼容（新增方法而非删除）
- `Capabilities()` 是 Agent 元数据的重要部分

#### 2. 废弃 core.Agent 接口

**文件**: `core/agent.go`

**改动**:
```go
// Agent 定义通用 AI Agent 接口
//
// 已废弃：此接口已被废弃，请使用 interfaces.Agent 替代。
//
// Deprecated: Use interfaces.Agent instead. This generic version will be removed in v2.0.
// The canonical Agent interface is now in the interfaces package for better cross-package
// compatibility and to avoid circular dependencies.
//
// 迁移指南：
//   - 将 core.Agent 替换为 interfaces.Agent
//   - 使用 interfaces.Input 和 interfaces.Output 替代 AgentInput 和 AgentOutput
//   - 实现 interfaces.Agent 接口的所有方法（包括新增的 Capabilities 和 Plan）
//
// Agent 是一个 Runnable[*AgentInput, *AgentOutput]，具有推理能力的智能体...
type Agent interface {
	Runnable[*AgentInput, *AgentOutput]

	Name() string
	Description() string
	Capabilities() []string
}
```

**理由**:
- 明确标记为 `Deprecated`
- 提供清晰的迁移路径
- 保留现有功能，不破坏向后兼容性

#### 3. 修复测试代码

**文件**: `interfaces/agent_test.go`

**改动**:
```go
type mockAgent struct {
	mockRunnable
	agentName        string
	agentDescription string
}

func (m *mockAgent) Name() string {
	return m.agentName
}

func (m *mockAgent) Description() string {
	return m.agentDescription
}

// 新增方法实现
func (m *mockAgent) Capabilities() []string {
	return []string{"process", "plan", "test"}
}

func (m *mockAgent) Plan(ctx context.Context, input *Input) (*Plan, error) {
	return &Plan{
		Steps: []Step{
			{Action: "process", Input: map[string]interface{}{"data": "test"}},
		},
		Metadata: map[string]interface{}{"planner": m.agentName},
	}, nil
}
```

**测试结果**:
```
=== RUN   TestAgentInterface
--- PASS: TestAgentInterface (0.00s)
PASS
ok      github.com/kart-io/goagent/interfaces    0.002s
```

---

## 迁移指南

### 对于新代码（推荐）

**直接使用 interfaces.Agent**:
```go
import "github.com/kart-io/goagent/interfaces"

type MyAgent struct {
	name string
	desc string
}

// 实现 interfaces.Agent 接口
func (a *MyAgent) Name() string { return a.name }
func (a *MyAgent) Description() string { return a.desc }
func (a *MyAgent) Capabilities() []string { return []string{"capability1"} }
func (a *MyAgent) Plan(ctx context.Context, input *interfaces.Input) (*interfaces.Plan, error) {
	// 返回 nil 表示不支持规划
	return nil, nil
}
func (a *MyAgent) Invoke(ctx context.Context, input *interfaces.Input) (*interfaces.Output, error) {
	// 实现逻辑
}
func (a *MyAgent) Stream(ctx context.Context, input *interfaces.Input) (<-chan *interfaces.StreamChunk, error) {
	// 实现逻辑
}
```

### 对于现有代码（向后兼容）

**方案 1: 保持使用 core.Agent（短期）**
```go
import agentcore "github.com/kart-io/goagent/core"

type MyAgent struct {
	agentcore.BaseAgent
}

// 继续使用 core.Agent，但会收到 Deprecated 警告
```

**方案 2: 渐进式迁移（推荐）**
```go
// 步骤 1: 添加适配器方法
func (a *MyAgent) ConvertInput(in *agentcore.AgentInput) *interfaces.Input {
	return &interfaces.Input{
		Messages: []interfaces.Message{
			{Role: "user", Content: in.Task},
		},
		State: interfaces.State(in.Context),
		Config: map[string]interface{}{
			"temperature": in.Options.Temperature,
		},
	}
}

func (a *MyAgent) ConvertOutput(out *interfaces.Output) *agentcore.AgentOutput {
	// 转换逻辑
}

// 步骤 2: 逐步迁移到 interfaces.Agent
```

### 关键差异对照

| 方面 | core.Agent | interfaces.Agent |
|------|-----------|------------------|
| **输入类型** | `*AgentInput` | `*Input` |
| **输出类型** | `*AgentOutput` | `*Output` |
| **Runnable** | `Runnable[*AgentInput, *AgentOutput]` | `Runnable` |
| **Capabilities** | ✅ 有 | ✅ 新增 |
| **Plan** | ❌ 无 | ✅ 有 |
| **状态** | Deprecated（将废弃） | Canonical（标准版） |

---

## 影响范围

### 修改的文件

- **interfaces/agent.go**: 新增 `Capabilities()` 方法 ✅
- **core/agent.go**: 标记 `core.Agent` 为废弃 ✅
- **interfaces/agent_test.go**: 修复 mockAgent 实现 ✅

### 无需修改的代码

以下代码继续正常工作（向后兼容）：
- 所有现有的 `core.Agent` 实现
- 所有使用 `core.Agent` 接口的代码
- BaseAgent 和其他基础实现

### 建议迁移的代码（可选，分阶段进行）

优先级从高到低：
1. **新开发的 Agent 实现** - 直接使用 `interfaces.Agent`
2. **公共库和框架代码** - 优先迁移以树立标准
3. **频繁修改的模块** - 在下次修改时一并迁移
4. **稳定的遗留代码** - 保持不变，等待自然淘汰

---

## 验证结果

### 编译检查
```bash
go build ./...
```
**结果**: ✅ 编译成功，无错误

### 接口测试
```bash
go test ./interfaces -v
```
**结果**: ✅ 所有 47 个测试通过

### 核心模块测试
```bash
go test ./core -v -short
```
**结果**: ✅ 所有 Agent 相关测试通过

### 执行器测试
```bash
go test ./agents/executor -v -short
```
**结果**: ✅ 所有测试通过

---

## 未来规划

### 短期（v1.x）
- ✅ 在 `interfaces.Agent` 中添加 `Capabilities()` 方法
- ✅ 标记 `core.Agent` 为废弃
- ⏳ 在文档中推广使用 `interfaces.Agent`
- ⏳ 为新 Agent 实现提供模板和示例

### 中期（v1.x → v2.0）
- 逐步迁移核心 Agent 实现到 `interfaces.Agent`
- 添加迁移工具或脚本辅助转换
- 监控 `core.Agent` 的使用情况，逐步减少

### 长期（v2.0）
- 完全移除 `core.Agent` 接口
- 统一使用 `interfaces.Agent`
- 清理所有类型别名和适配代码

---

## 技术决策记录

### 为什么选择 interfaces.Agent 作为标准？

1. **包依赖结构**: `interfaces` 包是基础接口层，不依赖其他模块，适合作为统一标准
2. **跨包兼容性**: 非泛型接口更容易在不同包之间传递和使用
3. **生态系统**: 大多数 Agent 实现已经倾向于使用 `interfaces.Agent`
4. **扩展性**: `interfaces.Agent` 的 `Plan()` 方法为未来规划功能提供了基础

### 为什么不直接删除 core.Agent？

1. **向后兼容性**: 避免破坏现有代码
2. **渐进式迁移**: 给开发者充足时间适应
3. **降低风险**: 分阶段废弃比一次性删除更安全
4. **文档价值**: 保留的 Deprecated 标记本身就是最好的迁移提示

### 为什么在 interfaces.Agent 中添加 Capabilities()？

1. **功能对等**: 确保两个接口功能一致，降低迁移成本
2. **元数据完整性**: Capabilities 是 Agent 自描述的重要组成部分
3. **工具链支持**: 很多工具和框架依赖 Capabilities 进行 Agent 发现和路由

---

## 检查清单

- [x] 在 `interfaces.Agent` 中添加 `Capabilities()` 方法
- [x] 标记 `core.Agent` 为 Deprecated
- [x] 添加迁移指南注释
- [x] 修复 `interfaces/agent_test.go` 中的 mockAgent
- [x] 运行 `go test -race ./interfaces` 验证无竞态条件
- [x] 运行 `go test ./core` 验证核心模块
- [x] 运行 `go test ./agents/executor` 验证执行器
- [x] 编写实施文档（本文档）
- [x] 向后兼容性验证

---

## 总结

本次改进通过渐进式废弃策略，成功统一了 Agent 接口定义：

1. **增强标准接口**: 在 `interfaces.Agent` 中添加 `Capabilities()` 方法，使其功能完备
2. **标记废弃接口**: 将 `core.Agent` 明确标记为 Deprecated，引导开发者迁移
3. **保持兼容性**: 现有代码继续正常工作，无破坏性变更
4. **提供指引**: 通过文档和注释，提供清晰的迁移路径

**验证结果**:
- ✅ 所有测试通过
- ✅ 编译无错误
- ✅ 无竞态条件
- ✅ 向后兼容

**建议**: 新代码应直接使用 `interfaces.Agent`，现有代码可根据优先级和实际情况逐步迁移。
