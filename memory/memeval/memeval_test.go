package memeval

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// fakeModel 按 reply 应答，供作答、判定与记忆抽取使用。
type fakeModel struct {
	reply func(system, user string) string
}

func (m fakeModel) Provider() constants.Provider { return constants.Provider("fake") }
func (m fakeModel) CompleteStream(_ context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	system, user := "", ""
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			system = msg.Content
		} else {
			user = msg.Content
		}
	}
	text := m.reply(system, user)
	ch := make(chan *llm.StreamChunk, 2)
	ch <- &llm.StreamChunk{Delta: text, Content: text}
	ch <- &llm.StreamChunk{Done: true, Content: text, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

const longMemEvalFixture = `[
 {"question_id":"q1","question_type":"knowledge-update","question":"Where do I live now?","answer":"Beijing","question_date":"2023/06/01 (Thu) 10:00",
  "haystack_session_ids":["s2","s1"],"haystack_dates":["2023/05/20 (Sat) 02:21","2023/01/02 (Mon) 09:00"],
  "haystack_sessions":[[{"role":"user","content":"I just moved to Beijing.","has_answer":true},{"role":"assistant","content":"Congrats!"}],
                       [{"role":"user","content":"I live in Shanghai."},{"role":"assistant","content":"Nice city."}]],
  "answer_session_ids":["s2"]},
 {"question_id":"q2_abs","question_type":"single-session-user","question":"What is my dog's name?","answer":"You never mentioned a dog.","question_date":"2023/06/01 (Thu) 10:00",
  "haystack_session_ids":["s3"],"haystack_dates":["2023/05/20 (Sat) 02:21"],
  "haystack_sessions":[[{"role":"user","content":"I like tea."},{"role":"assistant","content":"OK."}]],"answer_session_ids":[]}
]`

const locomoFixture = `[{"sample_id":"conv-1","conversation":{"speaker_a":"Caroline","speaker_b":"Melanie",
  "session_1_date_time":"1:56 pm on 8 May, 2023","session_1":[{"speaker":"Caroline","dia_id":"D1:1","text":"I went to a support group yesterday."},{"speaker":"Melanie","dia_id":"D1:2","text":"Great!"}],
  "session_2_date_time":"10:37 am on 27 June, 2023","session_2":[{"speaker":"Melanie","dia_id":"D2:1","text":"I painted a sunrise in 2022."}]},
  "qa":[{"question":"When did Melanie paint a sunrise?","answer":2022,"evidence":["D2:1"],"category":2},
        {"question":"What did Caroline realize after her race?","evidence":["D1:1","D1:2"],"category":5,"adversarial_answer":"self-care"}]}]`

func TestLoadLongMemEval(t *testing.T) {
	cases, err := LoadLongMemEval(strings.NewReader(longMemEvalFixture))
	require.NoError(t, err)
	require.Len(t, cases, 2)
	c := cases[0]
	require.Equal(t, []string{"s1", "s2"}, []string{c.Sessions[0].ID, c.Sessions[1].ID}, "会话按日期排序")
	require.Equal(t, time.Date(2023, 5, 20, 2, 21, 0, 0, time.UTC), c.Sessions[1].Date)
	require.Equal(t, "user", c.Sessions[1].Turns[0].Role)
	q := c.Questions[0]
	require.Equal(t, "knowledge-update", q.Type)
	require.Equal(t, []string{"s2"}, q.Evidence)
	require.False(t, q.Abstain)
	require.True(t, cases[1].Questions[0].Abstain, "ID 含 _abs 的是弃答题")

	_, err = LoadLongMemEval(strings.NewReader(`[{"question_id":"x","haystack_session_ids":["a"],"haystack_dates":[],"haystack_sessions":[[]]}]`))
	require.Error(t, err)
}

func TestLoadLoCoMo(t *testing.T) {
	cases, err := LoadLoCoMo(strings.NewReader(locomoFixture))
	require.NoError(t, err)
	require.Len(t, cases, 1)
	c := cases[0]
	require.Len(t, c.Sessions, 2)
	require.Equal(t, "user", c.Sessions[0].Turns[0].Role)
	require.Equal(t, "Caroline: I went to a support group yesterday.", c.Sessions[0].Turns[0].Content)
	require.Equal(t, "assistant", c.Sessions[1].Turns[0].Role)
	require.Equal(t, time.Date(2023, 6, 27, 10, 37, 0, 0, time.UTC), c.Sessions[1].Date)
	q := c.Questions[0]
	require.Equal(t, "2022", q.Answer, "数字答案转为文本")
	require.Equal(t, "temporal", q.Type)
	require.Equal(t, []string{"session_2"}, q.Evidence)
	require.Equal(t, c.Sessions[1].Date, q.Date, "提问时间取最后一个会话的时间")
	adv := c.Questions[1]
	require.True(t, adv.Abstain)
	require.Equal(t, []string{"session_1"}, adv.Evidence, "同一会话的证据只记一次")
}

func TestAnswerF1(t *testing.T) {
	tests := []struct {
		name string
		q    Question
		hyp  string
		want float64
	}{
		{"LongMemEval 不计 F1", Question{Answer: "Beijing"}, "Beijing", 0},
		{"单跳完全一致", Question{Category: 4, Answer: "The red car"}, "red car.", 1},
		{"部分重合", Question{Category: 2, Answer: "7 May 2023"}, "May 2023", 0.8},
		{"多跳按逗号拆分", Question{Category: 1, Answer: "Paris, Rome"}, "Rome", 0.5},
		{"开放域取分号前", Question{Category: 3, Answer: "psychology; counseling"}, "psychology", 1},
		{"对抗题正确弃答", Question{Category: 5}, "Not mentioned in the conversation", 1},
		{"对抗题给出答案", Question{Category: 5}, "self-care", 0},
		{"没有重合", Question{Category: 4, Answer: "tea"}, "coffee", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, AnswerF1(tt.q, tt.hyp), 1e-9)
		})
	}
}

func TestJudgePromptByType(t *testing.T) {
	require.Contains(t, JudgePrompt(Question{Abstain: true, Type: "temporal-reasoning"}, "x"), "unanswerable", "弃答优先")
	require.Contains(t, JudgePrompt(Question{Type: "temporal-reasoning"}, "x"), "off-by-one")
	require.Contains(t, JudgePrompt(Question{Type: "knowledge-update"}, "x"), "updated answer")
	require.Contains(t, JudgePrompt(Question{Type: "single-session-preference"}, "x"), "Rubric")
	require.Contains(t, JudgePrompt(Question{Type: "multi-session"}, "x"), "Correct Answer")
}

// 端到端：基线只看得到预算内最近的原文，长期记忆方案从抽取出的记忆作答，结果按方案与题型汇总。
func TestRunnerEndToEnd(t *testing.T) {
	cases, err := LoadLongMemEval(strings.NewReader(longMemEvalFixture))
	require.NoError(t, err)
	extractor := fakeModel{reply: func(system, user string) string {
		if !strings.Contains(system, "抽取") {
			return `{"op":"invalidate","target":""}`
		}
		switch {
		case strings.Contains(user, "Beijing"):
			return `{"memories":[{"kind":"fact","content":"User lives in Beijing","importance":0.9}]}`
		case strings.Contains(user, "Shanghai"):
			return `{"memories":[{"kind":"fact","content":"User lives in Shanghai","importance":0.9}]}`
		}
		return `{"memories":[]}`
	}}
	answerer := fakeModel{reply: func(_, user string) string {
		ctx, _, _ := strings.Cut(user, "当前日期")
		if strings.Contains(ctx, "Beijing") {
			return "Beijing"
		}
		return "not mentioned"
	}}
	judge := fakeModel{reply: func(_, user string) string {
		if strings.Contains(user, "unanswerable") {
			if strings.Contains(user, "Model Response: not mentioned") {
				return "yes"
			}
			return "no"
		}
		if strings.Contains(user, "Model Response: Beijing") {
			return "Yes."
		}
		return "no"
	}}
	mem := &Memory{Label: "memory", Dir: t.TempDir(), Configure: func(sessions session.Store) memory.Config {
		return memory.Config{Sessions: sessions, Model: extractor}
	}}
	var progress []string
	results, err := Runner{Systems: []System{Recent{Runes: 20}, mem}, Answerer: answerer, Judge: judge,
		Progress: func(r Result) { progress = append(progress, r.System+"/"+r.QuestionID) }}.Run(context.Background(), cases)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"recent/q1", "recent/q2_abs", "memory/q1", "memory/q2_abs"}, progress)
	var order []string
	for _, r := range results {
		order = append(order, r.System+"/"+r.QuestionID)
	}
	require.Equal(t, []string{"recent/q1", "recent/q2_abs", "memory/q1", "memory/q2_abs"}, order, "结果按方案、对话与题目排序")

	byKey := map[string]Result{}
	for _, r := range results {
		byKey[r.System+"/"+r.QuestionID] = r
	}
	require.LessOrEqual(t, byKey["recent/q1"].ContextRunes, 20, "基线不超过预算")
	require.False(t, byKey["recent/q1"].Correct, "预算内看不到答案")
	require.True(t, byKey["memory/q1"].Correct)
	require.True(t, byKey["memory/q1"].EvidenceHit, "召回的记忆来自证据会话")
	require.Contains(t, byKey["memory/q1"].Retrieved, "s2")
	require.True(t, byKey["memory/q2_abs"].Correct, "没有相关记忆时弃答")

	rows := Summarize(results)
	table := Report(results)
	require.Contains(t, table, "| memory | all | 2 | 100.0% |")
	require.Contains(t, table, "| recent | single-session-user（弃答） | 1 |")
	require.Contains(t, table, "| recent | memory | 0 | 1 |", "两两比较：q1 上 recent 错而 memory 对")
	require.Equal(t, "all", rows[len(rows)-1].Type, "每个方案的汇总行在最后")
}

// 抽取偶发失败时按任务的重试次数重试，而不是让整个评测失败；重试用尽才报错。每次尝试含一次 JSON 修正，共两次调用。
func TestMemoryIngestRetriesFailedJobs(t *testing.T) {
	noBackoff(t)
	cases, err := LoadLongMemEval(strings.NewReader(longMemEvalFixture))
	require.NoError(t, err)
	for _, tt := range []struct {
		name     string
		failures int
		wantErr  bool
	}{{"重试后成功", 2, false}, {"重试用尽", 2 * 3, true}} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			flaky := fakeModel{reply: func(string, string) string {
				calls++
				if calls <= tt.failures {
					return "服务暂时不可用"
				}
				return `{"memories":[]}`
			}}
			sys := &Memory{Label: "memory", Dir: t.TempDir(), Configure: func(sessions session.Store) memory.Config {
				return memory.Config{Sessions: sessions, Model: flaky}
			}}
			_, err := sys.Ingest(context.Background(), cases[1])
			if tt.wantErr {
				require.ErrorContains(t, err, "抽取失败")
				return
			}
			require.NoError(t, err)
		})
	}
}

// errModel 前 failures 次返回 err，之后返回 "ok"。
type errModel struct {
	err      error
	failures int
	calls    *int
}

func (m errModel) Provider() constants.Provider { return constants.Provider("fake") }
func (m errModel) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	*m.calls++
	if *m.calls <= m.failures {
		return nil, m.err
	}
	return fakeModel{reply: func(string, string) string { return "ok" }}.CompleteStream(ctx, req)
}

// 提供方的临时错误重试至多 chatAttempts 次；不可重试的错误立即返回。
func TestChatRetriesTransientErrors(t *testing.T) {
	chatBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() {
		chatBackoff = func(attempt int) time.Duration { return time.Duration(1<<attempt) * time.Second }
	})
	transient := agentErrors.New(agentErrors.CodeLLMRequest, "500")
	tests := []struct {
		name      string
		err       error
		failures  int
		wantErr   bool
		wantCalls int
	}{
		{"临时错误后成功", transient, chatAttempts - 1, false, chatAttempts},
		{"临时错误超过次数", transient, chatAttempts, true, chatAttempts},
		{"不可重试的错误", agentErrors.New(agentErrors.CodeInvalidInput, "bad"), 1, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			resp, err := chat(context.Background(), errModel{err: tt.err, failures: tt.failures, calls: &calls}, []llm.Message{llm.UserMessage("q")})
			require.Equal(t, tt.wantCalls, calls)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "ok", resp.Content)
		})
	}
}

// flakySystem 第一次取上下文时返回临时错误。
type flakySystem struct{ calls int }

func (*flakySystem) Name() string                                      { return "flaky" }
func (s *flakySystem) Ingest(context.Context, Case) (Retriever, error) { return s, nil }
func (s *flakySystem) Context(context.Context, Question) (Retrieved, error) {
	s.calls++
	if s.calls == 1 {
		return Retrieved{}, agentErrors.New(agentErrors.CodeLLMRequest, "嵌入服务 503")
	}
	return Retrieved{Text: "Beijing"}, nil
}

// 取上下文时的临时错误（如嵌入服务 503）重试，不中断整轮评测。
func TestRunnerRetriesContext(t *testing.T) {
	chatBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() {
		chatBackoff = func(attempt int) time.Duration { return time.Duration(1<<attempt) * time.Second }
	})
	cases, err := LoadLongMemEval(strings.NewReader(longMemEvalFixture))
	require.NoError(t, err)
	sys := &flakySystem{}
	echo := fakeModel{reply: func(string, string) string { return "yes" }}
	results, err := Runner{Systems: []System{sys}, Answerer: echo, Judge: echo}.Run(context.Background(), cases[:1])
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, 2, sys.calls)
	require.Equal(t, len("Beijing"), results[0].ContextRunes)
}

func noBackoff(t *testing.T) {
	saved := chatBackoff
	chatBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { chatBackoff = saved })
}

func TestSampleStratifiesByType(t *testing.T) {
	var cases []Case
	for ci := range 3 {
		c := Case{ID: "c" + string(rune('0'+ci))}
		for qi := range 10 {
			typ := "single-hop"
			switch {
			case qi < 2:
				typ = "temporal"
			case qi == 2:
				typ = "adversarial"
			}
			c.Questions = append(c.Questions, Question{ID: c.ID + "/" + string(rune('0'+qi)), Type: typ})
		}
		cases = append(cases, c)
	}
	tests := []struct {
		name  string
		n     int
		types map[string]int
		cases int
	}{
		{"按比例分配", 10, map[string]int{"single-hop": 7, "temporal": 2, "adversarial": 1}, 3},
		{"每个题型至少一道", 3, map[string]int{"single-hop": 1, "temporal": 1, "adversarial": 1}, 1},
		{"不少于总数时原样返回", 30, map[string]int{"single-hop": 21, "temporal": 6, "adversarial": 3}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sample(cases, tt.n)
			counts := map[string]int{}
			for _, c := range got {
				for _, q := range c.Questions {
					counts[q.Type]++
				}
			}
			require.Equal(t, tt.types, counts)
			require.Len(t, got, tt.cases, "题目分散在各个对话中")
		})
	}
}

// 多轮结果以题目为单位平均；各轮范围反映波动；两两比较按同一轮同一道题配对。
func TestSummarizeRepeatsAndCompare(t *testing.T) {
	var results []Result
	add := func(run int, system, id string, correct bool) {
		results = append(results, Result{Run: run, System: system, QuestionID: id, Type: "t", Correct: correct})
	}
	for run := 1; run <= 2; run++ {
		for i := range 10 {
			id := string(rune('a' + i))
			add(run, "a", id, true)
			add(run, "b", id, i < 5 || (run == 1 && i == 5))
		}
	}
	rows := Summarize(results)
	b := rows[len(rows)-1]
	require.Equal(t, "b", b.System)
	require.Equal(t, 10, b.Questions, "两轮同一道题只算一个样本")
	require.InDelta(t, 0.55, b.Accuracy, 1e-9)
	require.InDelta(t, 0.5, b.RunMin, 1e-9)
	require.InDelta(t, 0.6, b.RunMax, 1e-9)
	require.Less(t, b.Low, 0.55)
	require.Greater(t, b.High, 0.55)

	pairs := Compare(results)
	require.Len(t, pairs, 1)
	require.Equal(t, 9, pairs[0].Wins)
	require.Equal(t, 0, pairs[0].Losses)
	require.InDelta(t, 2.0/512, pairs[0].P, 1e-9, "9 胜 0 负的双侧符号检验")
	require.InDelta(t, 1, signTest(3, 3), 1e-9)
	require.InDelta(t, 1, signTest(0, 0), 1e-9)
}

// 并发与重复运行时结果按轮次、方案、对话与题目排序；每轮重新灌入。
func TestRunnerRepeatsConcurrently(t *testing.T) {
	cases, err := LoadLongMemEval(strings.NewReader(longMemEvalFixture))
	require.NoError(t, err)
	model := fakeModel{reply: func(string, string) string { return "no" }}
	results, err := Runner{Systems: []System{Recent{}, Recent{Runes: 10}}, Answerer: model, Judge: model, Repeats: 2, Concurrency: 3}.Run(context.Background(), cases)
	require.NoError(t, err)
	var got []string
	for _, r := range results {
		got = append(got, fmt.Sprintf("%d/%d/%s", r.Run, r.ContextRunes, r.QuestionID))
	}
	short := min(results[0].ContextRunes, 10)
	require.Equal(t, []string{
		fmt.Sprintf("1/%d/q1", results[0].ContextRunes), fmt.Sprintf("1/%d/q2_abs", results[1].ContextRunes),
		fmt.Sprintf("1/%d/q1", short), fmt.Sprintf("1/%d/q2_abs", min(results[1].ContextRunes, 10)),
		fmt.Sprintf("2/%d/q1", results[0].ContextRunes), fmt.Sprintf("2/%d/q2_abs", results[1].ContextRunes),
		fmt.Sprintf("2/%d/q1", short), fmt.Sprintf("2/%d/q2_abs", min(results[1].ContextRunes, 10)),
	}, got, "轮次、方案、对话、题目的顺序")
}

// 某个组合出错时其余组合照常完成；错误列出每个失败的轮次、方案与对话。
func TestRunnerContinuesAfterCaseError(t *testing.T) {
	cases, err := LoadLongMemEval(strings.NewReader(longMemEvalFixture))
	require.NoError(t, err)
	bad := &Memory{Label: "bad", Configure: func(session.Store) memory.Config { return memory.Config{} }}
	model := fakeModel{reply: func(string, string) string { return "no" }}
	results, err := Runner{Systems: []System{bad, Recent{}}, Answerer: model, Judge: model, Concurrency: 2}.Run(context.Background(), cases)
	require.ErrorContains(t, err, "第 1 轮 bad q1")
	require.ErrorContains(t, err, "第 1 轮 bad q2_abs")
	require.Len(t, results, 2, "recent 的两道题都有结果")
	for _, r := range results {
		require.Equal(t, "recent", r.System)
	}
}
