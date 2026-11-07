// HTTP 请求、响应与接口路径常量。
package constants

// HTTP 请求方法。
const (
	MethodGet     = "GET"
	MethodPost    = "POST"
	MethodPut     = "PUT"
	MethodDelete  = "DELETE"
	MethodPatch   = "PATCH"
	MethodHead    = "HEAD"
	MethodOptions = "OPTIONS"
	MethodConnect = "CONNECT"
	MethodTrace   = "TRACE"
)

// HTTP 内容类型。
const (
	ContentTypeJSON        = "application/json"
	ContentTypeText        = "text/plain"
	ContentTypeHTML        = "text/html"
	ContentTypeXML         = "application/xml"
	ContentTypeForm        = "application/x-www-form-urlencoded"
	ContentTypeMultipart   = "multipart/form-data"
	ContentTypeEventStream = "text/event-stream"
	ContentTypeOctetStream = "application/octet-stream"
)

// HTTP 头名称。
const (
	HeaderContentType     = "Content-Type"
	HeaderAccept          = "Accept"
	HeaderAuthorization   = "Authorization"
	HeaderUserAgent       = "User-Agent"
	HeaderAcceptEncoding  = "Accept-Encoding"
	HeaderContentEncoding = "Content-Encoding"
	HeaderCacheControl    = "Cache-Control"
	HeaderConnection      = "Connection"
	HeaderCookie          = "Cookie"
	HeaderSetCookie       = "Set-Cookie"
	HeaderLocation        = "Location"
	HeaderReferer         = "Referer"
	HeaderXForwardedFor   = "X-Forwarded-For"
	HeaderXRealIP         = "X-Real-IP"
)

// HTTP 状态码。
const (
	StatusCodeOK                  = 200
	StatusCodeCreated             = 201
	StatusCodeAccepted            = 202
	StatusCodeNoContent           = 204
	StatusCodeBadRequest          = 400
	StatusCodeUnauthorized        = 401
	StatusCodeForbidden           = 403
	StatusCodeNotFound            = 404
	StatusCodeMethodNotAllowed    = 405
	StatusCodeTimeout             = 408
	StatusCodeConflict            = 409
	StatusCodeTooManyRequests     = 429
	StatusCodeInternalServerError = 500
	StatusCodeBadGateway          = 502
	StatusCodeServiceUnavailable  = 503
	StatusCodeGatewayTimeout      = 504
)

// API 与健康检查路径。
const (
	PathAPIV1       = "/api/v1"
	PathAPIV1Agents = "/api/v1/agents"
	PathAPIV1Tools  = "/api/v1/tools"
	PathHealth      = "/health"
	PathHealthz     = "/healthz"
	PathMetrics     = "/metrics"
	PathStatus      = "/status"
	PathReady       = "/ready"
)

// HTTP 协议版本。
const (
	ProtocolHTTP10 = "HTTP/1.0"
	ProtocolHTTP11 = "HTTP/1.1"
	ProtocolHTTP2  = "HTTP/2"
)

// 字符编码与认证方案。
const (
	CharsetUTF8 = "charset=utf-8"
	Bearer      = "Bearer"
	Basic       = "Basic"
)
