// Live synthetic strategy evaluation. All strategies share cases, model and
// budgets; output matching is deliberately strict and no LLM judge is used.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/kart-io/goagent/agents/cot"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/agents/tot"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/evaluation"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm/providers"
)

var finalPattern = regexp.MustCompile(`(?i)ANSWER:\s*(-?\d+(?:\.\d+)?)`)

func main() {
	live := flag.Bool("live", false, "make billable model calls")
	dataset := flag.String("dataset", "evaluation/testdata/reasoning_synthetic.json", "synthetic labeled dataset")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "same model for every strategy")
	calls := flag.Int("max-calls", 8, "per-case call ceiling")
	output := flag.Int("max-output-tokens", 800, "per-call output ceiling")
	tokens := flag.Int("token-stop", 10000, "stop future calls at observed token usage")
	timeout := flag.Duration("timeout", 90*time.Second, "per-case timeout")
	inputPrice := flag.Float64("input-price", -1, "input price per million tokens, optional")
	outputPrice := flag.Float64("output-price", -1, "output price per million tokens, optional")
	flag.Parse()
	if !*live {
		log.Fatal("pass -live to run model evaluation; unit tests run without credentials")
	}
	if *model == "" {
		log.Fatal("set OPENAI_MODEL or -model")
	}
	data, err := os.ReadFile(*dataset)
	if err != nil {
		log.Fatal(err)
	}
	var cases []struct{ ID, Question, Answer string }
	if err = json.Unmarshal(data, &cases); err != nil {
		log.Fatal(err)
	}
	if len(cases) == 0 {
		log.Fatal("empty dataset")
	}
	client, err := providers.NewOpenAIWithOptions()
	if err != nil {
		log.Fatal(err)
	}
	budget := evaluation.Budget{Model: *model, MaxCalls: *calls, MaxOutputTokens: *output, ObservedTokens: *tokens, Temperature: 0.2}
	type invoker interface {
		Invoke(context.Context, *core.AgentInput) (*core.AgentOutput, error)
	}
	rows := []map[string]interface{}{}
	successes := map[string]int{}
	names := []string{"react", "cot", "tot"}
	for index, test := range cases {
		// Rotate execution order across cases to reduce order/cache bias.
		for j := range names {
			name := names[(index+j)%len(names)]
			meter := &evaluation.BudgetClient{Client: client, Budget: budget}
			var agent invoker
			switch name {
			case "react":
				agent = react.NewReActAgent(react.ReActConfig{LLM: meter, MaxSteps: *calls})
			case "cot":
				agent = cot.NewCoTAgent(cot.CoTConfig{LLM: meter, ZeroShot: true, FinalAnswerFormat: "ANSWER:"})
			case "tot":
				agent = tot.NewToTAgent(tot.ToTConfig{LLM: meter, MaxDepth: 2, BranchingFactor: 2, BeamWidth: 1, EvaluationMethod: "llm"})
			}
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			start := time.Now()
			result, runErr := agent.Invoke(ctx, &core.AgentInput{Task: test.Question + "\nEnd your final answer with ANSWER: followed by only the numeric answer."})
			elapsed := time.Since(start)
			cancel()
			text := ""
			if result != nil && result.Result != nil {
				text = fmt.Sprint(result.Result)
			}
			answer := strings.TrimSpace(text)
			matches := finalPattern.FindAllStringSubmatch(text, -1)
			if len(matches) > 0 {
				answer = matches[len(matches)-1][1]
			}
			success := runErr == nil && result != nil && result.Status == interfaces.StatusSuccess && answer == test.Answer
			if success {
				successes[name]++
			}
			measured := meter.Measurements()
			row := map[string]interface{}{"case": test.ID, "strategy": name, "success": success, "answer": answer, "duration_ms": elapsed.Milliseconds(), "measurements": measured}
			if result != nil {
				row["agent_status"] = result.Status
				row["agent_usage"] = result.TokenUsage
			}
			if runErr != nil {
				row["error"] = runErr.Error()
			}
			if *inputPrice >= 0 && *outputPrice >= 0 && measured.UsageReports == measured.Calls {
				row["estimated_cost"] = evaluation.EstimatedCost(measured.Usage, *inputPrice, *outputPrice)
			}
			rows = append(rows, row)
		}
	}
	rates := map[string]float64{}
	for _, name := range names {
		rates[name] = float64(successes[name]) / float64(len(cases))
	}
	report := map[string]interface{}{"dataset": *dataset, "synthetic": true, "budget": budget, "timeout": timeout.String(), "tool_count": 0, "success_rates": rates, "results": rows, "scoring": "last numeric ANSWER marker or exact numeric output; synthetic regression, not a business benchmark"}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err = enc.Encode(report); err != nil {
		log.Fatal(err)
	}
}
