package core

import (
	"github.com/kart-io/goagent/core/execution"
)

// Type aliases for backward compatibility
// These types have been moved to the execution package

type (
	LegacyStreamChunk  = execution.LegacyStreamChunk
	StreamOptions      = execution.StreamOptions
	StreamConsumer     = execution.StreamConsumer
	ChunkTransformFunc = execution.ChunkTransformFunc
	StreamOutput       = execution.StreamOutput
	ChunkType          = execution.ChunkType
	ChunkMetadata      = execution.ChunkMetadata
	StreamStatus       = execution.StreamStatus
	StreamState        = execution.StreamState
)

// Constants
const (
	ChunkTypeText     = execution.ChunkTypeText
	ChunkTypeError    = execution.ChunkTypeError
	ChunkTypeJSON     = execution.ChunkTypeJSON
	ChunkTypeProgress = execution.ChunkTypeProgress
	ChunkTypeBinary   = execution.ChunkTypeBinary
	ChunkTypeControl  = execution.ChunkTypeControl
	ChunkTypeStatus   = execution.ChunkTypeStatus

	StreamStateRunning  = execution.StreamStateRunning
	StreamStatePaused   = execution.StreamStatePaused
	StreamStateError    = execution.StreamStateError
	StreamStateComplete = execution.StreamStateComplete
	StreamStateClosed   = execution.StreamStateClosed
)

// Functions
var (
	DefaultStreamOptions = execution.DefaultStreamOptions
	NewTextChunk         = execution.NewTextChunk
	NewProgressChunk     = execution.NewProgressChunk
	NewErrorChunk        = execution.NewErrorChunk
)
