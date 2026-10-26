// Package agentbench 提供 GoAgent 的可复现任务评测与外部请求计量。
package agentbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/kart-io/goagent/evaluation"
	"github.com/kart-io/goagent/interfaces"
)

// Meter 在SDK 之外执行请求预算和用量计量，包含重试请求。
// 每个实验使用独立实例，Token 阈值只阻止后续请求，不是硬性计费上限。
type Meter struct {
	Endpoint     string
	Budget       evaluation.Budget
	Client       *http.Client
	mu           sync.Mutex
	measurements evaluation.Measurements
}

func (m *Meter) Measurements() evaluation.Measurements {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.measurements
}

func (m *Meter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
		http.Error(w, "仅支持聊天补全", http.StatusNotFound)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		http.Error(w, "请求读取失败", http.StatusBadRequest)
		return
	}
	var body map[string]any
	if json.Unmarshal(data, &body) != nil || body == nil {
		http.Error(w, "请求必须为 JSON 对象", http.StatusBadRequest)
		return
	}
	// 统一模型和生成预算，调用方提示词与工具协议保留原样。
	body["model"] = m.Budget.Model
	body["max_tokens"] = m.Budget.MaxOutputTokens
	delete(body, "max_completion_tokens")
	body["temperature"] = m.Budget.Temperature
	if body["stream"] == true {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	data, err = json.Marshal(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	if m.Budget.MaxCalls <= 0 || m.Budget.MaxOutputTokens <= 0 || m.Budget.Model == "" {
		m.mu.Unlock()
		http.Error(w, "预算无效", http.StatusBadRequest)
		return
	}
	if m.measurements.Calls >= m.Budget.MaxCalls || (m.Budget.ObservedTokens > 0 && m.measurements.Usage.TotalTokens >= m.Budget.ObservedTokens) {
		m.mu.Unlock()
		http.Error(w, "评测预算耗尽", http.StatusTooManyRequests)
		return
	}
	m.measurements.Calls++
	m.mu.Unlock()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(m.Endpoint, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		http.Error(w, "上游地址无效", http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	client := m.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		http.Error(w, "上游请求失败", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.WriteHeader(response.StatusCode)
	var usage *interfaces.TokenUsage
	defer func() {
		if usage != nil {
			m.mu.Lock()
			m.measurements.Usage.Add(usage)
			m.measurements.UsageReports++
			m.mu.Unlock()
		}
	}()
	capture := func(data []byte) {
		var envelope struct {
			Usage *interfaces.TokenUsage `json:"usage"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Usage != nil {
			usage = envelope.Usage
		}
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		completed := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "data: [DONE]" {
				completed = true
			}
			if strings.HasPrefix(line, "data:") {
				capture([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		// 流损坏必须让 SDK 看到错误帧，不能把截断当作正常结束。
		if err := scanner.Err(); err != nil || !completed {
			_, _ = fmt.Fprint(w, "data: {\"error\":{\"message\":\"上游事件流读取失败\",\"type\":\"stream_error\"}}\n\n")
		}
		return
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err == nil {
		capture(data)
		_, _ = w.Write(data)
	}
}
