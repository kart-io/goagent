package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
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
	jobs, err := pendingJobs(ctx, m.cfg.Store)
	if err != nil {
		return 0, err
	}
	for i, j := range jobs {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		err := m.process(WithNamespace(ctx, j.Namespace...), j)
		switch {
		case err == nil:
			j.Status, j.Error = JobDone, ""
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
	return len(jobs), nil
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
	if j.Aborted {
		return m.learnFromFailure(ctx, j, transcript)
	}
	var out struct {
		Memories []candidate `json:"memories"`
	}
	if err := m.chatJSON(ctx, strings.TrimSpace(extractPrompt+"\n"+m.cfg.Instructions), transcript, &out); err != nil {
		return err
	}
	for i, c := range out.Memories {
		if err := m.reconcile(ctx, j, i, c); err != nil {
			return err
		}
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
	"content 写成脱离上下文也能看懂的完整陈述；importance 取 0 到 1，琐碎信息不要记；不要记录密码、密钥、令牌等凭证；" +
	"标注为[相关长期记忆]的内容是已有记忆，不要重复抽取。没有值得记的内容时输出 {\"memories\":[]}。只输出 JSON。"

const reconcilePrompt = "你维护长期记忆。给定一条新信息和若干条相似的已有记忆，判断如何处理，输出 JSON：" +
	`{"op":"add|update|invalidate|noop","target":"已有记忆的 id","content":"update 时合并后的完整内容"}。` +
	"add：新信息与已有记忆无关；update：新信息补充或更新了某条已有记忆，给出合并后的内容；" +
	"invalidate：新信息与某条已有记忆矛盾，以新信息为准；noop：已有记忆已经包含新信息。只输出 JSON。"

// reconcile 把候选与相似的已有记忆比较（Mem0）：新增、合并更新或否定旧记忆。被更新或否定的旧记忆标记失效并指向新记录（Zep），不删除。
func (m *Memory) reconcile(ctx context.Context, j Job, i int, c candidate) error {
	if strings.TrimSpace(c.Content) == "" || !slices.Contains(kinds, c.Kind) {
		return nil
	}
	c.Importance = min(max(c.Importance, 0), 1)
	similar, err := m.recall(ctx, Query{Text: c.Content, K: m.cfg.ReconcileCandidates, Kinds: []Kind{c.Kind}}, false)
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
	content := c.Content
	switch decision.Op {
	case "noop":
		return nil
	case "update":
		if strings.TrimSpace(decision.Content) != "" {
			content = decision.Content
		}
	case "invalidate":
	default:
		target = nil
	}
	r, err := m.Remember(ctx, Record{
		ID: id, Kind: c.Kind, Content: content, Keywords: c.Keywords, Entities: c.Entities,
		Importance: c.Importance, Source: Source{Session: j.Session, From: j.From, To: j.To, Job: j.Key},
	})
	if err != nil || target == nil {
		return err
	}
	return m.invalidate(ctx, *target, r.ID)
}

const failurePrompt = "这一轮执行失败或被中止。根据对话记录写一条以后可以避免同类失败的教训，输出 JSON：" +
	`{"lesson":"…","keywords":["…"],"importance":0.0}。看不出原因时 lesson 为空。只输出 JSON。`

// learnFromFailure 为中止的轮次写一条教训（Reflexion）。
func (m *Memory) learnFromFailure(ctx context.Context, j Job, transcript string) error {
	var out struct {
		Lesson     string   `json:"lesson"`
		Keywords   []string `json:"keywords"`
		Importance float64  `json:"importance"`
	}
	if err := m.chatJSON(ctx, failurePrompt, transcript, &out); err != nil || strings.TrimSpace(out.Lesson) == "" {
		return err
	}
	_, err := m.Remember(ctx, Record{
		ID: stableID(j.Key, "lesson"), Kind: KindReflection, Content: out.Lesson, Keywords: out.Keywords,
		Importance: min(max(out.Importance, 0), 1), Source: Source{Session: j.Session, From: j.From, To: j.To, Job: j.Key},
	})
	return err
}

const reflectPrompt = "以下是同一用户近期的若干经历。提炼一到三条更高层的结论（如习惯、偏好、反复出现的问题），输出 JSON：" +
	`{"insights":[{"content":"…","keywords":["…"],"importance":0.0}]}。只输出 JSON。`

// reflect 在未参与反思的有效情景累计重要度达到阈值时提炼结论（Generative Agents），并把这些情景标记为已反思。
func (m *Memory) reflect(ctx context.Context) error {
	ns, err := requireNamespace(ctx)
	if err != nil {
		return err
	}
	all, err := m.records.List(ctx, ns)
	if err != nil {
		return err
	}
	var episodes []Record
	total := 0.0
	for _, r := range all {
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
	for i, in := range out.Insights {
		if strings.TrimSpace(in.Content) == "" {
			continue
		}
		if _, err := m.Remember(ctx, Record{ID: stableID(key, fmt.Sprint(i)), Kind: KindReflection, Content: in.Content, Keywords: in.Keywords, Importance: min(max(in.Importance, 0), 1)}); err != nil {
			return err
		}
	}
	for _, r := range episodes {
		r.Reflected = true
		if err := m.records.Put(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// renderTurn 把序号区间内的消息事件写成纯文本；跳过注入的召回块。
func renderTurn(events []session.Event, from, to int64) string {
	var sb strings.Builder
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
		if d.Message.Content != "" {
			fmt.Fprintf(&sb, "[%s] %s\n", d.Message.Role, truncate(d.Message.Content, 2000))
		}
		for _, tc := range d.Message.ToolCalls {
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

// chatJSON 请求模型并解析回复中的 JSON 对象（容忍代码块包裹与前后说明）。
func (m *Memory) chatJSON(ctx context.Context, system, user string, out any) error {
	resp, err := llm.Chat(ctx, m.cfg.Model, []llm.Message{llm.SystemMessage(system), llm.UserMessage(user)})
	if err != nil {
		return err
	}
	text := resp.Content
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return agentErrors.Newf(agentErrors.CodeLLMResponse, "记忆模型没有返回 JSON").WithComponent("memory").WithOperation("chat_json")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), out); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeLLMResponse, "记忆模型返回的 JSON 无法解析").WithComponent("memory").WithOperation("chat_json")
	}
	return nil
}
