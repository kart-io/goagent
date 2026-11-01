package budget

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"testing"
)

func BenchmarkAttributionEvidenceFixedLedger(b *testing.B) {
	for _, size := range []int{10, 1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			ctx := context.Background()
			c, e := New(Config{RootKey: "r", Version: "v1", MaxModelRequests: size, AttributionVersion: "v1", MaxAgentCalls: 1})
			if e != nil {
				b.Fatal(e)
			}
			owner := usage.Owner{RunID: "r", AgentID: "root", AgentCallKey: "r"}
			chain := []usage.Owner{owner}
			evidence := usage.Evidence{KnownFields: usage.BasicFields, Tokens: interfaces.TokenUsage{TotalTokens: 3}, Source: "provider", Revision: 1}
			c.memory.AgentCalls = map[string]usage.Owner{"r": owner}
			c.memory.AttemptOwners = map[string]string{}
			c.memory.Evidence = map[string]usage.Evidence{}
			for i := range size {
				key := fmt.Sprint(i)
				u := evidence.Tokens
				c.memory.Entries[key] = &u
				c.memory.AttemptOwners[key] = "r"
				c.memory.Evidence[key] = evidence
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, e = c.RecordEvidence(ctx, "0", chain, evidence); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
