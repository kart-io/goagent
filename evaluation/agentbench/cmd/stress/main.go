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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/evaluation"
	bench "github.com/kart-io/goagent/evaluation/agentbench"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	live := flag.Bool("live", false, "真实模型评测；默认仅生成题集")
	count := flag.Int("count", 120, "合成任务数，12 的倍数可保持类别均衡")
	seed := flag.Int64("seed", 20261026, "固定随机种子")
	dataset := flag.String("dataset", "", "可选脱敏 JSON 题集，提供后不使用合成数据")
	repeats := flag.Int("repeats", 1, "每题每配置重复次数")
	workers := flag.Int("workers", 4, "最大并发任务数")
	mode := flag.String("mode", "all", "all 全部对照；baseline 默认循环；contract 按预先声明的任务契约选择直返/推理")
	out := flag.String("out", "stress-results", "新建结果目录，不覆盖已有目录")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "模型名称")
	timeout := flag.Duration("timeout", 45*time.Second, "单任务超时")
	flag.Parse()
	if *count < 1 || *repeats < 1 || *workers < 1 || *workers > 16 || *timeout <= 0 {
		return fmt.Errorf("任务数、重复数、超时必须为正，并发需在 1 到 16 之间")
	}
	if *live && (*model == "" || os.Getenv("OPENAI_API_KEY") == "") {
		return fmt.Errorf("真实评测需要模型与 OPENAI_API_KEY")
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
	jobs, err := bench.SelectSuiteJobs(bench.SuiteJobs(cases, *repeats), *mode)
	if err != nil {
		return err
	}
	endpoint := os.Getenv("OPENAI_BASE_URL")
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	cfg := bench.Config{APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: endpoint, Timeout: *timeout, Budget: evaluation.Budget{Model: *model, MaxCalls: 2, MaxOutputTokens: 512, ObservedTokens: 8000, Temperature: 0.2}}
	if err := os.Mkdir(*out, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "dataset.json"), data, 0644); err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	revision, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	status, statusErr := exec.Command("git", "status", "--porcelain").Output()
	manifest := map[string]any{"seed": *seed, "cases": len(cases), "planned_runs": len(jobs), "maximum_upstream_requests": len(jobs) * cfg.Budget.MaxCalls, "repeats": *repeats, "workers": *workers, "live": *live, "config": cfg, "dataset_sha256": hex.EncodeToString(digest[:]), "goagent_revision": strings.TrimSpace(string(revision)), "goagent_dirty": statusErr != nil || len(status) > 0, "timestamp": time.Now().UTC(), "limitations": "固定模板合成数据，样本相关；无业务生产数据；默认与直返只在适用类别配对；失败不重试筛选；未知用量单列；并发延迟受负载影响，不做速度排名；最大请求数不等于金额上限"}
	manifest["mode"] = *mode
	manifest["prompt_version"] = "task-semantics-v2"
	manifest["goagent_final_turn"] = "omit_tools_after_results"
	manifest["tool_result_format"] = "native_text"
	manifest["generated"] = *dataset == ""
	if *dataset != "" {
		manifest["limitations"] = "外部题集需调用方自行确认脱敏、代表性和标签质量；失败保留；未知用量单列；并发延迟不可直接用于速度排名"
	}
	if err = writeJSON(filepath.Join(*out, "manifest.json"), manifest); err != nil {
		return err
	}
	if !*live {
		fmt.Printf("已生成 %d 题，真实评测将运行 %d 个任务。\n", len(cases), len(jobs))
		return nil
	}
	file, err := os.Create(filepath.Join(*out, "results.jsonl"))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	queue := make(chan bench.SuiteJob)
	completed := make(chan bench.SuiteOutcome)
	var wg sync.WaitGroup
	for worker := 0; worker < *workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				config := cfg
				config.DirectTools = job.Direct
				result := bench.Run(ctx, job.Case.Case, config)
				completed <- bench.ScoreSuiteJob(job, result)
			}
		}()
	}
	go func() {
		defer close(queue)
		for _, job := range jobs {
			select {
			case <-ctx.Done():
				return
			case queue <- job:
			}
		}
	}()
	go func() { wg.Wait(); close(completed) }()
	encoder := json.NewEncoder(file)
	var results []bench.SuiteOutcome
	var writeErr error
	failed := 0
	for result := range completed {
		results = append(results, result)
		if !result.ContractPassed {
			failed++
		}
		if err = encoder.Encode(result); err != nil && writeErr == nil {
			writeErr = err
			cancel()
		}
		if len(results)%10 == 0 {
			fmt.Fprintf(os.Stderr, "已完成 %d/%d，答案或调用契约失败 %d\n", len(results), len(jobs), failed)
		}
	}
	if writeErr != nil {
		return writeErr
	}
	if err = file.Sync(); err != nil {
		return err
	}
	summary := map[string]any{"planned": len(jobs), "completed": len(results), "contract_failures": failed, "complete": len(results) == len(jobs), "groups": bench.SummarizeSuite(results)}
	if err = writeJSON(filepath.Join(*out, "summary.json"), summary); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "完成 %d/%d，结果保存在 %s\n", len(results), len(jobs), *out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if failed > 0 {
		return fmt.Errorf("%d 个任务未满足答案或调用契约，完整失败已保留", failed)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
