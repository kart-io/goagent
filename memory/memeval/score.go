package memeval

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// AnswerF1 按 LOCOMO 官方评测（task_eval/evaluation.py）计算词级 F1：多跳题按逗号拆成子答案取平均，开放域题取分号前的答案，
// 对抗题在回答含 "no information available" 或 "not mentioned" 时记 1。与官方脚本的差别：未做 Porter 词干化，分数略偏低。
// LongMemEval 题目没有 F1，返回 0。
func AnswerF1(q Question, hypothesis string) float64 {
	switch q.Category {
	case 0:
		return 0
	case 5:
		h := strings.ToLower(hypothesis)
		if strings.Contains(h, "no information available") || strings.Contains(h, "not mentioned") {
			return 1
		}
		return 0
	case 1:
		var preds []string
		for _, p := range strings.Split(hypothesis, ",") {
			preds = append(preds, strings.TrimSpace(p))
		}
		total, golds := 0.0, strings.Split(q.Answer, ",")
		for _, g := range golds {
			best := 0.0
			for _, p := range preds {
				best = max(best, tokenF1(p, g))
			}
			total += best
		}
		return total / float64(len(golds))
	case 3:
		answer, _, _ := strings.Cut(q.Answer, ";")
		return tokenF1(hypothesis, answer)
	}
	return tokenF1(hypothesis, q.Answer)
}

var articles = regexp.MustCompile(`\b(a|an|the|and)\b`)

// normalizeAnswer 小写、去标点与冠词（官方脚本还去掉 and）、合并空白。
func normalizeAnswer(s string) []string {
	s = strings.ToLower(strings.ReplaceAll(s, ",", ""))
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, s)
	return strings.Fields(articles.ReplaceAllString(s, " "))
}

func tokenF1(prediction, gold string) float64 {
	pred, ref := normalizeAnswer(prediction), normalizeAnswer(gold)
	counts := map[string]int{}
	for _, t := range ref {
		counts[t]++
	}
	common := 0
	for _, t := range pred {
		if counts[t] > 0 {
			counts[t]--
			common++
		}
	}
	if common == 0 {
		return 0
	}
	p, r := float64(common)/float64(len(pred)), float64(common)/float64(len(ref))
	return 2 * p * r / (p + r)
}

// Row 是一个 System 在一类题上的汇总。以题目为单位：每道题先在各轮间取平均，再在题目间平均。
type Row struct {
	System, Type string
	Questions    int
	Accuracy     float64
	// Low、High 是准确率的 95% Wilson 区间（以题目数为样本量）。
	Low, High float64
	// RunMin、RunMax 是各轮准确率的最小与最大值，反映重新抽取带来的波动；只有一轮时等于 Accuracy。
	RunMin, RunMax float64
	F1             float64
	// EvidenceRecall 是有证据会话的题中，上下文依据了证据会话的比例。
	EvidenceRecall float64
	ContextRunes   float64
}

// Summarize 按 System 与题型汇总，每个 System 另有一行 "all"；System 按首次出现的顺序排列。
func Summarize(results []Result) []Row {
	type key struct{ system, typ string }
	type question struct {
		correct, f1, runes float64
		runs               int
		evidence, hits     int
	}
	groups := map[key]map[string]*question{}
	perRun := map[key]map[int][2]int{}
	var systems []string
	var keys []key
	for _, r := range results {
		if !slices.Contains(systems, r.System) {
			systems = append(systems, r.System)
		}
		typ := r.Type
		if r.Abstain {
			typ += "（弃答）"
		}
		for _, k := range []key{{r.System, typ}, {r.System, "all"}} {
			if groups[k] == nil {
				groups[k], perRun[k] = map[string]*question{}, map[int][2]int{}
				keys = append(keys, k)
			}
			q := groups[k][r.QuestionID]
			if q == nil {
				q = &question{}
				groups[k][r.QuestionID] = q
			}
			q.runs++
			q.f1 += r.F1
			q.runes += float64(r.ContextRunes)
			c := perRun[k][r.Run]
			c[1]++
			if r.Correct {
				q.correct++
				c[0]++
			}
			perRun[k][r.Run] = c
			if len(r.Evidence) > 0 && !r.Abstain {
				q.evidence++
				if r.EvidenceHit {
					q.hits++
				}
			}
		}
	}
	slices.SortStableFunc(keys, func(a, b key) int {
		if a.system != b.system {
			return slices.Index(systems, a.system) - slices.Index(systems, b.system)
		}
		if (a.typ == "all") != (b.typ == "all") {
			if a.typ == "all" {
				return 1
			}
			return -1
		}
		return strings.Compare(a.typ, b.typ)
	})
	rows := make([]Row, 0, len(keys))
	for _, k := range keys {
		row := Row{System: k.system, Type: k.typ, Questions: len(groups[k]), RunMin: 1}
		evidence, hits := 0, 0
		for _, q := range groups[k] {
			n := float64(q.runs)
			row.Accuracy += q.correct / n
			row.F1 += q.f1 / n
			row.ContextRunes += q.runes / n
			evidence += q.evidence
			hits += q.hits
		}
		correct := row.Accuracy
		total := float64(row.Questions)
		row.Accuracy, row.F1, row.ContextRunes = correct/total, row.F1/total, row.ContextRunes/total
		row.Low, row.High = wilson(correct, total)
		for _, c := range perRun[k] {
			acc := float64(c[0]) / float64(c[1])
			row.RunMin, row.RunMax = min(row.RunMin, acc), max(row.RunMax, acc)
		}
		if evidence > 0 {
			row.EvidenceRecall = float64(hits) / float64(evidence)
		}
		rows = append(rows, row)
	}
	return rows
}

// wilson 返回 n 次中成功 k 次的 95% Wilson 区间；k 可以是多轮平均后的小数。
func wilson(k, n float64) (float64, float64) {
	const z = 1.96
	p := k / n
	d := 1 + z*z/n
	c := p + z*z/(2*n)
	h := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n))
	return max((c-h)/d, 0), min((c+h)/d, 1)
}

// Pair 是两个 System 在同一轮同一道题上的配对比较：Wins 为 A 对而 B 错的次数，Losses 相反。
type Pair struct {
	A, B         string
	Wins, Losses int
	// P 是双侧精确符号检验的 p 值：两者实际无差别时，出现至少这样悬殊的胜负的概率。
	P float64
}

// Compare 两两比较各 System（按首次出现的顺序）。
func Compare(results []Result) []Pair {
	type key struct {
		run int
		id  string
	}
	correct := map[string]map[key]bool{}
	var systems []string
	for _, r := range results {
		if correct[r.System] == nil {
			correct[r.System] = map[key]bool{}
			systems = append(systems, r.System)
		}
		correct[r.System][key{r.Run, r.QuestionID}] = r.Correct
	}
	var out []Pair
	for i, a := range systems {
		for _, b := range systems[i+1:] {
			p := Pair{A: a, B: b}
			for k, ca := range correct[a] {
				cb, ok := correct[b][k]
				switch {
				case !ok:
				case ca && !cb:
					p.Wins++
				case cb && !ca:
					p.Losses++
				}
			}
			p.P = signTest(p.Wins, p.Losses)
			out = append(out, p)
		}
	}
	return out
}

// signTest 是 n=wins+losses 次中出现至少 max(wins,losses) 次同侧结果的双侧二项概率（p=0.5）。
func signTest(wins, losses int) float64 {
	n, k := wins+losses, max(wins, losses)
	if n == 0 {
		return 1
	}
	tail := 0.0
	for i := k; i <= n; i++ {
		lg, _ := math.Lgamma(float64(n + 1))
		li, _ := math.Lgamma(float64(i + 1))
		lr, _ := math.Lgamma(float64(n - i + 1))
		tail += math.Exp(lg - li - lr - float64(n)*math.Ln2)
	}
	return min(2*tail, 1)
}

// Report 把汇总与两两比较写成 Markdown。
func Report(results []Result) string {
	var sb strings.Builder
	sb.WriteString("| 方案 | 题型 | 题数 | 准确率 | 95% 区间 | 各轮范围 | F1 | 证据召回 | 平均上下文字符 |\n|---|---|---:|---:|---|---|---:|---:|---:|\n")
	for _, r := range Summarize(results) {
		fmt.Fprintf(&sb, "| %s | %s | %d | %.1f%% | %.0f%%–%.0f%% | %.0f%%–%.0f%% | %.3f | %.1f%% | %.0f |\n", r.System, r.Type, r.Questions,
			100*r.Accuracy, 100*r.Low, 100*r.High, 100*r.RunMin, 100*r.RunMax, r.F1, 100*r.EvidenceRecall, r.ContextRunes)
	}
	sb.WriteString("\n两两比较（同一轮同一道题，A 对 B 错为胜；p < 0.05 才说明差异不太可能来自波动）：\n\n| A | B | A 胜 | A 负 | p 值 |\n|---|---|---:|---:|---:|\n")
	for _, p := range Compare(results) {
		fmt.Fprintf(&sb, "| %s | %s | %d | %d | %.3f |\n", p.A, p.B, p.Wins, p.Losses, p.P)
	}
	return sb.String()
}
