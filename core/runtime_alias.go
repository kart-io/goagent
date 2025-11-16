package core

import (
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/state"
)

// Note: Runtime is a generic type and cannot be directly aliased.
// Users should import the execution package directly when using Runtime[C, S].
// For convenience, we provide a commonly used instantiation:

// DefaultRuntime is a commonly used Runtime instantiation
type DefaultRuntime = execution.Runtime[any, state.State]

// Runtime creation helper - users need to instantiate with their own types
// Example: execution.NewRuntime[MyContext, MyState](...)
// This is here for documentation purposes
const RuntimeNote = `
Runtime is a generic type from the execution package.
To use it, import "github.com/kart-io/goagent/core/execution"
and use execution.Runtime[YourContext, YourState] or execution.NewRuntime(...)
`
