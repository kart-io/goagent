# CoT vs ReAct 性能对比示例

## 概述

本示例演示了 Chain-of-Thought (CoT) 和 ReAct 两种推理模式在数学问题上的性能对比。

## 运行方式

```bash
export DEEPSEEK_API_KEY=your_api_key
go run main.go
```

## 关键修复

### 1. CoT 步骤解析优化 (已修复)

**问题**: 原始实现将 LLM 输出的每一行都当作独立步骤，导致：
- Markdown 格式行（`**Step 1:**`）被计为步骤
- LaTeX 公式的每一行（`\[`, `15`, `\]`）都被计为步骤
- 预期 5 步推理变成 27 步

**修复**: `agents/cot/cot.go:parseCoTResponse()`
- 按语义分组步骤，而不是按行分割
- 识别步骤标题（如 `**Step 1:**`）并将后续内容合并到同一步
- 过滤 LaTeX 分隔符和纯数字行
- 现在正确识别 5-8 个逻辑推理步骤

### 2. 性能对比计算修正 (已修复)

**问题**: 原始代码有两个计算错误：
- 速度提升计算：`reactLatency / cotLatency` 表示的是"ReAct 慢了多少倍"
- 步骤减少率：当 CoT 步骤 > ReAct 步骤时会产生负百分比

**修复**: `examples/optimization/cot_vs_react/main.go:77-102`
- 根据实际快慢关系显示正确的对比文本
- 处理步骤增加和步骤减少两种情况

### 3. ReAct 工具支持 (已添加)

**背景**: ReAct 模式设计为与工具交互的推理循环：
1. Thought: 分析当前情况
2. Action: 选择并调用工具
3. Observation: 观察工具结果
4. 重复直到得出结论

**实现**: 添加了 `SimpleCalculator` 工具支持基本算术运算
- 支持 add, subtract, multiply, divide 操作
- 输入格式: `"operation:number1,number2"`
- 例如: `"add:10,5"` 返回 `15`

## 实际行为说明

### CoT Agent
✅ **正常工作** - 按预期执行多步推理：
- 识别 5-8 个逻辑推理步骤
- 每步包含完整的说明和计算
- Token 消耗适中

### ReAct Agent
⚠️ **行为受 LLM 模型影响** - 在简单任务上可能直接输出答案：
- **简单任务**: LLM 认为不需要工具，直接返回 `Final Answer`（1 步）
- **复杂任务**: LLM 会进入 Thought-Action-Observation 循环（多步）

这是正常行为，原因：
1. **LLM 智能决策**: 现代 LLM 会评估任务复杂度，简单任务直接心算
2. **成本优化**: 避免不必要的工具调用和 Token 消耗
3. **ReAct 设计初衷**: ReAct 擅长需要外部工具的复杂任务，而非纯推理

### 对比结果解读

当前示例（简单数学问题）：
```
CoT 执行时间:    ~10s    CoT 推理步骤:    5-8
ReAct 执行时间:  ~7-9s   ReAct 推理步骤:  1
```

**结论**:
- **CoT 优势**: 适合纯推理任务，步骤可解释性强
- **ReAct 优势**: 在简单任务上更高效（直接输出），在复杂任务上展现工具调用能力

## 推荐使用场景

### 使用 CoT 当:
- 需要展示详细推理过程
- 纯逻辑/数学推理任务
- 需要可解释性和透明度
- 教学或演示目的

### 使用 ReAct 当:
- 需要调用外部工具（搜索、API、数据库）
- 任务需要多步交互和信息收集
- 需要根据中间结果动态调整策略
- 实际生产环境中的复杂问题解决

## 进一步优化

如需让 ReAct 在简单任务上也展示多步推理：
1. **增加任务复杂度**: 需要查询外部数据源的问题
2. **强制工具使用**: 在提示词中明确要求使用工具（已实现但 LLM 可能忽略）
3. **使用不同 LLM**: 某些模型更倾向于使用提供的工具

## 参考

- CoT 论文: [Chain-of-Thought Prompting Elicits Reasoning in Large Language Models](https://arxiv.org/abs/2201.11903)
- ReAct 论文: [ReAct: Synergizing Reasoning and Acting in Language Models](https://arxiv.org/abs/2210.03629)
