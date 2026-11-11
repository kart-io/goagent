package memeval

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

// Runner 对每个 System 灌入每个 Case 的历史，再逐题取上下文、作答与评分；整个过程重复 Repeats 次。
// 同一 System 在不同轮次重新抽取记忆，因而重复可以反映抽取与召回的波动。
type Runner struct {
	Systems []System
	// Answerer 根据上下文作答；Judge 判定回答是否正确（LongMemEval 的官方判定提示词）。
	Answerer, Judge llm.Client
	// Repeats 是重复次数，至少 1。
	Repeats int
	// Concurrency 是同时处理的（轮次、System、Case）组合数，至少 1。
	Concurrency int
	// Progress 接收每道题的结果，可为 nil；并发运行时调用是串行的。
	Progress func(Result)
}

// Result 是一个 System 在一轮中一道题上的结果。
type Result struct {
	Run        int      `json:"run"`
	System     string   `json:"system"`
	CaseID     string   `json:"case_id"`
	QuestionID string   `json:"question_id"`
	Type       string   `json:"type"`
	Abstain    bool     `json:"abstain"`
	Question   string   `json:"question"`
	Answer     string   `json:"answer"`
	Hypothesis string   `json:"hypothesis"`
	Correct    bool     `json:"correct"`
	F1         float64  `json:"f1"`
	Evidence   []string `json:"evidence"`
	Retrieved  []string `json:"retrieved"`
	// EvidenceHit 表示上下文所依据的会话中至少有一个证据会话；没有证据会话的题不计入召回率。
	EvidenceHit  bool `json:"evidence_hit"`
	ContextRunes int  `json:"context_runes"`

	order [3]int
}

// Run 返回全部结果，按轮次、System、Case 与题目的顺序排列。某个组合出错时记下错误、保留它已完成的题目，
// 其余组合照常运行：一次评测要几个小时，个别对话失败不应作废全部结果。返回的错误合并了所有失败组合。
func (r Runner) Run(ctx context.Context, cases []Case) ([]Result, error) {
	var (
		mu   sync.Mutex
		out  []Result
		errs []error
		wg   sync.WaitGroup
		sem  = make(chan struct{}, max(r.Concurrency, 1))
	)
	for run := range max(r.Repeats, 1) {
		for si, sys := range r.Systems {
			for ci, c := range cases {
				wg.Add(1)
				go func() {
					defer wg.Done()
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-ctx.Done():
						return
					}
					err := r.runCase(ctx, sys, c, func(qi int, res Result) {
						res.Run, res.order = run+1, [3]int{si, ci, qi}
						mu.Lock()
						defer mu.Unlock()
						out = append(out, res)
						if r.Progress != nil {
							r.Progress(res)
						}
					})
					if err != nil {
						mu.Lock()
						errs = append(errs, fmt.Errorf("第 %d 轮 %s %s：%w", run+1, sys.Name(), c.ID, err))
						mu.Unlock()
					}
				}()
			}
		}
	}
	wg.Wait()
	slices.SortFunc(out, func(a, b Result) int {
		if a.Run != b.Run {
			return a.Run - b.Run
		}
		return slices.Compare(a.order[:], b.order[:])
	})
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return out, errors.Join(errs...)
}

func (r Runner) runCase(ctx context.Context, sys System, c Case, emit func(int, Result)) error {
	retriever, err := sys.Ingest(ctx, c)
	if err != nil {
		return fmt.Errorf("灌入：%w", err)
	}
	for qi, q := range c.Questions {
		res, err := r.ask(ctx, sys.Name(), retriever, c, q)
		if err != nil {
			return fmt.Errorf("回答 %s：%w", q.ID, err)
		}
		emit(qi, res)
	}
	return nil
}

// answerPrompt 要求按问题语言简短作答；弃答用固定短语，以便 LOCOMO 对抗题按官方规则计分。
const answerPrompt = "你根据与用户过去对话中的信息回答问题。下面的上下文按时间记录，带有日期。必须使用与问题相同的语言作答（英文问题用英文回答），简短，只给出答案本身；" +
	"涉及时间时根据日期推算出具体时间，日期写成 \"7 May 2023\" 这样的格式（只知道月份或年份时写 \"May 2023\"、\"2023\"）。上下文中没有足够信息时只回答 not mentioned。"

func (r Runner) ask(ctx context.Context, system string, retriever Retriever, c Case, q Question) (Result, error) {
	got, err := retry(ctx, func() (Retrieved, error) { return retriever.Context(ctx, q) })
	if err != nil {
		return Result{}, err
	}
	user := fmt.Sprintf("上下文：\n%s\n\n当前日期：%s\n问题：%s", got.Text, q.Date.Format(time.DateTime), q.Text)
	resp, err := chat(ctx, r.Answerer, []llm.Message{llm.SystemMessage(answerPrompt), llm.UserMessage(user)})
	if err != nil {
		return Result{}, err
	}
	hyp := strings.TrimSpace(resp.Content)
	res := Result{System: system, CaseID: c.ID, QuestionID: q.ID, Type: q.Type, Abstain: q.Abstain, Question: q.Text, Answer: q.Answer,
		Hypothesis: hyp, Evidence: q.Evidence, Retrieved: got.Sessions, ContextRunes: len([]rune(got.Text)), F1: AnswerF1(q, hyp)}
	res.EvidenceHit = slices.ContainsFunc(got.Sessions, func(s string) bool { return slices.Contains(q.Evidence, s) })
	verdict, err := chat(ctx, r.Judge, []llm.Message{llm.UserMessage(JudgePrompt(q, hyp))})
	if err != nil {
		return Result{}, err
	}
	res.Correct = strings.Contains(strings.ToLower(verdict.Content), "yes")
	return res, nil
}

// JudgePrompt 返回 LongMemEval 官方评测（src/evaluation/evaluate_qa.py）按题型使用的判定提示词，原文照录以保持可比。
// LOCOMO 题目使用通用模板，对抗题使用弃答模板。
func JudgePrompt(q Question, response string) string {
	const generic = "I will give you a question, a correct answer, and a response from a model. Please answer yes if the response contains the correct answer. Otherwise, answer no. If the response is equivalent to the correct answer or contains all the intermediate steps to get the correct answer, you should also answer yes. If the response only contains a subset of the information required by the answer, answer no. "
	switch {
	case q.Abstain:
		return fmt.Sprintf("I will give you an unanswerable question, an explanation, and a response from a model. Please answer yes if the model correctly identifies the question as unanswerable. The model could say that the information is incomplete, or some other information is given but the asked information is not.\n\nQuestion: %s\n\nExplanation: %s\n\nModel Response: %s\n\nDoes the model correctly identify the question as unanswerable? Answer yes or no only.", q.Text, q.Answer, response)
	case q.Type == "temporal-reasoning":
		return fmt.Sprintf(generic+"In addition, do not penalize off-by-one errors for the number of days. If the question asks for the number of days/weeks/months, etc., and the model makes off-by-one errors (e.g., predicting 19 days when the answer is 18), the model's response is still correct. \n\nQuestion: %s\n\nCorrect Answer: %s\n\nModel Response: %s\n\nIs the model response correct? Answer yes or no only.", q.Text, q.Answer, response)
	case q.Type == "knowledge-update":
		return fmt.Sprintf("I will give you a question, a correct answer, and a response from a model. Please answer yes if the response contains the correct answer. Otherwise, answer no. If the response contains some previous information along with an updated answer, the response should be considered as correct as long as the updated answer is the required answer.\n\nQuestion: %s\n\nCorrect Answer: %s\n\nModel Response: %s\n\nIs the model response correct? Answer yes or no only.", q.Text, q.Answer, response)
	case q.Type == "single-session-preference":
		return fmt.Sprintf("I will give you a question, a rubric for desired personalized response, and a response from a model. Please answer yes if the response satisfies the desired response. Otherwise, answer no. The model does not need to reflect all the points in the rubric. The response is correct as long as it recalls and utilizes the user's personal information correctly.\n\nQuestion: %s\n\nRubric: %s\n\nModel Response: %s\n\nIs the model response correct? Answer yes or no only.", q.Text, q.Answer, response)
	}
	return fmt.Sprintf(generic+"\n\nQuestion: %s\n\nCorrect Answer: %s\n\nModel Response: %s\n\nIs the model response correct? Answer yes or no only.", q.Text, q.Answer, response)
}

// chatAttempts 是取上下文、作答与判定的最多尝试次数；评测跑数千次请求，提供方偶发的 5xx 与限流不应中断整轮。
const chatAttempts = 4

// chatBackoff 是第 attempt 次失败后的等待时间：2、4、8 秒。
var chatBackoff = func(attempt int) time.Duration { return time.Duration(1<<attempt) * time.Second }

func chat(ctx context.Context, c llm.Client, msgs []llm.Message) (*llm.CompletionResponse, error) {
	return retry(ctx, func() (*llm.CompletionResponse, error) { return llm.Chat(ctx, c, msgs) })
}

// retry 对可重试的错误（llm/common.IsRetryable）退避后重试。
func retry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	for attempt := 1; ; attempt++ {
		v, err := fn()
		if err == nil || attempt == chatAttempts || !common.IsRetryable(err) {
			return v, err
		}
		if err := sleep(ctx, chatBackoff(attempt)); err != nil {
			var zero T
			return zero, err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
