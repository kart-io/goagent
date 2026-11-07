// 通用角色、上下文键与字符串常量。
package constants

// 消息角色。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleFunction  = "function"
	RoleTool      = "tool"
)

// 上下文与配置键。
const (
	KeyKey        = "key"
	KeyValue      = "value"
	KeyMetadata   = "metadata"
	KeyAttributes = "attributes"
	KeyContext    = "context"
	KeyConfig     = "config"
	KeyOptions    = "options"
)

// 分隔符。
const (
	SeparatorComma      = ","
	SeparatorColon      = ":"
	SeparatorNewline    = "\n"
	SeparatorSpace      = " "
	SeparatorDash       = "-"
	SeparatorUnderscore = "_"
)

// 通用字符串值。
const (
	EmptyString   = ""
	DefaultString = "default"
	UnknownString = "unknown"
	NoneString    = "none"
)

// 布尔值的字符串表示。
const (
	StringTrue  = "true"
	StringFalse = "false"
)
