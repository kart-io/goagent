# GoAgent 工具系统示例

本目录包含 GoAgent 框架内置工具和自定义工具的完整使用示例。

## 目录结构

```
examples/tools/
├── README.md                    # 本文件
├── 01-calculator/              # 计算器工具示例
│   └── main.go
├── 02-http-api/                # HTTP API 工具示例
│   └── main.go
├── 03-shell/                   # Shell 命令工具示例
│   └── main.go
├── 04-search/                  # 搜索工具示例
│   └── main.go
├── 05-file-operations/         # 文件操作工具示例
│   └── main.go
├── 06-web-scraper/             # 网页抓取工具示例
│   └── main.go
├── 07-function-tool/           # 自定义函数工具示例
│   └── main.go
├── 08-tool-with-agent/         # 工具与 Agent 集成示例
│   └── main.go
├── middleware/                 # 工具中间件示例（已有）
└── registry/                   # 工具注册表示例（已有）
```

## 示例说明

### 01-calculator - 计算器工具

演示 `CalculatorTool` 和 `AdvancedCalculatorTool` 的使用：

- 基础数学表达式求值（+、-、*、/、^、括号）
- 高级数学函数（sqrt、abs、sin、cos、tan、log、ln）
- 错误处理（除零、括号不匹配等）

```bash
cd examples/tools/01-calculator
go run main.go
```

### 02-http-api - HTTP API 工具

演示 `APITool` 的使用：

- GET/POST/PUT/DELETE/PATCH 请求
- Builder 模式创建工具
- 自定义请求头和超时
- 错误处理（404、超时）

```bash
cd examples/tools/02-http-api
go run main.go
```

### 03-shell - Shell 命令工具

演示 `ShellTool` 的安全命令执行：

- 命令白名单机制
- 执行系统命令（ls、pwd、echo 等）
- 指定工作目录
- 超时控制
- 预定义常用工具集

```bash
cd examples/tools/03-shell
go run main.go
```

### 04-search - 搜索工具

演示 `SearchTool` 的搜索功能：

- 模拟搜索引擎
- Google/DuckDuckGo 搜索引擎适配器
- 聚合搜索（多引擎合并去重排序）
- 搜索结果元数据

```bash
cd examples/tools/04-search
go run main.go
```

### 05-file-operations - 文件操作工具

演示 `FileOperationsTool` 的文件系统操作：

- 文件读写（read/write/append）
- 文件信息（info）
- 目录操作（list）
- 文件搜索（search）
- 文件复制/移动/删除
- 文件压缩（gzip/zip）
- JSON/YAML 解析

```bash
cd examples/tools/05-file-operations
go run main.go
```

### 06-web-scraper - 网页抓取工具

演示 `WebScraperTool` 的网页内容抓取：

- 抓取网页内容
- CSS 选择器数据提取
- 元数据提取
- 链接和图片提取
- 自定义选择器

```bash
cd examples/tools/06-web-scraper
go run main.go
```

### 07-function-tool - 自定义函数工具

演示如何创建自定义工具：

- `NewFunctionTool` - 快速创建简单工具
- `FunctionToolBuilder` - 链式构建复杂工具
- `BaseTool` - 完全控制输入输出
- 带状态的工具（计数器示例）
- 模拟外部 API（天气查询示例）

```bash
cd examples/tools/07-function-tool
go run main.go
```

### 08-tool-with-agent - 工具与 Agent 集成

演示工具如何与 Agent 集成：

- 创建多种工具
- 将工具注册到 Agent
- Agent 自动选择工具
- 工具执行结果整合

```bash
# 需要设置 OPENAI_API_KEY 环境变量
export OPENAI_API_KEY=your-api-key
cd examples/tools/08-tool-with-agent
go run main.go
```

## 内置工具一览

| 工具 | 包路径 | 说明 |
|------|--------|------|
| CalculatorTool | `tools/compute` | 基础数学计算 |
| AdvancedCalculatorTool | `tools/compute` | 高级数学函数 |
| APITool | `tools/http` | HTTP API 调用 |
| ShellTool | `tools/shell` | Shell 命令执行 |
| SearchTool | `tools/search` | 搜索功能 |
| FileOperationsTool | `tools/practical` | 文件系统操作 |
| WebScraperTool | `tools/practical` | 网页内容抓取 |
| DatabaseQueryTool | `tools/practical` | 数据库查询 |
| APICallerTool | `tools/practical` | API 调用器 |

## 创建自定义工具

### 方式 1：使用 FunctionTool

```go
import "github.com/kart-io/goagent/tools"

myTool := tools.NewFunctionTool(
    "my_tool",
    "工具描述",
    `{"type": "object", "properties": {"param": {"type": "string"}}}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        param := args["param"].(string)
        return doSomething(param), nil
    },
)
```

### 方式 2：使用 FunctionToolBuilder

```go
myTool := tools.NewFunctionToolBuilder("my_tool").
    WithDescription("工具描述").
    WithArgsSchema(`{"type": "object", ...}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return result, nil
    }).
    MustBuild()
```

### 方式 3：实现 Tool 接口

```go
type MyTool struct{}

func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Description() string { return "工具描述" }
func (t *MyTool) ArgsSchema() string { return `{...}` }
func (t *MyTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
    // 实现逻辑
    return &interfaces.ToolOutput{Result: result, Success: true}, nil
}
```

## 工具注册到 Agent

```go
agent, _ := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你是一个助手，可以使用以下工具...").
    WithTools(tool1, tool2, tool3).
    Build()
```

## 安全注意事项

1. **Shell 工具**：始终使用白名单限制允许的命令
2. **HTTP 工具**：验证 URL，避免 SSRF 攻击
3. **文件操作工具**：限制访问路径，避免目录遍历
4. **网页抓取工具**：遵守 robots.txt 和服务条款

## 相关文档

- [工具系统 API 参考](../../docs/api/TOOL_API.md)
- [工具中间件指南](../../docs/guides/TOOL_MIDDLEWARE.md)
- [Agent 使用指南](../../docs/guides/USER_GUIDE.md)

## 运行所有示例

```bash
# 运行单个示例
cd examples/tools/01-calculator && go run main.go

# 或使用脚本运行所有示例
for dir in examples/tools/0*; do
    echo "=== Running $dir ==="
    (cd "$dir" && go run main.go)
    echo ""
done
```
