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
	// DefaultMemoryGraphHops 与 DefaultMemoryGraphRelations 是召回关系时扩展的跳数与返回的关系数。
	DefaultMemoryGraphHops      = 2
	DefaultMemoryGraphRelations = 8
	// DefaultMemoryGraphCommunities 是召回时返回的社区摘要数。
	DefaultMemoryGraphCommunities = 2
	// DefaultMemoryEntityCandidates 与 DefaultMemoryEntitySimilarity 是实体判重时交给模型的候选数与候选的最低相似度
	// （名称向量余弦或字符对重合；Graphiti 的向量阈值为 0.6）。
	DefaultMemoryEntityCandidates = 5
	DefaultMemoryEntitySimilarity = 0.5
	// DefaultMemoryPageRankDamping 是 Personalized PageRank 沿边续走的概率（HippoRAG 取 0.5）；
	// MemoryPageRankIterations 与 MemoryPageRankTolerance 是迭代上限与收敛阈值（L1 距离）。
	DefaultMemoryPageRankDamping = 0.5
	MemoryPageRankIterations     = 50
	MemoryPageRankTolerance      = 1e-9
	// DefaultMemoryCommunityMinSize 是社区的最少实体数；DefaultMemoryCommunityFacts 是写摘要时取的最近关系数；
	// DefaultMemoryCommunityHubDegree 是枢纽实体的关系数下限，枢纽不参与社区划分；MemoryLabelPropagationRounds 是标签传播的轮数上限。
	DefaultMemoryCommunityMinSize   = 3
	DefaultMemoryCommunityFacts     = 40
	DefaultMemoryCommunityHubDegree = 12
	MemoryLabelPropagationRounds    = 20
	// MemoryJSONRepairAttempts 是记忆模型回复不是合法 JSON 时请它修正的次数。
	MemoryJSONRepairAttempts = 1
	// DefaultMemoryWorkerInterval 是 Worker 没有任务时的轮询间隔。
	DefaultMemoryWorkerInterval = 2 * time.Second
	// DefaultMemoryMaxStateBytes 是一个命名空间的 state 解压后的上限；只拒绝让 state 变大且超限的提交。
	DefaultMemoryMaxStateBytes = 8 << 20
	// MemoryCommitTimeout 是长期记忆一次提交（Save 与结果未知时的复查）的时间上限；超时按提交结果未知处理。
	MemoryCommitTimeout = 30 * time.Second
	// DefaultMemoryEraseWithin 是遗忘的内容从长期记忆的基础快照中物理擦除的时限。
	DefaultMemoryEraseWithin = 5 * time.Minute
	// DefaultMemoryStateCacheBytes 是进程内 state 解码缓存的容量（按估计的堆占用计）。
	DefaultMemoryStateCacheBytes = 64 << 20
	// ToolMemorySearch、ToolMemorySave、ToolMemoryForget 是长期记忆工具名。
	ToolMemorySearch = "memory_search"
	ToolMemorySave   = "memory_save"
	ToolMemoryForget = "memory_forget"
)
