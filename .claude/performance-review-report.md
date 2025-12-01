# GoAgent 性能审查报告

**审查日期**: 2025-12-01
**审查人**: Performance Engineer (Claude Code)
**项目版本**: optimization 分支
**审查范围**: 热路径优化、内存管理、并发安全、缓存有效性、I/O 效率

---

## 执行摘要

### 综合评分: 78/100

**等级**: B (良好，但有改进空间)

**评分依据**:
- ✅ 代码质量: 88/100 (使用标准库，遵循最佳实践，良好的对象池设计)
- ✅ 并发安全: 85/100 (良好的锁设计，RWMutex 使用合理)
- ⚠️ 热路径优化: 70/100 (存在锁竞争和昂贵的哈希计算)
- ⚠️ 内存管理: 75/100 (对象池使用广泛，但有遗漏)
- ✅ 缓存策略: 80/100 (架构清晰，但键生成开销大)

**总体评价**:
goagent 项目在性能工程方面展现出良好的基础架构，特别是在对象池化和缓存策略方面。已经应用了 Go 1.21+ 的 `clear()` 优化，使用了 sync.Pool 减少 GC 压力。但存在一些中高优先级的性能问题需要解决，主要集中在锁竞争、缓存键生成开销和部分对象池覆盖不完整。

### 关键发现

**优势**:
- ✅ 广泛使用 sync.Pool 减少 GC 压力 (agentInputPool, chainInputPool, chainOutputPool)
- ✅ 使用 Go 1.21+ clear() 内置函数优化 map 清理
- ✅ 实现了多层缓存策略 (Agent 缓存、Tool 缓存)
- ✅ 对象池化设计合理，包含大小阈值策略 (maxContextMapSize=1000)
- ✅ 提供了全面的性能基准测试，覆盖池化、缓存、批量执行等场景
- ✅ 快慢路径分离设计 (tryAcquireFast + acquireSlow)
- ✅ 使用 atomic 操作进行统计，避免锁开销

**劣势**:
- ⚠️ AgentPool 存在锁竞争问题 (在锁内执行 O(n) 遍历)
- ⚠️ 缓存键生成使用了昂贵的 SHA256 哈希和 JSON 序列化
- ⚠️ AgentOutput 和 ToolOutput 未使用对象池
- ⚠️ AgentPool.acquireSlow 存在潜在的 goroutine 泄漏风险
- ⚠️ cleanup 操作持锁时间长，可能阻塞正常请求
- ⚠️ 缺少对大对象的特殊处理策略 (虽然有 maxContextMapSize，但策略单一)

---

## 性能问题清单

### P0 - 关键性能问题 (影响生产环境)

#### 无 P0 级别问题

当前代码库没有发现严重的性能瓶颈或关键缺陷。代码质量整体良好，基础架构设计合理。

---

### P1 - 高优先级性能问题

#### 1. AgentPool 的锁竞争问题

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/performance/pool.go`
**行号**: 162-193, 197-287
**严重程度**: P1
**影响**: 高并发场景下 Acquire 性能下降，随池大小线性退化

**问题描述**:
```go
// tryAcquireFast 中的锁持有时间过长
func (p *AgentPool) tryAcquireFast() (core.Agent, bool) {
    p.mu.Lock()
    defer p.mu.Unlock()  // 锁持有整个函数执行期间

    if p.closed {
        return nil, false
    }

    // 问题 1: 遍历整个 agents 数组，O(n) 复杂度
    for _, agent := range p.agents {
        if !agent.inUse {
            agent.inUse = true
            agent.lastUsedAt = time.Now()  // 问题 2: 在锁内调用 time.Now()
            p.stats.acquired.Add(1)
            return agent.agent, true
        }
    }

    // 问题 3: 在锁内创建新 agent
    if len(p.agents) < p.config.MaxSize {
        newAgent, err := p.createAgent()
        if err != nil {
            return nil, false
        }
        newAgent.inUse = true
        newAgent.lastUsedAt = time.Now()
        p.agents = append(p.agents, newAgent)
        p.stats.acquired.Add(1)
        return newAgent.agent, true
    }

    return nil, false
}
```

**性能影响**:
- 锁持有期间执行了 O(n) 遍历 (n=MaxSize，默认 50)
- 在锁内调用 time.Now()，增加锁持有时间
- 在锁内创建 agent (调用 factory 函数)，可能阻塞较长时间
- 高并发时会导致大量 goroutine 排队等待锁
- 随着池大小增长，性能线性下降

**优化建议**:
1. **引入空闲列表**: 使用双向链表或栈维护空闲 Agent，将查找复杂度降至 O(1)
```go
type AgentPool struct {
    // ...
    idleList *list.List  // 空闲 Agent 链表
    agentMap map[core.Agent]*list.Element  // Agent → 链表节点映射
}

func (p *AgentPool) tryAcquireFast() (core.Agent, bool) {
    p.mu.Lock()
    if p.idleList.Len() > 0 {
        elem := p.idleList.Front()
        agent := elem.Value.(*pooledAgent)
        p.idleList.Remove(elem)
        p.mu.Unlock()

        agent.inUse = true
        agent.lastUsedAt = time.Now()  // 移到锁外
        return agent.agent, true
    }
    p.mu.Unlock()
    return nil, false
}
```

2. **time.Now() 移到锁外**: 减少锁持有时间
3. **异步创建 agent**: 在锁外调用 factory 函数
4. **考虑分片锁**: 对于大池 (MaxSize > 100)，使用多个子池减少锁竞争

**预期收益**:
- 高并发场景 (100+ goroutines) 吞吐量提升 **30-50%**
- 平均延迟降低 **40-60%**
- 锁等待时间减少 **70-80%**

**实现难度**: 中等
**风险**: 低 (需要充分测试并发正确性)

---

#### 2. CachedAgent 的昂贵键生成

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/performance/cache_pool.go`
**行号**: 242-263
**严重程度**: P1
**影响**: 每次缓存查询 (包括命中) 都需要计算 SHA256 哈希

**问题描述**:
```go
func defaultKeyGenerator(input *core.AgentInput) string {
    // 问题 1: 创建临时结构体，增加堆分配
    data := struct {
        Task        string
        Instruction string
        Context     map[string]interface{}
    }{
        Task:        input.Task,
        Instruction: input.Instruction,
        Context:     input.Context,
    }

    // 问题 2: JSON 序列化开销大，特别是对于复杂 Context
    jsonBytes, err := json.Marshal(data)
    if err != nil {
        return fmt.Sprintf("%s:%s", input.Task, input.Instruction)
    }

    // 问题 3: SHA256 计算开销大 (加密级哈希，过度设计)
    hash := sha256.Sum256(jsonBytes)
    return fmt.Sprintf("%x", hash)  // 问题 4: 格式化为 hex 字符串
}
```

**性能影响** (基于代码分析):
- SHA256 计算: ~1-5 μs (取决于输入大小)
- JSON 序列化: ~0.5-2 μs (小对象) 到 10+ μs (大 Context)
- 总开销: ~2-15 μs **每次缓存查询**
- 对于高命中率场景 (90% 命中)，90% 的键生成是浪费的

**优化建议**:

**方案 1: 使用更快的哈希算法** (推荐)
```go
import "github.com/cespare/xxhash/v2"

func fastKeyGenerator(input *core.AgentInput) string {
    h := xxhash.New()
    h.WriteString(input.Task)
    h.WriteByte('|')
    h.WriteString(input.Instruction)
    h.WriteByte('|')

    // 简化 Context 序列化
    if input.Context != nil {
        // 使用更快的序列化或只哈希关键字段
        for k, v := range input.Context {
            h.WriteString(k)
            h.WriteByte(':')
            fmt.Fprintf(h, "%v", v)  // 或使用 msgpack
            h.WriteByte(';')
        }
    }

    return strconv.FormatUint(h.Sum64(), 36)
}
```

**方案 2: 缓存已计算的键** (最优)
```go
type AgentInput struct {
    // ...
    cacheKey     string
    cacheKeyOnce sync.Once
}

func (input *AgentInput) CacheKey() string {
    input.cacheKeyOnce.Do(func() {
        input.cacheKey = fastKeyGenerator(input)
    })
    return input.cacheKey
}
```

**方案 3: 简单场景直接拼接** (最快，但碰撞风险高)
```go
func simpleKeyGenerator(input *core.AgentInput) string {
    // 仅适用于 Context 很小或为空的场景
    var b strings.Builder
    b.Grow(len(input.Task) + len(input.Instruction) + 2)
    b.WriteString(input.Task)
    b.WriteByte('|')
    b.WriteString(input.Instruction)
    return b.String()
}
```

**预期收益**:
- 键生成时间: SHA256 ~5 μs → xxhash ~0.5 μs (减少 **90%**)
- 缓存查询延迟降低: **50-80%**
- CPU 使用率降低: **20-40%** (高缓存命中率场景)

**实现难度**: 小
**风险**: 低 (需要选择合适的哈希算法，平衡速度和碰撞率)

---

#### 3. SimpleToolCache 的缓存键生成性能问题

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/tools/simple_tool_cache.go`
**行号**: 154-183
**严重程度**: P1
**影响**: 工具调用的热路径性能

**问题描述**:
```go
func (c *CachedTool) generateCacheKey(input *interfaces.ToolInput) string {
    h := sha256.New()
    h.Write([]byte(c.tool.Name()))

    // 问题 1: 未排序的 map 遍历导致非确定性
    keys := make([]string, 0, len(input.Args))
    for k := range input.Args {
        keys = append(keys, k)
    }

    // 问题 2: 缺少排序，相同参数不同顺序会产生不同哈希
    for _, k := range keys {
        h.Write([]byte(k))
        h.Write([]byte(":"))
        fmt.Fprintf(h, "%v", input.Args[k])  // 问题 3: fmt.Fprintf 开销大
        h.Write([]byte("|"))
    }

    hashHex := hex.EncodeToString(h.Sum(nil))

    var builder strings.Builder
    builder.Grow(len(c.tool.Name()) + 1 + 64)
    builder.WriteString(c.tool.Name())
    builder.WriteByte(':')
    builder.WriteString(hashHex)

    return builder.String()
}
```

**性能影响**:
- SHA256 对于工具缓存来说过于昂贵
- 未排序导致缓存失效 (相同参数不同顺序被视为不同请求)
- fmt.Fprintf 动态格式化开销大
- 工具调用是高频操作，每次都执行完整哈希计算

**优化建议**:
```go
import (
    "github.com/cespare/xxhash/v2"
    "sort"
    "strconv"
)

func (c *CachedTool) generateCacheKey(input *interfaces.ToolInput) string {
    h := xxhash.New()
    h.WriteString(c.tool.Name())
    h.WriteByte(':')

    // 排序保证一致性
    keys := make([]string, 0, len(input.Args))
    for k := range input.Args {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    // 使用更高效的序列化
    for _, k := range keys {
        h.WriteString(k)
        h.WriteByte('=')

        // 根据类型选择序列化方式
        switch v := input.Args[k].(type) {
        case string:
            h.WriteString(v)
        case int:
            h.WriteString(strconv.Itoa(v))
        case int64:
            h.WriteString(strconv.FormatInt(v, 10))
        case float64:
            h.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
        case bool:
            h.WriteString(strconv.FormatBool(v))
        default:
            fmt.Fprintf(h, "%v", v)  // 降级到通用方式
        }
        h.WriteByte('&')
    }

    return c.tool.Name() + ":" + strconv.FormatUint(h.Sum64(), 36)
}
```

**预期收益**:
- 工具缓存查询性能提升: **60-90%**
- 缓存命中率提升: **5-10%** (修复未排序问题)
- CPU 使用率降低: **15-25%**

**实现难度**: 小
**风险**: 低

---

### P2 - 中优先级性能问题

#### 4. AgentPool.acquireSlow 的 goroutine 泄漏风险

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/performance/pool.go`
**行号**: 197-287
**严重程度**: P2
**影响**: 长时间运行可能导致 goroutine 泄漏

**问题描述**:
```go
func (p *AgentPool) acquireSlow(ctx context.Context) (core.Agent, error) {
    // ...
    stopWaiting := make(chan struct{})
    defer close(stopWaiting)

    go func() {
        p.mu.Lock()
        defer p.mu.Unlock()

        for {
            // 问题: 如果 resultCh 已满，这个 goroutine 可能无法退出
            select {
            case <-stopWaiting:
                return
            default:
            }

            if p.closed {
                select {
                case resultCh <- acquireResult{err: ErrPoolClosed}:
                default:  // 问题: default 分支导致 goroutine 继续循环
                }
                return
            }

            // ...

            // 问题: p.cond.Wait() 可能永久阻塞
            p.stats.waitCount.Add(1)
            p.cond.Wait()
        }
    }()

    select {
    case result := <-resultCh:
        // ...
    case <-timeoutCtx.Done():
        p.cond.Broadcast()  // 尝试唤醒等待的 goroutine
        return nil, ErrPoolTimeout
    }
}
```

**潜在问题**:
1. `resultCh` 是 buffered channel (容量=1)，但没有保证发送方能成功发送
2. 超时时调用 `Broadcast()` 唤醒，但 goroutine 可能已经退出或仍在锁外
3. 如果 `stopWaiting` channel 在 `p.cond.Wait()` 之前关闭，goroutine 可能错过信号

**优化建议**:
```go
func (p *AgentPool) acquireSlow(ctx context.Context) (core.Agent, error) {
    timeoutCtx, cancel := context.WithTimeout(ctx, p.config.AcquireTimeout)
    defer cancel()

    resultCh := make(chan acquireResult, 1)
    done := make(chan struct{})

    go func() {
        defer close(done)

        p.mu.Lock()
        defer p.mu.Unlock()

        for {
            // 使用 context 判断是否应该退出
            select {
            case <-timeoutCtx.Done():
                return
            default:
            }

            if p.closed {
                select {
                case resultCh <- acquireResult{err: ErrPoolClosed}:
                case <-timeoutCtx.Done():
                }
                return
            }

            // 查找空闲 agent...

            // 使用 context-aware 等待
            // 注意: p.cond.Wait() 会释放锁，所以需要特殊处理
            waitDone := make(chan struct{})
            go func() {
                p.cond.Wait()
                close(waitDone)
            }()

            p.mu.Unlock()
            select {
            case <-waitDone:
                p.mu.Lock()
            case <-timeoutCtx.Done():
                p.cond.Broadcast()  // 唤醒等待的 Wait()
                return
            }
        }
    }()

    select {
    case result := <-resultCh:
        return handleResult(result)
    case <-timeoutCtx.Done():
        // 等待 goroutine 退出
        <-done
        return nil, ErrPoolTimeout
    }
}
```

**预期收益**:
- 消除 goroutine 泄漏风险
- 更可靠的超时处理
- 更好的资源清理

**实现难度**: 中等
**风险**: 中 (需要仔细测试并发场景)

---

#### 5. AgentInput Context 的锁粒度问题

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/core/agent.go`
**行号**: 180-246
**严重程度**: P2
**影响**: 高频 Context 访问场景性能

**问题描述**:
```go
type AgentInput struct {
    // ...
    Context map[string]interface{} `json:"context"`
    contextMu sync.RWMutex `json:"-"`
}

// 每次访问都需要加锁
func (input *AgentInput) GetContext(key string) (interface{}, bool) {
    input.contextMu.RLock()
    defer input.contextMu.RUnlock()
    val, ok := input.Context[key]
    return val, ok
}

func (input *AgentInput) SetContext(key string, value interface{}) {
    input.contextMu.Lock()
    defer input.contextMu.Unlock()
    if input.Context == nil {
        input.Context = make(map[string]interface{})
    }
    input.Context[key] = value
}
```

**性能影响**:
- RWMutex 虽然对读操作优化，但仍有锁开销
- defer 有轻微的性能开销 (约 1-2 ns)
- 频繁的小粒度锁操作降低 CPU 缓存效率
- 对于读多写少的场景，可以进一步优化

**优化建议**:

**方案 1: 使用 sync.Map** (适用于读多写少)
```go
type AgentInput struct {
    // ...
    Context sync.Map  // 替代 map[string]interface{}
}

func (input *AgentInput) GetContext(key string) (interface{}, bool) {
    return input.Context.Load(key)  // 无锁读取
}

func (input *AgentInput) SetContext(key string, value interface{}) {
    input.Context.Store(key, value)
}
```

**方案 2: Copy-on-Write** (适用于写很少的场景)
```go
type AgentInput struct {
    // ...
    context atomic.Value  // 存储 map[string]interface{}
}

func (input *AgentInput) GetContext(key string) (interface{}, bool) {
    ctx := input.context.Load().(map[string]interface{})
    val, ok := ctx[key]
    return val, ok  // 无锁读取
}

func (input *AgentInput) SetContext(key string, value interface{}) {
    old := input.context.Load().(map[string]interface{})
    new := make(map[string]interface{}, len(old)+1)
    for k, v := range old {
        new[k] = v
    }
    new[key] = value
    input.context.Store(new)  // 原子替换
}
```

**方案 3: 去除 defer** (微优化)
```go
func (input *AgentInput) GetContext(key string) (interface{}, bool) {
    input.contextMu.RLock()
    val, ok := input.Context[key]
    input.contextMu.RUnlock()  // 直接解锁，避免 defer 开销
    return val, ok
}
```

**预期收益**:
- sync.Map 方案: 读操作性能提升 **50-100%**
- Copy-on-Write 方案: 读操作性能提升 **80-150%** (写操作变慢)
- 去除 defer: 性能提升 **5-10%** (微优化)

**实现难度**: 小到中等
**风险**: 中 (sync.Map 和 atomic.Value 有使用限制)

---

#### 6. ChainableAgent 的对象池使用不完整

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/core/agent.go`
**行号**: 569-661
**严重程度**: P2
**影响**: 链式调用场景的内存分配

**问题描述**:
```go
func (c *ChainableAgent) executeChain(ctx context.Context, input *AgentInput, useFastPath bool) (*AgentOutput, error) {
    // ...
    var pooledInput *AgentInput

    for i, agent := range c.agents {
        var output *AgentOutput
        var err error

        // 调用 agent
        output, err = agent.Invoke(ctx, currentInput)
        if err != nil {
            // ...
            return nil, err
        }

        finalOutput = output  // 问题: AgentOutput 未使用对象池

        // 只对中间步骤使用对象池
        if i < len(c.agents)-1 {
            if pooledInput != nil {
                resetAgentInput(pooledInput)
                agentInputPool.Put(pooledInput)
            }

            pooledInput = agentInputPool.Get().(*AgentInput)
            // ...
        }
    }
    // 问题: ReasoningSteps、ToolCalls 等切片也未池化
}
```

**性能影响**:
- 每个 Agent 调用都分配新的 AgentOutput
- 对于长链式调用 (10+ agents)，会产生大量临时对象
- AgentOutput 包含多个切片 (ReasoningSteps, ToolCalls)，分配开销大
- 增加 GC 压力

**优化建议**:
```go
// 1. 创建 AgentOutput 对象池
var agentOutputPool = sync.Pool{
    New: func() interface{} {
        return &AgentOutput{
            ReasoningSteps: make([]ReasoningStep, 0, 8),
            ToolCalls:      make([]ToolCall, 0, 4),
            Metadata:       make(map[string]interface{}, 4),
        }
    },
}

func GetAgentOutput() *AgentOutput {
    output := agentOutputPool.Get().(*AgentOutput)
    output.Result = nil
    output.Status = ""
    output.Message = ""
    output.ReasoningSteps = output.ReasoningSteps[:0]
    output.ToolCalls = output.ToolCalls[:0]
    output.Latency = 0
    output.Timestamp = time.Time{}
    if len(output.Metadata) > 0 {
        clear(output.Metadata)
    }
    return output
}

func PutAgentOutput(output *AgentOutput) {
    if output != nil {
        agentOutputPool.Put(output)
    }
}

// 2. 在 executeChain 中使用
func (c *ChainableAgent) executeChain(...) (*AgentOutput, error) {
    // ...
    intermediateOutputs := make([]*AgentOutput, 0, len(c.agents))
    defer func() {
        // 清理所有中间结果
        for _, out := range intermediateOutputs {
            PutAgentOutput(out)
        }
    }()

    for i, agent := range c.agents {
        output, err := agent.Invoke(ctx, currentInput)
        if err != nil {
            return nil, err
        }

        if i < len(c.agents)-1 {
            intermediateOutputs = append(intermediateOutputs, output)
        } else {
            finalOutput = output
        }
        // ...
    }

    return finalOutput, nil
}
```

**预期收益**:
- 长链式调用场景内存分配减少: **40-60%**
- GC 压力降低: **30-50%**
- 延迟降低: **10-20%** (减少 GC 暂停)

**实现难度**: 小
**风险**: 低

---

#### 7. AgentPool cleanup 的效率问题

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/performance/pool.go`
**行号**: 429-478
**严重程度**: P2
**影响**: 定期清理的 CPU 使用和锁阻塞

**问题描述**:
```go
func (p *AgentPool) cleanup() {
    p.mu.Lock()
    defer p.mu.Unlock()  // 问题: 持有全局锁进行遍历

    if p.closed {
        return
    }

    now := time.Now()

    // 双指针技术是好的，但有改进空间
    keepIdx := 0
    for i := 0; i < len(p.agents); i++ {
        agent := p.agents[i]
        shouldKeep := false

        if agent.inUse {
            shouldKeep = true
        } else if now.Sub(agent.createdAt) <= p.config.MaxLifetime {
            // 问题: 重复的 time.Sub 计算
            if now.Sub(agent.lastUsedAt) <= p.config.IdleTimeout || keepIdx < p.config.InitialSize {
                shouldKeep = true
            } else {
                p.stats.recycled.Add(1)
            }
        } else {
            p.stats.recycled.Add(1)
        }

        if shouldKeep {
            if keepIdx != i {
                p.agents[keepIdx] = agent
            }
            keepIdx++
        }
    }

    // 清除剩余元素
    for i := keepIdx; i < len(p.agents); i++ {
        p.agents[i] = nil
    }

    p.agents = p.agents[:keepIdx]
}
```

**性能影响**:
- 持有全局锁进行遍历，阻塞所有 Acquire/Release 操作
- 对于大池 (MaxSize=200+)，清理时间可能达到 100+ μs
- 重复的 time.Sub 计算
- 清理间隔固定 (1 分钟)，不够灵活

**优化建议**:
```go
// 方案 1: 快照机制减少锁持有时间
func (p *AgentPool) cleanup() {
    now := time.Now()
    maxLifetime := p.config.MaxLifetime
    idleTimeout := p.config.IdleTimeout
    initialSize := p.config.InitialSize

    // 第一阶段: 快速获取需要检查的 agents (持锁时间短)
    p.mu.Lock()
    if p.closed {
        p.mu.Unlock()
        return
    }

    snapshot := make([]*pooledAgent, len(p.agents))
    copy(snapshot, p.agents)
    p.mu.Unlock()

    // 第二阶段: 在锁外判断哪些需要清理
    toRemove := make(map[*pooledAgent]bool)
    keepCount := 0

    for _, agent := range snapshot {
        if agent.inUse {
            keepCount++
            continue
        }

        age := now.Sub(agent.createdAt)
        idleTime := now.Sub(agent.lastUsedAt)

        if age > maxLifetime || (idleTime > idleTimeout && keepCount >= initialSize) {
            toRemove[agent] = true
        } else {
            keepCount++
        }
    }

    // 第三阶段: 持锁更新 agents 切片 (持锁时间短)
    if len(toRemove) > 0 {
        p.mu.Lock()
        keepIdx := 0
        for i := 0; i < len(p.agents); i++ {
            agent := p.agents[i]
            if !toRemove[agent] {
                if keepIdx != i {
                    p.agents[keepIdx] = agent
                }
                keepIdx++
            } else {
                p.stats.recycled.Add(1)
            }
        }

        for i := keepIdx; i < len(p.agents); i++ {
            p.agents[i] = nil
        }
        p.agents = p.agents[:keepIdx]
        p.mu.Unlock()
    }
}

// 方案 2: 自适应清理间隔
func (p *AgentPool) cleanupLoop() {
    defer p.wg.Done()

    interval := p.config.CleanupInterval
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            start := time.Now()
            p.cleanup()
            duration := time.Since(start)

            // 根据清理耗时调整间隔
            if duration > 10*time.Millisecond {
                interval = interval * 2
            } else if interval > p.config.CleanupInterval {
                interval = interval / 2
            }
            ticker.Reset(interval)

        case <-p.stopCleanup:
            return
        }
    }
}
```

**预期收益**:
- 清理操作对正常请求的阻塞时间降低: **70-90%**
- 锁竞争减少: **50%**
- 自适应间隔减少不必要的清理: **30-40%**

**实现难度**: 中等
**风险**: 低到中

---

#### 8. 缺少对大 Context Map 的优化策略

**文件**: `/Users/costalong/code/go/src/github.com/kart/goagent/core/agent.go`
**行号**: 634-648, 673-679
**严重程度**: P2
**影响**: 大 Context 场景的内存效率

**问题描述**:
```go
const maxContextMapSize = 1000

// resetAgentInput 中的清理策略
func resetAgentInput(input *AgentInput) {
    // ...

    // 策略: 如果 map 过大，直接丢弃重建
    if len(input.Context) > maxContextMapSize {
        input.Context = make(map[string]interface{})
    } else if input.Context != nil {
        clear(input.Context)  // Go 1.21+ 内置函数
    }
}
```

**存在的问题**:
1. **固定阈值**: 1000 可能不适合所有场景
   - 对于简单场景，1000 太大
   - 对于复杂场景，1000 可能太小
2. **基于元素数量**: 应该基于实际内存占用
   - 1000 个小对象 vs 100 个大对象，内存占用差异巨大
3. **缺少内存监控**: 没有跟踪 map 的实际内存使用
4. **clear() 仍需遍历**: 对于大 map，clear() 仍然需要遍历所有元素

**性能影响**:
- 大 Context (1000+ 元素) 的 clear() 操作: ~10-50 μs
- 重建 map 的内存分配: ~100-500 ns (取决于容量)
- 内存驻留问题: Go map 只增长不收缩

**优化建议**:

**方案 1: 基于内存的阈值** (推荐)
```go
const (
    maxContextMapSize   = 1000      // 元素数量阈值
    maxContextMemoryKB  = 100       // 内存阈值 (KB)
)

type AgentInput struct {
    // ...
    Context       map[string]interface{}
    contextMemory int64  // 估算的内存占用 (bytes)
}

func resetAgentInput(input *AgentInput) {
    // ...

    // 策略 1: 元素过多，直接重建
    if len(input.Context) > maxContextMapSize {
        input.Context = make(map[string]interface{})
        input.contextMemory = 0
        return
    }

    // 策略 2: 内存占用过大，重建
    if input.contextMemory > maxContextMemoryKB*1024 {
        input.Context = make(map[string]interface{})
        input.contextMemory = 0
        return
    }

    // 策略 3: 正常清理
    if input.Context != nil {
        clear(input.Context)
        input.contextMemory = 0
    }
}

// SetContext 中更新内存估算
func (input *AgentInput) SetContext(key string, value interface{}) {
    input.contextMu.Lock()
    defer input.contextMu.Unlock()

    if input.Context == nil {
        input.Context = make(map[string]interface{})
    }

    // 估算新增的内存
    delta := estimateMemory(value)
    input.Context[key] = value
    atomic.AddInt64(&input.contextMemory, delta)
}

func estimateMemory(v interface{}) int64 {
    switch v := v.(type) {
    case string:
        return int64(len(v))
    case []byte:
        return int64(len(v))
    case map[string]interface{}:
        return int64(len(v) * 64)  // 粗略估算
    default:
        return 64  // 默认估算
    }
}
```

**方案 2: 多级对象池** (复杂但最优)
```go
// 根据 Context 大小选择不同的池
var (
    smallContextPool = sync.Pool{  // 0-10 个元素
        New: func() interface{} {
            return make(map[string]interface{}, 10)
        },
    }
    mediumContextPool = sync.Pool{  // 10-100 个元素
        New: func() interface{} {
            return make(map[string]interface{}, 100)
        },
    }
    largeContextPool = sync.Pool{  // 100+ 个元素
        New: func() interface{} {
            return make(map[string]interface{}, 500)
        },
    }
)

func getContext(size int) map[string]interface{} {
    if size <= 10 {
        return smallContextPool.Get().(map[string]interface{})
    } else if size <= 100 {
        return mediumContextPool.Get().(map[string]interface{})
    } else {
        return largeContextPool.Get().(map[string]interface{})
    }
}

func putContext(ctx map[string]interface{}) {
    size := len(ctx)
    clear(ctx)

    if size <= 10 {
        smallContextPool.Put(ctx)
    } else if size <= 100 {
        mediumContextPool.Put(ctx)
    } else if size <= 500 {
        largeContextPool.Put(ctx)
    }
    // 超大的直接丢弃
}
```

**方案 3: 可配置的阈值**
```go
type PoolConfig struct {
    // ...
    MaxContextSize   int  // 可配置的阈值
    MaxContextMemory int  // 可配置的内存阈值
}
```

**预期收益**:
- 大 Context 场景内存使用优化: **30-50%**
- 减少内存驻留: **40-60%**
- 提高对象池复用率: **20-30%**

**实现难度**: 中等到大
**风险**: 中

---

### P3 - 低优先级性能优化

#### 9. 过度的 defer 使用

**文件**: 多个文件
**严重程度**: P3
**影响**: 微小的性能开销 (每次 defer 约 1-2 ns)

**问题描述**:
在热路径中大量使用 defer，虽然提高了代码安全性，但在性能关键路径会产生额外开销。

**示例**:
```go
func (input *AgentInput) GetContext(key string) (interface{}, bool) {
    input.contextMu.RLock()
    defer input.contextMu.RUnlock()  // defer 开销约 1-2 ns
    val, ok := input.Context[key]
    return val, ok
}
```

**优化建议**:
对于性能关键的短函数，考虑手动解锁：
```go
func (input *AgentInput) GetContext(key string) (interface{}, bool) {
    input.contextMu.RLock()
    val, ok := input.Context[key]
    input.contextMu.RUnlock()
    return val, ok
}
```

**预期收益**: **5-10%** (微优化，仅在极高频调用场景有意义)
**实现难度**: 小
**风险**: 低 (需要确保所有路径都解锁)

---

#### 10. time.Now() 调用优化

**文件**: 多个文件
**严重程度**: P3
**影响**: time.Now() 约 20-50 ns，在热路径累积

**优化建议**:
- 批量操作时复用 time.Now() 的结果
- 使用单调时钟 (monotonic clock) 减少系统调用

```go
// 优化前
for _, agent := range agents {
    agent.lastUsedAt = time.Now()  // 每次都调用
}

// 优化后
now := time.Now()  // 只调用一次
for _, agent := range agents {
    agent.lastUsedAt = now
}
```

---

## 并发安全性分析

### 1. Goroutine 泄漏风险评估

**已发现的风险点**:

#### 高风险: AgentPool.acquireSlow
- **位置**: `performance/pool.go:197-287`
- **风险等级**: P2
- **问题**: goroutine 可能无法正确退出
- **建议**: 已在 P2-4 中详细说明

#### 中风险: 后台清理协程
- **位置**: `performance/pool.go:411-425`
- **风险等级**: P3
- **评价**: 实现良好，使用了 WaitGroup 和 stopCleanup channel
- **建议**: 添加超时保护，防止 ticker 泄漏

```go
func (p *AgentPool) cleanupLoop() {
    defer p.wg.Done()

    ticker := time.NewTicker(p.config.CleanupInterval)
    defer ticker.Stop()  // ✅ 正确停止 ticker

    for {
        select {
        case <-ticker.C:
            p.cleanup()
        case <-p.stopCleanup:
            return  // ✅ 正确退出
        }
    }
}
```

**总体评价**: 并发安全设计良好，但需要修复 acquireSlow 的潜在泄漏问题。

---

### 2. 锁竞争分析

**热点锁识别**:

1. **AgentPool.mu** (最高优先级)
   - **位置**: `performance/pool.go`
   - **访问频率**: 极高 (每次 Acquire/Release)
   - **持锁时间**: 中等到长 (O(n) 遍历)
   - **竞争程度**: 高并发场景严重
   - **建议**: P1-1 中已详细说明

2. **Registry.mu** (中优先级)
   - **位置**: `tools/registry.go`
   - **访问频率**: 中等 (注册和查询工具)
   - **持锁时间**: 短
   - **竞争程度**: 低
   - **评价**: 实现合理，无需优化

3. **AgentInput.contextMu** (中优先级)
   - **位置**: `core/agent.go`
   - **访问频率**: 高 (频繁读取 Context)
   - **持锁时间**: 短
   - **竞争程度**: 中等
   - **建议**: P2-5 中已详细说明

**锁使用最佳实践检查**:
- ✅ 使用 RWMutex 优化读多写少场景
- ✅ 避免在锁内调用外部函数 (大部分情况)
- ⚠️ 部分场景持锁时间过长 (AgentPool)
- ✅ 没有嵌套锁 (避免死锁)

**建议的测试**:
```bash
# 运行竞态检测
go test -race ./...

# 锁竞争分析
go test -bench=. -blockprofile=block.out
go tool pprof block.out
```

---

### 3. 数据竞态检查

**已审查的关键区域**:

#### ✅ AgentPool.stats (正确使用 atomic)
```go
type poolStats struct {
    created    atomic.Int64
    acquired   atomic.Int64
    released   atomic.Int64
    recycled   atomic.Int64
    waitCount  atomic.Int64
    waitTimeNs atomic.Int64
}
```
**评价**: 正确使用 atomic 操作，无数据竞态风险。

#### ✅ pooledAgent (受 AgentPool.mu 保护)
```go
type pooledAgent struct {
    agent      core.Agent
    createdAt  time.Time
    lastUsedAt time.Time
    inUse      bool
}
```
**评价**: 所有访问都在锁保护下，安全。

#### ⚠️ CachedAgent.stats (需要验证)
```go
type cacheStats struct {
    hits            atomic.Int64
    misses          atomic.Int64
    evictions       atomic.Int64
    expirations     atomic.Int64
    totalHitTimeNs  atomic.Int64
    totalMissTimeNs atomic.Int64
}
```
**评价**: 使用 atomic，但需要验证 Stats() 方法的读取是否安全。

**建议**:
```bash
# 定期运行竞态检测
go test -race -count=10 ./performance/...
go test -race -count=10 ./core/...
```

---

## 内存管理分析

### 1. 对象池使用情况总结

**已实现的对象池** (✅):

| 对象池 | 位置 | 用途 | 预分配容量 | 评价 |
|--------|------|------|------------|------|
| agentInputPool | core/agent.go:19-25 | 复用 AgentInput | Context: 无, Vars: 无 | ✅ 良好 |
| chainInputPool | core/chain.go:12-22 | 复用 ChainInput | Vars: 8, Extra: 4 | ✅ 优秀 |
| chainOutputPool | core/chain.go:24-31 | 复用 ChainOutput | StepsExecuted: 8, Metadata: 4 | ✅ 优秀 |
| valuePool | store/memory/memory.go:14-20 | 复用 store.Value | 无 | ✅ 良好 |

**缺失的对象池** (❌):

| 对象类型 | 影响场景 | 分配频率 | 优先级 | 预期收益 |
|----------|----------|----------|--------|----------|
| AgentOutput | 所有 Agent 调用 | 极高 | **P1** | 40-60% |
| ToolOutput | 工具调用 | 高 | P2 | 30-50% |
| ReasoningStep[] | 推理场景 | 中 | P3 | 20-30% |
| ToolCall[] | 工具调用场景 | 中 | P3 | 20-30% |

**建议**: 优先实现 AgentOutput 和 ToolOutput 的对象池 (已在 P2-6 中详细说明)。

---

### 2. 内存分配热点分析

**基于代码审查的预估** (需要通过 pprof 验证):

| 热点 | 位置 | 分配类型 | 频率 | 大小 | 优先级 |
|------|------|----------|------|------|--------|
| AgentOutput 分配 | 所有 Agent.Invoke | 堆分配 | 极高 | ~200B | P1 |
| 缓存键生成 | cache_pool.go:242 | 堆分配 (JSON) | 极高 | 变化 | P1 |
| Context map 增长 | agent.go:188-195 | 堆分配 | 高 | 变化 | P2 |
| StepsExecuted 切片 | chain.go:多处 | 堆分配 | 中 | ~64B | P3 |

**内存分配优化检查清单**:
- [x] 使用 sync.Pool 复用对象
- [x] 预分配切片容量
- [x] 使用 clear() 清理 map (Go 1.21+)
- [x] 避免不必要的字符串拼接
- [ ] 减少 interface{} 装箱
- [ ] 使用 []byte 代替 string (部分场景)

---

### 3. 内存泄漏风险点

#### 风险点 1: CachedAgent 的缓存增长

**文件**: `performance/cache_pool.go`
**问题**: 依赖 SimpleCache 的 TTL，缺少显式的容量限制

```go
type CacheConfig struct {
    MaxSize         int           // 最大缓存条目数
    TTL             time.Duration // 缓存过期时间
    CleanupInterval time.Duration // 清理间隔
    EnableStats     bool
    KeyGenerator    func(*core.AgentInput) string
}

func DefaultCacheConfig() CacheConfig {
    return CacheConfig{
        MaxSize:         1000,  // ✅ 有容量限制
        TTL:             10 * time.Minute,
        CleanupInterval: 1 * time.Minute,
        EnableStats:     true,
        KeyGenerator:    nil,
    }
}
```

**评价**: 已有 MaxSize 限制，但需要验证 SimpleCache 是否正确实施。

**建议**:
1. 验证 SimpleCache 的 LRU 或容量限制实现
2. 添加缓存大小的监控和报警
3. 考虑使用更严格的驱逐策略

---

#### 风险点 2: AgentPool 的 agents 切片增长

**文件**: `performance/pool.go`
**评价**: ✅ 有 MaxSize 限制和定期清理机制

```go
type PoolConfig struct {
    InitialSize     int           // ✅ 初始大小
    MaxSize         int           // ✅ 最大限制
    IdleTimeout     time.Duration // ✅ 空闲超时
    MaxLifetime     time.Duration // ✅ 最大生命周期
    AcquireTimeout  time.Duration
    CleanupInterval time.Duration // ✅ 定期清理
}
```

**建议**:
1. 监控池的实际大小和利用率
2. 根据实际负载调整配置
3. 添加内存使用的指标

---

#### 风险点 3: Context map 的无限增长

**文件**: `core/agent.go`
**评价**: ⚠️ 虽然有 maxContextMapSize 阈值，但缺少严格的强制限制

```go
func (input *AgentInput) SetContext(key string, value interface{}) {
    input.contextMu.Lock()
    defer input.contextMu.Unlock()
    if input.Context == nil {
        input.Context = make(map[string]interface{})
    }
    input.Context[key] = value  // 无限制地添加
}
```

**建议**:
1. 添加容量检查，拒绝过大的 Context
2. 实现 LRU 驱逐策略
3. 记录异常大的 Context 以便调查

```go
const maxContextEntries = 100

func (input *AgentInput) SetContext(key string, value interface{}) {
    input.contextMu.Lock()
    defer input.contextMu.Unlock()

    if input.Context == nil {
        input.Context = make(map[string]interface{})
    }

    // 检查容量
    if len(input.Context) >= maxContextEntries && input.Context[key] == nil {
        // 新键会超过限制
        return errors.New("context capacity exceeded")
    }

    input.Context[key] = value
}
```

---

### 4. GC 压力评估

**基于对象池使用的评估**:
- ✅ 已实现对象池减少临时对象分配
- ✅ 使用 clear() 而非重建 map，减少 GC 工作
- ⚠️ AgentOutput 缺失对象池，增加 GC 压力
- ⚠️ 缓存键生成的临时对象 (JSON 序列化)

**预期 GC 影响**:
- **当前**: 中等 GC 压力 (有对象池但不完整)
- **优化后**: 低 GC 压力 (完成所有对象池)

**建议的 GC 监控**:
```go
import "runtime"

func monitorGC() {
    var stats runtime.MemStats
    runtime.ReadMemStats(&stats)

    log.Printf("GC Stats:")
    log.Printf("  NumGC: %d", stats.NumGC)
    log.Printf("  PauseTotal: %v", time.Duration(stats.PauseTotalNs))
    log.Printf("  HeapAlloc: %d MB", stats.HeapAlloc/1024/1024)
    log.Printf("  HeapInuse: %d MB", stats.HeapInuse/1024/1024)
}
```

---

## I/O 效率分析

### 1. 网络请求优化

**当前状况**: 代码审查未发现明显的网络 I/O 实现 (主要在 LLM provider 层)。

**建议**:
1. **连接池**: 确保 HTTP 客户端使用连接池
2. **请求合并**: 对于批量操作，考虑合并请求
3. **超时控制**: 为所有网络请求设置合理的超时
4. **重试机制**: 实现指数退避的重试策略

```go
// 示例: HTTP 客户端配置
var httpClient = &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
        DisableKeepAlives:   false,  // 启用连接复用
    },
    Timeout: 30 * time.Second,
}
```

---

### 2. 文件操作优化

**当前状况**: 文件操作主要在测试代码中，生产代码较少涉及。

**建议**:
- 使用 bufio 进行缓冲 I/O
- 批量写入减少系统调用
- 使用 mmap 处理大文件

---

## 缓存策略分析

### 1. 多层缓存架构评估

**当前实现**:
```
┌─────────────────────┐
│  Agent 层缓存       │
│  (CachedAgent)      │
│  └─> SimpleCache    │
└─────────────────────┘
         ↓
┌─────────────────────┐
│  Tool 层缓存        │
│  (CachedTool)       │
│  └─> SimpleCache    │
└─────────────────────┘
```

**优点**:
- ✅ 架构清晰，层次分明
- ✅ 使用统一的 SimpleCache 实现
- ✅ 支持 TTL 自动过期
- ✅ 提供统计信息

**缺点**:
- ⚠️ 缓存键生成开销大 (SHA256 + JSON)
- ⚠️ 缺少缓存预热机制
- ⚠️ 没有缓存命中率监控和报警
- ⚠️ 缺少 LRU/LFU 等智能驱逐策略

---

### 2. 缓存命中率优化

**当前实现分析**:
```go
func (c *CachedAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    startTime := time.Now()

    // 生成缓存键
    cacheKey := c.config.KeyGenerator(input)

    // 尝试从缓存获取
    if output, found := c.getFromCache(cacheKey); found {
        // ✅ 有统计
        if c.config.EnableStats {
            hitTime := time.Since(startTime)
            c.stats.totalHitTimeNs.Add(int64(hitTime))
        }
        return output, nil
    }

    // 缓存未命中，执行 Agent
    output, err := c.agent.Invoke(ctx, input)
    if err != nil {
        return nil, err
    }

    // 保存到缓存
    c.putToCache(cacheKey, output)

    // ✅ 有统计
    if c.config.EnableStats {
        missTime := time.Since(startTime)
        c.stats.totalMissTimeNs.Add(int64(missTime))
    }

    return output, nil
}
```

**优化建议**:

#### 1. 缓存预热
```go
// 在系统启动时预热常用请求
func (c *CachedAgent) Warmup(ctx context.Context, inputs []*core.AgentInput) error {
    for _, input := range inputs {
        _, err := c.Invoke(ctx, input)
        if err != nil {
            return err
        }
    }
    return nil
}
```

#### 2. 智能缓存驱逐 (LRU)
```go
// 替换 SimpleCache 为 LRU Cache
import "github.com/hashicorp/golang-lru/v2"

type CachedAgent struct {
    // ...
    cache *lru.Cache[string, *CacheEntry]
}

func NewCachedAgent(agent core.Agent, config CacheConfig) *CachedAgent {
    cache, _ := lru.New[string, *CacheEntry](config.MaxSize)
    return &CachedAgent{
        agent:  agent,
        config: config,
        cache:  cache,
    }
}
```

#### 3. 缓存命中率监控
```go
// 定期报告缓存命中率
func (c *CachedAgent) monitorHitRate() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := c.Stats()
        if stats.HitRate < 50.0 {
            log.Printf("WARNING: Low cache hit rate: %.2f%%", stats.HitRate)
        }
    }
}
```

#### 4. 分层缓存策略
```go
// L1: 内存缓存 (热数据)
// L2: Redis 缓存 (温数据)
type TieredCache struct {
    l1 *cache.SimpleCache
    l2 *redis.Client
}

func (c *TieredCache) Get(key string) (interface{}, bool) {
    // 先查 L1
    if val, ok := c.l1.Get(context.Background(), key); ok {
        return val, true
    }

    // 再查 L2
    val, err := c.l2.Get(context.Background(), key).Result()
    if err == nil {
        // 提升到 L1
        c.l1.Set(context.Background(), key, val, 5*time.Minute)
        return val, true
    }

    return nil, false
}
```

---

### 3. 缓存容量规划

**当前配置**:
```go
func DefaultCacheConfig() CacheConfig {
    return CacheConfig{
        MaxSize:         1000,  // 固定值
        TTL:             10 * time.Minute,
        CleanupInterval: 1 * time.Minute,
        EnableStats:     true,
        KeyGenerator:    nil,
    }
}
```

**问题**:
- 固定的 MaxSize 不能适应不同负载
- 没有基于内存使用的动态调整
- 缺少对热点数据的特殊处理

**优化建议**:

#### 1. 自适应容量调整
```go
type AdaptiveCache struct {
    cache       *cache.SimpleCache
    maxSize     int
    currentSize int
    hitRate     float64

    // 自适应参数
    minSize     int
    maxMemoryMB int
}

func (c *AdaptiveCache) adjustCapacity() {
    stats := c.cache.GetStats()

    // 根据命中率调整容量
    if stats.HitRate > 90.0 && c.currentSize < c.maxSize {
        // 命中率高，增加容量
        c.currentSize = min(c.currentSize*2, c.maxSize)
    } else if stats.HitRate < 50.0 && c.currentSize > c.minSize {
        // 命中率低，减少容量
        c.currentSize = max(c.currentSize/2, c.minSize)
    }

    // 根据内存使用调整
    var mem runtime.MemStats
    runtime.ReadMemStats(&mem)
    usedMB := mem.HeapInuse / 1024 / 1024

    if usedMB > uint64(c.maxMemoryMB) {
        // 内存压力大，减少缓存
        c.currentSize = max(c.currentSize/2, c.minSize)
    }
}
```

#### 2. 基于负载的配置模板
```go
// 小型应用
func SmallCacheConfig() CacheConfig {
    return CacheConfig{
        MaxSize: 100,
        TTL:     5 * time.Minute,
    }
}

// 中型应用
func MediumCacheConfig() CacheConfig {
    return CacheConfig{
        MaxSize: 1000,
        TTL:     10 * time.Minute,
    }
}

// 大型应用
func LargeCacheConfig() CacheConfig {
    return CacheConfig{
        MaxSize: 10000,
        TTL:     30 * time.Minute,
    }
}
```

---

## 性能基准测试分析

### 1. 现有基准测试覆盖

**已覆盖的场景** (✅):

| 基准测试 | 位置 | 场景 | 质量 |
|----------|------|------|------|
| BenchmarkPooledVsNonPooled | performance/benchmark_test.go:64-111 | 池化对比 | ✅ 优秀 |
| BenchmarkCachedVsUncached | performance/benchmark_test.go:113-153 | 缓存对比 | ✅ 优秀 |
| BenchmarkBatchExecution | performance/benchmark_test.go:155-201 | 批量执行 | ✅ 良好 |
| BenchmarkConcurrentPoolAccess | performance/benchmark_test.go:203-250 | 并发访问 | ✅ 优秀 |
| BenchmarkCacheHitRate | performance/benchmark_test.go:252-305 | 缓存命中率 | ✅ 优秀 |
| BenchmarkPoolWithDifferentSizes | performance/benchmark_test.go:307-354 | 不同池大小 | ✅ 良好 |
| BenchmarkGetChainInput | core/chain_pool_bench_test.go:8-20 | 对象池 | ✅ 优秀 |

**缺失的场景** (❌):

| 场景 | 重要性 | 建议 |
|------|--------|------|
| 内存分配 profiling | 高 | 使用 -benchmem 和 -memprofile |
| CPU profiling | 高 | 使用 -cpuprofile |
| 真实负载模拟 | 中 | 使用实际的 LLM 响应数据 |
| 长时间压力测试 | 中 | 运行 1 小时+ 检测泄漏 |
| 锁竞争分析 | 中 | 使用 -blockprofile |

---

### 2. 基准测试质量评估

**优点**:
- ✅ 使用 `b.ReportAllocs()` 报告内存分配
- ✅ 使用 `b.ResetTimer()` 排除初始化开销
- ✅ 提供多种并发度的对比测试
- ✅ 使用 `b.RunParallel()` 测试并发性能
- ✅ 报告自定义指标 (hit_rate, utilization)

**改进建议**:

#### 1. 添加内存 profiling
```bash
# 运行内存 profiling
go test -bench=BenchmarkPooledVsNonPooled -benchmem -memprofile=mem.out
go tool pprof -alloc_space mem.out
go tool pprof -inuse_space mem.out

# 查看内存分配火焰图
go tool pprof -http=:8080 mem.out
```

#### 2. 添加 CPU profiling
```bash
# 运行 CPU profiling
go test -bench=BenchmarkConcurrentPoolAccess -cpuprofile=cpu.out
go tool pprof cpu.out

# 查看 CPU 火焰图
go tool pprof -http=:8080 cpu.out
```

#### 3. 添加锁竞争分析
```bash
# 运行锁竞争分析
go test -bench=BenchmarkConcurrentPoolAccess -blockprofile=block.out
go tool pprof block.out
```

#### 4. 使用 benchstat 比较优化效果
```bash
# 优化前
go test -bench=. -count=10 | tee old.txt

# 优化后
go test -bench=. -count=10 | tee new.txt

# 对比
benchstat old.txt new.txt
```

**示例输出**:
```
name                    old time/op    new time/op    delta
PooledVsNonPooled-8       1.23µs ± 2%    0.65µs ± 1%  -47.15%  (p=0.000 n=10+10)

name                    old alloc/op   new alloc/op   delta
PooledVsNonPooled-8        256B ± 0%      128B ± 0%  -50.00%  (p=0.000 n=10+10)

name                    old allocs/op  new allocs/op  delta
PooledVsNonPooled-8        8.00 ± 0%      4.00 ± 0%  -50.00%  (p=0.000 n=10+10)
```

#### 5. 添加真实负载基准测试
```go
func BenchmarkRealWorldScenario(b *testing.B) {
    // 模拟真实的 Agent 输入和输出
    agent := NewMockAgent("test", 5*time.Millisecond)
    pool, _ := NewAgentPool(func() (core.Agent, error) {
        return agent, nil
    }, DefaultPoolConfig())
    defer pool.Close()

    // 真实的输入数据分布
    inputs := generateRealWorldInputs(1000)

    b.ReportAllocs()
    b.ResetTimer()

    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            input := inputs[i%len(inputs)]
            _, err := pool.Execute(context.Background(), input)
            if err != nil {
                b.Error(err)
            }
            i++
        }
    })
}
```

---

### 3. 性能回归测试建议

**CI/CD 集成**:

```yaml
# .github/workflows/benchmark.yml
name: Performance Benchmark

on:
  pull_request:
    branches: [master]

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run benchmarks
        run: |
          go test -bench=. -benchmem -count=5 | tee new.txt

      - name: Checkout base branch
        run: |
          git fetch origin ${{ github.base_ref }}
          git checkout origin/${{ github.base_ref }}

      - name: Run base benchmarks
        run: |
          go test -bench=. -benchmem -count=5 | tee old.txt

      - name: Install benchstat
        run: go install golang.org/x/perf/cmd/benchstat@latest

      - name: Compare benchmarks
        run: |
          benchstat old.txt new.txt | tee comparison.txt

      - name: Check for regression
        run: |
          # 检查是否有超过 10% 的性能退化
          if grep -q '\-[0-9]\{2\}\.' comparison.txt; then
            echo "Performance regression detected!"
            exit 1
          fi
```

---

## 性能优化建议优先级

### 高优先级 (1-2 周内完成)

| 优化项 | 编号 | 预期收益 | 工作量 | 风险 |
|--------|------|----------|--------|------|
| 优化 AgentPool 锁竞争 | P1-1 | 吞吐量 +30-50% | 中 | 低 |
| 优化缓存键生成 (Agent) | P1-2 | 延迟 -50-80% | 小 | 低 |
| 优化缓存键生成 (Tool) | P1-3 | 延迟 -60-90% | 小 | 低 |

**理由**:
- 影响最广泛的热路径
- 实现相对简单
- 风险可控
- 收益显著

**实施计划**:
1. **Week 1**:
   - 实现 P1-2 和 P1-3 (缓存键生成优化)
   - 编写基准测试验证效果
   - 代码审查和合并

2. **Week 2**:
   - 实现 P1-1 (AgentPool 优化)
   - 充分测试并发正确性
   - 代码审查和合并

---

### 中优先级 (1 个月内完成)

| 优化项 | 编号 | 预期收益 | 工作量 | 风险 |
|--------|------|----------|--------|------|
| 修复 goroutine 泄漏风险 | P2-4 | 稳定性提升 | 中 | 中 |
| 优化 Context 访问 | P2-5 | 延迟 -20-30% | 中 | 中 |
| 添加 AgentOutput 对象池 | P2-6 | 内存 -40-60% | 小 | 低 |
| 改进 cleanup 策略 | P2-7 | 锁阻塞 -70-90% | 中 | 低 |
| 优化大 Context 处理 | P2-8 | 内存 -30-50% | 大 | 中 |

**实施计划**:
1. **Week 1-2**: P2-6 (AgentOutput 对象池) - 快速见效
2. **Week 2-3**: P2-4 (修复 goroutine 泄漏) - 提高稳定性
3. **Week 3-4**: P2-7 (cleanup 优化) - 减少锁阻塞
4. **Week 4+**: P2-5, P2-8 - 根据实际需求优先级调整

---

### 低优先级 (持续改进)

| 优化项 | 编号 | 预期收益 | 工作量 | 风险 |
|--------|------|----------|--------|------|
| 减少 defer 使用 | P3-9 | 延迟 -5-10% | 小 | 低 |
| 优化 time.Now() 调用 | P3-10 | CPU -2-5% | 小 | 低 |
| 添加性能监控 | - | 可观测性 | 中 | 低 |
| 实现分布式缓存 | - | 可扩展性 | 大 | 高 |

---

## 性能监控建议

### 1. 关键性能指标 (KPI)

**必须监控** (P0):
- [ ] **Agent 执行延迟**: P50, P95, P99
- [ ] **缓存命中率**: Agent 缓存和 Tool 缓存
- [ ] **对象池利用率**: AgentPool, ChainInputPool
- [ ] **Goroutine 数量**: 检测泄漏
- [ ] **内存使用量**: HeapAlloc, HeapInuse
- [ ] **GC 暂停时间**: P50, P95, P99

**建议监控** (P1):
- [ ] 工具调用延迟
- [ ] LLM API 延迟
- [ ] 锁等待时间
- [ ] 请求队列长度
- [ ] 错误率

**高级监控** (P2):
- [ ] CPU 使用率 (per goroutine)
- [ ] 内存分配速率
- [ ] 网络 I/O
- [ ] 磁盘 I/O

---

### 2. 性能监控工具集成

#### 推荐工具栈:

**1. Prometheus + Grafana** (指标收集和可视化)
```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    agentInvokeDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "agent_invoke_duration_seconds",
            Help:    "Agent invoke duration in seconds",
            Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
        },
        []string{"agent_name", "status"},
    )

    cacheHitRate = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "cache_hit_rate",
            Help: "Cache hit rate (0-100)",
        },
        []string{"cache_type"},
    )

    poolUtilization = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "pool_utilization",
            Help: "Pool utilization percentage",
        },
        []string{"pool_name"},
    )

    goroutineCount = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "goroutine_count",
            Help: "Number of goroutines",
        },
    )
)

// 在代码中使用
func (a *BaseAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
    start := time.Now()
    defer func() {
        duration := time.Since(start).Seconds()
        agentInvokeDuration.WithLabelValues(a.name, "success").Observe(duration)
    }()

    // ...
}

// 定期更新指标
func (p *AgentPool) updateMetrics() {
    stats := p.Stats()
    poolUtilization.WithLabelValues("agent_pool").Set(stats.UtilizationPct)
    goroutineCount.Set(float64(runtime.NumGoroutine()))
}
```

**2. pprof** (CPU 和内存分析)
```go
import (
    _ "net/http/pprof"
    "net/http"
)

func init() {
    // 启动 pprof HTTP 服务器
    go func() {
        http.ListenAndServe("localhost:6060", nil)
    }()
}
```

访问方式:
```bash
# CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Heap profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:6060/debug/pprof/goroutine

# 火焰图
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30
```

**3. Jaeger/Zipkin** (分布式追踪)
```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

func (a *BaseAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
    tracer := otel.Tracer("goagent")
    ctx, span := tracer.Start(ctx, "Agent.Invoke")
    defer span.End()

    span.SetAttributes(
        attribute.String("agent.name", a.name),
        attribute.String("agent.task", input.Task),
    )

    // ...
}
```

---

### 3. 性能仪表盘设计

**Grafana 仪表盘布局**:

```
┌─────────────────────────────────────────────────┐
│ Performance Overview                            │
│                                                 │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐        │
│ │ Throughput│ │  Latency │ │Error Rate│        │
│ │  1000 QPS│ │   50ms   │ │   0.1%   │        │
│ └──────────┘ └──────────┘ └──────────┘        │
├─────────────────────────────────────────────────┤
│ Latency Distribution                            │
│                                                 │
│ ┌───────────────────────────────────────────┐  │
│ │ P50: 20ms  P95: 80ms  P99: 150ms         │  │
│ │ [Histogram Chart]                         │  │
│ └───────────────────────────────────────────┘  │
├─────────────────────────────────────────────────┤
│ Cache Performance                               │
│                                                 │
│ ┌──────────────┐ ┌──────────────┐             │
│ │ Agent Cache  │ │  Tool Cache  │             │
│ │ Hit Rate:85% │ │ Hit Rate:92% │             │
│ └──────────────┘ └──────────────┘             │
├─────────────────────────────────────────────────┤
│ Resource Utilization                            │
│                                                 │
│ ┌──────────────┐ ┌──────────────┐             │
│ │ Pool: 60%    │ │ Memory: 2GB  │             │
│ │ Goroutines:  │ │ GC Pause:5ms │             │
│ │     150      │ │              │             │
│ └──────────────┘ └──────────────┘             │
└─────────────────────────────────────────────────┘
```

**关键查询示例**:
```promql
# P95 延迟
histogram_quantile(0.95,
  rate(agent_invoke_duration_seconds_bucket[5m])
)

# 缓存命中率
cache_hit_rate{cache_type="agent"}

# 池利用率
pool_utilization{pool_name="agent_pool"}

# Goroutine 增长率
rate(goroutine_count[5m])
```

---

### 4. 报警规则

**关键报警**:

```yaml
groups:
  - name: performance_alerts
    rules:
      # P95 延迟超过 100ms
      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(agent_invoke_duration_seconds_bucket[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High P95 latency detected"
          description: "P95 latency is {{ $value }}s"

      # 缓存命中率低于 50%
      - alert: LowCacheHitRate
        expr: cache_hit_rate < 50
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Low cache hit rate"
          description: "Cache hit rate is {{ $value }}%"

      # Goroutine 泄漏
      - alert: GoroutineLeaking
        expr: rate(goroutine_count[5m]) > 10
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Goroutine leak detected"
          description: "Goroutines increasing at {{ $value }}/s"

      # 内存使用过高
      - alert: HighMemoryUsage
        expr: process_resident_memory_bytes > 4e9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High memory usage"
          description: "Memory usage is {{ $value | humanize }}B"
```

---

## 性能测试建议

### 1. 压力测试场景

**场景 1: 高并发 Agent 调用**

```go
func StressTestHighConcurrency(t *testing.T) {
    pool, _ := NewAgentPool(mockAgentFactory, PoolConfig{
        InitialSize: 10,
        MaxSize:     100,
    })
    defer pool.Close()

    concurrency := []int{10, 50, 100, 500, 1000}
    duration := 60 * time.Second

    for _, c := range concurrency {
        t.Run(fmt.Sprintf("Concurrency_%d", c), func(t *testing.T) {
            var wg sync.WaitGroup
            errors := make(chan error, c)

            ctx, cancel := context.WithTimeout(context.Background(), duration)
            defer cancel()

            for i := 0; i < c; i++ {
                wg.Add(1)
                go func() {
                    defer wg.Done()

                    for {
                        select {
                        case <-ctx.Done():
                            return
                        default:
                            input := &core.AgentInput{Task: "test"}
                            _, err := pool.Execute(context.Background(), input)
                            if err != nil {
                                select {
                                case errors <- err:
                                default:
                                }
                            }
                        }
                    }
                }()
            }

            wg.Wait()
            close(errors)

            // 检查错误
            errorCount := 0
            for err := range errors {
                t.Logf("Error: %v", err)
                errorCount++
            }

            // 报告统计
            stats := pool.Stats()
            t.Logf("Concurrency: %d", c)
            t.Logf("Acquired: %d", stats.AcquiredTotal)
            t.Logf("Utilization: %.2f%%", stats.UtilizationPct)
            t.Logf("Errors: %d", errorCount)
        })
    }
}
```

**场景 2: 长时间运行测试** (检测内存泄漏)

```go
func StressTestLongRunning(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping long running test")
    }

    pool, _ := NewAgentPool(mockAgentFactory, DefaultPoolConfig())
    defer pool.Close()

    duration := 1 * time.Hour
    checkInterval := 5 * time.Minute

    ctx, cancel := context.WithTimeout(context.Background(), duration)
    defer cancel()

    var wg sync.WaitGroup
    wg.Add(1)

    go func() {
        defer wg.Done()

        for {
            select {
            case <-ctx.Done():
                return
            default:
                input := &core.AgentInput{Task: "test"}
                _, _ = pool.Execute(context.Background(), input)
                time.Sleep(10 * time.Millisecond)
            }
        }
    }()

    // 定期检查内存和 goroutine
    ticker := time.NewTicker(checkInterval)
    defer ticker.Stop()

    var memStats runtime.MemStats
    initialGoroutines := runtime.NumGoroutine()

    for {
        select {
        case <-ticker.C:
            runtime.ReadMemStats(&memStats)
            currentGoroutines := runtime.NumGoroutine()

            t.Logf("Memory: %d MB, Goroutines: %d (delta: %d)",
                memStats.HeapAlloc/1024/1024,
                currentGoroutines,
                currentGoroutines-initialGoroutines)

            // 检查泄漏
            if currentGoroutines > initialGoroutines+10 {
                t.Errorf("Goroutine leak detected: %d -> %d",
                    initialGoroutines, currentGoroutines)
            }

        case <-ctx.Done():
            wg.Wait()
            return
        }
    }
}
```

**场景 3: 缓存效率测试**

```go
func StressTestCacheEfficiency(t *testing.T) {
    agent := NewMockAgent("test", 5*time.Millisecond)
    cachedAgent := NewCachedAgent(agent, DefaultCacheConfig())
    defer cachedAgent.Close()

    // 不同命中率场景
    scenarios := []struct {
        name        string
        uniqueTasks int
        totalTasks  int
        expectedHitRate float64
    }{
        {"HighHitRate", 10, 1000, 99.0},
        {"MediumHitRate", 100, 1000, 90.0},
        {"LowHitRate", 500, 1000, 50.0},
    }

    for _, s := range scenarios {
        t.Run(s.name, func(t *testing.T) {
            cachedAgent.InvalidateAll()

            for i := 0; i < s.totalTasks; i++ {
                taskID := i % s.uniqueTasks
                input := &core.AgentInput{
                    Task: fmt.Sprintf("Task #%d", taskID),
                }
                _, err := cachedAgent.Invoke(context.Background(), input)
                assert.NoError(t, err)
            }

            stats := cachedAgent.Stats()
            t.Logf("Hit Rate: %.2f%% (expected: %.2f%%)",
                stats.HitRate, s.expectedHitRate)

            assert.Greater(t, stats.HitRate, s.expectedHitRate,
                "Cache hit rate lower than expected")
        })
    }
}
```

---

### 2. 性能回归测试

**CI/CD 集成** (已在前面详细说明):
- 自动运行基准测试
- 使用 benchstat 比较
- 性能退化超过阈值时失败

**本地测试脚本**:
```bash
#!/bin/bash
# scripts/perf_test.sh

set -e

echo "Running performance regression tests..."

# 1. 基准测试
echo "Step 1: Running benchmarks..."
go test -bench=. -benchmem -count=5 -timeout=30m ./performance/... | tee new_bench.txt

# 2. CPU profiling
echo "Step 2: CPU profiling..."
go test -bench=BenchmarkConcurrentPoolAccess -cpuprofile=cpu.out ./performance/
go tool pprof -text cpu.out | head -20

# 3. Memory profiling
echo "Step 3: Memory profiling..."
go test -bench=BenchmarkCachedVsUncached -memprofile=mem.out ./performance/
go tool pprof -text -alloc_space mem.out | head -20

# 4. Race detection
echo "Step 4: Race detection..."
go test -race -short ./...

# 5. 如果有旧的基准测试结果，进行对比
if [ -f old_bench.txt ]; then
    echo "Step 5: Comparing with baseline..."
    benchstat old_bench.txt new_bench.txt
fi

echo "Performance tests completed!"
```

---

## 容量规划建议

### 1. 当前容量评估

**AgentPool 默认配置分析**:
```go
func DefaultPoolConfig() PoolConfig {
    return PoolConfig{
        InitialSize:     5,
        MaxSize:         50,
        IdleTimeout:     5 * time.Minute,
        MaxLifetime:     30 * time.Minute,
        AcquireTimeout:  10 * time.Second,
        CleanupInterval: 1 * time.Minute,
    }
}
```

**容量计算**:
- **最大并发**: 50 个 Agent
- **单个 Agent 平均响应时间**: 假设 100ms
- **理论最大 QPS**: 50 / 0.1 = **500 QPS**
- **考虑池获取开销**: 实际约 **400 QPS**

**适用场景**:
- 小型到中型应用
- QPS < 400
- 并发用户 < 100

---

### 2. 不同规模的推荐配置

**Small (QPS < 100)**:
```go
func SmallPoolConfig() PoolConfig {
    return PoolConfig{
        InitialSize:     3,
        MaxSize:         20,
        IdleTimeout:     3 * time.Minute,
        MaxLifetime:     15 * time.Minute,
        AcquireTimeout:  5 * time.Second,
        CleanupInterval: 2 * time.Minute,
    }
}
```

**Medium (QPS 100-500)**:
```go
func MediumPoolConfig() PoolConfig {
    return PoolConfig{
        InitialSize:     10,
        MaxSize:         100,
        IdleTimeout:     5 * time.Minute,
        MaxLifetime:     30 * time.Minute,
        AcquireTimeout:  10 * time.Second,
        CleanupInterval: 1 * time.Minute,
    }
}
```

**Large (QPS 500-2000)**:
```go
func LargePoolConfig() PoolConfig {
    return PoolConfig{
        InitialSize:     50,
        MaxSize:         500,
        IdleTimeout:     10 * time.Minute,
        MaxLifetime:     60 * time.Minute,
        AcquireTimeout:  30 * time.Second,
        CleanupInterval: 30 * time.Second,
    }
}
```

**X-Large (QPS > 2000)**:
```go
// 推荐使用分布式架构
func XLargePoolConfig() PoolConfig {
    return PoolConfig{
        InitialSize:     100,
        MaxSize:         1000,
        IdleTimeout:     15 * time.Minute,
        MaxLifetime:     120 * time.Minute,
        AcquireTimeout:  60 * time.Second,
        CleanupInterval: 20 * time.Second,
    }
}
```

---

### 3. 扩展性建议

**当前架构**: 单机对象池

**扩展路径**:

#### Level 1: 垂直扩展 (单机优化)
- ✅ 当前阶段
- 优化锁竞争
- 增加池大小
- 适用于 QPS < 2000

#### Level 2: 水平扩展 (多实例)
- 部署多个 goagent 实例
- 使用负载均衡器 (如 Nginx, HAProxy)
- 共享缓存层 (Redis)
- 适用于 QPS 2000-10000

**架构**:
```
┌─────────────┐
│ Load Balancer│
└──────┬──────┘
       │
   ┌───┴───┬───────┬───────┐
   │       │       │       │
┌──▼──┐ ┌──▼──┐ ┌──▼──┐ ┌──▼──┐
│Inst1│ │Inst2│ │Inst3│ │Inst4│
└──┬──┘ └──┬──┘ └──┬──┘ └──┬──┘
   │       │       │       │
   └───┬───┴───┬───┴───┬───┘
       │       │       │
   ┌───▼───────▼───────▼───┐
   │   Shared Redis Cache   │
   └────────────────────────┘
```

#### Level 3: 分布式架构
- 分布式 Agent 池
- 跨机器的任务调度
- 分布式缓存
- 适用于 QPS > 10000

**需要实现的功能**:
1. 分布式任务队列 (如 RabbitMQ, Kafka)
2. 分布式锁 (如 Redis, etcd)
3. 服务发现 (如 Consul, etcd)
4. 健康检查和自动故障转移

---

## 代码质量评估

### 性能相关的代码质量

**优点**:
- ✅ 清晰的性能优化注释 (如 "Memory optimization", "Performance: fast path")
- ✅ 使用了现代 Go 特性 (Go 1.21+ clear())
- ✅ 良好的错误处理
- ✅ 合理的并发控制 (RWMutex, atomic)
- ✅ 遵循 Go 编码规范
- ✅ 良好的测试覆盖

**改进空间**:
- ⚠️ 部分性能关键代码缺少 benchmark (如 Context 访问)
- ⚠️ 缺少性能优化的设计文档
- ⚠️ 内联提示 (`//go:inline`) 使用较少
- ⚠️ 部分热路径缺少性能注释

**代码审查清单**:
- [x] 使用 sync.Pool 减少内存分配
- [x] 使用 sync.RWMutex 优化读多写少场景
- [x] 预分配切片容量
- [x] 使用 clear() 清理 map (Go 1.21+)
- [ ] 使用 atomic 操作代替锁 (部分场景)
- [ ] 使用 sync.Map 优化并发 map 访问 (考虑中)
- [ ] 减少不必要的堆内存分配
- [x] 避免在热路径使用反射

---

## 附录: 性能优化最佳实践

### 1. Go 性能优化清单

**内存优化**:
- [x] 使用 sync.Pool 减少内存分配
- [x] 预分配切片容量
- [x] 使用 clear() 清理 map (Go 1.21+)
- [x] 避免不必要的字符串拼接
- [ ] 使用 []byte 代替 string (部分场景)
- [ ] 避免 interface{} 装箱 (可优化)

**并发优化**:
- [x] 使用 sync.RWMutex 优化读多写少场景
- [x] 使用 atomic 操作减少锁
- [ ] 使用 sync.Map 优化高并发 map 访问
- [ ] 使用 channel 代替锁 (部分场景)
- [x] 使用 sync.Once 保证初始化只执行一次

**算法优化**:
- [ ] 使用更快的哈希算法 (xxhash 代替 SHA256)
- [x] 减少重复计算
- [x] 使用缓存减少计算
- [ ] 使用索引加速查找

**I/O 优化**:
- [ ] 使用缓冲 I/O (bufio)
- [ ] 批量读写减少系统调用
- [ ] 使用连接池复用连接
- [ ] 异步 I/O (goroutine)

---

### 2. 性能分析工具

**推荐工具链**:

1. **go test -bench** - 基准测试
   ```bash
   go test -bench=. -benchmem -cpuprofile=cpu.out
   ```

2. **pprof** - CPU/内存分析
   ```bash
   go tool pprof -http=:8080 cpu.out
   ```

3. **trace** - 执行追踪
   ```bash
   go test -trace=trace.out
   go tool trace trace.out
   ```

4. **benchstat** - 基准测试对比
   ```bash
   benchstat old.txt new.txt
   ```

5. **go-torch** - 火焰图
   ```bash
   go-torch -u http://localhost:6060
   ```

6. **vegeta** - HTTP 压测
   ```bash
   echo "GET http://localhost:8080/agent" | vegeta attack -duration=30s -rate=1000 | vegeta report
   ```

---

## 总结

### 关键发现

**优势** (保持):
1. 良好的对象池设计和使用
2. 合理的并发控制
3. 清晰的代码结构
4. 全面的基准测试

**问题** (需要解决):
1. **P1**: AgentPool 锁竞争 (高并发瓶颈)
2. **P1**: 缓存键生成开销大 (热路径性能)
3. **P1**: 工具缓存键生成性能问题
4. **P2**: Goroutine 泄漏风险
5. **P2**: AgentOutput 缺少对象池

### 优化路线图

**短期 (1-2 周)**:
1. 优化缓存键生成 (P1-2, P1-3)
2. 添加 AgentOutput 对象池 (P2-6)

**中期 (1 个月)**:
3. 优化 AgentPool 锁竞争 (P1-1)
4. 修复 goroutine 泄漏风险 (P2-4)
5. 改进 cleanup 策略 (P2-7)

**长期 (持续)**:
6. 建立性能监控体系
7. 实施性能回归测试
8. 容量规划和扩展

### 预期收益

完成所有 P1 和 P2 级别的优化后，预计：
- **高并发场景吞吐量**: +50-100%
- **内存使用**: -30-50%
- **P99 延迟**: -40-60%
- **缓存查询性能**: +60-90%
- **GC 压力**: -40-60%

---

**审查完成日期**: 2025-12-01
**下次审查建议**: 2025-03-01 (完成 P1 优化后)
**审查者签名**: Performance Engineer (Claude Code)
