# core/session: Session Event Log

[English](README.md) | [中文](README_CN.md)

An append-only session log: the conversation the model sees, turn boundaries, and snapshots of durable operations are all recorded in the same log. For the design and trade-offs see [design document sections 6 and 16](../../docs/design/HARNESS_CORE_DESIGN.md).

```go
store, _ := session.NewFileStore(dir) // or session.NewMemoryStore()

agent, _ := builder.NewSimpleBuilder(client).
    WithSessionID(sessionID).
    WithSessionLog(store). // history is derived from the log; memory is no longer read
    WithTools(tools...).
    Build()

// Durable run: durable operation snapshots are written to the same log
ctx = durable.WithJournal(durable.WithExecutionKey(ctx, runKey), session.DurableStore(store, sessionID))
```

## Events

Event identifiers all use `SessionEvent*` from `github.com/kart-io/goagent/constants`, for example `constants.SessionEventUserMessage` and `constants.SessionEventBoundarySnapshot`; the former `session.Type*` entry points have been removed. Event structures such as `session.Event` and `MessageData`, and the log behavior, remain in this package.

| Type | Content |
|---|---|
| `turn/start`, `turn/end` | A turn's input, answer, and usage; when the Builder's `WithSessionState` is enabled, `turn/end` also carries the state (`session.LastState` takes the last one not undone); an interrupted turn is recorded as `aborted` when the next turn starts |
| `system/message`, `tool/definitions` | System prompts and tool definitions that differ from the previous ones; not part of the conversation, used for export |
| `user/message`, `assistant/message`, `tool/result` | One conversation message; `tool/result` carries the tool name and whether it succeeded |
| `history/rewrite` | A rewrite of existing history (compression, context added by hooks, etc.); `Surface` identifies the replaced range |
| `session/rewind` | Rewind: undoes all events within the range |
| `boundary/snapshot` | One snapshot write from `core/durable`, addressed by execution key |

`DeriveMessages(events)` is the single source of model history: it takes messages in order, `session/rewind` and aborted turns undo the events within their ranges, and `history/rewrite` replaces messages over the remaining events.

## How Recording Works

Before each model request, the Builder compares the conversation actually sent (excluding the system prompt and run context) with the result derived from the log: new messages are recorded one by one, and history rewritten by PreStep, compression, and so on is recorded as a single `history/rewrite`. Therefore every request can be rebuilt from `system + DeriveMessages + run context`, and recording is independent of the specific rewrite method.

- When the previous turn has not ended: if a durable run resumes with the same execution key, it continues that turn (replayed messages match those already recorded and are not duplicated); otherwise that turn is recorded as aborted and its messages do not enter history.
- Only one Agent should write messages to a session at a time; conversation appended by other writers makes the write return `errors.ErrSessionConflict`. Durable operation snapshots may be written interleaved with it.
- Both plain streaming chat (`llm.Client`) and the native tool loop are supported; tool-less chat also supports history, durable replay, and Fork.

When a durable run replays the last completed turn with the same execution key and the original input, the Builder uses the history before that turn, appends no new turn or duplicate messages, and re-runs final output validation. Completed replay enables `durable.WithReplayOnly`: `Do` accepts only completed records whose intent and policy match, creates no new operations, and does not execute `run` or `Reconcile`; replay is rejected when the Agent scope changes, records are missing, or an incomplete record is encountered. Authorization and other host callbacks must still ensure on their own that they are read-only and idempotent.

New input, later turns, and sessions that were aborted or rewound use a new execution key. An old execution key cannot reactivate effects that were rewound.

## Storage

`Store` provides `Append` (compare-and-swap on the expected sequence number), `Read`, `List`, `Fork`, and `Lock`:

- `FileStore`: one JSONL file per session, each line holding all events of one `Append`; an incomplete line left by a crash during writing is ignored on read and truncated before the next append; appends are done inside a session-level flock and fsynced.
- `MemoryStore`: in-process implementation, for tests.
- PostgreSQL: [`durable/pgstore` in goagent-contrib](https://github.com/kart-io/goagent-contrib/tree/master/durable/pgstore) provides a `SessionStore` whose lock is a lease, and the holder is verified when appending snapshots.

When implementing your own `Store`, verify the contract with `sessiontest.Run`, passing two instances on the same backend to simulate two processes; backends that implement `Lock` with leases additionally run the takeover and write-isolation cases via `Options.Leases`.

Events have independent ownership: after `Append/AppendAll` returns, the caller may reuse the input `Data` and `Surface`; modifying the values returned by `Read` does not change the log or the source and branch of a `Fork`. Events returned by `Recorder.Append`, `OpenRecorder`, and `Recorder.Events` are likewise isolated from the recorder's internal state; modifying an existing return value does not constitute a log update. Custom Stores should keep the same conventions so that in-memory and file backends cannot produce different recovery results.

When a file is first created, exiting before the full header line and newline are written leaves an empty file or a partial first line; the next non-empty append rebuilds the full header, syncs the file and directory, and only then returns success. A complete but invalid header is still refused for both reads and appends, so corrupted data is never overwritten as an empty session.

`Rewind(ctx, store, id, to, expected)` does a compare-and-swap on the last sequence number the caller saw, and returns `errors.ErrSessionConflict` if there were new writes in between. Undone events remain in the log; durable operation snapshots are not affected. `Fork` copies a prefix of a session into a new session.

`Deleter` is an optional capability: `Delete(ctx, id)` removes a session and all of its events and succeeds when the session does not exist; sessions forked from it are independent copies and stay. `MemoryStore` and `FileStore` both implement it (`FileStore` keeps the lock file); the caller must make sure nothing still appends to the session. `sessiontest.Run` also checks a custom Store that implements `Deleter`.

`Validate(events)` checks a complete journal (as returned by `Read(ctx, id, 0)`) and returns the first violation wrapped in `errors.ErrSessionInvalidLog`: sequence numbers start at 1 and are consecutive; known event types decode and message roles match the event type; `turn/start` and `turn/end` alternate, and an aborted `turn/end` undoes exactly its own turn; `Surface` appears only on `history/rewrite`, `session/rewind` and aborted `turn/end` and covers earlier events only; an aborted `turn/end` covers every later message of its turn and a rewind ends right before itself. Unknown event types are accepted without a `Surface`. A prefix that ends inside a turn is valid. The Builder does not call it on every turn: its own writes are covered by tests, and a failed check on an existing log would leave the session unusable with no repair path. Hosts that hand-write or import events should call it at that entry point, as `management.ForkSession` does before copying a prefix; `sessiontest.Run` checks every log it reads back from a custom Store.

To fork from a point in a durable run and continue: after `store.Fork(ctx, src, dst, seq)`, resume on `dst` with the same execution key (`WithSessionID(dst)`, `session.DurableStore(store, dst)`). Operations completed before the fork point are replayed and those after it run for real; the original session is unchanged. The fork point should be on a message event (such as the sequence number of a `tool/result`); if it falls between the start and completion of an operation, resuming that operation returns an error the host must handle, just as if the process had crashed there.

## Trajectory Export

```go
traj, _ := session.BuildTrajectory(events)
traj.WriteOpenAI(w)   // {"messages": [...], "tools": [...]}, OpenAI fine-tuning format
traj.WriteShareGPT(w) // {"conversations": [...], "system": "...", "tools": "..."}, LLaMA-Factory sharegpt format
traj.Stats            // call count and failure count per tool
```

What is exported is the conversation the model finally saw (after compression and rewinds). ShareGPT requires the user side and model side to alternate: multiple tool calls or results in the same turn are merged into a JSON array, and a user message immediately following tool results (input appended during the run) is attached after the observation.

## Testing

```bash
go test ./core/session/... ./builder/ -run 'Session|Store|Derive|Export|Recorder|Conformance'
```
