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

// CohereEmbedder 使用 Cohere 的嵌入模型
//
// 支持 embed-english-v3.0, embed-multilingual-v3.0 等模型
type CohereEmbedder struct {
	*BaseEmbedder
	apiKey    string
	model     string
	inputType string
	baseURL   string
	client    *http.Client
}

// CohereEmbedderConfig Cohere 嵌入器配置
type CohereEmbedderConfig struct {
	// APIKey Cohere API Key
	APIKey string
	// Model 模型名称，如 embed-english-v3.0
	Model string
	// InputType 输入类型：search_document, search_query, classification, clustering
	InputType string
	// BaseURL API 基础 URL（可选）
	BaseURL string
	// Dimensions 向量维度
	Dimensions int
}

// cohereEmbedRequest Cohere 嵌入请求
type cohereEmbedRequest struct {
	Texts     []string `json:"texts"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
}

// cohereEmbedResponse Cohere 嵌入响应
type cohereEmbedResponse struct {
	ID         string      `json:"id"`
	Texts      []string    `json:"texts"`
	Embeddings [][]float32 `json:"embeddings"`
	Meta       struct {
		APIVersion struct {
			Version string `json:"version"`
		} `json:"api_version"`
	} `json:"meta"`
}

// cohereErrorResponse Cohere 错误响应
type cohereErrorResponse struct {
	Message string `json:"message"`
}

// NewCohereEmbedder 创建 Cohere 嵌入器
func NewCohereEmbedder(config CohereEmbedderConfig) (*CohereEmbedder, error) {
	if config.APIKey == "" {
		config.APIKey = os.Getenv("COHERE_API_KEY")
	}
	if config.APIKey == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "API key is required").
			WithComponent("cohere_embedder").
			WithOperation("create")
	}

	if config.Model == "" {
		config.Model = "embed-english-v3.0"
	}

	if config.InputType == "" {
		config.InputType = "search_document"
	}

	if config.BaseURL == "" {
		config.BaseURL = "https://api.cohere.ai/v1"
	}

	if config.Dimensions <= 0 {
		// Cohere embed-v3 模型默认维度
		switch config.Model {
		case "embed-english-v3.0", "embed-multilingual-v3.0":
			config.Dimensions = 1024
		case "embed-english-light-v3.0", "embed-multilingual-light-v3.0":
			config.Dimensions = 384
		default:
			config.Dimensions = 1024
		}
	}

	return &CohereEmbedder{
		BaseEmbedder: NewBaseEmbedder(config.Dimensions),
		apiKey:       config.APIKey,
		model:        config.Model,
		inputType:    config.InputType,
		baseURL:      config.BaseURL,
		client:       &http.Client{},
	}, nil
}

// Embed 批量嵌入文本
func (e *CohereEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 构建请求
	reqBody := cohereEmbedRequest{
		Texts:     texts,
		Model:     e.model,
		InputType: e.inputType,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to marshal request").
			WithComponent("cohere_embedder").
			WithOperation("embed")
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/embed", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create request").
			WithComponent("cohere_embedder").
			WithOperation("embed")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	// 发送请求
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to send request to Cohere").
			WithComponent("cohere_embedder").
			WithOperation("embed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to read response").
			WithComponent("cohere_embedder").
			WithOperation("embed")
	}

	// 检查错误响应
	if resp.StatusCode != http.StatusOK {
		var errResp cohereErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
			return nil, agentErrors.New(agentErrors.CodeRetrievalEmbedding, errResp.Message).
				WithComponent("cohere_embedder").
				WithOperation("embed").
				WithContext("status_code", resp.StatusCode)
		}
		return nil, agentErrors.New(agentErrors.CodeRetrievalEmbedding, "Cohere API error").
			WithComponent("cohere_embedder").
			WithOperation("embed").
			WithContext("status_code", resp.StatusCode).
			WithContext("body", string(body))
	}

	// 解析响应
	var embedResp cohereEmbedResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to parse response").
			WithComponent("cohere_embedder").
			WithOperation("embed")
	}

	return embedResp.Embeddings, nil
}

// EmbedQuery 嵌入单个查询文本
func (e *CohereEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	// 查询时使用 search_query 输入类型
	originalInputType := e.inputType
	e.inputType = "search_query"
	defer func() {
		e.inputType = originalInputType
	}()

	vectors, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, agentErrors.New(agentErrors.CodeInternal, "no embeddings returned").
			WithComponent("cohere_embedder").
			WithOperation("embed_query")
	}
	return vectors[0], nil
}
