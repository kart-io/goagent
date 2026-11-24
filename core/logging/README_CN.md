# logging 结构化日志接口

[English](README.md) | [中文](README_CN.md)

`logging.Logger` 是 goagent 写日志所用的结构化日志接口，方法集与 `*slog.Logger` 的带 ctx 方法一致，因此 `*slog.Logger`，或嵌入它的日志器（如 [kart-io/slogx](https://github.com/kart-io/slogx) 的 `*slogx.Logger`）可直接传入，无需适配器。库代码自身从不配置输出：没有传入日志器时写入由宿主配置的 `slog.Default()`。自己的组件需要接收一个可选日志器、并遵循同样规则时使用本包。

## 用法

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
    // nil 时回退到 slog.Default()
    return &Worker{log: logging.OrDefault(l)}
}

func (w *Worker) Run(ctx context.Context) {
    w.log.InfoContext(ctx, "worker started", "id", 42)
}

func main() {
    ctx := context.Background()
    NewWorker(slog.New(slog.NewJSONHandler(os.Stdout, nil))).Run(ctx) // 显式传入 *slog.Logger
    NewWorker(nil).Run(ctx)                                            // 使用 slog.Default()
    NewWorker(logging.Discard()).Run(ctx)                              // 丢弃全部记录
}
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Logger` | 含 `DebugContext`、`InfoContext`、`WarnContext`、`ErrorContext` 的接口；`args` 与 slog 相同，为交替的键值或 `slog.Attr` |
| `Default()` | 每次调用时经 `slog.Default()` 写出的日志器 |
| `OrDefault(l)` | 返回 `l`，`l` 为 nil 时返回 `Default()` |
| `Discard()` | 丢弃所有记录的日志器 |

## 行为说明

- `Default()` 每次调用都读取 `slog.Default()`，因此之后的 `slog.SetDefault`（或 `slogx.Init`）也会改变先前取得的日志器的去向。
- `Default()` 在构造记录前先检查 `Handler.Enabled`，并记录调用方的 PC，处理器的 source/caller 指向调用代码而非本包。
- 传给 `Default()` 日志器的 nil `ctx` 会被替换为 `context.Background()`。
- `Logger` 的实现必须可并发安全使用。

## 另见

- [宿主集成：日志](../../docs/guides/HOST_INTEGRATION_CN.md)：`slogx.Init`、`WithLogger` 的接入方式，以及哪些 goagent 组件会回退到 `logging.Default()`
