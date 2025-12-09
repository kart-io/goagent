# MCP (Model Context Protocol) 工具箱示例

本目录包含 MCP 工具箱的完整使用示例，展示如何在 AI Agent 中集成和使用 MCP 工具。

## 目录结构

```text
mcp/
├── README.md           # 本文档
├── basic_tools/        # 基础工具使用示例
├── tool_chain/         # 工具链编排示例
└── custom_tool/        # 自定义工具示例
```

## 快速开始

### 运行示例

```bash
# 基础工具使用
cd examples/mcp/basic_tools
go run main.go

# 工具链编排
cd examples/mcp/tool_chain
go run main.go

# 自定义工具
cd examples/mcp/custom_tool
go run main.go
```

## 示例说明

### 1. 基础工具使用 (`basic_tools/`)

演示 MCP 工具箱的基础功能：

- **工具注册**: 如何注册内置工具和自定义工具
- **工具发现**: 列出、搜索、按分类筛选工具
- **工具执行**: 执行工具并获取结果
- **参数验证**: 输入参数的自动验证
- **统计信息**: 获取工具使用统计
- **调用历史**: 查看历史调用记录

```go
// 创建工具箱
tb := toolbox.NewStandardToolBox()

// 注册内置工具
tools.RegisterBuiltinTools(tb)

// 执行工具
call := &core.ToolCall{
    ID:       "call-1",
    ToolName: "read_file",
    Input:    map[string]interface{}{"path": "/tmp/test.txt"},
}
result, err := tb.Execute(ctx, call)
```

### 2. 工具链编排 (`tool_chain/`)

演示多工具协同工作的高级场景：

- **顺序执行**: 按顺序执行多个工具，传递中间结果
- **数据传递**: 在工具之间传递和转换数据
- **批量并行**: 同时执行多个独立的工具调用
- **条件分支**: 根据执行结果决定后续流程
- **错误处理**: 处理执行错误和重试逻辑

```go
// 顺序执行：读取文件 -> 解析 JSON -> 处理数据
result1, _ := executor.Execute(ctx, "read_file", map[string]interface{}{"path": configFile})
content := result1.Result.Data.(map[string]interface{})["content"].(string)

result2, _ := executor.Execute(ctx, "json_parse", map[string]interface{}{"json": content})

// 批量并行执行
results, _ := tb.ExecuteBatch(ctx, []*core.ToolCall{call1, call2, call3})
```

### 3. 自定义工具 (`custom_tool/`)

演示如何创建自定义 MCP 工具：

- **工具结构定义**: 使用 `BaseTool` 基类创建工具
- **参数 Schema**: 定义 JSON Schema 格式的参数规范
- **Execute 实现**: 实现工具的核心执行逻辑
- **Validate 实现**: 实现参数验证逻辑
- **流式输出**: 支持流式返回结果
- **错误处理**: 正确处理和返回错误

```go
type MyTool struct {
    *core.BaseTool
}

func NewMyTool() *MyTool {
    schema := &core.ToolSchema{
        Type: "object",
        Properties: map[string]core.PropertySchema{
            "param1": {
                Type:        "string",
                Description: "参数描述",
            },
        },
        Required: []string{"param1"},
    }

    return &MyTool{
        BaseTool: core.NewBaseTool("my_tool", "工具描述", "category", schema),
    }
}

func (t *MyTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
    // 实现执行逻辑
    return &core.ToolResult{
        Success: true,
        Data:    map[string]interface{}{"result": "value"},
    }, nil
}
```

## 内置工具列表

| 工具名称 | 分类 | 描述 | 危险 |
|---------|------|------|------|
| `read_file` | filesystem | 读取文件内容 | 否 |
| `write_file` | filesystem | 写入文件内容 | 是 |
| `list_directory` | filesystem | 列出目录内容 | 否 |
| `search_files` | filesystem | 搜索文件 | 否 |
| `http_request` | network | 发送 HTTP 请求 | 否 |
| `json_parse` | data | 解析 JSON 数据 | 否 |
| `shell_execute` | system | 执行 Shell 命令 | 是 |

## 核心概念

### MCPTool 接口

每个工具都必须实现 `MCPTool` 接口：

```go
type MCPTool interface {
    Name() string                                              // 工具名称
    Description() string                                       // 工具描述
    Category() string                                          // 工具分类
    Schema() *ToolSchema                                       // 参数 Schema
    Execute(ctx, input) (*ToolResult, error)                   // 执行工具
    Validate(input) error                                      // 验证参数
    RequiresAuth() bool                                        // 是否需要认证
    IsDangerous() bool                                         // 是否危险操作
}
```

### ToolBox 接口

工具箱管理所有工具：

```go
type ToolBox interface {
    Register(tool MCPTool) error                               // 注册工具
    Unregister(name string) error                              // 注销工具
    Get(name string) (MCPTool, error)                          // 获取工具
    List() []MCPTool                                           // 列出所有工具
    Execute(ctx, call) (*ToolCallResult, error)                // 执行工具
    ExecuteBatch(ctx, calls) ([]*ToolCallResult, error)        // 批量执行
    HasPermission(userID, toolName) (bool, error)              // 检查权限
    Statistics() *ToolBoxStatistics                            // 统计信息
}
```

### ToolSchema 定义

使用 JSON Schema 格式定义工具参数：

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
        "tags": {
            Type:        "array",
            Description: "标签列表",
            Items: &core.PropertySchema{
                Type: "string",
            },
        },
    },
    Required: []string{"name"},
}
```

## 权限管理

MCP 工具箱支持细粒度的权限控制：

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
    Reason:            "需要管理员权限",
})
```

## 最佳实践

### 工具命名

使用动词_名词格式：

- `read_file`
- `write_file`
- `list_directory`
- `search_files`

### 参数设计

- 必需参数放在 `Required` 列表中
- 提供合理的默认值
- 添加详细的参数描述
- 使用适当的类型和验证规则

### 错误处理

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

### 性能优化

- 使用上下文取消长时间运行的操作
- 对大文件使用流式处理
- 缓存重复的计算结果
- 设置合理的超时时间

## 扩展阅读

- [MCP 模块文档](../../../mcp/README.md)
- [工具接口定义](../../../mcp/core/tool.go)
- [工具箱实现](../../../mcp/toolbox/toolbox.go)
- [内置工具列表](../../../mcp/tools/)
