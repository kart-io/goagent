package interfaces

import "context"

// DocumentRetriever retrieves evidence without requiring an execution framework.
// Existing retrieval.Retriever implementations satisfy this interface.
type DocumentRetriever interface {
	GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}

// RetrievalRequest describes a single retrieval. When TopK is 0 the retriever's
// own default is used. Every key in Filter must equal the field of the same name
// in the document metadata (all keys must match); nil means no filtering.
type RetrievalRequest struct {
	Query  string
	TopK   int
	Filter map[string]any
}

// RequestRetriever retrieves by request, for filtering on metadata such as
// tenant and permissions. When an implementation cannot apply a filter
// condition it must return an error; it must not ignore the condition and
// return unfiltered results.
type RequestRetriever interface {
	Retrieve(ctx context.Context, req RetrievalRequest) ([]*Document, error)
}
