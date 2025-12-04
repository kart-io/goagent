package retrieval

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/utils/json"
)

// HuggingFaceEmbedder 使用 Hugging Face 的嵌入模型
//
// 支持 sentence-transformers 系列模型和自定义推理端点
type HuggingFaceEmbedder struct {
	*BaseEmbedder
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

// HuggingFaceEmbedderConfig Hugging Face 嵌入器配置
type HuggingFaceEmbedderConfig struct {
	// APIKey Hugging Face API Key
	APIKey string
	// Model 模型名称，如 sentence-transformers/all-MiniLM-L6-v2
	Model string
	// Endpoint 自定义推理端点（可选）
	Endpoint string
	// Dimensions 向量维度
	Dimensions int
}

// hfEmbedRequest Hugging Face 嵌入请求
type hfEmbedRequest struct {
	Inputs  interface{} `json:"inputs"`
	Options struct {
		WaitForModel bool `json:"wait_for_model"`
	} `json:"options"`
}

// hfErrorResponse Hugging Face 错误响应
type hfErrorResponse struct {
	Error         string  `json:"error"`
	EstimatedTime float64 `json:"estimated_time,omitempty"`
}

// NewHuggingFaceEmbedder 创建 Hugging Face 嵌入器
func NewHuggingFaceEmbedder(config HuggingFaceEmbedderConfig) (*HuggingFaceEmbedder, error) {
	if config.APIKey == "" {
		config.APIKey = os.Getenv("HUGGINGFACE_API_KEY")
		if config.APIKey == "" {
			config.APIKey = os.Getenv("HF_API_KEY")
		}
	}
	if config.APIKey == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "API key is required").
			WithComponent("huggingface_embedder").
			WithOperation("create")
	}

	if config.Model == "" {
		config.Model = "sentence-transformers/all-MiniLM-L6-v2"
	}

	if config.Endpoint == "" {
		config.Endpoint = "https://api-inference.huggingface.co/pipeline/feature-extraction/" + config.Model
	}

	if config.Dimensions <= 0 {
		// 常用模型默认维度
		switch config.Model {
		case "sentence-transformers/all-MiniLM-L6-v2":
			config.Dimensions = 384
		case "sentence-transformers/all-mpnet-base-v2":
			config.Dimensions = 768
		case "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2":
			config.Dimensions = 384
		case "BAAI/bge-small-en-v1.5":
			config.Dimensions = 384
		case "BAAI/bge-base-en-v1.5":
			config.Dimensions = 768
		case "BAAI/bge-large-en-v1.5":
			config.Dimensions = 1024
		default:
			config.Dimensions = 768
		}
	}

	return &HuggingFaceEmbedder{
		BaseEmbedder: NewBaseEmbedder(config.Dimensions),
		apiKey:       config.APIKey,
		model:        config.Model,
		endpoint:     config.Endpoint,
		client:       &http.Client{},
	}, nil
}

// Embed 批量嵌入文本
func (e *HuggingFaceEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 构建请求
	reqBody := hfEmbedRequest{
		Inputs: texts,
	}
	reqBody.Options.WaitForModel = true

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to marshal request").
			WithComponent("huggingface_embedder").
			WithOperation("embed")
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", e.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create request").
			WithComponent("huggingface_embedder").
			WithOperation("embed")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	// 发送请求
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to send request to Hugging Face").
			WithComponent("huggingface_embedder").
			WithOperation("embed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to read response").
			WithComponent("huggingface_embedder").
			WithOperation("embed")
	}

	// 检查错误响应
	if resp.StatusCode != http.StatusOK {
		var errResp hfErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
			return nil, agentErrors.New(agentErrors.CodeRetrievalEmbedding, errResp.Error).
				WithComponent("huggingface_embedder").
				WithOperation("embed").
				WithContext("status_code", resp.StatusCode)
		}
		return nil, agentErrors.New(agentErrors.CodeRetrievalEmbedding, "Hugging Face API error").
			WithComponent("huggingface_embedder").
			WithOperation("embed").
			WithContext("status_code", resp.StatusCode).
			WithContext("body", string(body))
	}

	// 解析响应 - Hugging Face 返回格式为 [][]float64 或 [][][]float64
	// 尝试解析为 [][]float64（标准嵌入响应）
	var embeddings [][]float64
	if err := json.Unmarshal(body, &embeddings); err != nil {
		// 尝试解析为 [][][]float64（某些模型返回此格式）
		var nestedEmbeddings [][][]float64
		if err := json.Unmarshal(body, &nestedEmbeddings); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to parse response").
				WithComponent("huggingface_embedder").
				WithOperation("embed")
		}
		// 取每个文本的第一个 token 嵌入（通常是 [CLS] token）
		embeddings = make([][]float64, len(nestedEmbeddings))
		for i, nested := range nestedEmbeddings {
			if len(nested) > 0 {
				embeddings[i] = nested[0]
			}
		}
	}

	// 转换为 float32
	result := make([][]float32, len(embeddings))
	for i, emb := range embeddings {
		result[i] = make([]float32, len(emb))
		for j, v := range emb {
			result[i][j] = float32(v)
		}
	}

	return result, nil
}

// EmbedQuery 嵌入单个查询文本
func (e *HuggingFaceEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vectors, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, agentErrors.New(agentErrors.CodeInternal, "no embeddings returned").
			WithComponent("huggingface_embedder").
			WithOperation("embed_query")
	}
	return vectors[0], nil
}
