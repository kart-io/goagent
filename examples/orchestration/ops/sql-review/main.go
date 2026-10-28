// SQL 变更工单审核：确定性规则门禁先判，规则拒绝的工单不调用模型；模型复核只能否决或升级为人工，
// 输出严格 JSON，不合法一律收敛为 escalate/high。test 环境且 approve+low 才自动执行；
// 其余进入审批暂停；批准时在运行锁内重新读取工单比对整张工单的哈希，暂停期间被修改就拒绝批准、保持等待。
// 执行节点再比对一次，覆盖批准之后到执行之间的改动。数据全部是合成的。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/ops/internal/opscase"
	"github.com/kart-io/goagent/workflow"
)

// Ticket 是 SQL 变更工单。EstRows 是提交人自报的估算影响行数，规则引擎不核实。
type Ticket struct {
	ID         string   `json:"id"`
	Env        string   `json:"env"`
	Statements []string `json:"statements"`
	EstRows    int      `json:"est_rows"`
}

// Tickets 是合成工单，也是模拟工单系统里没有改动记录时的内容。
var Tickets = map[string]Ticket{
	"T-101": {"T-101", "test", []string{"INSERT INTO demo_coupons (code, discount) VALUES ('WELCOME5', 5)"}, 1},
	"T-102": {"T-102", "test", []string{"DROP TABLE demo_sessions_old"}, 0},
	"T-103": {"T-103", "test", []string{"TRUNCATE TABLE demo_audit_tmp"}, 0},
	"T-104": {"T-104", "test", []string{"ALTER TABLE demo_users ADD COLUMN level INT", "UPDATE demo_users SET level = 1 WHERE id < 100"}, 99},
	"T-105": {"T-105", "test", []string{"UPDATE demo_orders SET status = 'closed'"}, 0},
	"T-106": {"T-106", "test", []string{"DELETE FROM demo_events WHERE created_at < '2026-01-01'"}, 2400000},
	"T-107": {"T-107", "prod", []string{"UPDATE demo_users SET nickname = 'guest_42' WHERE id = 42"}, 1},
	"T-108": {"T-108", "test", []string{"UPDATE demo_flags SET enabled = 1 WHERE name = 'beta_search'"}, 1},
}

// MaxRows 以上（不含）的 DML 规则只给警告，不允许自动执行。
const MaxRows = 10000

// StatementCheck 是规则引擎对一条语句的判定：ok、warn 或 block。
type StatementCheck struct {
	SQL   string   `json:"sql"`
	Level string   `json:"level"`
	Rules []string `json:"rules,omitempty"`
}

// Check 是纯确定性规则门禁。任一语句 block 则 reject；有 warn 则 review；否则 pass。没有语句也 reject。
func Check(t Ticket) ([]StatementCheck, string) {
	if len(t.Statements) == 0 {
		return []StatementCheck{{Level: "block", Rules: []string{"R4-工单没有语句"}}}, "reject"
	}
	checks := make([]StatementCheck, len(t.Statements))
	var ddl, dml bool
	for i, sql := range t.Statements {
		c := StatementCheck{SQL: sql, Level: "ok"}
		// 关键字只在字符串字面量之外识别，'x WHERE y' 不算 WHERE。
		stripped := literal.ReplaceAllString(strings.ToUpper(sql), "?")
		f := strings.Fields(stripped)
		block := func(rule string) { c.Level, c.Rules = "block", append(c.Rules, rule) }
		switch {
		// 分号（含末尾分号）、注释、反斜杠转义与未闭合引号一律拒绝：杜绝多语句拼接、注释吞掉 WHERE、改变引号边界。
		case forbidden.MatchString(sql) || strings.ContainsAny(stripped, `'"`):
			block("R4-禁止分号、注释、反斜杠与未闭合引号")
		case len(f) == 0:
			block("R4-语句类型不在白名单")
		case f[0] == "DROP" || f[0] == "TRUNCATE":
			ddl = true
			block("R1-禁止 DROP/TRUNCATE")
		case f[0] == "CREATE" && len(f) > 1 && (f[1] == "TABLE" || f[1] == "INDEX"):
			ddl = true
		case f[0] == "ALTER":
			ddl = true
			if len(f) < 4 || f[1] != "TABLE" || f[3] != "ADD" || slices.Contains(f, "DROP") || strings.Contains(stripped, ",") {
				block("R4-ALTER 只允许单一 ADD 子句")
			}
		case f[0] == "INSERT":
			dml = true
		case f[0] == "UPDATE" || f[0] == "DELETE":
			dml = true
			if cond, ok := outerWhere(stripped); !ok {
				block("R3-UPDATE/DELETE 必须带 WHERE")
			} else if constantCondition(cond) {
				block("R3-WHERE 条件恒真或未引用列")
			}
		default:
			block("R4-语句类型不在白名单")
		}
		checks[i] = c
	}
	verdict := "pass"
	for i := range checks {
		c := &checks[i]
		if ddl && dml {
			c.Level, c.Rules = "block", append(c.Rules, "R2-同一工单不得混合 DDL 与 DML")
		}
		if t.EstRows > MaxRows && c.Level == "ok" && dml {
			c.Level, c.Rules = "warn", append(c.Rules, fmt.Sprintf("R5-自报影响行数 %d 超过 %d", t.EstRows, MaxRows))
		}
		if c.Level == "block" {
			verdict = "reject"
		} else if c.Level == "warn" && verdict == "pass" {
			verdict = "review"
		}
	}
	return checks, verdict
}

var (
	forbidden = regexp.MustCompile(`;|#|\\|--|/\*`)
	// literal 匹配单、双引号字符串字面量；反斜杠已被拒绝，'' 转义按两个相邻字面量处理即可。
	literal = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	whereKw = regexp.MustCompile(`\bWHERE\b`)
	orKw    = regexp.MustCompile(`\bOR\b`)
	ident   = regexp.MustCompile(`\b[A-Z_][A-Z0-9_]*\b`)
	sameEq  = regexp.MustCompile(`^([^\s=<>!]+)\s*(?:=|<=|>=)\s*([^\s=<>!]+)$`)
	// notColumn 是条件里不算列引用的关键字。
	notColumn = []string{"AND", "OR", "NOT", "IS", "NULL", "TRUE", "FALSE", "UNKNOWN", "IN", "LIKE", "BETWEEN", "ORDER", "BY", "LIMIT", "ASC", "DESC"}
)

// outerWhere 返回括号深度 0 处第一个 WHERE 之后的条件文本（输入已剥离字面量并转大写）；
// 子查询里的 WHERE 不算，括号不配对视为没有 WHERE。
func outerWhere(s string) (string, bool) {
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth--; depth < 0 {
				return "", false
			}
		}
	}
	if depth != 0 {
		return "", false
	}
	for _, loc := range whereKw.FindAllStringIndex(s, -1) {
		if strings.Count(s[:loc[0]], "(") == strings.Count(s[:loc[0]], ")") {
			return s[loc[1]:], true
		}
	}
	return "", false
}

// constantCondition 是示例级的恒真判定：按 OR 拆分（不论括号层级，宁可误拦），任一分支不引用列
// （WHERE 1、1=1、TRUE、'a'='a'、NOW() > 0）或两侧文本相同（id = id）即视为恒真。
// 不做语义分析：id IS NULL OR id IS NOT NULL、id >= 0 这类依赖值域的恒真条件拦不住，靠 R5 与人工审批兜底。
func constantCondition(cond string) bool {
	for _, branch := range orKw.Split(cond, -1) {
		if m := sameEq.FindStringSubmatch(strings.Trim(branch, " ()")); m != nil && m[1] == m[2] {
			return true
		}
		column := false
		for _, loc := range ident.FindAllStringIndex(branch, -1) {
			// 紧跟左括号的是函数名，不算列。
			isFunc := strings.HasPrefix(strings.TrimLeft(branch[loc[1]:], " "), "(")
			if !isFunc && !slices.Contains(notColumn, branch[loc[0]:loc[1]]) {
				column = true
				break
			}
		}
		if !column {
			return true
		}
	}
	return false
}

// Hash 是整张工单（id、env、statements、est_rows）的 sha256，用于作废判断、运行 ID 与执行记录键。
func Hash(t Ticket) string {
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Verdict 是模型复核的严格 JSON 输出。
type Verdict struct {
	Decision  string   `json:"decision"`
	RiskLevel string   `json:"risk_level"`
	Summary   string   `json:"summary"`
	Concerns  []string `json:"concerns"`
}

// Decisions 与 RiskLevels 是复核结论的取值范围，提示词与解析共用。
var Decisions, RiskLevels = []string{"approve", "reject", "escalate"}, []string{"low", "medium", "high"}

// ParseVerdict 解析模型输出；任何不合法都收敛为 escalate/high，并返回原因。不重试。
func ParseVerdict(text string) (Verdict, string) {
	var v Verdict
	err := opscase.StrictJSON(text, &v)
	switch {
	case err != nil:
	case !slices.Contains(Decisions, v.Decision):
		err = fmt.Errorf("decision %q 不在枚举内", v.Decision)
	case !slices.Contains(RiskLevels, v.RiskLevel):
		err = fmt.Errorf("risk_level %q 不在枚举内", v.RiskLevel)
	case strings.TrimSpace(v.Summary) == "":
		err = errors.New("summary 为空")
	}
	if err != nil {
		return Verdict{Decision: "escalate", RiskLevel: "high", Summary: "模型输出不合法，按最保守结论转人工"}, err.Error()
	}
	return v, ""
}

var reviewPrompt = `你是 DBA 变更复核员。输入是一张 SQL 变更工单和规则引擎的判定结果（规则已放行，不必重复检查语法）。
评估执行风险：影响行数、是否可回滚、是否涉及生产环境。你只能批准、拒绝或升级人工，不能推翻规则。
只输出一个 JSON 对象，不要任何其他文字。decision 取值 ` + opscase.Enum(Decisions) + `，risk_level 取值 ` + opscase.Enum(RiskLevels) + `：
{"decision":"...","risk_level":"...","summary":"一句话结论","concerns":["关注点"]}`

// Review 是图状态。Status：rejected、applied、invalidated；暂停时由 Run 标记为 waiting。
type Review struct {
	Ticket      Ticket           `json:"ticket"`
	Hash        string           `json:"hash"`
	Checks      []StatementCheck `json:"checks"`
	RuleVerdict string           `json:"rule_verdict"`
	Verdict     *Verdict         `json:"verdict,omitempty"`
	ModelError  string           `json:"model_error,omitempty"`
	Status      string           `json:"status"`
	Receipt     string           `json:"receipt,omitempty"`
}

func build(ctx context.Context, client *livemodel.Client, store durable.Store, applies *atomic.Int64) (*workflow.DurableGraph[Review], error) {
	gate := func(_ context.Context, r Review) (Review, error) {
		if r.Checks, r.RuleVerdict = Check(r.Ticket); r.RuleVerdict == "reject" {
			r.Status = "rejected"
		}
		return r, nil
	}
	review := func(ctx context.Context, r Review) (Review, error) {
		// 推理模型可能推理较长，留足 MaxTokens 避免被 length 截断；不改变请求数。
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope("reviewer").WithSystemPrompt(reviewPrompt).
			WithMaxIterations(1).WithMaxTokens(8192).Build()
		if err != nil {
			return r, err
		}
		// 这是交给模型的输入文本而非工具结果，框架对工具结果不转义的修复不覆盖这里；不转义 <、>，模型看到的 SQL 与工单原文一致。
		var input strings.Builder
		enc := json.NewEncoder(&input)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]any{"ticket": r.Ticket, "rule_verdict": r.RuleVerdict, "checks": r.Checks})
		out, err := agent.AsRunnable().Invoke(ctx, "工单："+input.String())
		if err != nil && ctx.Err() != nil {
			return r, err
		}
		// 模型调用失败（含预算拒绝）与输出不合法同样处理：转人工，不中止整批工单。
		v, reason := ParseVerdict("")
		if err == nil {
			v, reason = ParseVerdict(fmt.Sprint(out.Result))
		} else {
			reason = "模型调用失败：" + livemodel.Redact(err.Error())
		}
		r.Verdict, r.ModelError = &v, reason
		if v.Decision == "reject" {
			r.Status = "rejected"
		}
		return r, nil
	}
	// 自动执行与审批后执行共用：重新读取工单比对哈希，不一致则作废，不执行变更。
	// 审批路径已由批准前置校验核对过，这里只拦得住批准之后到执行之间的改动。
	apply := func(ctx context.Context, r Review) (Review, error) {
		current, err := loadTicket(ctx, store, r.Ticket.ID)
		switch {
		case err != nil:
		case Hash(current) != r.Hash:
			r.Status = "invalidated"
		default:
			if r.Receipt, err = applyChange(ctx, store, r, applies); err == nil {
				r.Status = "applied"
			}
		}
		return r, err
	}
	// 两处条件路由共用：只有规则 pass、test 环境、approve+low 才自动执行。
	router := func(_ context.Context, r Review) (string, error) {
		switch {
		case r.Status == "rejected":
			return workflow.END, nil
		case r.Verdict == nil:
			return "review", nil
		case r.Ticket.Env == "test" && r.RuleVerdict == "pass" && r.Verdict.Decision == "approve" && r.Verdict.RiskLevel == "low":
			return "apply", nil
		}
		return "apply_approved", nil
	}
	g := workflow.NewGraph[Review]()
	if err := errors.Join(
		g.AddNode("gate", gate), g.AddNode("review", review), g.AddNode("apply", apply), g.AddNode("apply_approved", apply),
		g.AddEdge(workflow.START, "gate"),
		g.AddBranch("gate", router, "review", workflow.END),
		g.AddBranch("review", router, "apply", "apply_approved", workflow.END),
		g.AddEdge("apply", workflow.END), g.AddEdge("apply_approved", workflow.END),
	); err != nil {
		return nil, err
	}
	// 批准前置校验在运行锁内执行：宿主无需先读快照再批准，两步之间的窗口不存在。
	beforeApprove := workflow.WithBeforeApprove(func(ctx context.Context, _ string, snap *workflow.GraphSnapshot[Review]) error {
		current, err := loadTicket(ctx, store, snap.State.Ticket.ID)
		if err != nil {
			return err
		}
		if Hash(current) != snap.State.Hash {
			return ErrTicketChanged
		}
		return nil
	})
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "sql-review-v1/" + client.Live.Provider + "/" + client.Live.Model, ApprovalNodes: []string{"apply_approved"}}, beforeApprove)
}

// ErrTicketChanged 表示工单在审批暂停期间被修改：本次批准被拒，运行保持等待；
// 工单改回审核时的内容后可再次批准，或再次 Run 以新内容开启新一轮审核。
var ErrTicketChanged = errors.New("工单在审批暂停期间被修改，拒绝批准")

func ticketKey(id string) string { return "demo-tickets/" + id }

// loadTicket 从模拟工单系统读取当前内容；没有改动记录时就是合成数据。
func loadTicket(ctx context.Context, store durable.Store, id string) (Ticket, error) {
	var t Ticket
	snap, err := store.Load(ctx, ticketKey(id))
	if seed, ok := Tickets[id]; ok && errors.Is(err, os.ErrNotExist) {
		return seed, nil
	} else if err != nil {
		return t, fmt.Errorf("读取工单 %s 失败：%w", id, err)
	}
	return t, json.Unmarshal(snap.Data, &t)
}

func changeKey(id, hash string) string { return "demo-db/" + id + "/" + hash }

// applyChange 模拟执行变更：按工单 id+整张工单哈希去重，崩溃重试也只执行一次，返回原回执。
func applyChange(ctx context.Context, store durable.Store, r Review, applies *atomic.Int64) (string, error) {
	key := changeKey(r.Ticket.ID, r.Hash)
	unlock, err := store.Lock(ctx, key)
	if err != nil {
		return "", err
	}
	defer func() { _ = unlock() }()
	var receipt string
	if snap, err := store.Load(ctx, key); err == nil {
		return receipt, json.Unmarshal(snap.Data, &receipt)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	receipt = "APPLY-" + r.Ticket.ID + "-" + r.Hash[:8]
	data, _ := json.Marshal(receipt)
	if err := store.Save(ctx, key, &durable.Snapshot{RunID: key, Version: "demo-db-v1", Data: data}); err != nil {
		return "", err
	}
	applies.Add(1)
	return receipt, nil
}

// Outcome 是一张工单本次推进后的状态。Status：rejected、applied、waiting、invalidated。
type Outcome struct {
	Review
	RunID, ApprovalKey string
}

// Result 汇总一次推进的工单结果与计量。
type Result struct {
	Outcomes      []Outcome
	NewModelCalls int64
	NewApplies    int64
	Budget        budget.Snapshot
}

// advanceFunc 推进一个持久运行；审批暂停视为正常结果。
type advanceFunc func(runID string, initial Review, approvalKey string) (Outcome, error)

// withSession 打开持久化目录执行 fn，并汇总本次新增的模型请求、执行次数与预算快照。
func withSession(ctx context.Context, client *livemodel.Client, dir string, fn func(durable.Store, advanceFunc) ([]Outcome, error)) (out Result, err error) {
	store, err := durable.NewFileStore(dir)
	if err != nil {
		return out, err
	}
	// 每张通过规则的工单复核一次，另为编辑后的重审留出同样多的额度。
	controller, err := budget.NewDurable(budget.Config{RootKey: "sql-review/batch", Version: "v1/" + client.Live.Provider + "/" + client.Live.Model, MaxModelRequests: 2 * len(Tickets)}, store)
	if err != nil {
		return out, err
	}
	var applies atomic.Int64
	graph, err := build(ctx, client, store, &applies)
	if err != nil {
		return out, err
	}
	advance := func(runID string, initial Review, approvalKey string) (Outcome, error) {
		snap, err := graph.Run(budget.WithController(ctx, controller), runID, initial, approvalKey)
		o := Outcome{RunID: runID}
		if snap != nil {
			o.Review = snap.State
			// 批准被拒（审批键不符、工单被改）时运行仍在等待，同样报告 waiting 与审批键。
			if snap.Waiting {
				o.Status, o.ApprovalKey = "waiting", snap.Key
			}
		}
		switch {
		case errors.Is(err, durable.ErrApprovalRequired):
			return o, nil
		case err == nil && (snap == nil || !snap.Done):
			return o, errors.New("流程没有结束")
		}
		return o, err
	}
	before := client.Calls()
	out.Outcomes, err = fn(store, advance)
	out.NewModelCalls, out.NewApplies = client.Calls()-before, applies.Load()
	out.Budget, _ = controller.Snapshot(ctx)
	return out, err
}

// Run 按工单号顺序审核全部工单。运行 ID 绑定整张工单的哈希：同目录再次运行只回放，
// 工单被修改后再次运行会以新内容开启一轮重审。
func Run(ctx context.Context, client *livemodel.Client, dir string) (Result, error) {
	return withSession(ctx, client, dir, func(store durable.Store, advance advanceFunc) (outs []Outcome, err error) {
		for _, id := range slices.Sorted(maps.Keys(Tickets)) {
			t, err := loadTicket(ctx, store, id)
			if err != nil {
				return outs, err
			}
			hash := Hash(t)
			o, err := advance("sql/"+id+"@"+hash[:12], Review{Ticket: t, Hash: hash}, "")
			if outs = append(outs, o); err != nil {
				return outs, err
			}
		}
		return outs, nil
	})
}

// Approve 批准 runID 当前等待的节点。宿主须先完成审批人认证与授权，审批键不是凭证。
func Approve(ctx context.Context, client *livemodel.Client, dir, runID, approvalKey string) (Result, error) {
	return withSession(ctx, client, dir, func(_ durable.Store, advance advanceFunc) ([]Outcome, error) {
		o, err := advance(runID, Review{}, approvalKey)
		return []Outcome{o}, err
	})
}

func main() {
	dir := flag.String("dir", "", "持久化目录（含模拟工单系统与数据库）；同一目录再次运行只回放")
	run := flag.String("approve-run", "", "要批准的运行 ID（上次输出的 RunID）")
	key := flag.String("approve-key", "", "要批准的审批键（上次输出的 ApprovalKey）")
	flag.Parse()
	if *dir == "" || (*run == "") != (*key == "") {
		fmt.Fprintln(os.Stderr, "用法：sql-review -dir <目录> [-approve-run <RunID> -approve-key <ApprovalKey>]")
		os.Exit(2)
	}
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var out Result
	if *run != "" {
		out, err = Approve(context.Background(), client, *dir, *run, *key)
	} else {
		out, err = Run(context.Background(), client, *dir)
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
}
