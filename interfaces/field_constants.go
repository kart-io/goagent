// Package interfaces defines field name constants used across the GoAgent framework.
// These constants provide standardized field names for JSON serialization, map keys,
// and data structure access patterns.
package interfaces

// Common Field Names define frequently used field identifiers across the framework.
const (
	// FieldID represents a unique identifier
	FieldID = "id"
	// FieldName represents a name field
	FieldName = "name"
	// FieldDescription represents a description field
	FieldDescription = "description"
	// FieldType represents a type field
	FieldType = "type"
	// FieldVersion represents a version field
	FieldVersion = "version"
	// FieldTimestamp represents a timestamp field
	FieldTimestamp = "timestamp"
	// FieldCreatedAt represents a creation time field
	FieldCreatedAt = "created_at"
	// FieldUpdatedAt represents an update time field
	FieldUpdatedAt = "updated_at"
	// FieldDeletedAt represents a deletion time field
	FieldDeletedAt = "deleted_at"
)

// Request and Response Fields define data exchange field names.
const (
	// FieldInput represents input data
	FieldInput = "input"
	// FieldOutput represents output data
	FieldOutput = "output"
	// FieldResult represents a result value
	FieldResult = "result"
	// FieldData represents generic data
	FieldData = "data"
	// FieldMessage represents a message field
	FieldMessage = "message"
	// FieldError represents an error field
	FieldError = "error"
	// FieldErrors represents multiple errors
	FieldErrors = "errors"
	// FieldStatus represents a status field
	FieldStatus = "status"
	// FieldCode represents a code field (status code, error code, etc.)
	FieldCode = "code"
	// FieldSuccess represents a success indicator
	FieldSuccess = "success"
	// FieldResponse represents a response field
	FieldResponse = "response"
	// FieldRequest represents a request field
	FieldRequest = "request"
)

// HTTP Request Fields define HTTP-specific field names.
const (
	// FieldURL represents a URL field
	FieldURL = "url"
	// FieldMethod represents an HTTP method field
	FieldMethod = "method"
	// FieldHeaders represents HTTP headers
	FieldHeaders = "headers"
	// FieldBody represents a request/response body
	FieldBody = "body"
	// FieldStatusCode represents an HTTP status code
	FieldStatusCode = "status_code"
	// FieldQuery represents query parameters
	FieldQuery = "query"
	// FieldParams represents path parameters
	FieldParams = "params"
	// FieldPath represents a URL path
	FieldPath = "path"
	// FieldHost represents a host field
	FieldHost = "host"
	// FieldPort represents a port field
	FieldPort = "port"
	// FieldScheme represents a URL scheme (http/https)
	FieldScheme = "scheme"
)

// Context and Session Fields define context-specific field names.
const (
	// FieldSessionID represents a session identifier
	FieldSessionID = "session_id"
	// FieldThreadID represents a thread identifier
	FieldThreadID = "thread_id"
	// FieldConversationID represents a conversation identifier
	FieldConversationID = "conversation_id"
	// FieldUserID represents a user identifier
	FieldUserID = "user_id"
	// FieldAgentID represents an agent identifier
	FieldAgentID = "agent_id"
	// FieldToolID represents a tool identifier
	FieldToolID = "tool_id"
	// FieldNamespace represents a namespace field
	FieldNamespace = "namespace"
	// FieldAgentName represents an agent name
	FieldAgentName = "agent_name"
	// FieldToolName represents a tool name
	FieldToolName = "tool_name"
	// FieldModel represents an LLM model name
	FieldModel = "model"
	// FieldProvider represents an LLM provider name
	FieldProvider = "provider"
	// FieldEndpoint represents an API endpoint
	FieldEndpoint = "endpoint"
	// FieldOperation represents an operation name
	FieldOperation = "operation"
	// FieldComponent represents a component name
	FieldComponent = "component"
	// FieldService represents a service name
	FieldService = "service"
)

// Metrics and Performance Fields define measurement-related field names.
const (
	// FieldDuration represents a duration measurement
	FieldDuration = "duration"
	// FieldLatency represents latency measurement
	FieldLatency = "latency"
	// FieldCount represents a count value
	FieldCount = "count"
	// FieldTotal represents a total value
	FieldTotal = "total"
	// FieldAverage represents an average value
	FieldAverage = "average"
	// FieldMin represents a minimum value
	FieldMin = "min"
	// FieldMax represents a maximum value
	FieldMax = "max"
	// FieldRate represents a rate value
	FieldRate = "rate"
	// FieldPercentage represents a percentage value
	FieldPercentage = "percentage"
	// FieldSize represents a size measurement
	FieldSize = "size"
	// FieldBytes represents a bytes count
	FieldBytes = "bytes"
)

// Configuration and Settings Fields define configuration-related field names.
const (
	// FieldTimeout represents a timeout value
	FieldTimeout = "timeout"
	// FieldRetry represents a retry count
	FieldRetry = "retry"
	// FieldMaxRetries represents maximum retry attempts
	FieldMaxRetries = "max_retries"
	// FieldInterval represents an interval duration
	FieldInterval = "interval"
	// FieldDelay represents a delay duration
	FieldDelay = "delay"
	// FieldBatchSize represents a batch size value
	FieldBatchSize = "batch_size"
	// FieldPageSize represents a page size for pagination
	FieldPageSize = "page_size"
	// FieldPage represents a page number
	FieldPage = "page"
	// FieldLimit represents a limit value
	FieldLimit = "limit"
	// FieldOffset represents an offset value
	FieldOffset = "offset"
	// FieldSort represents a sort field
	FieldSort = "sort"
	// FieldOrder represents a sort order
	FieldOrder = "order"
	// FieldFilter represents a filter field
	FieldFilter = "filter"
)

// Tool and Function Fields define tool execution-related field names.
const (
	// FieldFunction represents a function name
	FieldFunction = "function"
	// FieldArguments represents function arguments
	FieldArguments = "arguments"
	// FieldParameters represents function parameters
	FieldParameters = "parameters"
	// FieldReturn represents a return value
	FieldReturn = "return"
	// FieldCallback represents a callback function
	FieldCallback = "callback"
	// FieldSchema represents a schema definition
	FieldSchema = "schema"
	// FieldProperties represents object properties
	FieldProperties = "properties"
	// FieldRequired represents required fields
	FieldRequired = "required"
)

// State and Checkpoint Fields define state management field names.
const (
	// FieldState represents a state value
	FieldState = "state"
	// FieldCheckpoint represents a checkpoint identifier
	FieldCheckpoint = "checkpoint"
	// FieldSnapshot represents a snapshot value
	FieldSnapshot = "snapshot"
	// FieldHistory represents historical data
	FieldHistory = "history"
	// FieldPrevious represents a previous value
	FieldPrevious = "previous"
	// FieldCurrent represents a current value
	FieldCurrent = "current"
	// FieldNext represents a next value
	FieldNext = "next"
)

// Content and Media Fields define content-related field names.
const (
	// FieldContent represents content data
	FieldContent = "content"
	// FieldText represents text content
	FieldText = "text"
	// FieldTitle represents a title field
	FieldTitle = "title"
	// FieldSubtitle represents a subtitle field
	FieldSubtitle = "subtitle"
	// FieldAuthor represents an author field
	FieldAuthor = "author"
	// FieldSource represents a source field
	FieldSource = "source"
	// FieldFormat represents a format field
	FieldFormat = "format"
	// FieldEncoding represents an encoding field
	FieldEncoding = "encoding"
	// FieldLanguage represents a language field
	FieldLanguage = "language"
	// FieldTags represents tags or labels
	FieldTags = "tags"
	// FieldCategories represents categories
	FieldCategories = "categories"
)
