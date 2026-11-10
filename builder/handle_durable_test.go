package builder

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/panictest"
	"github.com/stretchr/testify/require"
)

const handleCrashExit = 87

func durableHandleContext(t *testing.T, store durable.Store) context.Context {
	t.Helper()
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), "handle-crash"), store)
}

func newHandleStore(t *testing.T, dir string) durable.Store {
	t.Helper()
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	return store
}

func requestText(req *llm.CompletionRequest) string {
	var text strings.Builder
	for _, msg := range req.Messages {
		text.WriteString(msg.Content)
		text.WriteByte('\n')
	}
	return text.String()
}

// runHandleChild 以子进程重新运行当前测试，要求它以 handleCrashExit 退出。
func runHandleChild(t *testing.T, name, env, dir string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), env+"="+dir)
	output, err := panictest.CombinedOutput(cmd, panictest.ChildTimeout(t, time.Minute))
	var exited *exec.ExitError
	require.ErrorAs(t, err, &exited, string(output))
	require.Equal(t, handleCrashExit, exited.ExitCode(), string(output))
}

// 真实退出进程：入队成功的输入在被领取前不能随进程丢失，重启后的 Start 要继续处理。
func TestHandleAcceptedInputsSurviveProcessExit(t *testing.T) {
	const env = "GOAGENT_HANDLE_CRASH_DIR"
	if dir := os.Getenv(env); dir != "" {
		started := make(chan struct{})
		agent, err := NewSimpleBuilder(newScriptModel(answer("draft"))).WithDurableScope("handle").WithMaxIterations(5).
			WithTools(dispatcherFor(t, plainTool()).Tools()...).
			WithTurnStopping(func(context.Context, *Stopping) (string, error) {
				close(started)
				select {}
			}).Build()
		require.NoError(t, err)
		h := agent.Start(durableHandleContext(t, newHandleStore(t, dir)), "task")
		<-started
		require.NoError(t, h.Steer("steer-after-crash"))
		require.NoError(t, h.Inject("inject-after-crash"))
		require.NoError(t, h.Redirect("redirect-after-crash"))
		require.NoError(t, h.Followup("followup-after-crash"))
		os.Exit(handleCrashExit)
	}

	dir := t.TempDir()
	runHandleChild(t, "TestHandleAcceptedInputsSurviveProcessExit", env, dir)

	model := newScriptModel(answer("after followup"))
	agent, err := NewSimpleBuilder(model).WithDurableScope("handle").WithMaxIterations(5).WithTools(dispatcherFor(t, plainTool()).Tools()...).Build()
	require.NoError(t, err)
	h := agent.Start(durableHandleContext(t, newHandleStore(t, dir)), "task")
	require.Equal(t, "after followup", waitAnswer(t, h).Result)
	require.Empty(t, h.PendingInputs())
	require.Equal(t, 1, model.count(), "已完成的第一轮从日志回放，不重新请求模型")
	text := requestText(model.request(0))
	for _, input := range []string{"steer-after-crash", "inject-after-crash", "redirect-after-crash", "followup-after-crash"} {
		require.Contains(t, text, input)
	}
}

// exitOnInboxAck 在领取已记入日志、收件箱尚未确认时退出进程。
type exitOnInboxAck struct {
	durable.Store
	saves int
}

func (s *exitOnInboxAck) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if strings.HasPrefix(id, "goagent/inbox/") {
		s.saves++
		if s.saves == 2 { // 第一次是入队，第二次是确认领取。
			os.Exit(handleCrashExit)
		}
	}
	return s.Store.Save(ctx, id, snapshot)
}

// 领取记录已落盘但确认未完成时崩溃：恢复后输入只进入模型一次。
func TestHandleClaimedInputIsNotDuplicatedAfterCrashBeforeAck(t *testing.T) {
	const env = "GOAGENT_HANDLE_ACK_CRASH_DIR"
	if dir := os.Getenv(env); dir != "" {
		started, release := make(chan struct{}), make(chan struct{})
		turns := 0
		agent, err := NewSimpleBuilder(newScriptModel(callTool("call-1", "lookup"), answer("unreachable"))).
			WithDurableScope("handle").WithMaxIterations(5).WithTools(dispatcherFor(t, plainTool()).Tools()...).
			WithRequestHook(func(context.Context, *ModelRequest) error {
				if turns++; turns == 1 {
					close(started)
					<-release
				}
				return nil
			}).Build()
		require.NoError(t, err)
		h := agent.Start(durableHandleContext(t, &exitOnInboxAck{Store: newHandleStore(t, dir)}), "task")
		<-started
		require.NoError(t, h.Steer("only-once"))
		close(release)
		_, _ = h.Wait(context.Background())
		t.Fatal("期望在确认领取时退出进程")
	}

	dir := t.TempDir()
	runHandleChild(t, "TestHandleClaimedInputIsNotDuplicatedAfterCrashBeforeAck", env, dir)

	model := newScriptModel(answer("done"))
	agent, err := NewSimpleBuilder(model).WithDurableScope("handle").WithMaxIterations(5).
		WithTools(dispatcherFor(t, plainTool()).Tools()...).Build()
	require.NoError(t, err)
	h := agent.Start(durableHandleContext(t, newHandleStore(t, dir)), "task")
	require.Equal(t, "done", waitAnswer(t, h).Result)
	require.Equal(t, 1, strings.Count(requestText(model.request(0)), "only-once"))
	require.Empty(t, h.PendingInputs())
}

// 通知方在写入后、标记完成前崩溃，重投时输入可能已被领取：同一键不会第二次进入对话。
func TestHandleSteerOnceDeduplicatesAfterRestartAndClaim(t *testing.T) {
	dir := t.TempDir()
	const env = "GOAGENT_HANDLE_STEER_ONCE_DIR"
	if child := os.Getenv(env); child != "" {
		started := make(chan struct{})
		agent, err := NewSimpleBuilder(newScriptModel(answer("unreachable"))).WithDurableScope("handle").WithMaxIterations(5).
			WithTools(dispatcherFor(t, plainTool()).Tools()...).
			WithRequestHook(func(context.Context, *ModelRequest) error {
				close(started)
				select {}
			}).Build()
		require.NoError(t, err)
		h := agent.Start(durableHandleContext(t, newHandleStore(t, child)), "task")
		<-started
		require.NoError(t, h.SteerOnce("notice-1", "only-once"))
		os.Exit(handleCrashExit)
	}
	runHandleChild(t, "TestHandleSteerOnceDeduplicatesAfterRestartAndClaim", env, dir)

	model := newScriptModel(callTool("call-1", "lookup"), callTool("call-2", "lookup"), answer("done"))
	var h *Handle
	started := make(chan struct{})
	turns := 0
	agent, err := NewSimpleBuilder(model).WithDurableScope("handle").WithMaxIterations(5).
		WithTools(dispatcherFor(t, plainTool()).Tools()...).
		WithRequestHook(func(context.Context, *ModelRequest) error {
			<-started
			if turns++; turns == 2 {
				require.Empty(t, h.PendingInputs(), "第二次请求之前输入已被领取")
				require.NoError(t, h.SteerOnce("notice-1", "only-once"))
			}
			return nil
		}).Build()
	require.NoError(t, err)
	h = agent.Start(durableHandleContext(t, newHandleStore(t, dir)), "task")
	close(started)
	require.Equal(t, "done", waitAnswer(t, h).Result)
	require.Equal(t, 1, strings.Count(requestText(model.request(2)), "only-once"))
}

// 非持久运行在 Handle 内去重；已结束的 Handle 不接受输入，由调用方另行投递。
func TestHandleSteerOnceWithoutJournal(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := newScriptModel(answer("draft"), answer("done"))
	turns := 0
	agent, err := NewSimpleBuilder(model).WithMaxIterations(5).WithTools(dispatcherFor(t, plainTool()).Tools()...).
		WithRequestHook(func(context.Context, *ModelRequest) error {
			if turns++; turns == 1 {
				close(started)
				<-release
			}
			return nil
		}).Build()
	require.NoError(t, err)
	h := agent.Start(context.Background(), "task")
	<-started
	require.NoError(t, h.SteerOnce("k", "only-once"))
	require.NoError(t, h.SteerOnce("k", "only-once"))
	require.Len(t, h.PendingInputs(), 1)
	close(release)
	require.Equal(t, "done", waitAnswer(t, h).Result)
	require.ErrorIs(t, h.SteerOnce("k2", "late"), agentErrors.ErrAgentHandleDone)
}

// failInboxSave 使收件箱的写入失败。
type failInboxSave struct {
	durable.Store
	err error
}

func (s failInboxSave) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if strings.HasPrefix(id, "goagent/inbox/") {
		return s.err
	}
	return s.Store.Save(ctx, id, snapshot)
}

// 写入失败时不算接受：调用方收到错误，输入也不留在内存里被处理。
func TestHandleInputPersistFailureIsReported(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	boom := errors.New("disk full")
	model := newScriptModel(answer("done"))
	agent, err := NewSimpleBuilder(model).WithDurableScope("handle").WithMaxIterations(2).WithTools(dispatcherFor(t, plainTool()).Tools()...).
		WithRequestHook(func(context.Context, *ModelRequest) error {
			close(started)
			<-release
			return nil
		}).Build()
	require.NoError(t, err)
	h := agent.Start(durableHandleContext(t, failInboxSave{Store: newHandleStore(t, t.TempDir()), err: boom}), "task")
	<-started
	require.ErrorIs(t, h.Steer("lost"), boom)
	require.ErrorIs(t, h.Inject("lost"), boom)
	require.ErrorIs(t, h.Followup("lost"), boom)
	require.ErrorIs(t, h.Redirect("lost"), boom)
	require.Empty(t, h.PendingInputs())
	close(release)
	require.Equal(t, "done", waitAnswer(t, h).Result)
	require.NotContains(t, requestText(model.request(0)), "lost")
}

// Followup 批次被领取后崩溃：恢复时用记录的批次继续同一轮，不需要宿主重建，也不重复。
func TestHandleClaimedFollowupBatchResumesAfterRestart(t *testing.T) {
	const env = "GOAGENT_HANDLE_FOLLOWUP_CRASH_DIR"
	build := func(model *scriptModel, hook func(context.Context, *ModelRequest) error) *ConfigurableAgent[*core.AgentState] {
		agent, err := NewSimpleBuilder(model).WithDurableScope("handle").WithMaxIterations(2).
			WithTools(dispatcherFor(t, plainTool()).Tools()...).WithRequestHook(hook).Build()
		require.NoError(t, err)
		return agent
	}
	if dir := os.Getenv(env); dir != "" {
		started, release := make(chan struct{}), make(chan struct{})
		requests := 0
		h := build(newScriptModel(answer("first")), func(context.Context, *ModelRequest) error {
			if requests++; requests == 2 { // 第二轮已开始：批次已领取。
				os.Exit(handleCrashExit)
			}
			close(started)
			<-release
			return nil
		}).Start(durableHandleContext(t, newHandleStore(t, dir)), "task")
		<-started
		require.NoError(t, h.Followup("second-turn"))
		close(release)
		_, _ = h.Wait(context.Background())
		t.Fatal("期望在第二轮开始时退出进程")
	}

	dir := t.TempDir()
	runHandleChild(t, "TestHandleClaimedFollowupBatchResumesAfterRestart", env, dir)
	noHook := func(context.Context, *ModelRequest) error { return nil }
	ctx := durableHandleContext(t, newHandleStore(t, dir))

	model := newScriptModel(answer("second"))
	h := build(model, noHook).Start(ctx, "task")
	require.Equal(t, "second", waitAnswer(t, h).Result)
	require.Empty(t, h.PendingInputs())
	require.Equal(t, 1, model.count())
	require.Equal(t, 1, strings.Count(requestText(model.request(0)), "second-turn"))

	// 全部完成后再次启动只回放，不产生新请求。
	replayed := newScriptModel()
	require.Equal(t, "second", waitAnswer(t, build(replayed, noHook).Start(ctx, "task")).Result)
	require.Zero(t, replayed.count())
}
