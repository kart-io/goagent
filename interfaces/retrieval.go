package interfaces

import "context"

// DocumentRetriever retrieves evidence without requiring an execution framework.
// Existing retrieval.Retriever implementations satisfy this interface.
type DocumentRetriever interface {
	GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}

// RetrievalRequest 描述一次检索。TopK 为 0 时使用检索器自身的默认值；
// Filter 的每个键都须与文档元数据中同名字段相等（多个键同时满足），nil 表示不过滤。
type RetrievalRequest struct {
	Query  string
	TopK   int
	Filter map[string]any
}

// RequestRetriever 按请求检索，用于按租户、权限等元数据过滤。
// 实现无法执行某个过滤条件时必须返回错误，不得忽略条件返回未过滤的结果。
type RequestRetriever interface {
	Retrieve(ctx context.Context, req RetrievalRequest) ([]*Document, error)
}
