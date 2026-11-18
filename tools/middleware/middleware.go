package middleware

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// ToolInvoker is one tool call; middleware must preserve the combined
// semantics of result and error.
type ToolInvoker func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)

// ToolMiddlewareFunc receives the next invoker and returns a wrapper.
// Assembly runs in reverse order, so the first function in the list is
// outermost; per-call temporary state must stay inside the call closure.
type ToolMiddlewareFunc func(interfaces.Tool, ToolInvoker) ToolInvoker
