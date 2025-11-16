# Document Loaders and Splitters

完整的文档加载和分割系统,用于从各种来源加载文档并智能分割成可处理的块。

## 目录

- [概述](#概述)
- [Document Loaders](#document-loaders)
- [Text Splitters](#text-splitters)
- [快速开始](#快速开始)
- [API 参考](#api-参考)
- [最佳实践](#最佳实践)
- [性能优化](#性能优化)

## 概述

Document Loaders 和 Splitters 系统提供了强大的文档处理能力:

- **多格式支持**: 文本、Markdown、JSON、Web 页面
- **智能分割**: 字符、Token、递归、Markdown、代码等多种分割策略
- **元数据保留**: 自动保留和扩展文档元数据
- **Callback 集成**: 完整的回调系统支持监控和调试
- **高性能**: 优化的分割算法,支持大规模文档处理

## Document Loaders

### 支持的加载器

#### 1. TextLoader

加载纯文本文件。

```go
loader := document.NewTextLoader(document.TextLoaderConfig{
    FilePath: "document.txt",
    Encoding: "utf-8",
})

docs, err := loader.Load(context.Background())
```

**特性**:

- 支持多种编码
- 自动元数据提取(文件大小、路径等)
- 高性能文件读取

#### 2. MarkdownLoader

加载 Markdown 文件,支持结构解析。

```go
loader := document.NewMarkdownLoader(document.MarkdownLoaderConfig{
    FilePath:      "README.md",
    RemoveImages:  true,
    RemoveLinks:   false,
    RemoveCodeFmt: false,
})

docs, err := loader.Load(context.Background())
```

**特性**:

- 提取标题作为元数据
- 可选移除图片、链接、代码格式
- 保持 Markdown 结构

#### 3. JSONLoader

加载 JSON 和 JSON Lines 格式。

```go
loader := document.NewJSONLoader(document.JSONLoaderConfig{
    FilePath:     "data.json",
    JSONLines:    false,
    ContentKey:   "content",
    MetadataKeys: []string{"author", "date"},
})

docs, err := loader.Load(context.Background())
```

**特性**:

- 支持标准 JSON 和 JSON Lines
- 灵活的字段映射
- 自动元数据提取

#### 4. DirectoryLoader

批量加载目录中的文件。

```go
loader := document.NewDirectoryLoader(document.DirectoryLoaderConfig{
    DirPath:   "./documents",
    Glob:      "*.txt",
    Recursive: true,
    Loader: func(path string) document.DocumentLoader {
        return document.NewTextLoader(document.TextLoaderConfig{
            FilePath: path,
        })
    },
})

docs, err := loader.Load(context.Background())
```

**特性**:

- 支持 glob 模式匹配
- 递归目录遍历
- 自定义文件加载器

#### 5. WebLoader

从 Web 加载内容。

```go
loader := document.NewWebLoader(document.WebLoaderConfig{
    URL:       "https://example.com",
    StripHTML: true,
    Headers: map[string]string{
        "User-Agent": "MyBot/1.0",
    },
    Timeout: 30 * time.Second,
})

docs, err := loader.Load(context.Background())
```

**特性**:

- HTTP/HTTPS 支持
- 自定义请求头
- HTML 标签移除
- 超时控制

## Text Splitters

### 支持的分割器

#### 1. CharacterTextSplitter

基于字符数的简单分割。

```go
splitter := document.NewCharacterTextSplitter(document.CharacterTextSplitterConfig{
    Separator:    "\n\n",
    ChunkSize:    1000,
    ChunkOverlap: 200,
})

chunks, err := splitter.SplitText(text)
```

**特性**:

- 自定义分隔符
- 块重叠支持
- 简单高效

#### 2. RecursiveCharacterTextSplitter

使用多层分隔符递归分割。

```go
splitter := document.NewRecursiveCharacterTextSplitter(
    document.RecursiveCharacterTextSplitterConfig{
        Separators: []string{"\n\n", "\n", ". ", " "},
        ChunkSize:  1000,
        ChunkOverlap: 200,
    },
)

chunks, err := splitter.SplitText(text)
```

**特性**:

- 智能选择分隔符
- 保持语义完整性
- 适用于自然语言

#### 3. TokenTextSplitter

基于 Token 数量分割。

```go
splitter := document.NewTokenTextSplitter(document.TokenTextSplitterConfig{
    Encoding:     "cl100k_base",
    ChunkSize:    500,
    ChunkOverlap: 50,
})

chunks, err := splitter.SplitText(text)
```

**特性**:

- Token 级别精确控制
- 适合 LLM 输入
- 可配置编码方式

#### 4. MarkdownTextSplitter

按 Markdown 结构分割。

```go
splitter := document.NewMarkdownTextSplitter(document.MarkdownTextSplitterConfig{
    HeadersToSplitOn: []string{"#", "##", "###"},
    ChunkSize:        1000,
    ChunkOverlap:     100,
})

chunks, err := splitter.SplitText(markdown)
```

**特性**:

- 保持 Markdown 结构
- 按标题层级分割
- 保留格式信息

#### 5. CodeTextSplitter

针对代码的智能分割。

```go
splitter := document.NewCodeTextSplitter(document.CodeTextSplitterConfig{
    Language:     document.LanguageGo,
    ChunkSize:    500,
    ChunkOverlap: 50,
})

chunks, err := splitter.SplitText(code)
```

**支持的语言**:

- Go
- Python
- JavaScript/TypeScript
- Java
- Rust
- C/C++

**特性**:

- 保持代码结构
- 函数/类级别分割
- 语言特定分隔符

## 快速开始

### 基本用法

```go
package main

import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/document"
)

func main() {
    // 1. 加载文档
    loader := document.NewTextLoader(document.TextLoaderConfig{
        FilePath: "example.txt",
    })

    docs, err := loader.Load(context.Background())
    if err != nil {
        panic(err)
    }

    fmt.Printf("Loaded %d documents\n", len(docs))

    // 2. 分割文档
    splitter := document.NewRecursiveCharacterTextSplitter(
        document.RecursiveCharacterTextSplitterConfig{
            ChunkSize:    1000,
            ChunkOverlap: 200,
        },
    )

    chunks, err := splitter.SplitDocuments(docs)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Split into %d chunks\n", len(chunks))
}
```

### Load and Split 组合

```go
loader := document.NewMarkdownLoader(document.MarkdownLoaderConfig{
    FilePath: "README.md",
})

splitter := document.NewMarkdownTextSplitter(document.MarkdownTextSplitterConfig{
    ChunkSize: 500,
})

// 一步完成加载和分割
docs, err := loader.LoadAndSplit(context.Background(), splitter)
```

### 批量处理

```go
loader := document.NewDirectoryLoader(document.DirectoryLoaderConfig{
    DirPath:   "./documents",
    Glob:      "*.md",
    Recursive: true,
})

splitter := document.NewRecursiveCharacterTextSplitter(
    document.RecursiveCharacterTextSplitterConfig{
        ChunkSize: 1000,
    },
)

docs, err := loader.LoadAndSplit(context.Background(), splitter)
```

## API 参考

### DocumentLoader 接口

```go
type DocumentLoader interface {
    // 加载文档
    Load(ctx context.Context) ([]*retrieval.Document, error)

    // 加载并分割
    LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*retrieval.Document, error)

    // 获取元数据
    GetMetadata() map[string]interface{}
}
```

### TextSplitter 接口

```go
type TextSplitter interface {
    // 分割文本
    SplitText(text string) ([]string, error)

    // 分割文档
    SplitDocuments(docs []*retrieval.Document) ([]*retrieval.Document, error)

    // 获取配置
    GetChunkSize() int
    GetChunkOverlap() int
}
```

### Document 结构

```go
type Document struct {
    ID          string
    PageContent string
    Metadata    map[string]interface{}
    Score       float64
}
```

**自动添加的元数据** (在分割时):

- `chunk_index`: 块索引
- `chunk_total`: 总块数
- `source_id`: 源文档 ID

## 最佳实践

### 1. 选择合适的分割器

- **自然语言**: 使用 `RecursiveCharacterTextSplitter`
- **代码**: 使用 `CodeTextSplitter` 并指定语言
- **Markdown**: 使用 `MarkdownTextSplitter`
- **精确控制**: 使用 `TokenTextSplitter`

### 2. 配置块大小

```go
// 根据用途选择块大小
const (
    SmallChunk  = 500   // 精确搜索
    MediumChunk = 1000  // 通用用途
    LargeChunk  = 2000  // 长上下文
)
```

### 3. 使用重叠

```go
// 重叠通常是块大小的 10-20%
splitter := document.NewCharacterTextSplitter(document.CharacterTextSplitterConfig{
    ChunkSize:    1000,
    ChunkOverlap: 200, // 20%
})
```

### 4. 批量处理

```go
// 使用 DirectoryLoader 批量处理
loader := document.NewDirectoryLoader(document.DirectoryLoaderConfig{
    DirPath:   "./large-corpus",
    Recursive: true,
})

// 分批处理避免内存问题
docs, _ := loader.Load(ctx)
for i := 0; i < len(docs); i += batchSize {
    batch := docs[i:min(i+batchSize, len(docs))]
    // 处理批次
}
```

### 5. 元数据管理

```go
// 添加自定义元数据
loader := document.NewTextLoader(document.TextLoaderConfig{
    FilePath: "doc.txt",
    Metadata: map[string]interface{}{
        "category":   "technical",
        "importance": "high",
        "version":    "1.0",
    },
})
```

## 性能优化

### 1. 内存优化

```go
// 对于大文件,使用流式处理
// 或分批加载和处理

const batchSize = 100

for i := 0; i < totalDocs; i += batchSize {
    // 处理批次
    processBatch(i, min(i+batchSize, totalDocs))
}
```

### 2. 并发处理

```go
// 并发处理多个文件
var wg sync.WaitGroup
semaphore := make(chan struct{}, 10) // 限制并发数

for _, file := range files {
    wg.Add(1)
    go func(f string) {
        defer wg.Done()
        semaphore <- struct{}{}
        defer func() { <-semaphore }()

        loader := document.NewTextLoader(document.TextLoaderConfig{
            FilePath: f,
        })
        docs, _ := loader.Load(ctx)
        // 处理文档
    }(file)
}

wg.Wait()
```

### 3. 缓存策略

```go
// 缓存已加载的文档
type CachedLoader struct {
    loader document.DocumentLoader
    cache  map[string][]*retrieval.Document
    mu     sync.RWMutex
}

func (l *CachedLoader) Load(ctx context.Context) ([]*retrieval.Document, error) {
    key := l.getCacheKey()

    l.mu.RLock()
    if docs, ok := l.cache[key]; ok {
        l.mu.RUnlock()
        return docs, nil
    }
    l.mu.RUnlock()

    docs, err := l.loader.Load(ctx)
    if err != nil {
        return nil, err
    }

    l.mu.Lock()
    l.cache[key] = docs
    l.mu.Unlock()

    return docs, nil
}
```

### 4. 监控和调试

```go
// 使用 Callbacks 监控性能
import "github.com/kart-io/goagent/core"

callbacks := core.NewCallbackManager(
    core.NewStdoutCallback(true), // 彩色输出
)

loader := document.NewTextLoader(document.TextLoaderConfig{
    FilePath:        "large-file.txt",
    CallbackManager: callbacks,
})
```

## 示例

完整示例请参考:

- `examples/loaders/` - 各种加载器示例
- `examples/splitters/` - 各种分割器示例
- `examples/advanced/` - 高级用法和场景

## 测试

运行测试:

```bash
go test -v ./document/...
```

运行基准测试:

```bash
go test -bench=. -benchmem ./document/...
```

## 相关文档

- [Retrieval 系统](../retrieval/README.md)
- [Callback 系统](../core/README.md)
- [Vector Stores](../vectorstores/README.md)
