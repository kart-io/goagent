# logging: Structured Logger Interface

[English](README.md) | [中文](README_CN.md)

`logging.Logger` is the structured logger goagent writes through. Its method set equals the context-aware methods of `*slog.Logger`, so a `*slog.Logger`, or any logger that embeds one such as `*slogx.Logger` from [kart-io/slogx](https://github.com/kart-io/slogx), is passed in directly without an adapter. Library code never configures output itself: when no logger is given it writes through `slog.Default()`, which the host configures. Reach for this package when your own component accepts an optional logger and should follow the same rules.

## Usage

```go
import (
    "context"
    "log/slog"
    "os"

    "github.com/kart-io/goagent/core/logging"
)

type Worker struct {
    log logging.Logger
}

func NewWorker(l logging.Logger) *Worker {
    // nil falls back to slog.Default()
    return &Worker{log: logging.OrDefault(l)}
}

func (w *Worker) Run(ctx context.Context) {
    w.log.InfoContext(ctx, "worker started", "id", 42)
}

func main() {
    ctx := context.Background()
    NewWorker(slog.New(slog.NewJSONHandler(os.Stdout, nil))).Run(ctx) // explicit *slog.Logger
    NewWorker(nil).Run(ctx)                                            // slog.Default()
    NewWorker(logging.Discard()).Run(ctx)                              // drop everything
}
```

## Key API

| Identifier | Description |
|---|---|
| `Logger` | Interface with `DebugContext`, `InfoContext`, `WarnContext`, `ErrorContext`; `args` are alternating keys and values or `slog.Attr`, as in slog |
| `Default()` | Logger that writes through `slog.Default()` at the time of each call |
| `OrDefault(l)` | Returns `l`, or `Default()` when `l` is nil |
| `Discard()` | Logger that drops every record |

## Behavior Notes

- `Default()` reads `slog.Default()` on every call, so a later `slog.SetDefault` (or `slogx.Init`) also redirects loggers obtained earlier.
- `Default()` checks `Handler.Enabled` before building a record and records the caller's PC, so the handler's source/caller points at the calling code, not this package.
- A nil `ctx` passed to the `Default()` logger is replaced with `context.Background()`.
- `Logger` implementations must be safe for concurrent use.

## See Also

- [Host Integration: Logging](../../docs/guides/HOST_INTEGRATION.md#logging): wiring `slogx.Init`, `WithLogger`, and which goagent components fall back to `logging.Default()`
