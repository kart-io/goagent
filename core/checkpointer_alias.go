package core

import (
	"github.com/kart-io/goagent/core/checkpoint"
)

// Type aliases for backward compatibility
// These types have been moved to the checkpoint package

type (
	Checkpointer                = checkpoint.Checkpointer
	InMemorySaver               = checkpoint.InMemorySaver
	CheckpointInfo              = checkpoint.CheckpointInfo
	CheckpointerConfig          = checkpoint.CheckpointerConfig
	CheckpointerWithAutoCleanup = checkpoint.CheckpointerWithAutoCleanup
)

// Constructor functions
var (
	NewInMemorySaver = checkpoint.NewInMemorySaver
)
