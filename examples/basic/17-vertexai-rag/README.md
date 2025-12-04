# Vertex AI RAG 示例

本示例演示如何使用 Google Vertex AI（Gemini）构建 RAG（检索增强生成）系统。

参考项目：https://github.com/meteatamel/genai-beyond-basics/tree/main/samples/grounding/llamaindex-vertexai

## 功能特点

- 使用 Vertex AI text-embedding-005 模型进行文档向量化
- 使用 Gemini 模型（gemini-2.0-flash）进行问答生成
- 支持 PDF 文档加载和文本分割
- 内存向量存储，支持余弦相似度检索
- 提供简单嵌入器用于演示（无需 Google Cloud 账号）

## 前置条件

### 使用 Vertex AI（推荐）

1. 拥有 Google Cloud 账号和项目
2. 启用 Vertex AI API
3. 配置应用默认凭证：

```bash
# 登录并配置
gcloud auth application-default login
gcloud config set project YOUR_PROJECT_ID
```

### 使用简单模式（演示）

如果没有 Google Cloud 账号，可以使用 `-simple` 参数运行简单模式，使用基于 TF-IDF 的简单嵌入器。

## 安装依赖

```bash
go mod tidy
```

## 使用方法

### 基本用法

```bash
# 直接查询 Gemini（无 RAG）
go run main.go -prompt "什么是机器学习？"

# 使用 PDF 文档进行 RAG 查询
go run main.go -pdf document.pdf -prompt "文档的主要内容是什么？"
```

### 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-pdf` | PDF 文件路径 | 空（无 RAG） |
| `-prompt` | 查询问题（必需） | - |
| `-project` | Google Cloud 项目 ID | 环境变量 |
| `-location` | Vertex AI 区域 | us-central1 |
| `-model` | Gemini 模型名称 | gemini-2.0-flash |
| `-chunk-size` | 文本分块大小 | 1024 |
| `-overlap` | 分块重叠大小 | 20 |
| `-topk` | 检索返回文档数 | 4 |
| `-simple` | 使用简单嵌入器 | false |

### 示例

```bash
# 使用 Vertex AI 嵌入（需要 Google Cloud）
go run main.go \
  -project my-gcp-project \
  -pdf cymbal-starlight-2024.pdf \
  -prompt "Cymbal Starlight 的主要特点是什么？"

# 使用简单嵌入器（无需 Google Cloud）
go run main.go \
  -simple \
  -pdf cymbal-starlight-2024.pdf \
  -prompt "文档讲了什么内容？"

# 自定义参数
go run main.go \
  -pdf document.pdf \
  -prompt "总结文档内容" \
  -chunk-size 512 \
  -overlap 50 \
  -topk 6
```

## 架构说明

```
┌─────────────────────────────────────────────────────────────────┐
│                        用户查询                                  │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      PDF 文档加载                                │
│                   (document.PDFLoader)                          │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      文本分割                                    │
│          (document.RecursiveCharacterTextSplitter)              │
│              1024 字符块，20 字符重叠                            │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      向量嵌入                                    │
│         Vertex AI: text-embedding-005 (768维)                   │
│         或 SimpleEmbedder（演示用）                              │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     内存向量存储                                 │
│               (retrieval.MemoryVectorStore)                     │
│                    余弦相似度检索                                │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     RAG 检索器                                   │
│                (retrieval.RAGRetriever)                         │
│                   返回 TopK 相关文档                             │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Gemini 生成                                  │
│                  (gemini-2.0-flash)                             │
│               基于检索上下文生成回答                             │
└─────────────────────────────────────────────────────────────────┘
```

## 与 PDF RAG 示例的区别

| 特性 | 16-pdf-rag | 17-vertexai-rag |
|------|------------|-----------------|
| LLM | DeepSeek | Gemini |
| 嵌入模型 | SimpleEmbedder | Vertex AI text-embedding-005 |
| 向量维度 | 384 | 768 |
| 分块大小 | 500 | 1024 |
| 重叠大小 | 100 | 20 |
| 云服务依赖 | 无 | Google Cloud |

## 环境变量

```bash
# Google Cloud 项目 ID（可选，也可使用 -project 参数）
export GOOGLE_CLOUD_PROJECT=your-project-id
# 或
export GCLOUD_PROJECT=your-project-id
```

## 注意事项

1. **费用**：使用 Vertex AI 会产生 Google Cloud 费用
2. **区域**：确保选择的区域支持 text-embedding-005 模型
3. **配额**：注意 Vertex AI API 的调用配额限制
4. **认证**：需要正确配置 Google Cloud 应用默认凭证

## 故障排除

### 认证失败

```bash
# 重新登录
gcloud auth application-default login

# 检查当前项目
gcloud config get-value project
```

### API 未启用

```bash
# 启用 Vertex AI API
gcloud services enable aiplatform.googleapis.com
```

### 模型不可用

某些区域可能不支持特定模型，尝试更换区域：

```bash
go run main.go -location us-east1 -prompt "测试"
```

## 参考资料

- [Vertex AI 文档](https://cloud.google.com/vertex-ai/docs)
- [Gemini API](https://cloud.google.com/vertex-ai/docs/generative-ai/model-reference/gemini)
- [Text Embeddings](https://cloud.google.com/vertex-ai/docs/generative-ai/embeddings/get-text-embeddings)
- [原始参考项目](https://github.com/meteatamel/genai-beyond-basics/tree/main/samples/grounding/llamaindex-vertexai)
