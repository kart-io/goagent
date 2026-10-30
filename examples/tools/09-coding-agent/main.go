// Package main 演示用 tools/coding 搭建编码 Agent：在临时 Go 仓库里运行测试、定位并修复 bug、提交改动。
//
// 运行：DEEPSEEK_API_KEY=... go run ./examples/tools/09-coding-agent
// 设置 CODING_DOCKER_IMAGE=golang:1.25 时命令在断网容器中执行。
// 权限策略放行读写文件，run_command 与 git_commit 经 tools.Pipeline 的审批人确认；示例自动批准并打印，
// 实际宿主可在审批人里弹出命令行确认或发送 IM 卡片。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/coding"
)

const systemPrompt = `你是一个严谨的编码 Agent，在给定的 Go 仓库中工作。
多步任务先用 todo_write 列出步骤并随进度更新。先用 run_command 运行测试了解现状，用 read_file、grep、glob 阅读代码，用 edit_file 做最小改动修复问题。
改完再次运行测试确认通过，然后用 git_commit 提交，提交信息说明修复原因。最后用一两句话汇报结果。`

const task = "go test ./... 当前失败，请修复 bug 让测试通过并提交。"

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

func run(apiKey string) error {
	root, err := os.MkdirTemp("", "coding-agent-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := seedRepo(root); err != nil {
		return err
	}

	opts := []coding.Option{
		coding.WithCommandTimeout(2 * time.Minute),
		// 编辑 Go 文件后运行 gofmt 检查，格式不对的编辑被撤销并把输出交给模型。
		coding.WithLinter(func(path string) []string {
			if filepath.Ext(path) != ".go" {
				return nil
			}
			return []string{"sh", "-c", `out=$(gofmt -l "$0") && [ -z "$out" ] || { gofmt -d "$0"; exit 1; }`, path}
		}),
	}
	if image := os.Getenv("CODING_DOCKER_IMAGE"); image != "" {
		opts = append(opts, coding.WithExecutor(env.DockerExecutor{Root: root, Image: image}))
	}
	ws, err := coding.NewWorkspace(root, opts...)
	if err != nil {
		return err
	}

	registry := tools.NewRegistry()
	for _, tool := range ws.Tools() {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	// 管线：默认策略放行读写、命令与提交需审批；示例的审批人自动选择"本会话允许"并打印授权范围。
	pipeline := &tools.Pipeline{
		Pre: []tools.PreHook{coding.DefaultPolicy().Hook()},
		Approver: tools.ApproverFunc(func(_ context.Context, req tools.ApprovalRequest) (tools.ApprovalResult, error) {
			scope := "仅本次"
			if req.Grant != nil {
				scope = req.Grant.Describe()
			}
			fmt.Printf("  [审批] %s %v → 自动批准（%s）\n", req.Call.Tool, req.Call.Args, scope)
			return tools.ApprovalResult{Outcome: tools.ApprovalAllowedSession}, nil
		}),
		Post: []tools.PostHook{coding.SpillHook(ws, 100_000)},
	}
	dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
		// 参数错误交还模型改正，而不是中止整个任务。
		ReturnArgumentErrors: true,
		Pipeline:             pipeline,
		OnResult: func(_ context.Context, r tools.DispatchResult) error {
			fmt.Printf("→ %s %v\n", r.Name, r.Args)
			if msg, ok := tools.RejectedResult(r.Output); ok {
				fmt.Printf("  ✗ %s\n", msg)
			}
			if r.Name == coding.ToolTodoWrite {
				for _, todo := range ws.Todos() {
					fmt.Printf("  [%s] %s\n", todo.Status, todo.Content)
				}
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	client, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0),
		llm.WithTimeout(2*time.Minute),
	)
	if err != nil {
		return err
	}
	instructions, err := ws.ProjectInstructions(context.Background())
	if err != nil {
		return err
	}
	agent, err := builder.NewSimpleBuilder(client).
		WithSessionID("coding-demo").
		WithSystemPrompt(systemPrompt + "\n\n项目说明：\n" + instructions).
		WithTools(dispatcher.Tools()...).
		WithMaxIterations(30).
		Build()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := agent.AsRunnable().Invoke(ctx, task)
	if err != nil {
		return err
	}
	fmt.Printf("\nAgent: %v\n\n", out.Result)
	// 宿主可据此展示改动，或在用户不认可时用 ws.Revert(ctx) 回滚。
	for _, c := range ws.Changes() {
		fmt.Printf("改动文件: %s (新建: %v)\n", c.Path, c.Created)
	}

	// 独立于 Agent 的汇报核对结果。
	test := exec.Command("go", "test", "./...")
	test.Dir = root
	if output, err := test.CombinedOutput(); err != nil {
		return fmt.Errorf("测试仍失败: %v\n%s", err, output)
	}
	log, err := exec.Command("git", "-C", root, "log", "--oneline").Output()
	if err != nil {
		return err
	}
	fmt.Printf("测试通过，提交记录:\n%s", log)
	return nil
}

// seedRepo 创建一个 Sum 漏加最后一个元素的小仓库。
func seedRepo(root string) error {
	files := map[string]string{
		"go.mod":    "module example.com/calc\n\ngo 1.21\n",
		"AGENTS.md": "提交信息使用中文，格式为 `fix: 原因`。\n",
		"calc.go": `package calc

// Sum 返回所有元素之和。
func Sum(nums []int) int {
	total := 0
	for i := 0; i < len(nums)-1; i++ {
		total += nums[i]
	}
	return total
}
`,
		"calc_test.go": `package calc

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]int{1, 2, 3}); got != 6 {
		t.Fatalf("Sum = %d, want 6", got)
	}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "coding-agent"},
		{"config", "user.email", "coding-agent@example.com"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", args, err, out)
		}
	}
	return nil
}
