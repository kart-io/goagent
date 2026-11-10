package constants

import "time"

// 长期记忆的默认策略。
const (
	// DefaultMemoryRecallK 是每轮自动召回的记录数；MaxMemoryRecallK 是一次召回的上限。
	DefaultMemoryRecallK = 6
	MaxMemoryRecallK     = 50
	// DefaultMemoryRecallRunes 是注入请求的召回块（含钉住的核心记忆）的字符上限。
	DefaultMemoryRecallRunes = 4000
	// DefaultMemoryReconcileCandidates 是判定新事实时检索的相似旧记录数。
	DefaultMemoryReconcileCandidates = 5
	// DefaultMemoryRecencyHalfLife 是新近性得分减半的时间（MemoryBank 遗忘曲线）。
	DefaultMemoryRecencyHalfLife = 7 * 24 * time.Hour
	// DefaultMemoryReflectionThreshold 是触发反思的未反思情景累计重要度。
	DefaultMemoryReflectionThreshold = 3.0
	// DefaultMemoryInvalidRetention 是失效记录保留的时间，之后被 Prune 删除。
	DefaultMemoryInvalidRetention = 30 * 24 * time.Hour
	// DefaultMemoryJobAttempts 是抽取任务失败后的最多尝试次数。
	DefaultMemoryJobAttempts = 3
	// DefaultMemoryWorkerInterval 是 Worker 没有任务时的轮询间隔。
	DefaultMemoryWorkerInterval = 2 * time.Second
	// ToolMemorySearch、ToolMemorySave、ToolMemoryForget 是长期记忆工具名。
	ToolMemorySearch = "memory_search"
	ToolMemorySave   = "memory_save"
	ToolMemoryForget = "memory_forget"
)
