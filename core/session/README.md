# core/session — 会话事件日志

追加写的会话日志：模型看到的对话、轮次边界与持久操作的快照都记在同一份日志中。设计与取舍见 [设计文档第 6、16 节](../../docs/design/HARNESS_CORE_DESIGN.md)。

```go
store, _ := session.NewFileStore(dir) // 或 session.NewMemoryStore()

agent, _ := builder.NewSimpleBuilder(client).
    WithSessionID(sessionID).
    WithSessionLog(store). // 历史从日志推导，不再读取记忆
    WithTools(tools...).
    Build()

// 持久运行：durable 的操作快照写入同一份日志
ctx = durable.WithJournal(durable.WithExecutionKey(ctx, runKey), session.DurableStore(store, sessionID))
```

## 事件

| 类型 | 内容 |
|---|---|
| `turn/start`、`turn/end` | 一轮的输入、答复与用量；中断的轮次在下一轮开始时记为 `aborted` |
| `system/message`、`tool/definitions` | 与上次不同的 system 提示词与工具定义，不进入对话，供导出 |
| `user/message`、`assistant/message`、`tool/result` | 一条对话消息；`tool/result` 带工具名与是否成功 |
| `history/rewrite` | 对已有历史的改写（压缩、钩子补充的上下文等），`Surface` 指出被替换的区间 |
| `session/rewind` | 退回：撤销区间内的全部事件 |
| `boundary/snapshot` | `core/durable` 的一次快照写入，按执行键寻址 |

`DeriveMessages(events)` 是模型历史的唯一来源：按顺序取消息，`session/rewind` 与中止的轮次撤销其区间内的事件，`history/rewrite` 在其余事件上替换消息。

## 记录方式

Builder 在每次模型请求前把实际发送的对话（不含 system 与运行上下文）与日志推导的结果比较：新增的消息逐条记录，被 PreStep、压缩等改写的历史记为一条 `history/rewrite`。因此每次请求都能由 `system + DeriveMessages + 运行上下文` 重建，记录与具体的改写方式无关。

- 上一轮未结束时：持久运行以同一执行键恢复则续写该轮（回放的消息与已记录的一致，不会重复），否则该轮记为中止，其消息不进入历史。
- 同一会话同时只应有一个 Agent 写入消息；其他写入者追加的对话使写入返回 `ErrConflict`。持久操作的快照可与之交错写入。
- 只支持原生工具流（`llm.ToolStreamClient` 且配置了工具）。

## 存储

`Store` 提供 `Append`（按期望序号做比较并交换）、`Read`、`List`、`Fork`、`Lock`：

- `FileStore`：每个会话一个 JSONL 文件，每行是一次 `Append` 的全部事件，写入中崩溃留下的不完整行在读取时忽略、下次追加前截掉；追加在会话级 flock 内完成并 fsync。
- `MemoryStore`：进程内实现，用于测试。

`Rewind(ctx, store, id, to, expected)` 以调用方看到的最后序号做比较并交换，其间有新写入时返回 `ErrConflict`。被撤销的事件仍保留在日志中；持久操作的快照不受影响。`Fork` 把会话的前缀复制为新会话。

## 轨迹导出

```go
traj, _ := session.BuildTrajectory(events)
traj.WriteOpenAI(w)   // {"messages": [...], "tools": [...]}，OpenAI 微调格式
traj.WriteShareGPT(w) // {"conversations": [...], "system": "...", "tools": "..."}，LLaMA-Factory 的 sharegpt 格式
traj.Stats            // 每个工具的调用次数与失败次数
```

导出的是模型最终看到的对话（压缩与退回之后）。ShareGPT 要求用户侧与模型侧交替：同一回合的多个工具调用或结果合并为 JSON 数组，工具结果之后紧接的用户消息（运行中追加的输入）附在 observation 之后。

## 测试

```bash
go test ./core/session/ ./builder/ -run 'Session|Store|Derive|Export|Recorder'
```
