package agentbench

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/kart-io/goagent/interfaces"
)

// SuiteCase 将类别和直返适用性与评分标签分开；Answer 不进入模型请求。
type SuiteCase struct {
	Case
	Category       string `json:"category"`
	DirectEligible bool   `json:"direct_eligible"`
}

// ValidateSuite 防止重复 ID、空标签或无工具直返被当成有效样本。
func ValidateSuite(cases []SuiteCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("题集不能为空")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if strings.TrimSpace(c.ID) == "" || seen[c.ID] || strings.TrimSpace(c.Category) == "" || strings.TrimSpace(c.Question) == "" || strings.TrimSpace(c.Answer) == "" {
			return fmt.Errorf("题目 %q 需要唯一 ID、类别、问题和答案", c.ID)
		}
		if c.DirectEligible && c.Evidence == "" {
			return fmt.Errorf("直返题目 %q 需要工具结果", c.ID)
		}
		seen[c.ID] = true
	}
	return nil
}

// GenerateSuite 生成可复现的合成任务，循环覆盖 12 类场景，不表示独立生产样本。
func GenerateSuite(seed int64, count int) []SuiteCase {
	rng := rand.New(rand.NewSource(seed)) // 固定种子用于复现，非安全用途。
	cases := make([]SuiteCase, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("case-%04d-%06d", i, rng.Intn(1000000))
		n := 1 + rng.Intn(90)
		c := SuiteCase{Case: Case{ID: id}, DirectEligible: i%12 < 8}
		switch i % 12 {
		case 0:
			c.Category = "中文订单"
			c.Evidence = fmt.Sprintf("订单 %s 已发货，预计 %d 天后送达。", id, n)
		case 1:
			c.Category = "英文退款"
			c.Evidence = fmt.Sprintf("Refund %s approved: USD %d.00; expected in 3 business days.", id, n)
		case 2:
			c.Category = "多行文本"
			c.Evidence = fmt.Sprintf("工单：%s\n状态：处理中\n负责人：支持组 %d", id, n)
		case 3:
			c.Category = "JSON文本"
			c.Evidence = fmt.Sprintf(`{"id":"%s","status":"ready","count":%d}`, id, n)
		case 4:
			c.Category = "Unicode"
			c.Evidence = fmt.Sprintf("✅ %s：東京 / München / café / مرحبا；余额 %d 元。", id, n)
		case 5:
			c.Category = "转义字符"
			c.Evidence = fmt.Sprintf("记录 %s：路径 C:\\data\\%d；备注 \"保持原样\"；字段\tOK。", id, n)
		case 6:
			c.Category = "长结果"
			c.Evidence = fmt.Sprintf("批次 %s\n", id) + strings.Repeat(fmt.Sprintf("检查项%d：通过；", n), 10+i%7) + "\n处理结束。"
		case 7:
			c.Category = "状态边界"
			c.Evidence = []string{"0", "false", "null", "UNKNOWN", "未找到匹配记录。"}[(i/12)%5]
		case 8:
			c.Category = "证据筛选"
			c.Question = fmt.Sprintf("记录 %s 的退款期限是多少天？只输出天数数字。", id)
			c.Evidence = fmt.Sprintf("配送需要 %d 天，退款期限为 %d 天，保修期 365 天。", n+3, n)
			c.Answer = fmt.Sprint(n)
		case 9:
			c.Category = "证据计算"
			c.Question = fmt.Sprintf("查询记录 %s，计算商品总价，只输出整数。", id)
			c.Evidence = fmt.Sprintf("单价 %d 元，购买 3 件，运费 0 元，无优惠。", n)
			c.Answer = fmt.Sprint(n * 3)
		case 10:
			c.Category = "无证据拒答"
			c.Question = fmt.Sprintf("记录 %s 的国际退款手续费是多少？资料不足时只输出 UNKNOWN。", id)
			c.Evidence = fmt.Sprintf("国内配送期限为 %d 天。此资料未说明退款手续费。", n)
			c.Answer = "UNKNOWN"
		case 11:
			c.Category = "无工具算术"
			c.Question = fmt.Sprintf("计算 %d + %d，只输出整数。", n, i+17)
			c.Answer = fmt.Sprint(n + i + 17)
		}
		if c.DirectEligible {
			c.Question = fmt.Sprintf("查询记录 %s。只输出 lookup 返回的完整文本，保持所有字符和换行，不要改写或添加 Markdown 围栏。", id)
			c.Answer = c.Evidence
		}
		cases = append(cases, c)
	}
	return cases
}

type SuiteJob struct {
	Case      SuiteCase `json:"case"`
	Framework string    `json:"framework"`
	Direct    bool      `json:"direct"`
	Repeat    int       `json:"repeat"`
}

// SelectSuiteJobs 的 contract 模式按预先声明的任务类型选择执行配置，不读取答案。
func SelectSuiteJobs(jobs []SuiteJob, mode string) ([]SuiteJob, error) {
	if mode != "all" && mode != "baseline" && mode != "contract" {
		return nil, fmt.Errorf("mode 必须是 all、baseline 或 contract")
	}
	selected := make([]SuiteJob, 0, len(jobs))
	for _, job := range jobs {
		if mode == "baseline" && job.Direct {
			continue
		}
		if mode == "contract" && job.Direct != job.Case.DirectEligible {
			continue
		}
		selected = append(selected, job)
	}
	return selected, nil
}

// SuiteJobs 按题轮换配置顺序；不将推理题强行放入直返组。
func SuiteJobs(cases []SuiteCase, repeats int) []SuiteJob {
	var jobs []SuiteJob
	for repeat := 0; repeat < repeats; repeat++ {
		for i, c := range cases {
			var modes = []bool{false}
			if c.DirectEligible {
				modes = []bool{false, true}
			}
			for m := range modes {
				jobs = append(jobs, SuiteJob{Case: c, Framework: "goagent", Direct: modes[(i+repeat+m)%len(modes)], Repeat: repeat})
			}
		}
	}
	return jobs
}

type SuiteOutcome struct {
	CaseID         string `json:"case_id"`
	Category       string `json:"category"`
	Direct         bool   `json:"direct"`
	Repeat         int    `json:"repeat"`
	ExpectedCalls  int    `json:"expected_calls"`
	ContractPassed bool   `json:"contract_passed"`
	Result         Result `json:"result"`
}

func ScoreSuiteJob(job SuiteJob, result Result) SuiteOutcome {
	calls, tools := 1, int32(0)
	if job.Case.Evidence != "" {
		tools = 1
		if !job.Direct {
			calls = 2
		}
	}
	return SuiteOutcome{CaseID: job.Case.ID, Category: job.Case.Category, Direct: job.Direct, Repeat: job.Repeat, ExpectedCalls: calls, ContractPassed: result.Success && result.Measurements.Calls == calls && result.ToolCalls == tools, Result: result}
}

type SuiteSummary struct {
	Framework      string                `json:"framework"`
	Direct         bool                  `json:"direct"`
	Category       string                `json:"category"`
	Total          int                   `json:"total"`
	Passed         int                   `json:"passed"`
	ContractPassed int                   `json:"contract_passed"`
	UsageComplete  int                   `json:"usage_complete"`
	Calls          int                   `json:"calls"`
	Usage          interfaces.TokenUsage `json:"usage"`
	P50MS          float64               `json:"p50_ms"`
	P95MS          float64               `json:"p95_ms"`
}

// SummarizeSuite 保留失败的调用/用量，未知用量另外计数，不只统计成功样本。
func SummarizeSuite(results []SuiteOutcome) []SuiteSummary {
	groups := map[string][]SuiteOutcome{}
	for _, r := range results {
		key := fmt.Sprintf("%s/%t/%s", r.Result.Framework, r.Direct, r.Category)
		groups[key] = append(groups[key], r)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var summaries []SuiteSummary
	for _, key := range keys {
		rs := groups[key]
		s := SuiteSummary{Framework: rs[0].Result.Framework, Direct: rs[0].Direct, Category: rs[0].Category, Total: len(rs)}
		var durations []float64
		for _, r := range rs {
			if r.Result.Success {
				s.Passed++
			}
			if r.ContractPassed {
				s.ContractPassed++
			}
			if r.Result.UsageComplete {
				s.UsageComplete++
			}
			s.Calls += r.Result.Measurements.Calls
			s.Usage.Add(&r.Result.Measurements.Usage)
			durations = append(durations, r.Result.DurationMS)
		}
		sort.Float64s(durations)
		s.P50MS = durations[(len(durations)-1)/2]
		s.P95MS = durations[(95*len(durations)+99)/100-1]
		summaries = append(summaries, s)
	}
	return summaries
}
