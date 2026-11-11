// Package memeval 在 LongMemEval 与 LOCOMO 上评测长期记忆：把历史会话灌入被测系统，按问题取上下文、作答并评分。
package memeval

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// Session 是一段历史会话。
type Session struct {
	ID    string
	Date  time.Time
	Turns []llm.Message
}

// Question 是一道题。Evidence 是包含答案的会话 ID；Abstain 表示历史中没有答案，正确的回答是指出无法回答。
type Question struct {
	ID       string
	Type     string
	Text     string
	Answer   string
	Date     time.Time
	Evidence []string
	Abstain  bool
	// Category 是 LOCOMO 的题目类别（1 多跳、2 时间、3 开放域、4 单跳、5 对抗），LongMemEval 为 0。
	Category int
}

// Case 是共享同一段历史的一组问题；每个 Case 使用独立的记忆命名空间。
type Case struct {
	ID        string
	Sessions  []Session
	Questions []Question
}

// longMemEvalDate 是 LongMemEval 的日期格式，如 "2023/05/20 (Sat) 02:21"。
const longMemEvalDate = "2006/01/02 (Mon) 15:04"

// LoadLongMemEval 读取 LongMemEval（oracle、s、m 任一版本）的 JSON：每道题自带一段历史，成为一个 Case。
func LoadLongMemEval(r io.Reader) ([]Case, error) {
	var raw []struct {
		QuestionID   string          `json:"question_id"`
		QuestionType string          `json:"question_type"`
		Question     string          `json:"question"`
		Answer       json.RawMessage `json:"answer"`
		QuestionDate string          `json:"question_date"`
		SessionIDs   []string        `json:"haystack_session_ids"`
		Dates        []string        `json:"haystack_dates"`
		Sessions     [][]struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"haystack_sessions"`
		AnswerSessionIDs []string `json:"answer_session_ids"`
	}
	if err := json.Decode(r, &raw); err != nil {
		return nil, err
	}
	out := make([]Case, 0, len(raw))
	for _, item := range raw {
		if len(item.SessionIDs) != len(item.Sessions) || len(item.Dates) != len(item.Sessions) {
			return nil, invalid("LongMemEval 题目 %s 的会话、会话 ID 与日期数量不一致", item.QuestionID)
		}
		c := Case{ID: item.QuestionID}
		for i, turns := range item.Sessions {
			date, err := time.Parse(longMemEvalDate, item.Dates[i])
			if err != nil {
				return nil, invalid("LongMemEval 题目 %s 的日期 %q 无法解析", item.QuestionID, item.Dates[i])
			}
			s := Session{ID: item.SessionIDs[i], Date: date}
			for _, t := range turns {
				s.Turns = append(s.Turns, llm.Message{Role: t.Role, Content: t.Content})
			}
			c.Sessions = append(c.Sessions, s)
		}
		// oracle 版本的会话未按时间排序。
		slices.SortStableFunc(c.Sessions, func(a, b Session) int { return a.Date.Compare(b.Date) })
		date, err := time.Parse(longMemEvalDate, item.QuestionDate)
		if err != nil {
			return nil, invalid("LongMemEval 题目 %s 的提问日期 %q 无法解析", item.QuestionID, item.QuestionDate)
		}
		c.Questions = []Question{{ID: item.QuestionID, Type: item.QuestionType, Text: item.Question, Answer: scalar(item.Answer),
			Date: date, Evidence: item.AnswerSessionIDs, Abstain: strings.Contains(item.QuestionID, "_abs")}}
		out = append(out, c)
	}
	return out, nil
}

// locomoDate 是 LOCOMO 的日期格式，如 "1:56 pm on 8 May, 2023"。
const locomoDate = "3:04 pm on 2 January, 2006"

// locomoTypes 是 LOCOMO 类别编号的含义（以官方评测代码为准，与论文的叙述顺序不同）。
var locomoTypes = map[int]string{1: "multi-hop", 2: "temporal", 3: "open-domain", 4: "single-hop", 5: "adversarial"}

// LoadLoCoMo 读取 LOCOMO 的 locomo10.json：每段对话成为一个 Case。speaker_a 记为 user、speaker_b 记为 assistant，
// 内容前加说话人姓名，因为问题按姓名提问。第 5 类（对抗题）没有答案，记为应当弃答。
func LoadLoCoMo(r io.Reader) ([]Case, error) {
	var raw []struct {
		SampleID     string                     `json:"sample_id"`
		Conversation map[string]json.RawMessage `json:"conversation"`
		QA           []struct {
			Question  string          `json:"question"`
			Answer    json.RawMessage `json:"answer"`
			Evidence  []string        `json:"evidence"`
			Category  int             `json:"category"`
			Adversary json.RawMessage `json:"adversarial_answer"`
		} `json:"qa"`
	}
	if err := json.Decode(r, &raw); err != nil {
		return nil, err
	}
	out := make([]Case, 0, len(raw))
	for _, item := range raw {
		var speakerA string
		_ = json.Unmarshal(item.Conversation["speaker_a"], &speakerA)
		c := Case{ID: item.SampleID}
		var last time.Time
		for n := 1; ; n++ {
			key := "session_" + strconv.Itoa(n)
			body, ok := item.Conversation[key]
			if !ok {
				break
			}
			var turns []struct {
				Speaker string `json:"speaker"`
				Text    string `json:"text"`
			}
			if err := json.Unmarshal(body, &turns); err != nil {
				return nil, err
			}
			var when string
			_ = json.Unmarshal(item.Conversation[key+"_date_time"], &when)
			date, err := time.Parse(locomoDate, strings.TrimSpace(when))
			if err != nil {
				return nil, invalid("LOCOMO %s 的 %s 日期 %q 无法解析", item.SampleID, key, when)
			}
			last = date
			s := Session{ID: key, Date: date}
			for _, t := range turns {
				role := "assistant"
				if t.Speaker == speakerA {
					role = "user"
				}
				s.Turns = append(s.Turns, llm.Message{Role: role, Content: t.Speaker + ": " + t.Text})
			}
			c.Sessions = append(c.Sessions, s)
		}
		for i, q := range item.QA {
			var evidence []string
			for _, e := range q.Evidence {
				// 证据形如 "D2:8"，即 session_2 的第 8 句。
				if d, _, ok := strings.Cut(strings.TrimPrefix(e, "D"), ":"); ok && !slices.Contains(evidence, "session_"+d) {
					evidence = append(evidence, "session_"+d)
				}
			}
			c.Questions = append(c.Questions, Question{ID: item.SampleID + "/" + strconv.Itoa(i), Type: cmp.Or(locomoTypes[q.Category], "unknown"),
				Text: q.Question, Answer: scalar(q.Answer), Date: last, Evidence: evidence, Abstain: q.Category == 5, Category: q.Category})
		}
		out = append(out, c)
	}
	return out, nil
}

// scalar 把字符串或数字形式的答案转为文本。
func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func invalid(format string, args ...any) error {
	return agentErrors.Newf(agentErrors.CodeInvalidInput, format, args...).WithComponent("memeval").WithOperation("load")
}

// String 便于日志输出。
func (q Question) String() string { return fmt.Sprintf("%s[%s] %s", q.ID, q.Type, q.Text) }

// Sample 按题型分层抽取 n 道题：各题型的名额与其题数成正比（最大余数法，题数足够时每个题型至少一道），
// 题型内按数据集顺序等间隔抽取，使题目分散在各个 Case 中。没有题目被选中的 Case 被去掉；n 不小于总题数时原样返回。
func Sample(cases []Case, n int) []Case {
	type ref struct{ c, q int }
	byType := map[string][]ref{}
	var types []string
	total := 0
	for ci, c := range cases {
		for qi, q := range c.Questions {
			if _, ok := byType[q.Type]; !ok {
				types = append(types, q.Type)
			}
			byType[q.Type] = append(byType[q.Type], ref{ci, qi})
			total++
		}
	}
	if n <= 0 || n >= total {
		return cases
	}
	quota := map[string]int{}
	type remainder struct {
		typ  string
		frac float64
	}
	var rest []remainder
	assigned := 0
	for _, t := range types {
		exact := float64(n) * float64(len(byType[t])) / float64(total)
		quota[t] = int(exact)
		if n >= len(types) {
			quota[t] = max(quota[t], 1)
		}
		assigned += quota[t]
		rest = append(rest, remainder{t, exact - float64(int(exact))})
	}
	slices.SortStableFunc(rest, func(a, b remainder) int { return cmp.Compare(b.frac, a.frac) })
	for i := 0; assigned < n; i = (i + 1) % len(rest) {
		if quota[rest[i].typ] < len(byType[rest[i].typ]) {
			quota[rest[i].typ]++
			assigned++
		}
	}
	for assigned > n {
		// 保底名额使总数超出时，从名额最多的题型扣回。
		t := slices.MaxFunc(types, func(a, b string) int { return cmp.Compare(quota[a], quota[b]) })
		quota[t]--
		assigned--
	}
	picked := map[ref]bool{}
	for _, t := range types {
		refs := byType[t]
		for i := range quota[t] {
			picked[refs[i*len(refs)/quota[t]]] = true
		}
	}
	var out []Case
	for ci, c := range cases {
		kept := c
		kept.Questions = nil
		for qi, q := range c.Questions {
			if picked[ref{ci, qi}] {
				kept.Questions = append(kept.Questions, q)
			}
		}
		if len(kept.Questions) > 0 {
			out = append(out, kept)
		}
	}
	return out
}
