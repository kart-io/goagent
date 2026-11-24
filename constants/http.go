package constants

// HTTP methods.
const (
	MethodGet    = "GET"
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodDelete = "DELETE"
	MethodPatch  = "PATCH"
)

// HTTP content types.
const (
	ContentTypeJSON        = "application/json"
	ContentTypeOctetStream = "application/octet-stream"
)

// HTTP header names.
const (
	HeaderContentType   = "Content-Type"
	HeaderAuthorization = "Authorization"
	HeaderUserAgent     = "User-Agent"
)

// HTTP authentication schemes.
const (
	Bearer = "Bearer"
)
