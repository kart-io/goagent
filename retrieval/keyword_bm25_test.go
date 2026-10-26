package retrieval

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

func TestBM25RetainsMatchesInSmallCorpora(t *testing.T) {
	tests := []struct {
		name      string
		documents []string
		query     string
		want      int
	}{
		{"single document", []string{"Refund policy allows returns within 30 days."}, "refund policy", 1},
		{"half the documents", []string{"Refund policy allows returns within 30 days.", "Shipping takes five business days."}, "What is the refund policy?", 1},
		{"term in every document", []string{"Refund policy allows returns.", "Refund requests need a receipt."}, "refund", 2},
		{"unmatched query", []string{"Refund policy allows returns."}, "shipping", 0},
		{"empty document", []string{""}, "refund", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := make([]*interfaces.Document, len(tt.documents))
			for i, content := range tt.documents {
				docs[i] = &interfaces.Document{PageContent: content}
			}
			r := NewKeywordRetriever(docs, RetrieverConfig{TopK: 4})
			results, err := r.GetRelevantDocuments(context.Background(), tt.query)
			require.NoError(t, err)
			require.Len(t, results, tt.want)
			for _, doc := range results {
				require.Greater(t, doc.Score, 0.0)
				require.False(t, math.IsNaN(doc.Score) || math.IsInf(doc.Score, 0))
			}
		})
	}
}
