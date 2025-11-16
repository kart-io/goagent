package retrieval

import (
	"context"

	"github.com/kart-io/goagent/core"
)

// Retriever 定义检索器接口
//
// 借鉴 LangChain 的 Retriever 设计，提供统一的文档检索接口
// 继承自 Runnable[string, []*Document]，支持管道操作和回调
type Retriever interface {
	core.Runnable[string, []*Document]

	// GetRelevantDocuments 检索相关文档
	//
	// 参数:
	//   - ctx: 上下文
	//   - query: 查询字符串
	//
	// 返回:
	//   - []*Document: 相关文档列表
	//   - error: 错误信息
	GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}

// BaseRetriever 提供 Retriever 的基础实现
//
// 实现了 Runnable 接口的通用功能
// 子类只需实现 GetRelevantDocuments 方法
type BaseRetriever struct {
	*core.BaseRunnable[string, []*Document]

	// TopK 返回的最大文档数
	TopK int

	// MinScore 最小分数阈值（过滤低分文档）
	MinScore float64

	// Name 检索器名称（用于日志和追踪）
	Name string
}

// NewBaseRetriever 创建基础检索器
func NewBaseRetriever() *BaseRetriever {
	return &BaseRetriever{
		BaseRunnable: core.NewBaseRunnable[string, []*Document](),
		TopK:         4,
		MinScore:     0.0,
		Name:         "base_retriever",
	}
}

// Invoke 执行检索（实现 Runnable 接口）
func (r *BaseRetriever) Invoke(ctx context.Context, query string) ([]*Document, error) {
	// 触发回调
	config := r.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, query); err != nil {
			return nil, err
		}
	}

	// 执行检索（由子类实现）
	docs, err := r.GetRelevantDocuments(ctx, query)

	// 触发回调
	for _, cb := range config.Callbacks {
		if err != nil {
			_ = cb.OnError(ctx, err)
		} else {
			_ = cb.OnEnd(ctx, docs)
		}
	}

	return docs, err
}

// Stream 流式执行（默认实现）
func (r *BaseRetriever) Stream(ctx context.Context, query string) (<-chan core.StreamChunk[[]*Document], error) {
	outChan := make(chan core.StreamChunk[[]*Document], 1)

	go func() {
		defer close(outChan)

		docs, err := r.Invoke(ctx, query)
		outChan <- core.StreamChunk[[]*Document]{
			Data:  docs,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Batch 批量执行
func (r *BaseRetriever) Batch(ctx context.Context, queries []string) ([][]*Document, error) {
	return r.BaseRunnable.Batch(ctx, queries, r.Invoke)
}

// Pipe 连接到另一个 Runnable
func (r *BaseRetriever) Pipe(next core.Runnable[[]*Document, any]) core.Runnable[string, any] {
	return core.NewRunnablePipe[string, []*Document, any](r, next)
}

// WithCallbacks 添加回调
func (r *BaseRetriever) WithCallbacks(callbacks ...core.Callback) core.Runnable[string, []*Document] {
	newRetriever := *r
	newRetriever.BaseRunnable = r.BaseRunnable.WithCallbacks(callbacks...)
	return &newRetriever
}

// WithConfig 配置 Retriever
func (r *BaseRetriever) WithConfig(config core.RunnableConfig) core.Runnable[string, []*Document] {
	newRetriever := *r
	newRetriever.BaseRunnable = r.BaseRunnable.WithConfig(config)
	return &newRetriever
}

// GetRelevantDocuments 基础实现（返回空列表）
//
// 子类应该重写此方法
func (r *BaseRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	return []*Document{}, nil
}

// FilterByScore 按分数过滤文档
func (r *BaseRetriever) FilterByScore(docs []*Document) []*Document {
	if r.MinScore <= 0.0 {
		return docs
	}

	filtered := make([]*Document, 0)
	for _, doc := range docs {
		if doc.Score >= r.MinScore {
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

// LimitTopK 限制返回的文档数量
func (r *BaseRetriever) LimitTopK(docs []*Document) []*Document {
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
