# 动态中断实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让工作流节点（含并行分支、嵌套子图）和显式开启的工具在执行中途暂停，等人提供输入后从检查点恢复。

**Architecture:** 在 `durable.Do` 上实现原语 `durable.AwaitInput`，人工输入作为 completed 值写入操作日志，首次成功持久化后固定。workflow 把节点收到的中断等待记为图检查点上的 `Interrupts`，恢复时注入输入并重新执行节点。工具通过可选接口 `tools.InterruptibleTool` 声明可中断，Dispatcher 为它使用 `{Resumable: true, MaxAttempts: 1}`。

**Tech Stack:** Go 1.26、`github.com/stretchr/testify/require`、`durable.FileStore`。

**Spec:** `docs/superpowers/specs/2026-11-04-dynamic-interrupt-design.md`

## Global Constraints

- 工作目录：`/home/hellotalk/code/go/src/github.com/kart-io/goagent-wt-interrupt`，分支 `feat/dynamic-interrupt`。
- 注释、错误提示、文档、提交信息使用简体中文；标识符沿用项目英文命名。
- 主模块不新增第三方依赖。
- 原语拒绝任何输入时都返回带 `Rejected` 的 `*durable.WaitError`，绝不返回普通错误。
- 中断键只用于定位，不是授权凭证；框架不做授权判断。
- `info` 必须是确定性的 JSON。
- 每个任务结束时 `go test -race` 覆盖本任务涉及的包，并且 `golangci-lint run` 无新增问题。
- 全部验证在本地执行。

## 与 spec 的差异（Task 6 同步修改 spec）

- 工具开关是 `FunctionToolBuilder.WithInterruptible(version string)`，不是 `WithInterruptible()`。原因：Dispatcher 对实现了 `DurableIntentVersioner` 却返回空版本的工具直接报错（`tools/dispatcher.go:156-158`），FunctionTool 不能无条件实现它。开启中断必须声明版本，这也让 FunctionTool 的策略迁移可以被检测：版本变化会改变 intent 哈希，旧记录报 "operation intent or attempt policy changed"。“只改 Resumable 不报错”的现状只对自行实现 `DurablePolicyProvider` 的工具成立，用 durable 层的测试固定。
- 离线示例用工作流节点中断演示，不用工具中断：工具中断需要模型的原生工具调用，离线示例无法提供。工具中断由 builder 测试覆盖。
- 恢复期间检查点保持中断等待，不在重新执行节点前清除。节点完成后正常推进时自然清除；重新执行中途崩溃，检查点仍是等待状态，可以按“已回应”规则恢复。

## 文件结构

| 文件 | 职责 |
|---|---|
| `core/durable/interrupt.go`（新） | `AwaitInput`、`AllowInterrupt`、`InterruptAnswer`、`InterruptContinuation`、`ErrInterruptNotDurable` |
| `core/durable/resume.go` | 新增 `ResumeInputFor` |
| `core/durable/operation.go` | `Do` 为子操作清除 `AllowInterrupt` 标记 |
| `core/durable/interrupt_test.go`（新） | 原语测试 |
| `workflow/interrupt.go`（新） | `InterruptPoint`、`interruptPoints`、`prepareInterruptResume`、`sameJSON`、`ErrInterruptAnswered` |
| `workflow/durable.go` | 状态字段、校验规则、节点暂停、恢复分支 |
| `workflow/durable_parallel.go` | 分支等待不取消兄弟分支，汇总等待 |
| `workflow/durable_runnable.go` | `ChildContinuation.Interrupts` |
| `workflow/interrupt_test.go`（新） | 节点、并行、嵌套、崩溃、损坏检测测试 |
| `tools/function_tool.go` | `WithInterruptible(version)`、`InterruptibleVersion()` |
| `tools/dispatcher.go` | `InterruptibleTool` 接口与策略 |
| `tools/interruptible_test.go`（新） | Dispatcher 策略与迁移测试 |
| `builder/interrupt_tool_test.go`（新） | Agent 原生工具循环中的暂停与恢复 |
| `docs/guides/HUMAN_IN_THE_LOOP.md`、`CHANGELOG.md`、`examples/human-in-the-loop-interrupt/`（新）、spec | 文档与示例 |

---

### Task 1: durable 原语

**Files:**
- Create: `core/durable/interrupt.go`
- Modify: `core/durable/resume.go`
- Modify: `core/durable/operation.go:247-249`（`Do` 中设置 `waitableContext` 的那一行之后）
- Test: `core/durable/interrupt_test.go`

**Interfaces:**
- Produces:
  - `func AwaitInput(ctx context.Context, id string, info json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error)`
  - `func AllowInterrupt(ctx context.Context) context.Context`
  - `func InterruptAnswer(ctx context.Context, store Store, key, id string) (json.RawMessage, bool, error)`
  - `func ResumeInputFor(ctx context.Context, key string) (string, bool)`
  - `type InterruptContinuation struct { Kind, ID string; Info json.RawMessage; Rejected bool; Reason string }`（JSON 标签 `kind`、`id`、`info`、`rejected,omitempty`、`reason,omitempty`）
  - `const ContinuationKindInterrupt = "interrupt"`、`const ReasonInvalidJSON = "invalid_json"`
  - `var ErrInterruptNotDurable`
  - 中断记录 ID 为 `"interrupt/" + id`，中断键为 `ChildKey(ExecutionKey(ctx), "interrupt/"+id)`

- [ ] **Step 1: 写失败测试**

`core/durable/interrupt_test.go`：

```go
package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var amountInfo = json.RawMessage(`{"question":"金额"}`)

func positive(raw json.RawMessage) error {
	var n int
	if json.Unmarshal(raw, &n) != nil || n <= 0 {
		return errors.New("需要正整数")
	}
	return nil
}

func interruptContext(t *testing.T) context.Context {
	t.Helper()
	return AllowInterrupt(operationContext(t))
}

func interruptKey(id string) string { return ChildKey("node-key", "interrupt/"+id) }

func requireInterruptWait(t *testing.T, err error) InterruptContinuation {
	t.Helper()
	waits := Waits(err)
	require.Len(t, waits, 1, "%v", err)
	require.Equal(t, interruptKey("amount"), waits[0].Path[len(waits[0].Path)-1].Key)
	var c InterruptContinuation
	require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
	require.Equal(t, ContinuationKindInterrupt, c.Kind)
	return c
}

func TestAwaitInputWaitsThenFixesFirstAnswer(t *testing.T) {
	ctx := interruptContext(t)
	_, err := AwaitInput(ctx, "amount", amountInfo, positive)
	c := requireInterruptWait(t, err)
	require.Equal(t, "amount", c.ID)
	require.JSONEq(t, string(amountInfo), string(c.Info))
	require.False(t, c.Rejected)

	got, err := AwaitInput(WithResume(ctx, interruptKey("amount"), `5`), "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `5`, string(got))

	// 已固定的输入只回放：不带输入、或带不同输入都返回第一次的值。
	got, err = AwaitInput(ctx, "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `5`, string(got))
	got, err = AwaitInput(WithResume(ctx, interruptKey("amount"), `9`), "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `5`, string(got))

	answer, ok, err := InterruptAnswer(ctx, JournalStore(ctx), interruptKey("amount"), "amount")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `5`, string(answer))
}

func TestAwaitInputRejectionsStayWaiting(t *testing.T) {
	for _, tc := range []struct {
		name, input, reason string
		validate            func(json.RawMessage) error
	}{
		{name: "空字符串", input: "", reason: ReasonInvalidJSON, validate: positive},
		{name: "非法 JSON", input: "{", reason: ReasonInvalidJSON, validate: positive},
		{name: "校验失败", input: "-1", reason: "需要正整数", validate: positive},
		{name: "null 被校验拒绝", input: "null", reason: "需要正整数", validate: positive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := interruptContext(t)
			_, err := AwaitInput(WithResume(ctx, interruptKey("amount"), tc.input), "amount", amountInfo, tc.validate)
			c := requireInterruptWait(t, err)
			require.True(t, c.Rejected)
			require.Equal(t, tc.reason, c.Reason)
			_, ok, err := InterruptAnswer(ctx, JournalStore(ctx), interruptKey("amount"), "amount")
			require.NoError(t, err)
			require.False(t, ok)
			got, err := AwaitInput(WithResume(ctx, interruptKey("amount"), `2`), "amount", amountInfo, tc.validate)
			require.NoError(t, err)
			require.JSONEq(t, `2`, string(got))
		})
	}
}

func TestAwaitInputAcceptsNullWithoutValidator(t *testing.T) {
	ctx := interruptContext(t)
	got, err := AwaitInput(WithResume(ctx, interruptKey("amount"), `null`), "amount", amountInfo, nil)
	require.NoError(t, err)
	require.Equal(t, "null", string(got))
}

func TestAwaitInputRequiresJournalAndWaitableContext(t *testing.T) {
	_, err := AwaitInput(AllowInterrupt(context.Background()), "amount", amountInfo, nil)
	require.ErrorIs(t, err, ErrInterruptNotDurable)

	ctx := operationContext(t)
	_, err = AwaitInput(ctx, "amount", amountInfo, nil)
	require.ErrorIs(t, err, ErrWaitNotResumable)
	_, err = InspectOperation(ctx, "interrupt/amount")
	require.ErrorIs(t, err, os.ErrNotExist, "不可等待时不能写记录")

	// Do 为子操作清除 AllowInterrupt 标记：不可续跑的操作里不能暂停。
	host := AllowInterrupt(ctx)
	_, err = Do(host, "plain", json.RawMessage(`{}`), OperationPolicy{}, func(ctx context.Context) (json.RawMessage, error) {
		_, e := AwaitInput(ctx, "amount", amountInfo, nil)
		return nil, e
	})
	require.ErrorIs(t, err, ErrWaitNotResumable)
	_, err = InspectOperation(WithExecutionKey(host, ChildKey("node-key", "plain")), "interrupt/amount")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAwaitInputInsideResumableOperation(t *testing.T) {
	ctx := operationContext(t)
	run := func(ctx context.Context) (json.RawMessage, error) {
		return AwaitInput(ctx, "amount", amountInfo, positive)
	}
	policy := OperationPolicy{Resumable: true, MaxAttempts: 1}
	_, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, run)
	waits := Waits(err)
	require.Len(t, waits, 1)
	key := waits[0].Path[len(waits[0].Path)-1].Key
	require.Equal(t, ChildKey(ChildKey("node-key", "tool/c1"), "interrupt/amount"), key)
	record, err := Do(WithResume(ctx, key, `3`), "tool/c1", json.RawMessage(`{}`), policy, run)
	require.NoError(t, err)
	require.JSONEq(t, `3`, string(record.Value))
}

func TestAwaitInputReentersAfterCrashBeforeWaitingSaved(t *testing.T) {
	ctx := interruptContext(t)
	key := interruptKey("amount")
	pending := &OperationRecord{ID: "interrupt/amount", Key: key, InputHash: digest(string(amountInfo)), Status: "pending",
		Attempts: 1, MaxAttempts: interruptAttempts, Value: json.RawMessage(`null`), FailureKind: "ambiguous"}
	require.NoError(t, saveOperation(ctx, JournalStore(ctx), operationStorageID(key), pending))
	got, err := AwaitInput(WithResume(ctx, key, `7`), "amount", amountInfo, positive)
	require.NoError(t, err)
	require.JSONEq(t, `7`, string(got))
}

func TestAwaitInputInfoChangeIsIntentChange(t *testing.T) {
	ctx := interruptContext(t)
	_, err := AwaitInput(ctx, "amount", amountInfo, nil)
	requireInterruptWait(t, err)
	_, err = AwaitInput(ctx, "amount", json.RawMessage(`{"question":"其他"}`), nil)
	require.ErrorContains(t, err, "intent")
}

// 只改 Resumable 不会被 Do 检测：记录只比较输入哈希与 MaxAttempts。迁移须提升 intent 版本。
func TestDoDoesNotDetectResumableChange(t *testing.T) {
	ctx := operationContext(t)
	calls := 0
	run := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`1`), nil }
	_, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 1}, run)
	require.NoError(t, err)
	record, err := Do(ctx, "tool/c1", json.RawMessage(`{}`), OperationPolicy{MaxAttempts: 1, Resumable: true}, run)
	require.NoError(t, err)
	require.True(t, record.Replayed)
	require.Equal(t, 1, calls)
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./core/durable/ -run 'AwaitInput|DoDoesNotDetect' -count=1`
Expected: 编译失败，提示 `undefined: AllowInterrupt`、`AwaitInput` 等。

- [ ] **Step 3: 实现**

`core/durable/resume.go`：把 `ResumeInput` 改为基于 `ResumeInputFor`：

```go
// ResumeInput 返回宿主为当前执行键提供的续跑输入。
func ResumeInput(ctx context.Context) (string, bool) {
	return ResumeInputFor(ctx, ExecutionKey(ctx))
}

// ResumeInputFor 返回宿主为 key 提供的续跑输入，供承载等待的上层判断是否需要重新进入。
func ResumeInputFor(ctx context.Context, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	inputs, _ := ctx.Value(resumeContext{}).(map[string]string)
	input, ok := inputs[key]
	return input, ok
}
```

`core/durable/operation.go`：在 `Do` 中 `childCtx = context.WithValue(childCtx, waitableContext{}, ...)` 这一行之后加一行：

```go
	childCtx = context.WithValue(childCtx, interruptHostContext{}, false)
```

`core/durable/interrupt.go`：

```go
package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ContinuationKindInterrupt 标识 WaitError.Continuation 是 InterruptContinuation。
const ContinuationKindInterrupt = "interrupt"

// ReasonInvalidJSON 是输入不是合法 JSON 时的拒绝原因。
const ReasonInvalidJSON = "invalid_json"

const (
	interruptPrefix = "interrupt/"
	// interruptAttempts 允许在写 pending 与写 waiting 之间崩溃后重入；中断本身没有副作用。
	interruptAttempts = 3
	maxReasonRunes    = 200
)

// ErrInterruptNotDurable 表示 ctx 没有持久日志，暂停后无法恢复。
var ErrInterruptNotDurable = errors.New("暂停需要持久日志")

// InterruptContinuation 是 AwaitInput 等待时放进 WaitError.Continuation 的内容。
// Info 会经快照暴露给宿主，不得包含凭证。
type InterruptContinuation struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Info     json.RawMessage `json:"info"`
	Rejected bool            `json:"rejected,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

type interruptHostContext struct{}

// AllowInterrupt 标记 ctx 可以直接承载 AwaitInput 的等待，如工作流节点。承载方必须把收到的
// 等待当作暂停，并在恢复时重新进入；Do 会为子操作清除此标记。
func AllowInterrupt(ctx context.Context) context.Context {
	return context.WithValue(ctx, interruptHostContext{}, true)
}

func interruptHost(ctx context.Context) bool {
	ok, _ := ctx.Value(interruptHostContext{}).(bool)
	return ok
}

// AwaitInput 在当前持久操作中暂停，等待宿主回应。首次成功持久化的输入就此固定，之后的重放都返回它；
// 这不保证调用前的业务副作用只执行一次。id 在同一父执行键下必须唯一，循环中须带序号。
// 输入不是合法 JSON 或 validate 失败时保持等待，并在 Continuation 中标记 Rejected。
// JSON null 是合法的显式输入；需要非空输入时由 validate 拒绝。
func AwaitInput(ctx context.Context, id string, info json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error) {
	if id == "" || !json.Valid(info) {
		return nil, fmt.Errorf("中断需要 ID 与 JSON 格式的 info")
	}
	if !HasJournal(ctx) {
		return nil, ErrInterruptNotDurable
	}
	if !CanWait(ctx) && !interruptHost(ctx) {
		return nil, fmt.Errorf("%w：中断 %q 须在可续跑的操作或工作流节点内调用", ErrWaitNotResumable, id)
	}
	policy := OperationPolicy{Resumable: true, MaxAttempts: interruptAttempts}
	record, err := Do(ctx, interruptPrefix+id, info, policy, func(ctx context.Context) (json.RawMessage, error) {
		wait := func(reason string) error {
			data, e := json.Marshal(InterruptContinuation{Kind: ContinuationKindInterrupt, ID: id, Info: info, Rejected: reason != "", Reason: reason})
			if e != nil {
				return e
			}
			return &WaitError{Continuation: data}
		}
		raw, ok := ResumeInput(ctx)
		if !ok {
			return nil, wait("")
		}
		input := json.RawMessage(raw)
		if !json.Valid(input) {
			return nil, wait(ReasonInvalidJSON)
		}
		if validate != nil {
			if e := validate(input); e != nil {
				return nil, wait(rejectReason(e))
			}
		}
		return append(json.RawMessage(nil), input...), nil
	})
	if err != nil {
		return nil, err
	}
	return record.Value, nil
}

// rejectReason 截断校验错误；validate 由宿主提供，不含第三方错误原文。
func rejectReason(err error) string {
	r := []rune(err.Error())
	if len(r) > maxReasonRunes {
		r = r[:maxReasonRunes]
	}
	return string(r)
}

// InterruptAnswer 只读查询 key 处的中断是否已回应，已回应时返回固定的输入。
// key 是 WaitError.Path 最后一项的 Key，id 是 AwaitInput 的 id。
func InterruptAnswer(ctx context.Context, store Store, key, id string) (answer json.RawMessage, ok bool, err error) {
	if store == nil || key == "" || id == "" {
		return nil, false, fmt.Errorf("查询中断需要 Store、键和 ID")
	}
	storageID := operationStorageID(key)
	unlock, err := store.Lock(ctx, storageID)
	if err != nil {
		return nil, false, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	record, err := loadOperation(ctx, store, key, interruptPrefix+id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || record.Status != "completed" {
		return nil, false, err
	}
	return record.Value, true, nil
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test -race ./core/durable/ -count=1`
Expected: PASS（包括已有测试）。

- [ ] **Step 5: lint 与提交**

```bash
golangci-lint run ./core/durable/...
git add core/durable/interrupt.go core/durable/interrupt_test.go core/durable/resume.go core/durable/operation.go
git commit -m "feat(durable): 新增执行中途等待人工输入的 AwaitInput

节点与工具只能在声明的审批节点前暂停。AwaitInput 建在 Do 上，输入写入日志后固定，
重入只回放；任何拒绝都保持等待，避免记录写成 failed 后无法续跑。"
```

---

### Task 2: workflow 节点内暂停与恢复

**Files:**
- Create: `workflow/interrupt.go`
- Modify: `workflow/durable.go`（`GraphSnapshot`、`graphState`、`publicGraphSnapshot`、`run`、`validateContinuation`）
- Test: `workflow/interrupt_test.go`

**Interfaces:**
- Consumes: Task 1 的 `AwaitInput`、`AllowInterrupt`、`InterruptAnswer`、`ResumeInputFor`、`InterruptContinuation`、`ContinuationKindInterrupt`
- Produces:
  - `type InterruptPoint struct { Key, ID string; Info json.RawMessage; Rejected bool; Reason string }`（JSON 标签 `key`、`id`、`info`、`rejected,omitempty`、`reason,omitempty`）
  - `GraphSnapshot[T].Interrupts []InterruptPoint`
  - `var ErrInterruptAnswered`，以 `&ResumeRejectedError{Err: ErrInterruptAnswered}` 返回
  - 节点暂停事件类型 `interrupt_required`
  - `Resume`、`Run`、`RunRequest.Resume` 的 `Key` 可以是中断键

- [ ] **Step 1: 写失败测试**

`workflow/interrupt_test.go`：

```go
package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

var errDisk = errors.New("disk full")

// filterFailStore 让 fail 返回真的图检查点写入失败，操作记录照常保存。
type filterFailStore struct {
	durable.Store
	fail func(*durable.Snapshot) bool
}

func (s *filterFailStore) Save(ctx context.Context, id string, v *durable.Snapshot) error {
	if s.fail != nil && !strings.HasPrefix(id, "goagent/operation/") && s.fail(v) {
		return errDisk
	}
	return s.Store.Save(ctx, id, v)
}

func positiveInt(raw json.RawMessage) error {
	var n int
	if json.Unmarshal(raw, &n) != nil || n <= 0 {
		return errors.New("需要正整数")
	}
	return nil
}

// askNode 先执行一个记日志的准备步骤，再向人要一个正整数并加到状态上。
func askNode(prepared *atomic.Int32) workflow.Node[int] {
	return func(ctx context.Context, n int) (int, error) {
		if _, err := durable.Do(ctx, "prepare", json.RawMessage(`{}`), durable.OperationPolicy{}, func(context.Context) (json.RawMessage, error) {
			prepared.Add(1)
			return json.RawMessage(`true`), nil
		}); err != nil {
			return 0, err
		}
		raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"金额"}`), positiveInt)
		if err != nil {
			return 0, err
		}
		var amount int
		if err := json.Unmarshal(raw, &amount); err != nil {
			return 0, err
		}
		return n + amount, nil
	}
}

func compileAsk(t *testing.T, store durable.Store, prepared *atomic.Int32, opts ...workflow.DurableOption[int]) *workflow.DurableGraph[int] {
	t.Helper()
	cfg := workflow.DurableConfig{Store: store, Version: "ask-v1"}
	run, err := oneNode(t, askNode(prepared)).CompileDurable(context.Background(), cfg, opts...)
	require.NoError(t, err)
	return run
}

func requireInterrupted(t *testing.T, snap *workflow.GraphSnapshot[int], err error) workflow.InterruptPoint {
	t.Helper()
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.True(t, snap.Waiting)
	require.Len(t, snap.Interrupts, 1)
	require.Equal(t, "amount", snap.Interrupts[0].ID)
	require.JSONEq(t, `{"question":"金额"}`, string(snap.Interrupts[0].Info))
	return snap.Interrupts[0]
}

func TestNodeInterruptPausesAndResumesAfterRestart(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	snap, err := compileAsk(t, store, &prepared).Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)
	require.EqualValues(t, 1, prepared.Load())

	// 不带输入的 Run 不执行节点，原样返回等待。
	snap, err = compileAsk(t, store, &prepared).Run(ctx, "ask", 0, "")
	requireInterrupted(t, snap, err)

	// 重新编译模拟新进程。
	out, err := compileAsk(t, store, &prepared).Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.NoError(t, err)
	require.True(t, out.Done)
	require.Equal(t, 15, out.State)
	require.Empty(t, out.Interrupts)
	require.EqualValues(t, 1, prepared.Load(), "准备步骤只回放")

	_, err = compileAsk(t, store, &prepared).Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.ErrorIs(t, err, workflow.ErrApprovalMismatch)
}

func TestNodeInterruptRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	snap, err := run.Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)

	var rejected *workflow.ResumeRejectedError
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`{`)})
	require.ErrorAs(t, err, &rejected)
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key})
	require.ErrorAs(t, err, &rejected)
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: "forged", Input: json.RawMessage(`1`)})
	require.ErrorIs(t, err, workflow.ErrApprovalMismatch)

	snap, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`-1`)})
	require.ErrorAs(t, err, &rejected)
	require.True(t, snap.Waiting)
	require.Len(t, snap.Interrupts, 1)
	require.True(t, snap.Interrupts[0].Rejected)
	require.Equal(t, "需要正整数", snap.Interrupts[0].Reason)

	out, err := run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`2`)})
	require.NoError(t, err)
	require.Equal(t, 12, out.State)
}

func TestNodeInterruptAnswerLoggedBeforeCheckpointUpdate(t *testing.T) {
	ctx := context.Background()
	base := newStore(t)
	store := &filterFailStore{Store: base}
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	snap, err := run.Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)

	// 输入写入日志后，节点完成后的推进写入失败：检查点仍是中断等待。
	store.fail = func(v *durable.Snapshot) bool { return bytes.Contains(v.Data, []byte(`"routing":true`)) }
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.ErrorIs(t, err, errDisk)
	store.fail = nil
	snap, err = run.Snapshot(ctx, "ask")
	require.NoError(t, err)
	require.True(t, snap.Waiting)

	var rejected *workflow.ResumeRejectedError
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`7`)})
	require.ErrorAs(t, err, &rejected)
	require.ErrorIs(t, err, workflow.ErrInterruptAnswered)
	require.EqualValues(t, 1, prepared.Load(), "冲突输入不执行节点")

	out, err := run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(" 5 ")})
	require.NoError(t, err, "空白差异视为相同输入")
	require.Equal(t, 15, out.State)
}

func TestNodeInterruptRecoversWithoutKeyWhenAnswered(t *testing.T) {
	ctx := context.Background()
	base := newStore(t)
	store := &filterFailStore{Store: base}
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	snap, err := run.Run(ctx, "ask", 10, "")
	point := requireInterrupted(t, snap, err)
	store.fail = func(v *durable.Snapshot) bool { return bytes.Contains(v.Data, []byte(`"routing":true`)) }
	_, err = run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.ErrorIs(t, err, errDisk)
	store.fail = nil

	out, err := run.Run(ctx, "ask", 0, "")
	require.NoError(t, err)
	require.Equal(t, 15, out.State)
}

func TestInterruptAfterApprovalDoesNotApproveAgain(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared, checks atomic.Int32
	before := workflow.WithBeforeApprove(func(context.Context, string, *workflow.GraphSnapshot[int]) error { checks.Add(1); return nil })
	cfg := workflow.DurableConfig{Store: store, Version: "ask-v1", ApprovalNodes: []string{"effect"}}
	run, err := oneNode(t, askNode(&prepared)).CompileDurable(ctx, cfg, before)
	require.NoError(t, err)
	wait, err := run.Run(ctx, "ask", 10, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Empty(t, wait.Interrupts)

	snap, err := run.Run(ctx, "ask", 0, wait.Key)
	point := requireInterrupted(t, snap, err)
	require.EqualValues(t, 1, checks.Load())

	out, err := run.Resume(ctx, "ask", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`1`)})
	require.NoError(t, err)
	require.Equal(t, 11, out.State)
	require.EqualValues(t, 1, checks.Load(), "中断恢复不再走审批")
}

func TestInterruptCheckpointCorruptionIsDetected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*durable.Snapshot, map[string]any)
	}{
		{name: "审批与等待并存但没有中断", mutate: func(cp *durable.Snapshot, data map[string]any) {
			cp.Approved = true
			delete(data, "interrupts")
		}},
		{name: "路由位置带中断", mutate: func(_ *durable.Snapshot, data map[string]any) { data["routing"] = true }},
		{name: "中断缺少键", mutate: func(_ *durable.Snapshot, data map[string]any) {
			data["interrupts"] = []any{map[string]any{"id": "amount", "info": map[string]any{}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newStore(t)
			var prepared atomic.Int32
			run := compileAsk(t, store, &prepared)
			_, err := run.Run(ctx, "ask", 10, "")
			require.ErrorIs(t, err, durable.ErrApprovalRequired)
			cp, err := store.Load(ctx, "ask")
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, json.Unmarshal(cp.Data, &data))
			tc.mutate(cp, data)
			cp.Data, err = json.Marshal(data)
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, "ask", cp))
			_, err = run.Snapshot(ctx, "ask")
			require.ErrorContains(t, err, "corrupt")
		})
	}
}

func TestInterruptThroughRunnableEmitsEvent(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	run := compileAsk(t, store, &prepared)
	stream, err := run.AsRunnable().Stream(ctx, workflow.RunRequest[int]{RunID: "ask", Initial: 10})
	require.NoError(t, err)
	var sawEvent bool
	var final *workflow.GraphSnapshot[int]
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		if chunk.Event != nil && chunk.Event.Type == "interrupt_required" {
			sawEvent = true
		}
		if chunk.Done {
			final = chunk.Data
		}
	}
	require.True(t, sawEvent)
	require.NotNil(t, final)
	require.True(t, final.Waiting)
	require.Len(t, final.Interrupts, 1)

	out, err := run.AsRunnable().Invoke(ctx, workflow.RunRequest[int]{RunID: "ask",
		Resume: &workflow.ResumeRequest{Key: final.Interrupts[0].Key, Input: json.RawMessage(`4`)}})
	require.NoError(t, err)
	require.Equal(t, 14, out.State)
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./workflow/ -run 'Interrupt' -count=1`
Expected: 编译失败，提示 `snap.Interrupts undefined`、`workflow.InterruptPoint undefined`、`workflow.ErrInterruptAnswered undefined`。

- [ ] **Step 3: 实现 `workflow/interrupt.go`**

```go
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core/durable"
)

// ErrInterruptAnswered 表示该中断已经回应，新的输入与已固定的输入不同，被拒绝。
var ErrInterruptAnswered = errors.New("中断已回应")

// InterruptPoint 是一处等待人工输入的中断。Key 只用于 Resume 定位，不是授权凭证；
// 宿主必须先认证回应者并确认其有权回应。
type InterruptPoint struct {
	Key      string          `json:"key"`
	ID       string          `json:"id"`
	Info     json.RawMessage `json:"info"`
	Rejected bool            `json:"rejected,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

// interruptPoints 把节点收到的等待转换为按 Key 排序的中断列表；含非中断等待时返回 false，节点按失败处理。
func interruptPoints(waits []*durable.WaitError) ([]InterruptPoint, bool) {
	var points []InterruptPoint
	for _, w := range waits {
		var probe struct {
			Kind       string           `json:"kind"`
			Interrupts []InterruptPoint `json:"interrupts"`
		}
		if json.Unmarshal(w.Continuation, &probe) != nil {
			return nil, false
		}
		switch {
		case probe.Kind == durable.ContinuationKindInterrupt:
			var c durable.InterruptContinuation
			if json.Unmarshal(w.Continuation, &c) != nil || len(w.Path) == 0 {
				return nil, false
			}
			points = append(points, InterruptPoint{Key: w.Path[len(w.Path)-1].Key, ID: c.ID, Info: c.Info, Rejected: c.Rejected, Reason: c.Reason})
		case len(probe.Interrupts) > 0:
			points = append(points, probe.Interrupts...)
		default:
			return nil, false
		}
	}
	slices.SortFunc(points, func(a, b InterruptPoint) int { return strings.Compare(a.Key, b.Key) })
	return points, len(points) > 0
}

// prepareInterruptResume 决定中断等待的图是否重新执行节点，返回注入输入后的 ctx 与本次回应的键。
// 不需要执行时返回 durable.ErrApprovalRequired；拒绝时检查点不变。
func (d *DurableGraph[T]) prepareInterruptResume(ctx context.Context, store durable.Store, points []InterruptPoint, key string, input json.RawMessage) (context.Context, string, error) {
	if key == "" {
		if input != nil {
			return ctx, "", &ResumeRejectedError{Err: fmt.Errorf("人工输入需要中断键")}
		}
		for _, p := range points {
			if _, ok := durable.ResumeInputFor(ctx, p.Key); ok {
				return ctx, "", nil
			}
			_, answered, err := durable.InterruptAnswer(ctx, store, p.Key, p.ID)
			if err != nil {
				return ctx, "", err
			}
			if answered {
				return ctx, "", nil
			}
		}
		return ctx, "", durable.ErrApprovalRequired
	}
	i := slices.IndexFunc(points, func(p InterruptPoint) bool { return p.Key == key })
	if i < 0 {
		return ctx, "", ErrApprovalMismatch
	}
	recorded, answered, err := durable.InterruptAnswer(ctx, store, key, points[i].ID)
	if err != nil {
		return ctx, "", err
	}
	if answered {
		if input != nil && !sameJSON(input, recorded) {
			return ctx, "", &ResumeRejectedError{Err: ErrInterruptAnswered}
		}
		return ctx, "", nil
	}
	if input == nil {
		return ctx, "", &ResumeRejectedError{Err: fmt.Errorf("中断 %q 需要输入", points[i].ID)}
	}
	if !json.Valid(input) {
		return ctx, "", &ResumeRejectedError{Err: fmt.Errorf("中断输入必须是合法 JSON")}
	}
	return durable.WithResume(ctx, key, string(input)), key, nil
}

// sameJSON 按 json.Compact 后的字节比较：空白差异相同，键顺序或数字写法不同算不同。
func sameJSON(a, b json.RawMessage) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}
```

- [ ] **Step 4: 修改 `workflow/durable.go`**

4a. `GraphSnapshot[T]` 在 `Done bool` 后增加字段：

```go
	// Interrupts 是节点执行中途等待人工输入的中断，按 Key 排序；审批等待时为空。
	Interrupts []InterruptPoint
```

4b. `graphState[T]` 增加字段：

```go
	Interrupts []InterruptPoint `json:"interrupts,omitempty"`
```

4c. `publicGraphSnapshot` 返回值增加 `Interrupts: slices.Clone(state.Interrupts)`。

4d. `run` 中，把从 `if approvalKey != "" && (!checkpoint.Waiting || checkpoint.Key != approvalKey) {` 开始、到 `} else if approvalKey != "" && d.beforeApprove != nil { ... }` 结束的整段替换为下面的代码。`save` 闭包的函数体保持原样，只是移到最前面：

```go
	// Save using fresh snapshots; failed writes leave the last acknowledged state.
	save := func(next durable.Snapshot, nextState graphState[T]) error {
		data, e := json.Marshal(nextState)
		if e != nil {
			return e
		}
		next.Data = data
		if e = store.Save(ctx, id, &next); e != nil {
			return e
		}
		checkpoint = &next
		state = nextState
		result = publicGraphSnapshot(checkpoint, state)
		return nil
	}
	// 中断等待恢复期间检查点保持等待：重新执行中途崩溃时仍可按“已回应”规则恢复。
	answered := ""
	if len(state.Interrupts) > 0 {
		if ctx, answered, err = d.prepareInterruptResume(ctx, store, state.Interrupts, approvalKey, resumeInput); err != nil {
			return result, err
		}
		approvalKey, resumeInput = "", nil
	} else {
		if approvalKey != "" && (!checkpoint.Waiting || checkpoint.Key != approvalKey) {
			return result, ErrApprovalMismatch
		}
		if resumeInput != nil || (approvalKey != "" && d.resumeHandler != nil) {
			// 原有的人工输入审批分支，内容保持不变。
		} else if approvalKey != "" && d.beforeApprove != nil {
			// 原有的前置校验分支，内容保持不变。
		}
	}
```

上面两处注释只标明位置：把原文件中这两个分支的函数体原样搬进来，一行都不要改。

4e. 节点 ctx 允许承载中断。把

```go
		nodeCtx, endNode := observe.Start(durable.WithJournal(durable.WithExecutionKey(ctx, intent.Key), store), observe.Event{Type: "node", Path: nodeName, OperationKey: intent.Key})
```

改为

```go
		nodeCtx, endNode := observe.Start(durable.AllowInterrupt(durable.WithJournal(durable.WithExecutionKey(ctx, intent.Key), store)), observe.Event{Type: "node", Path: nodeName, OperationKey: intent.Key})
```

4f. 节点返回错误的处理。把

```go
		if e != nil {
			return result, fmt.Errorf("node %q: %w", state.Node, e)
		}
```

改为

```go
		if e != nil {
			if points, ok := interruptPoints(durable.Waits(e)); ok && ctx.Err() == nil {
				waiting := *checkpoint
				waiting.Waiting = true
				paused := state
				paused.Interrupts = points
				if err = save(waiting, paused); err != nil {
					return result, err
				}
				if err = emit(&core.StreamEvent{Type: "interrupt_required", Step: nodeName}); err != nil {
					return result, err
				}
				if i := slices.IndexFunc(points, func(p InterruptPoint) bool { return p.Key == answered && p.Rejected }); answered != "" && i >= 0 {
					return result, &ResumeRejectedError{Err: fmt.Errorf("中断输入被拒绝：%s", points[i].Reason)}
				}
				return result, durable.ErrApprovalRequired
			}
			return result, fmt.Errorf("node %q: %w", state.Node, e)
		}
```

4g. `validateContinuation` 整体替换为：

```go
func (d *DurableGraph[T]) validateContinuation(id string, cp *durable.Snapshot, state graphState[T]) error {
	interrupted := len(state.Interrupts) > 0
	if state.Parallel != nil {
		cfg, ok := d.graph.parallel[state.Node]
		if !ok || state.Routing || cp.Key == "" || (cp.Waiting && !interrupted) || (d.approvals[state.Node] && !cp.Approved) {
			return fmt.Errorf("corrupt parallel continuation")
		}
		if err := validateParallel[T](cfg, state.Parallel); err != nil {
			return err
		}
	}
	if state.Routing && (state.Node == END || cp.Next == 0) {
		return fmt.Errorf("corrupt graph routing position")
	}
	if state.Node == END && cp.Next == 0 {
		return fmt.Errorf("corrupt graph completion position")
	}
	if state.Routing || state.Node == END {
		if cp.Key != "" || cp.Waiting || cp.Approved || interrupted {
			return fmt.Errorf("corrupt completed node intent")
		}
		return nil
	}
	if interrupted {
		// 中断等待：节点已开始执行；审批节点必须已获批准。
		if !cp.Waiting || cp.Key == "" || (d.approvals[state.Node] && !cp.Approved) {
			return fmt.Errorf("corrupt graph interrupt state")
		}
		for _, p := range state.Interrupts {
			if p.Key == "" || p.ID == "" || !json.Valid(p.Info) {
				return fmt.Errorf("corrupt graph interrupt state")
			}
		}
	} else {
		if cp.Waiting && cp.Approved {
			return fmt.Errorf("corrupt graph approval state")
		}
		if cp.Waiting || cp.Approved {
			if !d.approvals[state.Node] || cp.Key == "" {
				return fmt.Errorf("corrupt graph approval binding")
			}
		}
	}
	expected := graphDigest([]any{id, d.plan, state.Node, cp.Next})
	if cp.Key != "" && cp.Key != expected {
		return fmt.Errorf("corrupt graph execution key")
	}
	return nil
}
```

- [ ] **Step 5: 运行测试，确认通过**

Run: `go test -race ./workflow/ -count=1`
Expected: PASS（包括已有的审批、并行、子图测试）。

- [ ] **Step 6: lint 与提交**

```bash
golangci-lint run ./workflow/...
git add workflow/interrupt.go workflow/interrupt_test.go workflow/durable.go
git commit -m "feat(workflow): 节点执行中途等待人工输入并从检查点恢复

节点收到 AwaitInput 的等待时记为中断等待而非失败；恢复时注入输入重新执行节点，
已记日志的步骤回放。已回应的中断不接受不同输入，审批过的节点中断后不再重复审批。"
```

---

### Task 3: 并行分支中断

**Files:**
- Modify: `workflow/durable_parallel.go`（导入 `errors`；两处处理 `done.err` 的位置；函数结尾）
- Test: `workflow/interrupt_test.go`（追加）

**Interfaces:**
- Consumes: Task 2 的节点暂停、`validateContinuation`（并行检查点允许中断等待）
- Produces: 并行节点返回 `errors.Join` 汇总的等待，兄弟分支不会被取消

- [ ] **Step 1: 写失败测试**

追加到 `workflow/interrupt_test.go`：

```go
func askBranch(name string, calls *atomic.Int32) workflow.Branch[int] {
	return workflow.Branch[int]{Name: name, Run: func(ctx context.Context, n int) (int, error) {
		calls.Add(1)
		raw, err := durable.AwaitInput(ctx, "value", json.RawMessage(`{"branch":"`+name+`"}`), positiveInt)
		if err != nil {
			return 0, err
		}
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			return 0, err
		}
		return n + v, nil
	}}
}

func interruptFor(t *testing.T, snap *workflow.GraphSnapshot[int], branch string) workflow.InterruptPoint {
	t.Helper()
	for _, p := range snap.Interrupts {
		if strings.Contains(string(p.Info), `"`+branch+`"`) {
			return p
		}
	}
	t.Fatalf("没有分支 %s 的中断：%+v", branch, snap.Interrupts)
	return workflow.InterruptPoint{}
}

func TestParallelInterruptsDoNotCancelSiblings(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var a, b, c atomic.Int32
	plain := workflow.Branch[int]{Name: "c", Run: func(_ context.Context, n int) (int, error) { c.Add(1); return n * 10, nil }}
	run, err := parallelGraph(t, 3, []workflow.Branch[int]{askBranch("a", &a), askBranch("b", &b), plain}, sumBranches).
		CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "par-v1"})
	require.NoError(t, err)

	snap, err := run.Run(ctx, "par", 1, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 2)
	require.EqualValues(t, 1, c.Load())

	snap, err = run.Resume(ctx, "par", workflow.ResumeRequest{Key: interruptFor(t, snap, "a").Key, Input: json.RawMessage(`2`)})
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 1)
	interruptFor(t, snap, "b")
	require.EqualValues(t, 1, c.Load(), "已完成的兄弟分支不重跑")

	out, err := run.Resume(ctx, "par", workflow.ResumeRequest{Key: interruptFor(t, snap, "b").Key, Input: json.RawMessage(`3`)})
	require.NoError(t, err)
	require.Equal(t, 3+4+10, out.State)
}

func TestParallelPartialAnswerSurvivesCrash(t *testing.T) {
	ctx := context.Background()
	base := newStore(t)
	store := &filterFailStore{Store: base}
	var a, b atomic.Int32
	run, err := parallelGraph(t, 2, []workflow.Branch[int]{askBranch("a", &a), askBranch("b", &b)}, sumBranches).
		CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "par-v1"})
	require.NoError(t, err)
	snap, err := run.Run(ctx, "par", 1, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)

	// A 的输入写入日志后，保存 A 的分支输出失败：检查点仍等待 A 与 B。
	store.fail = func(v *durable.Snapshot) bool { return !bytes.Contains(v.Data, []byte(`"outputs":{}`)) }
	_, err = run.Resume(ctx, "par", workflow.ResumeRequest{Key: interruptFor(t, snap, "a").Key, Input: json.RawMessage(`2`)})
	require.ErrorIs(t, err, errDisk)
	store.fail = nil

	snap, err = run.Run(ctx, "par", 0, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 1, "只剩 B")
	interruptFor(t, snap, "b")

	out, err := run.Resume(ctx, "par", workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`3`)})
	require.NoError(t, err)
	require.Equal(t, 3+4, out.State)
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./workflow/ -run 'Parallel(Interrupts|PartialAnswer)' -count=1`
Expected: FAIL。第一个分支等待时兄弟分支被取消，报 `parallel branch ... canceled` 或节点失败，而不是 `ErrApprovalRequired` 加两个中断。

- [ ] **Step 3: 实现**

`workflow/durable_parallel.go`：

- 导入加上 `"errors"`。
- 在 `var firstErr error` 下一行声明 `var waits []error`。
- 协调循环 `case done := <-results:` 中，把

```go
			if done.err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("parallel branch %q: %w", name, done.err)
					cancel()
				}
				continue
			}
```

改为

```go
			if done.err != nil {
				// 等待不是失败：不取消兄弟分支，等全部结束后汇总。
				if durable.Waits(done.err) != nil {
					waits = append(waits, fmt.Errorf("parallel branch %q: %w", name, done.err))
					continue
				}
				if firstErr == nil {
					firstErr = fmt.Errorf("parallel branch %q: %w", name, done.err)
					cancel()
				}
				continue
			}
```

- 排空循环中，把

```go
		if done.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("parallel branch %q: %w", name, done.err)
			}
			continue
		}
```

改为

```go
		if done.err != nil {
			if durable.Waits(done.err) != nil {
				waits = append(waits, fmt.Errorf("parallel branch %q: %w", name, done.err))
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("parallel branch %q: %w", name, done.err)
			}
			continue
		}
```

- 在 `if firstErr != nil { return result, firstErr }` 之后加：

```go
	if len(waits) > 0 {
		return result, errors.Join(waits...)
	}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test -race ./workflow/ -count=1`
Expected: PASS。

- [ ] **Step 5: lint 与提交**

```bash
golangci-lint run ./workflow/...
git add workflow/durable_parallel.go workflow/interrupt_test.go
git commit -m "feat(workflow): 并行分支中断时不取消兄弟分支

分支等待原先按失败处理并取消兄弟分支，兄弟分支里已记日志的操作被记为 canceled，
恢复时需要人工介入。改为收集全部等待后汇总返回，已完成分支的输出照常保存。"
```

---

### Task 4: 嵌套子图中断

**Files:**
- Modify: `workflow/durable_runnable.go`（`ChildContinuation`、`childWait`、`WithResume` 的注释）
- Test: `workflow/interrupt_test.go`（追加）

**Interfaces:**
- Consumes: Task 2 的 `interruptPoints`（读取 `interrupts` 字段）、`prepareInterruptResume`
- Produces: `ChildContinuation.Interrupts []InterruptPoint`（JSON 标签 `interrupts,omitempty`），每一项的 Key 都是最内层的中断键

- [ ] **Step 1: 写失败测试**

追加到 `workflow/interrupt_test.go`：

```go
// nestedGraphs 返回外层图：其节点在可续跑的操作内运行内层子图，内层节点向人要输入。
func nestedGraphs(t *testing.T, store durable.Store, prepared *atomic.Int32) *workflow.DurableGraph[int] {
	t.Helper()
	ctx := context.Background()
	inner := compileAsk(t, store, prepared)
	outer, err := oneNode(t, func(ctx context.Context, n int) (int, error) {
		rec, err := durable.Do(ctx, "inner", json.RawMessage(`{}`), durable.OperationPolicy{MaxAttempts: 3, Resumable: true}, func(ctx context.Context) (json.RawMessage, error) {
			out, err := inner.AsChildRunnable().Invoke(ctx, n)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		})
		if err != nil {
			return 0, err
		}
		var out int
		err = json.Unmarshal(rec.Value, &out)
		return out * 2, err
	}).CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "outer-v1"})
	require.NoError(t, err)
	return outer
}

func TestNestedInterruptResumesWithLeafKey(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	outer := nestedGraphs(t, store, &prepared)
	snap, err := outer.Run(ctx, "outer", 10, "")
	point := requireInterrupted(t, snap, err)

	out, err := outer.Resume(ctx, "outer", workflow.ResumeRequest{Key: point.Key, Input: json.RawMessage(`5`)})
	require.NoError(t, err)
	require.Equal(t, 30, out.State)
	require.EqualValues(t, 1, prepared.Load())
}

func TestNestedInterruptResumesWithDirectResumeContext(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var prepared atomic.Int32
	outer := nestedGraphs(t, store, &prepared)
	snap, err := outer.Run(ctx, "outer", 10, "")
	point := requireInterrupted(t, snap, err)

	out, err := outer.Run(durable.WithResume(ctx, point.Key, `5`), "outer", 0, "")
	require.NoError(t, err)
	require.Equal(t, 30, out.State)
}

func TestChildRunnableReportsInterruptsAndAcceptsWrappedResume(t *testing.T) {
	base := newStore(t)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), base)
	var prepared atomic.Int32
	child := compileAsk(t, base, &prepared)
	run := func(ctx context.Context) (json.RawMessage, error) {
		out, err := child.AsChildRunnable().Invoke(ctx, 10)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
	policy := durable.OperationPolicy{MaxAttempts: 3, Resumable: true}
	_, err := durable.Do(ctx, "tool/c1", json.RawMessage(`{}`), policy, run)
	c := childContinuation(t, err)
	require.Len(t, c.Interrupts, 1)
	waits := durable.Waits(err)
	opKey := waits[0].Path[len(waits[0].Path)-1].Key

	// 冲突与非法输入走拒绝等待，父操作不失败。
	wrapped, err := workflow.WithResume(ctx, opKey, workflow.ResumeRequest{Key: c.Interrupts[0].Key, Input: json.RawMessage(`-1`)})
	require.NoError(t, err)
	_, err = durable.Do(wrapped, "tool/c1", json.RawMessage(`{}`), policy, run)
	rejected := childContinuation(t, err)
	require.True(t, rejected.Rejected)
	require.Equal(t, workflow.RejectResumeInput, rejected.Reason)

	wrapped, err = workflow.WithResume(ctx, opKey, workflow.ResumeRequest{Key: c.Interrupts[0].Key, Input: json.RawMessage(`5`)})
	require.NoError(t, err)
	rec, err := durable.Do(wrapped, "tool/c1", json.RawMessage(`{}`), policy, run)
	require.NoError(t, err)
	require.JSONEq(t, `15`, string(rec.Value))
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./workflow/ -run 'Nested|ChildRunnableReportsInterrupts' -count=1`
Expected: 编译失败，提示 `c.Interrupts undefined`。如果先临时加上字段，嵌套测试会因为外层节点收不到 `interrupts` 而按失败返回。

- [ ] **Step 3: 实现**

`workflow/durable_runnable.go`：

`ChildContinuation` 在 `Reason` 后增加：

```go
	// Interrupts 是子图（含更深层）待回应的中断，Key 为最内层中断键。可以直接用
	// durable.WithResume(ctx, Key, input) 回应，也可以经 WithResume 封装给本子图。
	Interrupts []InterruptPoint `json:"interrupts,omitempty"`
```

`childWait` 改为：

```go
func childWait[T any](id string, snapshot *GraphSnapshot[T], reason string) error {
	continuation, err := json.Marshal(ChildContinuation{RunID: id, Node: snapshot.Node, ApprovalKey: snapshot.Key, Rejected: reason != "", Reason: reason, Interrupts: snapshot.Interrupts})
	if err != nil {
		return err
	}
	return &durable.WaitError{Continuation: continuation}
}
```

`workflow/resume.go` 中 `WithResume` 的注释补一句：`req.Key 也可以是 ChildContinuation.Interrupts 中的中断键。`

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test -race ./workflow/ ./builder/ -count=1`
Expected: PASS（`builder/nested_resume_test.go` 等已有测试不受影响）。

- [ ] **Step 5: lint 与提交**

```bash
golangci-lint run ./workflow/...
git add workflow/durable_runnable.go workflow/resume.go workflow/interrupt_test.go
git commit -m "feat(workflow): 嵌套子图上报最内层中断键

子图暂停时父操作只能看到自己的执行键，宿主无从定位最内层的中断。
ChildContinuation 带上各层待回应中断；直接按中断键或经 WithResume 封装都能恢复。"
```

---

### Task 5: 可中断工具

**Files:**
- Modify: `tools/dispatcher.go:162-168`（`DurablePolicyProvider` 检查之后）
- Modify: `tools/function_tool.go`（`FunctionTool`、`FunctionToolBuilder`、`Build`）
- Test: `tools/interruptible_test.go`、`builder/interrupt_tool_test.go`

**Interfaces:**
- Consumes: Task 1 的 `durable.AwaitInput`、`durable.InterruptContinuation`
- Produces:
  - `type InterruptibleTool interface{ InterruptibleVersion() (version string, ok bool) }`
  - `func (b *FunctionToolBuilder) WithInterruptible(version string) *FunctionToolBuilder`
  - `func (f *FunctionTool) InterruptibleVersion() (string, bool)`
  - Dispatcher 为声明可中断的工具使用 `durable.OperationPolicy{Resumable: true, MaxAttempts: 1}`，intent 版本取 `version`

- [ ] **Step 1: 写失败测试（tools）**

`tools/interruptible_test.go`：

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func confirmTool(t *testing.T, version string, calls *int) *FunctionTool {
	t.Helper()
	b := NewFunctionToolBuilder("confirm").WithDescription("向用户确认").WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			*calls++
			raw, err := durable.AwaitInput(ctx, "confirm", json.RawMessage(`{"question":"确认？"}`), nil)
			if err != nil {
				return nil, err
			}
			return string(raw), nil
		})
	if version != "" {
		b = b.WithInterruptible(version)
	}
	tool, err := b.Build()
	require.NoError(t, err)
	return tool
}

func journalCtx(t *testing.T) context.Context {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
}

func dispatcherWith(t *testing.T, cfg DispatchConfig, tool interfaces.Tool) *Dispatcher {
	t.Helper()
	reg := NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return d
}

func confirmRequest() ToolRequest {
	return ToolRequest{Name: "confirm", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{}}}
}

func TestInterruptibleToolPausesAndResumes(t *testing.T) {
	ctx := journalCtx(t)
	calls := 0
	d := dispatcherWith(t, DispatchConfig{}, confirmTool(t, "v1", &calls))
	_, err := d.Invoke(ctx, confirmRequest())
	waits := durable.Waits(err)
	require.Len(t, waits, 1, "%v", err)
	var c durable.InterruptContinuation
	require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
	require.Equal(t, "confirm", c.ID)
	key := waits[0].Path[len(waits[0].Path)-1].Key

	out, err := d.Invoke(durable.WithResume(ctx, key, `"yes"`), confirmRequest())
	require.NoError(t, err)
	require.Equal(t, "\"yes\"", out.Result)
	require.Equal(t, 2, calls, "续跑重新执行整个工具")
}

func TestToolWithoutInterruptibleCannotPause(t *testing.T) {
	ctx := journalCtx(t)
	calls := 0
	d := dispatcherWith(t, DispatchConfig{}, confirmTool(t, "", &calls))
	_, err := d.Invoke(ctx, confirmRequest())
	require.ErrorIs(t, err, durable.ErrWaitNotResumable)
	require.Nil(t, durable.Waits(err))
}

func TestInterruptibleToolRequiresVersion(t *testing.T) {
	calls := 0
	tool := confirmTool(t, "", &calls)
	tool.interruptible = true
	reg := NewRegistry()
	require.NoError(t, reg.Register(tool))
	_, err := NewDispatcher(reg, DispatchConfig{})
	require.ErrorContains(t, err, "version")
}

// FunctionTool 开启中断必须声明版本，intent 哈希随之变化，旧记录会被检测到，不会悄悄按新策略处理。
func TestEnablingInterruptibleIsDetectedByIntent(t *testing.T) {
	ctx := journalCtx(t)
	plainCalls := 0
	plain, err := NewFunctionToolBuilder("confirm").WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(context.Context, map[string]interface{}) (interface{}, error) { plainCalls++; return "ok", nil }).Build()
	require.NoError(t, err)
	_, err = dispatcherWith(t, DispatchConfig{MaxAttempts: 1}, plain).Invoke(ctx, confirmRequest())
	require.NoError(t, err)

	calls := 0
	_, err = dispatcherWith(t, DispatchConfig{MaxAttempts: 1}, confirmTool(t, "v1", &calls)).Invoke(ctx, confirmRequest())
	require.ErrorContains(t, err, "intent")
	require.Zero(t, calls)
}
```

说明：`out.Result` 是工具函数返回的 `string(raw)`，即带引号的 JSON 字符串 `"yes"`。如果 Dispatcher 在完成路径上对结果重新编码或解码，以 `tools/dispatcher.go` 的行为为准修正这一处期望值，断言的含义（返回首次固定的输入）不变。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./tools/ -run 'Interruptible|WithoutInterruptible' -count=1`
Expected: 编译失败，提示 `WithInterruptible undefined`、`tool.interruptible undefined`。

- [ ] **Step 3: 实现**

`tools/function_tool.go`：

`FunctionTool` 增加字段：

```go
	interruptible bool
	version       string
```

增加方法：

```go
// InterruptibleVersion 报告工具是否声明可以在执行中途调用 durable.AwaitInput，以及对应的持久 intent 版本。
func (f *FunctionTool) InterruptibleVersion() (string, bool) { return f.version, f.interruptible }
```

`FunctionToolBuilder` 增加字段 `interruptible bool`、`version string`，并增加方法：

```go
// WithInterruptible 声明工具可以在执行中途调用 durable.AwaitInput 暂停。续跑会重新执行整个工具，
// AwaitInput 之前未经 durable.Do 记录的副作用会重复，开启前须确认可以接受。
// version 是持久 intent 版本，改变工具语义时须提升。
func (b *FunctionToolBuilder) WithInterruptible(version string) *FunctionToolBuilder {
	b.interruptible, b.version = true, version
	return b
}
```

`Build` 中把 `return NewFunctionTool(b.name, b.description, b.argsSchema, b.fn), nil` 改为：

```go
	tool := NewFunctionTool(b.name, b.description, b.argsSchema, b.fn)
	tool.interruptible, tool.version = b.interruptible, b.version
	return tool, nil
```

`tools/dispatcher.go`，在 `DurablePolicyProvider` 那段之后增加：

```go
		if v, ok := tool.(InterruptibleTool); ok {
			if version, on := v.InterruptibleVersion(); on {
				if entry.policy != nil {
					return nil, fmt.Errorf("tool %q declares both a durable policy and interruptible", name)
				}
				if version == "" {
					return nil, fmt.Errorf("interruptible tool %q version required", name)
				}
				// MaxAttempts 为 1：只允许从等待中重入，崩溃留下的 pending 仍交给宿主处理。
				policy := durable.OperationPolicy{Resumable: true, MaxAttempts: 1}
				entry.policy, entry.version = &policy, version
			}
		}
```

在同一文件定义接口（放在 `DispatchTimeoutProvider` 定义附近）：

```go
// InterruptibleTool 声明工具可以在执行中途调用 durable.AwaitInput；ok 为 false 时视为未声明。
// version 是持久 intent 版本，开启时必填。
type InterruptibleTool interface{ InterruptibleVersion() (version string, ok bool) }
```

- [ ] **Step 4: 运行 tools 测试，确认通过**

Run: `go test -race ./tools/ -count=1`
Expected: PASS。

- [ ] **Step 5: 写 builder 测试**

`builder/interrupt_tool_test.go`：

```go
package builder

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func confirmRefundTool(t *testing.T, calls *int) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewFunctionToolBuilder("confirm_refund").WithDescription("向用户确认退款").
		WithArgsSchema(`{"type":"object","properties":{}}`).WithInterruptible("v1").
		WithFunction(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			*calls++
			raw, err := durable.AwaitInput(ctx, "confirm", json.RawMessage(`{"question":"确认退款？"}`), nil)
			if err != nil {
				return nil, err
			}
			return string(raw), nil
		}).Build()
	require.NoError(t, err)
	return tool
}

func interruptAgentContext(t *testing.T, root string) context.Context {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), root), store)
	controller, err := budget.NewDurable(budget.Config{RootKey: root, Version: "v1", MaxModelRequests: 3}, store)
	require.NoError(t, err)
	return budget.WithController(ctx, controller)
}

func interruptModel() *nativeEventModel {
	return nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c1", "confirm_refund", `{}`)}, Usage: &llm.Usage{TotalTokens: 3}}},
		answerTurn("已确认退款", 4),
	)
}

func interruptKeyFrom(t *testing.T, err error) string {
	t.Helper()
	require.ErrorIs(t, err, ErrWaiting)
	var waiting *WaitingError
	require.True(t, errors.As(err, &waiting))
	require.Len(t, waiting.Waits, 1)
	var c durable.InterruptContinuation
	require.NoError(t, json.Unmarshal(waiting.Waits[0].Continuation, &c))
	require.Equal(t, "confirm", c.ID)
	path := waiting.Waits[0].Path
	return path[len(path)-1].Key
}

func TestInterruptibleToolPausesAgentAndResumesWithInvoke(t *testing.T) {
	ctx := interruptAgentContext(t, "ticket-int-1")
	calls := 0
	model := interruptModel()
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, confirmRefundTool(t, &calls)).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "退款 A1")
	key := interruptKeyFrom(t, err)
	require.Len(t, model.requests, 1)

	out, err := agent.AsRunnable().Invoke(durable.WithResume(ctx, key, `"yes"`), "退款 A1")
	require.NoError(t, err)
	require.Equal(t, "已确认退款", out.Result)
	require.Len(t, model.requests, 2)
	require.Equal(t, 2, calls)
}

func TestInterruptibleToolResumesWithStart(t *testing.T) {
	ctx := interruptAgentContext(t, "ticket-int-2")
	calls := 0
	model := interruptModel()
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").
		WithTools(dispatcherFor(t, confirmRefundTool(t, &calls)).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = agent.Start(ctx, "退款 A1").Wait(waitCtx)
	key := interruptKeyFrom(t, err)
	out, err := agent.Start(durable.WithResume(ctx, key, `"yes"`), "退款 A1").Wait(waitCtx)
	require.NoError(t, err)
	require.Equal(t, "已确认退款", out.Result)
}

func TestParallelToolCallsOneInterruptsOthersComplete(t *testing.T) {
	ctx := interruptAgentContext(t, "ticket-int-3")
	calls, lookups := 0, 0
	lookup, err := tools.NewFunctionToolBuilder("lookup").WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(context.Context, map[string]interface{}) (interface{}, error) { lookups++; return "订单存在", nil }).Build()
	require.NoError(t, err)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{
			nativeCall("c1", "confirm_refund", `{}`), nativeCall("c2", "lookup", `{}`)}, Usage: &llm.Usage{TotalTokens: 3}}},
		answerTurn("完成", 4),
	)
	agent, err := NewSimpleBuilder(model).WithDurableScope("support").WithParallelToolCalls(2).
		WithTools(dispatcherFor(t, confirmRefundTool(t, &calls), lookup).Tools()...).WithMaxIterations(2).Build()
	require.NoError(t, err)

	_, err = agent.AsRunnable().Invoke(ctx, "退款 A1")
	key := interruptKeyFrom(t, err)
	require.Equal(t, 1, lookups)

	out, err := agent.AsRunnable().Invoke(durable.WithResume(ctx, key, `"yes"`), "退款 A1")
	require.NoError(t, err)
	require.Equal(t, "完成", out.Result)
	require.Equal(t, 1, lookups, "已完成的兄弟调用只回放")
}
```

- [ ] **Step 6: 运行 builder 测试**

Run: `go test -race ./builder/ -run 'Interruptible|ParallelToolCallsOneInterrupts' -count=1`
Expected: PASS。

- [ ] **Step 7: 全量测试、lint 与提交**

```bash
go test -race ./tools/... ./builder/... -count=1
golangci-lint run ./tools/... ./builder/...
git add tools/function_tool.go tools/dispatcher.go tools/interruptible_test.go builder/interrupt_tool_test.go
git commit -m "feat(tools): 工具显式声明可中断后可在执行中途向人要输入

普通工具的持久策略不可续跑，调用 AwaitInput 只会失败。新增 InterruptibleTool，
声明后使用 {Resumable, MaxAttempts:1}：只允许从等待重入，崩溃仍交给宿主。
开启必须带版本，使 intent 变化可被检测，不会把旧记录悄悄按新策略处理。"
```

---

### Task 6: 文档、示例与全量验证

**Files:**
- Modify: `docs/guides/HUMAN_IN_THE_LOOP.md`
- Modify: `docs/superpowers/specs/2026-11-04-dynamic-interrupt-design.md`
- Modify: `CHANGELOG.md`（`## [Unreleased]` 下）
- Modify: `examples/README.md`
- Create: `examples/human-in-the-loop-interrupt/main.go`、`examples/human-in-the-loop-interrupt/README.md`

**Interfaces:**
- Consumes: Task 1–5 的全部公开 API

- [ ] **Step 1: 指南新增一节**

在 `docs/guides/HUMAN_IN_THE_LOOP.md` 的“嵌套在 Agent 工具中”之前插入：

````markdown
## 节点或工具执行中途暂停

`durable.AwaitInput(ctx, id, info, validate)` 在工作流节点、并行分支或声明可中断的工具里暂停，等人提供输入：

```go
raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"退款金额"}`), validateAmount)
if err != nil {
    return state, err // 等待原样返回，由框架记为暂停
}
```

- 图暂停时 `Run` 返回 `durable.ErrApprovalRequired`，`Snapshot.Interrupts` 列出 `Key`、`ID`、`Info`。用 `graph.Resume(ctx, runID, workflow.ResumeRequest{Key: point.Key, Input: ...})` 回应；也可以用 `durable.WithResume(ctx, point.Key, input)` 加上不带键的 `Run`。
- 恢复会从头重新执行节点或工具。`AwaitInput` 之前的外部写入必须经 `durable.Do` 记录，或者按 `workflow.DurableKey(ctx)` 自行幂等，否则会重复执行。
- 首次成功持久化的输入就此固定，之后的重放都返回它。用同一个键再交不同输入时返回 `ResumeRejectedError`（`workflow.ErrInterruptAnswered`），节点不执行；输入是否相同按 `json.Compact` 后的字节比较。
- 输入不是合法 JSON 或 `validate` 失败时保持等待，`Interrupts[i].Rejected` 和 `Reason` 说明原因，修正后可以重交。`null` 是合法的显式输入。
- `id` 在同一节点或工具内必须唯一，循环中须带序号，否则会回放上一次的答案。`info` 必须是确定性的 JSON，不得包含凭证。
- 并行节点中一个分支暂停时，兄弟分支照常完成，中断逐个回应。嵌套子图的 `ChildContinuation.Interrupts` 给出最内层中断键。
- 工具须用 `tools.NewFunctionToolBuilder(...).WithInterruptible(version)` 显式开启；自定义工具实现 `tools.InterruptibleTool`。开启或改变语义时提升版本。等使用该工具的进行中运行结束后再切换；自行实现 `DurablePolicyProvider` 的工具，只改 `Resumable` 不会被框架检测到。
- 动态中断的回应不经过 `WithResumeHandler`、`WithBeforeApprove`。中断键不是授权凭证：宿主必须先认证回应者，并确认其有权回应该中断。

| 机制 | 适用 |
|---|---|
| `ApprovalNodes` + `WithResumeHandler` | 节点执行前审批，人工修改业务状态 |
| `durable.AwaitInput` | 节点或工具执行中途要一个输入 |
| `core.InterruptManager` | 进程内阻塞等待，不能跨进程恢复 |
| `Handle.Interrupt` | 取消正在运行的 Agent，不是暂停 |
````

- [ ] **Step 2: 同步 spec**

修改 `docs/superpowers/specs/2026-11-04-dynamic-interrupt-design.md`：

- “工具”一节：`WithInterruptible()` 改为 `WithInterruptible(version)`，写明经可选接口 `tools.InterruptibleTool` 声明，开启必须带版本。
- “破坏性变更与迁移”：FunctionTool 开启中断时版本变化会改变 intent 哈希，旧记录报 intent 变更；“只改 Resumable 不报错”只适用于自行实现 `DurablePolicyProvider` 的工具。
- “文档”一节：离线示例改为工作流节点中断，工具中断由 builder 测试覆盖。
- “恢复”一节：恢复期间检查点保持中断等待，不在重新执行前清除。

- [ ] **Step 3: 离线示例**

`examples/human-in-the-loop-interrupt/main.go`：

```go
// human-in-the-loop-interrupt 演示节点执行中途向人要输入，跨进程恢复，不调用模型。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

type refund struct {
	Order  string `json:"order"`
	Amount int    `json:"amount"`
	Status string `json:"status"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("human-in-the-loop-interrupt", flag.ContinueOnError)
	dir := flags.String("dir", "", "检查点目录")
	order := flags.String("order", "A1", "订单号（首次运行）")
	key := flags.String("key", "", "要回应的中断键")
	input := flags.String("input", "", "人工输入（JSON）")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || flags.NArg() > 0 {
		return errors.New("provide -dir and no positional arguments")
	}
	store, err := durable.NewFileStore(*dir)
	if err != nil {
		return err
	}
	graph, err := build(ctx, store)
	if err != nil {
		return err
	}
	var snap *workflow.GraphSnapshot[refund]
	if *key == "" {
		snap, err = graph.Run(ctx, "refund-"+*order, refund{Order: *order}, "")
	} else {
		// 实际应用须先认证回应者；中断键只定位暂停，不是授权凭证。
		snap, err = graph.Resume(ctx, "refund-"+*order, workflow.ResumeRequest{Key: *key, Input: json.RawMessage(*input)})
	}
	if err != nil && !errors.Is(err, durable.ErrApprovalRequired) {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

func build(ctx context.Context, store durable.Store) (*workflow.DurableGraph[refund], error) {
	g := workflow.NewGraph[refund]()
	err := g.AddNode("refund", func(ctx context.Context, r refund) (refund, error) {
		raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"退款金额（正整数）"}`), func(raw json.RawMessage) error {
			var n int
			if json.Unmarshal(raw, &n) != nil || n <= 0 {
				return errors.New("需要正整数")
			}
			return nil
		})
		if err != nil {
			return r, err
		}
		if err := json.Unmarshal(raw, &r.Amount); err != nil {
			return r, err
		}
		r.Status = "refunded"
		return r, nil
	})
	if err != nil {
		return nil, err
	}
	if err := g.AddEdge(workflow.START, "refund"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("refund", workflow.END); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "refund-interrupt-v1"})
}
```

`examples/human-in-the-loop-interrupt/README.md`：

````markdown
# 节点执行中途向人要输入

无需模型或 API 密钥。

```sh
DIR=$(mktemp -d)
go run ./examples/human-in-the-loop-interrupt -dir "$DIR" -order A1
```

输出 `Waiting: true`，`Interrupts[0]` 给出 `Key` 与问题。在另一个进程中回应：

```sh
go run ./examples/human-in-the-loop-interrupt -dir "$DIR" -order A1 -key '输出中的 Key' -input 120
```

返回 `Done: true`，`status` 为 `refunded`。输入 `-1` 会被拒绝，仍在等待，改正后可以重交。
````

在 `examples/README.md` 的示例列表中，紧接 `human-in-the-loop` 那一行之后加一行：`- [human-in-the-loop-interrupt](human-in-the-loop-interrupt/README.md)：节点执行中途向人要输入并跨进程恢复。`

- [ ] **Step 4: CHANGELOG**

在 `CHANGELOG.md` 的 `## [Unreleased]` 下的 `### Added` 中追加：

```markdown
- `durable.AwaitInput`：工作流节点、并行分支与声明可中断的工具可以在执行中途等待人工输入，从检查点恢复；首次持久化的输入固定，非法 JSON 或校验失败时保持等待。`GraphSnapshot.Interrupts`、`ChildContinuation.Interrupts` 给出中断键，`Resume` 同时接受审批键与中断键。工具经 `FunctionToolBuilder.WithInterruptible(version)` 或 `tools.InterruptibleTool` 显式开启。
```

如果 `## [Unreleased]` 下还没有 `### Changed`，就新建一个，追加：

```markdown
- 持久图节点返回中断等待时记为暂停而非失败；并行节点中分支等待不再取消兄弟分支。迁移：依赖“节点返回 `*durable.WaitError` 即失败”的宿主需改为处理 `ErrApprovalRequired` 与 `Snapshot.Interrupts`；已有工具开启中断时须提升版本，并等进行中的运行结束后再切换。
```

- [ ] **Step 5: 全量验证**

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
golangci-lint run
bash scripts/verify_imports.sh
bash scripts/check_coverage.sh
D=$(mktemp -d)
go run ./examples/human-in-the-loop-interrupt -dir "$D" -order A1 > "$D/first.json"
# 顶层 "Key" 是节点 intent 键；中断键是 Interrupts 中小写的 "key"。
KEY=$(sed -n 's/.*"key": "\(.*\)".*/\1/p' "$D/first.json")
go run ./examples/human-in-the-loop-interrupt -dir "$D" -order A1 -key "$KEY" -input -1 || true
go run ./examples/human-in-the-loop-interrupt -dir "$D" -order A1 -key "$KEY" -input 120
```

Expected：全部命令成功；`-input -1` 那一步报 `resume input rejected`；最后一条输出 `"Done": true` 与 `"status": "refunded"`。

- [ ] **Step 6: 提交**

```bash
git add docs/guides/HUMAN_IN_THE_LOOP.md docs/superpowers/specs/2026-11-04-dynamic-interrupt-design.md CHANGELOG.md examples/README.md examples/human-in-the-loop-interrupt
git commit -m "docs(hitl): 说明执行中途暂停的语义、授权边界与迁移

动态中断会重新执行节点或工具、固定首次输入、不经过审批钩子，这些都会影响宿主的
副作用设计与授权方式，需要写明；附离线示例供跨进程恢复验证。"
```
