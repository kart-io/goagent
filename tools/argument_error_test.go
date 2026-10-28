package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type argTestTool struct {
	*BaseTool
	validate func(*interfaces.ToolInput) error
}

func (t *argTestTool) Validate(_ context.Context, in *interfaces.ToolInput) error {
	return t.validate(in)
}

const idSchema = `{"type":"object","properties":{"id":{"type":"string","minLength":1}},"required":["id"],"additionalProperties":false}`

func argDispatcher(t *testing.T, cfg DispatchConfig, tools ...interfaces.Tool) *Dispatcher {
	t.Helper()
	reg := NewRegistry()
	for _, tool := range tools {
		require.NoError(t, reg.Register(tool))
	}
	d, err := NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return d
}

// modelCall 走 Tools() 的模型调用路径；只有这条路径会交还参数错误。
func modelCall(ctx context.Context, d *Dispatcher, name string, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return (&dispatchedTool{d: d, entry: dispatchEntry{name: name}}).Invoke(ctx, in)
}

func requireArgumentResult(t *testing.T, out *interfaces.ToolOutput, err error, message string) {
	t.Helper()
	require.NoError(t, err)
	require.False(t, out.Success)
	require.Empty(t, out.Error)
	require.Equal(t, map[string]any{"error": "invalid_arguments", "message": message}, out.Result)
}

func TestArgumentErrorClassification(t *testing.T) {
	a := &ArgumentError{Message: "id 必填"}
	b := &ArgumentError{Message: "amount 必须为正数"}
	// 内层登记的是整条错误链；外层按节点身份识别，同类型的其他值不受影响。
	innerArg, innerMarked := &ArgumentError{Message: "内层"}, marked("url 必填")
	ctx, settled := withSettledSet(context.Background())
	innerErr := fmt.Errorf("child: %w", errors.Join(innerArg, innerMarked))
	require.True(t, Settle(ctx, innerErr) == innerErr, "登记不改变错误值")
	require.Nil(t, Settle(ctx, nil))
	for name, c := range map[string]struct {
		err     error
		message string
		ok      bool
	}{
		"直接":                 {a, "id 必填", true},
		"包装后取内层消息":           {fmt.Errorf("decode: %w", a), "id 必填", true},
		"全部分支可恢复":            {errors.Join(a, b), "id 必填; amount 必须为正数", true},
		"多重 %w":              {fmt.Errorf("%w / %w", a, b), "id 必填; amount 必须为正数", true},
		"混入普通错误":             {errors.Join(a, errors.New("cleanup failed")), "", false},
		"包装的 Join 混入普通错误":    {fmt.Errorf("x: %w", errors.Join(a, errors.New("cleanup failed"))), "", false},
		"普通错误":               {errors.New("upstream"), "", false},
		"nil":                {nil, "", false},
		"空 Join":             {errors.Join(), "", false},
		"原因为深度超限":            {&ArgumentError{Message: "x", Err: ErrNestedExecutionLimit}, "", false},
		"原因为重入":              {&ArgumentError{Message: "x", Err: ErrDispatcherReentry}, "", false},
		"原因为等待宿主":            {&ArgumentError{Message: "x", Err: ErrToolWorkflowWaiting}, "", false},
		"与取消并列":              {errors.Join(a, context.Canceled), "", false},
		"原因为超时":              {&ArgumentError{Message: "x", Err: context.DeadlineExceeded}, "", false},
		"内层已定性":              {innerArg, "", false},
		"内层原样返回的整条链":         {innerErr, "", false},
		"包装的内层已定性":           {fmt.Errorf("outer: %w", innerArg), "", false},
		"与内层已定性并列":           {errors.Join(a, innerArg), "", false},
		"同类型的另一个值":           {&ArgumentError{Message: "内层"}, "内层", true},
		"已标记的 AgentError":    {marked("url 必填"), "url 必填", true},
		"包装的已标记":             {agentErrors.Wrap(marked("url 必填"), agentErrors.CodeToolExecution, "run"), "url 必填", true},
		"已标记与参数错误并列":         {errors.Join(marked("url 必填"), a), "url 必填; id 必填", true},
		"未标记的 INVALID_INPUT": {agentErrors.New(agentErrors.CodeInvalidInput, "access to namespace is not allowed"), "", false},
		"已标记与未标记并列":          {errors.Join(marked("url 必填"), agentErrors.NewInvalidInputError("llm", "request", "400")), "", false},
		"已标记但内层已定性":          {innerMarked, "", false},
		"重新标记内层错误视为本层定性":     {agentErrors.Wrap(innerArg, agentErrors.CodeInvalidInput, "重新定性").MarkModelArgument(), "重新定性", true},
		"已标记但原因为取消":          {agentErrors.Wrap(context.Canceled, agentErrors.CodeInvalidInput, "x").MarkModelArgument(), "", false},
	} {
		t.Run(name, func(t *testing.T) {
			message, ok := argumentErrorMessage(c.err, settled)
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.message, message)
		})
	}
	_, ok := argumentErrorMessage(innerArg, nil)
	require.True(t, ok, "未登记时按本层参数错误处理")
	cause := errors.New("cause")
	wrapped := &ArgumentError{Message: "shown", Err: cause}
	require.ErrorIs(t, wrapped, cause)
	require.Equal(t, "shown", wrapped.Error(), "原因不出现在模型可见的消息里")
}

// 默认关闭：行为与引入前一致，错误文本不变，且仍可用 errors.As 识别。
func TestDispatcherArgumentErrorsOffByDefault(t *testing.T) {
	calls := 0
	tool := NewBaseTool("lookup", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return nil, &ArgumentError{Message: "未知 id"}
	})
	d := argDispatcher(t, DispatchConfig{}, tool)
	out, err := d.Invoke(context.Background(), ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{"id": ""}}})
	require.Nil(t, out)
	require.ErrorContains(t, err, `tool "lookup" arguments:`)
	var arg *ArgumentError
	require.ErrorAs(t, err, &arg)
	require.Zero(t, calls)
	_, err = d.Invoke(context.Background(), ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{"id": "x"}}})
	require.EqualError(t, err, "未知 id")
}

func TestDispatcherReturnsArgumentErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("Schema 校验失败不执行但仍授权", func(t *testing.T) {
		calls, authorized := 0, 0
		tool := NewBaseTool("lookup", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			calls++
			return &interfaces.ToolOutput{Success: true}, nil
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true, Authorize: func(context.Context, string, *interfaces.ToolInput) error {
			authorized++
			return nil
		}}, tool)
		for _, args := range []map[string]any{{}, {"id": ""}, {"id": 1}, {"id": "x", "extra": true}} {
			out, err := modelCall(ctx, d, "lookup", &interfaces.ToolInput{Args: args})
			require.NoError(t, err, args)
			require.False(t, out.Success)
			require.Equal(t, "invalid_arguments", out.Result.(map[string]any)["error"])
			require.Contains(t, out.Result.(map[string]any)["message"], `tool "lookup" arguments:`)
		}
		require.Zero(t, calls)
		require.Equal(t, 4, authorized)
	})
	t.Run("自定义校验只有 ArgumentError 可恢复", func(t *testing.T) {
		var verr error
		tool := &argTestTool{BaseTool: NewBaseTool("v", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		}), validate: func(*interfaces.ToolInput) error { return verr }}
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		in := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{"id": "x"}} }
		verr = &ArgumentError{Message: "id 不存在"}
		out, err := modelCall(ctx, d, "v", in())
		requireArgumentResult(t, out, err, "id 不存在")
		verr = errors.New("validator backend down")
		out, err = modelCall(ctx, d, "v", in())
		require.Nil(t, out)
		require.EqualError(t, err, "validator backend down")
	})
	t.Run("工具返回的错误", func(t *testing.T) {
		var terr error
		tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return nil, terr
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		invoke := func(e error) (*interfaces.ToolOutput, error) {
			terr = e
			return modelCall(ctx, d, "op", &interfaces.ToolInput{Args: map[string]any{}})
		}
		out, err := invoke(fmt.Errorf("decode: %w", &ArgumentError{Message: "date 格式错误", Err: errors.New("parse internals")}))
		requireArgumentResult(t, out, err, "date 格式错误")
		for _, fatal := range []error{
			errors.New("upstream"),
			errors.Join(&ArgumentError{Message: "x"}, errors.New("cleanup failed")),
			ErrNestedExecutionLimit,
			ErrDispatcherReentry,
			&ArgumentError{Message: "x", Err: ErrNestedExecutionLimit},
		} {
			out, err = invoke(fatal)
			require.Nil(t, out, fatal)
			require.Error(t, err, fatal)
		}
	})
	t.Run("授权拒绝仍中止", func(t *testing.T) {
		tool := NewBaseTool("op", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true, Authorize: func(context.Context, string, *interfaces.ToolInput) error {
			return &ArgumentError{Message: "denied"}
		}}, tool)
		out, err := modelCall(ctx, d, "op", &interfaces.ToolInput{Args: map[string]any{"id": "x"}})
		require.Nil(t, out)
		require.EqualError(t, err, "denied")
	})
	t.Run("授权拒绝先于参数错误结果", func(t *testing.T) {
		calls, authorized := 0, 0
		tool := NewBaseTool("op", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			calls++
			return &interfaces.ToolOutput{Success: true}, nil
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true, Authorize: func(context.Context, string, *interfaces.ToolInput) error {
			authorized++
			return errors.New("denied")
		}}, tool)
		out, err := modelCall(ctx, d, "op", &interfaces.ToolInput{Args: map[string]any{}})
		require.Nil(t, out)
		require.EqualError(t, err, "denied")
		require.Equal(t, 1, authorized)
		require.Zero(t, calls)
	})
	t.Run("上下文结束时不转换", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			cancel()
			return nil, &ArgumentError{Message: "x"}
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		out, err := modelCall(cctx, d, "op", &interfaces.ToolInput{Args: map[string]any{}})
		require.Nil(t, out)
		require.Error(t, err)
	})
	t.Run("未知工具与缺少参数对象仍中止", func(t *testing.T) {
		tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		_, err := modelCall(ctx, d, "missing", &interfaces.ToolInput{Args: map[string]any{}})
		require.ErrorContains(t, err, "unknown tool")
		_, err = modelCall(ctx, d, "op", &interfaces.ToolInput{})
		require.ErrorContains(t, err, "requires object arguments")
	})
}

// 参数错误按已完成写入日志：恢复时回放同一结果、不重复执行。意图与开关无关，
// 开关切换后合法调用仍可回放；参数错误记录在未开启恢复的路径上回放则中止。
func TestDispatcherArgumentErrorJournal(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "exec"), store)
	calls := 0
	tool := NewBaseTool("op", "test", idSchema, func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		if in.Args["id"] == "bad" {
			return nil, &ArgumentError{Message: "id 不存在"}
		}
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	})
	on := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	off := argDispatcher(t, DispatchConfig{}, tool)
	in := func(id, callID string) *interfaces.ToolInput {
		return &interfaces.ToolInput{CallID: callID, Args: map[string]any{"id": id}}
	}
	for range 2 {
		out, err := modelCall(ctx, on, "op", in("bad", "c1"))
		requireArgumentResult(t, out, err, "id 不存在")
	}
	require.Equal(t, 1, calls, "第二次为回放")
	for _, path := range []func() (*interfaces.ToolOutput, error){
		func() (*interfaces.ToolOutput, error) { return modelCall(ctx, off, "op", in("bad", "c1")) },
		func() (*interfaces.ToolOutput, error) {
			return on.Invoke(ctx, ToolRequest{Name: "op", Input: in("bad", "c1")})
		},
	} {
		out, err := path()
		require.Nil(t, out)
		require.ErrorContains(t, err, "argument recovery is not enabled")
	}
	require.Equal(t, 1, calls)

	out, err := modelCall(ctx, on, "op", in("x", "c2"))
	require.NoError(t, err)
	require.True(t, out.Success)
	out, err = modelCall(ctx, off, "op", in("y", "c3"))
	require.NoError(t, err)
	require.True(t, out.Success)
	require.Equal(t, 3, calls)
	out, err = modelCall(ctx, off, "op", in("x", "c2"))
	require.NoError(t, err, "开启时写下的合法调用关闭后可回放")
	require.Equal(t, "ok", out.Result)
	out, err = modelCall(ctx, on, "op", in("y", "c3"))
	require.NoError(t, err, "关闭时写下的合法调用开启后可回放")
	require.Equal(t, "ok", out.Result)
	require.Equal(t, 3, calls)

	// 关闭时失败按 RetryFailed 重试。
	for range 2 {
		_, err = modelCall(ctx, off, "op", in("bad", "c4"))
		require.EqualError(t, err, "id 不存在")
	}
	require.Equal(t, 5, calls)
}

// 校验依赖外部状态：首次校验失败的结果写入日志；恢复时即便校验已能通过也回放原结果，不执行工具。
func TestDispatcherValidationArgumentErrorJournaled(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "exec"), store)
	calls, exists := 0, false
	tool := &argTestTool{BaseTool: NewBaseTool("v", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return &interfaces.ToolOutput{Success: true, Result: "created"}, nil
	}), validate: func(*interfaces.ToolInput) error {
		if !exists {
			return &ArgumentError{Message: "id 不存在"}
		}
		return nil
	}}
	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	in := &interfaces.ToolInput{CallID: "c1", Args: map[string]any{"id": "x"}}
	out, err := modelCall(ctx, d, "v", in)
	requireArgumentResult(t, out, err, "id 不存在")
	exists = true
	again, err := modelCall(ctx, d, "v", in)
	requireArgumentResult(t, again, err, "id 不存在")
	require.Zero(t, calls)
}

// 开关只作用于模型调用路径：Invoke 与 AsRunnable 仍以 error 报告参数错误。
func TestDispatcherHostPathsIgnoreArgumentRecovery(t *testing.T) {
	tool := NewBaseTool("op", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, &ArgumentError{Message: "id 不存在"}
	})
	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	for _, args := range []map[string]any{{}, {"id": "x"}} {
		req := ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: args}}
		out, err := d.Invoke(context.Background(), req)
		require.Nil(t, out)
		var arg *ArgumentError
		require.ErrorAs(t, err, &arg)
		out, err = d.AsRunnable().Invoke(context.Background(), req)
		require.Nil(t, out)
		require.ErrorAs(t, err, &arg)
	}
}

// 校验阶段与执行阶段交还的参数错误发出相同的事件。
func TestDispatcherArgumentErrorEvents(t *testing.T) {
	tool := NewBaseTool("op", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, &ArgumentError{Message: "id 不存在"}
	})
	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	for _, args := range []map[string]any{{}, {"id": "x"}} {
		var events []string
		out, err := d.invoke(context.Background(), ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: args}}, func(e *core.StreamEvent) error {
			events = append(events, e.Type)
			return nil
		}, true)
		require.NoError(t, err)
		require.False(t, out.Success)
		require.Equal(t, []string{"tool_start", "tool_end"}, events, args)
	}
}

func marked(message string) error {
	return agentErrors.New(agentErrors.CodeInvalidInput, message).MarkModelArgument()
}

// ValidationError 的类型与 Error() 文本与引入恢复前一致；仅在开启恢复的模型调用路径上交还 message。
func TestValidationErrorIsModelArgument(t *testing.T) {
	const legacy = "[INVALID_INPUT] [fetch] operation=run: url 必填 (field=url)"
	resp := NewToolErrorResponse("fetch").WithOperation("run")
	out, verr := resp.ValidationError("url 必填", "field", "url")
	require.Equal(t, &interfaces.ToolOutput{Success: false, Error: "url 必填"}, out)
	agentErr, ok := verr.(*agentErrors.AgentError)
	require.True(t, ok, "仍可直接类型断言")
	require.Equal(t, legacy, verr.Error())
	require.Equal(t, agentErrors.CodeInvalidInput, agentErr.Code)
	require.Nil(t, agentErr.Cause)
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.ErrorWithRetry(verr, 1, 3).Code)

	tool := NewBaseTool("fetch", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return resp.ValidationError("url 必填", "field", "url")
	})
	in := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{}} }

	off := argDispatcher(t, DispatchConfig{}, tool)
	out, err := modelCall(context.Background(), off, "fetch", in())
	require.Nil(t, out)
	require.EqualError(t, err, legacy, "关闭时错误文本不变")
	require.ErrorAs(t, err, &agentErr)

	on := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	out, err = modelCall(context.Background(), on, "fetch", in())
	requireArgumentResult(t, out, err, "url 必填")
	out, err = on.Invoke(context.Background(), ToolRequest{Name: "fetch", Input: in()})
	require.Nil(t, out)
	require.EqualError(t, err, legacy, "宿主路径不受开关影响")
}

// 未经 ValidationError 标记的 CodeInvalidInput 多为宿主侧问题（越权、配置、上游 400），开启后仍中止。
func TestUnmarkedInvalidInputIsFatal(t *testing.T) {
	for _, hostErr := range []error{
		agentErrors.New(agentErrors.CodeInvalidInput, "access to namespace is not allowed").WithComponent("tool_runtime"),
		agentErrors.NewInvalidInputError("openai", "request", "Bad Request"),
		agentErrors.Wrap(errors.New("bad regex"), agentErrors.CodeInvalidInput, "invalid regex pattern"),
	} {
		tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: false, Error: "x"}, hostErr
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		out, err := modelCall(context.Background(), d, "op", &interfaces.ToolInput{Args: map[string]any{}})
		require.Nil(t, out)
		require.EqualError(t, err, hostErr.Error())
	}
}

// 嵌套：内层交还的参数错误结果只在外层也走恢复路径时透传；内层报告的错误一律不被外层恢复。
func TestNestedDispatcherArgumentErrors(t *testing.T) {
	ctx := context.Background()
	inner := func(recover bool) interfaces.Tool {
		tool := NewBaseTool("lookup", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return nil, &ArgumentError{Message: "未知 id"}
		})
		return argDispatcher(t, DispatchConfig{ReturnArgumentErrors: recover}, tool).Tools()[0]
	}
	in := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{"id": "x"}} }

	outer := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, inner(true))
	out, err := modelCall(ctx, outer, "lookup", in())
	requireArgumentResult(t, out, err, "未知 id")

	out, err = outer.Invoke(ctx, ToolRequest{Name: "lookup", Input: in()})
	require.Nil(t, out)
	require.EqualError(t, err, `tool "lookup" failed: 未知 id`, "宿主路径致命且带上消息")

	outer = argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, inner(false))
	out, err = modelCall(ctx, outer, "lookup", in())
	require.Nil(t, out)
	require.EqualError(t, err, "未知 id")
}

// 组合工具子会话里内层 Dispatcher 的参数错误属于内层模型或宿主，外层模型传参合法，必须中止。
func TestRunnableChildArgumentErrorIsFatalForOuter(t *testing.T) {
	inner := argDispatcher(t, DispatchConfig{}, NewBaseTool("lookup", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true}, nil
	})).Tools()[0]
	child := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, n int, emit func(core.StreamChunk[int]) error) error {
		if _, err := inner.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{}}); err != nil {
			return fmt.Errorf("child: %w", err)
		}
		return emit(core.StreamChunk[int]{Done: true, Data: n})
	})
	composite, err := NewRunnableTool(child, runnableConfig())
	require.NoError(t, err)
	outer := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, composite)
	out, err := modelCall(context.Background(), outer, "increment", &interfaces.ToolInput{Args: map[string]any{"n": float64(1)}})
	require.Nil(t, out)
	require.ErrorContains(t, err, `tool "lookup" arguments:`)
	var arg *ArgumentError
	require.ErrorAs(t, err, &arg, "定性不影响 errors.As")
}

// 组合工具：解码前发现的参数问题可交还模型，且不创建子会话；子会话失败和深度超限仍中止。
func TestRunnableToolArgumentErrors(t *testing.T) {
	cfg := runnableConfig()
	cfg.Decode = func(args map[string]any) (int, error) {
		n := args["n"].(float64)
		if n < 0 {
			return 0, fmt.Errorf("decode: %w", &ArgumentError{Message: "n 不能为负数"})
		}
		if n > 100 {
			return 0, errors.New("codec backend down")
		}
		return int(n), nil
	}
	created := 0
	fail := false
	tool, err := NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
		created++
		runs := 0
		return OwnedRunnable[int, int]{Runnable: numberRun(&runs, fail), Close: func(context.Context) error { return nil }}, nil
	}, cfg)
	require.NoError(t, err)

	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": "1"}})
	var arg *ArgumentError
	require.ErrorAs(t, err, &arg, "Schema 失败")
	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"n": float64(-1)}})
	require.ErrorAs(t, err, &arg, "Decode 失败")
	require.Zero(t, created)

	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	invoke := func(ctx context.Context, n any) (*interfaces.ToolOutput, error) {
		return modelCall(ctx, d, "increment", &interfaces.ToolInput{Args: map[string]any{"n": n}})
	}
	out, err := invoke(context.Background(), float64(-1))
	requireArgumentResult(t, out, err, "n 不能为负数")
	require.Zero(t, created)
	out, err = invoke(context.Background(), float64(101))
	require.Nil(t, out)
	require.EqualError(t, err, "codec backend down", "Decode 的普通错误仍然致命")
	require.Zero(t, created)
	out, err = invoke(context.Background(), float64(1))
	require.NoError(t, err)
	require.True(t, out.Success)
	require.Equal(t, 1, created)

	fail = true
	out, err = invoke(context.Background(), float64(1))
	require.Nil(t, out)
	require.EqualError(t, err, "execution failed")

	deep := context.Background()
	for range 8 {
		deep, err = enterRunnable(deep, 8, false)
		require.NoError(t, err)
	}
	out, err = invoke(deep, float64(-1))
	require.Nil(t, out)
	require.ErrorIs(t, err, ErrNestedExecutionLimit, "深度检查先于参数校验")
}

// 子会话返回的错误原样交给外层：组合工具不包装、errors.Is 可识别，外层模型路径不恢复。
func TestRunnableChildErrorsSettled(t *testing.T) {
	in := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{"n": float64(1)}} }
	for _, cause := range []error{context.Canceled, ErrNestedExecutionLimit, &ArgumentError{Message: "内层"}, marked("内层")} {
		child := core.NewStreamRunnable(core.StreamModeFinal, func(context.Context, int, func(core.StreamChunk[int]) error) error {
			return cause
		})
		tool, err := NewRunnableTool(child, runnableConfig())
		require.NoError(t, err)
		out, err := modelCall(context.Background(), argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool), "increment", in())
		require.Nil(t, out)
		require.ErrorIs(t, err, cause)

		// 工厂错误不经过 StreamRunnable，可验证同一个值穿过组合工具与外层 Dispatcher。
		tool, err = NewRunnableToolFactory(func(context.Context) (OwnedRunnable[int, int], error) {
			return OwnedRunnable[int, int]{}, cause
		}, runnableConfig())
		require.NoError(t, err)
		_, err = tool.Invoke(context.Background(), in())
		require.True(t, err == cause, "组合工具不包装子会话错误")
		out, err = modelCall(context.Background(), argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool), "increment", in())
		require.Nil(t, out)
		require.True(t, err == cause, "外层不恢复且不包装")
	}
}

// 经 Dispatcher 返回的工具错误与工具原始返回值是同一个值，可直接类型断言和 == 比较。
func TestDispatcherReturnsToolErrorUnchanged(t *testing.T) {
	sentinel := errors.New("sentinel")
	agentErr := agentErrors.New(agentErrors.CodeToolExecution, "upstream down")
	argErr := &ArgumentError{Message: "id 不存在"}
	var terr error
	tool := NewBaseTool("op", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, terr
	})
	in := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{"id": "x"}} }
	for _, cfg := range []DispatchConfig{{}, {ReturnArgumentErrors: true}} {
		d := argDispatcher(t, cfg, tool)
		paths := map[string]func() (*interfaces.ToolOutput, error){
			"Invoke": func() (*interfaces.ToolOutput, error) {
				return d.Invoke(context.Background(), ToolRequest{Name: "op", Input: in()})
			},
			"Tools()": func() (*interfaces.ToolOutput, error) { return d.Tools()[0].Invoke(context.Background(), in()) },
		}
		for name, call := range paths {
			terr = sentinel
			_, err := call()
			require.True(t, err == sentinel, name)
			terr = agentErr
			_, err = call()
			got, ok := err.(*agentErrors.AgentError)
			require.True(t, ok, name)
			require.Same(t, agentErr, got, name)
			if name == "Invoke" || !cfg.ReturnArgumentErrors {
				terr = argErr
				_, err = call()
				require.True(t, err == argErr, name)
			}
		}
	}
}

// 外层开、内层关：内层参数错误原样到达外层调用方且致命，无论工具直接转交还是自行包装。
func TestNestedArgumentErrorIdentity(t *testing.T) {
	ctx := context.Background()
	innerArg := &ArgumentError{Message: "未知 id"}
	inner := argDispatcher(t, DispatchConfig{}, NewBaseTool("lookup", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, innerArg
	}))
	outer := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, inner.Tools()[0])
	_, err := modelCall(ctx, outer, "lookup", &interfaces.ToolInput{Args: map[string]any{"id": "x"}})
	require.True(t, err == innerArg)

	// 普通工具代码调用内层 Dispatcher 并自行包装错误。
	forward := NewBaseTool("forward", "test", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		_, err := inner.Invoke(ctx, ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{"id": "x"}}})
		return nil, fmt.Errorf("forward: %w", err)
	})
	outer = argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, forward)
	out, err := modelCall(ctx, outer, "forward", &interfaces.ToolInput{Args: map[string]any{}})
	require.Nil(t, out)
	require.EqualError(t, err, "forward: 未知 id")
	require.True(t, errors.Unwrap(err) == innerArg)
}

// 登记集合按外层调用隔离：并发调用互不影响；同一调用内并发的内层错误都会登记。
// 同一个哨兵在同一调用里既被内层返回又被工具自己返回时保守地视为内层错误。
func TestSettledSetConcurrency(t *testing.T) {
	ctx := context.Background()
	shared := &ArgumentError{Message: "共享哨兵"}
	inner := argDispatcher(t, DispatchConfig{MaxConcurrency: 8}, NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return nil, shared
	}))
	tool := NewBaseTool("op", "test", `{"type":"object","properties":{"mode":{"type":"string"}}}`, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		switch in.Args["mode"] {
		case "inner":
			errs := make([]error, 4)
			var wg sync.WaitGroup
			for i := range errs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, errs[i] = inner.Invoke(ctx, ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{}}})
				}()
			}
			wg.Wait()
			return nil, errors.Join(errs...)
		case "reuse":
			_, _ = inner.Invoke(ctx, ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: map[string]any{}}})
			return nil, shared
		}
		return nil, shared
	})
	outer := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true, MaxConcurrency: 16}, tool)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mode := []string{"inner", "self"}[i%2]
			out, err := modelCall(ctx, outer, "op", &interfaces.ToolInput{Args: map[string]any{"mode": mode}})
			if mode == "self" {
				assert.NoError(t, err)
				assert.Equal(t, "invalid_arguments", out.Result.(map[string]any)["error"])
				return
			}
			assert.Nil(t, out)
			assert.ErrorIs(t, err, shared)
		}()
	}
	wg.Wait()
	out, err := modelCall(ctx, outer, "op", &interfaces.ToolInput{Args: map[string]any{"mode": "reuse"}})
	require.Nil(t, out)
	require.True(t, err == shared)
}

// 只透传直接注册的内层 Dispatcher 工具的参数错误结果；其他工具返回同形状结果按失败中止。
func TestArgumentResultShapeNotTrusted(t *testing.T) {
	spoof := NewBaseTool("op", "t", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: false, Result: map[string]any{"error": "invalid_arguments", "message": "backend quota exceeded"}}, nil
	})
	out, err := modelCall(context.Background(), argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, spoof), "op", &interfaces.ToolInput{Args: map[string]any{}})
	require.Nil(t, out)
	require.ErrorContains(t, err, "backend quota exceeded")

	inner := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, NewBaseTool("lookup", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	// 工具用宿主推导的参数调用内层模型路径并原样返回结果，外层模型没有传过这些参数。
	fwd := NewBaseTool("fwd", "t", `{"type":"object"}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return inner.Tools()[0].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"id": ""}})
	})
	out, err = modelCall(context.Background(), argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, fwd), "fwd", &interfaces.ToolInput{Args: map[string]any{}})
	require.Nil(t, out)
	require.ErrorContains(t, err, `tool "fwd" failed`)
}

// 参数不合法时 Authorize 仍会执行；它的 panic 单独报告，不算作工具 panic。
func TestAuthorizePanicReported(t *testing.T) {
	tool := NewBaseTool("op", "t", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true}, nil
	})
	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true, Authorize: func(_ context.Context, _ string, in *interfaces.ToolInput) error {
		_ = in.Args["id"].(string)
		return nil
	}}, tool)
	out, err := modelCall(context.Background(), d, "op", &interfaces.ToolInput{Args: map[string]any{}})
	require.Nil(t, out)
	require.EqualError(t, err, `tool "op" authorize panicked`)
}

func TestDispatcherNilContext(t *testing.T) {
	d := argDispatcher(t, DispatchConfig{}, NewBaseTool("op", "t", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true}, nil
	}))
	//nolint:staticcheck // 验证 nil ctx 不会让 panic 逃出 Dispatcher
	out, err := d.Invoke(nil, ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.Nil(t, out)
	require.EqualError(t, err, `tool "op" panicked`)
	require.Nil(t, Settle(nil, nil))
}

func TestArgumentErrorResultRecognizesOnlyArgumentShape(t *testing.T) {
	message, ok := ArgumentErrorResult(&interfaces.ToolOutput{Result: map[string]any{"error": "invalid_arguments", "message": "缺少 id"}})
	require.True(t, ok)
	require.Equal(t, "缺少 id", message)
	for _, out := range []*interfaces.ToolOutput{
		nil,
		{Success: true, Result: map[string]any{"error": "invalid_arguments"}},
		{Error: "失败", Result: map[string]any{"error": "invalid_arguments"}},
		{Result: map[string]any{"error": "other"}},
		{Result: "invalid_arguments"},
	} {
		_, ok := ArgumentErrorResult(out)
		require.False(t, ok)
	}
}
