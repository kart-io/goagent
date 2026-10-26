package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/evaluation"
	bench "github.com/kart-io/goagent/evaluation/agentbench"
	"github.com/kart-io/goagent/evaluation/einocompare"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	live := flag.Bool("live", false, "call the configured upstream; default only writes a reproducible dataset")
	count := flag.Int("count", 24, "number of generated synthetic tasks")
	seed := flag.Int64("seed", 20261026, "dataset seed")
	dataset := flag.String("dataset", "", "optional labeled SuiteCase JSON dataset")
	mode := flag.String("mode", "baseline", "all, baseline, or contract")
	out := flag.String("out", "compare-results", "new output directory; existing paths are rejected")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "same model for both frameworks")
	maxCalls := flag.Int("max-calls", 2, "upstream requests per task, including retries")
	maxTokens := flag.Int("max-output-tokens", 512, "output tokens per request")
	tokenStop := flag.Int("token-stop", 8000, "stop subsequent requests after observed total tokens")
	timeout := flag.Duration("timeout", 45*time.Second, "per-task timeout")
	flag.Parse()
	if *count < 1 || *timeout <= 0 || *maxCalls < 1 || *maxTokens < 1 || *tokenStop < 0 {
		return fmt.Errorf("invalid task count, timeout or budget")
	}
	cases := bench.GenerateSuite(*seed, *count)
	if *dataset != "" {
		data, err := os.ReadFile(*dataset)
		if err != nil {
			return err
		}
		if cases, err = bench.DecodeSuite(data); err != nil {
			return err
		}
	}
	if err := bench.ValidateSuite(cases); err != nil {
		return err
	}
	jobs, err := bench.SelectSuiteJobs(bench.SuiteJobs(cases, 1), *mode)
	if err != nil {
		return err
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	cfg := bench.Config{APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: baseURL, Timeout: *timeout, Budget: evaluation.Budget{Model: *model, MaxCalls: *maxCalls, MaxOutputTokens: *maxTokens, ObservedTokens: *tokenStop, Temperature: 0.2}}
	if *live && (cfg.APIKey == "" || cfg.Budget.Model == "") {
		return fmt.Errorf("-live requires OPENAI_API_KEY and OPENAI_MODEL or -model")
	}
	if err := os.Mkdir(*out, 0700); err != nil {
		return err
	}
	write := func(name string, v any) error {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(*out, name), append(data, '\n'), 0600)
	}
	data, err := json.Marshal(cases)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if err = write("dataset.json", cases); err != nil {
		return err
	}
	if err = write("manifest.json", map[string]any{"dataset_sha256": hex.EncodeToString(digest[:]), "seed": *seed, "mode": *mode, "live": *live, "config": cfg, "eino": "v0.9.22", "eino_openai": "v0.1.13", "execution": "sequential; framework order alternates per task", "scope": "synthetic/provided task labels; not production acceptance or pure framework overhead"}); err != nil {
		return err
	}
	if !*live {
		fmt.Printf("Prepared %d tasks (%d paired jobs); no model requests.\n", len(cases), len(jobs))
		return nil
	}
	file, err := os.OpenFile(filepath.Join(*out, "results.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var results []bench.SuiteOutcome
	failed := 0
	for i, job := range jobs {
		order := []string{"goagent", "eino"}
		if i%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, framework := range order {
			if ctx.Err() != nil {
				if err = write("summary.json", bench.SummarizeSuite(results)); err != nil {
					return err
				}
				return ctx.Err()
			}
			cfg.DirectTools = job.Direct
			result := einocompare.Run(ctx, framework, job.Case.Case, cfg)
			scored := bench.ScoreSuiteJob(job, result)
			if !scored.ContractPassed {
				failed++
			}
			results = append(results, scored)
			if err = json.NewEncoder(file).Encode(scored); err != nil {
				return err
			}
			if err = file.Sync(); err != nil {
				return err
			}
		}
	}
	if err = write("summary.json", bench.SummarizeSuite(results)); err != nil {
		return err
	}
	fmt.Printf("Completed %d runs; %d failed answer/call contracts.\n", len(results), failed)
	if failed > 0 {
		return fmt.Errorf("%d failed runs retained in results.jsonl", failed)
	}
	return nil
}
