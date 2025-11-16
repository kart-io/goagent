package tools

import (
	"github.com/kart-io/goagent/mcp/core"
)

// BuiltinTools 内置工具注册表
var BuiltinTools = []core.Tool{
	// 文件系统工具 (4)
	NewReadFileTool(),
	NewWriteFileTool(),
	NewListDirectoryTool(),
	NewSearchFilesTool(),

	// 网络工具 (1)
	NewHTTPRequestTool(),

	// 数据处理工具 (1)
	NewJSONParseTool(),

	// 系统工具 (1)
	NewShellExecuteTool(),
}

// RegisterBuiltinTools 注册所有内置工具到工具箱
func RegisterBuiltinTools(toolbox core.ToolBox) error {
	for _, tool := range BuiltinTools {
		if err := toolbox.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

// GetToolsByCategory 按分类获取工具
func GetToolsByCategory(category string) []core.Tool {
	tools := make([]core.Tool, 0)
	for _, tool := range BuiltinTools {
		if tool.Category() == category {
			tools = append(tools, tool)
		}
	}
	return tools
}

// ToolCategories 工具分类
var ToolCategories = []string{
	"filesystem", // 文件系统
	"network",    // 网络
	"data",       // 数据处理
	"system",     // 系统
	"database",   // 数据库
	"text",       // 文本处理
}

// CategoryDescriptions 分类描述
var CategoryDescriptions = map[string]string{
	"filesystem": "文件系统操作工具（读写、列表、搜索）",
	"network":    "网络请求工具（HTTP、WebSocket、DNS）",
	"data":       "数据处理工具（JSON、XML、正则表达式）",
	"system":     "系统操作工具（Shell、进程、系统信息）",
	"database":   "数据库操作工具（SQL、Redis）",
	"text":       "文本处理工具（搜索、替换、转换）",
}
