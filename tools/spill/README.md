# tools/spill: Oversized Tool Result Spilling

[English](README.md) | [中文](README_CN.md)

Writes oversized tool results to a host-provided directory and replaces the result the model sees with a preview and a file name. The model reads the full content in segments with the `read_spill` tool when needed. Use it when tool results can be too large to put into the conversation. The directory is outside the workspace and is not affected by the workspace read-only mode or change tracking.

## Usage

```go
import (
    "time"

    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/spill"
)

store, err := spill.New(spillDir, 32*1024) // spill results larger than 32 KiB
if err != nil {
    return err
}
if err := registry.Register(store.ReadTool()); err != nil {
    return err
}
dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    Pipeline: &tools.Pipeline{Post: []tools.PostHook{store.Hook()}},
})

// remove spill files older than a day
err = store.Prune(24 * time.Hour)
```

To choose preview and read limits, use `NewWithConfig`:

```go
store, err := spill.NewWithConfig(spillDir, spill.Config{
    MaxBytes:       64 * 1024,
    PreviewRunes:   500,
    MaxLineRunes:   1000,
    ReadLimitLines: 200,
})
```

## Key API

| Identifier | Description |
|---|---|
| `New(dir, maxBytes)` | Creates a store with the preset preview and read limits |
| `NewWithConfig(dir, cfg)` | Creates a store with explicit limits |
| `Config` | `MaxBytes` (required), `PreviewRunes`, `MaxLineRunes`, `ReadLimitLines` |
| `(*Store).Hook()` | `tools.PostHook` that spills successful oversized results of any tool |
| `(*Store).ReadTool()` | The `read_spill` tool, limited to files in this directory |
| `(*Store).Prune(olderThan)` | Removes spill files modified more than `olderThan` ago |
| `(*Store).Dir()` | Returns the spill directory |

## Behavior Notes

- `New` and `NewWithConfig` create the directory if missing (mode `0700`). `MaxBytes` must be positive and the other limits must not be negative; otherwise an `errors.CodeInvalidConfig` error is returned.
- `New` uses `constants.SpillPreviewRunes`, `constants.SpillMaxLineRunes` and `constants.DefaultSpillReadLimitLines`. In `Config`, 0 for `PreviewRunes`, `MaxLineRunes` or `ReadLimitLines` means no truncation (read to the end).
- Only results with `Success == true` are considered. A result spills when its text form exceeds `MaxBytes`: strings as is, maps as one `== key ==` section per key, other values as indented JSON.
- The replaced result has `truncated`, `bytes`, `preview`, `note`, and `spill_path` when the write succeeded. If writing fails, the hook still returns the preview with the reason and no error, so a tool whose side effects already happened is not rerun.
- Files are named `<session>/<tool>-<call ID>.txt`; the session defaults to `default`, and a random ID is used when the call has none.
- `read_spill` takes `path`, `offset` (from 1) and `limit`, and returns numbered lines. A missing file or invalid offset is returned to the model as a rejected result.
- Nothing is deleted automatically; call `Prune` yourself.

## See Also

- [Tool Middleware guide, Dispatcher Pipeline](../../docs/guides/TOOL_MIDDLEWARE.md#dispatcher-pipeline)
- [Harness migration](../../docs/guides/HARNESS_MIGRATION.md)
