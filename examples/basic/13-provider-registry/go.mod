module github.com/kart-io/goagent/examples/basic/13-provider-registry

go 1.25.0

require (
	github.com/kart-io/goagent v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/anthropic v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/cohere v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/deepseek v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/gemini v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/huggingface v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/kimi v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/ollama v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/openai v0.1.0
	github.com/kart-io/goagent/contrib/llm-providers/siliconflow v0.1.0
)

require (
	cloud.google.com/go v0.121.2 // indirect
	cloud.google.com/go/aiplatform v1.90.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.5.2 // indirect
	cloud.google.com/go/longrunning v0.6.7 // indirect
	cloud.google.com/go/vertexai v0.15.0 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.14.2 // indirect
	github.com/bytedance/sonic/loader v0.4.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-resty/resty/v2 v2.16.5 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.7 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/sashabaranov/go-openai v1.41.2 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	golang.org/x/arch v0.3.0 // indirect
	golang.org/x/crypto v0.44.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/oauth2 v0.33.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/api v0.256.0 // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251020155222-88f65dc88635 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251111163417-95abcf5c77ba // indirect
	google.golang.org/grpc v1.76.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/kart-io/goagent => ../../../

replace github.com/kart-io/goagent/contrib/llm-providers/openai => ../../../contrib/llm-providers/openai

replace github.com/kart-io/goagent/contrib/llm-providers/deepseek => ../../../contrib/llm-providers/deepseek

replace github.com/kart-io/goagent/contrib/llm-providers/gemini => ../../../contrib/llm-providers/gemini

replace github.com/kart-io/goagent/contrib/llm-providers/anthropic => ../../../contrib/llm-providers/anthropic

replace github.com/kart-io/goagent/contrib/llm-providers/cohere => ../../../contrib/llm-providers/cohere

replace github.com/kart-io/goagent/contrib/llm-providers/huggingface => ../../../contrib/llm-providers/huggingface

replace github.com/kart-io/goagent/contrib/llm-providers/ollama => ../../../contrib/llm-providers/ollama

replace github.com/kart-io/goagent/contrib/llm-providers/kimi => ../../../contrib/llm-providers/kimi

replace github.com/kart-io/goagent/contrib/llm-providers/siliconflow => ../../../contrib/llm-providers/siliconflow
