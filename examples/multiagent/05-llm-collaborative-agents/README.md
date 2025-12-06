# 05-llm-collaborative-agents LLM 协作 Agent 示例

本示例演示如何创建具有 LLM 推理能力的协作 Agent，展示多专家代码审查、协作研究分析和流水线处理等场景。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [应用场景](#应用场景)

## 架构设计

### LLM 协作架构

```mermaid
graph TB
    subgraph LLMProviders["LLM 提供商"]
        DeepSeek["DeepSeek API"]
        OpenAI["OpenAI API"]
        Ollama["Ollama 本地"]
    end

    subgraph LLMClient["LLM 客户端"]
        Client["llm.Client 接口"]
    end

    subgraph Agents["LLM 协作 Agent"]
        Security["安全审查专家"]
        Performance["性能优化专家"]
        Quality["代码质量专家"]
        Tech["技术研究员"]
        Market["市场研究员"]
    end

    subgraph MAS["MultiAgentSystem"]
        Registry["Agent 注册表"]
        TaskExec["任务执行器"]
    end

    DeepSeek --> Client
    OpenAI --> Client
    Ollama --> Client

    Client --> Security
    Client --> Performance
    Client --> Quality
    Client --> Tech
    Client --> Market

    Agents --> Registry
    TaskExec --> Agents
```

### LLMCollaborativeAgent 结构

```mermaid
classDiagram
    class LLMCollaborativeAgent {
        -base *BaseCollaborativeAgent
        -llmClient Client
        -systemPrompt string
        -expertise string
        +Collaborate(ctx, task) Assignment
    }

    class BaseCollaborativeAgent {
        -id string
        -description string
        -role Role
        +Name() string
        +GetRole() Role
    }

    class LLMClient {
        <<interface>>
        +Complete(ctx, req) Response
        +Chat(ctx, messages) Response
        +Provider() Provider
        +IsAvailable() bool
    }

    class CompletionRequest {
        +Messages list~Message~
        +MaxTokens int
        +Temperature float64
    }

    class CompletionResponse {
        +Content string
        +Model string
        +TokensUsed int
        +Provider string
    }

    LLMCollaborativeAgent *-- BaseCollaborativeAgent : 嵌入
    LLMCollaborativeAgent --> LLMClient : 使用
    LLMClient ..> CompletionRequest : 输入
    LLMClient ..> CompletionResponse : 输出
```

### LLM 提供商选择流程

```mermaid
flowchart TD
    Start["创建 LLM 客户端"] --> CheckDeepSeek{"DEEPSEEK_API_KEY<br/>环境变量存在?"}

    CheckDeepSeek --> |"是"| UseDeepSeek["使用 DeepSeek"]
    CheckDeepSeek --> |"否"| CheckOpenAI{"OPENAI_API_KEY<br/>环境变量存在?"}

    CheckOpenAI --> |"是"| UseOpenAI["使用 OpenAI"]
    CheckOpenAI --> |"否"| CheckOllama{"Ollama 本地<br/>服务可用?"}

    CheckOllama --> |"是"| UseOllama["使用 Ollama"]
    CheckOllama --> |"否"| UseMock["使用 Mock 客户端"]

    UseDeepSeek --> Ready["LLM 客户端就绪"]
    UseOpenAI --> Ready
    UseOllama --> Ready
    UseMock --> Ready

    style UseDeepSeek fill:#c8e6c9
    style UseOpenAI fill:#c8e6c9
    style UseOllama fill:#c8e6c9
    style UseMock fill:#fff9c4
```

## 核心组件

### 1. LLMCollaborativeAgent

将 LLM 能力与协作 Agent 框架结合的核心组件。

```mermaid
graph LR
    subgraph Input["输入"]
        Task["协作任务"]
        SystemPrompt["系统提示"]
        Expertise["专业领域"]
    end

    subgraph Process["处理"]
        Build["构建 Prompt"]
        Call["调用 LLM"]
        Parse["解析响应"]
    end

    subgraph Output["输出"]
        Assignment["任务分配结果"]
        Analysis["分析内容"]
        Tokens["Token 使用量"]
    end

    Task --> Build
    SystemPrompt --> Build
    Expertise --> Build
    Build --> Call
    Call --> Parse
    Parse --> Assignment
    Parse --> Analysis
    Parse --> Tokens
```

### 2. 三种协作场景

```mermaid
graph TB
    subgraph Scenario1["场景1: 多专家代码审查"]
        direction TB
        Code["待审查代码"]
        S1["安全专家"]
        P1["性能专家"]
        Q1["质量专家"]
        Review["综合审查报告"]

        Code --> S1 --> Review
        Code --> P1 --> Review
        Code --> Q1 --> Review
    end

    subgraph Scenario2["场景2: 协作研究分析"]
        direction TB
        Topic["研究主题"]
        Tech["技术研究员"]
        Market["市场研究员"]
        Summary["研究报告"]

        Topic --> Tech --> Summary
        Topic --> Market --> Summary
    end

    subgraph Scenario3["场景3: 流水线处理"]
        direction TB
        Task3["写作任务"]
        Outline["大纲生成"]
        Write["内容撰写"]
        Edit["编辑审校"]

        Task3 --> Outline --> Write --> Edit
    end
```

## 执行流程

### 场景1: 多专家代码审查

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant SE as 安全专家
    participant PE as 性能专家
    participant QE as 质量专家
    participant LLM as LLM API

    Client->>MAS: 提交代码审查任务

    par 并行审查
        MAS->>SE: Collaborate(task)
        SE->>LLM: Complete(安全分析提示)
        LLM-->>SE: 安全分析结果
        SE-->>MAS: 安全审查报告
    and
        MAS->>PE: Collaborate(task)
        PE->>LLM: Complete(性能分析提示)
        LLM-->>PE: 性能分析结果
        PE-->>MAS: 性能审查报告
    and
        MAS->>QE: Collaborate(task)
        QE->>LLM: Complete(质量分析提示)
        LLM-->>QE: 质量分析结果
        QE-->>MAS: 质量审查报告
    end

    MAS->>MAS: 汇总审查结果
    MAS-->>Client: 综合代码审查报告
```

### 场景2: 协作研究分析

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant TR as 技术研究员
    participant MR as 市场研究员
    participant LLM as LLM API

    Client->>MAS: 提交研究任务
    Note over Client,MAS: 主题: LLM 企业应用最佳实践

    par 并行研究
        MAS->>TR: Collaborate(task)
        TR->>LLM: Complete(技术分析提示)
        LLM-->>TR: 技术分析结果
        Note over TR: 技术实现、架构设计<br/>部署方案、成本评估
        TR-->>MAS: 技术研究报告
    and
        MAS->>MR: Collaborate(task)
        MR->>LLM: Complete(市场分析提示)
        LLM-->>MR: 市场分析结果
        Note over MR: 市场趋势、竞争格局<br/>商业价值、风险因素
        MR-->>MAS: 市场研究报告
    end

    MAS->>MAS: 综合研究结果
    MAS-->>Client: 完整研究报告
```

### 场景3: 流水线处理

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant OL as 大纲生成器
    participant WR as 内容撰写者
    participant ED as 编辑审校者
    participant LLM as LLM API

    Client->>MAS: 提交写作任务
    Note over Client,MAS: Go 并发编程最佳实践

    rect rgb(230, 245, 255)
        Note over OL: 阶段1: 大纲生成
        MAS->>OL: Collaborate(task)
        OL->>LLM: Complete(大纲生成提示)
        LLM-->>OL: 文章大纲
        OL-->>MAS: 大纲结果
    end

    rect rgb(255, 245, 230)
        Note over WR: 阶段2: 内容撰写
        MAS->>WR: Collaborate(task + 大纲)
        WR->>LLM: Complete(撰写提示 + 大纲)
        LLM-->>WR: 文章初稿
        WR-->>MAS: 初稿结果
    end

    rect rgb(230, 255, 230)
        Note over ED: 阶段3: 编辑审校
        MAS->>ED: Collaborate(task + 初稿)
        ED->>LLM: Complete(审校提示 + 初稿)
        LLM-->>ED: 优化后文章
        ED-->>MAS: 最终稿件
    end

    MAS-->>Client: 完成的文章
```

### LLM 调用流程

```mermaid
flowchart TD
    Start["开始协作任务"] --> Build["构建 Prompt"]

    Build --> |"系统提示"| SysPrompt["你是一位资深的代码安全专家..."]
    Build --> |"用户提示"| UserPrompt["任务: 审查以下代码<br/>专业领域: 安全分析"]

    SysPrompt --> Request["构建 CompletionRequest"]
    UserPrompt --> Request

    Request --> Call["调用 llmClient.Complete()"]
    Call --> Response["获取 CompletionResponse"]

    Response --> Extract["提取分析结果"]
    Extract --> Result["构建 Assignment"]

    Result --> Return["返回协作结果"]

    subgraph Assignment["Assignment 结构"]
        AgentID["agent_id: security-reviewer"]
        Expertise["expertise: 安全分析"]
        Analysis["analysis: LLM 分析内容"]
        Model["model: deepseek-chat"]
        Tokens["tokens_used: 150"]
    end
```

## 使用方法

### 环境配置

```bash
# 使用 DeepSeek (推荐)
export DEEPSEEK_API_KEY="your-api-key"

# 或使用 OpenAI
export OPENAI_API_KEY="your-api-key"

# 或使用本地 Ollama
ollama run qwen2:7b
```

### 运行示例

```bash
cd examples/multiagent/05-llm-collaborative-agents
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          LLM 多智能体协作示例                                  ║
║   展示如何创建具有 LLM 推理能力的协作 Agent                    ║
╚════════════════════════════════════════════════════════════════╝

【场景 1】多专家代码审查
════════════════════════════════════════════════════════════════

场景描述: 多位专家从不同角度审查代码

✓ 注册专家: security-reviewer
✓ 注册专家: performance-reviewer
✓ 注册专家: quality-reviewer

待审查代码:
────────────────────────────────────────
func ProcessUserData(db *sql.DB, userID string) ([]byte, error) {
    query := "SELECT * FROM users WHERE id = '" + userID + "'"
    ...
}
────────────────────────────────────────

执行多专家并行审查...
✓ 审查完成 (耗时: 35s)

审查结果:
════════════════════════════════════════

【security-reviewer】
## 安全分析报告
**严重问题:**
1. SQL 注入漏洞：直接拼接用户输入到 SQL 查询中
   - 风险等级: 高危
   - 建议: 使用参数化查询

【performance-reviewer】
## 性能分析报告
**性能问题:**
1. SELECT * 查询效率低
2. 未使用连接池配置
...

【quality-reviewer】
## 代码质量分析报告
**质量问题:**
1. 函数职责不单一
2. 缺少文档注释
...
```

### 关键代码

#### 创建 LLM 协作 Agent

```go
agent := NewLLMCollaborativeAgent(
    "security-reviewer",
    "安全审查专家",
    multiagent.RoleSpecialist,
    system,
    llmClient,
    "你是一位资深的代码安全专家，专注于识别安全漏洞和潜在风险。",
    "安全分析",
)
```

#### 实现 Collaborate 方法

```go
func (a *LLMCollaborativeAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
    prompt := fmt.Sprintf(`任务: %s
请根据你的专业领域(%s)分析并完成此任务。`, task.Input, a.expertise)

    response, err := a.llmClient.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "system", Content: a.systemPrompt},
            {Role: "user", Content: prompt},
        },
    })

    return &multiagent.Assignment{
        AgentID: a.Name(),
        Result: map[string]interface{}{
            "analysis":    response.Content,
            "tokens_used": response.TokensUsed,
        },
        Status: multiagent.TaskStatusCompleted,
    }, nil
}
```

## 应用场景

### LLM 协作应用矩阵

```mermaid
mindmap
    root((LLM 协作))
        代码审查
            安全漏洞检测
            性能瓶颈分析
            代码质量评估
            最佳实践建议
        研究分析
            技术调研
            市场分析
            竞品研究
            趋势预测
        内容创作
            大纲生成
            内容撰写
            编辑审校
            翻译润色
        决策支持
            方案评估
            风险分析
            成本估算
            优先级排序
```

### 协作模式对比

| 模式 | 说明 | 适用场景 | LLM 调用方式 |
|------|------|---------|-------------|
| **并行专家** | 多专家同时分析 | 代码审查、方案评估 | 并行调用 |
| **协作研究** | 多领域协同分析 | 技术调研、市场分析 | 并行调用 |
| **流水线** | 链式顺序处理 | 内容创作、数据处理 | 顺序调用 |

### LLM 提供商对比

| 提供商 | 优势 | 劣势 | 推荐场景 |
|--------|------|------|---------|
| **DeepSeek** | 中文能力强、性价比高 | 国际可用性 | 中文项目 |
| **OpenAI** | 能力全面、生态丰富 | 成本较高 | 复杂推理 |
| **Ollama** | 本地部署、隐私安全 | 需要 GPU | 敏感数据 |

## 扩展阅读

- [01-basic-system](../01-basic-system/) - 基础系统示例
- [02-collaboration-types](../02-collaboration-types/) - 协作类型示例
- [04-specialized-agents](../04-specialized-agents/) - 专业化 Agent 示例
- [llm 包文档](../../../llm/) - LLM 客户端使用指南
