# Agent接口统一任务报告

**生成时间**：2025-11-30
**任务状态**：需要用户决策后继续

## 执行摘要

任务目标是"统一项目中的 Agent 接口定义"，将 core.Agent 删除并统一到 interfaces.Agent。

经过深入分析发现：**两个接口完全不兼容，无法直接统一**。

## 问题分析

### 接口不兼容性

**interfaces.Agent**（interfaces/agent.go:19-36）：
- 基于非泛型 `interfaces.Runnable`
- 使用 `*Input` / `*Output`
- 包含 `Plan()` 方法

**core.Agent**（core/agent.go:27-51）：
- 基于泛型 `Runnable[*AgentInput, *AgentOutput]`
- 使用 `*AgentInput` / `*AgentOutput`
- 无 `Plan()` 方法

### 使用情况

- **core.Agent**: 被18处代码实际使用
- **interfaces.Agent**: 0处引用，完全未被使用

## 可行方案

### 方案A：废弃 interfaces.Agent，新增泛型接口（推荐）

1. 在 interfaces 包新增 GenericAgent[I, O] 泛型接口
2. 标记旧 interfaces.Agent 为 Deprecated
3. 在 core 包添加别名指向新接口
4. 分阶段迁移引用

**优点**：符合规范，保留泛型优势
**缺点**：需要分阶段执行

### 方案B：扩展 interfaces.Input/Output

扩展 Input/Output 以支持所有 AgentInput 字段

**优点**：保留现有设计
**缺点**：破坏性大，丢失泛型优势

### 方案C：创建适配层

在两个接口之间创建适配器

**优点**：兼容现有代码
**缺点**：增加复杂度，性能开销

## 当前状态

已保留 core.Agent 定义，仅添加注释说明迁移计划。

**文件变更**：
- /Users/costalong/code/go/src/github.com/kart/goagent/core/agent.go (第27-30行)

**验证结果**：
- ✅ go build ./core/... (编译通过)
- ✅ go test ./core/... (测试通过)

## 建议

建议采用**方案A**，分3个阶段执行：

1. 阶段1：新增 interfaces.GenericAgent
2. 阶段2：逐步迁移引用
3. 阶段3：删除 core.Agent

## 需要确认

请确认：

1. 是否同意废弃 interfaces.Agent（非泛型版本）？
2. 是否同意新增 interfaces.GenericAgent（泛型版本）？
3. 是否接受分阶段迁移（而非一次性完成）？

确认后我将立即执行。
