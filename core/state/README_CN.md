# state Agent 状态

[English](README.md) | [中文](README_CN.md)

本包定义 Agent 状态接口 `State` 及两种实现。`AgentState` 是带读写锁的键值表，是 Builder 的默认状态类型（`core.State`、`core.AgentState`、`core.NewAgentState` 都是本包的别名），也是 `core/middleware` 请求携带的 `State`。`TypedState` 在此之上加入 `StateSchema`：字段类型、必填字段、默认值、自定义校验与严格模式，并提供带版本的 JSON 信封。状态只保存在进程内，本包不做任何持久化。

## 用法

```go
import (
    "fmt"

    "github.com/kart-io/goagent/codec/json"
    "github.com/kart-io/goagent/core/state"
)

s := state.NewAgentState()
s.Set("user_id", "u1")
s.Update(map[string]interface{}{"turn": 1, "tags": []string{"a"}})

if name, ok := s.GetString("user_id"); ok {
    fmt.Println(name)
}
snap := s.Snapshot() // map 的副本，可随意修改
clone := s.Clone()   // 深拷贝
fmt.Println(len(snap), clone.Size())
```

带 schema 校验的状态：

```go
schema := state.NewStateSchema("order", "v1").
    AddField(&state.FieldSchema{Name: "id", Type: state.FieldTypeString, Required: true}).
    AddField(&state.FieldSchema{Name: "qty", Type: state.FieldTypeInt, Default: 1}).
    WithStrictMode()

ts := state.NewTypedState(schema) // qty 初始为默认值
if err := ts.Set("id", "o-1"); err != nil {
    return err
}
if err := ts.Set("unknown", true); err != nil {
    fmt.Println("rejected:", err) // 严格模式拒绝未定义字段
}
qty, err := ts.GetInt("qty") // int64
if err != nil {
    return err
}
fmt.Println(qty)

data, err := json.Marshal(ts) // {"schema_name":"order","schema_version":"v1","data":{...}}
if err != nil {
    return err
}
restored := state.NewTypedState(schema)
return json.Unmarshal(data, restored)
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `State` | 接口：`Get`、`Set`、`Update`、`Snapshot`、`Clone`、`Delete`、`Clear`、`Keys`、`Size` |
| `NewAgentState()` / `NewAgentStateWithData(m)` | 并发安全的 `State` 实现 |
| `AgentState.GetString`、`GetInt`、`GetFloat64`、`GetBool`、`GetMap`、`GetSlice` | 类型化读取；键不存在或类型不符时返回 false |
| `NewStateSchema(name, version)` | schema；`AddField`、`WithStrictMode`、`Validate(map)` |
| `FieldSchema` / `FieldType` | 字段定义（`Type`、`Required`、`Default`、`ItemType`、`ObjectSchema`、`Validator`）与 `FieldType*` 常量 |
| `NewTypedState(schema)` | 带 schema 校验的状态；`Set`、`SetBatch`、`FromMap`、`ToMap`、`GetString`/`GetInt`/`GetFloat`/`GetBool` |
| `TypedState.IsDirty` / `MarkClean` | 未保存修改标记，供调用方自己的持久化使用 |
| `NewSchemaRegistry()` / `GlobalSchemaRegistry()` | 按名称与版本登记 schema；`Register`、`Get`、`CreateState` |
| `StateEnvelope` | `TypedState.MarshalJSON` 写出的 JSON 结构 |

## 行为说明

- `AgentState` 与 `TypedState` 由 `sync.RWMutex` 保护，可并发使用。
- `AgentState.Clone` 深拷贝 map 与切片；其他复杂值经 JSON 往返复制回原类型，往返失败时保留原值。
- `AgentState.UnmarshalJSON` 替换全部值，恢复为通用 JSON 类型（数字为 `float64`，对象为 map）。需要恢复原类型时，定义自己的 `State` 并实现这两个 JSON 方法。
- `TypedState.Set` 校验已定义字段；未定义字段只在严格模式下被拒绝。`SetBatch` 与 `FromMap` 先校验整个 map（含必填字段）；`FromMap` 替换全部数据并清除 dirty 标记。
- `TypedState.Delete` 拒绝删除必填字段。`TypedState.Clone` 复制 map，但不复制其中的值。
- `TypedState.UnmarshalJSON` 在 schema 版本不一致时失败，把 `int`、`bytes`、`time` 字段恢复为声明的类型，并且只有全部字段解码和校验通过后才替换状态。
- `SchemaRegistry.Register` 在同名同版本已登记时返回错误。

## 另见

- [Builder API 参考](../../docs/guides/BUILDER_API_REFERENCE_CN.md)：`NewAgentBuilder[S core.State]`、`WithState`、自定义状态类型
- [middleware](../middleware/README_CN.md)：`MiddlewareRequest.State`
