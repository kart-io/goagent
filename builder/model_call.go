package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

type modelScopeKey struct{}

func ensureModelScope(ctx context.Context) context.Context {
	if ctx.Value(modelScopeKey{}) == nil {
		return context.WithValue(ctx, modelScopeKey{}, new(atomic.Int64))
	}
	return ctx
}
func modelInvocations(ctx context.Context) int64 {
	n, _ := ctx.Value(modelScopeKey{}).(*atomic.Int64)
	if n == nil {
		return 0
	}
	return n.Load()
}
func validateModelBudget(ctx context.Context) error {
	if c, ok := budget.FromContext(ctx); ok && durable.HasJournal(ctx) && !c.IsDurable() {
		return fmt.Errorf("durable model calls require a durable root budget")
	}
	return nil
}
func recordTurnUsage(ctx context.Context, key string, turn *llm.StreamChunk) error {
	c, ok := budget.FromContext(ctx)
	if !ok || turn == nil {
		return nil
	}
	var usage *interfaces.TokenUsage
	if turn.Usage != nil {
		usage = &interfaces.TokenUsage{PromptTokens: turn.Usage.PromptTokens, CompletionTokens: turn.Usage.CompletionTokens, TotalTokens: turn.Usage.TotalTokens}
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return c.RecordUsage(saveCtx, key, usage)
}

type modelRetryKey struct{}

type modelRetry struct {
	max     int
	backoff func(retry int) time.Duration
}

// withModelRetry 总是覆盖外层设置：子 Agent 不继承父 Agent 的重试配置。
func withModelRetry(ctx context.Context, max int, backoff func(int) time.Duration) context.Context {
	return context.WithValue(ctx, modelRetryKey{}, modelRetry{max: max, backoff: backoff})
}

func defaultModelRetryBackoff(retry int) time.Duration {
	return min(500*time.Millisecond<<min(retry-1, 5), 8*time.Second)
}

// delay 只重试尚未向消费者输出任何内容、且 Provider 判定为暂时性的失败；等待不能越过截止时间。
func (r modelRetry) delay(ctx context.Context, retry int, emitted bool, err error) (time.Duration, bool) {
	var pe *llm.ProviderError
	if err == nil || emitted || retry > r.max || !errors.As(err, &pe) || !pe.Retryable() {
		return 0, false
	}
	delay := pe.RetryAfter
	if delay <= 0 {
		backoff := r.backoff
		if backoff == nil {
			backoff = defaultModelRetryBackoff
		}
		delay = max(backoff(retry), 0)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
		return 0, false
	}
	return delay, true
}

// savedTurn 是持久日志中的模型回合；Attempt 记下成功或最终失败的是第几次尝试，
// 恢复时用它找回该次尝试的预算键。旧记录没有该字段，即第 0 次。
type savedTurn struct {
	llm.StreamChunk
	Attempt int `json:"model_attempt,omitempty"`
}

func modelOperation(ctx context.Context, id string, intent json.RawMessage, invoke func(context.Context, func(AgentEvent) error) (*llm.StreamChunk, error), validate func(*llm.StreamChunk) error, emit func(AgentEvent) error) (result *llm.StreamChunk, resultErr error) {
	c, metered := budget.FromContext(ctx)
	key := durable.ChildKey(durable.ExecutionKey(ctx), id)
	if (metered || observe.Enabled(ctx)) && (!durable.HasJournal(ctx) || key == "") {
		key = uuid.NewString()
	}
	ctx, endModel := observe.Start(ctx, observe.Event{Type: "model", Path: id, OperationKey: key})
	replayed := false
	defer func() { endModel(resultErr, replayed) }()
	defer recoverPanic("模型调用", &resultErr)
	if err := validateModelBudget(ctx); err != nil {
		return nil, err
	}
	// 每次尝试各占一份预算：同一键不能重复预留，且已登记的用量不可改写。
	attemptKey := func(n int) string {
		if n == 0 || key == "" {
			return key
		}
		return durable.ChildKey(key, fmt.Sprintf("retry/%d", n))
	}
	reserve := func(callCtx context.Context, n int) error {
		if metered {
			return c.Reserve(callCtx, attemptKey(n))
		}
		return nil
	}
	send := func(callCtx context.Context) (*llm.StreamChunk, bool, error) {
		if counter, ok := ctx.Value(modelScopeKey{}).(*atomic.Int64); ok {
			counter.Add(1)
		}
		emitted := false
		turn, err := invoke(callCtx, func(event AgentEvent) error {
			emitted = true
			return emit(event)
		})
		return turn, emitted, err
	}
	retry, _ := ctx.Value(modelRetryKey{}).(modelRetry)
	// call 在同一次模型回合操作内重试，持久日志仍只有一条回合记录；返回最后一次发出请求的尝试序号。
	call := func(callCtx context.Context) (*llm.StreamChunk, int, error) {
		if err := reserve(callCtx, 0); err != nil {
			return nil, 0, err
		}
		turn, emitted, err := send(callCtx)
		for n := 1; ; n++ {
			delay, ok := retry.delay(callCtx, n, emitted, err)
			if !ok {
				return turn, n - 1, err
			}
			if e := recordTurnUsage(ctx, attemptKey(n-1), turn); e != nil {
				return turn, n - 1, errors.Join(err, e)
			}
			timer := time.NewTimer(delay)
			select {
			case <-callCtx.Done():
				timer.Stop()
				return turn, n - 1, errors.Join(err, callCtx.Err())
			case <-timer.C:
			}
			// 额度不足时在发送前停止，保留上一次尝试的结果作为证据。
			if e := reserve(callCtx, n); e != nil {
				return turn, n - 1, errors.Join(err, e)
			}
			retryCtx, endRetry := observe.Start(callCtx, observe.Event{Type: "model_retry", Path: fmt.Sprintf("retry/%d", n), OperationKey: attemptKey(n)})
			turn, emitted, err = send(retryCtx)
			endRetry(err, false)
		}
	}
	if !durable.HasJournal(ctx) {
		turn, n, err := call(ctx)
		return turn, errors.Join(err, recordTurnUsage(ctx, attemptKey(n), turn))
	}
	if metered {
		var err error
		intent, err = json.Marshal(struct {
			Original json.RawMessage
			Budget   budget.Config
		}{intent, c.Configuration()})
		if err != nil {
			return nil, err
		}
	}
	attempted := false
	record, opErr := durable.Do(ctx, id, intent, durable.OperationPolicy{}, func(callCtx context.Context) (json.RawMessage, error) {
		attempted = true
		turn, n, err := call(callCtx)
		if turn == nil {
			return nil, err
		}
		saved := savedTurn{StreamChunk: *turn, Attempt: n}
		saved.Error = nil
		if err == nil && validate != nil {
			err = validate(&saved.StreamChunk)
		}
		data, e := json.Marshal(&saved)
		return data, errors.Join(err, e)
	})
	if record != nil {
		replayed = record.Replayed
	}
	var turn *llm.StreamChunk
	usageKey := key
	// Failed operations retain known usage. An intent mismatch is not a replay.
	if record != nil && (opErr == nil || record.Replayed || attempted) && len(record.Value) > 0 && string(record.Value) != "null" {
		var saved savedTurn
		if err := json.Unmarshal(record.Value, &saved); err != nil {
			return nil, errors.Join(opErr, err)
		}
		turn, usageKey = &saved.StreamChunk, attemptKey(saved.Attempt)
	}
	if turn != nil {
		opErr = errors.Join(opErr, recordTurnUsage(ctx, usageKey, turn))
	}
	if opErr != nil {
		return turn, opErr
	}
	if turn == nil {
		return nil, fmt.Errorf("model operation has no result")
	}
	if record.Replayed {
		if err := emit(AgentEvent{Type: "step", Step: "model_replayed"}); err != nil {
			return turn, err
		}
		if turn.Content != "" {
			if err := emit(AgentEvent{Type: "text", Delta: turn.Content}); err != nil {
				return turn, err
			}
		}
	}
	return turn, nil
}
func readPlainModelTurn(ctx context.Context, id string, client llm.StreamClient, req *llm.CompletionRequest, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	var intent json.RawMessage
	var err error
	if durable.HasJournal(ctx) {
		intent, err = json.Marshal(struct {
			Provider string
			Request  *llm.CompletionRequest
		}{string(client.Provider()), req})
	}
	if err != nil {
		return nil, err
	}
	return modelOperation(ctx, id, intent, func(callCtx context.Context, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
		stream, e := client.CompleteStream(callCtx, req)
		if e != nil {
			return nil, e
		}
		return readToolTurn(callCtx, stream, emit)
	}, nil, emit)
}
