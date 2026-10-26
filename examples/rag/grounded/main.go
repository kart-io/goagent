// Run with OPENAI_API_KEY set. OPENAI_BASE_URL and OPENAI_MODEL are optional.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/retrieval"
)

func main() {
	client, err := providers.NewOpenAIWithOptions()
	if err != nil {
		log.Fatal(err)
	}
	docs := []*interfaces.Document{
		{ID: "returns", PageContent: "The refund policy allows returns within 30 days of purchase.", Metadata: map[string]interface{}{"source": "support-handbook/returns"}},
		{ID: "shipping", PageContent: "Standard shipping takes three to five business days.", Metadata: map[string]interface{}{"source": "support-handbook/shipping"}},
	}
	retriever := retrieval.NewKeywordRetriever(docs, retrieval.RetrieverConfig{TopK: 2})
	agent, err := builder.NewSimpleBuilder(client).
		WithSystemPrompt("Answer support questions using the reference material.").
		ConfigureForRAG().
		WithRetriever(retriever, builder.RetrievalConfig{MaxDocuments: 2, MaxContextRunes: 2000}).
		Build()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := agent.Execute(ctx, "What is the refund policy?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(output.Result)
	for _, source := range output.Metadata["retrieval_sources"].([]builder.RetrievalSource) {
		fmt.Printf("[%d] %s (id=%s, score=%.3f)\n", source.Reference, source.Source, source.ID, source.Score)
	}
}
