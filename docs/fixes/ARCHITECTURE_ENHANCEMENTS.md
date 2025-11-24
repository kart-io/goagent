# GoAgent 架构增强报告

## 概述

本报告记录了 GoAgent 框架的三个关键架构增强，解决了以下核心问题：

1. **泛型与动态插件冲突** - 类型擦除边界问题
2. **状态序列化瓶颈** - `map[string]interface{}` 契约地狱
3. **生命周期管理缺失** - 缺乏统一的 Init/Start/Stop/HealthCheck

---

## 1. 泛型与动态插件桥接 (Plugin Bridge)

### 问题描述

Go 泛型 (`Runnable[I, O]`) 在编译时提供类型安全，但与运行时动态插件系统存在根本冲突：

- 插件在运行时加载，编译器无法验证类型
- 泛型类型信息在编译后被擦除
- 无法将 `Runnable[string, int]` 安全地转换为动态类型

### 解决方案

创建三层桥接架构：

```text
┌─────────────────────────────────────────────────────┐
│  Layer 1: DynamicRunnable (插件兼容接口)            │
│  - InvokeDynamic(ctx, any) (any, error)             │
│  - StreamDynamic(ctx, any) (<-chan, error)          │
│  - BatchDynamic(ctx, []any) ([]any, error)          │
│  - TypeInfo() TypeInfo                              │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│  Layer 2: Type Adapters (类型转换)                  │
│  - TypedToDynamicAdapter[I,O]: 泛型 → 动态          │
│  - DynamicToTypedAdapter[I,O]: 动态 → 泛型          │
└─────────────────────────────────────────────────────┘
                         ↕
┌─────────────────────────────────────────────────────┐
│  Layer 3: PluginRegistry (插件注册中心)             │
│  - Register/Unregister 插件                         │
│  - 运行时类型检查                                   │
│  - 元数据管理                                       │
└─────────────────────────────────────────────────────┘
```

### 新增文件

| 文件 | 描述 |
|------|------|
| `core/plugin_bridge.go` | 核心实现 |
| `core/plugin_bridge_test.go` | 测试 (12 个用例) |

### 使用示例

```go
// 注册类型安全组件到插件系统
registry := core.NewPluginRegistry()
processor := core.NewRunnableFunc(func(ctx context.Context, s string) (int, error) {
    return len(s), nil
})
core.RegisterTyped(registry, "string-length", processor, nil)

// 动态调用
dynamic, _ := registry.Get("string-length")
result, _ := dynamic.InvokeDynamic(ctx, "hello") // result = 5

// 类型安全调用
typed, _ := core.GetTyped[string, int](registry, "string-length")
result, _ := typed.Invoke(ctx, "hello") // result = 5，编译时类型检查
```

---

## 2. 类型安全状态管理 (TypedState)

### 问题描述

原有 `map[string]interface{}` 状态管理存在问题：

- 无契约：任何组件可以写入任意字段
- 序列化脆弱：字段名拼写错误导致数据丢失
- 类型不安全：运行时才发现类型错误
- 分布式困难：缺乏版本控制和模式验证

### 解决方案

创建 Schema 验证的类型安全状态系统：

```text
┌─────────────────────────────────────────────────────┐
│  StateSchema                                        │
│  - 字段定义 (名称、类型、是否必需)                  │
│  - 自定义验证器                                     │
│  - 嵌套对象支持                                     │
│  - 严格/宽松模式                                    │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  TypedState                                         │
│  - Schema 绑定验证                                  │
│  - 类型安全 Getter/Setter                           │
│  - 脏数据跟踪                                       │
│  - JSON 序列化（带版本信封）                        │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  SchemaRegistry                                     │
│  - Schema 版本管理                                  │
│  - 全局/局部注册                                    │
│  - 按 Schema 创建状态                               │
└─────────────────────────────────────────────────────┘
```

### 新增文件

| 文件 | 描述 |
|------|------|
| `core/state/typed_state.go` | 核心实现 |
| `core/state/typed_state_test.go` | 测试 (38 个用例) |

### 使用示例

```go
// 定义 Schema
schema := state.NewStateSchema("agent-state", "1.0").
    AddField(&state.FieldSchema{
        Name:     "conversation_id",
        Type:     state.FieldTypeString,
        Required: true,
    }).
    AddField(&state.FieldSchema{
        Name:    "message_count",
        Type:    state.FieldTypeInt,
        Default: 0,
    }).
    WithStrictMode()

// 创建状态
ts := state.NewTypedState(schema)
ts.Set("conversation_id", "conv-123") // ✓
ts.Set("message_count", 5)            // ✓
ts.Set("unknown_field", "x")          // ✗ 严格模式报错

// 类型安全读取
count, _ := ts.GetInt("message_count")  // int64

// 序列化（带版本信息）
data, _ := json.Marshal(ts)
// {"schema_name":"agent-state","schema_version":"1.0","data":{...}}
```

---

## 3. 统一生命周期管理 (Lifecycle Management)

### 问题描述

原有组件缺乏统一生命周期：

- Tool/Agent 只有 Invoke 方法
- 无初始化钩子（连接池、配置加载）
- 无优雅关闭（资源释放、状态保存）
- 无健康检查（监控、自愈）

### 解决方案

创建统一的 Lifecycle 接口和 Manager：

```text
┌─────────────────────────────────────────────────────┐
│  Lifecycle Interface                                │
│  - Init(ctx, config) error                          │
│  - Start(ctx) error                                 │
│  - Stop(ctx) error                                  │
│  - HealthCheck(ctx) HealthStatus                    │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  LifecycleManager                                   │
│  - 优先级启动/关闭                                  │
│  - 依赖拓扑排序                                     │
│  - 并发健康检查                                     │
│  - 优雅关闭信号                                     │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Extensions                                         │
│  - DependencyAware: 声明依赖                        │
│  - Reloadable: 热重载配置                           │
│  - LifecycleAware: 简化钩子                         │
└─────────────────────────────────────────────────────┘
```

### 新增文件

| 文件 | 描述 |
|------|------|
| `interfaces/lifecycle.go` | 接口定义 |
| `core/lifecycle_manager.go` | Manager 实现 |
| `core/lifecycle_manager_test.go` | 测试 (16 个用例) |

### 使用示例

```go
// 创建组件
db := NewDatabaseComponent()
cache := NewCacheComponent()
app := NewAppComponent()

// 配置生命周期管理器
manager := core.NewLifecycleManager()
manager.Register("database", db, 1)    // 优先级 1，先启动
manager.Register("cache", cache, 2)    // 优先级 2
manager.Register("app", app, 3)        // 优先级 3，依赖前两者

// 启动所有组件（按优先级顺序）
manager.InitAll(ctx)
manager.StartAll(ctx)

// 健康检查
health := manager.HealthCheckAll(ctx)
for name, status := range health {
    fmt.Printf("%s: %s\n", name, status.State)
}

// 优雅关闭（逆序）
manager.StopAll(ctx)
```

### 依赖感知示例

```go
type AppComponent struct {
    core.BaseLifecycle
}

// 声明依赖
func (a *AppComponent) Dependencies() []string {
    return []string{"database", "cache"}
}

// 管理器自动按依赖顺序启动：database → cache → app
// 关闭顺序：app → cache → database
```

---

## 测试验证

### 测试统计

| 模块 | 测试数量 | 状态 |
|------|----------|------|
| Plugin Bridge | 12 | ✅ PASS |
| TypedState | 38 | ✅ PASS |
| Lifecycle Manager | 16 | ✅ PASS |
| **总计** | **66** | ✅ PASS |

### 质量检查

```bash
make lint           # 0 issues
./verify_imports.sh # All import layering rules satisfied
go test ./...       # All tests pass
```

---

## 新增错误码

在 `errors/errors.go` 中添加：

```go
// Plugin/Type system errors
CodeTypeMismatch   ErrorCode = "TYPE_MISMATCH"
CodeAlreadyExists  ErrorCode = "ALREADY_EXISTS"
CodeNotFound       ErrorCode = "NOT_FOUND"
CodeInvalidOutput  ErrorCode = "INVALID_OUTPUT"
```

---

## 架构影响

### 分层遵循

所有新增代码严格遵循 4 层架构：

- **Layer 1** (`interfaces/`): `lifecycle.go` - 接口定义
- **Layer 2** (`core/`): `plugin_bridge.go`, `lifecycle_manager.go`
- **Layer 2** (`core/state/`): `typed_state.go`

### 向后兼容

- ✅ 所有新功能为可选增强
- ✅ 现有代码无需修改
- ✅ 渐进式迁移支持

---

## 使用建议

### 何时使用 Plugin Bridge

- 需要动态加载组件（Go 插件、RPC 服务）
- 跨模块传递 Runnable（类型信息丢失）
- 构建可扩展的插件系统

### 何时使用 TypedState

- 分布式 Agent 状态同步
- 需要状态版本控制
- 严格的数据契约要求

### 何时使用 Lifecycle Manager

- 多组件服务启动
- 需要健康检查监控
- 优雅关闭要求

---

## 完成时间

**日期**: 2025-11-24
**验证状态**: ✅ 全部通过

**核心指标**:

- Lint Issues: 0
- Import Violations: 0
- New Tests: 66
- New Files: 6
