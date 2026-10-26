package interfaces

import "context"

// DocumentRetriever retrieves evidence without requiring an execution framework.
// Existing retrieval.Retriever implementations satisfy this interface.
type DocumentRetriever interface {
	GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}
