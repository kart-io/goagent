package constants

// Skills 只读加载的工具名与资源上限。上限按字节/条目计，不是模型 token 预算。
const (
	ToolLoadSkill          = "load_skill"
	SkillMaxDocumentBytes  = 1 << 20
	SkillMaxReferenceBytes = 4 << 20
	SkillMaxEntries        = 10000
	SkillMaxCount          = 2000
	SkillMaxResourceDepth  = 32
)
