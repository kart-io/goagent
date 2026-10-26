package agentbench

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSuiteGenerationAndScheduling(t *testing.T) {
	cases := GenerateSuite(20261026, 120)
	require.NoError(t, ValidateSuite(cases))
	require.Equal(t, cases, GenerateSuite(20261026, 120))
	require.NotEqual(t, cases, GenerateSuite(7, 120))
	ids := map[string]bool{}
	counts := map[string]int{}
	for _, c := range cases {
		require.False(t, ids[c.ID])
		ids[c.ID] = true
		counts[c.Category]++
		if c.DirectEligible {
			require.Equal(t, c.Evidence, c.Answer)
		}
	}
	require.Len(t, counts, 12)
	for _, n := range counts {
		require.Equal(t, 10, n)
	}
	jobs := SuiteJobs(cases, 2)
	require.Len(t, jobs, 400)
	seen := map[string]bool{}
	for _, j := range jobs {
		if j.Direct {
			require.True(t, j.Case.DirectEligible)
		}
		key := fmt.Sprintf("%s/%s/%t/%d", j.Case.ID, j.Framework, j.Direct, j.Repeat)
		require.False(t, seen[key])
		seen[key] = true
	}
}

func TestDecodeSuiteUsesOnlyImportedFields(t *testing.T) {
	cases, err := DecodeSuite([]byte(`[{"id":"plain","category":"plain","question":"Return 42","answer":"42"},{"id":"direct","category":"direct","question":"Return evidence","evidence":"value","answer":"value","direct_eligible":true}]`))
	require.NoError(t, err)
	require.Len(t, cases, 2)
	require.Empty(t, cases[0].Evidence)
	require.False(t, cases[0].DirectEligible)
	jobs, err := SelectSuiteJobs(SuiteJobs(cases, 1), "all")
	require.NoError(t, err)
	require.Len(t, jobs, 3, "plain task must not inherit a synthetic tool or direct flag")
	for _, input := range []string{`null`, `[]`, `{}`, `[{"id":"missing"}]`} {
		_, err := DecodeSuite([]byte(input))
		require.Error(t, err)
	}
}

func TestSuiteValidation(t *testing.T) {
	require.Error(t, ValidateSuite(nil))
	for _, field := range []string{"id", "category", "question", "answer", "evidence", "duplicate"} {
		cases := GenerateSuite(1, 2)
		switch field {
		case "id":
			cases[0].ID = ""
		case "category":
			cases[0].Category = ""
		case "question":
			cases[0].Question = ""
		case "answer":
			cases[0].Answer = ""
		case "evidence":
			cases[0].Evidence = ""
		case "duplicate":
			cases[1].ID = cases[0].ID
		}
		require.Error(t, ValidateSuite(cases), field)
	}
}

func TestSuiteContractRoutingDoesNotUseAnswers(t *testing.T) {
	cases := GenerateSuite(20261026, 120)
	for i := range cases {
		cases[i].Answer = "不应影响路由的评分标签"
	}
	jobs, err := SelectSuiteJobs(SuiteJobs(cases, 1), "contract")
	require.NoError(t, err)
	require.Len(t, jobs, 120)
	direct := 0
	seen := map[string]bool{}
	for _, job := range jobs {
		require.False(t, seen[job.Case.ID])
		seen[job.Case.ID] = true
		require.Equal(t, job.Case.DirectEligible, job.Direct)
		require.Equal(t, "goagent", job.Framework)
		if job.Direct {
			direct++
		}
	}
	require.Equal(t, 80, direct)
	baseline, err := SelectSuiteJobs(SuiteJobs(cases, 1), "baseline")
	require.NoError(t, err)
	require.Len(t, baseline, 120)
	_, err = SelectSuiteJobs(nil, "bad")
	require.Error(t, err)
}

// 使用实际 SDK 批量覆盖传输/序列化；假模型只用于协议验证，不计入线上成功率。
func TestDirectSuiteThroughSDK(t *testing.T) {
	server := fakeModel(t)
	defer server.Close()
	total := 0
	for _, c := range GenerateSuite(20261026, 1200) {
		if !c.DirectEligible {
			continue
		}
		cfg := testConfig(server.URL)
		cfg.DirectTools = true
		cfg.Budget.MaxCalls = 1
		result := Run(context.Background(), c.Case, cfg)
		out := ScoreSuiteJob(SuiteJob{Case: c, Framework: "goagent", Direct: true}, result)
		require.True(t, out.ContractPassed, "%s/%s: %+v", "goagent", c.ID, result)
		require.True(t, result.UsageComplete)
		total++
	}
	require.Equal(t, 800, total)
}

func TestSuiteSummaryIncludesFailures(t *testing.T) {
	success := Result{Framework: "goagent", Success: true, UsageComplete: true, DurationMS: 10}
	success.Measurements.Calls = 1
	success.Measurements.Usage.TotalTokens = 5
	failure := success
	failure.Success = false
	failure.UsageComplete = false
	failure.DurationMS = 100
	failure.Measurements.Usage.TotalTokens = 0
	s := SummarizeSuite([]SuiteOutcome{{Category: "文本", Result: success, ContractPassed: true}, {Category: "文本", Result: failure}})
	require.Len(t, s, 1)
	require.Equal(t, 2, s[0].Total)
	require.Equal(t, 1, s[0].Passed)
	require.Equal(t, 1, s[0].ContractPassed)
	require.Equal(t, 1, s[0].UsageComplete)
	require.Equal(t, 2, s[0].Calls)
	require.Equal(t, 5, s[0].Usage.TotalTokens)
	require.Equal(t, float64(100), s[0].P95MS)
}

func TestConcurrentRuns(t *testing.T) {
	server := fakeModel(t)
	t.Cleanup(server.Close)
	for i := 0; i < 40; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(server.URL)
			cfg.DirectTools = i%2 == 0

			c := SuiteCase{Case: Case{ID: fmt.Sprint(i), Question: "退款天数", Evidence: "30", Answer: "30"}, Category: "并发隔离", DirectEligible: true}
			result := Run(context.Background(), c.Case, cfg)
			out := ScoreSuiteJob(SuiteJob{Case: c, Framework: "goagent", Direct: cfg.DirectTools}, result)
			require.True(t, out.ContractPassed, "%+v", result)
			require.True(t, result.UsageComplete)
		})
	}
}
