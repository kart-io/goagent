package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type chainFastAgent struct {
	*MockAgent
	fast func(context.Context, *AgentInput) (*AgentOutput, error)
}

func (a *chainFastAgent) InvokeFast(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	return a.fast(ctx, input)
}

func newChainFastAgent(run func(context.Context, *AgentInput) (*AgentOutput, error)) *chainFastAgent {
	return &chainFastAgent{MockAgent: NewMockAgent("节点", "链回归节点", []string{"测试"}, run), fast: run}
}

// 第三节点清空继承字段或归还仍被引用的输入时，本测试必须失败。
func TestChainableAgentPreservesThreeStepInput(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(map[bool]string{false: "普通入口", true: "快速入口"}[fast], func(t *testing.T) {
			var retained []*AgentInput
			node := newChainFastAgent(func(_ context.Context, input *AgentInput) (*AgentOutput, error) {
				require.Equal(t, "任务", input.Task)
				require.Equal(t, "指令", input.Instruction)
				require.Equal(t, "会话", input.SessionID)
				require.Equal(t, 123, input.Options.MaxTokens)
				require.Equal(t, []string{"工具"}, input.Options.AllowedTools)
				if len(retained) > 0 {
					require.Equal(t, "下一步", input.Context["数据"])
				}
				retained = append(retained, input)
				return &AgentOutput{Status: "success", Result: "完成", Metadata: map[string]any{"数据": "下一步"}}, nil
			})
			chain := NewChainableAgent("链", "说明", node, node, node, node)
			input := &AgentInput{Task: "任务", Instruction: "指令", SessionID: "会话", Options: AgentOptions{MaxTokens: 123, AllowedTools: []string{"工具"}}}
			invoke := chain.Invoke
			if fast {
				invoke = chain.InvokeFast
			}
			out, err := invoke(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, "完成", out.Result)
			require.Len(t, retained, 4)
			for _, seen := range retained {
				require.Equal(t, "任务", seen.Task, "已交给节点的输入不得被后续步骤清空")
			}
		})
	}
}

// 普通入口误选提升的 InvokeFast 时，只实现 Invoke 的业务节点无法执行。
func TestChainableAgentInvokesOrdinaryAgent(t *testing.T) {
	node := NewMockAgent("节点", "仅实现普通入口", nil, func(_ context.Context, input *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{Status: "success", Result: input.Task}, nil
	})
	out, err := NewChainableAgent("链", "说明", node).Invoke(context.Background(), &AgentInput{Task: "任务"})
	require.NoError(t, err)
	require.Equal(t, "任务", out.Result)
}

// 嵌套链只有显式快速调用可以选择节点的快速入口并跳过链回调。
func TestChainableAgentExplicitFastPath(t *testing.T) {
	node := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{Status: "success", Result: "普通"}, nil
	})
	node.fast = func(context.Context, *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{Status: "success", Result: "快速"}, nil
	}
	cb := &streamCallback{BaseCallback: NewBaseCallback()}
	inner := NewChainableAgent("内链", "说明", node).WithCallbacks(cb).(*ChainableAgent)
	chain := NewChainableAgent("外链", "说明", inner).WithCallbacks(cb).(*ChainableAgent)
	out, err := chain.Invoke(context.Background(), &AgentInput{})
	require.NoError(t, err)
	require.Equal(t, "普通", out.Result)
	require.EqualValues(t, 2, cb.starts.Load())
	require.EqualValues(t, 2, cb.ends.Load())
	out, err = chain.InvokeFast(context.Background(), &AgentInput{})
	require.NoError(t, err)
	require.Equal(t, "快速", out.Result)
	require.EqualValues(t, 2, cb.starts.Load())
	require.EqualValues(t, 2, cb.ends.Load())
}

// 任何标准入口绑定 BaseAgent 或配置副本丢失链时，本测试必须失败。
func TestChainableAgentRunnableEntrypoints(t *testing.T) {
	for _, entry := range []string{"Invoke", "Stream", "Batch", "Pipe", "WithConfig", "WithCallbacks", "RunGenerator"} {
		t.Run(entry, func(t *testing.T) {
			node := newChainFastAgent(func(_ context.Context, input *AgentInput) (*AgentOutput, error) {
				return &AgentOutput{Status: "success", Result: input.Task + "完成"}, nil
			})
			chain := NewChainableAgent("链", "说明", node)
			input := &AgentInput{Task: "任务"}
			ctx := context.Background()
			var output *AgentOutput
			var err error
			switch entry {
			case "Invoke":
				output, err = chain.Invoke(ctx, input)
			case "Stream":
				var stream <-chan StreamChunk[*AgentOutput]
				stream, err = chain.Stream(ctx, input)
				require.NoError(t, err)
				var chunks []StreamChunk[*AgentOutput]
				for chunk := range stream {
					chunks = append(chunks, chunk)
				}
				require.Len(t, chunks, 1)
				require.True(t, chunks[0].Done)
				output, err = chunks[0].Data, chunks[0].Error
			case "Batch":
				var outputs []*AgentOutput
				outputs, err = chain.Batch(ctx, []*AgentInput{input, {Task: "另一任务"}})
				require.NoError(t, err)
				require.Len(t, outputs, 2)
				require.Equal(t, "另一任务完成", outputs[1].Result)
				output = outputs[0]
			case "Pipe":
				pipe := chain.Pipe(NewRunnableFunc(func(_ context.Context, out *AgentOutput) (any, error) { return out.Result, nil }))
				result, pipeErr := pipe.Invoke(ctx, input)
				require.NoError(t, pipeErr)
				require.Equal(t, "任务完成", result)
				return
			case "WithConfig":
				configured := chain.WithConfig(RunnableConfig{MaxConcurrency: 1})
				output, err = configured.Invoke(ctx, input)
			case "WithCallbacks":
				configured := chain.WithCallbacks(NewBaseCallback())
				output, err = configured.Invoke(ctx, input)
			case "RunGenerator":
				count := 0
				for out, runErr := range chain.RunGenerator(ctx, input) {
					output, err = out, runErr
					count++
				}
				require.Equal(t, 1, count)
			}
			require.NoError(t, err)
			require.NotNil(t, output)
			require.Equal(t, "任务完成", output.Result)
		})
	}
}

type chainRejectCallback struct {
	*BaseCallback
	err error
}

func (c chainRejectCallback) OnStart(context.Context, any) error { return c.err }

// 普通链绕过节点或自身的拒绝回调时，业务效果将错误地发生。
func TestChainableAgentPreservesStandardCallbacks(t *testing.T) {
	rejected := errors.New("回调拒绝")
	for _, scope := range []string{"链", "节点"} {
		t.Run(scope, func(t *testing.T) {
			calls := 0
			cb := chainRejectCallback{BaseCallback: NewBaseCallback(), err: rejected}
			node := newChainFastAgent(func(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
				calls++
				return &AgentOutput{Status: "success"}, nil
			})
			if scope == "节点" {
				node.executeFunc = func(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
					if err := cb.OnStart(ctx, input); err != nil {
						return nil, err
					}
					calls++
					return &AgentOutput{Status: "success"}, nil
				}
			}
			var chain Runnable[*AgentInput, *AgentOutput] = NewChainableAgent("链", "说明", node)
			if scope == "链" {
				chain = chain.WithCallbacks(cb)
			}
			_, err := chain.Invoke(context.Background(), &AgentInput{})
			require.ErrorIs(t, err, rejected)
			require.Zero(t, calls)
		})
	}
}

// 追加回调的链副本必须拥有独立配置容器，不能把改写泄漏到原链。
func TestChainableAgentCallbackCloneOwnsConfiguration(t *testing.T) {
	chain := NewChainableAgent("链", "说明").WithConfig(RunnableConfig{
		Tags: []string{"原标签"}, Metadata: map[string]any{"归属": "原链"},
	}).(*ChainableAgent)
	clone, ok := chain.WithCallbacks(NewBaseCallback()).(*ChainableAgent)
	require.True(t, ok, "配置副本必须保留完整 Agent 类型")
	clone.GetConfig().Tags[0] = "副本标签"
	clone.GetConfig().Metadata["归属"] = "副本"
	require.Equal(t, []string{"原标签"}, chain.GetConfig().Tags)
	require.Equal(t, "原链", chain.GetConfig().Metadata["归属"])
	require.Empty(t, chain.GetConfig().Callbacks)
}

// 节点失败时，各公开入口必须保留原因，且不得执行后继节点。
func TestChainableAgentFailureStopsFollowingNodes(t *testing.T) {
	failure := errors.New("节点失败")
	for _, entry := range []string{"Invoke", "InvokeFast", "Stream", "Batch", "Pipe", "RunGenerator"} {
		t.Run(entry, func(t *testing.T) {
			var calls atomic.Int32
			broken := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) { return nil, failure })
			tail := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
				calls.Add(1)
				return &AgentOutput{Status: "success"}, nil
			})
			cb := &streamCallback{BaseCallback: NewBaseCallback()}
			chain := NewChainableAgent("链", "说明", broken, tail).WithCallbacks(cb).(*ChainableAgent)
			ctx, input := context.Background(), &AgentInput{Task: "任务"}
			var err error
			switch entry {
			case "Invoke":
				_, err = chain.Invoke(ctx, input)
			case "InvokeFast":
				_, err = chain.InvokeFast(ctx, input)
			case "Stream":
				var stream <-chan StreamChunk[*AgentOutput]
				stream, err = chain.Stream(ctx, input)
				require.NoError(t, err)
				count := 0
				for chunk := range stream {
					require.True(t, chunk.Done)
					require.Nil(t, chunk.Data)
					err = chunk.Error
					count++
				}
				require.Equal(t, 1, count)
			case "Batch":
				_, err = chain.Batch(ctx, []*AgentInput{input})
			case "Pipe":
				_, err = chain.Pipe(NewRunnableFunc(func(_ context.Context, out *AgentOutput) (any, error) {
					calls.Add(1)
					return out, nil
				})).Invoke(ctx, input)
			case "RunGenerator":
				for _, err = range chain.RunGenerator(ctx, input) {
				}
			}
			require.ErrorIs(t, err, failure)
			require.Zero(t, calls.Load())
			if entry == "InvokeFast" {
				require.Zero(t, cb.starts.Load())
				require.Zero(t, cb.failures.Load())
			} else {
				require.EqualValues(t, 1, cb.starts.Load())
				require.EqualValues(t, 1, cb.failures.Load())
				require.Zero(t, cb.ends.Load())
			}
		})
	}
}

// 即使节点忽略取消并返回成功，链也必须停止，不能调用下一节点。
func TestChainableAgentCancellationStopsFollowingNodes(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(map[bool]string{false: "普通入口", true: "快速入口"}[fast], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			first := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
				cancel()
				return &AgentOutput{Status: "success"}, nil
			})
			tail := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
				calls++
				return &AgentOutput{Status: "success"}, nil
			})
			chain := NewChainableAgent("链", "说明", first, tail)
			invoke := chain.Invoke
			if fast {
				invoke = chain.InvokeFast
			}
			out, err := invoke(ctx, &AgentInput{})
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, out)
			require.Zero(t, calls)
		})
	}
}

// 空输出必须作为执行失败处理，不能 panic 或让后继节点得到伪成功。
func TestChainableAgentRejectsNilOutput(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(map[bool]string{false: "普通入口", true: "快速入口"}[fast], func(t *testing.T) {
			calls := 0
			empty := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) { return nil, nil })
			tail := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
				calls++
				return &AgentOutput{Status: "success"}, nil
			})
			chain := NewChainableAgent("链", "说明", empty, tail)
			invoke := chain.Invoke
			if fast {
				invoke = chain.InvokeFast
			}
			require.NotPanics(t, func() {
				out, err := invoke(context.Background(), &AgentInput{})
				require.Error(t, err)
				require.Nil(t, out)
			})
			require.Zero(t, calls)
		})
	}
}

// 构造方替换切片元素不能改变已经创建或配置过的链。
func TestChainableAgentOwnsNodeList(t *testing.T) {
	first := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{Status: "success", Result: "原节点"}, nil
	})
	replacement := newChainFastAgent(func(context.Context, *AgentInput) (*AgentOutput, error) {
		return &AgentOutput{Status: "success", Result: "替换节点"}, nil
	})
	nodes := []Agent{first}
	chain := NewChainableAgent("链", "说明", nodes...)
	configured := chain.WithConfig(RunnableConfig{MaxConcurrency: 1})
	nodes[0] = replacement
	for _, runner := range []Runnable[*AgentInput, *AgentOutput]{chain, configured} {
		out, err := runner.Invoke(context.Background(), &AgentInput{})
		require.NoError(t, err)
		require.Equal(t, "原节点", out.Result)
	}
}

// 并发执行与配置复制时，三个以上节点仍须获得各自调用的任务和会话。
func TestChainableAgentConcurrentConfigurationAndExecution(t *testing.T) {
	node := newChainFastAgent(func(_ context.Context, input *AgentInput) (*AgentOutput, error) {
		if input.Task != "任务" || input.SessionID != "会话" || input.Options.MaxTokens != 123 {
			return nil, errors.New("调用数据串扰")
		}
		return &AgentOutput{Status: "success", Result: input.Task}, nil
	})
	chain := NewChainableAgent("链", "说明", node, node, node, node)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			configured := chain.WithConfig(RunnableConfig{MaxConcurrency: 2}).WithCallbacks(NewBaseCallback())
			out, err := configured.Invoke(context.Background(), &AgentInput{Task: "任务", SessionID: "会话", Options: AgentOptions{MaxTokens: 123}})
			if err != nil {
				t.Errorf("并发调用失败：%v", err)
				return
			}
			if out.Result != "任务" {
				t.Errorf("并发调用结果错误：%v", out.Result)
			}
		})
	}
	wg.Wait()
}
