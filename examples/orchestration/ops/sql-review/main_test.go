package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// scripted 按工单给出预先写好的复核结果，并记录模型实际看到了哪些工单。
type scripted struct {
	mu   sync.Mutex
	seen []string
	// onReview 在模型复核某张工单时调用，用于模拟复核与执行之间工单被修改。
	onReview func(id string)
}

var replies = map[string]string{
	"T-101": `{"decision":"approve","risk_level":"low","summary":"单行插入，可回滚","concerns":[]}`,
	"T-106": "```json\n" + `{"decision":"escalate","risk_level":"high","summary":"删除 240 万行","concerns":["需分批"]}` + "\n```",
	"T-107": `{"decision":"approve","risk_level":"low","summary":"按主键更新一行","concerns":["生产环境"]}`,
	"T-108": "好的，这张工单风险很低，可以通过。",
}

const rejectReply = `{"decision":"reject","risk_level":"high","summary":"越权修改角色","concerns":["提权"]}`

func (s *scripted) client() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: s.respond}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

func (s *scripted) respond(r *llm.CompletionRequest) llm.StreamChunk {
	input := r.Messages[len(r.Messages)-1].Content
	for id, text := range replies {
		if strings.Contains(input, `"id":"`+id+`"`) {
			if strings.Contains(input, "admin") {
				text = rejectReply
			}
			s.mu.Lock()
			s.seen = append(s.seen, id)
			s.mu.Unlock()
			if s.onReview != nil {
				s.onReview(id)
			}
			return llm.StreamChunk{Content: text, Delta: text}
		}
	}
	panic("脚本没有覆盖的工单：" + input)
}

// expected 是各工单按规则应得的去向：rejected、auto、approval。
var expected = map[string]string{"T-101": "auto", "T-102": "rejected", "T-103": "rejected", "T-104": "rejected", "T-105": "rejected", "T-106": "approval", "T-107": "approval", "T-108": "auto"}

// wantStatus 是规则去向与模型结论共同决定的唯一状态。模型只能降级：reject 结束，
// 只有规则去向为 auto 且 approve+low 才自动执行，其余（含非法输出、调用失败）都等待审批。
func wantStatus(id string, v *Verdict) string {
	switch {
	case expected[id] == "rejected", v != nil && v.Decision == "reject":
		return "rejected"
	case expected[id] == "auto" && v != nil && v.Decision == "approve" && v.RiskLevel == "low":
		return "applied"
	}
	return "waiting"
}

// editTicket 模拟提交人在模拟工单系统里修改工单。
func editTicket(t *testing.T, dir, id string, edit func(*Ticket)) Ticket {
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	ticket, err := loadTicket(context.Background(), store, id)
	require.NoError(t, err)
	edit(&ticket)
	data, err := json.Marshal(ticket)
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), ticketKey(id), &durable.Snapshot{RunID: ticketKey(id), Version: "demo-tickets-v1", Data: data}))
	return ticket
}

// applied 读取模拟数据库的执行记录，确认某个版本的工单是否执行过。
func applied(t *testing.T, dir, id, hash string) bool {
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	_, err = store.Load(context.Background(), changeKey(id, hash))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	require.NoError(t, err)
	return true
}

func byID(r Result) map[string]Outcome {
	m := map[string]Outcome{}
	for _, o := range r.Outcomes {
		m[o.Ticket.ID] = o
	}
	return m
}

func TestCheckRules(t *testing.T) {
	want := map[string]string{"T-101": "pass", "T-102": "reject", "T-103": "reject", "T-104": "reject", "T-105": "reject", "T-106": "review", "T-107": "pass", "T-108": "pass"}
	for id, v := range want {
		checks, verdict := Check(Tickets[id])
		require.Equal(t, v, verdict, id)
		require.Len(t, checks, len(Tickets[id].Statements), id)
	}
	checks, _ := Check(Tickets["T-104"])
	for _, c := range checks {
		require.Contains(t, c.Rules, "R2-同一工单不得混合 DDL 与 DML")
	}
	checks, _ = Check(Tickets["T-105"])
	require.Equal(t, []string{"R3-UPDATE/DELETE 必须带 WHERE"}, checks[0].Rules)
	checks, _ = Check(Ticket{Statements: []string{"DELETE FROM demo_events WHERE 1 = 1"}})
	require.Equal(t, []string{"R3-WHERE 条件恒真或未引用列"}, checks[0].Rules)
	checks, _ = Check(Ticket{Statements: []string{"UPDATE demo_orders SET status = (SELECT s FROM demo_cfg WHERE id = 1)"}})
	require.Equal(t, []string{"R3-UPDATE/DELETE 必须带 WHERE"}, checks[0].Rules)
	// 边界：恰好 MaxRows 不警告；白名单外的语句与空语句一律拦截。
	_, v := Check(Ticket{Statements: []string{"DELETE FROM t WHERE id > 0"}, EstRows: MaxRows})
	require.Equal(t, "pass", v)
	for _, sql := range []string{
		"GRANT ALL ON *.* TO demo", "ALTER TABLE t DROP COLUMN c", "  ",
		// 多语句拼接、分号（含末尾分号）、注释、反斜杠与未闭合引号。
		"INSERT INTO demo_a VALUES (1); DROP TABLE demo_users",
		"INSERT INTO demo_a VALUES (1);",
		"DELETE FROM demo_orders -- WHERE id = 1",
		"DELETE FROM demo_orders # WHERE id = 1",
		"DELETE FROM demo_orders /* WHERE id = 1 */",
		`UPDATE demo_orders SET note = 'x\' WHERE id = 1`,
		"UPDATE demo_orders SET note = 'x WHERE id = 1",
		// ALTER 只允许单一 ADD 子句。
		"ALTER TABLE demo_users ADD COLUMN c INT, DROP COLUMN email",
		"ALTER TABLE demo_users ADD COLUMN c INT, ADD COLUMN d INT",
		"ALTER TABLE demo_users DROP COLUMN email",
		// WHERE 只在字符串字面量之外识别。
		"UPDATE demo_orders SET note = 'x WHERE y'",
		`UPDATE demo_orders SET note = "x WHERE y"`,
		"DELETE FROM demo_orders WHERE_ok = 1",
		// WHERE 只认外层语句：子查询里的 WHERE 不算，括号不配对也不算。
		"UPDATE demo_orders SET status = (SELECT s FROM demo_cfg WHERE id = 1)",
		"UPDATE demo_orders SET status = 1) WHERE (id = 1",
		"DELETE FROM demo_events WHERE (id = 1",
		// 恒真或不引用列的条件。
		"DELETE FROM demo_events WHERE 1 = 1",
		"UPDATE demo_orders SET status = 'closed' WHERE 1",
		"UPDATE demo_orders SET status = 'closed' WHERE TRUE",
		"DELETE FROM demo_events WHERE 'a' = 'a'",
		"DELETE FROM demo_events WHERE (1=1)",
		"DELETE FROM demo_events WHERE id = id",
		"DELETE FROM demo_events WHERE id = 7 OR 1 = 1",
		"DELETE FROM demo_events WHERE NOW() > 0",
		"DELETE FROM demo_events WHERE 1E5 > 0",
	} {
		checks, v := Check(Ticket{Statements: []string{sql}})
		require.Equal(t, "reject", v, sql)
		require.Equal(t, "block", checks[0].Level, sql)
	}
	checks, v = Check(Ticket{ID: "T-0", Env: "test"})
	require.Equal(t, "reject", v, "没有语句")
	require.Equal(t, []string{"R4-工单没有语句"}, checks[0].Rules)
	// 字面量里的关键字与 '' 转义不影响判定。
	for _, sql := range []string{
		"UPDATE demo_orders SET note = 'it''s DROP -x, ok' WHERE id = 1",
		"ALTER TABLE demo_users ADD COLUMN c VARCHAR(8) DEFAULT 'a b'",
		// 子查询出现在 SET 中、外层另有 WHERE；括号包裹的条件、函数与列比较、LIMIT 子句都正常放行。
		"UPDATE demo_orders SET status = (SELECT s FROM demo_cfg WHERE id = 1) WHERE id = 7",
		"DELETE FROM demo_events WHERE (id = 1)",
		"DELETE FROM demo_events WHERE created_at < NOW() LIMIT 100",
		"UPDATE demo_orders SET status = 'closed' WHERE status = 'open' OR id IN (1, 2)",
	} {
		_, v := Check(Ticket{Statements: []string{sql}})
		require.Equal(t, "pass", v, sql)
	}
}

func TestParseVerdict(t *testing.T) {
	v, reason := ParseVerdict(replies["T-106"])
	require.Empty(t, reason, "允许包在一个代码块里")
	require.Equal(t, [2]string{"escalate", "high"}, [2]string{v.Decision, v.RiskLevel})
	for name, text := range map[string]string{
		"非 JSON":      replies["T-108"],
		"未知字段":        `{"decision":"approve","risk_level":"low","summary":"x","concerns":[],"auto_apply":true}`,
		"多余内容":        `{"decision":"approve","risk_level":"low","summary":"x","concerns":[]} 以上`,
		"非法 decision": `{"decision":"pass","risk_level":"low","summary":"x","concerns":[]}`,
		"非法风险级别":      `{"decision":"approve","risk_level":"none","summary":"x","concerns":[]}`,
		"summary 为空":  `{"decision":"approve","risk_level":"low","summary":" ","concerns":[]}`,
		"null":        `null`,
		"字段名大小写不符":    `{"Decision":"approve","risk_level":"low","summary":"x","concerns":[]}`,
		"重复键":         `{"decision":"reject","decision":"approve","risk_level":"low","summary":"x","concerns":[]}`,
	} {
		v, reason := ParseVerdict(text)
		require.NotEmpty(t, reason, name)
		require.Equal(t, [2]string{"escalate", "high"}, [2]string{v.Decision, v.RiskLevel}, name)
	}
}

func TestApplyChangeIsIdempotent(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	var applies atomic.Int64
	r := Review{Ticket: Tickets["T-101"], Hash: Hash(Tickets["T-101"])}
	first, err := applyChange(context.Background(), store, r, &applies)
	require.NoError(t, err)
	again, err := applyChange(context.Background(), store, r, &applies)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, int64(1), applies.Load())
}

// 规则拒绝的工单不调用模型；只有 approve+low 的 test 工单自动执行；非法 JSON 收敛为转人工；回放 0 请求。
func TestScriptedBatch(t *testing.T) {
	s := &scripted{}
	client, dir := s.client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	got := byID(first)
	require.ElementsMatch(t, []string{"T-101", "T-106", "T-107", "T-108"}, s.seen, "规则拒绝的工单不调用模型")
	for _, id := range []string{"T-102", "T-103", "T-104", "T-105"} {
		require.Equal(t, "rejected", got[id].Status, id)
		require.Nil(t, got[id].Verdict, id)
	}
	require.Equal(t, "applied", got["T-101"].Status)
	for _, id := range []string{"T-106", "T-107", "T-108"} {
		require.Equal(t, "waiting", got[id].Status, id)
		require.NotEmpty(t, got[id].ApprovalKey, id)
		require.False(t, applied(t, dir, id, got[id].Hash), "审批前不得执行 %s", id)
	}
	for id, o := range got {
		require.Equal(t, wantStatus(id, o.Verdict), o.Status, id)
	}
	require.Equal(t, "escalate", got["T-108"].Verdict.Decision, "非法输出收敛为转人工")
	require.Equal(t, "high", got["T-108"].Verdict.RiskLevel)
	require.Contains(t, got["T-108"].ModelError, "输出必须是一个 JSON 对象")
	require.Equal(t, int64(1), first.NewApplies)
	require.Equal(t, int64(4), first.NewModelCalls)
	require.Equal(t, 4, first.Budget.ReservedRequests)

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls+again.NewApplies)
	require.Equal(t, first.Outcomes, again.Outcomes)
	require.Equal(t, first.Budget, again.Budget)
}

// prod 工单暂停 → 批准 → 执行一次；错误审批键与重复批准都被拒绝。
func TestScriptedApproveApplies(t *testing.T) {
	client, dir := (&scripted{}).client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	waiting := byID(first)["T-107"]

	_, err = Approve(context.Background(), client, dir, waiting.RunID, "not-the-key")
	require.ErrorContains(t, err, "approval does not match")

	approved, err := Approve(context.Background(), client, dir, waiting.RunID, waiting.ApprovalKey)
	require.NoError(t, err)
	require.Equal(t, "applied", approved.Outcomes[0].Status)
	require.Equal(t, int64(1), approved.NewApplies)
	require.Zero(t, approved.NewModelCalls, "审批后不重新复核")
	require.True(t, applied(t, dir, "T-107", waiting.Hash))

	_, err = Approve(context.Background(), client, dir, waiting.RunID, waiting.ApprovalKey)
	require.ErrorContains(t, err, "approval does not match", "已结束的运行不能再次批准")
	replay, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Equal(t, "applied", byID(replay)["T-107"].Status)
	require.Zero(t, replay.NewModelCalls+replay.NewApplies)
}

// 暂停期间工单被修改：批准前置校验在运行锁内拒绝，运行保持等待、不执行；再次运行以新内容重审，模型拒绝。
func TestScriptedEditBlocksApproval(t *testing.T) {
	client, dir := (&scripted{}).client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	waiting := byID(first)["T-107"]
	edited := editTicket(t, dir, "T-107", func(tk *Ticket) {
		tk.Statements = []string{"UPDATE demo_users SET role = 'admin' WHERE id = 42"}
	})

	for range 2 { // 拒绝不改动检查点，同一审批键再次批准仍被拒。
		approved, err := Approve(context.Background(), client, dir, waiting.RunID, waiting.ApprovalKey)
		require.ErrorIs(t, err, ErrTicketChanged)
		require.ErrorContains(t, err, "批准前置校验拒绝")
		require.Equal(t, "waiting", approved.Outcomes[0].Status)
		require.Equal(t, waiting.ApprovalKey, approved.Outcomes[0].ApprovalKey)
		require.Equal(t, waiting.Hash, approved.Outcomes[0].Hash)
		require.Zero(t, approved.NewApplies+approved.NewModelCalls)
	}
	for _, hash := range []string{waiting.Hash, Hash(edited)} {
		require.False(t, applied(t, dir, "T-107", hash), "被拒的批准不执行任何版本")
	}

	rereview, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	got := byID(rereview)["T-107"]
	require.NotEqual(t, waiting.RunID, got.RunID, "新内容开启新一轮")
	require.Equal(t, Hash(edited), got.Hash)
	require.Equal(t, "rejected", got.Status)
	require.Equal(t, int64(1), rereview.NewModelCalls, "只有被修改的工单重审")
	require.Equal(t, 5, rereview.Budget.ReservedRequests)
}

// 工单改回审核时的内容后，原运行可用原审批键批准并执行一次。
func TestScriptedRevertedEditApproves(t *testing.T) {
	client, dir := (&scripted{}).client(), t.TempDir()
	first, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	waiting := byID(first)["T-107"]
	editTicket(t, dir, "T-107", func(tk *Ticket) { tk.EstRows = 2 })
	_, err = Approve(context.Background(), client, dir, waiting.RunID, waiting.ApprovalKey)
	require.ErrorIs(t, err, ErrTicketChanged)

	editTicket(t, dir, "T-107", func(tk *Ticket) { *tk = Tickets["T-107"] })
	approved, err := Approve(context.Background(), client, dir, waiting.RunID, waiting.ApprovalKey)
	require.NoError(t, err)
	require.Equal(t, "applied", approved.Outcomes[0].Status)
	require.Equal(t, int64(1), approved.NewApplies)
	require.Zero(t, approved.NewModelCalls)
	require.True(t, applied(t, dir, "T-107", waiting.Hash))
}

// 哈希覆盖整张工单：暂停期间只改 EstRows 或 Env，语句不变也拒绝批准；再次运行按新内容重审。
func TestScriptedNonStatementEditBlocksApproval(t *testing.T) {
	for name, tc := range map[string]struct {
		edit   func(*Ticket)
		status string
	}{
		"est_rows": {func(tk *Ticket) { tk.EstRows = 5 * MaxRows }, "waiting"}, // 规则警告，仍须审批
		"env":      {func(tk *Ticket) { tk.Env = "test" }, "applied"},          // test 环境 approve+low 自动执行
	} {
		t.Run(name, func(t *testing.T) {
			client, dir := (&scripted{}).client(), t.TempDir()
			first, err := Run(context.Background(), client, dir)
			require.NoError(t, err)
			waiting := byID(first)["T-107"]
			edited := editTicket(t, dir, "T-107", tc.edit)
			require.Equal(t, Tickets["T-107"].Statements, edited.Statements)
			require.NotEqual(t, waiting.Hash, Hash(edited))

			approved, err := Approve(context.Background(), client, dir, waiting.RunID, waiting.ApprovalKey)
			require.ErrorIs(t, err, ErrTicketChanged)
			require.Equal(t, "waiting", approved.Outcomes[0].Status)
			require.Zero(t, approved.NewApplies+approved.NewModelCalls)
			require.False(t, applied(t, dir, "T-107", waiting.Hash))

			rereview, err := Run(context.Background(), client, dir)
			require.NoError(t, err)
			got := byID(rereview)["T-107"]
			require.NotEqual(t, waiting.RunID, got.RunID)
			require.Equal(t, edited, got.Ticket)
			require.Equal(t, tc.status, got.Status)
			require.Equal(t, int64(1), rereview.NewModelCalls, "只有被修改的工单重审")
			require.Equal(t, tc.status == "applied", applied(t, dir, "T-107", Hash(edited)))
		})
	}
}

// 自动执行路径不经过审批：复核之后、执行之前工单被修改，执行节点重读比对后作废，不执行。
func TestScriptedEditBeforeAutoApplyInvalidates(t *testing.T) {
	dir := t.TempDir()
	s := &scripted{onReview: func(id string) {
		if id == "T-101" {
			editTicket(t, dir, id, func(tk *Ticket) { tk.EstRows = 3 })
		}
	}}
	out, err := Run(context.Background(), s.client(), dir)
	require.NoError(t, err)
	got := byID(out)["T-101"]
	require.Equal(t, "invalidated", got.Status)
	require.Equal(t, Hash(Tickets["T-101"]), got.Hash)
	require.False(t, applied(t, dir, "T-101", got.Hash))
	require.Zero(t, out.NewApplies)
}

// 真实模型：状态必须等于规则去向与模型结论推出的唯一状态（模型降级不算失败）；
// 规则拒绝的不调模型，只有 test 环境 approve+low 才自动执行；T-107 未被模型拒绝时必须等待，批准后执行一次。
func TestLiveSQLReview(t *testing.T) {
	client := livemodel.Require(t)
	dir := t.TempDir()
	first, err := Run(context.Background(), client, dir)
	t.Logf("%s/%s：%+v", client.Live.Provider, client.Live.Model, first)
	require.NoError(t, err)
	reviewed := 0
	for _, o := range first.Outcomes {
		id := o.Ticket.ID
		if expected[id] == "rejected" {
			require.Nil(t, o.Verdict, "规则拒绝的工单 %s 不调用模型", id)
		} else {
			reviewed++
			require.NotNil(t, o.Verdict, id)
		}
		require.Equal(t, wantStatus(id, o.Verdict), o.Status, id)
		require.Equal(t, o.Status == "applied", applied(t, dir, id, o.Hash), "只有 applied 的工单执行过：%s", id)
		if expected[id] == "auto" && o.Status != "applied" {
			t.Logf("%s 被模型降级：%s/%s %s", id, o.Verdict.Decision, o.Verdict.RiskLevel, o.ModelError)
		}
	}
	require.Equal(t, int64(reviewed), first.NewModelCalls)
	require.Equal(t, int(first.NewModelCalls), first.Budget.ReservedRequests)

	if prod := byID(first)["T-107"]; prod.Status == "waiting" {
		approved, err := Approve(context.Background(), client, dir, prod.RunID, prod.ApprovalKey)
		require.NoError(t, err)
		require.Equal(t, "applied", approved.Outcomes[0].Status)
		require.Equal(t, int64(1), approved.NewApplies)
		require.Zero(t, approved.NewModelCalls)
		require.True(t, applied(t, dir, "T-107", prod.Hash))
	} else {
		t.Logf("T-107 被模型拒绝，跳过批准流程：%+v", prod.Verdict)
	}

	again, err := Run(context.Background(), client, dir)
	require.NoError(t, err)
	require.Zero(t, again.NewModelCalls+again.NewApplies)
	require.Equal(t, first.Budget, again.Budget)
}

// failing 让含 T-107 的复核请求在传输层失败，其余照常由脚本回复。
type failing struct{ *demomodel.Model }

func (f failing) CompleteStream(ctx context.Context, r *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	if strings.Contains(r.Messages[len(r.Messages)-1].Content, `"id":"T-107"`) {
		return nil, errors.New("upstream 503")
	}
	return f.Model.CompleteStream(ctx, r)
}

func (f failing) CompleteToolsStream(ctx context.Context, r *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return f.CompleteStream(ctx, r)
}

// 模型调用失败与输出不合法同样转人工，不中止整批工单，也不自动执行。
func TestScriptedModelFailureEscalates(t *testing.T) {
	s := &scripted{}
	var calls atomic.Int64
	client := livemodel.Wrap(failing{&demomodel.Model{Calls: &calls, Respond: s.respond}}, livemodel.Config{Provider: "scripted", Model: "v1"})
	out, err := Run(context.Background(), client, t.TempDir())
	require.NoError(t, err)
	got := byID(out)["T-107"]
	require.Equal(t, "waiting", got.Status)
	require.Equal(t, "escalate", got.Verdict.Decision)
	require.Contains(t, got.ModelError, "模型调用失败：upstream 503")
	require.Equal(t, "applied", byID(out)["T-101"].Status, "其他工单照常处理")
}
