# Phase 3 阶段 2 迁移计划

**创建时间**: 2025-11-28
**状态**: 准备执行

## 概述

Phase 3 阶段 1 已完成（删除废弃别名），现在执行阶段 2：迁移所有旧 API 调用到新 API。

## 当前状态

**旧 API 使用统计**（总计158处）:
- NewOpenAI(): 18 处
- NewAnthropic(): 20 处
- NewGemini(): 12 处
- NewDeepSeek(): 62 处
- NewKimi(): 1 处
- NewOllama(): 2 处
- NewSiliconFlow(): 1 处
- NewCohere(): 21 处
- NewHuggingFace(): 21 处

**分布**:
- 测试文件 (*_test.go): ~111 处
- 示例文件 (examples/): ~75 处

## 迁移策略

采用**分批渐进式迁移**（方案 B），降低风险。

### 批次 1: llm/providers/ 核心测试文件

**文件列表**:
- llm/providers/comprehensive_test.go
- llm/providers/extended_test.go
- llm/integration_test.go

**迁移模式**:
```go
// 旧 API
provider, err := NewOpenAI(&agentllm.LLMOptions{
    APIKey: "key",
    Model:  "gpt-4",
})

// 新 API
provider, err := NewOpenAIWithOptions(
    agentllm.WithAPIKey("key"),
    agentllm.WithModel("gpt-4"),
)
```

**工作量**: 30-45 分钟
**风险**: 低（有完整测试覆盖）

### 批次 2: examples/basic/ 示例

**文件列表**:
- examples/basic/05-provider-consistency/main.go
- examples/basic/06-all-providers/main.go
- 其他基础示例

**工作量**: 30-45 分钟
**风险**: 低（简单示例）

### 批次 3: examples/advanced/ 示例

**文件列表**:
- examples/advanced/supervisor_agent/main.go
- examples/advanced/multiagent-patterns/main.go
- examples/advanced/multi-agent-collaboration/main_simple.go
- examples/advanced/multi-agent-modular/main.go
- examples/advanced/multi-agent-simple/main.go
- 其他高级示例

**工作量**: 45-60 分钟
**风险**: 中（复杂示例）

### 批次 4: 删除旧构造函数

删除9个 provider 文件中的旧构造函数：
- NewOpenAI(config *LLMOptions)
- NewAnthropic(config *LLMOptions)
- NewGemini(config *LLMOptions)
- NewDeepSeek(config *LLMOptions)
- NewKimi(config *LLMOptions)
- NewOllama(config *LLMOptions)
- NewSiliconFlow(config *LLMOptions)
- NewCohere(config *LLMOptions)
- NewHuggingFace(config *LLMOptions)

**工作量**: 15 分钟
**风险**: 低（已验证无使用）

## 执行顺序

1. ✅ 批次 1: 核心测试文件
2. ✅ 验证测试通过
3. ✅ Git 提交
4. ✅ 批次 2: 基础示例
5. ✅ 验证示例编译
6. ✅ Git 提交
7. ✅ 批次 3: 高级示例
8. ✅ 验证示例编译
9. ✅ Git 提交
10. ✅ 批次 4: 删除旧构造函数
11. ✅ 完整项目验证
12. ✅ Git 提交

## 验收标准

- [ ] 所有测试通过（跳过2个已知失败）
- [ ] 所有示例编译成功
- [ ] 旧 API 使用计数为 0
- [ ] 旧构造函数已删除
- [ ] Git 提交清晰记录

## 预期收益

- 删除约 9 个废弃构造函数（~90 行代码）
- 强制使用新 Options 模式
- 代码库更一致
- 为未来完全清理铺路

---

🤖 Generated with Claude Code
📅 2025-11-28
🎯 Phase 3 阶段 2 规划
