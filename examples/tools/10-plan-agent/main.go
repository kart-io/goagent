// Package main 是循环扩展点的端到端案例：用真实模型跑一次带计划模式、Claude Code 钩子、压缩与运行中交互的编码任务，
// 并逐项核对结果，任一项不通过即以非零状态退出。
//
// 运行：DEEPSEEK_API_KEY=... go run ./examples/tools/10-plan-agent
//
// 案例：
//  1. 计划模式：Agent 先调查再提交计划，计划获批前的写入被拒绝，获批后才修改代码。
//  2. 钩子：仓库里的 .claude/settings.json 定义 UserPromptSubmit、Stop、SessionStart 钩子。文件属于工作区内容，
//     首次 Build 返回 *TrustRequiredError，宿主授权后重新 Build。
//  3. 运行中交互：第一次写入代码后用 Steer 追加要求；Followup 在第一轮结束后开始第二轮，续接对话。
//  4. 压缩：阈值设得很低，对话变长后较早的消息被压缩为摘要。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/harness/preset"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/coding"
)

const systemPrompt = `你是一个严谨的编码 Agent，在给定的 Go 仓库中工作。用 read_file、grep、glob 阅读代码，用 write_file、edit_file 修改，
用 run_command 运行 go test 验证。最后用几句话汇报结果。`

const (
	task     = "给 calc 包增加 Sub(a, b int) int 函数，并补充测试。"
	followup = "再增加 Mul(a, b int) int 函数和测试，完成后运行 go test ./... 确认通过。"
	steer    = "补充要求：Sub 的注释里写明它返回 a 减 b 的差。"
)

const hostConfig = `presets:
  - id: planner
    name: 先计划后执行的编码助手
    components:
      - {id: persona, use: persona, config: {prompt: %q}}
      - {id: workspace, use: workspace, config: {sandbox: danger-full-access, executor: local, network: true, commandTimeout: 2m}}
      - {id: coding, use: coding-tools}
      - {id: permissions, use: permissions, config: {base: coding}}
      - {id: plan, use: plan-mode}
      - {id: hooks, use: claude-code-hooks, config: {path: .claude/settings.json}}
      - {id: compact, use: compaction, config: {maxTokens: 3000, keepTokens: 800}}
`

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("请设置 DEEPSEEK_API_KEY 环境变量")
		return
	}
	if err := run(apiKey); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}

// record 汇总运行中观察到的事件，供最后核对。
type record struct {
	mu            sync.Mutex
	planApproved  bool
	plan          string
	deniedInPlan  int
	writeBeforeOK int
	writes        int
	summaries     int
	handle        *builder.Handle
	steered       bool
}

func (r *record) onResult(_ context.Context, res tools.DispatchResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg, rejected := tools.RejectedResult(res.Output)
	status := "✓"
	if rejected || !res.Output.Success {
		status = "✗ " + firstLine(msg)
	}
	fmt.Printf("  → %s %s\n", res.Name, status)
	if rejected && strings.Contains(msg, "plan mode is active") {
		r.deniedInPlan++
	}
	if res.Name == preset.ToolExitPlanMode && res.Output.Success {
		r.planApproved = true
	}
	if isWrite(res.Name) && res.Output.Success {
		r.writes++
		if !r.planApproved {
			r.writeBeforeOK++
		}
		// 第一次写入后追加要求：Steer 在本批工具结束后作为 user 消息进入下一步。
		if !r.steered && r.handle != nil {
			r.steered = true
			fmt.Printf("  [Steer] %s\n", steer)
			if err := r.handle.Steer(steer); err != nil {
				return err
			}
		}
	}
	return nil
}

func isWrite(name string) bool {
	return name == coding.ToolWriteFile || name == coding.ToolEditFile || name == coding.ToolMultiEdit
}

func (r *record) approve(_ context.Context, req tools.ApprovalRequest) (tools.ApprovalResult, error) {
	if req.Call.Tool == preset.ToolExitPlanMode {
		plan, _ := req.Call.Args["plan"].(string)
		r.mu.Lock()
		r.plan = plan
		r.mu.Unlock()
		fmt.Printf("  [审批] 提交的计划：\n%s\n  → 批准\n", indent(plan))
	} else {
		fmt.Printf("  [审批] %s %v → 批准\n", req.Call.Tool, req.Call.Args)
	}
	return tools.ApprovalResult{Outcome: tools.ApprovalAllowedOnce}, nil
}

// summarizer 记录压缩次数。
type summarizer struct {
	llm.Client
	r *record
}

func (s summarizer) Chat(ctx context.Context, msgs []llm.Message) (*llm.CompletionResponse, error) {
	resp, err := s.Client.Chat(ctx, msgs)
	if err == nil {
		s.r.mu.Lock()
		s.r.summaries++
		s.r.mu.Unlock()
		fmt.Printf("  [压缩] %d 字节的历史 → 摘要 %d 字节\n", len(msgs[1].Content), len(resp.Content))
	}
	return resp, err
}

func run(apiKey string) error {
	root, err := os.MkdirTemp("", "plan-agent-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	// 钩子日志与溢出文件放在工作区之外，不进入 Agent 的视野。
	logs, err := os.MkdirTemp("", "plan-agent-logs-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(logs) }()
	if err := seedRepo(root, logs); err != nil {
		return err
	}

	set, err := preset.Load(preset.Source{Layer: preset.LayerHost, Path: "examples/tools/10-plan-agent", Trusted: true,
		Data: fmt.Appendf(nil, hostConfig, systemPrompt)})
	if err != nil {
		return err
	}
	planner, _ := set.Get("planner")
	client, err := providers.NewDeepSeekWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0), llm.WithTimeout(2*time.Minute))
	if err != nil {
		return err
	}
	rec := &record{}
	trust := &preset.MemoryTrustStore{}
	host := preset.Host{
		Root: root, ArtifactDir: filepath.Join(logs, "artifacts"), Trust: trust,
		Approver:        tools.ApproverFunc(rec.approve),
		CompactionModel: summarizer{Client: client, r: rec},
		Dispatch:        tools.DispatchConfig{OnResult: rec.onResult},
	}

	// 钩子文件中的命令在宿主机执行，需要授权；实际宿主应把扩权项展示给用户确认。
	_, err = preset.Build(context.Background(), planner, host)
	var trustErr *preset.TrustRequiredError
	if !errors.As(err, &trustErr) {
		return fmt.Errorf("期望钩子文件需要授权，得到 %v", err)
	}
	fmt.Println("需要授权的扩权项：")
	for _, item := range trustErr.Items {
		fmt.Printf("  - %s：%s\n", item.Component, firstLine(item.Reason))
	}
	if err := trust.Grant(trustErr.Root, trustErr.Digest); err != nil {
		return err
	}
	asm, err := preset.Build(context.Background(), planner, host)
	if err != nil {
		return err
	}
	defer func() { _ = asm.Close() }()

	agent, err := preset.Configure(builder.NewSimpleBuilder(client), asm).
		WithSessionID("plan-demo").
		WithMaxIterations(30).
		Build()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	fmt.Printf("\n第一轮：%s\n", task)
	h := agent.Start(ctx, task)
	rec.mu.Lock()
	rec.handle = h
	rec.mu.Unlock()
	// Followup 排在第一轮结束之后，第二轮续接第一轮的对话。
	if err := h.Followup(followup); err != nil {
		return err
	}
	out, err := h.Wait(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("\n最终答复：\n%s\n\n", out.Result)
	return verify(root, logs, rec, fmt.Sprint(out.Result))
}

func verify(root, logs string, rec *record, answer string) error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	calc, _ := os.ReadFile(filepath.Join(root, "calc.go"))
	prompts, _ := os.ReadFile(filepath.Join(logs, "prompts"))
	starts, _ := os.ReadFile(filepath.Join(logs, "starts"))
	test := exec.Command("go", "test", "./...")
	test.Dir = root
	testOut, testErr := test.CombinedOutput()

	checks := []struct {
		name string
		ok   bool
		info string
	}{
		{"计划模式：提交的计划获批", rec.planApproved && strings.TrimSpace(rec.plan) != "", fmt.Sprintf("计划 %d 字", len([]rune(rec.plan)))},
		{"计划模式：获批前没有成功的写入", rec.writeBeforeOK == 0, fmt.Sprintf("获批前写入 %d 次，被拦截 %d 次", rec.writeBeforeOK, rec.deniedInPlan)},
		{"获批后修改了代码", rec.writes > 0, fmt.Sprintf("写入 %d 次", rec.writes)},
		{"Steer：追加的要求被执行", rec.steered && strings.Contains(string(calc), "减"), "calc.go 中 Sub 的注释提到「减」"},
		{"Followup：第二轮完成", strings.Contains(string(calc), "func Mul("), "calc.go 中有 Mul"},
		{"go test ./... 通过", testErr == nil, firstLine(string(testOut))},
		{"UserPromptSubmit：轮次输入、Steer 与 Followup 都经过钩子", strings.Count(string(prompts), "\n---\n") >= 3, fmt.Sprintf("记录 %d 条输入", strings.Count(string(prompts), "\n---\n"))},
		{"SessionStart：同一会话只执行一次", string(starts) == "x\n", fmt.Sprintf("执行 %d 次", strings.Count(string(starts), "x"))},
		{"Stop：答复按钩子要求写了验证结果", strings.Contains(answer, "验证结果"), ""},
		{"压缩：较早的历史被写成摘要", rec.summaries > 0, fmt.Sprintf("压缩 %d 次", rec.summaries)},
	}
	failed := 0
	fmt.Println("案例核对：")
	for _, c := range checks {
		mark := "✓"
		if !c.ok {
			mark = "✗"
			failed++
		}
		fmt.Printf("  %s %s  %s\n", mark, c.name, c.info)
	}
	if failed > 0 {
		return fmt.Errorf("%d 项未通过", failed)
	}
	fmt.Println("全部通过")
	return nil
}

// seedRepo 创建示例仓库：calc 包与 Claude Code 风格的钩子配置。钩子把收到的输入与会话启动记入 logs 目录。
func seedRepo(root, logs string) error {
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		return err
	}
	settings := fmt.Sprintf(`{"hooks": {
  "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "printf '%%s\\n---\\n' \"$(cat)\" >> %[1]s/prompts; echo '（项目约定：新增函数须有中文注释，测试使用表驱动）'"}]}],
  "Stop": [{"hooks": [{"type": "command", "command": "input=$(cat); case \"$input\" in *'\"stop_hook_active\":true'*|*验证结果*) exit 0;; esac; echo '答复最后须单独写一行「验证结果：」并给出 go test 的结论' >&2; exit 2"}]}],
  "SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "echo x >> %[1]s/starts; echo '本仓库是 calc 示例，模块名 example.com/calc，只使用标准库。'"}]}]
}}`, logs)
	files := map[string]string{
		"go.mod":                "module example.com/calc\n\ngo 1.21\n",
		".claude/settings.json": settings,
		"calc.go":               "package calc\n\n// Add 返回 a 与 b 的和。\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"calc_test.go": `package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d", got)
	}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n    ")
}
