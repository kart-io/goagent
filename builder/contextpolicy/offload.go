package contextpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
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

// Offload 把超长的工具结果写入 ResultStore，在对话中换成引用，模型经读取工具（实例本身，名为 ReadToolName）分页读回；
// 最后一次请求前把引用还原为原文。Builder 经 WithOffload 把它装为带身份的 PreStep，读取工具由宿主与其他工具一起注册。
//
// 一个实例供多次执行并发共享，引用表按结果作用域区分。引用中带有证据的键与摘要，引用表可从对话重建：
// 持久运行恢复时 PreStep 的结果被回放，Builder 用回放的对话调用 Track，读取不受影响。
// 证据保留到对该作用域调用 Release：Builder 为非持久执行自动生成的临时作用域在执行结束时释放；
// 宿主经 WithResultScope 提供作用域（包括持久执行）时，由宿主在运行不再恢复后调用 Release。
type Offload struct {
	config     OffloadConfig
	identity   string
	persistent bool
	// nonce 使非持久存储中不同实例的证据互不冲突。
	nonce string

	mu        sync.Mutex
	tables    map[string]map[string]Ref
	offloaded int64
}

// reference 是对话中代替原文的引用。模型只需抄写 Alias；Key 与 SHA256 用于从对话重建引用表。
type reference struct {
	Alias    string `json:"context_result"`
	Runes    int    `json:"runes"`
	ReadTool string `json:"read_tool"`
	Stored   struct {
		Key    string `json:"key"`
		SHA256 string `json:"sha256"`
	} `json:"stored"`
}

// NewOffload validates immutable configuration.
func NewOffload(cfg OffloadConfig) (*Offload, error) {
	if cfg.MaxToolResultRunes < 1 || cfg.KeepRecentToolRounds < 0 || cfg.ReadPageRunes < 0 || cfg.ReadPageRunes > 16384 || cfg.Store == nil {
		return nil, fmt.Errorf("offload requires store and positive limits")
	}
	if cfg.ID == "" {
		cfg.ID = "offload"
	}
	if cfg.Version == "" {
		cfg.Version = "v3"
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
	p := &Offload{config: cfg, identity: cfg.ID + ":" + cfg.Version + ":" + digest(fingerprint), persistent: persistent, tables: map[string]map[string]Ref{}}
	if !persistent {
		p.nonce = uuid.NewString()
	}
	return p, nil
}

// Identity 标识配置（ID、版本与各项限制）；作为 PreStep 的身份进入持久日志，配置改变后旧日志不被复用。
func (p *Offload) Identity() string { return p.identity }

// Stats 返回本实例累计换成引用的结果数。
func (p *Offload) Stats() map[string]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]int64{"offloaded_results": p.offloaded}
}

// Release 删除当前结果作用域的全部证据与引用表。
func (p *Offload) Release(ctx context.Context) error {
	run := ResultScope(ctx)
	if run == "" {
		return nil
	}
	p.mu.Lock()
	delete(p.tables, run)
	p.mu.Unlock()
	return p.config.Store.Release(ctx, run)
}

// parse 识别引用，返回其别名与证据的 Ref。
func parse(run, content string) (string, Ref, bool) {
	var r reference
	if !strings.HasPrefix(content, `{"context_result"`) || json.Unmarshal([]byte(content), &r) != nil || r.ReadTool != ReadToolName || r.Alias == "" || r.Stored.Key == "" {
		return "", Ref{}, false
	}
	return r.Alias, Ref{RunKey: run, Key: r.Stored.Key, SHA256: r.Stored.SHA256, Version: resultVersion}, true
}

// track 从对话中的引用重建 run 的引用表；调用方持有 p.mu。
func (p *Offload) track(run string, messages []llm.Message) map[string]Ref {
	table := p.tables[run]
	if table == nil {
		table = map[string]Ref{}
		p.tables[run] = table
	}
	for _, m := range messages {
		if m.Role != "tool" {
			continue
		}
		if alias, ref, ok := parse(run, m.Content); ok {
			table[alias] = ref
		}
	}
	return table
}

// Track 从对话重建当前结果作用域的引用表，供 PreStep 的结果被回放后读取。
func (p *Offload) Track(ctx context.Context, messages []llm.Message) {
	if run := ResultScope(ctx); run != "" {
		p.mu.Lock()
		p.track(run, messages)
		p.mu.Unlock()
	}
}

// Reduce 返回改写后的对话：超长的工具结果写入存储后换成引用；final 为真时（最后一次请求，模型无法再读取）把引用还原为原文。
// 结果只取决于对话与配置，同一对话重复调用得到逐字节相同的结果。
func (p *Offload) Reduce(ctx context.Context, messages []llm.Message, final bool) ([]llm.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run := ResultScope(ctx)
	if run == "" {
		return nil, fmt.Errorf("offload result scope required")
	}
	if durable.HasJournal(ctx) && (!p.persistent || durable.ExecutionKey(ctx) == "") {
		return nil, fmt.Errorf("durable offload requires a durable result store and stable execution key")
	}
	out := slices.Clone(messages)
	table := p.track(run, out)
	if final {
		for i, m := range out {
			if _, ref, ok := parse(run, m.Content); ok && m.Role == "tool" {
				data, err := p.config.Store.Get(ctx, ref)
				if err != nil {
					return nil, err
				}
				out[i].Content = string(data)
			}
		}
		return out, nil
	}
	total := 0
	for _, m := range out {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			total++
		}
	}
	names := map[string]string{}
	round := 0
	for i, m := range out {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			round++
			for _, call := range m.ToolCalls {
				names[call.ID] = call.Function.Name
			}
		}
		name := names[m.ToolCallID]
		if m.Role != "tool" || name == "" || name == ReadToolName || round > total-p.config.KeepRecentToolRounds || slices.Contains(p.config.ExcludeTools, name) || utf8.RuneCountInString(m.Content) <= p.config.MaxToolResultRunes {
			continue
		}
		if _, _, ok := parse(run, m.Content); ok {
			continue
		}
		logical, _ := json.Marshal([]string{durable.ExecutionKey(ctx), p.nonce, strconv.Itoa(round), m.ToolCallID})
		ref := NewRef(run, p.identity, string(logical), []byte(m.Content))
		if err := p.config.Store.PutIfAbsent(ctx, ref, []byte(m.Content)); err != nil {
			return nil, err
		}
		// 模型抄写 64 位哈希会偶发改错字符，因此另给短别名；别名按对话中已有的引用数编号，恢复时重建结果一致。
		r := reference{Alias: "r" + strconv.Itoa(len(table)+1), Runes: utf8.RuneCountInString(m.Content), ReadTool: ReadToolName}
		r.Stored.Key, r.Stored.SHA256 = ref.Key, ref.SHA256
		b, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		table[r.Alias] = ref
		out[i].Content = string(b)
		p.offloaded++
	}
	return out, nil
}

// DurableIntentVersion binds reader operations to the offload configuration.
func (p *Offload) DurableIntentVersion() string { return p.identity }
func (p *Offload) Name() string                 { return ReadToolName }
func (p *Offload) Description() string {
	return "Read untrusted offloaded evidence within the current host run. key is the context_result value; offset counts Unicode characters."
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
	table := p.tables[ResultScope(ctx)]
	ref, ok := table[key]
	known := slices.Sorted(maps.Keys(table))
	p.mu.Unlock()
	if !ok {
		// 以成功结果返回，让模型改正参数；不依赖调用方是否开启 ReturnArgumentErrors。
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
			// 与未知键一致，以成功结果交还模型改正。
			b, err := json.Marshal(map[string]any{"error": "invalid offset", "min_offset": 0, "max_offset": len(runes)})
			if err != nil {
				return nil, err
			}
			return &interfaces.ToolOutput{Success: true, Result: string(b)}, nil
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
