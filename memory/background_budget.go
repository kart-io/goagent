package memory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// BackgroundConfig sets the maintenance window and bounds each physical request.
// Estimators must conservatively cover the complete request, including the
// allowed output. SingleRequestClients asserts that clients never split or retry.
type BackgroundConfig struct {
	Budget                                               *budget.Controller
	MaxInputRunes, MaxOutputRunes, MaxDerivationsPerPass int
	MaxOutputTokens                                      int
	EstimatorVersion                                     string
	EstimateChat                                         func(*llm.CompletionRequest) (int, error)
	EstimateEmbedding                                    func(string) (int, error)
	SingleRequestClients                                 bool
}

func (b *BackgroundConfig) validate() error {
	if b == nil {
		return nil
	}
	if b.Budget.IsDurable() {
		c := b.Budget.Configuration()
		if c.MaxModelRequests > 0 && c.MaxReservedTokens > 0 && b.MaxInputRunes > 0 && b.MaxOutputRunes > 0 && b.MaxOutputTokens > 0 && b.MaxDerivationsPerPass > 0 && b.SingleRequestClients && b.EstimateChat != nil && b.EstimateEmbedding != nil && b.EstimatorVersion != "" && b.EstimatorVersion == c.TokenPolicyVersion {
			return nil
		}
	}
	return agentErrors.New(agentErrors.CodeInvalidConfig, "后台维护需要持久且有界的预算、匹配的估算器和单请求客户端").WithComponent("memory")
}

type maintenanceKey struct{}
type cancelOnly struct{ context.Context }

func (cancelOnly) Value(any) any { return nil }

// 不继承调用方的 Journal、归属和在线预算，保留原始取消和截止时间。
func (m *Memory) maintenanceParent(ctx context.Context) context.Context {
	if m.cfg.Background == nil {
		return ctx
	}
	return budget.WithController(cancelOnly{ctx}, m.cfg.Background.Budget)
}
func (m *Memory) maintenanceContext(ctx context.Context, ns []string, scope string) context.Context {
	if m.cfg.Background == nil {
		return WithNamespace(ctx, ns...)
	}
	ctx = m.maintenanceParent(ctx)
	return context.WithValue(WithNamespace(ctx, ns...), maintenanceKey{}, scope)
}

const errBackgroundBudget = sentinel("后台维护窗口额度已耗尽")
const errInputOversized = sentinel("后台维护输入超长")

func (m *Memory) reserveBackground(ctx context.Context, scope string, estimate int) (string, error) {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return "", err
	}
	if estimate <= 0 {
		return "", agentErrors.ErrTokenEstimateRequired
	}
	var seq int64
	_, err = m.mutate(ctx, ns, scope, func(t *tx) error {
		if err := currentDerivation(ctx, t); err != nil {
			return err
		}
		if t.scalars.RequestSeq == math.MaxInt64 {
			return conflict("维护请求序号溢出")
		}
		t.scalars.RequestSeq++
		seq = t.scalars.RequestSeq
		return nil
	})
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("memory/%s/%d", scopeName(ns, scope), seq)
	err = m.cfg.Background.Budget.ReserveAttributed(ctx, key, estimate, nil)
	if errors.Is(err, agentErrors.ErrModelBudgetExceeded) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return "", errors.Join(errBackgroundBudget, err)
	}
	return key, err
}
func (m *Memory) backgroundChat(ctx context.Context, msgs []llm.Message) (*llm.CompletionResponse, error) {
	scope, ok := ctx.Value(maintenanceKey{}).(string)
	if !ok {
		return llm.Chat(ctx, m.cfg.Model, msgs)
	}
	b := m.cfg.Background
	left := b.MaxInputRunes
	for _, msg := range msgs {
		left -= utf8.RuneCountInString(msg.Content)
		if left < 0 {
			return nil, errInputOversized
		}
	}
	req := &llm.CompletionRequest{Messages: msgs, MaxTokens: b.MaxOutputTokens}
	estimate, err := b.EstimateChat(req)
	if err != nil {
		return nil, err
	}
	if estimate < b.MaxOutputTokens {
		return nil, agentErrors.ErrTokenEstimateRequired
	}
	key, err := m.reserveBackground(ctx, scope, estimate)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := budget.Bind(ctx, b.Budget)
	defer cancel()
	resp, evidence, err := m.collectBackground(callCtx, req)
	err = windowDeadline(ctx, callCtx, err)
	_, recordErr := b.Budget.RecordEvidence(context.WithoutCancel(ctx), key, nil, evidence)
	return resp, errors.Join(err, recordErr)
}
func (m *Memory) collectBackground(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, usage.Evidence, error) {
	var evidence usage.Evidence
	if err := ctx.Err(); err != nil {
		return nil, evidence, err
	}
	stream, err := m.cfg.Model.CompleteStream(ctx, req)
	if err != nil {
		return nil, evidence, err
	}
	if stream == nil {
		return nil, evidence, io.ErrUnexpectedEOF
	}
	var content strings.Builder
	for {
		select {
		case <-ctx.Done():
			return nil, evidence, ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				return nil, evidence, io.ErrUnexpectedEOF
			}
			if chunk == nil {
				continue
			}
			if chunk.Usage != nil {
				evidence = usage.Evidence{Tokens: chunk.Usage.Tokens(), KnownFields: chunk.Usage.Fields(), Source: "provider"}
			}
			if chunk.Error != nil {
				return nil, evidence, chunk.Error
			}
			content.WriteString(chunk.Delta)
			if chunk.Done {
				result := chunk.Content
				if result == "" {
					result = content.String()
				}
				if utf8.RuneCountInString(result) > m.cfg.Background.MaxOutputRunes {
					return nil, evidence, fmt.Errorf("后台维护输出超长")
				}
				return &llm.CompletionResponse{Content: result, FinishReason: chunk.FinishReason}, evidence, nil
			}
		}
	}
}
func (m *Memory) requestEmbedding(ctx context.Context, text string) ([]float32, error) {
	scope, ok := ctx.Value(maintenanceKey{}).(string)
	if !ok {
		return m.cfg.Embedder.Embed(ctx, text)
	}
	b := m.cfg.Background
	if utf8.RuneCountInString(text) > b.MaxInputRunes {
		return nil, errInputOversized
	}
	estimate, err := b.EstimateEmbedding(text)
	if err != nil {
		return nil, err
	}
	key, err := m.reserveBackground(ctx, scope, estimate)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := budget.Bind(ctx, b.Budget)
	defer cancel()
	vec, err := m.cfg.Embedder.Embed(callCtx, text)
	err = windowDeadline(ctx, callCtx, err)
	_, recordErr := b.Budget.RecordEvidence(context.WithoutCancel(ctx), key, nil, usage.Evidence{})
	return vec, errors.Join(err, recordErr)
}

func windowDeadline(parent, call context.Context, err error) error {
	if err != nil && parent.Err() == nil && errors.Is(call.Err(), context.DeadlineExceeded) {
		return errors.Join(errBackgroundBudget, err)
	}
	return err
}
