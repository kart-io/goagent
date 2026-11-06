package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kart-io/goagent/codec/json"

	"github.com/sashabaranov/go-openai"
)

type zeroTemperatureKey struct{}

func withOpenAITemperature(ctx context.Context, temperature float64) context.Context {
	return context.WithValue(ctx, zeroTemperatureKey{}, temperature == 0)
}

// zeroTemperatureDoer 补齐 SDK 的 omitempty 丢失的零，不修改 SDK、非零请求或其他端点。
// 使用 RawMessage 保留工具参数中的大整数等字段，不经 float64 往返。
type zeroTemperatureDoer struct{ next openai.HTTPDoer }

func (d zeroTemperatureDoer) Do(request *http.Request) (*http.Response, error) {
	zero, _ := request.Context().Value(zeroTemperatureKey{}).(bool)
	if !zero || request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		return d.next.Do(request)
	}
	data, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("读取零温请求：%w", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("解析零温请求：%w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("零温请求必须为 JSON 对象")
	}
	payload["temperature"] = json.RawMessage("0")
	data, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request = request.Clone(request.Context())
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.ContentLength = int64(len(data))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	request.Header.Del("Content-Length")
	return d.next.Do(request)
}
