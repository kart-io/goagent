package budget_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/interfaces"
)

// BenchmarkRecordUsageFixedLedger只更新既有key，ledger规模不随b.N增长。
func BenchmarkRecordUsageFixedLedger(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("entries_%d", size), func(b *testing.B) {
			ctx := context.Background()
			c, err := budget.New(budget.Config{RootKey: "benchmark", Version: "v1", MaxModelRequests: size})
			if err != nil {
				b.Fatal(err)
			}
			usage := &interfaces.TokenUsage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("operation-%d", i)
				if err := c.Reserve(ctx, key); err != nil {
					b.Fatal(err)
				}
				if err := c.RecordUsage(ctx, key, usage); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.RecordUsage(ctx, "operation-0", usage); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
