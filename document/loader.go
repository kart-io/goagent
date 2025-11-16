package document

import (
	"context"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

// DocumentLoader 文档加载器接口
//
// 负责从各种来源加载文档,支持单独加载或加载后分割
type DocumentLoader interface {
	// Load 加载文档
	Load(ctx context.Context) ([]*interfaces.Document, error)

	// LoadAndSplit 加载并分割文档
	LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error)

	// GetMetadata 获取加载器元数据
	GetMetadata() map[string]interface{}
}

// BaseDocumentLoader 基础文档加载器
//
// 提供默认的 LoadAndSplit 实现
type BaseDocumentLoader struct {
	metadata        map[string]interface{}
	callbackManager *core.CallbackManager
}

// NewBaseDocumentLoader 创建基础加载器
func NewBaseDocumentLoader(metadata map[string]interface{}, callbacks *core.CallbackManager) *BaseDocumentLoader {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	return &BaseDocumentLoader{
		metadata:        metadata,
		callbackManager: callbacks,
	}
}

// GetMetadata 获取元数据
func (l *BaseDocumentLoader) GetMetadata() map[string]interface{} {
	return l.metadata
}

// LoadAndSplit 默认实现:先加载后分割
func (l *BaseDocumentLoader) LoadAndSplit(
	ctx context.Context,
	loader DocumentLoader,
	splitter TextSplitter,
) ([]*interfaces.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"operation": "load_and_split",
		}); err != nil {
			return nil, err
		}
	}

	// 加载文档
	docs, err := loader.Load(ctx)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 分割文档
	if splitter != nil {
		docs, err = splitter.SplitDocuments(docs)
		if err != nil {
			if l.callbackManager != nil {
				_ = l.callbackManager.OnError(ctx, err)
			}
			return nil, err
		}
	}

	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_docs": len(docs),
		}); err != nil {
			return nil, err
		}
	}

	return docs, nil
}

// GetCallbackManager 获取回调管理器
func (l *BaseDocumentLoader) GetCallbackManager() *core.CallbackManager {
	return l.callbackManager
}
