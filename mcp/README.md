# MCP (Model Context Protocol) 工具箱

完整的 MCP 工具箱实现，为 AI Agent 提供强大的工具调用能力。

## 特性

- 符合 MCP 协议规范
- 完整的工具生命周期管理
- JSON Schema 参数验证
- 权限和安全控制
- 速率限制
- 工具执行审计
- 流式输出支持
- 7+ 内置工具

## 架构

```
mcp/
├── core/                    # MCP 核心接口
│   ├── tool.go             # 工具接口定义
│   └── toolbox.go          # 工具箱接口定义
├── toolbox/                # 工具箱实现
│   ├── toolbox.go          # 标准工具箱
│   ├── registry.go         # 工具注册表
│   ├── executor.go         # 工具执行器
│   ├── validator.go        # 参数验证器
│   └── permission.go       # 权限管理器
└── tools/                  # 内置工具集
    ├── read_file.go        # 读取文件
    ├── write_file.go       # 写入文件
    ├── filesystem.go       # 文件系统工具
    ├── network.go          # 网络工具
    └── registry.go         # 工具注册表
```

## 核心接口

### Tool 接口

每个工具都实现 `Tool` 接口：

```go
type Tool interface {
    Name() string                    // 工具名称
    Description() string             // 工具描述
    Category() string                // 工具分类
    Schema() *ToolSchema             // 参数 Schema
    Execute(ctx, input) (*ToolResult, error)  // 执行工具
    Validate(input) error            // 验证参数
    RequiresAuth() bool              // 是否需要认证
    IsDangerous() bool               // 是否危险操作
}
```

### ToolBox 接口

工具箱管理所有工具：

```go
type ToolBox interface {
    Register(tool Tool) error                    // 注册工具
    Unregister(name string) error                // 注销工具
    Get(name string) (Tool, error)               // 获取工具
    List() []Tool                                // 列出所有工具
    Execute(ctx, call) (*ToolCallResult, error)  // 执行工具
    HasPermission(userID, toolName) (bool, error) // 检查权限
    Statistics() *ToolBoxStatistics              // 统计信息
}
```

## 内置工具

### 文件系统工具

| 工具名称         | 描述         | 危险 |
| ---------------- | ------------ | ---- |
| `read_file`      | 读取文件内容 | ✗    |
| `write_file`     | 写入文件内容 | ✓    |
| `list_directory` | 列出目录内容 | ✗    |
| `search_files`   | 搜索文件     | ✗    |

### 网络工具

| 工具名称       | 描述           | 危险 |
| -------------- | -------------- | ---- |
| `http_request` | 发送 HTTP 请求 | ✗    |

### 数据处理工具

| 工具名称     | 描述           | 危险 |
| ------------ | -------------- | ---- |
| `json_parse` | 解析 JSON 数据 | ✗    |

### 系统工具

| 工具名称        | 描述            | 危险 |
| --------------- | --------------- | ---- |
| `shell_execute` | 执行 Shell 命令 | ✓    |

## 快速开始

### 1. 创建工具箱并注册工具

```go
import (
    "github.com/kart-io/goagent/mcp/toolbox"
    "github.com/kart-io/goagent/mcp/tools"
)

// 创建工具箱
tb := toolbox.NewStandardToolBox()

// 注册所有内置工具
tools.RegisterBuiltinTools(tb)

// 或手动注册单个工具
tb.Register(tools.NewReadFileTool())
```

### 2. 执行工具

```go
import "github.com/kart-io/goagent/mcp/core"

// 创建工具调用
call := &core.ToolCall{
    ID:       "call-1",
    ToolName: "read_file",
    Input: map[string]interface{}{
        "path": "/tmp/test.txt",
    },
    Timestamp: time.Now(),
}

// 执行工具
result, err := tb.Execute(context.Background(), call)
if err != nil {
    log.Fatal(err)
}

// 检查结果
if result.Result.Success {
    data := result.Result.Data.(map[string]interface{})
    fmt.Println("文件内容:", data["content"])
}
```

### 3. 权限管理

```go
// 获取权限管理器
pm := toolbox.NewPermissionManager()
tb.SetPermissionManager(pm)

// 设置用户权限
pm.SetPermission(&core.ToolPermission{
    UserID:            "user-1",
    ToolName:          "read_file",
    Allowed:           true,
    MaxCallsPerMinute: 100,
})

// 拒绝危险操作
pm.SetPermission(&core.ToolPermission{
    UserID:            "user-1",
    ToolName:          "shell_execute",
    Allowed:           false,
    Reason:            "危险操作需要管理员权限",
})
```

## 自定义工具

### 1. 定义工具结构

```go
type MyCustomTool struct {
    *core.BaseTool
}

func NewMyCustomTool() *MyCustomTool {
    schema := &core.ToolSchema{
        Type: "object",
        Properties: map[string]core.PropertySchema{
            "param1": {
                Type:        "string",
                Description: "第一个参数",
            },
        },
        Required: []string{"param1"},
    }

    return &MyCustomTool{
        BaseTool: core.NewBaseTool(
            "my_tool",
            "我的自定义工具",
            "custom",
            schema,
        ),
    }
}
```

### 2. 实现 Execute 方法

```go
func (t *MyCustomTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
    startTime := time.Now()

    param1 := input["param1"].(string)

    // 执行工具逻辑
    result := doSomething(param1)

    return &core.ToolResult{
        Success: true,
        Data: map[string]interface{}{
            "result": result,
        },
        Duration:  time.Since(startTime),
        Timestamp: time.Now(),
    }, nil
}
```

### 3. 实现 Validate 方法

```go
func (t *MyCustomTool) Validate(input map[string]interface{}) error {
    param1, ok := input["param1"].(string)
    if !ok || param1 == "" {
        return &core.ErrInvalidInput{
            Field:   "param1",
            Message: "必须是非空字符串",
        }
    }
    return nil
}
```

### 4. 注册工具

```go
tb.Register(NewMyCustomTool())
```

## 工具 Schema 定义

工具使用 JSON Schema 定义参数：

```go
schema := &core.ToolSchema{
    Type: "object",
    Properties: map[string]core.PropertySchema{
        "name": {
            Type:        "string",
            Description: "名称",
            MinLength:   intPtr(1),
            MaxLength:   intPtr(100),
        },
        "age": {
            Type:        "integer",
            Description: "年龄",
            Minimum:     float64Ptr(0),
            Maximum:     float64Ptr(150),
        },
        "email": {
            Type:        "string",
            Description: "邮箱",
            Format:      "email",
        },
        "tags": {
            Type:        "array",
            Description: "标签列表",
            Items: &core.PropertySchema{
                Type: "string",
            },
        },
    },
    Required: []string{"name", "age"},
}
```

## 安全特性

### 1. 工具认证

```go
tool.SetRequiresAuth(true)  // 需要认证
```

### 2. 危险操作标记

```go
tool.SetIsDangerous(true)  // 标记为危险操作
```

### 3. 权限控制

```go
// 检查用户是否有权限
allowed, err := tb.HasPermission("user-1", "shell_execute")
```

### 4. 速率限制

```go
pm.SetPermission(&core.ToolPermission{
    UserID:            "user-1",
    ToolName:          "http_request",
    MaxCallsPerMinute: 60,  // 每分钟最多 60 次
})
```

### 5. 审计日志

```go
// 获取调用历史
history := tb.GetCallHistory()

for _, record := range history {
    fmt.Printf("工具: %s, 用户: %s, 时间: %v\n",
        record.Call.ToolName,
        record.Call.UserID,
        record.ExecutedAt,
    )
}
```

## 统计信息

```go
stats := tb.Statistics()

fmt.Printf("工具总数: %d\n", stats.TotalTools)
fmt.Printf("总调用次数: %d\n", stats.TotalCalls)
fmt.Printf("成功率: %.2f%%\n",
    float64(stats.SuccessfulCalls)/float64(stats.TotalCalls)*100)
fmt.Printf("平均延迟: %.2f ms\n", stats.AverageLatency)

// 工具使用排名
for tool, count := range stats.ToolUsage {
    fmt.Printf("%s: %d 次\n", tool, count)
}
```

## 示例

### 基础工具使用

```bash
cd examples/mcp/basic_tools
go run main.go
```

演示：

- 工具注册
- 工具执行
- 参数验证
- 统计信息
- 调用历史

### 工具链编排

```bash
cd examples/mcp/tool_chain
go run main.go
```

演示：

- 顺序执行多个工具
- 工具间数据传递
- 条件分支
- 错误处理

### 自定义工具

```bash
cd examples/mcp/custom_tool
go run main.go
```

演示：

- 创建自定义工具
- 实现复杂参数验证
- 流式输出
- 错误处理

## 最佳实践

### 1. 工具命名

使用动词\_名词格式：

- `read_file` ✓
- `write_file` ✓
- `list_directory` ✓

### 2. 参数设计

- 必需参数放在 `Required` 列表中
- 提供合理的默认值
- 添加详细的参数描述
- 使用适当的验证规则

### 3. 错误处理

```go
if err != nil {
    return &core.ToolResult{
        Success:   false,
        Error:     fmt.Sprintf("操作失败: %v", err),
        ErrorCode: "OPERATION_FAILED",
        Timestamp: time.Now(),
    }, err
}
```

### 4. 性能优化

- 使用上下文取消长时间运行的操作
- 对大文件使用流式处理
- 缓存重复的计算结果
- 设置合理的超时时间

### 5. 安全考虑

- 验证所有输入参数
- 限制文件访问路径
- 清理 Shell 命令参数
- 记录所有危险操作
- 实施速率限制

## 扩展

### 添加新工具类别

1. 在 `tools/` 下创建新文件
2. 实现工具接口
3. 注册到 `registry.go`

### 自定义验证器

```go
type CustomValidator struct {
    *toolbox.JSONSchemaValidator
}

func (v *CustomValidator) ValidateInput(schema, input) error {
    // 自定义验证逻辑
    return nil
}

tb.validator = NewCustomValidator()
```

### 自定义执行器

```go
type CustomExecutor struct {
    *toolbox.StandardExecutor
}

func (e *CustomExecutor) Execute(ctx, tool, call) (*core.ToolResult, error) {
    // 添加预处理
    result, err := e.StandardExecutor.Execute(ctx, tool, call)
    // 添加后处理
    return result, err
}

tb.executor = NewCustomExecutor()
```

## 性能指标

- 工具注册: < 1ms
- 参数验证: < 1ms
- 工具执行: 取决于具体工具
- 权限检查: < 0.1ms
- 统计更新: < 0.1ms

## 限制

- 同步执行（异步执行可通过 goroutine 实现）
- 内存中的权限存储（可扩展为持久化）
- 简单的速率限制（可扩展为分布式限流）

## 未来计划

- [ ] 工具链编排 DSL
- [ ] 异步工具执行
- [ ] 分布式工具注册
- [ ] 工具版本管理
- [ ] 工具依赖管理
- [ ] 更多内置工具（目标 30+）
- [ ] MCP 协议服务器/客户端
- [ ] 工具市场

## 贡献

欢迎贡献新工具！请遵循以下步骤：

1. 在 `tools/` 下创建工具文件
2. 实现 `Tool` 接口
3. 添加完整的测试
4. 更新文档
5. 提交 PR

## 许可证

[项目许可证]
