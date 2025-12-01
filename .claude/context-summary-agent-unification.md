## 项目上下文摘要（Agent接口统一）
生成时间：2025-11-30

### 1. 问题分析

**存在两个冲突的 Agent 接口定义**：

1. **interfaces/agent.go** (第19行)
   - 使用 `interfaces.Input/Output`
   - 继承 `Runnable` 接口
   - 提供标准化的 Input/Output 结构（Messages、State、Config）
   - 这是项目的**规范接口定义位置**

2. **core/agent.go** (第35行)
   - 使用 `core.AgentInput/AgentOutput`
   - 继承 `Runnable[*AgentInput, *AgentOutput]`（泛型版本）
   - 提供更具体的字段（Task、Instruction、Context等）
   - 包含 `BaseAgent` 具体实现

### 2. 相似实现分析

**实现1: interfaces/agent.go:19-36**
- 模式：接口定义（Interface Definition）
- 设计理念：标准化、通用化
- 特点：
  - 纯接口定义，无实现
  - 使用 `interfaces.Input/Output`（通用数据结构）
  - 支持消息驱动的交互模式
  - 明确注释：这是规范定义（canonical definition）

**实现2: core/agent.go:35-48**
- 模式：接口定义 + 具体实现（Interface + Implementation）
- 设计理念：实用化、优化化
- 特点：
  - 同时定义接口和 `BaseAgent` 实现
  - 使用泛型 `Runnable[*AgentInput, *AgentOutput]`
  - 提供更丰富的输入输出结构
  - 包含并发安全、内存池优化等高级特性

### 3. 项目约定

**命名约定**：
- 接口定义统一在 `interfaces/` 包
- 具体实现在 `core/` 包或子包（如 `agents/`）
- 类型别名用于兼容性过渡

**文件组织**：
- `interfaces/`: 纯接口定义
- `core/`: 基础实现和工具
- `agents/`: 特化的 Agent 实现（cot、react等）
- `performance/`: 性能优化组件

**依赖方向**：
- `core` 可以依赖 `interfaces`（core/agent.go:8 已导入）
- `interfaces` 不应依赖 `core`

### 4. 引用统计

**使用 core.Agent 的文件（18处）**：
1. reflection/reflective_agent.go:86 - 嵌入
2. builder/reasoning_presets.go:200,207 - 函数返回类型
3. performance/pool.go:21,53,143,161,197,290 - Agent池相关
4. performance/batch.go:77,94 - 批处理执行器
5. performance/benchmark_test.go - 测试
6. multiagent/system.go:120 - 多Agent系统
7. examples/supervisor_agent_example_test.go:16 - 示例
8. performance/example_test.go - 示例
9. planning/test_helpers.go:19 - 测试辅助

**使用 interfaces.Agent 的文件**：
- 0处（通过搜索未发现）

**关键发现**：
- 整个项目实际上使用的是 `core.Agent`
- `interfaces.Agent` 虽然有注释说是"规范定义"，但实际未被使用

### 5. 可复用组件清单

无需新增组件，需要：
1. 在 core/agent.go 添加类型别名指向 interfaces.Agent（如果保留兼容）
2. 或直接删除 core.Agent 定义，全面切换到 interfaces.Agent

### 6. 测试策略

**测试框架**: Go标准testing包
**测试模式**: 单元测试
**覆盖要求**:
- go build ./... - 编译通过
- go test ./... - 所有测试通过
- 重点测试引用 core.Agent 的18个文件

### 7. 依赖和集成点

**外部依赖**: 无
**内部依赖**:
- core.Agent → interfaces.Agent（统一后）
- 所有使用 core.Agent 的代码需更新

**集成方式**:
- 直接类型引用
- 不涉及运行时配置

### 8. 技术选型理由

**方案A: 删除 core.Agent，全面切换到 interfaces.Agent**
- 优势：
  - 符合项目规范（接口在interfaces包）
  - 消除重复定义
  - 代码更清晰
- 劣势：
  - 需要修改18处引用
  - interfaces.Agent 使用非泛型 Input/Output，可能需要适配
  - core.AgentInput/AgentOutput 提供的丰富字段需要迁移或保留

**方案B: 在 core 添加类型别名**
- 优势：
  - 改动最小
  - 保持向后兼容
- 劣势：
  - 仍然存在重复定义
  - 不符合"禁止向后兼容"的要求

**建议方案：混合方案（符合项目要求）**
1. **保留 interfaces.Agent 作为规范接口**
2. **删除 core/agent.go 中的 Agent 接口定义**（第35-48行）
3. **添加类型别名到 interfaces.Agent**（临时兼容）
4. **逐步将所有引用从 core.Agent 迁移到 interfaces.Agent**
5. **保留 core.BaseAgent、AgentInput、AgentOutput** 作为具体实现

**关键问题：Input/Output 不匹配**
- interfaces.Agent 使用 `*Input`（interfaces.Input）
- core.Agent 使用 `*AgentInput`（core.AgentInput）
- 需要适配层或统一数据结构

### 9. 关键风险点

**并发问题**: 无
**边界条件**:
- AgentInput/AgentOutput 与 Input/Output 的字段映射
- 泛型类型参数的兼容性

**性能瓶颈**:
- 如果需要在 Input 和 AgentInput 之间转换，可能有性能开销

**架构风险**:
- interfaces.Agent 的 Input/Output 结构过于简化（只有Messages/State/Config）
- core.AgentInput 提供了更丰富的字段（Task/Instruction/Options/SessionID等）
- 需要评估是否扩展 interfaces.Input 还是保留两套结构

### 10. 推荐方案（符合CLAUDE.md要求）

根据"禁止向后兼容"和"破坏性删除"的要求：

**阶段1：接口统一（本次任务）**
1. 在 core/agent.go 删除 Agent 接口定义
2. 添加类型别名：`type Agent = interfaces.Agent`
3. 验证编译通过

**阶段2：数据结构评估（后续任务）**
评估是否需要：
- 扩展 interfaces.Input/Output 以支持 core.AgentInput 的字段
- 或保留 core.AgentInput/AgentOutput 作为具体实现结构
- 或创建适配器

**阶段3：全面迁移（后续任务）**
- 将所有 core.Agent 引用改为 interfaces.Agent
- 移除类型别名

**本次任务范围**：仅执行阶段1，确保编译和测试通过。
