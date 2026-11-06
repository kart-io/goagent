package agentbench

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/toolargs"
)

func receiptContract(stringsOnly bool) ToolContract {
	return ToolContract{ID: "9007199254740993", Rate: "0.1234567890123456789", Label: "<测试&>", Receipt: "receipt-test-91", StringNumbers: stringsOnly}
}

func TestToolContractRejectsRoundedAndWrongArguments(t *testing.T) {
	for _, tc := range []struct {
		name, raw       string
		stringsOnly, ok bool
	}{
		{"数值保持精度", `{"id":9007199254740993,"rate":0.1234567890123456789,"label":"<测试&>"}`, false, true},
		{"历史MiniMax小数已缩短", `{"id":9007199254740993,"rate":0.12345678901234568,"label":"<测试&>"}`, false, false},
		{"字符串保持精度", `{"id":"9007199254740993","rate":"0.1234567890123456789","label":"<测试&>"}`, true, true},
		{"字符串Schema拒绝数值", `{"id":9007199254740993,"rate":0.12345678901234568,"label":"<测试&>"}`, true, false},
		{"错误字符串不能通过", `{"id":"9007199254740993","rate":"0.12345678901234568","label":"<测试&>"}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract := receiptContract(tc.stringsOnly)
			effects := 0
			registry := tools.NewRegistry()
			require.NoError(t, registry.Register(tools.NewBaseTool("verify", "合成核验", contract.Schema(), func(_ context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				require.NoError(t, contract.CheckArguments(input))
				effects++
				return &interfaces.ToolOutput{Success: true, Result: contract.Receipt}, nil
			})))
			dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{Authorize: func(_ context.Context, _ string, input *interfaces.ToolInput) error {
				return contract.CheckArguments(input)
			}})
			require.NoError(t, err)
			args, ok := toolargs.Parse(tc.raw)
			require.True(t, ok)
			_, err = dispatcher.Invoke(context.Background(), tools.ToolRequest{Name: "verify", Input: &interfaces.ToolInput{Args: args}})
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, 1, effects)
			} else {
				require.Error(t, err)
				require.Zero(t, effects)
			}
		})
	}
}

func TestToolContractCorrectsOnceAndReplaysWithoutEffects(t *testing.T) {
	contract := receiptContract(true)
	model := mocks.NewScriptedClient(
		mocks.ToolCallReply("调用工具", mocks.ToolCall("c1", "verify", `{"id":"9007199254740993","rate":"0.1234567890123456789","label":"<测试&>"}`)),
		mocks.TextReply("历史格式失败", `{"receipt":"receipt-test-91","id":"9007199254740993"}`),
		mocks.TextReply("按原契约纠正", "receipt-test-91"),
	)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	effects := 0
	for range 2 {
		registry := tools.NewRegistry()
		require.NoError(t, registry.Register(tools.NewBaseTool("verify", "核验", contract.Schema(), func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			effects++
			return &interfaces.ToolOutput{Success: true, Result: map[string]string{"receipt": contract.Receipt}}, nil
		})))
		d, err := tools.NewDispatcher(registry, tools.DispatchConfig{Authorize: func(_ context.Context, _ string, in *interfaces.ToolInput) error { return contract.CheckArguments(in) }})
		require.NoError(t, err)
		ctrl, err := budget.NewDurable(budget.Config{RootKey: "receipt", Version: "v2", MaxModelRequests: 3, MaxToolCalls: 1}, store)
		require.NoError(t, err)
		ctx := durable.WithJournal(durable.WithExecutionKey(budget.WithController(context.Background(), ctrl), "receipt"), store)
		a, err := builder.NewSimpleBuilder(model).WithDurableScope("strict-v2").WithTools(d.Tools()...).WithMaxIterations(5).WithTurnStopping(contract.Stop).Build()
		require.NoError(t, err)
		out, err := a.AsRunnable().Invoke(ctx, "核验后只输出回执")
		require.NoError(t, err)
		require.NoError(t, contract.CheckAnswer(out.Result))
		require.Equal(t, "receipt-test-91", out.Result)
		require.Equal(t, 1, out.Metadata["stopping_continued"])
		require.Nil(t, out.Metadata["turn_stopping_skipped"])
	}
	require.Equal(t, 1, effects)
	require.Len(t, model.Requests(), 3)
}

func TestToolContractFailsAfterOneCorrection(t *testing.T) {
	contract := receiptContract(true)
	model := mocks.NewScriptedClient(mocks.TextReply("错误答案", `{"receipt":"receipt-test-91"}`), mocks.TextReply("仍是错误格式", `{"receipt":"receipt-test-91"}`))
	a, err := builder.NewSimpleBuilder(model).WithTools(tools.NewBaseTool("unused", "未调用", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		t.Fatal("不应执行工具")
		return nil, nil
	})).WithMaxIterations(5).WithTurnStopping(contract.Stop).Build()
	require.NoError(t, err)
	out, err := a.AsRunnable().Invoke(context.Background(), "只输出回执")
	require.Error(t, err)
	require.Nil(t, out)
	require.Len(t, model.Requests(), 2)
	require.Error(t, contract.CheckAnswer(`{"receipt":"receipt-test-91"}`))
	require.NoError(t, contract.CheckAnswer("receipt-test-91"))
}
