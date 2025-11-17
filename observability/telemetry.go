package observability

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agentErrors "github.com/kart-io/goagent/errors"
)

// TelemetryProvider OpenTelemetry 提供者
type TelemetryProvider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	logger         *log.Logger
	config         *TelemetryConfig
	resource       *resource.Resource
}

// TelemetryConfig 配置
type TelemetryConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string

	// Trace
	TraceEnabled    bool
	TraceExporter   string  // "otlp", "stdout", "noop"
	TraceEndpoint   string  // OTLP endpoint
	TraceSampleRate float64 // 0.0 to 1.0

	// Metrics
	MetricsEnabled  bool
	MetricsExporter string // "prometheus", "otlp", "noop"
	MetricsEndpoint string
	MetricsInterval time.Duration

	// Logs (optional)
	LogsEnabled  bool
	LogsExporter string
	LogsEndpoint string

	// Resource attributes
	ResourceAttributes map[string]string
}

// DefaultTelemetryConfig 返回默认配置
func DefaultTelemetryConfig() *TelemetryConfig {
	return &TelemetryConfig{
		ServiceName:     "agent-service",
		ServiceVersion:  "1.0.0",
		Environment:     "development",
		TraceEnabled:    true,
		TraceExporter:   "otlp",
		TraceEndpoint:   "localhost:4317",
		TraceSampleRate: 1.0,
		MetricsEnabled:  true,
		MetricsExporter: "prometheus",
		MetricsInterval: 60 * time.Second,
		LogsEnabled:     false,
		ResourceAttributes: map[string]string{
			"deployment.environment": "development",
		},
	}
}

// NewTelemetryProvider 创建提供者
func NewTelemetryProvider(config *TelemetryConfig) (*TelemetryProvider, error) {
	if config == nil {
		config = DefaultTelemetryConfig()
	}

	provider := &TelemetryProvider{
		config: config,
		logger: log.Default(),
	}

	// 创建资源
	res, err := provider.createResource()
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create resource").
			WithComponent("telemetry").
			WithOperation("NewTelemetryProvider")
	}
	provider.resource = res

	// 初始化 Tracer
	if config.TraceEnabled {
		tracerProvider, err := provider.initTracer(res)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to initialize tracer").
				WithComponent("telemetry").
				WithOperation("NewTelemetryProvider")
		}
		provider.tracerProvider = tracerProvider
		otel.SetTracerProvider(tracerProvider)
	}

	// 初始化 Meter
	if config.MetricsEnabled {
		meterProvider := provider.initMeter(res)
		provider.meterProvider = meterProvider
		otel.SetMeterProvider(meterProvider)
	}

	return provider, nil
}

// createResource 创建资源
func (p *TelemetryProvider) createResource() (*resource.Resource, error) {
	attrs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceName(p.config.ServiceName),
			semconv.ServiceVersion(p.config.ServiceVersion),
			semconv.DeploymentEnvironment(p.config.Environment),
		),
	}

	// 添加自定义属性
	if len(p.config.ResourceAttributes) > 0 {
		customAttrs := make([]attribute.KeyValue, 0, len(p.config.ResourceAttributes)*2)
		for k, v := range p.config.ResourceAttributes {
			customAttrs = append(customAttrs, attribute.String(k, v))
		}
		attrs = append(attrs, resource.WithAttributes(customAttrs...))
	}

	res, err := resource.New(
		context.Background(),
		attrs...,
	)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create resource").
			WithComponent("telemetry").
			WithOperation("createResource")
	}

	return res, nil
}

// initTracer 初始化 Tracer
func (p *TelemetryProvider) initTracer(res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error

	switch p.config.TraceExporter {
	case "otlp":
		exporter, err = p.createOTLPExporter()
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create OTLP exporter").
				WithComponent("telemetry").
				WithOperation("initTracer").
				WithContext("endpoint", p.config.TraceEndpoint)
		}
	case "stdout":
		// stdout exporter is deprecated, using noop instead
		exporter = &noopExporter{}
	case "noop":
		exporter = &noopExporter{}
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "unsupported trace exporter").
			WithComponent("telemetry").
			WithOperation("initTracer").
			WithContext("exporter", p.config.TraceExporter)
	}

	// 创建采样器
	sampler := sdktrace.AlwaysSample()
	if p.config.TraceSampleRate < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(p.config.TraceSampleRate)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sampler),
	)

	return tracerProvider, nil
}

// createOTLPExporter 创建 OTLP 导出器
func (p *TelemetryProvider) createOTLPExporter() (sdktrace.SpanExporter, error) {
	ctx := context.Background()

	// 创建 gRPC 连接
	conn, err := grpc.NewClient(
		p.config.TraceEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to create gRPC connection").
			WithComponent("telemetry").
			WithOperation("createOTLPExporter").
			WithContext("endpoint", p.config.TraceEndpoint)
	}

	// 创建 OTLP trace 导出器
	exporter, err := otlptrace.New(
		ctx,
		otlptracegrpc.NewClient(
			otlptracegrpc.WithGRPCConn(conn),
		),
	)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create OTLP trace exporter").
			WithComponent("telemetry").
			WithOperation("createOTLPExporter").
			WithContext("endpoint", p.config.TraceEndpoint)
	}

	return exporter, nil
}

// initMeter 初始化 Meter
func (p *TelemetryProvider) initMeter(res *resource.Resource) *sdkmetric.MeterProvider {
	// 创建 Manual Reader
	reader := sdkmetric.NewManualReader()

	// 创建 Meter Provider
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)

	return meterProvider
}

// GetTracer 获取 Tracer
func (p *TelemetryProvider) GetTracer(name string) trace.Tracer {
	if p.tracerProvider == nil {
		return trace.NewNoopTracerProvider().Tracer(name)
	}
	return p.tracerProvider.Tracer(name)
}

// GetMeter 获取 Meter
func (p *TelemetryProvider) GetMeter(name string) metric.Meter {
	if p.meterProvider == nil {
		// Return a noop meter provider's meter
		return sdkmetric.NewMeterProvider().Meter(name)
	}
	return p.meterProvider.Meter(name)
}

// Shutdown 关闭
func (p *TelemetryProvider) Shutdown(ctx context.Context) error {
	var err error

	if p.tracerProvider != nil {
		if shutdownErr := p.tracerProvider.Shutdown(ctx); shutdownErr != nil {
			err = agentErrors.Wrap(shutdownErr, agentErrors.CodeInternal, "failed to shutdown tracer provider").
				WithComponent("telemetry").
				WithOperation("Shutdown")
		}
	}

	if p.meterProvider != nil {
		if shutdownErr := p.meterProvider.Shutdown(ctx); shutdownErr != nil {
			if err != nil {
				// Combine errors - append the meter provider shutdown error
				meterErr := agentErrors.Wrap(shutdownErr, agentErrors.CodeInternal, "failed to shutdown meter provider").
					WithComponent("telemetry").
					WithOperation("Shutdown")
				err = agentErrors.Wrap(err, agentErrors.CodeInternal, meterErr.Error()).
					WithComponent("telemetry").
					WithOperation("Shutdown")
			} else {
				err = agentErrors.Wrap(shutdownErr, agentErrors.CodeInternal, "failed to shutdown meter provider").
					WithComponent("telemetry").
					WithOperation("Shutdown")
			}
		}
	}

	return err
}

// ForceFlush 强制刷新
func (p *TelemetryProvider) ForceFlush(ctx context.Context) error {
	var err error

	if p.tracerProvider != nil {
		if flushErr := p.tracerProvider.ForceFlush(ctx); flushErr != nil {
			err = agentErrors.Wrap(flushErr, agentErrors.CodeInternal, "failed to flush tracer provider").
				WithComponent("telemetry").
				WithOperation("ForceFlush")
		}
	}

	if p.meterProvider != nil {
		if flushErr := p.meterProvider.ForceFlush(ctx); flushErr != nil {
			if err != nil {
				// Combine errors - append the meter provider flush error
				meterErr := agentErrors.Wrap(flushErr, agentErrors.CodeInternal, "failed to flush meter provider").
					WithComponent("telemetry").
					WithOperation("ForceFlush")
				err = agentErrors.Wrap(err, agentErrors.CodeInternal, meterErr.Error()).
					WithComponent("telemetry").
					WithOperation("ForceFlush")
			} else {
				err = agentErrors.Wrap(flushErr, agentErrors.CodeInternal, "failed to flush meter provider").
					WithComponent("telemetry").
					WithOperation("ForceFlush")
			}
		}
	}

	return err
}

// noopExporter 空操作导出器
type noopExporter struct{}

func (e *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *noopExporter) Shutdown(ctx context.Context) error {
	return nil
}
