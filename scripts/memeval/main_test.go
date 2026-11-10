package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(empty, []byte("[]"), 0o600))
	tests := []struct {
		name, format, data, chat, embed, systems string
		want                                     string
	}{
		{"未知格式", "squad", empty, "deepseek", "", "recent", "受支持的 -format"},
		{"缺少数据文件", "longmemeval", "", "deepseek", "", "recent", "需要 -data"},
		{"未知提供商", "longmemeval", empty, "nope", "", "recent", "未知提供商"},
		{"未知方案", "longmemeval", empty, "ollama", "", "recent,unknown", "未知方案"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(ctx, options{format: tt.format, data: tt.data, chat: tt.chat, embed: tt.embed, systems: tt.systems})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// 没有题目时不请求模型，只输出表头。
func TestRunWritesReport(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.json")
	require.NoError(t, os.WriteFile(data, []byte("[]"), 0o600))
	out, results := filepath.Join(dir, "report.md"), filepath.Join(dir, "results.jsonl")
	require.NoError(t, run(context.Background(), options{format: "locomo", data: data, chat: "ollama:qwen", systems: "recent,memory", out: out, results: results}))
	report, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(report), "| 方案 | 题型 |")
	require.FileExists(t, results)
}
