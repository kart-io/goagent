package document

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
	"github.com/kart-io/k8s-agent/common/errors"
)

// JSONLoader JSON 文件加载器
//
// 加载 JSON 文件,支持 JSON Lines 格式
type JSONLoader struct {
	*BaseDocumentLoader
	filePath     string
	jsonLines    bool
	contentKey   string
	metadataKeys []string
}

// JSONLoaderConfig JSON 加载器配置
type JSONLoaderConfig struct {
	FilePath        string
	JSONLines       bool     // 是否为 JSON Lines 格式
	ContentKey      string   // 内容字段的键
	MetadataKeys    []string // 要提取为元数据的键
	Metadata        map[string]interface{}
	CallbackManager *core.CallbackManager
}

// NewJSONLoader 创建 JSON 加载器
func NewJSONLoader(config JSONLoaderConfig) *JSONLoader {
	if config.ContentKey == "" {
		config.ContentKey = "content"
	}

	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	config.Metadata["source"] = config.FilePath
	config.Metadata["source_type"] = "json"

	return &JSONLoader{
		BaseDocumentLoader: NewBaseDocumentLoader(config.Metadata, config.CallbackManager),
		filePath:           config.FilePath,
		jsonLines:          config.JSONLines,
		contentKey:         config.ContentKey,
		metadataKeys:       config.MetadataKeys,
	}
}

// Load 加载 JSON 文件
func (l *JSONLoader) Load(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader":     "json",
			"file_path":  l.filePath,
			"json_lines": l.jsonLines,
		}); err != nil {
			return nil, err
		}
	}

	// 读取文件
	content, err := os.ReadFile(l.filePath)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to read json file", err)
	}

	var docs []*interfaces.Document

	if l.jsonLines {
		docs = l.loadJSONLines(content)
	} else {
		docs, err = l.loadJSON(content)
	}

	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
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

// LoadAndSplit 加载并分割
func (l *JSONLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error) {
	return l.BaseDocumentLoader.LoadAndSplit(ctx, l, splitter)
}

// loadJSON 加载标准 JSON
func (l *JSONLoader) loadJSON(content []byte) ([]*interfaces.Document, error) {
	var data interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, errors.Wrap(errors.CodeInternalError, "failed to parse json", err)
	}

	docs := make([]*interfaces.Document, 0)

	switch v := data.(type) {
	case map[string]interface{}:
		// 单个对象
		doc := l.parseJSONObject(v)
		if doc != nil {
			docs = append(docs, doc)
		}

	case []interface{}:
		// 对象数组
		for _, item := range v {
			if obj, ok := item.(map[string]interface{}); ok {
				doc := l.parseJSONObject(obj)
				if doc != nil {
					docs = append(docs, doc)
				}
			}
		}

	default:
		return nil, errors.New(errors.CodeInternalError, "unsupported json structure")
	}

	return docs, nil
}

// loadJSONLines 加载 JSON Lines
func (l *JSONLoader) loadJSONLines(content []byte) []*interfaces.Document {
	lines := strings.Split(string(content), "\n")
	docs := make([]*interfaces.Document, 0)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			// 跳过无效行
			continue
		}

		doc := l.parseJSONObject(obj)
		if doc != nil {
			doc.SetMetadata("line_number", i+1)
			docs = append(docs, doc)
		}
	}

	return docs
}

// parseJSONObject 解析 JSON 对象为文档
func (l *JSONLoader) parseJSONObject(obj map[string]interface{}) *retrieval.Document {
	// 提取内容
	contentValue, ok := obj[l.contentKey]
	if !ok {
		return nil
	}

	content := fmt.Sprintf("%v", contentValue)

	// 提取元数据
	metadata := make(map[string]interface{})

	// 复制基础元数据
	for k, v := range l.GetMetadata() {
		metadata[k] = v
	}

	// 提取指定的元数据字段
	if len(l.metadataKeys) > 0 {
		for _, key := range l.metadataKeys {
			if value, ok := obj[key]; ok {
				metadata[key] = value
			}
		}
	} else {
		// 提取所有字段(除了内容字段)
		for key, value := range obj {
			if key != l.contentKey {
				metadata[key] = value
			}
		}
	}

	return retrieval.NewDocument(content, metadata)
}
