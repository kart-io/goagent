package codec_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/codec/yaml"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/devtools"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/observability"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/middleware"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

// 可从配置文件加载的类型须给每个导出字段写 json 标签，函数、接口等不可加载的字段写 "-"；
// 漏写时字段名只能按 Go 名区分大小写匹配，与其余 snake_case 字段不一致。
func TestConfigTypesTagEveryField(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[contextpolicy.OffloadConfig](),
		reflect.TypeFor[builder.RetrievalConfig](),
		reflect.TypeFor[cache.CacheConfig](),
		reflect.TypeFor[durable.OperationPolicy](),
		reflect.TypeFor[core.RunnableConfig](),
		reflect.TypeFor[devtools.RecorderConfig](),
		reflect.TypeFor[common.HTTPClientConfig](),
		reflect.TypeFor[common.RetryConfig](),
		reflect.TypeFor[llm.LLMOptions](),
		reflect.TypeFor[llm.StreamOptions](),
		reflect.TypeFor[memory.Config](),
		reflect.TypeFor[memory.Retention](),
		reflect.TypeFor[memory.Weights](),
		reflect.TypeFor[memory.Record](),
		reflect.TypeFor[observability.TelemetryConfig](),
		reflect.TypeFor[tools.DispatchConfig](),
		reflect.TypeFor[middleware.CachingConfig](),
		reflect.TypeFor[middleware.RateLimitConfig](),
		reflect.TypeFor[httpclient.CachingConfig](),
		reflect.TypeFor[workflow.DurableConfig](),
	} {
		for f := range typ.Fields() {
			if f.IsExported() {
				_, ok := f.Tag.Lookup("json")
				require.True(t, ok, "%s.%s 缺少 json 标签", typ, f.Name)
			}
		}
	}
}

func TestLoadCacheConfigFromYAML(t *testing.T) {
	cfg := cache.DefaultCacheConfig()
	require.NoError(t, yaml.Unmarshal([]byte("default_ttl: 5m\n"), &cfg, json.Strict))
	require.Equal(t, 5*time.Minute, cfg.DefaultTTL)
	require.True(t, cfg.Enabled, "未写的字段保留默认值")
}

func TestDecodeLLMOptions(t *testing.T) {
	doc := "provider: openai\napi_key: sk-test\nmodel: gpt-test\ntemperature: 0\nstream_idle_timeout: 30s\nstream_timeout: 2m\n"
	opts := llm.DefaultLLMOptions()
	require.NoError(t, yaml.Unmarshal([]byte(doc), opts, json.Strict))
	require.Equal(t, "sk-test", opts.APIKey)
	require.True(t, opts.TemperatureSet, "显式写的 temperature: 0 不能丢")
	require.Zero(t, opts.Temperature)
	require.Equal(t, 30*time.Second, opts.StreamIdleTimeout)
	require.Equal(t, 2*time.Minute, opts.StreamTimeout)

	err := yaml.Unmarshal([]byte("provider: openai\napi_kye: sk-test\n"), llm.DefaultLLMOptions(), json.Strict)
	require.ErrorContains(t, err, "api_kye", "自定义解码方法内也要报告未知字段")
}
