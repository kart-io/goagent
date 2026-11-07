// 序列化、映射与数据交换字段常量。
package constants

// 通用标识与时间字段。
const (
	FieldID          = "id"
	FieldName        = "name"
	FieldDescription = "description"
	FieldType        = "type"
	FieldVersion     = "version"
	FieldTimestamp   = "timestamp"
	FieldCreatedAt   = "created_at"
	FieldUpdatedAt   = "updated_at"
	FieldDeletedAt   = "deleted_at"
)

// 请求与响应字段。
const (
	FieldInput    = "input"
	FieldOutput   = "output"
	FieldResult   = "result"
	FieldData     = "data"
	FieldMessage  = "message"
	FieldError    = "error"
	FieldErrors   = "errors"
	FieldStatus   = "status"
	FieldCode     = "code"
	FieldSuccess  = "success"
	FieldResponse = "response"
	FieldRequest  = "request"
)

// HTTP 请求字段。
const (
	FieldURL        = "url"
	FieldMethod     = "method"
	FieldHeaders    = "headers"
	FieldBody       = "body"
	FieldStatusCode = "status_code"
	FieldQuery      = "query"
	FieldParams     = "params"
	FieldPath       = "path"
	FieldHost       = "host"
	FieldPort       = "port"
	FieldScheme     = "scheme"
)

// 上下文、会话与组件字段。
const (
	FieldSessionID      = "session_id"
	FieldThreadID       = "thread_id"
	FieldConversationID = "conversation_id"
	FieldUserID         = "user_id"
	FieldAgentID        = "agent_id"
	FieldToolID         = "tool_id"
	FieldNamespace      = "namespace"
	FieldAgentName      = "agent_name"
	FieldToolName       = "tool_name"
	FieldModel          = "model"
	FieldProvider       = "provider"
	FieldEndpoint       = "endpoint"
	FieldOperation      = "operation"
	FieldComponent      = "component"
	FieldService        = "service"
)

// 指标与性能字段。
const (
	FieldDuration   = "duration"
	FieldLatency    = "latency"
	FieldCount      = "count"
	FieldTotal      = "total"
	FieldAverage    = "average"
	FieldMin        = "min"
	FieldMax        = "max"
	FieldRate       = "rate"
	FieldPercentage = "percentage"
	FieldSize       = "size"
	FieldBytes      = "bytes"
)

// 配置与分页字段。
const (
	FieldTimeout    = "timeout"
	FieldRetry      = "retry"
	FieldMaxRetries = "max_retries"
	FieldInterval   = "interval"
	FieldDelay      = "delay"
	FieldBatchSize  = "batch_size"
	FieldPageSize   = "page_size"
	FieldPage       = "page"
	FieldLimit      = "limit"
	FieldOffset     = "offset"
	FieldSort       = "sort"
	FieldOrder      = "order"
	FieldFilter     = "filter"
)

// 工具与函数字段。
const (
	FieldFunction   = "function"
	FieldArguments  = "arguments"
	FieldParameters = "parameters"
	FieldReturn     = "return"
	FieldCallback   = "callback"
	FieldSchema     = "schema"
	FieldProperties = "properties"
	FieldRequired   = "required"
)

// 状态与检查点字段。
const (
	FieldState      = "state"
	FieldCheckpoint = "checkpoint"
	FieldSnapshot   = "snapshot"
	FieldHistory    = "history"
	FieldPrevious   = "previous"
	FieldCurrent    = "current"
	FieldNext       = "next"
)

// 内容与媒体字段。
const (
	FieldContent    = "content"
	FieldText       = "text"
	FieldTitle      = "title"
	FieldSubtitle   = "subtitle"
	FieldAuthor     = "author"
	FieldSource     = "source"
	FieldFormat     = "format"
	FieldEncoding   = "encoding"
	FieldLanguage   = "language"
	FieldTags       = "tags"
	FieldCategories = "categories"
)
