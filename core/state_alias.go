package core

import (
	"github.com/kart-io/goagent/core/state"
)

// Type aliases for backward compatibility
// These types have been moved to the state package

type (
	State      = state.State
	AgentState = state.AgentState
)

// Constructor functions
var (
	NewAgentState = state.NewAgentState
)
