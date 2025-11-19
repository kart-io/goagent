package tools

// Tool names
const (
	// Compute tools
	ToolCalculator = "calculator"

	// HTTP tools
	ToolAPI = "api"

	// Search tools
	ToolSearch = "search"

	// Shell tools
	ToolShell = "shell"

	// Practical tools
	ToolFileOperations = "file_operations"
	ToolDatabaseQuery  = "database_query"
	ToolWebScraper     = "web_scraper"
	ToolAPICaller      = "api_caller"
)

// Tool descriptions
const (
	// Compute tools
	DescCalculator = "计算器工具，支持基本数学运算（加减乘除、幂运算、括号）"

	// HTTP tools
	DescAPI = "HTTP API 调用工具，支持 GET、POST、PUT、DELETE、PATCH 等方法"

	// Search tools
	DescSearch = "搜索工具，提供网络搜索功能"

	// Shell tools
	DescShell = "Shell 命令执行工具，安全地执行白名单中的系统命令"

	// Practical tools
	DescFileOperations = "文件操作工具，支持读取、写入、删除文件"
	DescDatabaseQuery  = "数据库查询工具，支持执行 SQL 查询"
	DescWebScraper     = "网页抓取工具，提取网页内容和结构化数据"
	DescAPICaller      = "API 调用工具，简化 RESTful API 调用流程"
)

// Tool error messages
const (
	ErrToolNotFound      = "tool not found"
	ErrInvalidInput      = "invalid tool input"
	ErrExecutionFailed   = "tool execution failed"
	ErrTimeout           = "tool execution timeout"
	ErrUnauthorized      = "tool execution unauthorized"
	ErrToolAlreadyExists = "tool already exists"
)

// Tool execution constants
const (
	DefaultToolTimeout     = 30 // seconds
	MaxConcurrentToolCalls = 10
	MaxRetries             = 3
)
