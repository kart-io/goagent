// Package config 配置结构定义
//
// 配置结构独立于业务逻辑，便于维护和扩展
package config

// Config 应用配置
type Config struct {
	Name     string    `yaml:"Name"`
	Log      LogConfig `yaml:"Log"`
	Skill    SkillConf `yaml:"Skill"`
	Keywords []string  `yaml:"Keywords"`
	LLM      LLMConfig `yaml:"LLM"`
}

// LLMConfig LLM 配置
type LLMConfig struct {
	Provider    string  `yaml:"Provider"`    // 提供商：deepseek, openai
	Model       string  `yaml:"Model"`       // 模型名称
	MaxTokens   int     `yaml:"MaxTokens"`   // 最大 Token 数
	Temperature float64 `yaml:"Temperature"` // 温度参数
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `yaml:"Level"`
	Mode  string `yaml:"Mode"`
}

// SkillConf 技能配置
type SkillConf struct {
	Name            string      `yaml:"Name"`
	Description     string      `yaml:"Description"`
	Version         string      `yaml:"Version"`
	DefaultCity     string      `yaml:"DefaultCity"`
	SupportedCities []string    `yaml:"SupportedCities"`
	Cache           CacheConfig `yaml:"Cache"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled bool `yaml:"Enabled"`
	TTL     int  `yaml:"TTL"`
}
