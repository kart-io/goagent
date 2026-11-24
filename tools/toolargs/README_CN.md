# tools/toolargs 工具参数解析

[English](README.md) | [中文](README_CN.md)

解析模型产出的工具参数。`float64` 无法精确表示的数字保留为 `json.Number`，使校验、授权、审批、持久意图、缓存与执行看到同一个值。Builder 与 Dispatcher 已在使用它；自行解析或校验模型参数、或在工具内读取数字参数时直接调用。

## 用法

```go
import (
    "github.com/google/jsonschema-go/jsonschema"

    "github.com/kart-io/goagent/codec/json"
    "github.com/kart-io/goagent/tools/toolargs"
)

args, ok := toolargs.Parse(raw)
if !ok {
    return nil // 不是 JSON 对象，或数字超出 float64 范围
}

var s jsonschema.Schema
if err := json.Unmarshal([]byte(schemaText), &s); err != nil {
    return err
}
r, err := s.Resolve(nil)
if err != nil {
    return err
}
if err := toolargs.Validate(r, args); err != nil {
    return err
}

if n, ok := toolargs.Float64(args["limit"]); ok {
    _ = n
}
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Parse(raw)` | 解析 JSON 对象，丢精度的数字保留为 `json.Number` |
| `Validate(r, args)` | 按已解析的 schema 校验，`json.Number` 按原文精确检查 |
| `Float64(v)` | 把 `float64` 或 `json.Number` 参数读为 `float64` |
| `Compare(n, f)` | 按原文比较 `json.Number` 与 schema 边界 |
| `IsInteger(n)` | 按原文判断 `json.Number` 是否为整数 |

## 行为说明

- `Parse` 只接受对象（`null` 不算）。超出 `float64` 范围的数字（如 `1e400`）使其返回 `false`。
- `Validate` 对 `json.Number` 精确检查 `integer`、`minimum`、`maximum`、`exclusiveMinimum`、`exclusiveMaximum` 与 `multipleOf`。该值路径上的 schema 用了其他关键字（`anyOf`、`patternProperties`、元组 items、非本地 `$ref` 等）时直接返回错误。由于结构校验先看舍入后的值，部分合法值会被保守拒绝；错误码为 `errors.CodeToolValidation`，供交还模型修正。
- `Float64` 把 `json.Number` 舍入到最接近的 `float64`。

## 另见

- [原生编排指南：参数错误交还模型](../../docs/guides/NATIVE_ORCHESTRATION_CN.md#参数错误交还模型)
