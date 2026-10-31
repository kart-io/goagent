// Package executionbench 提供不调用模型、不产生外部工具副作用的离线开销探针。
package executionbench

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Config 选择预算模式和固定操作数。TempDir仅供file模式指定临时子目录的父目录。
type Config struct {
	Mode       string `json:"mode"`
	Operations int    `json:"operations"`
	TempDir    string `json:"tempDir,omitempty"`
}

// Report 中内存与goroutine为进程时点采样，不能作为无泄漏证明。
// 所有时间单位均为纳秒；合成usage不代表真实模型调用、效果或费用。
type Report struct {
	Config           Config                `json:"config"`
	Operations       int                   `json:"operations"`
	Successes        int                   `json:"successes"`
	TotalDurationNS  int64                 `json:"totalDurationNS"`
	P50NS            int64                 `json:"p50NS"`
	P95NS            int64                 `json:"p95NS"`
	TotalAllocBytes  uint64                `json:"totalAllocBytes"`
	LiveHeapBytes    uint64                `json:"liveHeapBytes"`
	GoroutineDelta   int                   `json:"goroutineDelta"`
	FileCount        int                   `json:"fileCount"`
	FileBytes        int64                 `json:"fileBytes"`
	BudgetSnapshot   *budget.Snapshot      `json:"budgetSnapshot,omitempty"`
	SyntheticUsage   interfaces.TokenUsage `json:"syntheticUsagePerOperation"`
	MeasurementScope string                `json:"measurementScope"`
}

// Run 顺序执行固定数量的无副作用工具操作。计时及TotalAlloc仅覆盖操作循环，
// 排除初始化、最终统计和清理；file模式仅持久化预算，不开启工具日志。
// Run只接收未绑定Journal的context，保留其取消、期限和其他值。
// Run只清理自己创建的临时子目录，调用者拥有父目录。
func Run(ctx context.Context, cfg Config) (report Report, err error) {
	report.Config = cfg
	report.SyntheticUsage = interfaces.TokenUsage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}
	report.MeasurementScope = "操作循环：合成usage预留、Dispatcher工具调用及usage记账；不含初始化、统计和清理；无模型请求；TotalAlloc为循环期间进程级差值，GC后堆和goroutine为进程采样，不证明无泄漏"
	if ctx == nil {
		return report, fmt.Errorf("context不能为空")
	}
	if cfg.Mode != "none" && cfg.Mode != "memory" && cfg.Mode != "file" {
		return report, fmt.Errorf("mode必须是none、memory或file")
	}
	if cfg.Operations <= 0 {
		return report, fmt.Errorf("operations必须为正数")
	}
	if cfg.TempDir != "" && cfg.Mode != "file" {
		return report, fmt.Errorf("tempDir仅适用于file模式")
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if durable.JournalStore(ctx) != nil {
		return report, fmt.Errorf("离线探针不接受已绑定Journal的context")
	}
	var controller *budget.Controller
	var dir string
	bc := budget.Config{RootKey: "executionbench", Version: "synthetic-v1", MaxModelRequests: cfg.Operations, MaxToolCalls: cfg.Operations}
	switch cfg.Mode {
	case "memory":
		controller, err = budget.New(bc)
	case "file":
		dir, err = os.MkdirTemp(cfg.TempDir, "goagent-executionbench-*")
		if err != nil {
			return report, err
		}
		defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
		var store *durable.FileStore
		store, err = durable.NewFileStore(dir)
		if err == nil {
			controller, err = budget.NewDurable(bc, store)
		}
	}
	if err != nil {
		return report, err
	}
	ctx, cancel := budget.Bind(ctx, controller)
	defer cancel()
	reg := tools.NewRegistry()
	err = reg.Register(tools.NewBaseTool("noop", "离线无副作用工具", `{"type":"object","additionalProperties":false}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	}))
	if err != nil {
		return report, err
	}
	dispatcher, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: 1})
	if err != nil {
		return report, err
	}
	durations := make([]int64, 0, cfg.Operations)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutines := runtime.NumGoroutine()
	start := time.Now()
	for i := 0; i < cfg.Operations; i++ {
		opStart := time.Now()
		if err = ctx.Err(); err != nil {
			break
		}
		report.Operations++
		key := fmt.Sprintf("operation-%d", i)
		if controller != nil {
			if err = controller.Reserve(ctx, key); err != nil {
				break
			}
		}
		var out *interfaces.ToolOutput
		out, err = dispatcher.Invoke(ctx, tools.ToolRequest{Name: "noop", Input: &interfaces.ToolInput{CallID: key, Args: map[string]any{}}})
		if err != nil {
			break
		}
		if out == nil || !out.Success {
			err = fmt.Errorf("操作%s未成功", key)
			break
		}
		if controller != nil {
			if err = controller.RecordUsage(ctx, key, &report.SyntheticUsage); err != nil {
				break
			}
		}
		durations = append(durations, time.Since(opStart).Nanoseconds())
		report.Successes++
	}
	report.TotalDurationNS = time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	report.TotalAllocBytes = after.TotalAlloc - before.TotalAlloc
	runtime.GC()
	runtime.ReadMemStats(&after)
	report.LiveHeapBytes = after.HeapAlloc
	runtime.KeepAlive(controller)
	runtime.KeepAlive(ctx)
	report.GoroutineDelta = runtime.NumGoroutine() - goroutines
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	if len(durations) > 0 {
		report.P50NS = durations[int(math.Ceil(float64(len(durations))*.5))-1]
		report.P95NS = durations[int(math.Ceil(float64(len(durations))*.95))-1]
	}
	if err != nil {
		return report, err
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if controller != nil {
		var snapshot budget.Snapshot
		snapshot, err = controller.Snapshot(ctx)
		if err != nil {
			return report, err
		}
		report.BudgetSnapshot = &snapshot
	}
	if dir != "" {
		err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			report.FileCount++
			report.FileBytes += info.Size()
			return nil
		})
	}
	return report, err
}
