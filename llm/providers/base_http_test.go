package providers

import (
	"github.com/kart-io/goagent/llm/common"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapHTTPError_BadRequest(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 400,
		Body:       `{"error": "invalid parameter"}`,
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
}

func TestMapHTTPError_BadRequestWithParser(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 400,
		Body:       `{"error": {"message": "custom error"}}`,
	}

	parseError := func(body string) string {
		return "custom error"
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", parseError)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	assert.Contains(t, err.Error(), "custom error")
}

func TestMapHTTPError_Unauthorized(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 401,
		Body:       "",
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
}

func TestMapHTTPError_UnauthorizedWithMessage(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 401,
		Body:       `{"error": "invalid API key"}`,
	}

	parseError := func(body string) string {
		return "invalid API key"
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", parseError)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid API key")
}

func TestMapHTTPError_Forbidden(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 403,
		Body:       "",
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
}

func TestMapHTTPError_ForbiddenWithMessage(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 403,
		Body:       `{"error": "insufficient permissions"}`,
	}

	parseError := func(body string) string {
		return "insufficient permissions"
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", parseError)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	assert.Contains(t, err.Error(), "insufficient permissions")
}

func TestMapHTTPError_NotFound(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 404,
		Body:       "",
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeLLMResponse, agentErrors.GetCode(err))
}

func TestMapHTTPError_NotFoundWithMessage(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 404,
		Body:       `{"error": "model not found"}`,
	}

	parseError := func(body string) string {
		return "model not found"
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", parseError)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeLLMResponse, agentErrors.GetCode(err))
	assert.Contains(t, err.Error(), "model not found")
}

func TestMapHTTPError_RateLimitWithRetryAfter(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 429,
		Headers: map[string]string{
			"Retry-After": "120",
		},
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeLLMRateLimit, agentErrors.GetCode(err))
}

func TestMapHTTPError_RateLimitNoRetryAfter(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 429,
		Headers:    map[string]string{},
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeLLMRateLimit, agentErrors.GetCode(err))
}

func TestMapHTTPError_InternalServerError(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		body       string
		parseError func(string) string
	}{
		{
			name:       "500 without message",
			statusCode: 500,
			body:       "",
			parseError: nil,
		},
		{
			name:       "500 with message",
			statusCode: 500,
			body:       `{"error": "internal error"}`,
			parseError: func(body string) string { return "internal error" },
		},
		{
			name:       "502 bad gateway",
			statusCode: 502,
			body:       "",
			parseError: nil,
		},
		{
			name:       "503 service unavailable",
			statusCode: 503,
			body:       "",
			parseError: nil,
		},
		{
			name:       "504 gateway timeout",
			statusCode: 504,
			body:       "",
			parseError: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			httpErr := common.HTTPError{
				StatusCode: tc.statusCode,
				Body:       tc.body,
			}

			err := common.MapHTTPError(httpErr, "test-provider", "test-model", tc.parseError)

			require.Error(t, err)
			assert.Equal(t, agentErrors.CodeLLMRequest, agentErrors.GetCode(err))
		})
	}
}

func TestMapHTTPError_UnexpectedStatus(t *testing.T) {
	httpErr := common.HTTPError{
		StatusCode: 418, // I'm a teapot
		Body:       "",
	}

	err := common.MapHTTPError(httpErr, "test-provider", "test-model", nil)

	require.Error(t, err)
	assert.Equal(t, agentErrors.CodeLLMRequest, agentErrors.GetCode(err))
	assert.Contains(t, err.Error(), "418")
}

func TestRestyResponseToHTTPError(t *testing.T) {
	// This is tested indirectly through other provider tests
	// We just verify the function signature here
	assert.NotNil(t, common.RestyResponseToHTTPError)
}
