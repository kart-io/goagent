package document

import (
	"context"
	"os"
	"path/filepath"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
	"github.com/kart-io/k8s-agent/common/errors"
)

// TextLoader 文本文件加载器
//
// 加载纯文本文件,支持各种编码
type TextLoader struct {
	*BaseDocumentLoader
	filePath string
	encoding string
}

// TextLoaderConfig 文本加载器配置
type TextLoaderConfig struct {
	FilePath        string
	Encoding        string
	Metadata        map[string]interface{}
	CallbackManager *core.CallbackManager
}

// NewTextLoader 创建文本加载器
func NewTextLoader(config TextLoaderConfig) *TextLoader {
	if config.Encoding == "" {
		config.Encoding = "utf-8"
	}

	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	// 添加文件元数据
	config.Metadata["source"] = config.FilePath
	config.Metadata["encoding"] = config.Encoding

	return &TextLoader{
		BaseDocumentLoader: NewBaseDocumentLoader(config.Metadata, config.CallbackManager),
		filePath:           config.FilePath,
		encoding:           config.Encoding,
	}
}

// Load 加载文本文件
func (l *TextLoader) Load(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader":    "text",
			"file_path": l.filePath,
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
		return nil, errors.Wrap(errors.CodeInternalError, "failed to read file", err)
	}

	// 创建文档
	metadata := l.GetMetadata()
	metadata["file_size"] = len(content)

	doc := retrieval.NewDocument(string(content), metadata)

	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_docs":  1,
			"file_size": len(content),
		}); err != nil {
			return nil, err
		}
	}

	return []*interfaces.Document{doc}, nil
}

// LoadAndSplit 加载并分割
func (l *TextLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error) {
	return l.BaseDocumentLoader.LoadAndSplit(ctx, l, splitter)
}

// DirectoryLoader 目录加载器
//
// 批量加载目录中的文件
type DirectoryLoader struct {
	*BaseDocumentLoader
	dirPath   string
	glob      string
	recursive bool
	loader    func(string) DocumentLoader
}

// DirectoryLoaderConfig 目录加载器配置
type DirectoryLoaderConfig struct {
	DirPath         string
	Glob            string
	Recursive       bool
	Loader          func(string) DocumentLoader
	Metadata        map[string]interface{}
	CallbackManager *core.CallbackManager
}

// NewDirectoryLoader 创建目录加载器
func NewDirectoryLoader(config DirectoryLoaderConfig) *DirectoryLoader {
	if config.Glob == "" {
		config.Glob = "*"
	}

	if config.Loader == nil {
		// 默认使用文本加载器
		config.Loader = func(path string) DocumentLoader {
			return NewTextLoader(TextLoaderConfig{
				FilePath: path,
			})
		}
	}

	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	config.Metadata["source_dir"] = config.DirPath
	config.Metadata["glob_pattern"] = config.Glob

	return &DirectoryLoader{
		BaseDocumentLoader: NewBaseDocumentLoader(config.Metadata, config.CallbackManager),
		dirPath:            config.DirPath,
		glob:               config.Glob,
		recursive:          config.Recursive,
		loader:             config.Loader,
	}
}

// Load 加载目录中的所有文件
func (l *DirectoryLoader) Load(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader":    "directory",
			"dir_path":  l.dirPath,
			"recursive": l.recursive,
		}); err != nil {
			return nil, err
		}
	}

	// 查找匹配的文件
	var files []string
	var err error

	if l.recursive {
		err = filepath.Walk(l.dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				matched, err := filepath.Match(l.glob, filepath.Base(path))
				if err != nil {
					return err
				}
				if matched {
					files = append(files, path)
				}
			}
			return nil
		})
	} else {
		pattern := filepath.Join(l.dirPath, l.glob)
		files, err = filepath.Glob(pattern)
	}

	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to list files", err)
	}

	// 加载每个文件
	var allDocs []*interfaces.Document

	for _, file := range files {
		loader := l.loader(file)
		docs, err := loader.Load(ctx)
		if err != nil {
			// 跳过错误文件,继续处理
			continue
		}

		allDocs = append(allDocs, docs...)
	}

	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_files": len(files),
			"num_docs":  len(allDocs),
		}); err != nil {
			return nil, err
		}
	}

	return allDocs, nil
}

// LoadAndSplit 加载并分割
func (l *DirectoryLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error) {
	return l.BaseDocumentLoader.LoadAndSplit(ctx, l, splitter)
}
