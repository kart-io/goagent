package llm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
)

type reasoningClient struct {
	plainClient
	validated *CompletionRequest
}

func (c *reasoningClient) ValidateReasoning(req *CompletionRequest) error {
	c.validated = req
	return nil
}

// resolvingClient 用客户端默认值覆盖请求中的推理设置。
type resolvingClient struct {
	plainClient
	config *usage.ReasoningConfig
}

func (c resolvingClient) ResolveReasoning(*CompletionRequest) *usage.ReasoningConfig { return c.config }

func TestValidateReasoningRequest(t *testing.T) {
	high := &usage.ReasoningConfig{Effort: "high"}
	invalid := &usage.ReasoningConfig{Effort: "extreme"}
	cases := []struct {
		name     string
		client   Client
		req      *CompletionRequest
		wantCode agentErrors.ErrorCode // 空表示应成功
	}{
		{name: "请求为 nil", client: plainClient{}},
		{name: "未设置推理", client: plainClient{}, req: &CompletionRequest{}},
		{name: "不支持的客户端拒绝显式设置", client: plainClient{}, req: &CompletionRequest{Reasoning: high}, wantCode: agentErrors.CodeInvalidConfig},
		{name: "非法设置先于能力检查被拒绝", client: &reasoningClient{}, req: &CompletionRequest{Reasoning: invalid}, wantCode: agentErrors.CodeInvalidConfig},
		{name: "支持的客户端交给其校验", client: &reasoningClient{}, req: &CompletionRequest{Reasoning: high}},
		{name: "客户端默认值参与检查", client: resolvingClient{config: high}, req: &CompletionRequest{}, wantCode: agentErrors.CodeInvalidConfig},
		{name: "客户端默认值清空请求设置", client: resolvingClient{}, req: &CompletionRequest{Reasoning: high}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateReasoningRequest(c.client, c.req)
			if c.wantCode == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, c.wantCode, agentErrors.GetCode(err))
		})
	}

	client := &reasoningClient{}
	req := &CompletionRequest{Reasoning: high}
	require.NoError(t, ValidateReasoningRequest(client, req))
	require.Same(t, req, client.validated)
}
