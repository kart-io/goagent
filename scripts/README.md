# Provider 迁移脚本

本目录包含自动化迁移脚本，帮助将项目从旧的 LLM provider 使用方式迁移到新的方式。

## 脚本列表

### migrate-to-registry.sh

自动将项目从旧的 `llm/providers` 包迁移到新的 Provider Registry 方式。

**功能**:
- 自动检测并迁移所有 Go 文件
- 添加必要的导入（registry、constants、contrib providers）
- 替换函数调用从 `providers.NewXXX()` 到 `registry.New()`
- 支持 dry-run 模式预览更改
- 可选创建备份文件
- 详细的进度输出

## 使用方法

### 基本用法

```bash
# 迁移当前目录
./scripts/migrate-to-registry.sh

# 迁移指定目录
./scripts/migrate-to-registry.sh ./examples

# 预览将要进行的更改（推荐先运行）
./scripts/migrate-to-registry.sh -d ./myproject

# 迁移并创建备份
./scripts/migrate-to-registry.sh -b ./myproject

# 详细输出
./scripts/migrate-to-registry.sh -v ./myproject
```

### 命令行选项

| 选项 | 说明 |
|------|------|
| `-h, --help` | 显示帮助信息 |
| `-d, --dry-run` | 仅显示将要进行的更改，不实际修改文件 |
| `-b, --backup` | 在修改前创建备份文件 (.bak) |
| `-v, --verbose` | 显示详细输出 |
| `--no-registry` | 仅更新导入路径，不迁移到 registry 模式 |

### 两种迁移模式

#### 模式 1: Registry 模式（默认，推荐）

将代码完全迁移到 Provider Registry 方式。

**迁移前**:
```go
import "github.com/kart-io/goagent/llm/providers"

client, err := providers.NewOpenAIWithOptions(
    agentllm.WithAPIKey("key"),
)
```

**迁移后**:
```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm/constants"
    "github.com/kart-io/goagent/llm/registry"
)

client, err := registry.New(
    constants.ProviderOpenAI,
    agentllm.WithAPIKey("key"),
)
```

**使用**:
```bash
./scripts/migrate-to-registry.sh ./myproject
```

#### 模式 2: 仅更新导入（使用 --no-registry）

仅更新导入路径到新的 contrib 包，不使用 registry。

**迁移前**:
```go
import "github.com/kart-io/goagent/llm/providers"

client, err := providers.NewOpenAIWithOptions(
    agentllm.WithAPIKey("key"),
)
```

**迁移后**:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"

client, err := openai.New(
    agentllm.WithAPIKey("key"),
)
```

**使用**:
```bash
./scripts/migrate-to-registry.sh --no-registry ./myproject
```

## 使用流程

### 步骤 1: 预览更改（Dry Run）

先运行 dry-run 查看将要进行的更改：

```bash
./scripts/migrate-to-registry.sh -d ./myproject
```

输出示例：
```
=== GoAgent Provider Registry 迁移工具 ===
目标目录: ./myproject
模式: Registry 模式
Dry run: 是

扫描 Go 文件...
[DRY RUN] 将迁移: ./myproject/main.go
[DRY RUN] 将迁移: ./myproject/client.go

=== 迁移完成 ===
扫描文件数: 10
修改文件数: 2

这是 dry run 模式，没有实际修改文件
移除 -d 或 --dry-run 选项以执行实际迁移
```

### 步骤 2: 创建备份并迁移

确认更改合理后，创建备份并执行迁移：

```bash
./scripts/migrate-to-registry.sh -b ./myproject
```

### 步骤 3: 验证迁移

运行测试确保一切正常：

```bash
cd ./myproject
go mod tidy
go test ./...
```

### 步骤 4: 恢复备份（如需要）

如果迁移出现问题，可以恢复备份：

```bash
# 恢复所有备份文件
find ./myproject -name '*.bak' -exec bash -c 'mv "$0" "${0%.bak}"' {} \;

# 删除备份文件
find ./myproject -name '*.bak' -delete
```

## 支持的 Providers

脚本自动检测和迁移以下 providers：

- OpenAI
- DeepSeek
- Gemini
- Anthropic
- Cohere
- HuggingFace
- Ollama
- Kimi
- SiliconFlow

## 注意事项

### 脚本会做什么

✅ 自动添加必要的导入
✅ 替换函数调用
✅ 移除不再需要的旧导入
✅ 保持代码格式（使用 gofmt）

### 脚本不会做什么

❌ 不会修改注释
❌ 不会修改字符串内容
❌ 不会修改测试文件中的 mock 代码
❌ 不会修改 vendor 目录

### 限制

- 脚本使用正则表达式进行替换，对于复杂的代码结构可能需要手动调整
- 建议在迁移前先提交代码到版本控制系统
- 对于大型项目，建议分批迁移

## 手动迁移步骤

如果自动脚本无法正确处理某些文件，可以手动迁移：

### Registry 模式手动迁移

1. 添加导入：
```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm/constants"
    "github.com/kart-io/goagent/llm/registry"
)
```

2. 替换函数调用：
```go
// 旧代码
client, err := providers.NewOpenAIWithOptions(opts...)

// 新代码
client, err := registry.New(constants.ProviderOpenAI, opts...)
```

3. 移除旧导入：
```go
// 移除这行
import "github.com/kart-io/goagent/llm/providers"
```

### 仅更新导入手动迁移

1. 添加新导入：
```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"
```

2. 替换函数调用：
```go
// 旧代码
client, err := providers.NewOpenAIWithOptions(opts...)

// 新代码
client, err := openai.New(opts...)
```

3. 移除旧导入：
```go
// 移除这行
import "github.com/kart-io/goagent/llm/providers"
```

## 故障排查

### 问题: 脚本报告 "未找到 Go 文件"

**解决**: 检查目标目录路径是否正确

### 问题: 迁移后编译失败

**解决**:
1. 运行 `go mod tidy` 更新依赖
2. 检查是否有手动导入需要调整
3. 查看错误信息，手动修复特殊情况

### 问题: 想要撤销迁移

**解决**:
```bash
# 如果创建了备份
find ./myproject -name '*.bak' -exec bash -c 'mv "$0" "${0%.bak}"' {} \;

# 如果使用了 Git
git checkout ./myproject
```

## 示例

### 示例 1: 迁移单个项目

```bash
# 1. 预览更改
./scripts/migrate-to-registry.sh -d ./myapp

# 2. 创建备份并迁移
./scripts/migrate-to-registry.sh -b ./myapp

# 3. 验证
cd ./myapp
go mod tidy
go build ./...
go test ./...

# 4. 清理备份（如果一切正常）
find ./myapp -name '*.bak' -delete
```

### 示例 2: 批量迁移多个项目

```bash
#!/bin/bash
for dir in project1 project2 project3; do
    echo "迁移 $dir..."
    ./scripts/migrate-to-registry.sh -b -v "./$dir"
done
```

### 示例 3: 仅迁移示例目录

```bash
# 仅迁移 examples 目录
./scripts/migrate-to-registry.sh -b ./examples

# 验证示例
cd ./examples
for dir in */; do
    (cd "$dir" && go build . && echo "✓ $dir")
done
```

## 相关文档

- [Provider 使用指南](../docs/guides/PROVIDER_USAGE_GUIDE.md)
- [Registry 迁移指南](../docs/guides/REGISTRY_MIGRATION_GUIDE.md)
- [Registry 完整文档](../llm/registry/README.md)
- [Registry 示例](../examples/basic/13-provider-registry/)

## 贡献

如果你发现脚本的问题或有改进建议，欢迎提交 Issue 或 Pull Request。

## 许可

本脚本是 GoAgent 项目的一部分，遵循相同的许可证。
