# GoAgent 插件系统完整指南

## 概述

GoAgent 实现了灵活的插件化架构，支持两种插件模式：

1. **进程内适配器模式** (In-Process Adapter) - 推荐
2. **基于 MCP 的远程插件** (Remote Process/RPC)

本指南展示如何使用完整的插件系统功能。

---

## 架构设计

### 核心组件

```text
┌────────────────────────────────────────────────────────┐
│  Enhanced Plugin Registry                              │
│  - 版本管理 (多版本共存)                                │
│  - 依赖注入容器                                         │
│  - 生命周期管理                                         │
└────────────────────────────────────────────────────────┘
                         ↓
┌────────────────────────────────────────────────────────┐
│  Plugin Interface                                      │
│  - Init/Start/Stop/HealthCheck                         │
│  - GetTools/GetAgents/GetMiddleware                    │
└────────────────────────────────────────────────────────┘
                         ↓
┌────────────────────────────────────────────────────────┐
│  Type Adapters                                         │
│  - TypedToDynamicAdapter (泛型→动态)                    │
│  - DynamicToTypedAdapter (动态→泛型)                    │
└────────────────────────────────────────────────────────┘
```

---

## 方案一：进程内适配器模式 (推荐)

### 示例：创建自定义插件

```go
package myplugin

import (
    "context"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
)

// MyCustomPlugin 自定义插件实现
type MyCustomPlugin struct {
    *core.BasePlugin
    config map[string]interface{”}
}

// NewMyCustomPlugin 创建插件实例
func NewMyCustomPlugin() *MyCustomPlugin {
    plugin := &MyCustomPlugin{
        BasePlugin: core.NewBasePlugin("my-custom-plugin", "1.0.0"),
    }

    // 添加工具
    searchTool := core.NewRunnableFunc(func(ctx context.Context, query string) (string, error) {
        return "Search results for: " + query, nil
    })
    plugin.AddTool(core.NewTypedToDynamicAdapter(searchTool, "search"))

    // 添加代理
    assistantAgent := core.NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
        return "Assistant response: " + input, nil
    })
    plugin.AddAgent(core.NewTypedToDynamicAdapter(assistantAgent, "assistant"))

    return plugin
}

// Init 初始化插件
func (p *MyCustomPlugin) Init(ctx context.Context, config interface{}) error {
    if cfg, ok := config.(map[string]interface{}); ok {
        p.config = cfg
    }

    // 从容器获取共享服务（如Logger）
    // logger := container.MustResolve("logger")

    return nil
}

// Start 启动插件
func (p *MyCustomPlugin) Start(ctx context.Context) error {
    // 启动后台任务、连接数据库等
    return nil
}

// Stop 停止插件
func (p *MyCustomPlugin) Stop(ctx context.Context) error {
    // 清理资源
    return nil
}

// HealthCheck 健康检查
func (p *MyCustomPlugin) HealthCheck(ctx context.Context) interfaces.HealthStatus {
    return interfaces.NewHealthyStatus()
}
```

### 注册和使用插件

```go
package main

import (
    "context"
    "fmt"
    "github.com/kart-io/goagent/core"
    "myplugin"
)

func main() {
    // 1. 创建增强注册中心
    registry := core.NewEnhancedPluginRegistry()

    // 2. 注册共享服务到容器
    logger := NewLogger()
    registry.Container().Register("logger", logger)

    // 3. 创建并注册插件
    plugin := myplugin.NewMyCustomPlugin()
    config := &core.PluginConfig{
        Name:    "my-custom-plugin",
        Version: "1.0.0",
        Config:  map[string]interface{”}{
            "api_key": "secret",
        },
        Enabled: true,
    }

    err := registry.RegisterPlugin(plugin, config)
    if err != nil {
        panic(err)
    }

    // 4. 初始化和启动所有插件
    ctx := context.Background()
    registry.InitializeAll(ctx)
    registry.StartAll(ctx)

    // 5. 使用插件提供的工具
    tools := registry.GetAllTools()
    for name, toolList := range tools {
        fmt.Printf("Plugin %s provides %d tools\n", name, len(toolList))

        for _, tool := range toolList {
            // 动态调用工具
            result, _ := tool.InvokeDynamic(ctx, "test query")
            fmt.Printf("  Result: %v\n", result)
        }
    }

    // 6. 优雅关闭
    defer registry.StopAll(ctx)
}
```

### 多版本支持示例

```go
// 注册同一插件的多个版本
registry := core.NewEnhancedPluginRegistry()

plugin_v1 := myplugin.NewMyCustomPlugin() // v1.0.0
plugin_v2 := myplugin.NewMyCustomPluginV2() // v2.0.0

registry.RegisterPlugin(plugin_v1, &core.PluginConfig{
    Name: "my-plugin", Version: "1.0.0",
})
registry.RegisterPlugin(plugin_v2, &core.PluginConfig{
    Name: "my-plugin", Version: "2.0.0",
})

// 获取特定版本
plugin, _ := registry.GetPlugin("my-plugin", "1.0.0")

// 获取最新版本
latest, version, _ := registry.GetLatestPlugin("my-plugin")
fmt.Printf("Latest version: %s\n", version) // 2.0.0
```

---

## 方案二：基于 MCP 的远程插件

### MCP 服务器插件示例

```go
package mcpplugin

import (
    "context"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/mcp"
)

// MCPPlugin 包装远程 MCP 服务器
type MCPPlugin struct {
    *core.BasePlugin
    client *mcp.Client
}

func NewMCPPlugin(serverPath string) (*MCPPlugin, error) {
    plugin := &MCPPlugin{
        BasePlugin: core.NewBasePlugin("mcp-plugin", "1.0.0"),
    }

    // 创建 MCP 客户端（连接到外部进程）
    client, err := mcp.NewStdioClient(serverPath)
    if err != nil {
        return nil, err
    }
    plugin.client = client

    return plugin, nil
}

func (p *MCPPlugin) Init(ctx context.Context, config interface{}) error {
    // 初始化 MCP 连接
    return p.client.Initialize(ctx)
}

func (p *MCPPlugin) GetTools() []core.DynamicRunnable {
    // 获取远程工具列表
    mcpTools, _ := p.client.ListTools(context.Background())

    // 包装为 DynamicRunnable
    tools := make([]core.DynamicRunnable, len(mcpTools))
    for i, mcpTool := range mcpTools {
        tools[i] = &MCPToolAdapter{
            client:   p.client,
            toolName: mcpTool.Name,
        }
    }
    return tools
}

func (p *MCPPlugin) Stop(ctx context.Context) error {
    return p.client.Close()
}
```

### MCP 工具适配器

```go
// MCPToolAdapter 将 MCP 工具适配为 DynamicRunnable
type MCPToolAdapter struct {
    client   *mcp.Client
    toolName string
}

func (a *MCPToolAdapter) InvokeDynamic(ctx context.Context, input any) (any, error) {
    // 将输入转换为 MCP 格式
    params := input.(map[string]any)

    // 调用远程工具
    result, err := a.client.CallTool(ctx, a.toolName, params)
    if err != nil {
        return nil, err
    }

    return result, nil
}

func (a *MCPToolAdapter) StreamDynamic(ctx context.Context, input any) (<-chan core.DynamicStreamChunk, error) {
    // 实现流式调用
    return nil, nil
}

func (a *MCPToolAdapter) BatchDynamic(ctx context.Context, inputs []any) ([]any, error) {
    results := make([]any, len(inputs))
    for i, input := range inputs {
        result, err := a.InvokeDynamic(ctx, input)
        if err != nil {
            return nil, err
        }
        results[i] = result
    }
    return results, nil
}

func (a *MCPToolAdapter) TypeInfo() core.TypeInfo {
    return core.TypeInfo{
        Name: a.toolName,
    }
}
```

---

## 依赖注入容器

### 注册服务

```go
container := registry.Container()

// 注册实例
logger := NewLogger()
container.Register("logger", logger)

// 注册工厂（延迟初始化）
container.RegisterFactory("database", func() (interface{}, error) {
    db, err := sql.Open("postgres", "...")
    return db, err
})
```

### 在插件中使用服务

```go
func (p *MyPlugin) Init(ctx context.Context, config interface{}) error {
    // 通过容器获取服务
    logger, err := core.ResolveTyped[*Logger](container, "logger")
    if err != nil {
        return err
    }

    logger.Info("Plugin initialized")

    // 工厂服务在第一次访问时才创建
    db, err := core.ResolveTyped[*sql.DB](container, "database")
    if err != nil {
        return err
    }

    return nil
}
```

---

## 插件配置

### 配置结构

```go
type PluginConfig struct {
    Name         string
    Version      string
    Config       map[string]interface{}
    Dependencies []PluginDependency
    Enabled      bool
}

type PluginDependency struct {
    Name              string
    VersionConstraint string
    Optional          bool
}
```

### 使用配置

```go
config := &core.PluginConfig{
    Name:    "my-plugin",
    Version: "1.0.0",
    Config: map[string]interface{”}{
        "api_key":   "secret",
        "timeout":   30,
        "debug":     true,
    },
    Dependencies: []core.PluginDependency{
        {
            Name:              "base-tools",
            VersionConstraint: ">=1.0.0",
            Optional:          false,
        },
    },
    Enabled: true,
}

registry.RegisterPlugin(plugin, config)
```

---

## 生命周期管理

### 插件生命周期阶段

```text
1. Uninitialized
       ↓ Init(config)
2. Initialized
       ↓ Start()
3. Running
       ↓ Stop()
4. Stopped
```

### 批量管理

```go
registry := core.NewEnhancedPluginRegistry()

// 注册多个插件
registry.RegisterPlugin(plugin1, config1)
registry.RegisterPlugin(plugin2, config2)
registry.RegisterPlugin(plugin3, config3)

// 批量初始化（按依赖顺序）
registry.InitializeAll(ctx)

// 批量启动
registry.StartAll(ctx)

// 健康检查
healthStatus := registry.Lifecycle().HealthCheckAll(ctx)
for name, status := range healthStatus {
    fmt.Printf("%s: %s\n", name, status.State)
}

// 优雅关闭（逆序）
defer registry.StopAll(ctx)
```

---

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "github.com/kart-io/goagent/core"
)

func main() {
    // 1. 创建注册中心
    registry := core.NewEnhancedPluginRegistry()

    // 2. 配置容器
    registry.Container().Register("config", map[string]string{
        "env": "production",
    })

    // 3. 注册插件
    searchPlugin := NewSearchPlugin()
    registry.RegisterPlugin(searchPlugin, &core.PluginConfig{
        Name: "search", Version: "1.0.0", Enabled: true,
    })

    analyticsPlugin := NewAnalyticsPlugin()
    registry.RegisterPlugin(analyticsPlugin, &core.PluginConfig{
        Name: "analytics", Version: "1.0.0", Enabled: true,
    })

    // 4. 启动系统
    ctx := context.Background()
    if err := registry.InitializeAll(ctx); err != nil {
        panic(err)
    }
    if err := registry.StartAll(ctx); err != nil {
        panic(err)
    }
    defer registry.StopAll(ctx)

    // 5. 使用插件功能
    tools := registry.GetAllTools()
    for pluginName, toolList := range tools {
        fmt.Printf("Plugin: %s\n", pluginName)
        for _, tool := range toolList {
            info := tool.TypeInfo()
            fmt.Printf("  - %s\n", info.Name)
        }
    }

    // 6. 运行时版本切换
    latest, version, _ := registry.GetLatestPlugin("search")
    fmt.Printf("Using search plugin version: %s\n", version)
}
```

---

## 最佳实践

### 1. 插件隔离

- 每个插件独立命名空间
- 通过容器共享服务，避免直接依赖
- 使用接口而非具体类型

### 2. 版本管理

- 使用语义化版本 (Semantic Versioning)
- 提供版本约束检查
- 支持多版本共存用于 A/B 测试

### 3. 错误处理

- Init 失败应阻止插件启动
- Start 失败应能回滚
- Stop 应能处理部分失败

### 4. 性能优化

- 使用工厂模式延迟初始化昂贵资源
- 批量操作优于单个操作
- 考虑插件缓存热数据

---

## 总结

GoAgent 插件系统提供：

✅ **类型安全**: 泛型与动态类型的无缝桥接
✅ **版本管理**: 多版本共存和依赖管理
✅ **依赖注入**: 容器化的服务管理
✅ **生命周期**: 统一的 Init/Start/Stop
✅ **双模式**: 进程内和远程 MCP 插件

推荐使用**进程内适配器模式**以获得最佳性能，在需要语言隔离时使用 **MCP 远程模式**。