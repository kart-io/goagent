package providers

import (
	"context"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

// BaseProvider is now an alias to common.BaseProvider for backward compatibility.
// Deprecated: Use common.BaseProvider directly.
type BaseProvider = common.BaseProvider

// NewBaseProvider is now an alias to common.NewBaseProvider for backward compatibility.
// Deprecated: Use common.NewBaseProvider directly.
var NewBaseProvider = common.NewBaseProvider

// ConfigToOptions is now an alias to common.ConfigToOptions for backward compatibility.
// Deprecated: Use common.ConfigToOptions directly.
var ConfigToOptions = common.ConfigToOptions

// HTTPClientConfig is now an alias to common.HTTPClientConfig for backward compatibility.
// Deprecated: Use common.HTTPClientConfig directly.
type HTTPClientConfig = common.HTTPClientConfig

// RetryConfig is now an alias to common.RetryConfig for backward compatibility.
// Deprecated: Use common.RetryConfig directly.
type RetryConfig = common.RetryConfig

// DefaultRetryConfig is now an alias to common.DefaultRetryConfig for backward compatibility.
// Deprecated: Use common.DefaultRetryConfig directly.
var DefaultRetryConfig = common.DefaultRetryConfig

// ExecuteFunc is now an alias to common.ExecuteFunc for backward compatibility.
// Deprecated: Use common.ExecuteFunc directly.
type ExecuteFunc[T any] = common.ExecuteFunc[T]

// ExecuteWithRetry wraps common.ExecuteWithRetry for backward compatibility.
// Deprecated: Use common.ExecuteWithRetry directly.
func ExecuteWithRetry[T any](ctx context.Context, cfg RetryConfig, providerName string, execute ExecuteFunc[T]) (T, error) {
	return common.ExecuteWithRetry(ctx, cfg, providerName, execute)
}

// HTTPError is now an alias to common.HTTPError for backward compatibility.
// Deprecated: Use common.HTTPError directly.
type HTTPError = common.HTTPError

// ProviderCapabilities is now an alias to common.ProviderCapabilities for backward compatibility.
// Deprecated: Use common.ProviderCapabilities directly.
type ProviderCapabilities = common.ProviderCapabilities

// NewProviderCapabilities is now an alias to common.NewProviderCapabilities for backward compatibility.
// Deprecated: Use common.NewProviderCapabilities directly.
var NewProviderCapabilities = common.NewProviderCapabilities

// MapHTTPError is now an alias to common.MapHTTPError for backward compatibility.
// Deprecated: Use common.MapHTTPError directly.
var MapHTTPError = common.MapHTTPError

// RestyResponseToHTTPError is now an alias to common.RestyResponseToHTTPError for backward compatibility.
// Deprecated: Use common.RestyResponseToHTTPError directly.
var RestyResponseToHTTPError = common.RestyResponseToHTTPError

// MessageConverter is now an alias to common.MessageConverter for backward compatibility.
// Deprecated: Use common.MessageConverter directly.
type MessageConverter[T any] = common.MessageConverter[T]

// ConvertMessages wraps common.ConvertMessages for backward compatibility.
// Deprecated: Use common.ConvertMessages directly.
func ConvertMessages[T any](messages []agentllm.Message, converter MessageConverter[T]) []T {
	return common.ConvertMessages(messages, converter)
}

// StandardMessage is now an alias to common.StandardMessage for backward compatibility.
// Deprecated: Use common.StandardMessage directly.
type StandardMessage = common.StandardMessage

// ToStandardMessage is now an alias to common.ToStandardMessage for backward compatibility.
// Deprecated: Use common.ToStandardMessage directly.
var ToStandardMessage = common.ToStandardMessage

// ConvertToStandardMessages is now an alias to common.ConvertToStandardMessages for backward compatibility.
// Deprecated: Use common.ConvertToStandardMessages directly.
var ConvertToStandardMessages = common.ConvertToStandardMessages

// RoleMapper is now an alias to common.RoleMapper for backward compatibility.
// Deprecated: Use common.RoleMapper directly.
type RoleMapper = common.RoleMapper

// DefaultRoleMapper is now an alias to common.DefaultRoleMapper for backward compatibility.
// Deprecated: Use common.DefaultRoleMapper directly.
var DefaultRoleMapper = common.DefaultRoleMapper

// ConvertMessagesWithRoleMapping wraps common.ConvertMessagesWithRoleMapping for backward compatibility.
// Deprecated: Use common.ConvertMessagesWithRoleMapping directly.
func ConvertMessagesWithRoleMapping[T any](
	messages []agentllm.Message,
	roleMapper RoleMapper,
	converter func(msg agentllm.Message, mappedRole string) T,
) []T {
	return common.ConvertMessagesWithRoleMapping(messages, roleMapper, converter)
}

// MessagesToPrompt is now an alias to common.MessagesToPrompt for backward compatibility.
// Deprecated: Use common.MessagesToPrompt directly.
var MessagesToPrompt = common.MessagesToPrompt

// DefaultPromptFormatter is now an alias to common.DefaultPromptFormatter for backward compatibility.
// Deprecated: Use common.DefaultPromptFormatter directly.
var DefaultPromptFormatter = common.DefaultPromptFormatter

// secureRandomInt63n is now an alias to common.SecureRandomInt63n for backward compatibility.
// Deprecated: Use common.SecureRandomInt63n directly.
func secureRandomInt63n(n int64) int64 {
	return common.SecureRandomInt63n(n)
}
