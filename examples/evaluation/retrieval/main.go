// Run from the repository root. -live makes billable calls using OPENAI_*.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/evaluation"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/retrieval"
)

type metered struct {
	llm.Client
	calls int
	usage interfaces.TokenUsage
}

func (m *metered) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	m.calls++
	r, e := m.Client.Chat(ctx, messages)
	if r != nil {
		m.usage.Add(r.Usage)
	}
	return r, e
}
func (m *metered) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.calls++
	r, err := m.Client.Complete(ctx, req)
	if r != nil {
		m.usage.Add(r.Usage)
	}
	return r, err
}

func main() {
	dataset := flag.String("dataset", "evaluation/testdata/support_synthetic.json", "labeled JSON dataset")
	live := flag.Bool("live", false, "call the configured model for reranking and grounded answers")
	k := flag.Int("k", 2, "retrieval cutoff")
	flag.Parse()
	data, err := os.ReadFile(*dataset)
	if err != nil {
		log.Fatal(err)
	}
	var fixture struct {
		Synthetic bool                   `json:"synthetic"`
		Documents []*interfaces.Document `json:"documents"`
		Cases     []struct {
			Query    string   `json:"query"`
			Relevant []string `json:"relevant"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		log.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		log.Fatal("dataset has no cases")
	}
	var model *metered
	if *live {
		client, e := providers.NewOpenAIWithOptions()
		if e != nil {
			log.Fatal(e)
		}
		model = &metered{Client: client}
	}
	base := retrieval.NewKeywordRetriever(fixture.Documents, retrieval.RetrieverConfig{TopK: 6})
	var retriever interfaces.DocumentRetriever = base
	if model != nil {
		retriever = retrieval.NewRerankingRetriever(base, retrieval.NewLLMRerankerWithClient(model, *k), 6, retrieval.RetrieverConfig{TopK: *k})
	}
	rows := []map[string]interface{}{}
	recall, mrr := 0.0, 0.0
	for _, test := range fixture.Cases {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		started := time.Now()
		docs, e := retriever.GetRelevantDocuments(ctx, test.Query)
		if e != nil {
			cancel()
			log.Fatal(e)
		}
		ids := []string{}
		for _, d := range docs {
			ids = append(ids, d.ID)
		}
		score, e := evaluation.ScoreRetrieval(ids, test.Relevant, *k)
		if e != nil {
			cancel()
			log.Fatal(e)
		}
		row := map[string]interface{}{"query": test.Query, "retrieval": score}
		if model != nil {
			// Reuse the scored candidates so the answer does not pay for a second rerank.
			fixed := retrieval.NewFuncRetriever(func(context.Context, string) ([]*interfaces.Document, error) { return docs, nil })
			agent, e := builder.NewSimpleBuilder(model).WithSystemPrompt("Answer the user question concisely in the user language, using only the supplied evidence.").ConfigureForRAG().WithRetriever(fixed, builder.RetrievalConfig{MaxDocuments: *k}).Build()
			if e != nil {
				cancel()
				log.Fatal(e)
			}
			output, e := agent.Execute(ctx, test.Query)
			if e != nil {
				cancel()
				log.Fatal(e)
			}
			sources := output.Metadata["retrieval_sources"].([]builder.RetrievalSource)
			sourceIDs := []string{}
			for _, s := range sources {
				sourceIDs = append(sourceIDs, s.ID)
			}
			citations, e := evaluation.ScoreCitations(fmt.Sprint(output.Result), sourceIDs, test.Relevant)
			if e != nil {
				cancel()
				log.Fatal(e)
			}
			row["answer"] = output.Result
			row["citations"] = citations
		}
		row["duration_ms"] = time.Since(started).Milliseconds()
		cancel()
		rows = append(rows, row)
		recall += score.RecallAtK
		mrr += score.ReciprocalRank
	}
	report := map[string]interface{}{"synthetic": fixture.Synthetic, "live": *live, "k": *k, "cases": rows, "mean_recall_at_k": recall / float64(len(rows)), "mrr": mrr / float64(len(rows)), "citation_scope": "reference validity and labeled source precision/recall; not claim entailment"}
	if model != nil {
		report["model_calls"] = model.calls
		report["usage"] = model.usage
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		log.Fatal(err)
	}
}
