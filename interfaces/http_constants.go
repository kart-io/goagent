// Package interfaces defines HTTP-related constants used across the GoAgent framework.
// These constants provide standardized HTTP methods, headers, content types, and status codes.
package interfaces

// HTTP Methods define standard HTTP request methods.
const (
	// MethodGet represents the HTTP GET method
	MethodGet = "GET"
	// MethodPost represents the HTTP POST method
	MethodPost = "POST"
	// MethodPut represents the HTTP PUT method
	MethodPut = "PUT"
	// MethodDelete represents the HTTP DELETE method
	MethodDelete = "DELETE"
	// MethodPatch represents the HTTP PATCH method
	MethodPatch = "PATCH"
	// MethodHead represents the HTTP HEAD method
	MethodHead = "HEAD"
	// MethodOptions represents the HTTP OPTIONS method
	MethodOptions = "OPTIONS"
	// MethodConnect represents the HTTP CONNECT method
	MethodConnect = "CONNECT"
	// MethodTrace represents the HTTP TRACE method
	MethodTrace = "TRACE"
)

// Content Types define standard MIME types for HTTP requests and responses.
const (
	// ContentTypeJSON represents JSON content type
	ContentTypeJSON = "application/json"
	// ContentTypeText represents plain text content type
	ContentTypeText = "text/plain"
	// ContentTypeHTML represents HTML content type
	ContentTypeHTML = "text/html"
	// ContentTypeXML represents XML content type
	ContentTypeXML = "application/xml"
	// ContentTypeForm represents URL-encoded form data
	ContentTypeForm = "application/x-www-form-urlencoded"
	// ContentTypeMultipart represents multipart form data
	ContentTypeMultipart = "multipart/form-data"
	// ContentTypeEventStream represents server-sent events stream
	ContentTypeEventStream = "text/event-stream"
	// ContentTypeOctetStream represents binary data
	ContentTypeOctetStream = "application/octet-stream"
)

// HTTP Headers define standard HTTP header names.
const (
	// HeaderContentType represents the Content-Type header
	HeaderContentType = "Content-Type"
	// HeaderAccept represents the Accept header
	HeaderAccept = "Accept"
	// HeaderAuthorization represents the Authorization header
	HeaderAuthorization = "Authorization"
	// HeaderUserAgent represents the User-Agent header
	HeaderUserAgent = "User-Agent"
	// HeaderAcceptEncoding represents the Accept-Encoding header
	HeaderAcceptEncoding = "Accept-Encoding"
	// HeaderContentEncoding represents the Content-Encoding header
	HeaderContentEncoding = "Content-Encoding"
	// HeaderCacheControl represents the Cache-Control header
	HeaderCacheControl = "Cache-Control"
	// HeaderConnection represents the Connection header
	HeaderConnection = "Connection"
	// HeaderCookie represents the Cookie header
	HeaderCookie = "Cookie"
	// HeaderSetCookie represents the Set-Cookie header
	HeaderSetCookie = "Set-Cookie"
	// HeaderLocation represents the Location header
	HeaderLocation = "Location"
	// HeaderReferer represents the Referer header
	HeaderReferer = "Referer"
	// HeaderXForwardedFor represents the X-Forwarded-For header
	HeaderXForwardedFor = "X-Forwarded-For"
	// HeaderXRealIP represents the X-Real-IP header
	HeaderXRealIP = "X-Real-IP"
)

// HTTP Status Code Ranges
const (
	// StatusCodeOK represents HTTP 200 OK
	StatusCodeOK = 200
	// StatusCodeCreated represents HTTP 201 Created
	StatusCodeCreated = 201
	// StatusCodeAccepted represents HTTP 202 Accepted
	StatusCodeAccepted = 202
	// StatusCodeNoContent represents HTTP 204 No Content
	StatusCodeNoContent = 204
	// StatusCodeBadRequest represents HTTP 400 Bad Request
	StatusCodeBadRequest = 400
	// StatusCodeUnauthorized represents HTTP 401 Unauthorized
	StatusCodeUnauthorized = 401
	// StatusCodeForbidden represents HTTP 403 Forbidden
	StatusCodeForbidden = 403
	// StatusCodeNotFound represents HTTP 404 Not Found
	StatusCodeNotFound = 404
	// StatusCodeMethodNotAllowed represents HTTP 405 Method Not Allowed
	StatusCodeMethodNotAllowed = 405
	// StatusCodeTimeout represents HTTP 408 Request Timeout
	StatusCodeTimeout = 408
	// StatusCodeConflict represents HTTP 409 Conflict
	StatusCodeConflict = 409
	// StatusCodeTooManyRequests represents HTTP 429 Too Many Requests
	StatusCodeTooManyRequests = 429
	// StatusCodeInternalServerError represents HTTP 500 Internal Server Error
	StatusCodeInternalServerError = 500
	// StatusCodeBadGateway represents HTTP 502 Bad Gateway
	StatusCodeBadGateway = 502
	// StatusCodeServiceUnavailable represents HTTP 503 Service Unavailable
	StatusCodeServiceUnavailable = 503
	// StatusCodeGatewayTimeout represents HTTP 504 Gateway Timeout
	StatusCodeGatewayTimeout = 504
)

// API Paths define common API endpoint paths.
const (
	// PathAPIV1 represents the base path for API v1
	PathAPIV1 = "/api/v1"
	// PathAPIV1Agents represents the agents endpoint
	PathAPIV1Agents = "/api/v1/agents"
	// PathAPIV1Tools represents the tools endpoint
	PathAPIV1Tools = "/api/v1/tools"
	// PathHealth represents the health check endpoint
	PathHealth = "/health"
	// PathHealthz represents an alternative health check endpoint
	PathHealthz = "/healthz"
	// PathMetrics represents the metrics endpoint
	PathMetrics = "/metrics"
	// PathStatus represents the status endpoint
	PathStatus = "/status"
	// PathReady represents the readiness endpoint
	PathReady = "/ready"
)

// HTTP Protocol Versions
const (
	// ProtocolHTTP10 represents HTTP/1.0
	ProtocolHTTP10 = "HTTP/1.0"
	// ProtocolHTTP11 represents HTTP/1.1
	ProtocolHTTP11 = "HTTP/1.1"
	// ProtocolHTTP2 represents HTTP/2
	ProtocolHTTP2 = "HTTP/2"
)

// Common HTTP Values
const (
	// CharsetUTF8 represents UTF-8 character encoding
	CharsetUTF8 = "charset=utf-8"
	// Bearer represents the Bearer authentication scheme
	Bearer = "Bearer"
	// Basic represents the Basic authentication scheme
	Basic = "Basic"
)
