package constants

// Skill loading tool name and resource limits. Limits are in bytes or
// entries, not model tokens.
const (
	ToolLoadSkill          = "load_skill"
	SkillMaxDocumentBytes  = 1 << 20
	SkillMaxReferenceBytes = 4 << 20
	SkillMaxEntries        = 10000
	SkillMaxCount          = 2000
	SkillMaxResourceDepth  = 32
)
