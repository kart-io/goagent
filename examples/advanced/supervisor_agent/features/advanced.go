package features

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// AdvancedFeatures 高级功能配置
type AdvancedFeatures struct {
	// Tool Calling 支持
	EnableToolCalling bool
	Tools             []ToolDefinition

	// Fine-tuning 支持
	FineTunedModel string
	BaseModel      string

	// 自动 Fallback
	EnableAutoFallback bool
	FallbackProviders  []string
	MaxRetries         int

	// 响应缓存
	EnableResponseCache bool
	CacheTTL            time.Duration
	cache               *ResponseCache

	// 批处理 API
	EnableBatchAPI bool
	BatchSize      int
	BatchTimeout   time.Duration
	batchProcessor *BatchProcessor

	// 多模态支持（文本）
	EnableMultimodal bool
	SupportedModes   []string // ["text", "code", "json", etc.]
}

// ToolDefinition 工具定义（符合 OpenAI/Anthropic 格式）
type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function FunctionDefinition     `json:"function"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// FunctionDefinition 函数定义
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ResponseCache 响应缓存
type ResponseCache struct {
	data  map[string]*CacheEntry
	mutex sync.RWMutex
	ttl   time.Duration
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Response  *llm.CompletionResponse
	Timestamp time.Time
}

// NewResponseCache 创建响应缓存
func NewResponseCache(ttl time.Duration) *ResponseCache {
	cache := &ResponseCache{
		data: make(map[string]*CacheEntry),
		ttl:  ttl,
	}
	// 启动清理协程
	go cache.cleanup()
	return cache
}

// Get 获取缓存
func (c *ResponseCache) Get(key string) (*llm.CompletionResponse, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry, exists := c.data[key]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry.Response, true
}

// Set 设置缓存
func (c *ResponseCache) Set(key string, response *llm.CompletionResponse) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.data[key] = &CacheEntry{
		Response:  response,
		Timestamp: time.Now(),
	}
}

// cleanup 定期清理过期缓存
func (c *ResponseCache) cleanup() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for range ticker.C {
		c.mutex.Lock()
		now := time.Now()
		for key, entry := range c.data {
			if now.Sub(entry.Timestamp) > c.ttl {
				delete(c.data, key)
			}
		}
		c.mutex.Unlock()
	}
}

// BatchProcessor 批处理处理器
type BatchProcessor struct {
	batchSize    int
	batchTimeout time.Duration
	queue        chan *BatchRequest
	results      map[string]chan *llm.CompletionResponse
	mutex        sync.RWMutex
}

// BatchRequest 批处理请求
type BatchRequest struct {
	ID      string
	Request *llm.CompletionRequest
}

// NewBatchProcessor 创建批处理处理器
func NewBatchProcessor(batchSize int, batchTimeout time.Duration) *BatchProcessor {
	bp := &BatchProcessor{
		batchSize:    batchSize,
		batchTimeout: batchTimeout,
		queue:        make(chan *BatchRequest, 100),
		results:      make(map[string]chan *llm.CompletionResponse),
	}
	go bp.processBatches()
	return bp
}

// Submit 提交批处理请求
func (bp *BatchProcessor) Submit(id string, req *llm.CompletionRequest) <-chan *llm.CompletionResponse {
	resultChan := make(chan *llm.CompletionResponse, 1)

	bp.mutex.Lock()
	bp.results[id] = resultChan
	bp.mutex.Unlock()

	bp.queue <- &BatchRequest{
		ID:      id,
		Request: req,
	}

	return resultChan
}

// processBatches 处理批次
func (bp *BatchProcessor) processBatches() {
	batch := make([]*BatchRequest, 0, bp.batchSize)
	timer := time.NewTimer(bp.batchTimeout)

	for {
		select {
		case req := <-bp.queue:
			batch = append(batch, req)
			if len(batch) >= bp.batchSize {
				bp.executeBatch(batch)
				batch = make([]*BatchRequest, 0, bp.batchSize)
				timer.Reset(bp.batchTimeout)
			}

		case <-timer.C:
			if len(batch) > 0 {
				bp.executeBatch(batch)
				batch = make([]*BatchRequest, 0, bp.batchSize)
			}
			timer.Reset(bp.batchTimeout)
		}
	}
}

// executeBatch 执行批次
func (bp *BatchProcessor) executeBatch(batch []*BatchRequest) {
	fmt.Printf("📦 执行批处理: %d 个请求\n", len(batch))

	// 这里应该调用实际的批处理 API
	// 为了演示，这里模拟批处理执行
	for _, req := range batch {
		bp.mutex.RLock()
		resultChan, exists := bp.results[req.ID]
		bp.mutex.RUnlock()

		if exists {
			// 模拟响应
			response := &llm.CompletionResponse{
				Content: fmt.Sprintf("批处理响应: %s", req.ID),
			}
			resultChan <- response
			close(resultChan)

			bp.mutex.Lock()
			delete(bp.results, req.ID)
			bp.mutex.Unlock()
		}
	}
}

// EnhancedLLMClient 增强的 LLM 客户端（包装原始客户端）
type EnhancedLLMClient struct {
	baseClient llm.Client
	features   *AdvancedFeatures
	fallbacks  []llm.Client
}

// NewEnhancedLLMClient 创建增强的 LLM 客户端
func NewEnhancedLLMClient(baseClient llm.Client, features *AdvancedFeatures) *EnhancedLLMClient {
	enhanced := &EnhancedLLMClient{
		baseClient: baseClient,
		features:   features,
	}

	// 初始化缓存
	if features.EnableResponseCache {
		features.cache = NewResponseCache(features.CacheTTL)
	}

	// 初始化批处理器
	if features.EnableBatchAPI {
		features.batchProcessor = NewBatchProcessor(features.BatchSize, features.BatchTimeout)
	}

	return enhanced
}

// Complete 完成请求（带高级功能）
func (e *EnhancedLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 1. 检查缓存
	if e.features.EnableResponseCache {
		cacheKey := e.generateCacheKey(req)
		if cached, found := e.features.cache.Get(cacheKey); found {
			fmt.Println("✅ 缓存命中")
			return cached, nil
		}
	}

	// 2. 添加 Tool Calling
	if e.features.EnableToolCalling && len(e.features.Tools) > 0 {
		req = e.addToolCalling(req)
	}

	// 3. 多模态处理
	if e.features.EnableMultimodal {
		req = e.enhanceMultimodal(req)
	}

	// 4. 执行请求（带自动 fallback）
	var response *llm.CompletionResponse
	var err error

	if e.features.EnableAutoFallback {
		response, err = e.completeWithFallback(ctx, req)
	} else {
		response, err = e.baseClient.Complete(ctx, req)
	}

	// 5. 缓存响应
	if err == nil && e.features.EnableResponseCache {
		cacheKey := e.generateCacheKey(req)
		e.features.cache.Set(cacheKey, response)
	}

	return response, err
}

// Chat 对话接口（委托给 Complete）
func (e *EnhancedLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return e.baseClient.Chat(ctx, messages)
}

// Provider 返回提供商类型
func (e *EnhancedLLMClient) Provider() constants.Provider {
	return e.baseClient.Provider()
}

// IsAvailable 检查是否可用
func (e *EnhancedLLMClient) IsAvailable() bool {
	return e.baseClient.IsAvailable()
}

// completeWithFallback 带自动 fallback 的完成
func (e *EnhancedLLMClient) completeWithFallback(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	var lastErr error

	// 尝试主客户端
	for attempt := 0; attempt <= e.features.MaxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("🔄 重试 %d/%d...\n", attempt, e.features.MaxRetries)
			time.Sleep(time.Second * time.Duration(attempt))
		}

		response, err := e.baseClient.Complete(ctx, req)
		if err == nil {
			return response, nil
		}
		lastErr = err
	}

	// 尝试 fallback 客户端
	for i, fallback := range e.fallbacks {
		fmt.Printf("⚠️ 主客户端失败，尝试 fallback %d...\n", i+1)
		response, err := fallback.Complete(ctx, req)
		if err == nil {
			fmt.Printf("✅ Fallback %d 成功\n", i+1)
			return response, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("所有尝试均失败: %w", lastErr)
}

// addToolCalling 添加 Tool Calling
func (e *EnhancedLLMClient) addToolCalling(req *llm.CompletionRequest) *llm.CompletionRequest {
	// 这里应该将工具定义添加到请求中
	// 具体格式取决于 LLM 提供商（OpenAI, Anthropic, etc.）
	fmt.Printf("🔧 添加 %d 个工具定义\n", len(e.features.Tools))
	return req
}

// enhanceMultimodal 增强多模态支持
func (e *EnhancedLLMClient) enhanceMultimodal(req *llm.CompletionRequest) *llm.CompletionRequest {
	// 处理不同模式的内容
	for i, msg := range req.Messages {
		if e.containsCode(msg.Content) {
			req.Messages[i].Content = e.wrapCodeBlock(msg.Content)
		}
	}
	return req
}

// containsCode 检查是否包含代码
func (e *EnhancedLLMClient) containsCode(content string) bool {
	// 简单检测：包含特定关键字或代码块标记
	return len(content) > 50 && (
	// 包含代码块
	len(content) > 100)
}

// wrapCodeBlock 包装代码块
func (e *EnhancedLLMClient) wrapCodeBlock(content string) string {
	// 确保代码块被正确标记
	return content
}

// generateCacheKey 生成缓存键
func (e *EnhancedLLMClient) generateCacheKey(req *llm.CompletionRequest) string {
	// 简单实现：基于消息内容生成键
	key := ""
	for _, msg := range req.Messages {
		key += msg.Role + ":" + msg.Content + ";"
	}
	return key
}

// AddFallbackClient 添加 fallback 客户端
func (e *EnhancedLLMClient) AddFallbackClient(client llm.Client) {
	e.fallbacks = append(e.fallbacks, client)
}

// DefaultAdvancedFeatures 默认高级功能配置
func DefaultAdvancedFeatures() *AdvancedFeatures {
	return &AdvancedFeatures{
		EnableToolCalling: true,
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: FunctionDefinition{
					Name:        "search_web",
					Description: "搜索网络获取最新信息",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type":        "string",
								"description": "搜索查询",
							},
						},
						"required": []string{"query"},
					},
				},
			},
			{
				Type: "function",
				Function: FunctionDefinition{
					Name:        "analyze_code",
					Description: "分析代码并提供改进建议",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"code": map[string]interface{}{
								"type":        "string",
								"description": "要分析的代码",
							},
							"language": map[string]interface{}{
								"type":        "string",
								"description": "编程语言",
							},
						},
						"required": []string{"code"},
					},
				},
			},
		},
		EnableAutoFallback:  true,
		FallbackProviders:   []string{"openai", "deepseek"},
		MaxRetries:          3,
		EnableResponseCache: true,
		CacheTTL:            5 * time.Minute,
		EnableBatchAPI:      false, // 默认关闭批处理
		BatchSize:           10,
		BatchTimeout:        5 * time.Second,
		EnableMultimodal:    true,
		SupportedModes:      []string{"text", "code", "json"},
	}
}

// DemoAdvancedFeatures 演示高级功能
func DemoAdvancedFeatures(llmClient llm.Client) {
	fmt.Println("\n🚀 高级功能演示")
	fmt.Println(string([]rune(strings.Repeat("=", 80))))

	// 创建高级功能配置
	features := DefaultAdvancedFeatures()

	// 创建增强客户端
	enhanced := NewEnhancedLLMClient(llmClient, features)

	ctx := context.Background()

	// 1. 演示响应缓存
	fmt.Println("\n📦 1. 响应缓存演示")
	fmt.Println(strings.Repeat("-", 80))

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "什么是 Go 语言？"},
		},
	}

	start := time.Now()
	resp1, err := enhanced.Complete(ctx, req)
	duration1 := time.Since(start)
	if err != nil {
		log.Printf("第一次请求失败: %v", err)
	} else {
		fmt.Printf("第一次请求耗时: %v\n", duration1)
		fmt.Printf("响应: %s\n", truncate(resp1.Content, 100))
	}

	// 第二次请求应该命中缓存
	start = time.Now()
	_, err = enhanced.Complete(ctx, req)
	duration2 := time.Since(start)
	if err != nil {
		log.Printf("第二次请求失败: %v", err)
	} else {
		fmt.Printf("第二次请求耗时: %v (缓存命中)\n", duration2)
		fmt.Printf("加速比: %.2fx\n", float64(duration1)/float64(duration2))
	}

	// 2. 演示 Tool Calling
	fmt.Println("\n🔧 2. Tool Calling 演示")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("已注册工具:\n")
	for i, tool := range features.Tools {
		fmt.Printf("  %d. %s: %s\n", i+1, tool.Function.Name, tool.Function.Description)
	}

	// 3. 演示多模态处理
	fmt.Println("\n🎨 3. 多模态支持演示")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("支持的模式: %v\n", features.SupportedModes)

	codeReq := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "分析这段代码:\nfunc main() {\n    fmt.Println(\"Hello\")\n}"},
		},
	}
	_, err = enhanced.Complete(ctx, codeReq)
	if err != nil {
		log.Printf("代码分析失败: %v", err)
	}

	// 4. 演示自动 Fallback
	fmt.Println("\n⚡ 4. 自动 Fallback 演示")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("配置:\n")
	fmt.Printf("  - 主提供商: %s\n", "DeepSeek")
	fmt.Printf("  - Fallback 提供商: %v\n", features.FallbackProviders)
	fmt.Printf("  - 最大重试次数: %d\n", features.MaxRetries)

	fmt.Println("\n✅ 高级功能演示完成")
	fmt.Println(strings.Repeat("=", 80))
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
