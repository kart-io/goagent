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
	"strings"
	"time"

	"github.com/kart-io/goagent/evaluation"
	bench "github.com/kart-io/goagent/evaluation/agentbench"
)

func main() {
	live := flag.Bool("live", false, "启用真实模型调用")
	direct := flag.Bool("direct-tools", false, "启用工具直接返回，仅用于终态工具题集")
	dataset := flag.String("dataset", "testdata/cases.json", "题集路径")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "使用的模型")
	repeats := flag.Int("repeats", 1, "每题重复次数")
	calls := flag.Int("max-calls", 4, "每题最大模型调用数")
	tokens := flag.Int("max-output-tokens", 256, "单次最大输出 Token")
	stop := flag.Int("token-stop", 8000, "已观测 Token 达到阈值后停止后续调用")
	timeout := flag.Duration("timeout", 60*time.Second, "单题超时")
	flag.Parse()
	if !*live {
		log.Fatal("真实评测须显式传入 -live；离线协议检查使用 go test ./...")
	}
	if *model == "" || os.Getenv("OPENAI_API_KEY") == "" || *repeats < 1 || *calls < 1 || *tokens < 1 || *timeout <= 0 {
		log.Fatal("需要模型、API Key 和正数预算")
	}
	data, err := os.ReadFile(*dataset)
	if err != nil {
		log.Fatal(err)
	}
	var cases []bench.Case
	if err = json.Unmarshal(data, &cases); err != nil {
		log.Fatal(err)
	}
	if len(cases) == 0 {
		log.Fatal("题集不能为空")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if c.ID == "" || seen[c.ID] || c.Question == "" || c.Answer == "" {
			log.Fatal("题目需要唯一 ID、问题和答案")
		}
		seen[c.ID] = true
	}
	endpoint := os.Getenv("OPENAI_BASE_URL")
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	cfg := bench.Config{APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: endpoint, Timeout: *timeout, Budget: evaluation.Budget{Model: *model, MaxCalls: *calls, MaxOutputTokens: *tokens, ObservedTokens: *stop, Temperature: 0.2}}
	cfg.DirectTools = *direct
	var results []bench.Result
	for repeat := 0; repeat < *repeats; repeat++ {
		for _, c := range cases {
			results = append(results, bench.Run(context.Background(), c, cfg))
		}
	}

	digest := sha256.Sum256(data)
	revision, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		revision = []byte("unknown")
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	dirty := err != nil || len(status) > 0
	report := map[string]any{"live": true, "dataset": *dataset, "dataset_sha256": hex.EncodeToString(digest[:]), "goagent_revision": strings.TrimSpace(string(revision)), "goagent_dirty": dirty, "config": cfg, "repeats": *repeats, "timestamp": time.Now().UTC(), "results": results, "scoring": "最终答案去除首尾空白后完全匹配；有证据的题目还必须调用工具；失败样本保留", "limitations": "合成数据；未禁用供应商缓存；Token 阈值不是硬性计费上限；首次文本不是工具首帧；本实验不评价持久化恢复"}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		log.Fatal(fmt.Errorf("输出报告：%w", err))
	}
}
