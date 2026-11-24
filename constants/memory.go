package constants

import "time"

// Long-term memory defaults.
const (
	// DefaultMemoryRecallK is the number of records recalled per turn;
	// MaxMemoryRecallK caps a single recall.
	DefaultMemoryRecallK = 6
	MaxMemoryRecallK     = 50
	// DefaultMemoryRecallRunes caps the runes of the recall block injected into
	// a request, pinned core memories included.
	DefaultMemoryRecallRunes = 4000
	// DefaultMemoryReconcileCandidates is how many similar records are fetched
	// when reconciling a new fact.
	DefaultMemoryReconcileCandidates = 5
	// DefaultMemoryRecencyHalfLife is the recency score half-life (MemoryBank
	// forgetting curve).
	DefaultMemoryRecencyHalfLife = 7 * 24 * time.Hour
	// DefaultMemoryReflectionThreshold is the accumulated importance of
	// unreflected episodes that triggers reflection.
	DefaultMemoryReflectionThreshold = 3.0
	// DefaultMemoryInvalidRetention is how long invalidated records are kept
	// before Prune deletes them.
	DefaultMemoryInvalidRetention = 30 * 24 * time.Hour
	// DefaultMemoryJobAttempts is the maximum attempts of an extraction job.
	DefaultMemoryJobAttempts = 3
	// DefaultMemoryGraphHops and DefaultMemoryGraphRelations are the hops
	// expanded and relations returned when recalling relations.
	DefaultMemoryGraphHops      = 2
	DefaultMemoryGraphRelations = 8
	// DefaultMemoryGraphCommunities is the number of community summaries
	// returned on recall.
	DefaultMemoryGraphCommunities = 2
	// DefaultMemoryEntityCandidates and DefaultMemoryEntitySimilarity are the
	// candidates handed to the model for entity deduplication and their minimum
	// similarity (name embedding cosine or bigram overlap; Graphiti uses 0.6).
	DefaultMemoryEntityCandidates = 5
	DefaultMemoryEntitySimilarity = 0.5
	// DefaultMemoryPageRankDamping is the Personalized PageRank continuation
	// probability (HippoRAG uses 0.5); MemoryPageRankIterations and
	// MemoryPageRankTolerance are the iteration cap and L1 convergence threshold.
	DefaultMemoryPageRankDamping = 0.5
	MemoryPageRankIterations     = 50
	MemoryPageRankTolerance      = 1e-9
	// DefaultMemoryCommunityMinSize is the minimum entities per community;
	// DefaultMemoryCommunityFacts is the recent relations used for a summary;
	// DefaultMemoryCommunityHubDegree is the relation count from which an entity
	// is a hub excluded from communities; MemoryLabelPropagationRounds caps label
	// propagation rounds.
	DefaultMemoryCommunityMinSize   = 3
	DefaultMemoryCommunityFacts     = 40
	DefaultMemoryCommunityHubDegree = 12
	MemoryLabelPropagationRounds    = 20
	// MemoryJSONRepairAttempts is how often the memory model is asked to repair
	// a reply that is not valid JSON.
	MemoryJSONRepairAttempts = 1
	// DefaultMemoryWorkerInterval is the Worker poll interval when idle.
	DefaultMemoryWorkerInterval = 2 * time.Second
	// DefaultMemoryMaxStateBytes caps a namespace state after decompression;
	// only commits that grow the state past the cap are rejected.
	DefaultMemoryMaxStateBytes = 8 << 20
	// MemoryCommitTimeout bounds one long-term memory commit (Save plus the
	// recheck when the outcome is unknown); a timeout counts as unknown.
	MemoryCommitTimeout = 30 * time.Second
	// DefaultMemoryEraseWithin is the deadline for physically erasing forgotten
	// content from the long-term memory base snapshot.
	DefaultMemoryEraseWithin = 5 * time.Minute
	// DefaultMemoryRetireGrace is how long a purged scope stays retiring before
	// the index is cleaned again to drop late vectors.
	DefaultMemoryRetireGrace = 10 * time.Minute
	// DefaultMemoryReindexEvery is how many maintenance rounds pass between
	// Reindex(false) of registered namespaces and expired tombstone collection.
	DefaultMemoryReindexEvery = 30
	// MemoryTombstoneRetention is how long tombstones of deleted scopes are kept;
	// it must far exceed the late-write retry window (commit timeout,
	// RetireGrace).
	MemoryTombstoneRetention = 24 * time.Hour
	// DefaultMemoryStateCacheBytes is the in-process decoded state cache
	// capacity, measured by estimated heap usage.
	DefaultMemoryStateCacheBytes = 64 << 20
	// Long-term memory tool names.
	ToolMemorySearch  = "memory_search"
	ToolMemorySave    = "memory_save"
	ToolMemoryReplace = "memory_replace"
	ToolMemoryForget  = "memory_forget"
)
