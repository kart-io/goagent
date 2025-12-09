# Input Serialization 输入序列化示例

本示例演示如何使用 `AgentBuilder` 的 `WithInputSerializer` 功能，在将用户输入发送给 LLM 之前对其进行预处理（如脱敏）。

## 目录

- [简介](#简介)
- [核心特性](#核心特性)
- [使用方法](#使用方法)
- [代码结构](#代码结构)

## 简介

在处理敏感数据（如个人身份信息 PII）时，直接将原始数据发送给 LLM 可能会导致隐私泄露。GoAgent 提供了 `InputSerializer` 接口，允许开发者自定义输入数据的序列化逻辑，从而在数据离开应用程序边界之前进行脱敏或格式化。

## 核心特性

### 1. InputSerializer 接口

```go
type InputSerializer interface {
    Serialize(input interface{}) (string, error)
}
```

### 2. 自定义脱敏器

示例中实现了一个简单的 `MaskingSerializer`，用于检测并掩盖电子邮件地址。

```go
type MaskingSerializer struct{}

func (s *MaskingSerializer) Serialize(input interface{}) (string, error) {
    str := fmt.Sprintf("%v", input)
    if strings.Contains(str, "@") {
        return "MASKED_EMAIL", nil
    }
    return str, nil
}
```

### 3. System Prompt 配合

为了让 LLM 理解脱敏后的数据，我们通过 `WithSystemPrompt` 告知 LLM 如何处理特殊标记（如 `MASKED_EMAIL`）。

## 使用方法

### 前置要求

- 设置 `DEEPSEEK_API_KEY` 环境变量。

### 运行示例

```bash
export DEEPSEEK_API_KEY=your_api_key
cd examples/builder/serialization
go run main.go
```

### 预期输出

```text
--- Default Serializer ---
Result: ... (LLM 可能会直接引用邮箱地址) ...

--- Custom Masking Serializer ---
Result: 脱敏数据是指对敏感信息进行处理... (LLM 解释脱敏数据)
```

## 代码结构

```text
serialization/
├── main.go          # 示例入口
└── README.md        # 本文档
```
