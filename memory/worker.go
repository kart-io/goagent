package memory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// Enqueue 登记一轮的抽取任务；同一 Key 已登记时不做任何事。Builder 在轮次结束（含中止）时调用。
func (m *Memory) Enqueue(ctx context.Context, j Job) error {
	if j.Key == "" || j.Session == "" || j.From <= 0 || j.To < j.From {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "抽取任务需要 Key、会话与合法的序号区间").WithComponent("memory").WithOperation("enqueue")
	}
	if err := validNamespace(j.Namespace); err != nil {
		return err
	}
	// 没有作用域的任务来自 G0 的绑定，可能跨越了 Purge；作用域不是当前的说明这一轮跨越了 Purge。
	// 两者都丢弃并返回 nil：已完成轮次的回放会再次登记，不能报错（设计 4.3、4.5）。
	if j.Scope == "" {
		m.droppedJobs.Add(1)
		return nil
	}
	meta, err := m.loadMeta(ctx, j.Namespace)
	if err != nil {
		return err
	}
	if meta.Current != j.Scope {
		m.droppedJobs.Add(1)
		return nil
	}
	if _, ok, err := getJob(ctx, m.cfg.Store, j.Key); err != nil || ok {
		return err
	}
	j.Namespace = slices.Clone(j.Namespace)
	j.Status, j.Attempts, j.Error, j.CreatedAt = JobPending, 0, "", m.now()
	return putJob(ctx, m.cfg.Store, j)
}

// Job 返回一个任务的当前状态。
func (m *Memory) Job(ctx context.Context, key string) (Job, bool, error) {
	return getJob(ctx, m.cfg.Store, key)
}

// Run 循环处理待处理的任务，直到 ctx 结束。同一任务存储只应运行一个 Worker：任务的领取没有互斥。
// 单个任务的失败记入任务并按 JobAttempts 重试；读取任务列表失败时调用 onError（可为 nil）后等待下一轮。
func (m *Memory) Run(ctx context.Context, onError func(error)) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		n, err := m.ProcessPending(ctx)
		if err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		wait := constants.DefaultMemoryWorkerInterval
		if n > 0 {
			wait = 0
		}
		timer.Reset(wait)
	}
}

// ProcessPending 依次处理当前全部待处理任务，返回处理的任务数；任务自身的失败记入任务，不作为返回错误。
func (m *Memory) ProcessPending(ctx context.Context) (int, error) {
	if m.cfg.Model == nil || m.cfg.Sessions == nil {
		return 0, agentErrors.Newf(agentErrors.CodeInvalidConfig, "抽取需要 Model 与 Sessions").WithComponent("memory").WithOperation("process")
	}
	cleanupErr := m.cleanupRetiring(ctx)
	jobs, err := pendingJobs(ctx, m.cfg.Store)
	if err != nil {
		return 0, errors.Join(cleanupErr, err)
	}
	for i, j := range jobs {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		err := m.processInScope(ctx, j)
		switch {
		case err == nil:
			j.Status, j.Error = JobDone, ""
		case errors.Is(err, errScopeChanged):
			m.droppedJobs.Add(1)
			j.Status, j.Error = JobDropped, err.Error()
		case ctx.Err() != nil:
			return i, ctx.Err()
		default:
			j.Attempts++
			j.Error = err.Error()
			if j.Attempts >= m.cfg.JobAttempts {
				j.Status = JobFailed
			}
		}
		if err := putJob(ctx, m.cfg.Store, j); err != nil {
			return i, err
		}
	}
	return len(jobs), cleanupErr
}

type jobScopeKey struct{}

// processInScope 只在任务的作用域仍是当前作用域时处理任务；处理期间的每次读取都核对作用域（checkJobScope），
// 任务跨越 Purge 时返回 errScopeChanged，不会写入新作用域。
func (m *Memory) processInScope(ctx context.Context, j Job) error {
	if j.Scope == "" {
		return errScopeChanged
	}
	meta, err := m.loadMeta(ctx, j.Namespace)
	if err != nil {
		return err
	}
	if meta.Current != j.Scope {
		return errScopeChanged
	}
	return m.process(context.WithValue(WithNamespace(ctx, j.Namespace...), jobScopeKey{}, j.Scope), j)
}

// jobScope 返回 Worker 正在处理的任务的作用域；不在任务中时为空（不限定作用域）。
func jobScope(ctx context.Context) string {
	s, _ := ctx.Value(jobScopeKey{}).(string)
	return s
}

// checkJobScope 核对读取得到的作用域是正在处理的任务的作用域。
func checkJobScope(ctx context.Context, scope string) error {
	if want := jobScope(ctx); want != "" && want != scope {
		return errScopeChanged
	}
	return nil
}

// process 抽取一轮的记忆。记录 ID 由任务 Key 与序号决定，崩溃后重做同一任务覆盖而不是重复。
func (m *Memory) process(ctx context.Context, j Job) error {
	events, err := m.cfg.Sessions.Read(ctx, j.Session, 0)
	if err != nil {
		return err
	}
	source := events
	if !j.Aborted {
		source = session.Active(events)
	}
	transcript := renderTurn(source, j.From, j.To)
	if transcript == "" {
		// 轮次已被退回，或只有注入的召回块。
		return nil
	}
	at := turnTime(events, j.From, m.now())
	if j.Aborted {
		return m.learnFromFailure(ctx, j, at, transcript)
	}
	var out struct {
		Memories  []candidate `json:"memories"`
		Relations []relation  `json:"relations"`
	}
	prompt := extractPrompt
	if m.cfg.Graph {
		prompt += relationPrompt
	}
	dated := "对话时间：" + at.Format(time.DateTime) + "\n" + transcript
	if err := m.chatJSON(ctx, strings.TrimSpace(prompt+"\n"+m.cfg.Instructions), dated, &out); err != nil {
		return err
	}
	for i, c := range out.Memories {
		if err := m.reconcile(ctx, j, at, i, c); err != nil {
			return err
		}
	}
	if err := m.applyRelations(ctx, j, at, out.Relations); err != nil {
		return err
	}
	if err := m.syncCommunities(ctx, j.Namespace); err != nil {
		return err
	}
	if err := m.reflect(ctx); err != nil {
		return err
	}
	return m.Prune(ctx)
}

type candidate struct {
	Kind       Kind     `json:"kind"`
	Content    string   `json:"content"`
	Keywords   []string `json:"keywords"`
	Entities   []string `json:"entities"`
	Importance float64  `json:"importance"`
}

const extractPrompt = "你从一轮对话中抽取值得跨会话长期记住的信息，输出 JSON：" +
	`{"memories":[{"kind":"fact|episode|skill","content":"…","keywords":["…"],"entities":["…"],"importance":0.0}]}。` +
	"fact 是关于用户或业务的稳定事实、偏好与决定；episode 是本轮做了什么、结果如何（至多一条）；skill 是本轮验证有效、可复用的做法。" +
	"content 写成脱离上下文也能看懂的完整陈述，对话中的相对时间（昨天、上周、下个月）按对话时间换算成具体日期写入；importance 取 0 到 1，琐碎信息不要记；不要记录密码、密钥、令牌等凭证；" +
	"标注为[相关长期记忆]的内容是已有记忆，不要重复抽取。没有值得记的内容时输出 {\"memories\":[]}。只输出 JSON。"

// relationPrompt 在启用图谱时附在抽取提示词后。
const relationPrompt = "\n另外在同一个 JSON 中输出实体之间的关系：" +
	`"relations":[{"subject":{"name":"…","type":"person|org|place|product|concept|…","aliases":["…"]},"predicate":"…","object":{"name":"…","type":"…"},"fact":"一句完整陈述","exclusive":true}]。` +
	"fact 中的相对时间同样换算成具体日期；用户本人的 name 写\"用户\"、type 写 person；predicate 用简短的动词短语，同一含义用同一说法；" +
	"exclusive 表示主体在该谓词上同时只能有一个客体（如住在、就职于），可以有多个时（如喜欢、认识）为 false。没有关系时输出空数组。"

const reconcilePrompt = "你维护长期记忆。给定一条新信息和若干条相似的已有记忆，判断如何处理，输出 JSON：" +
	`{"op":"add|update|invalidate|noop","target":"已有记忆的 id","content":"update 时合并后的完整内容"}。` +
	"add：新信息与已有记忆无关；update：新信息补充或更新了某条已有记忆，给出合并后的内容；" +
	"invalidate：新信息与某条已有记忆矛盾，以新信息为准；noop：已有记忆已经包含新信息。只输出 JSON。"

// reconcile 把候选与相似的已有记忆比较（Mem0）：新增、合并更新或否定旧记忆。被更新或否定的旧记忆标记失效并指向新记录（Zep），不删除。
func (m *Memory) reconcile(ctx context.Context, j Job, at time.Time, i int, c candidate) error {
	if strings.TrimSpace(c.Content) == "" || !slices.Contains(kinds, c.Kind) {
		return nil
	}
	c.Importance = min(max(c.Importance, 0), 1)
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	similar, scope, err := m.search(ctx, ns, Query{Text: c.Content, K: m.cfg.ReconcileCandidates, Kinds: []Kind{c.Kind}})
	if err == nil {
		err = checkJobScope(ctx, scope)
	}
	if err != nil {
		return err
	}
	decision := struct {
		Op      string `json:"op"`
		Target  string `json:"target"`
		Content string `json:"content"`
	}{Op: "add"}
	if len(similar) > 0 {
		var existing strings.Builder
		for _, s := range similar {
			fmt.Fprintf(&existing, "- id=%s：%s\n", s.ID, s.Content)
		}
		if err := m.chatJSON(ctx, reconcilePrompt, "新信息："+c.Content+"\n\n已有记忆：\n"+existing.String(), &decision); err != nil {
			return err
		}
	}
	id := stableID(j.Key, fmt.Sprint(i))
	var target *Record
	for k := range similar {
		// 重做的任务会看到上次写入的同一记录，它不能否定自己。
		if similar[k].ID == decision.Target && decision.Target != id {
			target = &similar[k].Record
		}
	}
	content, sources := c.Content, []Source{j.source()}
	switch decision.Op {
	case "noop":
		return nil
	case "update":
		if strings.TrimSpace(decision.Content) != "" {
			content = decision.Content
		}
		if target != nil {
			// 合并后的内容同时依据旧记录与本轮。
			sources = append(slices.DeleteFunc(slices.Clone(target.Sources), func(s Source) bool { return s.Job == j.Key }), sources...)
		}
	case "invalidate":
	default:
		target = nil
	}
	now := m.now()
	return m.derive(ctx, ns, scope, []Record{{
		ID: id, Kind: c.Kind, Content: content, Keywords: c.Keywords, Entities: c.Entities, ValidFrom: at,
		Importance: c.Importance, Sources: sources,
	}}, func(t *tx) []string {
		if target == nil {
			return nil
		}
		// 只否定召回时看到的那个版本：期间被修改、遗忘或重建的记录不动。
		cur, ok := t.Records.Get(target.ID)
		if !ok || !cur.Valid() || cur.Incarnation != target.Incarnation || cur.Revision != target.Revision {
			return nil
		}
		t.Records.Put(cur.ID, cur.invalidated(now, ReasonSuperseded, id))
		return []string{cur.ID}
	})
}

// derive 在一次提交中写入 Worker 推导出的记录：rs 中尚不存在的记录被创建（重做的任务不会重复创建或覆盖），
// 再调用 then 做同一提交中的其余修改，then 返回需要移出索引的记录。嵌入在锁外完成，索引在提交之后更新。
func (m *Memory) derive(ctx context.Context, ns []string, scope string, rs []Record, then func(t *tx) []string) error {
	vecs := make([][]float32, len(rs))
	for i := range rs {
		r, err := m.prepare(rs[i], ns)
		if err != nil {
			return err
		}
		if vecs[i], err = m.embed(ctx, r.Content); err != nil {
			return err
		}
		if vecs[i] != nil {
			r.EmbedModel = m.cfg.EmbedModel
		}
		rs[i] = r
	}
	var created []int
	var removed []string
	_, err := m.mutate(ctx, ns, scope, func(t *tx) error {
		created, removed = nil, nil
		for i, r := range rs {
			if createRecord(t, r) {
				created = append(created, i)
			}
		}
		if then != nil {
			removed = then(t)
		}
		return nil
	})
	if err != nil {
		return err
	}
	errs := []error{m.unindex(ctx, ns, removed...)}
	for _, i := range created {
		errs = append(errs, m.upsert(ctx, ns, rs[i].ID, vecs[i]))
	}
	return errors.Join(errs...)
}

const failurePrompt = "这一轮执行失败或被中止。根据对话记录写一条以后可以避免同类失败的教训，输出 JSON：" +
	`{"lesson":"…","keywords":["…"],"importance":0.0}。看不出原因时 lesson 为空。只输出 JSON。`

// learnFromFailure 为中止的轮次写一条教训（Reflexion）。
func (m *Memory) learnFromFailure(ctx context.Context, j Job, at time.Time, transcript string) error {
	var out struct {
		Lesson     string   `json:"lesson"`
		Keywords   []string `json:"keywords"`
		Importance float64  `json:"importance"`
	}
	if err := m.chatJSON(ctx, failurePrompt, transcript, &out); err != nil || strings.TrimSpace(out.Lesson) == "" {
		return err
	}
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	return m.derive(ctx, ns, jobScope(ctx), []Record{{
		ID: stableID(j.Key, "lesson"), Kind: KindReflection, Content: out.Lesson, Keywords: out.Keywords, ValidFrom: at,
		Importance: min(max(out.Importance, 0), 1), Sources: []Source{j.source()},
	}}, nil)
}

const reflectPrompt = "以下是同一用户近期的若干经历。提炼一到三条更高层的结论（如习惯、偏好、反复出现的问题），输出 JSON：" +
	`{"insights":[{"content":"…","keywords":["…"],"importance":0.0}]}。只输出 JSON。`

// reflect 在未参与反思的有效情景累计重要度达到阈值时提炼结论（Generative Agents），并把这些情景标记为已反思。
func (m *Memory) reflect(ctx context.Context) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	scope, s, err := m.read(ctx, ns)
	if err == nil {
		err = checkJobScope(ctx, scope)
	}
	if err != nil {
		return err
	}
	var episodes []Record
	total := 0.0
	for _, id := range slices.Sorted(maps.Keys(s.Records)) {
		r := s.Records[id]
		if r.Valid() && r.Kind == KindEpisode && !r.Reflected {
			episodes = append(episodes, r)
			total += r.Importance
		}
	}
	if total < m.cfg.ReflectionThreshold {
		return nil
	}
	var list strings.Builder
	ids := make([]string, 0, len(episodes))
	for _, r := range episodes {
		fmt.Fprintf(&list, "- %s\n", r.Content)
		ids = append(ids, r.ID)
	}
	var out struct {
		Insights []candidate `json:"insights"`
	}
	if err := m.chatJSON(ctx, reflectPrompt, list.String(), &out); err != nil {
		return err
	}
	key := stableID(ids...)
	var insights []Record
	for i, in := range out.Insights {
		if strings.TrimSpace(in.Content) == "" {
			continue
		}
		insights = append(insights, Record{ID: stableID(key, fmt.Sprint(i)), Kind: KindReflection, Content: in.Content, Keywords: in.Keywords, Importance: min(max(in.Importance, 0), 1)})
	}
	// 结论与"已反思"标记在同一次提交中；只标记仍是读取时那个版本的情景。
	return m.derive(ctx, ns, scope, insights, func(t *tx) []string {
		for _, e := range episodes {
			cur, ok := t.Records.Get(e.ID)
			if !ok || !cur.Valid() || cur.Incarnation != e.Incarnation || cur.Revision != e.Revision {
				continue
			}
			cur.Reflected = true
			cur.Revision++
			t.Records.Put(cur.ID, cur)
		}
		return nil
	})
}

// turnTime 返回轮次开始事件的时间：记录与关系的生效时间是对话发生的时间，而不是 Worker 处理的时间。
func turnTime(events []session.Event, from int64, fallback time.Time) time.Time {
	for _, ev := range events {
		if ev.Seq == from && !ev.Time.IsZero() {
			return ev.Time
		}
	}
	return fallback
}

// memoryTools 是长期记忆自身的工具：它们的调用与结果是记忆的副本，抽取时跳过，避免被遗忘的内容经此重新学回。
var memoryTools = []string{constants.ToolMemorySearch, constants.ToolMemorySave, constants.ToolMemoryForget}

// renderTurn 把序号区间内的消息事件写成纯文本；跳过注入的召回块与记忆工具的调用和结果。
func renderTurn(events []session.Event, from, to int64) string {
	var sb strings.Builder
	skipped := map[string]bool{}
	for _, ev := range events {
		if ev.Seq < from || ev.Seq > to {
			continue
		}
		switch ev.Type {
		case constants.SessionEventUserMessage, constants.SessionEventAssistantMessage, constants.SessionEventToolResult:
		default:
			continue
		}
		var d session.MessageData
		if json.Unmarshal(ev.Data, &d) != nil || strings.HasPrefix(d.Message.Content, RecallPrefix) {
			continue
		}
		if ev.Type == constants.SessionEventToolResult &&
			(skipped[d.Message.ToolCallID] || (d.Tool != nil && slices.Contains(memoryTools, d.Tool.Name))) {
			continue
		}
		if d.Message.Content != "" {
			fmt.Fprintf(&sb, "[%s] %s\n", d.Message.Role, truncate(d.Message.Content, 2000))
		}
		for _, tc := range d.Message.ToolCalls {
			if slices.Contains(memoryTools, tc.Function.Name) {
				skipped[tc.ID] = true
				continue
			}
			fmt.Fprintf(&sb, "[%s] 调用工具 %s：%s\n", d.Message.Role, tc.Function.Name, truncate(tc.Function.Arguments, 500))
		}
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func stableID(parts ...string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))[:32]
}

// chatJSON 请求模型并解析回复中的 JSON 对象（容忍代码块包裹与前后说明）。回复不是合法 JSON 时，把原回复与解析错误
// 交回模型修正一次：温度为 0 时重试同一请求会得到同样的错误输出，修正请求的输入不同。被截断的回复不修正。
func (m *Memory) chatJSON(ctx context.Context, system, user string, out any) error {
	msgs := []llm.Message{llm.SystemMessage(system), llm.UserMessage(user)}
	for attempt := 0; ; attempt++ {
		resp, err := llm.Chat(ctx, m.cfg.Model, msgs)
		if err != nil {
			return err
		}
		if resp.FinishReason != "" && resp.FinishReason != "stop" {
			return agentErrors.Newf(agentErrors.CodeLLMResponse, "记忆模型的回复不完整（finish_reason=%s）；因长度截断时调大模型的 MaxTokens", resp.FinishReason).
				WithComponent("memory").WithOperation("chat_json")
		}
		err = decodeJSON(resp.Content, out)
		if err == nil || attempt == constants.MemoryJSONRepairAttempts {
			return err
		}
		msgs = append(msgs, llm.Message{Role: "assistant", Content: resp.Content},
			llm.UserMessage("上面的回复不是合法 JSON（"+err.Error()+"）。按原要求重新输出完整、合法的 JSON，只输出 JSON。"))
	}
}

// decodeJSON 解析 text 中第一个 { 到最后一个 } 之间的 JSON 对象。
func decodeJSON(text string, out any) error {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return agentErrors.Newf(agentErrors.CodeLLMResponse, "记忆模型没有返回 JSON").WithComponent("memory").WithOperation("chat_json")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), out); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeLLMResponse, "记忆模型返回的 JSON 无法解析").WithComponent("memory").WithOperation("chat_json")
	}
	return nil
}
