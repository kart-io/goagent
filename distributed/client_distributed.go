package distributed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger/core"
)

// Client 远程 Agent 客户端
// 负责调用远程服务的 Agent
type Client struct {
	httpClient *http.Client
	logger     core.Logger
}

// NewClient 创建客户端
func NewClient(logger core.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: logger.With("component", "agent-client"),
	}
}

// ExecuteAgent 执行远程 Agent
func (c *Client) ExecuteAgent(ctx context.Context, endpoint, agentName string, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	// 构建请求
	url := fmt.Sprintf("%s/api/v1/agents/%s/execute", endpoint, agentName)

	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	c.logger.Debug("Sending agent execution request",
		"endpoint", endpoint,
		"agent", agentName,
		"url", url)

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warnw("Failed to close response body", "error", closeErr)
		}
	}()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent execution failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var output agentcore.AgentOutput
	if err := json.Unmarshal(respBody, &output); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &output, nil
}

// ExecuteAgentAsync 异步执行远程 Agent
func (c *Client) ExecuteAgentAsync(ctx context.Context, endpoint, agentName string, input *agentcore.AgentInput) (string, error) {
	// 构建请求
	url := fmt.Sprintf("%s/api/v1/agents/%s/execute/async", endpoint, agentName)

	body, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warnw("Failed to close response body", "error", closeErr)
		}
	}()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// 检查状态码
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("async execution failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应获取任务 ID
	var result struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result.TaskID, nil
}

// GetAsyncResult 获取异步执行结果
func (c *Client) GetAsyncResult(ctx context.Context, endpoint, taskID string) (*agentcore.AgentOutput, bool, error) {
	// 构建请求
	url := fmt.Sprintf("%s/api/v1/agents/tasks/%s", endpoint, taskID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warnw("Failed to close response body", "error", closeErr)
		}
	}()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", err)
	}

	// 检查状态码
	if resp.StatusCode == http.StatusAccepted {
		// 任务仍在执行中
		return nil, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("failed to get result with status %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var output agentcore.AgentOutput
	if err := json.Unmarshal(respBody, &output); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &output, true, nil
}

// WaitForAsyncResult 等待异步执行完成
func (c *Client) WaitForAsyncResult(ctx context.Context, endpoint, taskID string, pollInterval time.Duration) (*agentcore.AgentOutput, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			output, completed, err := c.GetAsyncResult(ctx, endpoint, taskID)
			if err != nil {
				return nil, err
			}

			if completed {
				return output, nil
			}

			c.logger.Debug("Async task still running", "task_id", taskID)
		}
	}
}

// Ping 检查服务健康状态
func (c *Client) Ping(ctx context.Context, endpoint string) error {
	url := fmt.Sprintf("%s/health", endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warnw("Failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

// ListAgents 列出服务支持的所有 Agent
func (c *Client) ListAgents(ctx context.Context, endpoint string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/agents", endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warnw("Failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list agents with status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result.Agents, nil
}
