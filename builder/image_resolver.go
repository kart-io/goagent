package builder

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// ErrImageUnavailable reports that an image reference can never be resolved,
// for example because the image was deleted, is not visible to the session
// owner, or no longer matches its digest. A resolver returns it, possibly
// wrapped; the image is then replaced with a placeholder instead of failing
// the request.
var ErrImageUnavailable error = agentErrors.ErrImageUnavailable

// 降级原因，作为 ImagePolicy.Placeholder 的 reason 传入。
const (
	imageReasonUnavailable  = "不可用"
	imageReasonOutOfContext = "不在当前上下文中"
)

// ImageRequest is one image reference to resolve for an outgoing model
// request.
type ImageRequest struct {
	// Part is the image as recorded in the history.
	Part llm.ImagePart
	// TurnAge counts user turns from the end of the request: 0 is the current
	// turn's user message, 1 the previous one, and so on. Memory recall,
	// compaction summaries, step context and empty-answer re-asks are not
	// counted as turns.
	TurnAge int
}

// ImageResolver turns an image reference into a URL the provider can send,
// such as an https or data: URL. It runs right before every real model
// request and never while a recorded turn replays, so it may return a
// different URL each time (for example a fresh signed URL). Returning
// ErrImageUnavailable degrades the image to a placeholder; any other error
// fails the request and goes through the RequestError hooks like a provider
// error.
type ImageResolver func(ctx context.Context, req ImageRequest) (url string, err error)

// ImagePolicy decides which images of the history are attached to a request.
type ImagePolicy struct {
	// Identity identifies the resolver and policy configuration (for example
	// a version and the encoding settings). It is part of the model call
	// cache key, so cached turns are not shared across configurations; it is
	// not part of the durable intent.
	Identity string
	// KeepRecentTurns attaches images only from user turns whose TurnAge is at
	// most KeepRecentTurns; older images become placeholders. 0 attaches all.
	KeepRecentTurns int
	// MaxImagesPerRequest caps the attached images of one request, keeping
	// the newest; 0 means no limit.
	MaxImagesPerRequest int
	// EstimateTokensPerImage is the estimated token cost of one attached
	// image.
	EstimateTokensPerImage int
	// Placeholder renders the text appended to the message in place of a
	// degraded image. reason is "不可用" (ErrImageUnavailable) or
	// "不在当前上下文中" (outside KeepRecentTurns or MaxImagesPerRequest).
	// When nil, the text is "[图片 <ref>：<reason>]".
	Placeholder func(p llm.ImagePart, reason string) string
}

// WithImageResolver resolves image references into sendable URLs in the
// outgoing model request only. The history, the session log, the durable
// intent and the messages seen by PreStep and Request hooks keep the
// references with an empty URL; a recorded turn replays without calling r.
// Without a resolver, any image without a URL fails the run with
// CodeInvalidInput before the model is called. Each call replaces the previous
// resolver and policy.
func (b *AgentBuilder[S]) WithImageResolver(r ImageResolver, p ImagePolicy) *AgentBuilder[S] {
	b.config.images = &imageConfig{resolve: r, policy: p}
	return b
}

// imageConfig 是构建时固定的解析器与策略，构建后只读。
type imageConfig struct {
	resolve ImageResolver
	policy  ImagePolicy
}

func (b *AgentBuilder[S]) validateImages() error {
	c := b.config.images
	if c == nil {
		return nil
	}
	if c.resolve == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "WithImageResolver 需要非空解析器").WithComponent("builder").WithOperation("validate_images")
	}
	p := c.policy
	if p.KeepRecentTurns < 0 || p.MaxImagesPerRequest < 0 || p.EstimateTokensPerImage < 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "图片策略的 KeepRecentTurns、MaxImagesPerRequest、EstimateTokensPerImage 不能为负").WithComponent("builder").WithOperation("validate_images")
	}
	return nil
}

type imageConfigKey struct{}

// withImageConfig 由每次运行安装（可为 nil），子 Agent 不继承父 Agent 的解析器。
func withImageConfig(ctx context.Context, c *imageConfig) context.Context {
	return context.WithValue(ctx, imageConfigKey{}, c)
}

func imageConfigFrom(ctx context.Context) *imageConfig {
	c, _ := ctx.Value(imageConfigKey{}).(*imageConfig)
	return c
}

// imageCacheIdentity 是写入模型缓存键的图片配置身份；未配置解析器时为空，缓存键不变。
func imageCacheIdentity(ctx context.Context) string {
	if c := imageConfigFrom(ctx); c != nil {
		return "images:" + c.policy.Identity
	}
	return ""
}

// unresolvedImage 是无法发送的图片：没有解析器，或部件既无 URL 也无引用。
func unresolvedImage(p llm.ImagePart) error {
	return agentErrors.Newf(agentErrors.CodeInvalidInput, "图片 %q 没有可发送的 URL：引用须经 WithImageResolver 解析", p.Ref).WithComponent("builder").WithOperation("resolve_images")
}

// checkImageInput 在开始轮次之前拒绝无法解析的输入图片，不写日志、不请求模型。
func (a *ConfigurableAgent[S]) checkImageInput(images []llm.ImagePart) error {
	if a.config.images != nil {
		return nil
	}
	for _, p := range images {
		if p.URL == "" {
			return unresolvedImage(p)
		}
	}
	return nil
}

// countsAsUserTurn 认出真实的用户轮次消息，复用各合成消息已有的标记。
func countsAsUserTurn(m llm.Message) bool {
	return m.Role == constants.RoleUser &&
		!strings.HasPrefix(m.Content, memory.RecallPrefix) &&
		!contextpolicy.IsSummary(m) &&
		!strings.HasPrefix(m.Content, stepContextPrefix) &&
		m.Content != emptyAnswerPrompt
}

// resolveImages 返回本次真实发送的请求：按策略降级旧图，其余经解析器填入 URL。
// 只改副本（浅拷贝请求、克隆消息切片、为改动的消息新建 Images），req 与历史保持引用不变；
// 意图与缓存键在调用前已由未解析的 req 算出。
func resolveImages(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionRequest, error) {
	if !slices.ContainsFunc(req.Messages, func(m llm.Message) bool { return len(m.Images) > 0 }) {
		return req, nil
	}
	c := imageConfigFrom(ctx)
	if c == nil {
		for _, m := range req.Messages {
			for _, p := range m.Images {
				if p.URL == "" {
					return nil, unresolvedImage(p)
				}
			}
		}
		return req, nil
	}
	policy := c.policy
	ages := make([]int, len(req.Messages))
	age := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if countsAsUserTurn(req.Messages[i]) {
			age++
		}
		ages[i] = max(age, 0)
	}
	// 从最新往前决定附带：先按轮次，再按张数上限保留最新的。
	attach := make([][]bool, len(req.Messages))
	attached := 0
	for i := len(req.Messages) - 1; i >= 0; i-- {
		attach[i] = make([]bool, len(req.Messages[i].Images))
		for j := len(attach[i]) - 1; j >= 0; j-- {
			if policy.KeepRecentTurns > 0 && ages[i] > policy.KeepRecentTurns {
				continue
			}
			if policy.MaxImagesPerRequest > 0 && attached >= policy.MaxImagesPerRequest {
				continue
			}
			attach[i][j] = true
			attached++
		}
	}
	placeholder := policy.Placeholder
	if placeholder == nil {
		placeholder = func(p llm.ImagePart, reason string) string { return fmt.Sprintf("[图片 %s：%s]", p.Ref, reason) }
	}
	out := *req
	out.Messages = slices.Clone(req.Messages)
	for i, m := range req.Messages {
		if len(m.Images) == 0 {
			continue
		}
		var kept []llm.ImagePart
		var notes []string
		for j, p := range m.Images {
			switch {
			case !attach[i][j]:
				notes = append(notes, placeholder(p, imageReasonOutOfContext))
				continue
			case p.URL != "":
				kept = append(kept, p)
				continue
			case p.Ref == "":
				return nil, unresolvedImage(p)
			}
			url, err := c.resolve(ctx, ImageRequest{Part: p, TurnAge: ages[i]})
			switch {
			case errors.Is(err, ErrImageUnavailable):
				notes = append(notes, placeholder(p, imageReasonUnavailable))
				continue
			case err != nil:
				return nil, fmt.Errorf("解析图片 %s：%w", p.Ref, err)
			case url == "":
				return nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "图片解析器为 %q 返回了空 URL", p.Ref).WithComponent("builder").WithOperation("resolve_images")
			}
			// 引用只在宿主内有意义，发送的部件只带 URL 与描述字段。
			p.URL, p.Ref = url, ""
			kept = append(kept, p)
		}
		out.Messages[i].Images = kept
		if len(notes) > 0 {
			if m.Content != "" {
				notes = append([]string{m.Content}, notes...)
			}
			out.Messages[i].Content = strings.Join(notes, "\n")
		}
	}
	return &out, nil
}
