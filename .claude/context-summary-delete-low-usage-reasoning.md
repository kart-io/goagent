## 项目上下文摘要（删除低使用率推理模式）
生成时间：2025-11-30

### 1. 相似实现分析

无需参考相似实现，本任务是删除过时代码。

### 2. 项目约定
- **命名约定**：Go标准命名规范（驼峰命名）
- **文件组织**：按功能模块组织，agents/下按推理模式分目录
- **导入顺序**：标准库 → 第三方库 → 本地包
- **代码风格**：使用gofmt和golangci-lint

### 3. 待删除的推理模式

#### 目录结构
```
agents/
├── got/          ← 删除（Graph-of-Thought）
├── tot/          ← 删除（Tree-of-Thought）
├── sot/          ← 删除（Skeleton-of-Thought）
├── metacot/      ← 删除（Meta-CoT）
├── cot/          ← 保留（核心）
├── react/        ← 保留（核心）
└── pot/          ← 保留（有用）
```

#### builder/reasoning_presets.go 中的方法
需要删除的方法：
- `WithGraphOfThought()` (行327-379)
- `WithTreeOfThought()` (行95-143)
- `WithSkeletonOfThought()` (行447-493)
- `WithMetaCoT()` (行505-551)
- `WithBeamSearchToT()` (行306-312)
- `WithMonteCarloToT()` (行319-325)

需要删除的导入：
- `"github.com/kart-io/goagent/agents/got"` (行7)
- `"github.com/kart-io/goagent/agents/metacot"` (行8)
- `"github.com/kart-io/goagent/agents/sot"` (行11)
- `"github.com/kart-io/goagent/agents/tot"` (行12)

需要删除的switch case：
- BuildReasoningAgent() 中的 "tot" case (行220-228)

#### builder/reasoning_presets_test.go 中的测试
需要删除的测试函数：
- `TestWithTreeOfThought` (行73-117)
- `TestWithBeamSearchToT` (行240-253)
- `TestWithMonteCarloToT` (行256-270)
- `TestWithGraphOfThought` (行272-307)
- `TestWithSkeletonOfThought` (行344-377)
- `TestWithMetaCoT` (行379-410)

需要删除的导入：
- `"github.com/kart-io/goagent/agents/got"` (行11)
- `"github.com/kart-io/goagent/agents/metacot"` (行12)
- `"github.com/kart-io/goagent/agents/sot"` (行15)
- `"github.com/kart-io/goagent/agents/tot"` (行16)

需要删除测试中的相关调用：
- TestReasoningPresetsIntegration 中的 WithTreeOfThought() 调用 (行432)

### 4. interfaces/reasoning.go 中的策略常量

**需要保留的常量**（被 cot/react/pot 使用或通用）：
- `StrategyDepthFirst`
- `StrategyBreadthFirst`
- `StrategyGreedy`

**需要删除的常量**（仅被 ToT 使用）：
- `StrategyBeamSearch` (行195)
- `StrategyMonteCarlo` (行198)

### 5. 依赖和集成点

**被删除模式的导入位置**：
1. builder/reasoning_presets.go:6-12
2. builder/reasoning_presets_test.go:10-16

**被删除策略常量的使用位置**：
1. builder/reasoning_presets.go:104, 310, 321
2. builder/reasoning_presets_test.go:95, 103, 110, 115, 250, 266
3. agents/tot/tot.go
4. agents/tot/tot_test.go

### 6. 技术选型理由
- **为什么删除这些模式**：使用率低于5%，属于过度设计
- **为什么保留 cot/react/pot**：核心功能模式，使用率高
- **为什么彻底删除**：避免维护成本，遵循CLAUDE.md规范禁止兼容代码

### 7. 关键风险点
- **编译检查**：删除后必须确保 `go build ./...` 通过
- **测试验证**：必须确保 `go test ./builder/...` 和 `go test ./agents/...` 通过
- **引用检查**：确保没有其他地方引用被删除的模式
- **策略常量**：StrategyBeamSearch 和 StrategyMonteCarlo 仅被 ToT 使用，可以安全删除

### 8. 执行计划
1. 删除 agents/ 下的4个目录
2. 更新 builder/reasoning_presets.go（删除方法和导入）
3. 更新 builder/reasoning_presets_test.go（删除测试和导入）
4. 更新 interfaces/reasoning.go（删除策略常量）
5. 运行验证命令
