// 工具名称、描述与旧执行器默认值。
package constants

const (
	ToolCalculator            = "calculator"
	ToolAPI                   = "api"
	ToolSearch                = "search"
	ToolShell                 = "shell"
	ToolFileOperations        = "file_operations"
	ToolDatabaseQuery         = "database_query"
	ToolWebScraper            = "web_scraper"
	ToolAPICaller             = "api_caller"
	DescCalculator            = "计算器工具，支持基本数学运算（加减乘除、幂运算、括号）"
	DescAPI                   = "HTTP API 调用工具，支持 GET、POST、PUT、DELETE、PATCH 等方法"
	DescSearch                = "搜索工具，提供网络搜索功能"
	DescShell                 = "Shell 命令执行工具，安全地执行白名单中的系统命令"
	DescFileOperations        = "文件操作工具，支持读取、写入、删除文件"
	DescDatabaseQuery         = "数据库查询工具，支持执行 SQL 查询"
	DescWebScraper            = "网页抓取工具，提取网页内容和结构化数据"
	DescAPICaller             = "API 调用工具，简化 RESTful API 调用流程"
	DefaultToolTimeoutSeconds = 30
	DefaultToolConcurrency    = 10
	DefaultToolRetries        = 3
)
