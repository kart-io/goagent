package distributed

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/logger/core"
)

// Client 远程 Agent 客户端
// 负责调用远程服务的 Agent
type Client struct {
	client         *httpclient.Client
	logger         core.Logger
	circuitBreaker *CircuitBreaker
}

// NewClient 创建客户端
func NewClient(logger core.Logger) *Client {
	return NewClientWithCircuitBreaker(logger, DefaultCircuitBreakerConfig())
}

// NewClientWithCircuitBreaker 创建带自定义熔断器配置的客户端
func NewClientWithCircuitBreaker(logger core.Logger, cbConfig *CircuitBreakerConfig) *Client {
	client := httpclient.NewClient(&httpclient.Config{
		Timeout: 60 * time.Second,
		Headers: map[string]string{
			interfaces.HeaderContentType: interfaces.ContentTypeJSON,
			interfaces.HeaderAccept:      interfaces.ContentTypeJSON,
		},
	})

	// Configure circuit breaker with state change logging
	if cbConfig == nil {
		cbConfig = DefaultCircuitBreakerConfig()
	}

	// Wrap the original OnStateChange callback to add logging
	originalCallback := cbConfig.OnStateChange
	cbConfig.OnStateChange = func(from, to CircuitState) {
		logger.Info("Circuit breaker state changed",
			"from", from.String(),
			"to", to.String())

		if originalCallback != nil {
			originalCallback(from, to)
		}
	}

	return &Client{
		client:         client,
		logger:         logger.With("component", "agent-client"),
		circuitBreaker: NewCircuitBreaker(cbConfig),
	}
}

// ExecuteAgent 执行远程 Agent
func (c *Client) ExecuteAgent(ctx context.Context, endpoint, agentName string, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	var output *agentcore.AgentOutput

	// Execute through circuit breaker
	err := c.circuitBreaker.Execute(func() error {
		var execErr error
		output, execErr = c.executeAgentInternal(ctx, endpoint, agentName, input)
		return execErr
	})

	if err != nil {
		// If circuit is open, wrap the error with context
		if err == ErrCircuitOpen {
			return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "circuit breaker is open").
				WithComponent("distributed_client").
				WithOperation("execute_agent").
				WithContext(interfaces.FieldEndpoint, endpoint).
				WithContext(interfaces.FieldAgentName, agentName).
				WithContext("circuit_state", c.circuitBreaker.State().String()).
				WithContext("failures", c.circuitBreaker.Failures())
		}
		return nil, err
	}

	return output, nil
}

// executeAgentInternal is the internal implementation without circuit breaker
func (c *Client) executeAgentInternal(ctx context.Context, endpoint, agentName string, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	// 构建请求
	url := fmt.Sprintf("%s/api/v1/agents/%s/execute", endpoint, agentName)

	c.logger.Debug("Sending agent execution request",
		"endpoint", endpoint,
		"agent", agentName,
		"url", url)

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(input).
		Post(url)

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "failed to send request").
			WithComponent("distributed_client").
			WithOperation("execute_agent").
			WithContext(interfaces.FieldEndpoint, endpoint).
			WithContext(interfaces.FieldAgentName, agentName)
	}

	// 检查状态码
	if resp.StatusCode() != 200 {
		return nil, agentErrors.New(agentErrors.CodeAgentExecution, "agent execution failed").
			WithComponent("distributed_client").
			WithOperation("execute_agent").
			WithContext(interfaces.FieldStatusCode, resp.StatusCode()).
			WithContext(interfaces.FieldAgentName, agentName).
			WithContext("response_body", resp.String())
	}

	// 解析响应
	var output agentcore.AgentOutput
	if err := json.Unmarshal(resp.Body(), &output); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to unmarshal response").
			WithComponent("distributed_client").
			WithOperation("execute_agent").
			WithContext(interfaces.FieldAgentName, agentName)
	}

	return &output, nil
}

// ExecuteAgentAsync 异步执行远程 Agent
func (c *Client) ExecuteAgentAsync(ctx context.Context, endpoint, agentName string, input *agentcore.AgentInput) (string, error) {
	// 构建请求
	url := fmt.Sprintf("%s/api/v1/agents/%s/execute/async", endpoint, agentName)

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(input).
		Post(url)

	if err != nil {
		return "", agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "failed to send request").
			WithComponent("distributed_client").
			WithOperation("execute_agent_async").
			WithContext(interfaces.FieldEndpoint, endpoint).
			WithContext(interfaces.FieldAgentName, agentName)
	}

	// 检查状态码
	if resp.StatusCode() != 202 { // HTTP 202 Accepted
		return "", agentErrors.New(agentErrors.CodeAgentExecution, "async execution failed").
			WithComponent("distributed_client").
			WithOperation("execute_agent_async").
			WithContext(interfaces.FieldStatusCode, resp.StatusCode()).
			WithContext(interfaces.FieldAgentName, agentName).
			WithContext("response_body", resp.String())
	}

	// 解析响应获取任务 ID
	var result struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to unmarshal response").
			WithComponent("distributed_client").
			WithOperation("execute_agent_async").
			WithContext(interfaces.FieldAgentName, agentName)
	}

	return result.TaskID, nil
}

// GetAsyncResult 获取异步执行结果
func (c *Client) GetAsyncResult(ctx context.Context, endpoint, taskID string) (*agentcore.AgentOutput, bool, error) {
	// 构建请求
	url := fmt.Sprintf("%s/api/v1/agents/tasks/%s", endpoint, taskID)

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		Get(url)

	if err != nil {
		return nil, false, agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "failed to send request").
			WithComponent("distributed_client").
			WithOperation("get_async_result").
			WithContext(interfaces.FieldEndpoint, endpoint).
			WithContext("task_id", taskID)
	}

	// 检查状态码
	if resp.StatusCode() == 202 { // HTTP 202 Accepted
		// 任务仍在执行中
		return nil, false, nil
	}

	if resp.StatusCode() != 200 {
		return nil, false, agentErrors.New(agentErrors.CodeAgentExecution, "failed to get result").
			WithComponent("distributed_client").
			WithOperation("get_async_result").
			WithContext(interfaces.FieldStatusCode, resp.StatusCode()).
			WithContext("task_id", taskID).
			WithContext("response_body", resp.String())
	}

	// 解析响应
	var output agentcore.AgentOutput
	if err := json.Unmarshal(resp.Body(), &output); err != nil {
		return nil, false, agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to unmarshal response").
			WithComponent("distributed_client").
			WithOperation("get_async_result").
			WithContext("task_id", taskID)
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

	resp, err := c.client.R().
		SetContext(ctx).
		Get(url)

	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "failed to send request").
			WithComponent("distributed_client").
			WithOperation("ping").
			WithContext(interfaces.FieldEndpoint, endpoint)
	}

	if resp.StatusCode() != 200 {
		return agentErrors.New(agentErrors.CodeDistributedHeartbeat, "health check failed").
			WithComponent("distributed_client").
			WithOperation("ping").
			WithContext(interfaces.FieldStatusCode, resp.StatusCode()).
			WithContext(interfaces.FieldEndpoint, endpoint)
	}

	return nil
}

// ListAgents 列出服务支持的所有 Agent
func (c *Client) ListAgents(ctx context.Context, endpoint string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/agents", endpoint)

	resp, err := c.client.R().
		SetContext(ctx).
		Get(url)

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedConnection, "failed to send request").
			WithComponent("distributed_client").
			WithOperation("list_agents").
			WithContext("endpoint", endpoint)
	}

	if resp.StatusCode() != 200 {
		return nil, agentErrors.New(agentErrors.CodeAgentExecution, "failed to list agents").
			WithComponent("distributed_client").
			WithOperation("list_agents").
			WithContext("status_code", resp.StatusCode()).
			WithContext("endpoint", endpoint)
	}

	var result struct {
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to unmarshal response").
			WithComponent("distributed_client").
			WithOperation("list_agents")
	}

	return result.Agents, nil
}

// CircuitBreaker returns the client's circuit breaker for monitoring
func (c *Client) CircuitBreaker() *CircuitBreaker {
	return c.circuitBreaker
}
