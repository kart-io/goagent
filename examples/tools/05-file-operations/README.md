# 文件操作工具示例

本示例展示了 GoAgent 文件操作工具的使用方法。

## 工具拆分说明

从版本 v1.x 开始，原有的 `FileOperationsTool` 已被拆分为 5 个专门的工具，以遵循单一职责原则:

### 1. FileReadTool - 文件读取工具

负责所有文件读取相关操作:

- `read`: 读取文件内容
- `parse`: 解析结构化文件 (JSON/YAML/CSV)
- `info`: 获取文件元数据信息
- `analyze`: 分析文件内容（行数、单词数、MIME类型等）

**使用示例:**

```go
readTool := practical.NewFileReadTool(nil) // 使用默认配置

// 读取文件
output, err := readTool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "operation": "read",
        "path":      "/path/to/file.txt",
    },
})
```

### 2. FileWriteTool - 文件写入工具

负责文件写入操作:

- `write`: 写入文件（覆盖/创建）
- `append`: 追加内容到文件末尾

### 3. FileManagementTool - 文件管理工具

负责文件和目录管理操作:

- `delete`: 删除文件/目录
- `copy`: 复制文件
- `move`: 移动/重命名文件
- `list`: 列出目录内容
- `search`: 搜索文件（支持 glob 和正则）

### 4. FileCompressionTool - 文件压缩工具

负责文件压缩和解压操作:

- `compress`: 压缩文件（支持 gzip 和 zip）
- `decompress`: 解压文件

### 5. FileWatchTool - 文件监控工具

负责文件变化监控:

- `watch`: 监控文件变化

## 向后兼容性

原有的 `FileOperationsTool` 仍然可用，但已标记为废弃（Deprecated）。

## 运行示例

```bash
cd examples/tools/05-file-operations
go run main.go
```

## 安全注意事项

- 所有工具默认限制在指定的 `basePath` 内
- 默认禁止访问系统敏感目录 (`/etc`, `/sys`, `/proc`)
- 有文件大小限制（默认 100MB）
