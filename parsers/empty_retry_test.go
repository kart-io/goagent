package parsers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

// 没有可尝试的解析时应返回带原因的错误，而不是值为 nil 的 *AgentError。
func requireRealAgentError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var agentErr *agentErrors.AgentError
	require.ErrorAs(t, err, &agentErr)
	require.NotNil(t, agentErr, "错误不能是值为 nil 的 *AgentError")
	require.Equal(t, agentErrors.CodeParserFailed, agentErr.Code)
}

func TestChainOutputParserWithoutParsers(t *testing.T) {
	p := NewChainOutputParser[string]()
	_, err := p.Parse(context.Background(), "x")
	requireRealAgentError(t, err)
}

func TestReActParseWithRetryNonPositiveRetries(t *testing.T) {
	_, err := NewReActOutputParser().ParseWithRetry(context.Background(), "Final Answer: x", 0)
	requireRealAgentError(t, err)
}
