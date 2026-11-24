# tools/spill 超大工具结果落盘

[English](README.md) | [中文](README_CN.md)

把超大的工具结果写入宿主提供的目录，并把交给模型的结果替换为预览与文件名；模型需要时用 `read_spill` 工具分段读取全文。工具结果可能大到不宜放进对话时使用它。该目录在工作区之外，不受工作区只读模式与变更跟踪影响。

## 用法

```go
import (
    "time"

    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/spill"
)

store, err := spill.New(spillDir, 32*1024) // 超过 32 KiB 的结果落盘
if err != nil {
    return err
}
if err := registry.Register(store.ReadTool()); err != nil {
    return err
}
dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    Pipeline: &tools.Pipeline{Post: []tools.PostHook{store.Hook()}},
})

// 删除一天前的落盘文件
err = store.Prune(24 * time.Hour)
```

需要自定预览与读取上限时用 `NewWithConfig`：

```go
store, err := spill.NewWithConfig(spillDir, spill.Config{
    MaxBytes:       64 * 1024,
    PreviewRunes:   500,
    MaxLineRunes:   1000,
    ReadLimitLines: 200,
})
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `New(dir, maxBytes)` | 使用预设预览与读取上限创建存储 |
| `NewWithConfig(dir, cfg)` | 使用显式上限创建存储 |
| `Config` | `MaxBytes`（必填）、`PreviewRunes`、`MaxLineRunes`、`ReadLimitLines` |
| `(*Store).Hook()` | `tools.PostHook`，对任何工具的成功超大结果落盘 |
| `(*Store).ReadTool()` | `read_spill` 工具，只能读本目录内的文件 |
| `(*Store).Prune(olderThan)` | 删除修改时间早于 `olderThan` 之前的落盘文件 |
| `(*Store).Dir()` | 返回落盘目录 |

## 行为说明

- `New` 与 `NewWithConfig` 在目录不存在时创建它（权限 `0700`）。`MaxBytes` 必须为正，其余上限不得为负，否则返回 `errors.CodeInvalidConfig` 错误。
- `New` 使用 `constants.SpillPreviewRunes`、`constants.SpillMaxLineRunes` 与 `constants.DefaultSpillReadLimitLines`。`Config` 中 `PreviewRunes`、`MaxLineRunes`、`ReadLimitLines` 为 0 表示不截断（读到末尾）。
- 只处理 `Success == true` 的结果。结果的文本形式超过 `MaxBytes` 时落盘：字符串原样写出，map 按键分成 `== key ==` 小节，其他值写成缩进 JSON。
- 替换后的结果含 `truncated`、`bytes`、`preview`、`note`，写入成功时还有 `spill_path`。写入失败时钩子仍返回带原因的预览且不返回错误，避免已产生副作用的工具因落盘失败而重跑。
- 文件名为 `<session>/<tool>-<call ID>.txt`；会话缺省为 `default`，调用没有 ID 时使用随机 ID。
- `read_spill` 接受 `path`、`offset`（从 1 开始）与 `limit`，返回带行号的内容。文件不存在或 offset 越界时以拒绝结果返回模型。
- 不会自动删除文件，需自行调用 `Prune`。

## 另见

- [工具中间件指南：Dispatcher 管线](../../docs/guides/TOOL_MIDDLEWARE_CN.md#dispatcher-管线)
- [Harness 迁移](../../docs/guides/HARNESS_MIGRATION_CN.md)
