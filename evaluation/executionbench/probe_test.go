package executionbench_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/durable"

	"github.com/kart-io/goagent/evaluation/executionbench"
)

func TestInvalidConfiguration(t *testing.T) {
	for _, cfg := range []executionbench.Config{{}, {Mode: "other", Operations: 1}, {Mode: "none", Operations: -1}, {Mode: "none", Operations: 1, TempDir: t.TempDir()}} {
		if _, err := executionbench.Run(context.Background(), cfg); err == nil {
			t.Fatalf("配置应失败: %+v", cfg)
		}
	}
	if _, err := executionbench.Run(nil, executionbench.Config{Mode: "none", Operations: 1}); err == nil {
		t.Fatal("nil ctx应失败")
	}
}

func TestCancelledDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := executionbench.Run(ctx, executionbench.Config{Mode: "file", Operations: 3, TempDir: dir})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("残留文件: %v %v", entries, err)
	}
}

func TestModesCountsBudgetJSONAndCleanup(t *testing.T) {
	for _, mode := range []string{"none", "memory", "file"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			sentinel := filepath.Join(dir, "keep")
			if err := os.WriteFile(sentinel, []byte("用户文件"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := executionbench.Config{Mode: mode, Operations: 3}
			if mode == "file" {
				cfg.TempDir = dir
			}
			report, err := executionbench.Run(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if report.Operations != 3 || report.Successes != 3 || report.TotalDurationNS <= 0 || report.P50NS <= 0 || report.P95NS < report.P50NS {
				t.Fatalf("无效报告: %+v", report)
			}
			if mode == "none" {
				if report.BudgetSnapshot != nil {
					t.Fatal("无预算模式出现快照")
				}
			} else {
				snap := report.BudgetSnapshot
				if snap == nil || snap.ReservedRequests != 3 || snap.KnownUsageEntries != 3 || snap.ReservedToolCalls != 3 || snap.KnownTokens.TotalTokens != 9 {
					t.Fatalf("无效预算: %+v", snap)
				}
			}
			if mode == "file" {
				if report.FileCount < 1 || report.FileBytes <= 0 {
					t.Fatal("未计数持久文件")
				}
			} else if report.FileCount != 0 || report.FileBytes != 0 {
				t.Fatal("非文件模式产生文件统计")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
				t.Fatalf("临时资源清理错误: %v %v", entries, err)
			}
			raw, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			var decoded executionbench.Report
			if err = json.Unmarshal(raw, &decoded); err != nil || decoded.Config.Mode != mode || decoded.Successes != 3 {
				t.Fatalf("JSON往返失败: %s %v", raw, err)
			}
		})
	}
}

// 无论有没有执行键，宿主日志都不应进入独立预算测量。
func TestRejectHostJournalBeforeInitialization(t *testing.T) {
	for _, mode := range []string{"none", "memory", "file"} {
		for _, binding := range []string{"有执行键", "无执行键", "typed-nil"} {
			t.Run(mode+"/"+binding, func(t *testing.T) {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("应拒绝日志而不是panic: %v", recovered)
					}
				}()
				hostDir := t.TempDir()
				store, err := durable.NewFileStore(hostDir)
				if err != nil {
					t.Fatal(err)
				}
				var bound durable.Store = store
				if binding == "typed-nil" {
					var nilStore *durable.FileStore
					bound = nilStore
				}
				ctx := durable.WithJournal(context.Background(), bound)
				if binding != "无执行键" {
					ctx = durable.WithExecutionKey(ctx, "host-run")
				}
				cfg := executionbench.Config{Mode: mode, Operations: 1}
				// 不存在的父目录还可验证拒绝发生在MkdirTemp之前。
				missingParent := filepath.Join(t.TempDir(), "must-not-create")
				if mode == "file" {
					cfg.TempDir = missingParent
				}
				report, err := executionbench.Run(ctx, cfg)
				if err == nil || !strings.Contains(err.Error(), "不接受已绑定Journal") {
					t.Errorf("应在初始化前拒绝Journal: %v", err)
				}
				if report.Operations != 0 || report.Successes != 0 {
					t.Errorf("拒绝前已执行操作: %+v", report)
				}
				entries, readErr := os.ReadDir(hostDir)
				if readErr != nil || len(entries) != 0 {
					t.Errorf("写入了宿主Store: %v %v", entries, readErr)
				}
				if _, statErr := os.Stat(missingParent); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("创建了临时父目录: %v", statErr)
				}
			})
		}
	}
}
