package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
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
	for name, c := range map[string]struct {
		err     error
		message string
		ok      bool
	}{
		"直接":              {a, "id 必填", true},
		"包装后取内层消息":        {fmt.Errorf("decode: %w", a), "id 必填", true},
		"全部分支可恢复":         {errors.Join(a, b), "id 必填; amount 必须为正数", true},
		"多重 %w":           {fmt.Errorf("%w / %w", a, b), "id 必填; amount 必须为正数", true},
		"混入普通错误":          {errors.Join(a, errors.New("cleanup failed")), "", false},
		"包装的 Join 混入普通错误": {fmt.Errorf("x: %w", errors.Join(a, errors.New("cleanup failed"))), "", false},
		"普通错误":            {errors.New("upstream"), "", false},
		"nil":             {nil, "", false},
		"空 Join":          {errors.Join(), "", false},
		"原因为深度超限":         {&ArgumentError{Message: "x", Err: ErrNestedExecutionLimit}, "", false},
		"原因为重入":           {&ArgumentError{Message: "x", Err: ErrDispatcherReentry}, "", false},
		"原因为等待宿主":         {&ArgumentError{Message: "x", Err: ErrToolWorkflowWaiting}, "", false},
		"与取消并列":           {errors.Join(a, context.Canceled), "", false},
		"原因为超时":           {&ArgumentError{Message: "x", Err: context.DeadlineExceeded}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			message, ok := argumentErrorMessage(c.err)
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.message, message)
		})
	}
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
	t.Run("Schema 校验失败不执行也不授权", func(t *testing.T) {
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
			out, err := d.Invoke(ctx, ToolRequest{Name: "lookup", Input: &interfaces.ToolInput{Args: args}})
			require.NoError(t, err, args)
			require.False(t, out.Success)
			require.Equal(t, "invalid_arguments", out.Result.(map[string]any)["error"])
			require.Contains(t, out.Result.(map[string]any)["message"], `tool "lookup" arguments:`)
		}
		require.Zero(t, calls)
		require.Zero(t, authorized)
	})
	t.Run("自定义校验只有 ArgumentError 可恢复", func(t *testing.T) {
		var verr error
		tool := &argTestTool{BaseTool: NewBaseTool("v", "test", idSchema, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		}), validate: func(*interfaces.ToolInput) error { return verr }}
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		in := func() *interfaces.ToolInput { return &interfaces.ToolInput{Args: map[string]any{"id": "x"}} }
		verr = &ArgumentError{Message: "id 不存在"}
		out, err := d.Invoke(ctx, ToolRequest{Name: "v", Input: in()})
		requireArgumentResult(t, out, err, "id 不存在")
		verr = errors.New("validator backend down")
		out, err = d.Invoke(ctx, ToolRequest{Name: "v", Input: in()})
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
			return d.Invoke(ctx, ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: map[string]any{}}})
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
		out, err := d.Invoke(ctx, ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: map[string]any{"id": "x"}}})
		require.Nil(t, out)
		require.EqualError(t, err, "denied")
	})
	t.Run("上下文结束时不转换", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			cancel()
			return nil, &ArgumentError{Message: "x"}
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		out, err := d.Invoke(cctx, ToolRequest{Name: "op", Input: &interfaces.ToolInput{Args: map[string]any{}}})
		require.Nil(t, out)
		require.Error(t, err)
	})
	t.Run("未知工具与缺少参数对象仍中止", func(t *testing.T) {
		tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		})
		d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
		_, err := d.Invoke(ctx, ToolRequest{Name: "missing", Input: &interfaces.ToolInput{Args: map[string]any{}}})
		require.ErrorContains(t, err, "unknown tool")
		_, err = d.Invoke(ctx, ToolRequest{Name: "op", Input: &interfaces.ToolInput{}})
		require.ErrorContains(t, err, "requires object arguments")
	})
}

// 参数错误按已完成写入日志：恢复时回放同一结果、不重复执行；切换开关会改变意图，拒绝复用。
func TestDispatcherArgumentErrorJournal(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "exec"), store)
	calls := 0
	tool := NewBaseTool("op", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		calls++
		return nil, &ArgumentError{Message: "id 不存在"}
	})
	req := ToolRequest{Name: "op", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{"id": "x"}}}
	on := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	for range 2 {
		out, err := on.Invoke(ctx, req)
		requireArgumentResult(t, out, err, "id 不存在")
	}
	require.Equal(t, 1, calls, "第二次为回放")
	off := argDispatcher(t, DispatchConfig{}, tool)
	_, err = off.Invoke(ctx, req)
	require.ErrorContains(t, err, "intent")
	require.Equal(t, 1, calls)

	// 关闭时意图与引入前一致：失败按 RetryFailed 重试，不受新字段影响。
	_, err = off.Invoke(ctx, ToolRequest{Name: "op", Input: &interfaces.ToolInput{CallID: "c2", Args: map[string]any{}}})
	require.EqualError(t, err, "id 不存在")
	_, err = off.Invoke(ctx, ToolRequest{Name: "op", Input: &interfaces.ToolInput{CallID: "c2", Args: map[string]any{}}})
	require.EqualError(t, err, "id 不存在")
	require.Equal(t, 3, calls)
}

// 组合工具：解码前发现的参数问题可交还模型，且不创建子会话；子会话失败和深度超限仍中止。
func TestRunnableToolArgumentErrors(t *testing.T) {
	cfg := runnableConfig()
	cfg.Decode = func(args map[string]any) (int, error) {
		n := args["n"].(float64)
		if n < 0 {
			return 0, fmt.Errorf("n 不能为负数")
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
	require.EqualError(t, err, "n 不能为负数")
	require.Zero(t, created)

	d := argDispatcher(t, DispatchConfig{ReturnArgumentErrors: true}, tool)
	invoke := func(ctx context.Context, n any) (*interfaces.ToolOutput, error) {
		return d.Invoke(ctx, ToolRequest{Name: "increment", Input: &interfaces.ToolInput{Args: map[string]any{"n": n}}})
	}
	out, err := invoke(context.Background(), float64(-1))
	requireArgumentResult(t, out, err, "n 不能为负数")
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
