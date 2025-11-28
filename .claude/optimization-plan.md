# GoAgent 代码优化任务规划

**生成时间**: 2025-11-28
**基于**: code-review-report.md 审查报告
**目标**: 解决高优先级技术债务,提升代码质量

---

## 任务优先级与分配

### 🔴 高优先级任务 (必须完成)

#### 任务1: 统一Agent接口定义 (TD-1)
**Agent类型**: kiro-task-executor
**预计工时**: 4-6小时
**复杂度**: 高
**影响范围**: interfaces/, core/, agents/

**问题描述**:
- interfaces.Agent 和 core.Agent 两套接口并存
- 导致跨包类型不兼容
- Capabilities()方法不一致

**实施步骤**:
1. 分析两套接口的差异
2. 在interfaces.Agent中添加Capabilities()方法
3. 移除core.Agent,保留interfaces.Agent
4. 更新所有引用core.Agent的代码
5. 运行测试验证无破坏性变更

**验收标准**:
- [ ] interfaces.Agent包含所有必要方法
- [ ] core.Agent已移除
- [ ] 所有测试通过
- [ ] 无类型转换错误

---

#### 任务2: 修复并发安全问题 (TD-4)
**Agent类型**: kiro-task-executor
**预计工时**: 2-3小时
**复杂度**: 中
**影响范围**: core/agent.go

**问题描述**:
- AgentInput.Context是普通map,并发访问不安全
- 可能导致竞态条件和panic

**实施步骤**:
1. 将AgentInput.Context类型从map改为sync.Map
2. 更新Context的读写方法
3. 添加并发安全测试
4. 运行go test -race验证

**验收标准**:
- [ ] Context使用sync.Map或加锁保护
- [ ] go test -race无竞态警告
- [ ] 添加并发测试用例
- [ ] 性能无明显下降

---

#### 任务3: 提升builder模块测试覆盖率 (TD-2)
**Agent类型**: kiro-task-executor
**预计工时**: 6-8小时
**复杂度**: 中
**影响范围**: builder/

**问题描述**:
- builder模块测试覆盖率仅44.2%
- 核心API缺乏充分测试
- 生产风险高

**实施步骤**:
1. 分析未覆盖代码路径
2. 添加边界条件测试
3. 添加错误场景测试
4. 添加集成测试
5. 验证覆盖率达到80%

**验收标准**:
- [ ] 测试覆盖率 ≥ 80%
- [ ] 所有公共API有测试
- [ ] 包含边界条件测试
- [ ] 包含错误处理测试

---

#### 任务4: 添加工具输入验证 (TD-3)
**Agent类型**: kiro-task-executor
**预计工时**: 3-4小时
**复杂度**: 中
**影响范围**: interfaces/tool.go, tools/

**问题描述**:
- Tool.Execute接受map[string]interface{},缺乏类型验证
- 存在安全风险和运行时错误风险

**实施步骤**:
1. 定义ToolInput验证接口
2. 为常用工具添加输入验证
3. 添加类型检查和范围检查
4. 添加验证测试

**验收标准**:
- [ ] 定义了Validator接口
- [ ] 核心工具实现了验证
- [ ] 验证失败返回明确错误
- [ ] 添加验证测试用例

---

### 🟡 中优先级任务 (计划修复)

#### 任务5: 拆分大文件
**Agent类型**: kiro-task-executor
**预计工时**: 8-10小时
**复杂度**: 高
**影响范围**: agents/tot/, agents/metacot/, agents/got/, agents/pot/, agents/react/

**目标文件**:
- agents/tot/tot.go (1288行 → <500行)
- agents/metacot/metacot.go (958行 → <500行)
- agents/got/got.go (942行 → <500行)
- agents/pot/pot.go (906行 → <500行)
- agents/react/react.go (898行 → <500行)

**拆分策略**:
```
agents/tot/
  ├── tot.go (核心接口与Agent定义, <200行)
  ├── tree.go (树结构和节点管理, <200行)
  ├── search.go (搜索算法BFS/DFS, <200行)
  ├── pruning.go (剪枝策略, <200行)
  └── evaluator.go (节点评估器, <200行)
```

---

#### 任务6: 简化Runnable接口
**Agent类型**: kiro-feature-designer
**预计工时**: 4-6小时
**复杂度**: 高
**影响范围**: core/runnable.go, 所有实现

**问题描述**:
- Runnable接口方法过多(6个)
- Pipe方法返回类型any,丧失类型安全
- 应该拆分为多个小接口

**设计方案**:
```go
// 最小Runnable接口
type Runnable[I, O any] interface {
    Invoke(ctx context.Context, input I) (O, error)
}

// 流式能力
type Streamable[I, O any] interface {
    Runnable[I, O]
    Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error)
}

// 批量处理能力
type Batchable[I, O any] interface {
    Runnable[I, O]
    Batch(ctx context.Context, inputs []I) ([]O, error)
}
```

---

#### 任务7: 添加集成测试
**Agent类型**: kiro-task-executor
**预计工时**: 6-8小时
**复杂度**: 中
**影响范围**: 新建 tests/integration/

**测试场景**:
1. 多Agent协作场景
2. 长时间运行场景(内存泄漏检测)
3. 高并发场景(压力测试)
4. 故障恢复场景(断点续传)

---

#### 任务8: 简化AgentBuilder泛型
**Agent类型**: kiro-feature-designer
**预计工时**: 4-6小时
**复杂度**: 高
**影响范围**: builder/

**问题描述**:
- AgentBuilder[C, S]泛型参数增加复杂度
- 大多数场景不需要泛型
- 影响用户体验

**设计方案**:
```go
// 简化版Builder
type AgentBuilder struct {
    llmClient llm.Client
    tools     []Tool
    config    *AgentConfig
    state     interfaces.State
    context   map[string]interface{}
}
```

---

## 执行策略

### 阶段1: 关键问题修复 (第1周)

**并行执行**:
- Agent 1: 任务2 (修复并发安全) - 优先级最高,风险最大
- Agent 2: 任务4 (添加工具验证) - 安全相关,优先处理

**顺序执行**:
- Agent 3: 任务1 (统一接口定义) - 影响范围大,需要单独处理

### 阶段2: 测试覆盖提升 (第2周)

**顺序执行**:
- Agent 4: 任务3 (提升builder测试) - 依赖阶段1完成

### 阶段3: 架构优化 (第3-4周)

**设计阶段** (使用kiro-feature-designer):
- 任务6: 设计Runnable接口重构方案
- 任务8: 设计AgentBuilder简化方案

**实施阶段** (使用kiro-task-executor):
- 任务5: 拆分大文件
- 任务7: 添加集成测试

---

## 执行命令

### 立即执行 (阶段1)

#### 执行任务2: 修复并发安全问题
```bash
/kiro:execute goagent "修复AgentInput.Context并发安全问题,将map改为sync.Map,添加并发测试"
```

#### 执行任务4: 添加工具输入验证
```bash
/kiro:execute goagent "为Tool接口添加输入验证机制,定义Validator接口,实现核心工具的验证逻辑"
```

#### 执行任务1: 统一Agent接口定义
```bash
/kiro:execute goagent "统一Agent接口定义,在interfaces.Agent中添加Capabilities方法,移除core.Agent重复定义,更新所有引用"
```

### 后续执行 (阶段2-3)

```bash
# 阶段2: 提升测试覆盖率
/kiro:execute goagent "提升builder模块测试覆盖率至80%,添加边界条件测试和错误处理测试"

# 阶段3: 设计阶段
/kiro:design "Runnable接口重构方案,拆分为最小接口组合"
/kiro:design "AgentBuilder简化方案,移除泛型复杂度"

# 阶段3: 实施阶段
/kiro:execute goagent "拆分agents/tot/tot.go大文件为多个小文件"
/kiro:execute goagent "添加集成测试覆盖多Agent协作场景"
```

---

## 预期成果

### 代码质量提升
- 测试覆盖率: 73% → 85%
- 并发安全: 100%通过race检测
- 接口一致性: 统一使用interfaces.Agent
- 文件规模: 所有文件 < 500行

### 技术债务清理
- 高优先级债务: 4/4 完成 (100%)
- 中优先级债务: 4/4 完成 (100%)
- 综合评分: 82 → 90+

### 开发体验改善
- API一致性提升
- 文档完善度提高
- 新手上手更容易
- 维护成本降低

---

**执行负责人**: Claude Code + Kiro Agents
**预计完成时间**: 4周
**下次审查**: 完成后1周
