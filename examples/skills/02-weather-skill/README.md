# 天气技能示例 (Weather Skill)

用分层结构（Handler → Logic → ServiceContext）实现一个配置驱动的天气技能，并把天气查询封装成 goagent 工具，让模型通过工具调用回答自然语言问题。

技能接口、注册表、执行器都定义在本示例的 `internal/skill` 中，并非 goagent 提供的包；天气数据由 `internal/logic` 随机生成，不访问真实天气服务。

## 目录结构

```text
02-weather-skill/
├── etc/config.yaml              # 配置文件
├── internal/
│   ├── config/config.go         # 配置结构
│   ├── handler/handler.go       # 请求解析与响应格式化，业务委托给 logic
│   ├── llmtools/tools.go        # 用 tools.NewFunctionTool 定义的三个工具
│   ├── logic/logic.go           # 城市校验与模拟天气生成
│   ├── skill/weather.go         # Skill 接口、WeatherSkill、SkillRegistry、SkillExecutor
│   ├── skill/llm_weather.go     # LLMWeatherSkill：交给模型选择工具
│   ├── svc/servicecontext.go    # 配置、缓存与 LLM 客户端
│   └── types/types.go           # 请求/响应类型
└── main.go
```

## 运行

```bash
cd examples/skills/02-weather-skill
go run .
```

配置从当前目录下的 `etc/config.yaml` 读取，文件不存在时（例如从仓库根目录 `go run ./examples/skills/02-weather-skill`）使用 `main.go` 中内容相同的默认配置。

LLM 客户端由配置中的 `LLM.Provider` 决定：`deepseek`（默认）读取 `DEEPSEEK_API_KEY`，`openai` 读取 `OPENAI_API_KEY`。对应的 key 未设置时，场景 6 改为直接调用三个工具（Mock 模式），整个示例无需网络即可运行。

## 演示场景

1. **创建技能**：`skill.NewWeatherSkill(cfg)`，输出名称、描述、版本、关键词。
2. **注册**：`skill.NewSkillRegistry()` 注册并列出技能。
3. **路由**：`registry.Route(&skill.RoutingContext{...})` 按关键词匹配，返回技能和分数。
4. **直接执行**：`GetWeather`（北京）、`GetForecast`（上海，5 天）、`Execute` 调用 `list_cities`，以及不支持城市“火星”的错误。
5. **执行器**：`NewSkillExecutor(registry)` 的 `ExecuteByName`、`ExecuteByRouting`，以及技能不存在的错误。
6. **LLM 工具调用**：`skill.NewLLMWeatherSkill(cfg).AskNatural(ctx, query)`，依次询问北京今天天气、上海未来三天预报、支持的城市。

## 支持的 Action / 工具

| 名称 | 参数 | 说明 |
| --- | --- | --- |
| `get_weather` | `city` | 当前天气 |
| `get_forecast` | `city`、`days`（1-7） | 天气预报 |
| `list_cities` | 无 | 支持的城市列表 |

`LLMWeatherSkill` 额外接受 `natural_query`（参数 `query`），`AskNatural` 即用它调用。

## LLM 工具调用流程

`LLMWeatherSkill.processWithLLM`：

1. `llm.AsToolCaller(client)` 获取工具调用能力，取不到时退回 `Chat`；
2. `GenerateWithTools(ctx, prompt, tools)` 发送带工具定义的请求，失败时同样退回 `Chat`；
3. 模型返回 `ToolCalls` 时，按名称找到工具并 `Invoke`，结果作为技能输出；否则直接返回模型文本。

## 配置

`etc/config.yaml` 包含服务名、日志、技能（名称、描述、版本、默认城市、支持城市、缓存开关与 TTL）、路由关键词和 LLM（`Provider`、`Model`、`MaxTokens`、`Temperature`）。默认支持北京、上海、广州、深圳、杭州、成都、武汉、西安。

与 [01-demo](../01-demo/) 相比：01-demo 在单个文件中演示多技能的注册、路由、并行与聚合；本示例聚焦单个技能的分层实现与模型工具调用。
