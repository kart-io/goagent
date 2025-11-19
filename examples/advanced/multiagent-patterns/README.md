# 多智能体协作模式示例

本示例基于文章 [AI Agents & Multi-Agent Architectures (Part 7)](https://medium.com/@vipra_singh/ai-agents-multi-agent-architectures-part-7-0f0e185bb083) 实现了三种核心的多智能体协作模式。

## 包含的模式

### 1. Aggregator（聚合模式）

**场景**: 社交媒体情感分析

**描述**: 多个Agent并行分析不同社交媒体平台（Twitter、Instagram、Reddit）的内容，聚合者综合所有结果生成加权情感报告。

**特点**:

- 并行处理提高效率
- 综合多个数据源
- 加权聚合算法

**文件**: `aggregator_sentiment.go`

### 2. Router（路由模式）

**场景**: 智能客服工单分配

**描述**: 中央路由器根据工单类型（账单、技术、一般咨询、紧急升级）自动分配给对应的专业Agent处理。

**特点**:

- 智能分类路由
- 专家分工协作
- 提高处理效率

**文件**: `router_support.go`

### 3. Loop（循环模式）

**场景**: 代码编写与测试迭代

**描述**: 编写者Agent和测试者Agent循环协作，基于测试反馈不断改进代码，直到通过所有测试用例。

**特点**:

- 迭代优化
- 自动反馈循环
- 质量持续改进

**文件**: `loop_code_iteration.go`

## 快速开始

### 前置条件

1. Go 1.25.0+
2. LLM API密钥（DeepSeek 或 OpenAI）

### 设置API密钥

```bash
# DeepSeek
export DEEPSEEK_API_KEY="your-api-key"

# 或 OpenAI
export OPENAI_API_KEY="your-api-key"
```

### 运行示例

```bash
# 进入示例目录
cd examples/advanced/multiagent-patterns

# 运行单个模式
go run . -pattern=aggregator -provider=deepseek
go run . -pattern=router -provider=deepseek
go run . -pattern=loop -provider=deepseek

# 运行所有模式
go run . -pattern=all -provider=deepseek

# 使用 OpenAI
go run . -pattern=all -provider=openai
```

### 使用 Makefile

```bash
# 运行所有模式
make run

# 运行特定模式
make run-aggregator
make run-router
make run-loop
```

## 命令行参数

- `-pattern`: 要运行的模式
  - `aggregator` - 聚合模式
  - `router` - 路由模式
  - `loop` - 循环模式
  - `all` - 运行所有模式（默认）
- `-provider`: LLM提供者
  - `deepseek` - DeepSeek（默认）
  - `openai` - OpenAI

## 示例输出

### Aggregator模式输出示例

```
🔄 模式 1: Aggregator（聚合模式）- 社交媒体情感分析
说明：多个Agent并行分析不同平台的内容，聚合者综合所有结果生成最终报告
================================================================================

📊 任务: 分析多个社交媒体平台关于某产品的情感倾向
🎯 输入: 分析关于'新款智能手机XYZ'的社交媒体情感

✅ 分析完成！

📈 各平台分析结果:
--------------------------------------------------------------------------------

Twitter:
  情感极性: 0.60 (-1=负面, 0=中性, 1=正面)
  主观性得分: 0.70
  帖子数量: 1500
  处理时间: 0.32秒

Instagram:
  情感极性: 0.80
  主观性得分: 0.60
  帖子数量: 800
  处理时间: 0.28秒

Reddit:
  情感极性: 0.30
  主观性得分: 0.80
  帖子数量: 600
  处理时间: 0.25秒

📊 聚合报告:
--------------------------------------------------------------------------------
加权情感极性: 0.59
总帖子数: 2900
总处理时间: 0.85秒

摘要: 整体情感偏向正面。跨3个平台分析了2900条帖子，加权情感极性为0.59。
```

### Router模式输出示例

```
🔀 模式 2: Router（路由模式）- 智能客服工单分配
说明：中央路由器根据工单类型自动分配给专业Agent处理
================================================================================

📋 接收到 4 个工单，开始智能分配...

处理工单 TICKET-001: 账单金额异常
  ├─ 分类: billing
  ├─ 分配给: billing
  └─ ✅ 处理完成 (用时: 0.21秒)

处理工单 TICKET-002: 无法登录账户
  ├─ 分类: technical
  ├─ 分配给: technical
  └─ ✅ 处理完成 (用时: 0.22秒)
```

### Loop模式输出示例

```
🔄 模式 3: Loop（循环模式）- 代码编写与测试迭代
说明：编写者Agent和测试者Agent循环协作，直到代码通过所有测试
================================================================================

📝 任务要求:
编写一个 Go 函数 factorial，计算阶乘。
要求：
1. 函数签名：func factorial(n int) int
2. 处理负数输入（返回 -1）
3. 处理 0 和 1（返回 1）
4. 正确计算正整数的阶乘
5. 处理大数情况（n > 20 时返回 -1 避免溢出）

开始迭代开发...

--- 第 1 次迭代 ---
📝 编写者Agent正在编写代码...
✅ 代码已生成

🧪 测试者Agent正在测试代码...
❌ 测试失败 (3/5 测试用例通过)
   失败原因: 缺少负数输入处理
   失败的测试: [负数测试 大数测试]

--- 第 2 次迭代 ---
📝 编写者Agent正在编写代码...
✅ 代码已生成

🧪 测试者Agent正在测试代码...
❌ 测试失败 (4/5 测试用例通过)
   失败原因: 缺少大数溢出处理
   失败的测试: [大数测试]

--- 第 3 次迭代 ---
📝 编写者Agent正在编写代码...
✅ 代码已生成

🧪 测试者Agent正在测试代码...
✅ 测试通过! (5/5 测试用例通过)

🎉 代码开发完成！
```

## 架构设计

### 项目结构

```
multiagent-patterns/
├── main.go                    # 入口程序
├── aggregator_sentiment.go   # 聚合模式实现
├── router_support.go          # 路由模式实现
├── loop_code_iteration.go    # 循环模式实现
├── README.md                  # 本文档
└── Makefile                   # 构建脚本
```

### 核心组件

1. **MultiAgentSystem**: 多智能体管理系统（来自 `multiagent` 包）
2. **CollaborativeAgent**: 协作Agent接口
3. **CollaborativeTask**: 协作任务定义
4. **各模式特定的Agent实现**

## 扩展建议

### 添加新模式

文章中介绍的其他模式可以继续实现：

- **Sequential（顺序模式）**: 多级审批流程
- **Hierarchical（分层模式）**: 树形组织结构
- **Network（网络模式）**: 去中心化协作
- **Supervisor（监督者模式）**: 中央监督者管理

### 优化建议

1. **集成真实LLM**: 当前某些Agent使用模拟数据，可以集成真实的LLM调用
2. **添加持久化**: 保存任务执行历史和结果
3. **可视化**: 添加Agent协作过程的可视化
4. **性能监控**: 添加详细的性能指标和追踪

## 相关文档

- 项目主文档: [DOCUMENTATION_INDEX.md](../../../DOCUMENTATION_INDEX.md)
- 架构文档: [docs/architecture/ARCHITECTURE.md](../../../docs/architecture/ARCHITECTURE.md)
- 多智能体系统: [multiagent/](../../../multiagent/)
- 其他示例: [examples/advanced/](../)

## 参考资料

- 原文章: [AI Agents & Multi-Agent Architectures (Part 7)](https://medium.com/@vipra_singh/ai-agents-multi-agent-architectures-part-7-0f0e185bb083)
- LangChain 文档: <https://python.langchain.com/docs/>
- GoAgent 项目: <https://github.com/kart-io/goagent>

## 许可证

本示例代码遵循项目主许可证。
