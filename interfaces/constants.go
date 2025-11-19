// Package interfaces defines core constants used across all layers of the GoAgent framework.
// These constants are foundational and can be imported by any layer without violating
// the import layering rules (Layer 1 - Foundation).
package interfaces

// Message Roles define the role of a message in a conversation.
// These constants are used across LLM interactions, memory systems, and agent communications.
const (
	// RoleSystem represents system-level instructions or configuration messages
	RoleSystem = "system"
	// RoleUser represents messages from the end user
	RoleUser = "user"
	// RoleAssistant represents messages from the AI assistant
	RoleAssistant = "assistant"
	// RoleFunction represents function/tool call results
	RoleFunction = "function"
	// RoleTool represents tool execution messages
	RoleTool = "tool"
)

// Generic Context Keys are commonly used keys for context values across the framework.
const (
	// KeyKey represents a generic key identifier
	KeyKey = "key"
	// KeyValue represents a generic value
	KeyValue = "value"
	// KeyMetadata represents metadata information
	KeyMetadata = "metadata"
	// KeyAttributes represents additional attributes
	KeyAttributes = "attributes"
	// KeyContext represents contextual information
	KeyContext = "context"
	// KeyConfig represents configuration data
	KeyConfig = "config"
	// KeyOptions represents options or settings
	KeyOptions = "options"
)

// Common Separators and Delimiters
const (
	// SeparatorComma is a comma separator
	SeparatorComma = ","
	// SeparatorColon is a colon separator
	SeparatorColon = ":"
	// SeparatorNewline is a newline separator
	SeparatorNewline = "\n"
	// SeparatorSpace is a space separator
	SeparatorSpace = " "
	// SeparatorDash is a dash separator
	SeparatorDash = "-"
	// SeparatorUnderscore is an underscore separator
	SeparatorUnderscore = "_"
)

// Common String Values
const (
	// EmptyString represents an empty string
	EmptyString = ""
	// DefaultString represents a default string value
	DefaultString = "default"
	// UnknownString represents an unknown value
	UnknownString = "unknown"
	// NoneString represents no value
	NoneString = "none"
)

// Boolean String Representations
const (
	// StringTrue represents boolean true as a string
	StringTrue = "true"
	// StringFalse represents boolean false as a string
	StringFalse = "false"
)
