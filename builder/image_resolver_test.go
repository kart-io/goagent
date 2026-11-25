package builder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
)

// imageResolverStub 记录每次解析请求，按 next 给出结果。
type imageResolverStub struct {
	mu    sync.Mutex
	calls []ImageRequest
	next  func(n int, req ImageRequest) (string, error)
}

func (r *imageResolverStub) resolve(_ context.Context, req ImageRequest) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	n := len(r.calls)
	r.mu.Unlock()
	return r.next(n, req)
}

func (r *imageResolverStub) requests() []ImageRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ImageRequest(nil), r.calls...)
}

func dataURL(req ImageRequest) string { return "data:image/png;base64," + req.Part.Ref }

func resolveTo(f func(ImageRequest) string) *imageResolverStub {
	return &imageResolverStub{next: func(_ int, req ImageRequest) (string, error) { return f(req), nil }}
}

func failWith(err error) *imageResolverStub {
	return &imageResolverStub{next: func(int, ImageRequest) (string, error) { return "", err }}
}

func imageRef(n int) llm.ImagePart {
	return llm.ImagePart{Ref: fmt.Sprintf("image:%d", n), MIME: "image/png", SHA256: fmt.Sprintf("sha%d", n)}
}

func resolvedImage(n int) llm.ImagePart {
	p := imageRef(n)
	p.URL, p.Ref = "data:image/png;base64,"+p.Ref, ""
	return p
}

// lastUser 返回请求中最后一条用户消息。
func lastUser(t *testing.T, req llm.CompletionRequest) llm.Message {
	t.Helper()
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == constants.RoleUser {
			return req.Messages[i]
		}
	}
	t.Fatal("请求中没有用户消息")
	return llm.Message{}
}

func imageAgent(t *testing.T, model llm.Client, store session.Store, r *imageResolverStub, p ImagePolicy, configure ...func(*AgentBuilder[*core.AgentState]) *AgentBuilder[*core.AgentState]) *ConfigurableAgent[*core.AgentState] {
	t.Helper()
	b := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(store).WithDurableScope("img")
	if r != nil {
		b = b.WithImageResolver(r.resolve, p)
	}
	for _, c := range configure {
		b = c(b)
	}
	agent, err := b.Build()
	require.NoError(t, err)
	return agent
}

func durableRun(store session.Store, key string) context.Context {
	return durable.WithJournal(durable.WithExecutionKey(context.Background(), key), session.DurableStore(store, "s"))
}

func TestResolverFillsURLOnlyInOutgoingRequest(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.ToolCallReply("查", mocks.ToolCall("c1", "lookup", "{}")), mocks.TextReply("好", "好"))
	r := resolveTo(dataURL)
	var stepImages, hookImages [][]llm.ImagePart
	agent := imageAgent(t, model, store, r, ImagePolicy{}, func(b *AgentBuilder[*core.AgentState]) *AgentBuilder[*core.AgentState] {
		return b.WithTools(plainTool()).
			WithPreStep(func(_ context.Context, s *Step) (PreStepResult, error) {
				for _, m := range s.Messages {
					if m.Role == constants.RoleUser {
						stepImages = append(stepImages, m.Images)
					}
				}
				return PreStepResult{}, nil
			}).
			WithRequestHook(func(_ context.Context, req *ModelRequest) error {
				hookImages = append(hookImages, lastUser(t, *req.Request).Images)
				return nil
			})
	})
	_, err := agent.AsRunnable().Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}})
	require.NoError(t, err)

	for _, sent := range model.Requests() {
		require.Equal(t, []llm.ImagePart{resolvedImage(1)}, lastUser(t, sent).Images, "每次真实请求都带解析后的 URL")
	}
	require.Len(t, r.requests(), 2, "每步请求各解析一次")
	require.Len(t, stepImages, 2)
	for _, images := range append(stepImages, hookImages...) {
		require.Equal(t, []llm.ImagePart{imageRef(1)}, images, "PreStep 与 Request 钩子只看到引用")
	}
	derived, err := session.DeriveMessages(sessionEvents(t, store, "s"))
	require.NoError(t, err)
	require.Equal(t, []llm.ImagePart{imageRef(1)}, derived[0].Images, "会话日志只存引用")
}

func TestResolverNotCalledWhenReplayingCompletedTurn(t *testing.T) {
	store := session.NewMemoryStore()
	r := resolveTo(dataURL)
	input := UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}}
	out, err := imageAgent(t, mocks.NewScriptedClient(mocks.TextReply("好", "看到了")), store, r, ImagePolicy{}).AsRunnable().Invoke(durableRun(store, "run-1"), input)
	require.NoError(t, err)
	require.Equal(t, "看到了", out.Result)
	require.Len(t, r.requests(), 1)

	// 图片已被删除：回放已完成的轮次不得解析，也不得请求模型。
	gone := failWith(ErrImageUnavailable)
	replay := mocks.NewScriptedClient()
	out, err = imageAgent(t, replay, store, gone, ImagePolicy{}).AsRunnable().Invoke(durableRun(store, "run-1"), input)
	require.NoError(t, err)
	require.Equal(t, "看到了", out.Result)
	require.Empty(t, gone.requests())
	require.Zero(t, replay.Calls())
}

func TestResolverURLChangeKeepsIntent(t *testing.T) {
	store := session.NewMemoryStore()
	r := &imageResolverStub{next: func(n int, req ImageRequest) (string, error) {
		return fmt.Sprintf("https://cdn.example/%s?sig=%d", req.Part.Ref, n), nil
	}}
	input := UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}}
	resend := func(b *AgentBuilder[*core.AgentState]) *AgentBuilder[*core.AgentState] {
		return b.WithInterruptedModelResends(1)
	}

	// 第一次请求挂起到进程停止，回合未落盘。
	started := make(chan struct{})
	stuck := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "挂起", Respond: func(ctx context.Context, _ *llm.CompletionRequest) (mocks.ScriptedReply, error) {
		close(started)
		<-ctx.Done()
		return mocks.ScriptedReply{}, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(durableRun(store, "run-1"))
	done := make(chan error, 1)
	go func() {
		_, err := imageAgent(t, stuck, store, r, ImagePolicy{}, resend).AsRunnable().Invoke(ctx, input)
		done <- err
	}()
	<-started
	cancel()
	require.Error(t, <-done)

	// 恢复时解析器给出新的签名 URL：意图只含引用，不冲突。
	model := mocks.NewScriptedClient(mocks.TextReply("好", "看到了"))
	out, err := imageAgent(t, model, store, r, ImagePolicy{}, resend).AsRunnable().Invoke(durableRun(store, "run-1"), input)
	require.NoError(t, err)
	require.Equal(t, "看到了", out.Result)
	require.Len(t, r.requests(), 2)
	require.Equal(t, "https://cdn.example/image:1?sig=1", lastUser(t, stuck.Requests()[0]).Images[0].URL)
	require.Equal(t, "https://cdn.example/image:1?sig=2", lastUser(t, model.Requests()[0]).Images[0].URL)
}

func TestUnavailableImageBecomesPlaceholder(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
	r := failWith(fmt.Errorf("图片行已删除：%w", ErrImageUnavailable))
	_, err := imageAgent(t, model, store, r, ImagePolicy{}).AsRunnable().Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}})
	require.NoError(t, err)

	sent := lastUser(t, model.Requests()[0])
	require.Empty(t, sent.Images)
	require.Equal(t, "看图\n[图片 image:1：不可用]", sent.Content)
	derived, err := session.DeriveMessages(sessionEvents(t, store, "s"))
	require.NoError(t, err)
	require.Equal(t, llm.Message{Role: constants.RoleUser, Content: "看图", Images: []llm.ImagePart{imageRef(1)}}, derived[0], "降级不改历史")
}

// runImageTurns 依次执行每轮各带一张图的输入，返回最后一次请求与该请求触发的解析。
func runImageTurns(t *testing.T, turns int, p ImagePolicy) (llm.CompletionRequest, []ImageRequest) {
	t.Helper()
	store := session.NewMemoryStore()
	var rules []mocks.ScriptedRule
	for i := range turns {
		rules = append(rules, mocks.TextReply(fmt.Sprint(i), fmt.Sprintf("答%d", i)))
	}
	model := mocks.NewScriptedClient(rules...)
	r := resolveTo(dataURL)
	agent := imageAgent(t, model, store, r, p)
	before := 0
	for i := 1; i <= turns; i++ {
		before = len(r.requests())
		_, err := agent.AsRunnable().Invoke(context.Background(), UserInput{Text: fmt.Sprintf("第%d张", i), Images: []llm.ImagePart{imageRef(i)}})
		require.NoError(t, err)
	}
	requests := model.Requests()
	return requests[len(requests)-1], r.requests()[before:]
}

func userMessages(req llm.CompletionRequest) []llm.Message {
	var out []llm.Message
	for _, m := range req.Messages {
		if m.Role == constants.RoleUser {
			out = append(out, m)
		}
	}
	return out
}

func TestKeepRecentTurnsDegradesOlderImages(t *testing.T) {
	for _, test := range []struct {
		name  string
		keep  int
		want  []llm.Message
		calls []ImageRequest
	}{
		{"0 表示全部附带", 0, []llm.Message{
			{Role: constants.RoleUser, Content: "第1张", Images: []llm.ImagePart{resolvedImage(1)}},
			{Role: constants.RoleUser, Content: "第2张", Images: []llm.ImagePart{resolvedImage(2)}},
			{Role: constants.RoleUser, Content: "第3张", Images: []llm.ImagePart{resolvedImage(3)}},
		}, []ImageRequest{{imageRef(1), 2}, {imageRef(2), 1}, {imageRef(3), 0}}},
		{"1 表示本轮与上一轮", 1, []llm.Message{
			{Role: constants.RoleUser, Content: "第1张\n[图片 image:1：不在当前上下文中]"},
			{Role: constants.RoleUser, Content: "第2张", Images: []llm.ImagePart{resolvedImage(2)}},
			{Role: constants.RoleUser, Content: "第3张", Images: []llm.ImagePart{resolvedImage(3)}},
		}, []ImageRequest{{imageRef(2), 1}, {imageRef(3), 0}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			last, calls := runImageTurns(t, 3, ImagePolicy{KeepRecentTurns: test.keep})
			require.Equal(t, test.want, userMessages(last))
			require.Equal(t, test.calls, calls)
		})
	}
}

func TestTurnAgeFollowsSessionTurnsNotUserMessages(t *testing.T) {
	r := resolveTo(dataURL)
	turns := [][]llm.ImagePart{{imageRef(1)}, nil, {imageRef(2)}, {imageRef(3)}}
	ctx := withImageConfig(context.Background(), &imageConfig{resolve: r.resolve, policy: ImagePolicy{KeepRecentTurns: 1}}, turns)
	req := &llm.CompletionRequest{Messages: []llm.Message{
		{Role: constants.RoleUser, Content: "旧", Images: []llm.ImagePart{imageRef(1)}},
		llm.UserMessage("[此前对话的摘要]\n摘要"),
		llm.UserMessage("无图的一轮"),
		{Role: constants.RoleUser, Content: "上一轮", Images: []llm.ImagePart{imageRef(2)}},
		llm.UserMessage("上一轮的纠正"),
		llm.AssistantMessage("答"),
		{Role: constants.RoleUser, Content: "本轮", Images: []llm.ImagePart{imageRef(3)}},
		llm.UserMessage("插话"),
		llm.UserMessage(emptyAnswerPrompt),
		llm.UserMessage("[相关长期记忆]\n记忆"),
		stepContextMessage("上下文"),
	}}
	sent, err := resolveImages(ctx, req)
	require.NoError(t, err)
	require.Equal(t, []ImageRequest{{imageRef(2), 1}, {imageRef(3), 0}}, r.requests(), "轮次内追加的用户消息不计入轮次")
	require.Equal(t, "旧\n[图片 image:1：不在当前上下文中]", sent.Messages[0].Content)
	require.Equal(t, []llm.ImagePart{imageRef(1)}, req.Messages[0].Images, "原请求不变")
	require.Equal(t, []llm.ImagePart{imageRef(3)}, req.Messages[6].Images, "原请求不变")
}

func TestTurnAgeCountsSessionTurnsNotInTurnMessages(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.TextReply("1", "答1"), mocks.TextReply("2a", "答2a"), mocks.TextReply("2b", "答2b"), mocks.TextReply("3", "答3"))
	r := resolveTo(dataURL)
	continued := 0
	agent := imageAgent(t, model, store, r, ImagePolicy{KeepRecentTurns: 1}, func(b *AgentBuilder[*core.AgentState]) *AgentBuilder[*core.AgentState] {
		return b.WithTools(plainTool()).WithTurnStopping(func(context.Context, *Stopping) (string, error) {
			// 只在第 2 轮续跑一次：轮次内追加的用户消息不开始新轮次。
			if len(model.Requests()) == 2 && continued == 0 {
				continued++
				return "再看看", nil
			}
			return "", nil
		})
	})
	var calls [][]ImageRequest
	for i := 1; i <= 3; i++ {
		before := len(r.requests())
		_, err := agent.AsRunnable().Invoke(context.Background(), UserInput{Text: fmt.Sprintf("第%d张", i), Images: []llm.ImagePart{imageRef(i)}})
		require.NoError(t, err)
		calls = append(calls, r.requests()[before:])
	}
	require.Equal(t, 1, continued)
	require.Len(t, model.Requests(), 4)
	require.Equal(t, []ImageRequest{{imageRef(1), 1}, {imageRef(2), 0}, {imageRef(1), 1}, {imageRef(2), 0}}, calls[1], "续跑的第二次请求仍在第 2 轮")
	require.Equal(t, []llm.ImagePart{resolvedImage(1)}, userMessages(model.Requests()[2])[0].Images, "上一轮的图不提前降级")
	require.Equal(t, []ImageRequest{{imageRef(2), 1}, {imageRef(3), 0}}, calls[2], "历史里的续跑消息不计入轮次")
	require.Equal(t, []llm.ImagePart{resolvedImage(2)}, userMessages(model.Requests()[3])[1].Images)
}

func TestEmptyImagePartRejectedBeforeSessionLog(t *testing.T) {
	store := session.NewMemoryStore()
	model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
	agent := imageAgent(t, model, store, resolveTo(dataURL), ImagePolicy{})
	_, err := agent.AsRunnable().Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{{}}})
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), "%v", err)
	require.Zero(t, model.Calls())
	_, err = store.Read(context.Background(), "s", 0)
	require.ErrorIs(t, err, agentErrors.ErrSessionNotFound, "未写入会话日志")
}

func TestMaxImagesPerRequestKeepsNewest(t *testing.T) {
	last, calls := runImageTurns(t, 2, ImagePolicy{MaxImagesPerRequest: 1})
	require.Equal(t, []llm.Message{
		{Role: constants.RoleUser, Content: "第1张\n[图片 image:1：不在当前上下文中]"},
		{Role: constants.RoleUser, Content: "第2张", Images: []llm.ImagePart{resolvedImage(2)}},
	}, userMessages(last))
	require.Equal(t, []ImageRequest{{imageRef(2), 0}}, calls)
}

func TestImageRefWithoutResolverFails(t *testing.T) {
	for _, test := range []struct {
		name  string
		tools bool
	}{{"纯聊天", false}, {"工具循环", true}} {
		t.Run(test.name, func(t *testing.T) {
			store := session.NewMemoryStore()
			model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
			agent := imageAgent(t, model, store, nil, ImagePolicy{}, func(b *AgentBuilder[*core.AgentState]) *AgentBuilder[*core.AgentState] {
				if test.tools {
					return b.WithTools(plainTool())
				}
				return b
			})
			_, err := agent.AsRunnable().Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}})
			require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), "%v", err)
			require.Zero(t, model.Calls())
			_, err = store.Read(context.Background(), "s", 0)
			require.ErrorIs(t, err, agentErrors.ErrSessionNotFound, "未开始轮次")
		})
	}
}

func TestHistoryRefWithoutResolverFailsBeforeModel(t *testing.T) {
	req := &llm.CompletionRequest{Messages: []llm.Message{{Role: constants.RoleUser, Content: "看图", Images: []llm.ImagePart{imageRef(1)}}}}
	_, err := resolveImages(withImageConfig(context.Background(), nil, nil), req)
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))

	// 已有 URL 的图片不需要解析器。
	withURL := &llm.CompletionRequest{Messages: []llm.Message{{Role: constants.RoleUser, Images: []llm.ImagePart{{URL: "https://example.com/a.png"}}}}}
	sent, err := resolveImages(context.Background(), withURL)
	require.NoError(t, err)
	require.Same(t, withURL, sent)
}

func TestCustomPlaceholder(t *testing.T) {
	model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
	policy := ImagePolicy{Placeholder: func(p llm.ImagePart, reason string) string { return "<" + p.Ref + "|" + reason + ">" }}
	_, err := imageAgent(t, model, session.NewMemoryStore(), failWith(ErrImageUnavailable), policy).AsRunnable().
		Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}})
	require.NoError(t, err)
	require.Equal(t, "看图\n<image:1|不可用>", lastUser(t, model.Requests()[0]).Content)
}

func TestTransientResolverErrorFailsRequest(t *testing.T) {
	transient := errors.New("对象存储超时")
	t.Run("没有恢复钩子时请求失败", func(t *testing.T) {
		model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
		_, err := imageAgent(t, model, session.NewMemoryStore(), failWith(transient), ImagePolicy{}).AsRunnable().
			Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}})
		require.ErrorIs(t, err, transient)
		require.Zero(t, model.Calls())
	})
	t.Run("RequestError 钩子决定原样重试", func(t *testing.T) {
		model := mocks.NewScriptedClient(mocks.TextReply("好", "好"))
		r := &imageResolverStub{next: func(n int, req ImageRequest) (string, error) {
			if n == 1 {
				return "", transient
			}
			return dataURL(req), nil
		}}
		var seen []error
		retry := func(b *AgentBuilder[*core.AgentState]) *AgentBuilder[*core.AgentState] {
			return b.WithRequestError(1, func(_ context.Context, f *RequestFailure) (RequestErrorDecision, error) {
				seen = append(seen, f.Err)
				return RequestErrorDecision{Action: RequestRetry}, nil
			})
		}
		_, err := imageAgent(t, model, session.NewMemoryStore(), r, ImagePolicy{}, retry).AsRunnable().
			Invoke(context.Background(), UserInput{Text: "看图", Images: []llm.ImagePart{imageRef(1)}})
		require.NoError(t, err)
		require.Len(t, seen, 1)
		require.ErrorIs(t, seen[0], transient)
		require.Equal(t, []llm.ImagePart{resolvedImage(1)}, lastUser(t, model.Requests()[0]).Images)
	})
}

func TestWithImageResolverValidatesPolicy(t *testing.T) {
	r := resolveTo(dataURL).resolve
	for _, test := range []struct {
		name     string
		resolver ImageResolver
		policy   ImagePolicy
	}{
		{"解析器为空", nil, ImagePolicy{}},
		{"轮数为负", r, ImagePolicy{KeepRecentTurns: -1}},
		{"张数为负", r, ImagePolicy{MaxImagesPerRequest: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSimpleBuilder(mocks.NewScriptedClient()).WithImageResolver(test.resolver, test.policy).Build()
			require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err), "%v", err)
		})
	}
}

func TestImagePolicyIdentityKeysModelCache(t *testing.T) {
	req := &llm.CompletionRequest{Messages: []llm.Message{{Role: constants.RoleUser, Content: "看图", Images: []llm.ImagePart{imageRef(1)}}}}
	key := func(ctx context.Context) string {
		k, ok := modelCacheKey(cacheKeyInput{Identity: "scope", Client: "c", Request: req, Images: imageCacheIdentity(ctx)})
		require.True(t, ok)
		return k
	}
	legacy, ok := modelCacheKey(cacheKeyInput{Identity: "scope", Client: "c", Request: req})
	require.True(t, ok)
	require.Equal(t, legacy, key(context.Background()), "未配置解析器时缓存键不变")
	v1 := key(withImageConfig(context.Background(), &imageConfig{policy: ImagePolicy{Identity: "v1"}}, nil))
	v2 := key(withImageConfig(context.Background(), &imageConfig{policy: ImagePolicy{Identity: "v2"}}, nil))
	require.NotEqual(t, legacy, v1)
	require.NotEqual(t, v1, v2)
	require.False(t, strings.Contains(v1, "v1"))
}
