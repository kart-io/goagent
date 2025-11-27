package core

import (
	"github.com/kart-io/goagent/core/state"
)

// Type aliases for internal use within core package
type (
	State      = state.State
	AgentState = state.AgentState
)

// Constructor functions for internal use
var (
	NewAgentState = state.NewAgentState
)
