// Package config 配置结构定义
//
// 配置结构独立于业务逻辑，便于维护和扩展
package config

// Config 应用配置
type Config struct {
	Name     string    `json:"Name"`
	Log      LogConfig `json:"Log"`
	Skill    SkillConf `json:"Skill"`
	Keywords []string  `json:"Keywords"`
	LLM      LLMConfig `json:"LLM"`
}

// LLMConfig LLM 配置
type LLMConfig struct {
	Provider    string  `json:"Provider"`    // 提供商：deepseek, openai
	Model       string  `json:"Model"`       // 模型名称
	MaxTokens   int     `json:"MaxTokens"`   // 最大 Token 数
	Temperature float64 `json:"Temperature"` // 温度参数
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `json:"Level"`
	Mode  string `json:"Mode"`
}

// SkillConf 技能配置
type SkillConf struct {
	Name            string      `json:"Name"`
	Description     string      `json:"Description"`
	Version         string      `json:"Version"`
	DefaultCity     string      `json:"DefaultCity"`
	SupportedCities []string    `json:"SupportedCities"`
	Cache           CacheConfig `json:"Cache"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled bool `json:"Enabled"`
	TTL     int  `json:"TTL"`
}
