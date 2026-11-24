# tools/toolargs: Tool Argument Parsing

[English](README.md) | [中文](README_CN.md)

Parses tool arguments produced by the model. Numbers that `float64` cannot represent exactly are kept as `json.Number`, so validation, authorization, approval, durable intents, caching and execution all see the same value. The Builder and Dispatcher already use it; call it directly when you parse or validate model arguments yourself, or read numeric arguments inside a tool.

## Usage

```go
import (
    "github.com/google/jsonschema-go/jsonschema"

    "github.com/kart-io/goagent/codec/json"
    "github.com/kart-io/goagent/tools/toolargs"
)

args, ok := toolargs.Parse(raw)
if !ok {
    return nil // not a JSON object, or a number outside the float64 range
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

## Key API

| Identifier | Description |
|---|---|
| `Parse(raw)` | Parses a JSON object; keeps precision-losing numbers as `json.Number` |
| `Validate(r, args)` | Validates against a resolved schema, checking `json.Number` values exactly |
| `Float64(v)` | Reads a `float64` or `json.Number` argument as `float64` |
| `Compare(n, f)` | Compares a `json.Number` with a schema bound by literal text |
| `IsInteger(n)` | Reports whether a `json.Number` is an integer by literal text |

## Behavior Notes

- `Parse` accepts only objects (`null` is rejected). Numbers outside the `float64` range, such as `1e400`, make it return `false`.
- `Validate` checks `json.Number` values exactly for `integer`, `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` and `multipleOf`. If the schema on such a value's path uses other keywords (`anyOf`, `patternProperties`, tuple items, non-local `$ref`, etc.), it returns an error. Some valid values are rejected conservatively because structural validation looks at the rounded value first; errors use code `errors.CodeToolValidation` and are meant to be handed back to the model.
- `Float64` rounds a `json.Number` to the nearest `float64`.

## See Also

- [Native Orchestration guide, Returning Argument Errors to the Model](../../docs/guides/NATIVE_ORCHESTRATION.md#returning-argument-errors-to-the-model)
