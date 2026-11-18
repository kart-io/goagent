// Model configuration error fields and provider error hints.

package errors

const (
	ErrorFieldAPIKey              = "api_key"
	MessageAPIKeyMissing          = "%s API key is required (set via config or environment variable)"
	MessageMaxRetriesExceeded     = "max retries exceeded"
	MessageBadRequest             = "bad request"
	MessageInvalidAPIKey          = "invalid API key"
	MessageAPIKeyLacksPermissions = "API key lacks required permissions"
	MessageModelNotFound          = "model not found"
)
