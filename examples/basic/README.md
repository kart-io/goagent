# Basic Examples

简单、专注的示例，用于学习 agent 框架。

每个示例演示一个单一的功能或概念。

## 示例列表

### 01-simple-agent
最简单的 Agent 创建示例

### 02-tools
工具系统的完整示例，包括各种内置工具和自定义工具

### 03-agent-with-memory
带有记忆功能的 Agent 示例

### 04-ollama-agent
使用 Ollama 本地大语言模型的 Agent 示例

### 05-provider-consistency
展示不同 LLM 提供商的一致性使用

### 06-all-providers
展示如何使用所有支持的 LLM 提供商

### 07-smart-agent-with-tools ✨ 新增
**智能 Agent 示例 - 时间获取与 API 调用**

展示如何创建具有以下功能的智能 Agent：
- 获取当前时间（支持不同时区和格式）
- 调用 HTTP API 接口（GET/POST 请求）
- 查询天气信息
- 集成多个工具到一个 Agent

**特性：**
- 🕐 时间工具：自定义时区、格式、详细时间信息
- 🌐 API 工具：HTTP 请求、自定义请求头、超时控制
- 🌤️ 天气工具：温度、湿度、风速查询
- 🤖 Agent 集成：展示如何将工具集成到 LLM Agent

**运行方式：**
```bash
cd 07-smart-agent-with-tools
go run main.go
```

查看各子目录以获取具体示例文档。
