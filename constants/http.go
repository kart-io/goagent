// HTTP 请求、响应与接口路径常量。
package constants

// HTTP 请求方法。
const (
	MethodGet    = "GET"
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodDelete = "DELETE"
	MethodPatch  = "PATCH"
)

// HTTP 内容类型。
const (
	ContentTypeJSON        = "application/json"
	ContentTypeOctetStream = "application/octet-stream"
)

// HTTP 头名称。
const (
	HeaderContentType   = "Content-Type"
	HeaderAuthorization = "Authorization"
	HeaderUserAgent     = "User-Agent"
)

// 字符编码与认证方案。
const (
	Bearer = "Bearer"
	Basic  = "Basic"
)
