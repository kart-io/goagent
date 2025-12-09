package retrieval

import (
	"context"
	"fmt"
	"os"
	"strings"

	aiplatform "cloud.google.com/go/aiplatform/apiv1"
	"cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/structpb"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/sashabaranov/go-openai"
)

// VertexAIEmbedder 使用 Google Vertex AI 的嵌入模型
//
// 支持 text-embedding-005 等模型，提供高质量的语义嵌入
type VertexAIEmbedder struct {
	*BaseEmbedder
	client    *aiplatform.PredictionClient
	projectID string
	location  string
	modelID   string
	endpoint  string
}

// VertexAIEmbedderConfig Vertex AI 嵌入器配置
type VertexAIEmbedderConfig struct {
	// ProjectID Google Cloud 项目 ID
	ProjectID string
	// Location 区域，如 us-central1
	Location string
	// ModelID 模型 ID，如 text-embedding-005
	ModelID string
	// Dimensions 向量维度（text-embedding-005 默认 768）
	Dimensions int
}

// NewVertexAIEmbedder 创建 Vertex AI 嵌入器
func NewVertexAIEmbedder(ctx context.Context, config VertexAIEmbedderConfig) (*VertexAIEmbedder, error) {
	// 设置默认值
	if config.ProjectID == "" {
		config.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		if config.ProjectID == "" {
			config.ProjectID = os.Getenv("GCLOUD_PROJECT")
		}
	}
	if config.ProjectID == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "project ID is required").
			WithComponent("vertex_ai_embedder").
			WithOperation("create")
	}

	if config.Location == "" {
		config.Location = "us-central1"
	}

	if config.ModelID == "" {
		config.ModelID = "text-embedding-005"
	}

	if config.Dimensions <= 0 {
		config.Dimensions = 768
	}

	// 创建 Prediction 客户端
	endpoint := fmt.Sprintf("%s-aiplatform.googleapis.com:443", config.Location)
	client, err := aiplatform.NewPredictionClient(ctx,
		option.WithEndpoint(endpoint),
	)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create Vertex AI client").
			WithComponent("vertex_ai_embedder").
			WithOperation("create")
	}

	// 构建模型端点
	modelEndpoint := fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s",
		config.ProjectID, config.Location, config.ModelID)

	return &VertexAIEmbedder{
		BaseEmbedder: NewBaseEmbedder(config.Dimensions),
		client:       client,
		projectID:    config.ProjectID,
		location:     config.Location,
		modelID:      config.ModelID,
		endpoint:     modelEndpoint,
	}, nil
}

// Embed 批量嵌入文本
func (e *VertexAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 构建请求实例
	instances := make([]*structpb.Value, len(texts))
	for i, text := range texts {
		instance, err := structpb.NewStruct(map[string]interface{}{
			"content":   text,
			"task_type": "RETRIEVAL_DOCUMENT",
		})
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create instance").
				WithComponent("vertex_ai_embedder").
				WithOperation("embed").
				WithContext("text_index", i)
		}
		instances[i] = structpb.NewStructValue(instance)
	}

	// 发送请求
	req := &aiplatformpb.PredictRequest{
		Endpoint:  e.endpoint,
		Instances: instances,
	}

	resp, err := e.client.Predict(ctx, req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to get embeddings from Vertex AI").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed").
			WithContext("num_texts", len(texts))
	}

	// 解析响应
	vectors := make([][]float32, len(resp.Predictions))
	for i, pred := range resp.Predictions {
		// 提取嵌入向量
		predStruct := pred.GetStructValue()
		if predStruct == nil {
			return nil, agentErrors.New(agentErrors.CodeInternal, "invalid prediction response").
				WithComponent("vertex_ai_embedder").
				WithOperation("embed").
				WithContext("prediction_index", i)
		}

		embeddings := predStruct.Fields["embeddings"]
		if embeddings == nil {
			return nil, agentErrors.New(agentErrors.CodeInternal, "embeddings field not found").
				WithComponent("vertex_ai_embedder").
				WithOperation("embed").
				WithContext("prediction_index", i)
		}

		embeddingsStruct := embeddings.GetStructValue()
		if embeddingsStruct == nil {
			return nil, agentErrors.New(agentErrors.CodeInternal, "invalid embeddings structure").
				WithComponent("vertex_ai_embedder").
				WithOperation("embed").
				WithContext("prediction_index", i)
		}

		values := embeddingsStruct.Fields["values"]
		if values == nil {
			return nil, agentErrors.New(agentErrors.CodeInternal, "values field not found").
				WithComponent("vertex_ai_embedder").
				WithOperation("embed").
				WithContext("prediction_index", i)
		}

		listVal := values.GetListValue()
		if listVal == nil {
			return nil, agentErrors.New(agentErrors.CodeInternal, "invalid values list").
				WithComponent("vertex_ai_embedder").
				WithOperation("embed").
				WithContext("prediction_index", i)
		}

		vector := make([]float32, len(listVal.Values))
		for j, v := range listVal.Values {
			vector[j] = float32(v.GetNumberValue())
		}
		vectors[i] = vector
	}

	return vectors, nil
}

// EmbedQuery 嵌入单个查询文本
func (e *VertexAIEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	// 构建请求实例（查询任务类型不同）
	instance, err := structpb.NewStruct(map[string]interface{}{
		"content":   query,
		"task_type": "RETRIEVAL_QUERY",
	})
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create instance").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	// 发送请求
	req := &aiplatformpb.PredictRequest{
		Endpoint:  e.endpoint,
		Instances: []*structpb.Value{structpb.NewStructValue(instance)},
	}

	resp, err := e.client.Predict(ctx, req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to get query embedding from Vertex AI").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query").
			WithContext("query", truncateString(query, 100))
	}

	if len(resp.Predictions) == 0 {
		return nil, agentErrors.New(agentErrors.CodeInternal, "no predictions returned").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	// 解析响应
	predStruct := resp.Predictions[0].GetStructValue()
	if predStruct == nil {
		return nil, agentErrors.New(agentErrors.CodeInternal, "invalid prediction response").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	embeddings := predStruct.Fields["embeddings"]
	if embeddings == nil {
		return nil, agentErrors.New(agentErrors.CodeInternal, "embeddings field not found").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	embeddingsStruct := embeddings.GetStructValue()
	if embeddingsStruct == nil {
		return nil, agentErrors.New(agentErrors.CodeInternal, "invalid embeddings structure").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	values := embeddingsStruct.Fields["values"]
	if values == nil {
		return nil, agentErrors.New(agentErrors.CodeInternal, "values field not found").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	listVal := values.GetListValue()
	if listVal == nil {
		return nil, agentErrors.New(agentErrors.CodeInternal, "invalid values list").
			WithComponent("vertex_ai_embedder").
			WithOperation("embed_query")
	}

	vector := make([]float32, len(listVal.Values))
	for j, v := range listVal.Values {
		vector[j] = float32(v.GetNumberValue())
	}

	return vector, nil
}

// Close 关闭客户端
func (e *VertexAIEmbedder) Close() error {
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}

// truncateString 截断字符串（用于日志）
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// OpenAIEmbedder 使用 OpenAI 的嵌入模型
//
// 支持 text-embedding-3-small, text-embedding-3-large 等模型
type OpenAIEmbedder struct {
	*BaseEmbedder
	client  *openai.Client
	model   openai.EmbeddingModel
	baseURL string
}

// OpenAIEmbedderConfig OpenAI 嵌入器配置
type OpenAIEmbedderConfig struct {
	// APIKey OpenAI API Key
	APIKey string
	// Model 模型名称，如 text-embedding-3-small
	Model string
	// BaseURL API 基础 URL（可选，用于兼容其他 API）
	BaseURL string
	// Dimensions 向量维度
	Dimensions int
}

// NewOpenAIEmbedder 创建 OpenAI 嵌入器
func NewOpenAIEmbedder(config OpenAIEmbedderConfig) (*OpenAIEmbedder, error) {
	if config.APIKey == "" {
		config.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	if config.APIKey == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "API key is required").
			WithComponent("openai_embedder").
			WithOperation("create")
	}

	if config.Model == "" {
		config.Model = "text-embedding-3-small"
	}

	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}

	if config.Dimensions <= 0 {
		// 根据模型设置默认维度
		switch config.Model {
		case "text-embedding-3-small":
			config.Dimensions = 1536
		case "text-embedding-3-large":
			config.Dimensions = 3072
		case "text-embedding-ada-002":
			config.Dimensions = 1536
		default:
			config.Dimensions = 1536
		}
	}

	// 创建 OpenAI 客户端
	clientConfig := openai.DefaultConfig(config.APIKey)
	clientConfig.BaseURL = strings.TrimSuffix(config.BaseURL, "/")

	// 将字符串模型转换为 openai.EmbeddingModel
	var model openai.EmbeddingModel
	switch config.Model {
	case "text-embedding-3-small":
		model = openai.SmallEmbedding3
	case "text-embedding-3-large":
		model = openai.LargeEmbedding3
	case "text-embedding-ada-002":
		model = openai.AdaEmbeddingV2
	default:
		model = openai.EmbeddingModel(config.Model)
	}

	return &OpenAIEmbedder{
		BaseEmbedder: NewBaseEmbedder(config.Dimensions),
		client:       openai.NewClientWithConfig(clientConfig),
		model:        model,
		baseURL:      strings.TrimSuffix(config.BaseURL, "/"),
	}, nil
}

// Embed 批量嵌入文本
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 调用 OpenAI 嵌入 API
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: texts,
		Model: e.model,
	})
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to create embeddings").
			WithComponent("openai_embedder").
			WithOperation("embed").
			WithContext("num_texts", len(texts))
	}

	if len(resp.Data) == 0 {
		return nil, agentErrors.New(agentErrors.CodeInternal, "no embeddings returned").
			WithComponent("openai_embedder").
			WithOperation("embed")
	}

	// 按索引排序结果（OpenAI API 可能返回乱序）
	results := make([][]float32, len(texts))
	for _, data := range resp.Data {
		if data.Index < len(results) {
			results[data.Index] = data.Embedding
		}
	}

	return results, nil
}

// EmbedQuery 嵌入单个查询文本
func (e *OpenAIEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vectors, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, agentErrors.New(agentErrors.CodeInternal, "no embeddings returned").
			WithComponent("openai_embedder").
			WithOperation("embed_query")
	}
	return vectors[0], nil
}
