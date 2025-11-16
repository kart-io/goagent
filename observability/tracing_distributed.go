package observability

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// DistributedTracer 分布式追踪器
type DistributedTracer struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// NewDistributedTracer 创建分布式追踪器
func NewDistributedTracer() *DistributedTracer {
	return &DistributedTracer{
		tracer:     otel.Tracer("distributed-agent"),
		propagator: otel.GetTextMapPropagator(),
	}
}

// InjectContext 注入上下文到 carrier
func (t *DistributedTracer) InjectContext(ctx context.Context, carrier propagation.TextMapCarrier) error {
	t.propagator.Inject(ctx, carrier)
	return nil
}

// ExtractContext 从 carrier 提取上下文
func (t *DistributedTracer) ExtractContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return t.propagator.Extract(ctx, carrier)
}

// StartRemoteSpan 开始远程 Span
func (t *DistributedTracer) StartRemoteSpan(ctx context.Context, name string, carrier propagation.TextMapCarrier) (context.Context, trace.Span) {
	// 从 carrier 提取上下文
	ctx = t.ExtractContext(ctx, carrier)
	// 启动新 span
	return t.tracer.Start(ctx, name)
}

// HTTPCarrier HTTP 载体
type HTTPCarrier struct {
	headers http.Header
}

// NewHTTPCarrier 创建 HTTP 载体
func NewHTTPCarrier(headers http.Header) *HTTPCarrier {
	return &HTTPCarrier{headers: headers}
}

// Get 获取值
func (c *HTTPCarrier) Get(key string) string {
	return c.headers.Get(key)
}

// Set 设置值
func (c *HTTPCarrier) Set(key string, value string) {
	c.headers.Set(key, value)
}

// Keys 获取所有键
func (c *HTTPCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for k := range c.headers {
		keys = append(keys, k)
	}
	return keys
}

// MessageCarrier 消息载体（用于消息队列）
type MessageCarrier struct {
	metadata map[string]string
}

// NewMessageCarrier 创建消息载体
func NewMessageCarrier(metadata map[string]string) *MessageCarrier {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	return &MessageCarrier{metadata: metadata}
}

// Get 获取值
func (c *MessageCarrier) Get(key string) string {
	return c.metadata[key]
}

// Set 设置值
func (c *MessageCarrier) Set(key string, value string) {
	c.metadata[key] = value
}

// Keys 获取所有键
func (c *MessageCarrier) Keys() []string {
	keys := make([]string, 0, len(c.metadata))
	for k := range c.metadata {
		keys = append(keys, k)
	}
	return keys
}

// GetMetadata 获取完整元数据
func (c *MessageCarrier) GetMetadata() map[string]string {
	return c.metadata
}

// CrossServiceTracer 跨服务追踪器
type CrossServiceTracer struct {
	tracer      *DistributedTracer
	serviceName string
}

// NewCrossServiceTracer 创建跨服务追踪器
func NewCrossServiceTracer(serviceName string) *CrossServiceTracer {
	return &CrossServiceTracer{
		tracer:      NewDistributedTracer(),
		serviceName: serviceName,
	}
}

// TraceHTTPRequest 追踪 HTTP 请求
func (t *CrossServiceTracer) TraceHTTPRequest(ctx context.Context, req *http.Request) (context.Context, trace.Span) {
	// 注入上下文到请求头
	carrier := NewHTTPCarrier(req.Header)
	t.tracer.InjectContext(ctx, carrier)

	// 启动 span
	ctx, span := t.tracer.tracer.Start(ctx, "http.request")
	return ctx, span
}

// TraceHTTPResponse 追踪 HTTP 响应
func (t *CrossServiceTracer) TraceHTTPResponse(ctx context.Context, resp *http.Response) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
	return nil
}

// TraceMessage 追踪消息（NATS/Kafka 等）
func (t *CrossServiceTracer) TraceMessage(ctx context.Context, topic string, message []byte) (context.Context, trace.Span, *MessageCarrier) {
	// 创建消息载体
	carrier := NewMessageCarrier(nil)

	// 注入上下文
	t.tracer.InjectContext(ctx, carrier)

	// 启动 span
	ctx, span := t.tracer.tracer.Start(ctx, "message.send")

	return ctx, span, carrier
}
