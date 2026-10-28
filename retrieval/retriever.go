package retrieval

import (
	"context"
	"reflect"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Retriever 定义检索器接口
//
// 借鉴 LangChain 的 Retriever 设计，提供统一的文档检索接口
// 继承自 Runnable[string, []*interfaces.Document]，支持管道操作和回调
type Retriever interface {
	core.Runnable[string, []*interfaces.Document]

	// GetRelevantDocuments 检索相关文档
	//
	// 参数:
	//   - ctx: 上下文
	//   - query: 查询字符串
	//
	// 返回:
	//   - []*interfaces.Document: 相关文档列表
	//   - error: 错误信息
	GetRelevantDocuments(ctx context.Context, query string) ([]*interfaces.Document, error)
}

// BaseRetriever 提供 Retriever 的基础实现
//
// 实现了 Runnable 接口的通用功能
// Concrete constructors bind their retrieval function explicitly. Go embedding
// does not dispatch calls to an embedding type's methods. Custom implementations
// should use AsRetriever or NewFuncRetriever to obtain the Runnable contract.
type BaseRetriever struct {
	*core.BaseRunnable[string, []*interfaces.Document]

	// TopK 返回的最大文档数
	TopK int

	// MinScore 最小分数阈值（过滤低分文档）
	MinScore float64

	// Name 检索器名称（用于日志和追踪）
	Name     string
	retrieve func(context.Context, string) ([]*interfaces.Document, error)
}

// NewFuncRetriever adapts a retrieval function to all Runnable entry points.
func NewFuncRetriever(fn func(context.Context, string) ([]*interfaces.Document, error)) *BaseRetriever {
	r := NewBaseRetriever()
	r.retrieve = fn
	return r
}

// AsRetriever adapts a document retriever without relying on embedded dispatch.
func AsRetriever(source interfaces.DocumentRetriever) Retriever {
	if source == nil || (reflect.ValueOf(source).Kind() == reflect.Pointer && reflect.ValueOf(source).IsNil()) {
		return NewBaseRetriever()
	}
	return NewFuncRetriever(source.GetRelevantDocuments)
}

// NewBaseRetriever 创建基础检索器
func NewBaseRetriever() *BaseRetriever {
	return &BaseRetriever{
		BaseRunnable: core.NewBaseRunnable[string, []*interfaces.Document](),
		TopK:         4,
		MinScore:     0.0,
		Name:         "base_retriever",
	}
}

// Invoke 执行检索（实现 Runnable 接口）
func (r *BaseRetriever) Invoke(ctx context.Context, query string) ([]*interfaces.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 触发回调
	callbacks := r.GetConfig().Callbacks
	if _, err := core.NotifyCallbacks(ctx, "retriever", "on_start", callbacks, func(cb core.Callback) error {
		return cb.OnStart(ctx, query)
	}); err != nil {
		return nil, err
	}

	// 执行检索（由子类实现）
	docs, err := r.GetRelevantDocuments(ctx, query)
	if err == nil {
		err = ctx.Err()
	}

	// 触发回调
	if err != nil {
		if panicked, panicErr := core.NotifyCallbacks(ctx, "retriever", "on_error", callbacks, func(cb core.Callback) error {
			_ = cb.OnError(ctx, err)
			return nil
		}); panicked {
			return nil, panicErr
		}
		return docs, err
	}
	if _, callbackErr := core.NotifyCallbacks(ctx, "retriever", "on_end", callbacks, func(cb core.Callback) error {
		return cb.OnEnd(ctx, docs)
	}); callbackErr != nil {
		return nil, callbackErr
	}

	return docs, nil
}

// Stream 流式执行（默认实现）
func (r *BaseRetriever) Stream(ctx context.Context, query string) (<-chan core.StreamChunk[[]*interfaces.Document], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	outChan := make(chan core.StreamChunk[[]*interfaces.Document], 1)

	go func() {
		defer close(outChan)

		docs, err := r.Invoke(ctx, query)
		outChan <- core.StreamChunk[[]*interfaces.Document]{
			Data:  docs,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Batch 批量执行
func (r *BaseRetriever) Batch(ctx context.Context, queries []string) ([][]*interfaces.Document, error) {
	return r.BaseRunnable.Batch(ctx, queries, r.Invoke)
}

// Pipe 连接到另一个 Runnable
func (r *BaseRetriever) Pipe(next core.Runnable[[]*interfaces.Document, any]) core.Runnable[string, any] {
	return core.NewRunnablePipe[string, []*interfaces.Document, any](r, next)
}

// WithCallbacks 添加回调
func (r *BaseRetriever) WithCallbacks(callbacks ...core.Callback) core.Runnable[string, []*interfaces.Document] {
	newRetriever := *r
	newRetriever.BaseRunnable = r.BaseRunnable.WithCallbacks(callbacks...)
	return &newRetriever
}

// WithConfig 配置 Retriever
func (r *BaseRetriever) WithConfig(config core.RunnableConfig) core.Runnable[string, []*interfaces.Document] {
	newRetriever := *r
	newRetriever.BaseRunnable = r.BaseRunnable.WithConfig(config)
	return &newRetriever
}

// GetRelevantDocuments invokes the bound implementation. An unbound base is a
// configuration error, not a successful search with no matching documents.
func (r *BaseRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*interfaces.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.retrieve == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "retriever has no retrieval function; use a concrete constructor or AsRetriever")
	}
	return r.retrieve(ctx, query)
}

// FilterByScore 按分数过滤文档
func (r *BaseRetriever) FilterByScore(docs []*interfaces.Document) []*interfaces.Document {
	if r.MinScore <= 0.0 {
		return docs
	}

	filtered := make([]*interfaces.Document, 0)
	for _, doc := range docs {
		if doc.Score >= r.MinScore {
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

// LimitTopK 限制返回的文档数量
func (r *BaseRetriever) LimitTopK(docs []*interfaces.Document) []*interfaces.Document {
	if r.TopK <= 0 || len(docs) <= r.TopK {
		return docs
	}

	// 按分数排序
	collection := DocumentCollection(docs)
	collection.SortByScore()

	return collection[:r.TopK]
}

// RetrieverConfig 检索器配置
type RetrieverConfig struct {
	TopK     int     // 返回的最大文档数
	MinScore float64 // 最小分数阈值
	Name     string  // 检索器名称
}

// DefaultRetrieverConfig 返回默认配置
func DefaultRetrieverConfig() RetrieverConfig {
	return RetrieverConfig{
		TopK:     4,
		MinScore: 0.0,
		Name:     "retriever",
	}
}
