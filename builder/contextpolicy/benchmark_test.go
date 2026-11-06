package contextpolicy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kart-io/goagent/llm"
)

// BenchmarkLargeToolResult 测量首次卸载和最终还原，夹具在计时外创建；输出必须完整往返。
func BenchmarkLargeToolResult(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		for _, operation := range []string{"卸载", "还原"} {
			b.Run(fmt.Sprintf("%d/%s", size, operation), func(b *testing.B) {
				ctx := WithResultScope(context.Background(), "benchmark")
				text := strings.Repeat("x", size)
				messages := []llm.Message{toolRound("a", "lookup"), {Role: "tool", ToolCallID: "a", Content: text}}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					p, err := NewOffload(OffloadConfig{Store: NewMemoryStore(), MaxToolResultRunes: 1024})
					if err != nil {
						b.Fatal(err)
					}
					input := messages
					if operation == "还原" {
						input, err = p.Reduce(ctx, messages, false)
						if err != nil {
							b.Fatal(err)
						}
					}
					b.StartTimer()
					out, err := p.Reduce(ctx, input, operation == "还原")
					b.StopTimer()
					if err != nil {
						b.Fatal(err)
					}
					if operation == "还原" {
						if out[1].Content != text {
							b.Fatal("原文未完整恢复")
						}
					} else if len(out[1].Content) >= len(text) {
						b.Fatal("结果未卸载")
					}
					b.StartTimer()
				}
			})
		}
	}
}
