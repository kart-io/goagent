# state: Agent State

[English](README.md) | [中文](README_CN.md)

This package defines the `State` interface for agent state and two implementations. `AgentState` is a mutex-guarded key/value map; it is the default state type of the Builder (`core.State`, `core.AgentState` and `core.NewAgentState` are aliases of this package) and the `State` carried by `core/middleware` requests. `TypedState` adds a `StateSchema` with field types, required fields, defaults, custom validators and strict mode, plus a versioned JSON envelope. State is in-process only; this package does not persist anything.

## Usage

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
snap := s.Snapshot() // copy of the map, safe to modify
clone := s.Clone()   // deep copy
fmt.Println(len(snap), clone.Size())
```

Schema-validated state:

```go
schema := state.NewStateSchema("order", "v1").
    AddField(&state.FieldSchema{Name: "id", Type: state.FieldTypeString, Required: true}).
    AddField(&state.FieldSchema{Name: "qty", Type: state.FieldTypeInt, Default: 1}).
    WithStrictMode()

ts := state.NewTypedState(schema) // qty starts at its default
if err := ts.Set("id", "o-1"); err != nil {
    return err
}
if err := ts.Set("unknown", true); err != nil {
    fmt.Println("rejected:", err) // strict mode rejects undefined fields
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

## Key API

| Identifier | Description |
|---|---|
| `State` | Interface: `Get`, `Set`, `Update`, `Snapshot`, `Clone`, `Delete`, `Clear`, `Keys`, `Size` |
| `NewAgentState()` / `NewAgentStateWithData(m)` | Thread-safe `State` implementation |
| `AgentState.GetString`, `GetInt`, `GetFloat64`, `GetBool`, `GetMap`, `GetSlice` | Typed getters; return false when missing or of another type |
| `NewStateSchema(name, version)` | Schema; `AddField`, `WithStrictMode`, `Validate(map)` |
| `FieldSchema` / `FieldType` | Field definition (`Type`, `Required`, `Default`, `ItemType`, `ObjectSchema`, `Validator`) and the `FieldType*` constants |
| `NewTypedState(schema)` | Schema-validated state; `Set`, `SetBatch`, `FromMap`, `ToMap`, `GetString`/`GetInt`/`GetFloat`/`GetBool` |
| `TypedState.IsDirty` / `MarkClean` | Unsaved-change flag for the caller's own persistence |
| `NewSchemaRegistry()` / `GlobalSchemaRegistry()` | Schemas keyed by name and version; `Register`, `Get`, `CreateState` |
| `StateEnvelope` | JSON shape written by `TypedState.MarshalJSON` |

## Behavior Notes

- `AgentState` and `TypedState` are guarded by `sync.RWMutex` and safe for concurrent use.
- `AgentState.Clone` deep-copies maps and slices; other complex values are copied through a JSON round trip into their original type, and kept as is when that round trip fails.
- `AgentState.UnmarshalJSON` replaces all values and restores generic JSON types (numbers as `float64`, objects as maps). To restore original types, define a custom `State` with its own JSON methods.
- `TypedState.Set` validates defined fields; undefined fields are rejected only in strict mode. `SetBatch` and `FromMap` validate the whole map first (including required fields); `FromMap` replaces all data and clears the dirty flag.
- `TypedState.Delete` refuses to delete a required field. `TypedState.Clone` copies the map but not the values inside it.
- `TypedState.UnmarshalJSON` fails on a schema version mismatch, restores `int`, `bytes` and `time` fields to their declared types, and replaces the state only after every field decodes and validates.
- `SchemaRegistry.Register` returns an error when the same name and version is already registered.

## See Also

- [Builder API Reference](../../docs/guides/BUILDER_API_REFERENCE.md): `NewAgentBuilder[S core.State]`, `WithState`, custom state types
- [middleware](../middleware/README.md): `MiddlewareRequest.State`
