# 项目上下文摘要（BaseReasoningAgent重构）
生成时间：2025-11-30

## 1. 相似实现分析

已分析7种推理Agent实现：

- **CoT** (agents/cot/cot.go): Chain-of-Thought，730行
- **ToT** (agents/tot/tot.go): Tree-of-Thought，1289行
- **ReAct** (agents/react/react.go): Reasoning+Acting，899行
- **GoT** (agents/got/got.go): Graph-of-Thought，943行
- **PoT** (agents/pot/pot.go): Program-of-Thought，907行
- **SoT** (agents/sot/sot.go): Skeleton-of-Thought，889行
- **MetaCoT** (agents/metacot/metacot.go): Meta-CoT/Self-Ask，959行

### 代码重复模式分析

**重复率90%的共同代码：**

1. **结构体字段（100%相同）：**
   ```go
   *agentcore.BaseAgent
   llm         llm.Client
   tools       []interfaces.Tool
   toolsByName map[string]interfaces.Tool
   config      XXXConfig
   ```

2. **Invoke方法结构（95%相同）：**
   ```go
   func (x *XXXAgent) Invoke(ctx, input) (*AgentOutput, error) {
       startTime := time.Now()

       // 触发开始回调（所有Agent相同）
       if err := x.triggerOnStart(ctx, input); err != nil {
           return nil, err
       }

       // 初始化输出（所有Agent相同）
       output := &agentcore.AgentOutput{
           ReasoningSteps: make([]agentcore.ReasoningStep, 0),
           ToolCalls:      make([]agentcore.ToolCall, 0),
           Metadata:       make(map[string]interface{}),
      }

       // ===== 唯一差异：推理逻辑 =====
       // CoT: 单次LLM调用+解析步骤
       // ToT: 树搜索（BFS/DFS/BeamSearch）
       // ReAct: 循环执行（Thought→Action→Observation）
       // GoT: 并行图执行
       // PoT: 代码生成+执行
       // SoT: 生成骨架+并行展开
       // MetaCoT: 递归问答

       // 构建最终输出（所有Agent相同）
       output.Status = "success"
       output.Timestamp = time.Now()
       output.Latency = time.Since(startTime)

       // 触发完成回调（所有Agent相同）
       if err := x.triggerOnFinish(ctx, output); err != nil {
           return nil, err
       }

       return output, nil
   }
   ```

3. **Stream方法（100%相同）：**
   ```go
   func (x *XXXAgent) Stream(ctx, input) (<-chan StreamChunk[*AgentOutput], error) {
       outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])
       go func() {
           defer close(outChan)
           output, err := x.Invoke(ctx, input)
           outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
               Data: output, Error: err, Done: true,
           }
       }()
       return outChan, nil
   }
   ```

4. **RunGenerator方法（90%相同）：**
   - 初始化accumulated output相同
   - yield中间结果的模式相同
   - 仅具体推理逻辑不同

5. **回调触发方法（100%相同）：**
   ```go
   func (x *XXXAgent) triggerOnStart(ctx, input) error {
       config := x.GetConfig()
       for _, cb := range config.Callbacks {
           if err := cb.OnStart(ctx, input); err != nil {
               return err
           }
       }
       return nil
   }
   // triggerOnFinish, triggerOnError完全一致
   ```

6. **WithCallbacks/WithConfig方法（95%相同）：**
   - 仅返回类型不同，逻辑完全一致

7. **错误处理方法（100%相同）：**
   ```go
   func (x *XXXAgent) handleError(ctx, output, message, err, startTime) (*AgentOutput, error) {
       output.Status = "failed"
       output.Message = message
       output.Timestamp = time.Now()
       output.Latency = time.Since(startTime)
       _ = x.triggerOnError(ctx, err)
       return output, err
   }
   ```

8. **createStepOutput辅助方法（100%相同）：**
   - 用于RunGenerator，创建中间输出快照
   - 所有Agent实现完全一致

## 2. 差异化分析

**唯一差异：推理策略实现（5-10%代码）**

| Agent | 核心差异方法 | 行数 | 描述 |
|-------|------------|------|------|
| CoT | `buildCoTPrompt`, `parseCoTResponse` | ~100行 | 构建CoT prompt，解析步骤和答案 |
| ToT | `beamSearch`, `depthFirstSearch`, `generateThoughts` | ~200行 | 树搜索算法 |
| ReAct | `executeCore`（Thought→Action→Observation循环） | ~150行 | ReAct循环 |
| GoT | `buildThoughtGraph`, `executeGraphParallel` | ~180行 | 图构建和并行执行 |
| PoT | `generateCode`, `executeCode`, `validateCode` | ~250行 | 代码生成和执行 |
| SoT | `generateSkeleton`, `elaborateSkeletonParallel` | ~150行 | 骨架生成和并行展开 |
| MetaCoT | `processSelfAsk`, `generateFollowupQuestions` | ~180行 | 递归问答 |

## 3. 项目约定

### 命名约定
- Agent类型：`XXXAgent` struct
- 配置类型：`XXXConfig` struct
- 工厂函数：`NewXXXAgent(config XXXConfig) *XXXAgent`
- 核心方法：`Invoke`, `Stream`, `RunGenerator`

### 代码风格
- 使用 `agentcore` 别名导入 `github.com/kart-io/goagent/core`
- 回调方法私有：`triggerOnXXX`
- 错误处理统一：`handleError`
- 所有公开方法添加GoDoc注释

### 文件组织
- 每个Agent独立目录：`agents/xxx/`
- 主文件：`xxx.go`
- 测试文件：`xxx_test.go`

### 导入顺序
1. 标准库
2. 第三方库
3. 项目内部库（`agentcore`, `interfaces`, `llm`等）

## 4. 可复用组件清单

**现有可复用基础：**
- `agentcore.BaseAgent`: 提供基础字段和方法
- `agentcore.AgentInput/AgentOutput`: 统一输入输出类型
- `agentcore.ReasoningStep`: 推理步骤记录
- `agentcore.ToolCall`: 工具调用记录
- `agentcore.Runnable`: Agent接口定义
- `agentcore.Generator`: 生成器模式

**缺失但可提取的复用代码：**
- 回调触发逻辑（`triggerOnStart`等）
- Stream实现（完全一致）
- RunGenerator框架（初始化+yield模式）
- 错误处理（`handleError`）
- 工具管理（`toolsByName` map）
- Token使用量累加
- createStepOutput逻辑

## 5. 测试策略

**现有测试模式（从xxx_test.go分析）：**
- 使用table-driven tests
- Mock LLM client
- 验证ReasoningSteps和ToolCalls
- 检查回调触发次数

**需要保持的测试覆盖：**
- 所有推理策略的特定逻辑
- 错误处理路径
- 回调机制
- Stream和RunGenerator

## 6. 依赖和集成点

**外部依赖：**
- `llm.Client`: LLM调用接口
- `interfaces.Tool`: 工具接口
- `agentcore.Callback`: 回调接口

**内部依赖：**
- `agentcore.BaseAgent`: 继承基础功能
- `agentErrors`: 统一错误处理

**集成方式：**
- 所有Agent实现`agentcore.Agent`接口
- 通过`agentcore.Runnable`支持链式调用
- 支持WithCallbacks和WithConfig配置

## 7. 技术选型理由

**为什么所有Agent结构如此相似：**
- 统一接口设计：便于替换和组合
- 一致的执行流程：降低使用成本
- 回调机制统一：便于监控和追踪

**优势：**
- 类型安全（泛型`Runnable[Input, Output]`）
- 可扩展（支持自定义回调）
- 可组合（WithCallbacks链式配置）

**风险：**
- 代码重复率高（目标：通过基类消除）
- 维护成本高（一个bug需要改7个文件）

## 8. 关键风险点

**并发问题：**
- GoT、SoT使用goroutine并行执行
- 需要保护共享状态（使用sync.RWMutex）
- createStepOutput需要copy而非引用

**边界条件：**
- MaxSteps/MaxDepth限制
- 超时处理（ElaborationTimeout）
- 空结果处理

**性能瓶颈：**
- 重复的LLM调用
- Token使用量累加
- 内存分配（ReasoningSteps/ToolCalls slice）

## 9. 重构策略

**采用策略模式：**

```go
// ReasoningStrategy 定义推理策略接口
type ReasoningStrategy interface {
    // Execute 执行推理逻辑，返回最终结果
    Execute(ctx context.Context, input *AgentInput, output *AgentOutput) (result interface{}, err error)

    // ExecuteWithGenerator 使用Generator模式执行（可选）
    ExecuteWithGenerator(ctx context.Context, input *AgentInput, output *AgentOutput, yield func(*AgentOutput, error) bool, startTime time.Time) (result interface{}, err error)
}

// BaseReasoningAgent 提供通用框架
type BaseReasoningAgent struct {
    *BaseAgent
    llm         llm.Client
    tools       []interfaces.Tool
    toolsByName map[string]interfaces.Tool
    strategy    ReasoningStrategy  // 策略接口
}

// Invoke 统一实现（所有Agent复用）
func (b *BaseReasoningAgent) Invoke(ctx, input) (*AgentOutput, error) {
    startTime := time.Now()

    // 触发开始回调
    if err := b.triggerOnStart(ctx, input); err != nil {
        return nil, err
    }

    // 初始化输出
    output := b.initOutput()

    // 执行策略（唯一差异）
    result, err := b.strategy.Execute(ctx, input, output)
    if err != nil {
        return b.handleError(ctx, output, "Strategy execution failed", err, startTime)
    }

    // 设置结果
    output.Result = result
    output.Status = "success"
    output.Timestamp = time.Now()
    output.Latency = time.Since(startTime)

    // 触发完成回调
    if err := b.triggerOnFinish(ctx, output); err != nil {
        return nil, err
    }

    return output, nil
}
```

**预期代码减少：**
- 每个Agent删除：~450行重复代码
- 7个Agent总计删除：~3150行
- 新增BaseReasoningAgent：~200行
- 7个策略实现：~1400行（仅保留差异化逻辑）
- 净减少：~1550行（减少约50%代码）

## 10. 实施计划

1. 创建 `agents/base/reasoning_agent.go`（BaseReasoningAgent）
2. 定义 `ReasoningStrategy` 接口
3. 重构 CoTAgent 使用 BaseReasoningAgent + CoTStrategy
4. 验证测试通过
5. 依次重构其他6个Agent
6. 删除重复代码

## 11. 验证标准

- ✅ 所有现有测试必须通过（`go test ./agents/...`）
- ✅ 无性能回归（benchmark结果不变）
- ✅ 代码行数减少≥50%
- ✅ 无新增依赖
- ✅ 保持破坏性更改（无兼容层）
