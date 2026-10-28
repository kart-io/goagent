// Package opscase 收纳运维 AI 案例在示例层补齐的宿主侧能力：
// 证据账本（工具结果编号与摘录核对）、结构化结论提交与有界修复、开启参数错误交还的 Dispatcher、严格 JSON 输出合同。
// 账本与结论都经 DispatchConfig.OnResult 登记，崩溃恢复后随日志回放重建。
package opscase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Dispatch 让工具经过开启 ReturnArgumentErrors 的 Dispatcher：参数不合法时交还模型改正。
// 返回顺序与输入一致。MaxConcurrency 沿用框架默认值：账本与结论提交都可并发调用。
func Dispatch(ts []interfaces.Tool, cfg tools.DispatchConfig) ([]interfaces.Tool, error) {
	reg := tools.NewRegistry()
	for _, t := range ts {
		if err := reg.Register(t); err != nil {
			return nil, err
		}
	}
	cfg.ReturnArgumentErrors = true
	d, err := tools.NewDispatcher(reg, cfg)
	if err != nil {
		return nil, err
	}
	byName := map[string]interfaces.Tool{}
	for _, t := range d.Tools() {
		byName[t.Name()] = t
	}
	out := make([]interfaces.Tool, 0, len(ts))
	for _, t := range ts {
		out = append(out, byName[t.Name()])
	}
	return out, nil
}

// Tool 用普通函数构造工具；fn 返回 *tools.ArgumentError 时交还模型改正。
func Tool(name, description, schema string, fn func(context.Context, map[string]any) (any, error)) interfaces.Tool {
	return tools.NewBaseTool(name, description, schema, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		result, err := fn(ctx, in.Args)
		if err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: result}, nil
	})
}

// Enum 把取值列表写成 JSON Schema 的 enum 数组。
func Enum[T any](values []T) string {
	b, _ := json.Marshal(values)
	return string(b)
}

// entry 是一次工具结果：text 按模型看到的字段顺序写成“字段名: 值”的规范文本，leaves 是其中的标量值。
// key 是派生引用号的执行键，用来区分重放同一结果与两个结果撞号。
type entry struct {
	ref, key string
	text     string
	leaves   []leaf
	keys     map[string]bool
}

// leaf 是工具结果中的一个标量值及其字段名。
type leaf struct{ key, value string }

// Ledger 给数据工具的每次结果分配引用号，并保存模型看到的内容，供结论核对引用的摘录确实出自该次结果。
//
// 引用号写进工具结果交给模型，结果随调用写入日志，回放时模型看到的与首次一致。持久调用的引用号由
// 执行键（父键 + CallID 派生，重启后不变）的摘要取 6 位十六进制，形如 E3fa29c：与同轮并发调用的
// 执行先后无关，父子会话共用一本账也不会撞号；万一撞号（两个不同调用得到同一引用号）登记时报错中止。
// 没有 CallID 的调用（非持久运行或直接 Invoke）按本进程登记顺序编号 E1、E2…，只在顺序执行时确定。
//
// 登记只经 OnResult：它必须配到包含 Wrap 工具的 Dispatcher 上。首次执行与日志回放都会触发，
// 崩溃恢复后账本因此按原结果重建。
type Ledger struct {
	mu      sync.Mutex
	names   map[string]bool
	entries map[string]entry
	seq     int
	runs    int
}

func NewLedger() *Ledger { return &Ledger{names: map[string]bool{}, entries: map[string]entry{}} }

// Wrap 在 tool 的结果里加入 ref 字段。结果不是对象时包成 {"ref","data"}。
func (l *Ledger) Wrap(tool interfaces.Tool) interfaces.Tool {
	l.mu.Lock()
	l.names[tool.Name()] = true
	l.mu.Unlock()
	return tools.NewBaseTool(tool.Name(), tool.Description(), tool.ArgsSchema(), func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		out, err := tool.Invoke(ctx, in)
		if err != nil || out == nil || !out.Success {
			return out, err
		}
		result := map[string]any{}
		if m, ok := out.Result.(map[string]any); ok {
			for k, v := range m {
				result[k] = v
			}
		} else {
			result["data"] = out.Result
		}
		// ref 最后写入，工具结果自带的同名字段不能改掉账本编号。
		result["ref"] = l.ref(in)
		// 以字符串交给模型，Builder 对字符串原样传入；自行编码时同样不能把 <、>、& 转义成 \u003c 之类，模型才能原样摘录。
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(result); err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: strings.TrimSpace(buf.String())}, nil
	})
}

func (l *Ledger) ref(in *interfaces.ToolInput) string {
	if in.CallID != "" && in.ExecutionKey != "" {
		sum := sha256.Sum256([]byte(in.ExecutionKey))
		return "E" + hex.EncodeToString(sum[:])[:6]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	return fmt.Sprintf("E%d", l.seq)
}

// OnResult 供 DispatchConfig.OnResult 使用：登记 Wrap 过的工具的成功结果，其余结果忽略。
func (l *Ledger) OnResult(_ context.Context, r tools.DispatchResult) error {
	l.mu.Lock()
	wrapped := l.names[r.Name]
	l.mu.Unlock()
	if !wrapped || r.Output == nil || !r.Output.Success {
		return nil
	}
	text, ok := r.Output.Result.(string)
	if !ok {
		return fmt.Errorf("工具 %s 的结果不是账本编码的文本", r.Name)
	}
	e := entry{key: r.ExecutionKey, keys: map[string]bool{}}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber() // 数字保留原文，2400000 不会变成 2.4e+06
	body, err := render(dec, "", true, &e)
	if err != nil {
		return err
	}
	if e.ref == "" {
		return fmt.Errorf("工具 %s 的结果缺少引用号", r.Name)
	}
	e.text = norm(body)
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.entries[e.ref]; ok {
		if old.key != e.key || old.text != e.text {
			return fmt.Errorf("引用号 %s 冲突：两个不同的工具结果得到同一编号", e.ref)
		}
		return nil
	}
	l.entries[e.ref] = e
	if !r.Replayed {
		l.runs++
	}
	return nil
}

// Restore 在账本缺少 ref 时，用宿主已核对过的摘录补登一条：只有这些摘录（及其中首尾完整的片段）能通过核对。
// 用于结果不在本 Dispatcher 日志里的场景，例如子会话已结束、只剩父会话日志中的报告可回放。
func (l *Ledger) Restore(ref string, excerpts []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.entries[ref]; ok || len(excerpts) == 0 {
		return
	}
	parts := make([]string, len(excerpts))
	for i, x := range excerpts {
		parts[i] = norm(x)
	}
	// \x00 不会出现在规范化后的摘录里，摘录因此不能跨两条已核对的片段拼接。
	l.entries[ref] = entry{ref: ref, text: strings.Join(parts, "\x00"), keys: map[string]bool{}}
}

// render 按模型看到的 JSON 原样顺序展开结果（结构体按字段顺序、map 按键名排序），顶层 ref 记为引用号，不计入内容。
func render(dec *json.Decoder, key string, top bool, e *entry) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	var parts []string
	switch tok {
	case json.Delim('{'):
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return "", err
			}
			name := k.(string)
			if top && name == "ref" {
				if e.ref, err = render(dec, name, false, &entry{keys: map[string]bool{}}); err != nil {
					return "", err
				}
				continue
			}
			value, err := render(dec, name, false, e)
			if err != nil {
				return "", err
			}
			e.keys[name] = true
			parts = append(parts, name+": "+value)
		}
	case json.Delim('['):
		for dec.More() {
			value, err := render(dec, key, false, e)
			if err != nil {
				return "", err
			}
			parts = append(parts, value)
		}
	default:
		value := fmt.Sprint(tok)
		if tok == nil {
			value = "null"
		}
		e.leaves = append(e.leaves, leaf{key, value})
		return value, nil
	}
	_, err = dec.Token() // 结束的 } 或 ]
	return strings.Join(parts, " "), err
}

// Len 返回已登记的结果数（含回放重建的）；Runs 只计本进程首次执行的。
func (l *Ledger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *Ledger) Runs() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.runs
}

// Refs 返回已登记的引用号，按字典序排列。
func (l *Ledger) Refs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Sorted(maps.Keys(l.entries))
}

// Values 返回已登记结果中的全部标量值，供宿主核对自由文本中的数字。
func (l *Ledger) Values() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, ref := range slices.Sorted(maps.Keys(l.entries)) {
		for _, lf := range l.entries[ref].leaves {
			out = append(out, lf.value)
		}
	}
	return out
}

// Citation 是结论中的一条证据：引用号加从该结果中原样摘出的片段。
type Citation struct {
	Ref     string `json:"ref"`
	Excerpt string `json:"excerpt"`
}

// Verify 逐条核对引用，返回不通过的原因；全部通过时返回 nil。
// 摘录必须是该结果中连续的一段（可以跨字段，JSON 引号、括号、逗号可省略），首尾落在词边界上：
// 不能截断数字或单词，不能丢掉负号，不能只有字段名；短于 3 个字母或数字的摘录必须等于完整的字段值。
// 核对只证明摘录确实出自该次结果，不证明摘录支撑结论。
func (l *Ledger) Verify(cs []Citation) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var problems []string
	for i, c := range cs {
		e, ok := l.entries[c.Ref]
		// 结果里的 ref 不算内容；模型整段照抄时一并带上的 ref 先去掉。
		excerpt := norm(refField.ReplaceAllString(norm(c.Excerpt), " "))
		quoted := fmt.Sprintf("evidence[%d] 的摘录 %q", i, truncate(c.Excerpt, 60))
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("evidence[%d]: 引用 %q 不存在，可用引用：%s", i, c.Ref, strings.Join(slices.Sorted(maps.Keys(l.entries)), ",")))
		case alnum(excerpt) < 3 && !slices.ContainsFunc(e.leaves, func(lf leaf) bool { return norm(lf.value) == excerpt }):
			problems = append(problems, quoted+"过短，短于 3 个字母或数字时必须是完整的字段值")
		case alnum(stripKeys(excerpt, e.keys)) == 0:
			problems = append(problems, quoted+"只有字段名，必须包含字段值")
		case !bounded(e.text, excerpt):
			problems = append(problems, quoted+"不在 "+c.Ref+" 的结果中，必须原样复制完整的词与数字（含负号）")
		}
	}
	return problems
}

var (
	spaces   = regexp.MustCompile(`\s+`)
	keyColon = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*`)
	keyOnly  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*:`)
	refField = regexp.MustCompile(`\bref: E[0-9a-f]+\b`)
	jsonMark = strings.NewReplacer(`\n`, " ", `\t`, " ", `\"`, " ", `"`, " ", "{", " ", "}", " ", "[", " ", "]", " ", ",", " ")
)

// norm 是摘录与结果共用的规范化：去掉 JSON 标点与转义、统一“字段名: ”写法、压缩空白、去掉外层引号。
func norm(s string) string {
	s = keyColon.ReplaceAllString(jsonMark.Replace(s), "$1: ")
	return strings.Trim(spaces.ReplaceAllString(s, " "), "'`“”‘’ ")
}

func stripKeys(s string, keys map[string]bool) string {
	var rest []string
	for _, f := range strings.Fields(keyOnly.ReplaceAllString(s, " ")) {
		if !keys[f] {
			rest = append(rest, f)
		}
	}
	return strings.Join(rest, " ")
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func alnum(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// bounded 判断 sub 是否作为完整片段出现在 s 中：两端都不能与相邻字符连成同一个词或数字。
func bounded(s, sub string) bool {
	if sub == "" {
		return false
	}
	for start := 0; ; {
		i := strings.Index(s[start:], sub)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(sub)
		if !joined(s, i-1, sub[0], -1) && !joined(s, end, sub[len(sub)-1], 1) {
			return true
		}
		start = i + 1
	}
}

// joined 判断 s[at] 与片段端点 edge 是否连在一起；dir 指向远离片段的方向。
//   - 端点是字母数字：相邻字母数字算连着；数字中的小数点也算，所以 87.5% 里摘出 5% 或 87 都不算完整片段；
//     数字左侧紧邻的 - 是负号，-100.00 里摘出 100.00 会反转方向，也算截断。
//   - 端点是标点：相邻字母数字算连着，所以 checkout-api 里摘出 -api、31/200 里摘出 /200 都不算完整片段。
func joined(s string, at int, edge byte, dir int) bool {
	if at < 0 || at >= len(s) {
		return false
	}
	switch {
	case word(edge):
		if digit(edge) && s[at] == '.' {
			next := at + dir
			return next >= 0 && next < len(s) && digit(s[next])
		}
		if digit(edge) && dir < 0 && s[at] == '-' {
			return true
		}
		return word(s[at])
	case edge < 0x80 && edge != ' ':
		return word(s[at])
	}
	return false
}

func digit(b byte) bool { return '0' <= b && b <= '9' }

func word(b byte) bool { return b == '_' || digit(b) || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' }

// EvidenceSchema 是结论 Schema 中 evidence 字段的定义。
const EvidenceSchema = `"evidence":{"type":"array","minItems":1,"items":{"type":"object","properties":{"ref":{"type":"string"},"excerpt":{"type":"string"}},"required":["ref","excerpt"],"additionalProperties":false}}`

// SubmitTool 是结论提交工具的名字；Agent 应以 WithReturnDirectTools(SubmitTool) 构建。
const SubmitTool = "submit_conclusion"

// Submitter 实现 submit_conclusion：解析结构化结论 C，按账本核对证据并执行 Check；
// 不通过作为参数错误交还模型修复，修复 MaxRepairs 次后仍不通过就中止。
// 通过时返回 Answer(C) 渲染的最终回答；配合 WithReturnDirectTools，它直接成为 Agent 的答案。
// 被接受的结论由 OnResult 从该次调用的参数中取出：首次执行与日志回放走同一路径，宿主不另行落盘，
// Agent 一结束（提交已写入日志）就能在崩溃后恢复。
type Submitter[C any] struct {
	Ledger     *Ledger
	MaxRepairs int
	Evidence   func(C) []Citation
	Check      func(C) []string // 可选的附加核验
	Answer     func(C) string   // 渲染最终回答，第一行应为“结论：…”

	mu          sync.Mutex
	accepted    *C
	submissions int
	repairs     int
	rejections  []string
}

// Tool 返回 submit_conclusion 工具。
func (s *Submitter[C]) Tool(description, schema string) interfaces.Tool {
	return Tool(SubmitTool, description, schema, func(_ context.Context, a map[string]any) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.submissions++
		c, err := decode[C](a)
		if err != nil {
			return nil, err
		}
		problems := s.Ledger.Verify(s.Evidence(c))
		if s.Check != nil {
			problems = append(problems, s.Check(c)...)
		}
		if problems != nil {
			s.rejections = append(s.rejections, strings.Join(problems, "；"))
			if s.repairs >= s.MaxRepairs {
				return nil, fmt.Errorf("结论核验 %d 次仍未通过：%s", s.submissions, strings.Join(problems, "；"))
			}
			s.repairs++
			return nil, &tools.ArgumentError{Message: "结论核验未通过：" + strings.Join(problems, "；")}
		}
		return s.Answer(c), nil
	})
}

// OnResult 供 DispatchConfig.OnResult 使用：先交给账本登记数据工具结果，再从成功的 submit_conclusion 调用参数中取出结论。
func (s *Submitter[C]) OnResult(ctx context.Context, r tools.DispatchResult) error {
	if err := s.Ledger.OnResult(ctx, r); err != nil {
		return err
	}
	if r.Name != SubmitTool || r.Output == nil || !r.Output.Success {
		return nil
	}
	c, err := decode[C](r.Args)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepted = &c
	return nil
}

func decode[C any](a map[string]any) (C, error) {
	var c C
	data, err := json.Marshal(a)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(data, &c)
}

// Stats 返回被接受的结论（含回放恢复的），以及本进程内的提交、修复次数。
func (s *Submitter[C]) Stats() (*C, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted, s.submissions, s.repairs
}

// Accepted 返回通过核验的结论；模型没有提交通过核验的结论就报错，宿主只发布这份结论，不采信自由文本。
func (s *Submitter[C]) Accepted() (*C, error) {
	if c, _, _ := s.Stats(); c != nil {
		return c, nil
	}
	return nil, errors.New("模型没有提交通过核验的结论")
}

// Rejections 返回每次核验不通过的原因，便于从运行记录判断模型卡在哪里。
func (s *Submitter[C]) Rejections() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rejections)
}

var fence = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")

// StrictJSON 把模型输出解析为 v（指向结构体的指针）：只允许一个 JSON 对象（可包在一个代码块里），
// 字段名必须与 json 标签完全一致（区分大小写）、不得重复、不得未知，对象之后不得有多余内容。
// 取值合法性由调用方继续校验。
func StrictJSON(text string, v any) error {
	text = strings.TrimSpace(text)
	if m := fence.FindStringSubmatch(text); m != nil {
		text = m[1]
	}
	if !strings.HasPrefix(text, "{") {
		return errors.New("输出必须是一个 JSON 对象")
	}
	dec := json.NewDecoder(strings.NewReader(text))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("输出不是合法 JSON 对象：%w", err)
	}
	if strings.TrimSpace(text[dec.InputOffset():]) != "" {
		return errors.New("JSON 对象之后还有多余内容")
	}
	if err := exactKeys(raw, reflect.TypeOf(v)); err != nil {
		return err
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(v); err != nil {
		return fmt.Errorf("输出不是合法 JSON 对象：%w", err)
	}
	return nil
}

// exactKeys 按目标类型递归检查对象字段：encoding/json 默认大小写不敏感、重复键取最后一个，这里都拒绝。
func exactKeys(raw json.RawMessage, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		fields := map[string]reflect.Type{}
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			fields[name] = f.Type
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			return nil // 类型不符交给解码报错
		}
		seen := map[string]bool{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			key := tok.(string)
			ft, ok := fields[key]
			switch {
			case seen[key]:
				return fmt.Errorf("字段 %q 重复", key)
			case !ok:
				return fmt.Errorf("未知字段 %q", key)
			}
			seen[key] = true
			var val json.RawMessage
			if err := dec.Decode(&val); err != nil {
				return err
			}
			if err := exactKeys(val, ft); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) == nil {
			for _, item := range items {
				if err := exactKeys(item, t.Elem()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
