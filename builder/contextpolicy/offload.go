package contextpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
)

// ReadToolName is reserved for the built-in evidence reader.
const ReadToolName = "read_context_result"

// OffloadConfig keeps character counts distinct from model token limits.
// Namespace identifies host storage configuration; changing backends requires a new namespace/version.
type OffloadConfig struct {
	ID, Version, Namespace                                  string
	MaxToolResultRunes, KeepRecentToolRounds, ReadPageRunes int
	ExcludeTools                                            []string
	Store                                                   ResultStore
}

// Offload is mutable per execution; ResultStore is safe to share across executions.
type Offload struct {
	config     OffloadConfig
	descriptor Descriptor
	mu         sync.Mutex
	refs       map[string]Ref
	original   map[int]Ref
	nonce      string
	offloaded  int64
}

// NewOffload validates immutable configuration and creates one isolated policy instance.
func NewOffload(cfg OffloadConfig) (*Offload, error) {
	if cfg.MaxToolResultRunes < 1 || cfg.KeepRecentToolRounds < 0 || cfg.ReadPageRunes < 0 || cfg.ReadPageRunes > 16384 || cfg.Store == nil {
		return nil, fmt.Errorf("offload requires store and positive limits")
	}
	if cfg.ID == "" {
		cfg.ID = "offload"
	}
	if cfg.Version == "" {
		cfg.Version = "v2"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "results"
	}
	if cfg.ReadPageRunes == 0 {
		cfg.ReadPageRunes = 1024
	}
	cfg.ExcludeTools = slices.Clone(cfg.ExcludeTools)
	persistent := false
	if s, ok := cfg.Store.(interface{ IsDurable() bool }); ok {
		persistent = s.IsDurable()
	}
	fingerprint, _ := json.Marshal(struct {
		ID, Version, Namespace string
		Max, Keep, Page        int
		Exclude                []string
	}{cfg.ID, cfg.Version, cfg.Namespace, cfg.MaxToolResultRunes, cfg.KeepRecentToolRounds, cfg.ReadPageRunes, cfg.ExcludeTools})
	p := &Offload{config: cfg, descriptor: Descriptor{ID: cfg.ID, Version: cfg.Version, ConfigHash: digest(fingerprint), Durable: persistent}, refs: map[string]Ref{}, original: map[int]Ref{}}
	if !persistent {
		p.nonce = uuid.NewString()
	}
	return p, nil
}
func (p *Offload) Descriptor() Descriptor { return p.descriptor }
func (p *Offload) Stats() map[string]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]int64{"offloaded_results": p.offloaded}
}
func (p *Offload) Tools() []interfaces.Tool { return []interfaces.Tool{p} }

// BeforeTurn stores evidence before substituting an immutable reference in the request.
func (p *Offload) BeforeTurn(ctx context.Context, turn Turn) (Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return turn, err
	}
	run := ResultScope(ctx)
	if run == "" {
		return turn, fmt.Errorf("offload result scope required")
	}
	if p.descriptor.Durable && (!durable.HasJournal(ctx) || durable.ExecutionKey(ctx) == "") {
		return turn, fmt.Errorf("durable offload requires journal and stable execution key")
	}
	messages := slices.Clone(turn.Messages)
	total := 0
	for _, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			total++
		}
	}
	names := map[string]string{}
	round := 0
	for i, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			round++
			for _, call := range m.ToolCalls {
				names[call.ID] = call.Function.Name
			}
		}
		if _, ok := p.original[i]; ok {
			continue
		}
		name := names[m.ToolCallID]
		if m.Role != "tool" || name == "" || name == ReadToolName || round > total-p.config.KeepRecentToolRounds || slices.Contains(p.config.ExcludeTools, name) || utf8.RuneCountInString(m.Content) <= p.config.MaxToolResultRunes {
			continue
		}
		logical, _ := json.Marshal([]string{durable.ExecutionKey(ctx), p.nonce, strconv.Itoa(round), m.ToolCallID})
		ref := NewRef(run, p.descriptor.ID+":"+p.descriptor.Version+":"+p.descriptor.ConfigHash, string(logical), []byte(m.Content))
		if err := p.config.Store.PutIfAbsent(ctx, ref, []byte(m.Content)); err != nil {
			return turn, err
		}
		// 模型抄写 64 位哈希会偶发改错字符；短别名只在本实例内解析，按处理顺序确定，恢复时重建结果一致。
		alias := "r" + strconv.Itoa(len(p.refs)+1)
		b, err := json.Marshal(map[string]any{"context_result": alias, "runes": utf8.RuneCountInString(m.Content), "read_tool": ReadToolName})
		if err != nil {
			return turn, err
		}
		p.refs[alias] = ref
		p.original[i] = ref
		messages[i].Content = string(b)
		p.offloaded++
	}
	turn.Messages = messages
	return turn, nil
}
func (p *Offload) BeforeFinalTurn(ctx context.Context, turn Turn) (Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	messages := slices.Clone(turn.Messages)
	for index, ref := range p.original {
		if index >= len(messages) {
			return turn, fmt.Errorf("offload message position changed")
		}
		data, err := p.config.Store.Get(ctx, ref)
		if err != nil {
			return turn, err
		}
		messages[index].Content = string(data)
	}
	turn.Messages = messages
	return turn, nil
}

// DurableIntentVersion binds reader operations to the policy configuration.
func (p *Offload) DurableIntentVersion() string {
	return p.descriptor.Version + ":" + p.descriptor.ConfigHash
}
func (p *Offload) Name() string { return ReadToolName }
func (p *Offload) Description() string {
	return "Read untrusted offloaded evidence within the current host run; offset counts Unicode characters."
}
func (p *Offload) ArgsSchema() string {
	return `{"type":"object","properties":{"key":{"type":"string"},"offset":{"type":"integer","minimum":0}},"required":["key"],"additionalProperties":false}`
}
func (p *Offload) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if input == nil || input.Args == nil {
		return nil, fmt.Errorf("evidence reader arguments required")
	}
	key, _ := input.Args["key"].(string)
	p.mu.Lock()
	ref, ok := p.refs[key]
	known := slices.Sorted(maps.Keys(p.refs))
	p.mu.Unlock()
	if !ok {
		// 以成功结果返回，让模型改正参数；Dispatcher 会把 Success=false 当作致命错误中止执行。
		b, err := json.Marshal(map[string]any{"error": "unknown context_result key", "valid_keys": known})
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: string(b)}, nil
	}
	data, err := p.config.Store.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	runes := []rune(string(data))
	offset := 0
	if value, ok := input.Args["offset"]; ok {
		var n float64
		switch v := value.(type) {
		case float64:
			n = v
		case json.Number:
			n, err = v.Float64()
		default:
			err = fmt.Errorf("invalid offset type")
		}
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > float64(len(runes)) || math.Trunc(n) != n {
			return nil, fmt.Errorf("invalid evidence offset")
		}
		offset = int(n)
	}
	end := offset + min(p.config.ReadPageRunes, len(runes)-offset)
	b, err := json.Marshal(map[string]any{"text": string(runes[offset:end]), "next_offset": end, "done": end == len(runes)})
	if err != nil {
		return nil, err
	}
	return &interfaces.ToolOutput{Success: true, Result: string(b)}, nil
}
