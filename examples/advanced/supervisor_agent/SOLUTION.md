# SupervisorAgent 实现方案

## 一、架构设计

### 1.1 整体架构

```
┌──────────────────────────────────────────────────────────────┐
│                     SupervisorAgent                          │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              Task Decomposer (LLM-based)               │ │
│  │  - 使用 LLM 分析复杂任务                               │ │
│  │  - 生成子任务列表                                      │ │
│  │  - 确定子任务依赖关系                                  │ │
│  └────────────────────────────────────────────────────────┘ │
│                           ↓                                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              SubAgent Scheduler                        │ │
│  │  - 根据依赖关系调度子任务                              │ │
│  │  - 管理并发执行                                        │ │
│  │  - 处理 Agent 失败和重试                               │ │
│  └────────────────────────────────────────────────────────┘ │
│                           ↓                                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              Result Aggregator                         │ │
│  │  - 根据策略聚合子任务结果                              │ │
│  │  - 使用 LLM 生成最终输出                               │ │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
                           │
          ┌────────────────┼────────────────┐
          ▼                ▼                ▼
    ┌──────────┐     ┌──────────┐     ┌──────────┐
    │SubAgent1 │     │SubAgent2 │     │SubAgent3 │
    │ (Search) │     │(Weather) │     │(Summary) │
    └──────────┘     └──────────┘     └──────────┘
```

### 1.2 核心组件

#### 1.2.1 SupervisorAgent
**职责**：
- 接收用户任务
- 协调各组件工作
- 返回最终结果

**实现位置**：`agents/supervisor_agent.go`（已存在）

#### 1.2.2 Task Decomposer
**职责**：
- 使用 LLM 分析任务
- 生成子任务列表
- 确定执行顺序

**实现方式**：
```go
type TaskDecomposer struct {
    llm llm.Client
}

func (t *TaskDecomposer) Decompose(ctx context.Context, task string) (*DecomposedTask, error) {
    prompt := fmt.Sprintf(`
分析以下任务，将其分解为子任务：

任务：%s

请按以下 JSON 格式输出：
{
  "subtasks": [
    {
      "id": "task_1",
      "description": "子任务描述",
      "agent": "负责的 agent 名称",
      "dependencies": ["task_id_1", "task_id_2"]
    }
  ]
}
`, task)

    response, err := t.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: prompt},
        },
    })

    // 解析 JSON 响应
    var decomposed DecomposedTask
    err = json.Unmarshal([]byte(response.Content), &decomposed)
    return &decomposed, err
}
```

#### 1.2.3 SubAgent Scheduler
**职责**：
- 根据依赖关系调度任务
- 管理并发执行
- 处理失败和重试

**实现方式**：
```go
type Scheduler struct {
    maxConcurrency int
    timeout        time.Duration
}

func (s *Scheduler) Execute(ctx context.Context, tasks []*SubTask, agents map[string]core.Agent) ([]*SubTaskResult, error) {
    // 构建依赖图
    graph := buildDependencyGraph(tasks)

    // 拓扑排序确定执行顺序
    executionOrder := topologicalSort(graph)

    results := make([]*SubTaskResult, 0, len(tasks))

    // 分层执行（同一层的任务可以并行）
    for _, layer := range executionOrder {
        layerResults := s.executeLayer(ctx, layer, agents)
        results = append(results, layerResults...)
    }

    return results, nil
}

func (s *Scheduler) executeLayer(ctx context.Context, tasks []*SubTask, agents map[string]core.Agent) []*SubTaskResult {
    // 使用 goroutine pool 并行执行
    sem := make(chan struct{}, s.maxConcurrency)
    results := make([]*SubTaskResult, len(tasks))

    var wg sync.WaitGroup
    for i, task := range tasks {
        wg.Add(1)
        go func(idx int, t *SubTask) {
            defer wg.Done()
            sem <- struct{}{}        // 获取信号量
            defer func() { <-sem }() // 释放信号量

            agent := agents[t.AgentName]
            result := s.executeTask(ctx, agent, t)
            results[idx] = result
        }(i, task)
    }

    wg.Wait()
    return results
}
```

#### 1.2.4 Result Aggregator
**职责**：
- 根据策略聚合结果
- 生成最终输出

**实现方式**：
```go
type Aggregator interface {
    Aggregate(ctx context.Context, results []*SubTaskResult) (interface{}, error)
}

// 并行聚合：简单合并
type ParallelAggregator struct{}

func (a *ParallelAggregator) Aggregate(ctx context.Context, results []*SubTaskResult) (interface{}, error) {
    output := make(map[string]interface{})
    for _, result := range results {
        output[result.TaskName] = result.Output
    }
    return output, nil
}

// 层次聚合：使用 LLM 综合
type HierarchyAggregator struct {
    llm llm.Client
}

func (a *HierarchyAggregator) Aggregate(ctx context.Context, results []*SubTaskResult) (interface{}, error) {
    // 构建摘要 prompt
    var summary strings.Builder
    summary.WriteString("请综合以下子任务的结果，生成最终答案：\n\n")

    for _, result := range results {
        summary.WriteString(fmt.Sprintf("子任务：%s\n结果：%v\n\n", result.TaskName, result.Output))
    }

    response, err := a.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: summary.String()},
        },
    })

    return response.Content, err
}

// 协商聚合：多个 Agent 达成共识
type ConsensusAggregator struct {
    llm llm.Client
}

func (a *ConsensusAggregator) Aggregate(ctx context.Context, results []*SubTaskResult) (interface{}, error) {
    prompt := a.buildConsensusPrompt(results)

    response, err := a.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: prompt},
        },
    })

    return response.Content, err
}
```

## 二、数据结构设计

### 2.1 核心数据结构

```go
// SupervisorAgent 配置
type SupervisorConfig struct {
    // 聚合策略
    AggregationStrategy string // "parallel", "hierarchy", "consensus", "voting"

    // 最大并发数
    MaxConcurrency int

    // 超时时间
    Timeout time.Duration

    // 是否启用容错
    EnableFallback bool

    // 重试次数
    MaxRetries int

    // LLM 配置
    LLMConfig *llm.Config
}

// 子任务定义
type SubTask struct {
    ID           string   // 任务 ID
    Description  string   // 任务描述
    AgentName    string   // 负责的 Agent 名称
    Dependencies []string // 依赖的任务 ID
    Input        interface{} // 输入数据
}

// 子任务结果
type SubTaskResult struct {
    TaskID    string        // 任务 ID
    TaskName  string        // 任务名称
    AgentName string        // 执行的 Agent
    Output    interface{}   // 输出结果
    Error     error         // 错误信息
    Duration  time.Duration // 执行耗时
    TokenUsage *interfaces.TokenUsage // Token 使用
}

// 分解后的任务
type DecomposedTask struct {
    OriginalTask string      `json:"original_task"`
    SubTasks     []*SubTask  `json:"subtasks"`
    Strategy     string      `json:"strategy"` // 建议的聚合策略
}

// 执行统计
type ExecutionStats struct {
    TotalTasks      int
    SuccessfulTasks int
    FailedTasks     int
    TotalDuration   time.Duration
    TotalTokens     int
    SubAgentStats   map[string]*AgentStats
}

type AgentStats struct {
    Invocations int
    Successes   int
    Failures    int
    AvgDuration time.Duration
    TotalTokens int
}
```

### 2.2 接口定义

```go
// SupervisorAgent 接口（扩展 core.Agent）
type SupervisorAgent interface {
    core.Agent

    // 添加子 Agent
    AddSubAgent(name string, agent core.Agent) error

    // 移除子 Agent
    RemoveSubAgent(name string) error

    // 列出所有子 Agent
    ListSubAgents() []string

    // 设置聚合策略
    SetAggregationStrategy(strategy string) error

    // 获取执行统计
    GetStats() *ExecutionStats
}

// 聚合器接口
type Aggregator interface {
    Aggregate(ctx context.Context, results []*SubTaskResult) (interface{}, error)
    Name() string
}

// 调度器接口
type Scheduler interface {
    Execute(ctx context.Context, tasks []*SubTask, agents map[string]core.Agent) ([]*SubTaskResult, error)
}
```

## 三、实现细节

### 3.1 并发控制

使用 `semaphore` 模式控制并发：

```go
type ConcurrencyController struct {
    maxConcurrency int
    sem            chan struct{}
}

func NewConcurrencyController(max int) *ConcurrencyController {
    return &ConcurrencyController{
        maxConcurrency: max,
        sem:            make(chan struct{}, max),
    }
}

func (c *ConcurrencyController) Acquire(ctx context.Context) error {
    select {
    case c.sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (c *ConcurrencyController) Release() {
    <-c.sem
}
```

### 3.2 依赖关系处理

使用拓扑排序算法：

```go
func topologicalSort(tasks []*SubTask) [][]*SubTask {
    // 构建入度表和邻接表
    inDegree := make(map[string]int)
    adjList := make(map[string][]*SubTask)
    taskMap := make(map[string]*SubTask)

    for _, task := range tasks {
        taskMap[task.ID] = task
        inDegree[task.ID] = len(task.Dependencies)

        for _, dep := range task.Dependencies {
            adjList[dep] = append(adjList[dep], task)
        }
    }

    // BFS 分层
    layers := make([][]*SubTask, 0)
    queue := make([]*SubTask, 0)

    // 找到所有入度为 0 的任务（第一层）
    for _, task := range tasks {
        if inDegree[task.ID] == 0 {
            queue = append(queue, task)
        }
    }

    for len(queue) > 0 {
        layerSize := len(queue)
        currentLayer := make([]*SubTask, layerSize)
        copy(currentLayer, queue)
        layers = append(layers, currentLayer)

        // 处理下一层
        queue = queue[:0]
        for _, task := range currentLayer {
            for _, nextTask := range adjList[task.ID] {
                inDegree[nextTask.ID]--
                if inDegree[nextTask.ID] == 0 {
                    queue = append(queue, nextTask)
                }
            }
        }
    }

    return layers
}
```

### 3.3 容错与重试

```go
func (s *Scheduler) executeTaskWithRetry(ctx context.Context, agent core.Agent, task *SubTask, maxRetries int) *SubTaskResult {
    var lastErr error

    for attempt := 0; attempt <= maxRetries; attempt++ {
        result, err := agent.Invoke(ctx, &core.AgentInput{
            Task: task.Description,
        })

        if err == nil {
            return &SubTaskResult{
                TaskID:    task.ID,
                TaskName:  task.Description,
                AgentName: task.AgentName,
                Output:    result.Result,
                Error:     nil,
            }
        }

        lastErr = err

        // 指数退避
        if attempt < maxRetries {
            backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
            select {
            case <-time.After(backoff):
            case <-ctx.Done():
                return &SubTaskResult{
                    TaskID:   task.ID,
                    Error:    ctx.Err(),
                }
            }
        }
    }

    return &SubTaskResult{
        TaskID:    task.ID,
        TaskName:  task.Description,
        AgentName: task.AgentName,
        Error:     fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr),
    }
}
```

### 3.4 Token 统计

```go
func aggregateTokenUsage(results []*SubTaskResult) *interfaces.TokenUsage {
    total := &interfaces.TokenUsage{}

    for _, result := range results {
        if result.TokenUsage != nil {
            total.PromptTokens += result.TokenUsage.PromptTokens
            total.CompletionTokens += result.TokenUsage.CompletionTokens
            total.TotalTokens += result.TokenUsage.TotalTokens
        }
    }

    return total
}
```

## 四、示例实现

### 4.1 旅行规划助手示例

```go
func TravelPlannerExample() {
    // 1. 创建 LLM 客户端
    llmClient, _ := providers.NewDeepSeek(&llm.Config{
        APIKey: os.Getenv("DEEPSEEK_API_KEY"),
        Model:  "deepseek-chat",
    })

    // 2. 创建子 Agent
    searchAgent := createSearchAgent(llmClient)
    weatherAgent := createWeatherAgent(llmClient)
    recommendAgent := createRecommendAgent(llmClient)

    // 3. 创建 SupervisorAgent
    config := agents.DefaultSupervisorConfig()
    config.AggregationStrategy = agents.StrategyHierarchy

    supervisor := agents.NewSupervisorAgent(llmClient, config)
    supervisor.AddSubAgent("search", searchAgent)
    supervisor.AddSubAgent("weather", weatherAgent)
    supervisor.AddSubAgent("recommend", recommendAgent)

    // 4. 执行任务
    result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
        Task: "我想去巴黎旅行，帮我了解天气、推荐景点和美食",
    })

    // 5. 输出结果
    fmt.Printf("旅行规划：%v\n", result.Result)
}
```

### 4.2 代码审查示例

```go
func CodeReviewExample() {
    llmClient, _ := providers.NewOpenAI(&llm.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
        Model:  "gpt-4",
    })

    // 创建专业审查 Agent
    securityAgent := createSecurityReviewAgent(llmClient)
    performanceAgent := createPerformanceReviewAgent(llmClient)
    readabilityAgent := createReadabilityReviewAgent(llmClient)

    // 使用协商策略
    config := agents.DefaultSupervisorConfig()
    config.AggregationStrategy = agents.StrategyConsensus

    supervisor := agents.NewSupervisorAgent(llmClient, config)
    supervisor.AddSubAgent("security", securityAgent)
    supervisor.AddSubAgent("performance", performanceAgent)
    supervisor.AddSubAgent("readability", readabilityAgent)

    codeToReview := `
func processData(data []byte) error {
    // 代码实现
}
`

    result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
        Task: fmt.Sprintf("请审查以下代码：\n%s", codeToReview),
    })

    fmt.Printf("审查结果：%v\n", result.Result)
}
```

## 五、测试策略

### 5.1 单元测试

```go
func TestSupervisorAgent_BasicExecution(t *testing.T) {
    // 创建 mock LLM
    mockLLM := &MockLLMClient{
        CompleteFn: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
            return &llm.CompletionResponse{
                Content: "test response",
            }, nil
        },
    }

    // 创建 mock SubAgents
    agent1 := testhelpers.NewMockAgent("agent1")
    agent1.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
        return &core.AgentOutput{Result: "result1"}, nil
    })

    // 创建 SupervisorAgent
    supervisor := agents.NewSupervisorAgent(mockLLM, agents.DefaultSupervisorConfig())
    supervisor.AddSubAgent("agent1", agent1)

    // 执行测试
    result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
        Task: "test task",
    })

    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

### 5.2 集成测试

```go
func TestSupervisorAgent_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // 使用真实的 LLM
    llmClient, err := providers.NewDeepSeek(&llm.Config{
        APIKey: os.Getenv("DEEPSEEK_API_KEY"),
    })
    require.NoError(t, err)

    // 创建真实场景的 SubAgents
    searchAgent := createRealSearchAgent(llmClient)
    weatherAgent := createRealWeatherAgent(llmClient)

    supervisor := agents.NewSupervisorAgent(llmClient, agents.DefaultSupervisorConfig())
    supervisor.AddSubAgent("search", searchAgent)
    supervisor.AddSubAgent("weather", weatherAgent)

    result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
        Task: "Search for Paris and get its weather",
    })

    assert.NoError(t, err)
    assert.NotNil(t, result)
    // 验证结果包含天气信息
}
```

### 5.3 性能测试

```go
func BenchmarkSupervisorAgent_ParallelExecution(b *testing.B) {
    supervisor := setupBenchmarkSupervisor()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := supervisor.Invoke(context.Background(), &core.AgentInput{
            Task: "benchmark task",
        })
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

## 六、部署与配置

### 6.1 环境变量配置

```bash
# LLM 配置
export DEEPSEEK_API_KEY="your-api-key"
export OPENAI_API_KEY="your-openai-key"

# SupervisorAgent 配置
export SUPERVISOR_MAX_CONCURRENCY=5
export SUPERVISOR_TIMEOUT=30s
export SUPERVISOR_MAX_RETRIES=3
export SUPERVISOR_STRATEGY="hierarchy"
```

### 6.2 代码配置

```go
config := &agents.SupervisorConfig{
    AggregationStrategy: agents.StrategyHierarchy,
    MaxConcurrency:      5,
    Timeout:             30 * time.Second,
    EnableFallback:      true,
    MaxRetries:          3,
    VerboseLogging:      true,
}

supervisor := agents.NewSupervisorAgent(llmClient, config)
```

## 七、监控与日志

### 7.1 日志格式

```
[Supervisor] 2025-11-19 12:00:00 INFO Task received: "研究巴黎并推荐景点"
[Supervisor] 2025-11-19 12:00:01 INFO Decomposed into 3 subtasks
[Supervisor] 2025-11-19 12:00:01 INFO Executing layer 1 (2 tasks in parallel)
[SubAgent:search] 2025-11-19 12:00:02 INFO Execution started
[SubAgent:weather] 2025-11-19 12:00:02 INFO Execution started
[SubAgent:search] 2025-11-19 12:00:03 INFO Execution completed (duration: 1.2s, tokens: 150)
[SubAgent:weather] 2025-11-19 12:00:04 INFO Execution completed (duration: 2.1s, tokens: 320)
[Supervisor] 2025-11-19 12:00:04 INFO Executing layer 2 (1 task)
[SubAgent:recommend] 2025-11-19 12:00:06 INFO Execution completed (duration: 2.0s, tokens: 400)
[Supervisor] 2025-11-19 12:00:06 INFO Aggregating results (strategy: hierarchy)
[Supervisor] 2025-11-19 12:00:07 INFO Task completed (total duration: 6.5s, total tokens: 870)
```

### 7.2 指标监控

```go
type Metrics struct {
    // 任务级指标
    TasksTotal       prometheus.Counter
    TasksSuccess     prometheus.Counter
    TasksFailure     prometheus.Counter
    TaskDuration     prometheus.Histogram

    // Agent 级指标
    AgentInvocations prometheus.CounterVec
    AgentDuration    prometheus.HistogramVec

    // Token 使用指标
    TokensUsed       prometheus.Counter

    // 并发指标
    ConcurrentTasks  prometheus.Gauge
}
```

## 八、优化建议

### 8.1 性能优化

1. **Agent 池化**：复用 Agent 实例，减少创建开销
2. **结果缓存**：对相同任务使用缓存结果
3. **Prompt 优化**：减少不必要的 Token 消耗
4. **流式处理**：支持流式输出，提高响应速度

### 8.2 可靠性优化

1. **断路器**：对频繁失败的 Agent 进行熔断
2. **降级策略**：关键 Agent 失败时的备用方案
3. **超时控制**：每个层级设置合理的超时时间
4. **健康检查**：定期检查 SubAgent 可用性

### 8.3 可扩展性优化

1. **插件化设计**：支持动态加载 Agent
2. **策略扩展**：支持自定义聚合策略
3. **分布式执行**：支持跨机器的 Agent 调度
4. **配置中心**：支持动态配置更新

## 九、已实现功能

根据现有代码 `agents/supervisor_agent.go`，以下功能已实现：

✅ **基础功能**
- SupervisorAgent 结构体
- AddSubAgent/RemoveSubAgent 方法
- 基本的 Invoke 实现

✅ **聚合策略**
- 并行聚合（StrategyParallel）
- 层次聚合（StrategyHierarchy）
- 协商聚合（StrategyConsensus）

✅ **配置管理**
- SupervisorConfig 结构
- DefaultSupervisorConfig 工厂函数

## 十、待补充功能

根据需求文档，需要补充：

🔲 **任务分解**
- 使用 LLM 自动分解复杂任务
- 生成依赖关系图

🔲 **智能调度**
- 拓扑排序算法
- 并发控制
- 重试机制

🔲 **执行统计**
- Token 使用统计
- 执行时间统计
- 成功/失败率统计

🔲 **容错增强**
- 断路器模式
- 降级策略
- 健康检查

## 十一、实现优先级

### Phase 1：核心功能（1-2 天）
- ✅ SupervisorAgent 基本结构
- ✅ 聚合策略实现
- 🔲 完善任务分解逻辑
- 🔲 实现智能调度器

### Phase 2：增强功能（2-3 天）
- 🔲 容错与重试机制
- 🔲 执行统计和监控
- 🔲 详细日志记录
- 🔲 性能优化

### Phase 3：高级功能（3-5 天）
- 🔲 投票聚合策略
- 🔲 自定义策略扩展
- 🔲 分布式执行支持
- 🔲 Agent 池化

---

**文档版本**：v1.0
**创建时间**：2025-11-19
**维护者**：GoAgent Team
