// Package evaluation provides transparent metrics for labeled, offline datasets.
package evaluation

import (
	"fmt"
	"regexp"
	"strconv"
)

// RetrievalMetrics uses unique retrieved IDs; irrelevant and duplicate results do
// not increase recall. Empty relevance labels are invalid, not perfect scores.
type RetrievalMetrics struct {
	RecallAtK      float64 `json:"recall_at_k"`
	ReciprocalRank float64 `json:"reciprocal_rank"`
}

func ScoreRetrieval(retrieved, relevant []string, k int) (RetrievalMetrics, error) {
	if k <= 0 || len(relevant) == 0 {
		return RetrievalMetrics{}, fmt.Errorf("positive k and relevance labels required")
	}
	labels := map[string]bool{}
	for _, id := range relevant {
		labels[id] = true
	}
	seen := map[string]bool{}
	hits := 0
	result := RetrievalMetrics{}
	for i, id := range retrieved {
		if i >= k {
			break
		}
		if labels[id] && !seen[id] {
			hits++
			if result.ReciprocalRank == 0 {
				result.ReciprocalRank = 1 / float64(i+1)
			}
		}
		seen[id] = true
	}
	result.RecallAtK = float64(hits) / float64(len(labels))
	return result, nil
}

// CitationMetrics measures reference validity and labeled source precision/recall.
// It does not measure whether prose is entailed by a source; that requires claim
// annotations or human review. Missing citations score zero, not perfect precision.
type CitationMetrics struct {
	Validity        float64 `json:"reference_validity"`
	SourcePrecision float64 `json:"source_precision"`
	SourceRecall    float64 `json:"source_recall"`
}

var citationPattern = regexp.MustCompile(`\[(\d+)\]`)

func ScoreCitations(answer string, sources, relevant []string) (CitationMetrics, error) {
	if len(relevant) == 0 {
		return CitationMetrics{}, fmt.Errorf("citation relevance labels required")
	}
	labels := map[string]bool{}
	for _, id := range relevant {
		labels[id] = true
	}
	refs := map[int]bool{}
	hits := map[string]bool{}
	valid, correct := 0, 0
	for _, match := range citationPattern.FindAllStringSubmatch(answer, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			n = 0
		}
		if refs[n] {
			continue
		}
		refs[n] = true
		if n > 0 && n <= len(sources) {
			valid++
			if labels[sources[n-1]] {
				correct++
				hits[sources[n-1]] = true
			}
		}
	}
	result := CitationMetrics{SourceRecall: float64(len(hits)) / float64(len(labels))}
	if len(refs) > 0 {
		result.Validity = float64(valid) / float64(len(refs))
		result.SourcePrecision = float64(correct) / float64(len(refs))
	}
	return result, nil
}
