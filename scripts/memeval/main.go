// memeval 在 LongMemEval 或 LOCOMO 上对比"只有最近原文"与长期记忆的各个配置，输出 Markdown 汇总与逐题 JSONL。
// 需要真实模型：抽取、作答与判定各调用一次以上，长期记忆方案每个会话还有若干次整理调用，先用较小的 -sample 估算费用。
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/memory/memeval"
	"github.com/kart-io/goagent/store"
)

// clients 是可用的提供商；API Key 与地址从各自的环境变量读取（如 DEEPSEEK_API_KEY、SILICONFLOW_API_KEY）。
var clients = map[string]func(...llm.ClientOption) (llm.Client, error){
	"deepseek":    func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewDeepSeekWithOptions(o...) },
	"openai":      func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewOpenAIWithOptions(o...) },
	"siliconflow": func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewSiliconFlowWithOptions(o...) },
	"kimi":        func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewKimiWithOptions(o...) },
	"ollama":      func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewOllamaWithOptions(o...) },
	"gemini":      func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewGeminiWithOptions(o...) },
}

var loaders = map[string]func(*os.File) ([]memeval.Case, error){
	"longmemeval": func(f *os.File) ([]memeval.Case, error) { return memeval.LoadLongMemEval(f) },
	"locomo":      func(f *os.File) ([]memeval.Case, error) { return memeval.LoadLoCoMo(f) },
}

// options 是一次评测的命令行参数。
type options struct {
	format, data                 string
	sample, repeats, concurrency int
	chat, judge, embed, systems  string
	recentRunes, maxTokens       int
	out, results                 string
}

func main() {
	var o options
	flag.StringVar(&o.format, "format", "longmemeval", "数据集格式：longmemeval 或 locomo")
	flag.StringVar(&o.data, "data", "", "数据文件，如 longmemeval_oracle.json 或 locomo10.json")
	flag.IntVar(&o.sample, "sample", 20, "按题型分层抽取的题目数，0 表示全部")
	flag.IntVar(&o.repeats, "repeats", 1, "重复次数；每轮重新抽取记忆，用于估计波动")
	flag.IntVar(&o.concurrency, "concurrency", 4, "同时处理的（轮次、方案、对话）组合数")
	flag.StringVar(&o.chat, "chat", "deepseek", "抽取与作答模型，格式为 提供商[:模型]")
	flag.StringVar(&o.judge, "judge", "", "判定模型，格式同 -chat；为空时与 -chat 相同（官方用 gpt-4o-2024-08-06）")
	flag.StringVar(&o.embed, "embed", "", "嵌入模型，格式为 提供商:模型，如 gemini:gemini-embedding-001；为空时只按文本重合召回")
	flag.StringVar(&o.systems, "systems", "recent,memory,graph,community", "参与对比的方案，逗号分隔：recent、memory、graph、community")
	flag.IntVar(&o.recentRunes, "recent-runes", 0, "recent 方案的上下文字符数，0 表示与召回块同为默认预算")
	flag.IntVar(&o.maxTokens, "max-tokens", 8000, "抽取与作答模型的最大输出 token；长会话的抽取结果（含关系）会超过提供商的默认值而被截断")
	flag.StringVar(&o.out, "out", "", "Markdown 汇总写入的文件，为空时打印")
	flag.StringVar(&o.results, "results", "", "逐题结果写入的 JSONL 文件")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, o); err != nil {
		fmt.Fprintln(os.Stderr, "memeval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	format, data, chatSpec, judgeSpec, embedSpec := o.format, o.data, o.chat, o.judge, o.embed
	load, ok := loaders[format]
	if !ok || data == "" {
		return fmt.Errorf("需要 -data 与受支持的 -format（longmemeval、locomo），当前为 %q", format)
	}
	f, err := os.Open(data)
	if err != nil {
		return err
	}
	cases, err := load(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	cases = memeval.Sample(cases, o.sample)
	model, err := newClient(chatSpec, llm.WithMaxTokens(o.maxTokens))
	if err != nil {
		return err
	}
	judgeModel := model
	if judgeSpec != "" {
		if judgeModel, err = newClient(judgeSpec); err != nil {
			return err
		}
	}
	var embedder llm.EmbeddingClient
	embedModel := ""
	if embedSpec != "" {
		provider, name, _ := strings.Cut(embedSpec, ":")
		c, err := newClient(provider, llm.WithEmbeddingModel(name))
		if err != nil {
			return err
		}
		if embedder, ok = c.(llm.EmbeddingClient); !ok {
			return fmt.Errorf("提供商 %s 不支持嵌入", provider)
		}
		embedModel = name
	}
	dir, err := os.MkdirTemp("", "memeval-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	configure := func(graph, communities bool) func(store.Store, session.Store) memory.Config {
		return func(st store.Store, sessions session.Store) memory.Config {
			cfg := memory.Config{Store: st, Sessions: sessions, Model: model, Graph: graph, Communities: communities}
			if embedder != nil {
				cfg.Index, cfg.Embedder, cfg.EmbedModel = memory.NewMemoryIndex(), embedder, embedModel
			}
			return cfg
		}
	}
	available := map[string]memeval.System{
		"recent":    memeval.Recent{Runes: o.recentRunes},
		"memory":    &memeval.Memory{Label: "memory", Configure: configure(false, false), Dir: dir},
		"graph":     &memeval.Memory{Label: "graph", Configure: configure(true, false), Dir: dir},
		"community": &memeval.Memory{Label: "community", Configure: configure(true, true), Dir: dir},
	}
	var selected []memeval.System
	for _, name := range strings.Split(o.systems, ",") {
		sys, ok := available[strings.TrimSpace(name)]
		if !ok {
			return fmt.Errorf("未知方案 %q", name)
		}
		selected = append(selected, sys)
	}
	runner := memeval.Runner{Systems: selected, Answerer: model, Judge: judgeModel, Repeats: o.repeats, Concurrency: o.concurrency, Progress: func(r memeval.Result) {
		fmt.Fprintf(os.Stderr, "run=%d %s %s correct=%v evidence=%v\n", r.Run, r.System, r.QuestionID, r.Correct, r.EvidenceHit)
	}}
	res, runErr := runner.Run(ctx, cases)
	if o.results != "" {
		var sb strings.Builder
		for _, r := range res {
			// json.Encode 自带换行。
			if err := json.Encode(&sb, r); err != nil {
				return err
			}
		}
		if err := os.WriteFile(o.results, []byte(sb.String()), 0o644); err != nil {
			return err
		}
	}
	questions := 0
	for _, c := range cases {
		questions += len(c.Questions)
	}
	report := fmt.Sprintf("数据集：%s（%s），%d 段对话、%d 题，重复 %d 轮；模型 %s，判定 %s，嵌入 %s\n\n%s", format, filepath.Base(data), len(cases), questions,
		max(o.repeats, 1), chatSpec, cmp.Or(judgeSpec, chatSpec), cmp.Or(embedSpec, "无"), memeval.Report(res))
	if runErr != nil {
		report += "\n失败的组合（其题目缺少结果，两两比较只用双方都有结果的题）：\n\n```text\n" + runErr.Error() + "\n```\n"
	}
	if o.out == "" {
		fmt.Print(report)
	} else if err := os.WriteFile(o.out, []byte(report), 0o644); err != nil {
		return err
	}
	return runErr
}

// newClient 按 "提供商[:模型]" 创建温度为 0 的客户端；opts 追加在后。
func newClient(spec string, opts ...llm.ClientOption) (llm.Client, error) {
	provider, model, _ := strings.Cut(spec, ":")
	build, ok := clients[provider]
	if !ok {
		names := make([]string, 0, len(clients))
		for name := range clients {
			names = append(names, name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("未知提供商 %q，可选：%s", provider, strings.Join(names, "、"))
	}
	base := []llm.ClientOption{llm.WithTemperature(0)}
	if model != "" {
		base = append(base, llm.WithModel(model))
	}
	return build(append(base, opts...)...)
}
