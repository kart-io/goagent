## 项目上下文摘要（Validator 统一任务）
生成时间：2025-11-30 15:30:00

### 1. 相似实现分析

**实现1: tools/validator.go (302行)**
- 模式：面向 `interfaces.Tool` 接口的验证器
- 核心类型：`InputValidator` 结构体
- 可复用：
  - `NewInputValidator()` - 创建默认配置验证器
  - `NewStrictInputValidator()` - 创建严格模式验证器
  - `Validate()` - 验证工具输入
  - `ValidateAndInvoke()` - 便捷方法：验证后执行
- 需注意：
  - 解析 JSON Schema 字符串（从 `tool.ArgsSchema()` 获取）
  - 支持自定义验证（`ValidatableTool` 接口）
  - 使用 `agentErrors` 包装错误
  - 配置项：StrictMode、ValidateTypes、ValidateRequired

**实现2: mcp/toolbox/validator.go (362行)**
- 模式：面向 `core.ToolSchema` 结构的验证器
- 核心类型：`JSONSchemaValidator` 结构体
- 可复用：
  - `NewJSONSchemaValidator()` - 创建验证器
  - `ValidateSchema()` - 验证 Schema 定义本身
  - `ValidateInput()` - 验证输入参数
  - `ValidateOutput()` - 验证输出结果（占位符）
- 需注意：
  - 直接操作 `core.ToolSchema` 结构（不解析字符串）
  - 支持更多高级特性：正则表达式、格式验证（email、URL、UUID）
  - 使用 `core.ErrInvalidInput` 自定义错误类型
  - 支持 `AdditionalProperties` 控制

**差异对比**：
| 特性 | tools/validator.go | mcp/toolbox/validator.go | 重复度 |
|------|-------------------|-------------------------|--------|
| 必需参数验证 | ✅ | ✅ | 100% |
| 类型验证 | ✅ | ✅ | 100% |
| 数值范围验证 | ✅ | ✅ | 100% |
| 字符串长度验证 | ✅ | ✅ | 100% |
| 枚举值验证 | ✅ | ✅ | 100% |
| 正则表达式验证 | ❌ | ✅ (Pattern) | 0% |
| 格式验证 | ❌ | ✅ (email/URL/UUID) | 0% |
| Schema 自验证 | ❌ | ✅ | 0% |
| 自定义验证钩子 | ✅ (ValidatableTool) | ❌ | - |
| 输入来源 | JSON Schema 字符串 | core.ToolSchema 结构 | - |
| 错误类型 | agentErrors | core.ErrInvalidInput | - |

**重复度评估：核心功能 80%+ 重复，高级特性互补**

### 2. 项目约定

**命名约定**：
- 类型名：大驼峰 (InputValidator, JSONSchemaValidator)
- 工厂函数：NewXxx()
- 方法名：大驼峰 (Validate, ValidateInput)
- 私有方法：小驼峰 (validateRequired, validateTypes)
- 配置字段：大驼峰 + bool 类型 (StrictMode, ValidateTypes)

**文件组织**：
- tools/validator.go - 通用工具包验证器
- mcp/toolbox/validator.go - MCP 工具箱专用验证器
- 测试文件：tools/validator_test.go (537行，完整覆盖)
- mcp/toolbox/ 无独立测试文件，集成在 toolbox_test.go

**导入顺序**：
```go
// 标准库
import (
	"context"
	"encoding/json"
	"fmt"
)

// 第三方库
import (
	"github.com/google/uuid"
)

// 项目内部
import (
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/mcp/core"
)
```

**代码风格**：
- 使用 tab 缩进
- 错误处理：立即返回，使用 Wrap 添加上下文
- 注释：中文注释，描述"做什么"和"为什么"
- 结构：公开方法 → 私有辅助方法 → 类型定义

### 3. 可复用组件清单

**验证器核心功能（可统一）**：
- `validateRequired()` - 必需参数验证
- `validateTypes()` - 类型验证
- `validateString()` - 字符串验证（长度、枚举）
- `validateNumber()` - 数值验证（范围、整数检查）
- `validateBoolean()` - 布尔值验证
- `validateArray()` - 数组验证
- `validateObject()` - 对象验证

**工具箱增强功能（需合并）**：
- `validateFormat()` - 格式验证 (email/URL/UUID)
- `ValidateSchema()` - Schema 自验证
- 正则表达式支持 (Pattern 字段)

**错误处理组件**：
- `agentErrors` - 项目标准错误包
- `core.ErrInvalidInput` - MCP 专用错误类型

### 4. 测试策略

**测试框架**：testify/assert

**测试模式**：表驱动测试（table-driven tests）
```go
tests := []struct {
	name    string
	args    map[string]interface{}
	wantErr bool
	errMsg  string
}{
	{"case1", args1, false, ""},
	{"case2", args2, true, "expected error"},
}
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		// 测试逻辑
	})
}
```

**参考文件**：tools/validator_test.go (537行)
- `TestInputValidator_ValidateRequired` - 必需参数
- `TestInputValidator_ValidateTypes` - 类型验证
- `TestInputValidator_StrictMode` - 严格模式
- `TestInputValidator_CustomValidation` - 自定义验证
- `TestInputValidator_NumericConstraints` - 数值约束
- `TestInputValidator_StringConstraints` - 字符串约束
- `TestInputValidator_NilInputs` - 空输入处理
- `TestInputValidator_EmptySchema` - 空 schema
- `TestValidateAndInvoke` - 便捷方法

**覆盖要求**：
- 正常流程：所有类型的有效输入
- 边界条件：nil、空字符串、空 map、最大/最小值
- 错误处理：类型错误、缺失必需参数、超出范围

### 5. 依赖和集成点

**外部依赖**：
- 无第三方验证库依赖
- 测试依赖：github.com/stretchr/testify/assert

**内部依赖**：
- tools/validator.go 依赖：
  - `interfaces.Tool` - 工具接口
  - `interfaces.ToolInput` - 工具输入
  - `interfaces.ToolOutput` - 工具输出
  - `interfaces.ValidatableTool` - 可验证工具接口
  - `agentErrors` - 错误处理

- mcp/toolbox/validator.go 依赖：
  - `core.ToolSchema` - Schema 结构
  - `core.PropertySchema` - 属性 Schema
  - `core.ErrInvalidInput` - 验证错误
  - `agentErrors` - 错误处理

**集成方式**：
- tools/validator.go：独立使用，通过 `ValidateAndInvoke()` 便捷调用
- mcp/toolbox/validator.go：作为 `StandardToolBox.validator` 字段使用
  - 在 `NewStandardToolBox()` 中初始化
  - 在 `Register()` 中验证 Schema
  - 在 `Validate()` 中验证输入

**当前引用**：
- mcp/toolbox/toolbox.go:44 - `validator: NewJSONSchemaValidator()`
- mcp/toolbox/toolbox.go:58 - `tb.validator.ValidateSchema(tool.Schema())`
- mcp/toolbox/toolbox.go:275 - `tb.validator.ValidateInput(tool.Schema(), call.Input)`
- mcp/toolbox/toolbox_test.go:301-302 - 测试代码

### 6. 技术选型理由

**为什么有两个验证器**：
1. **历史演进**：
   - tools/validator.go 先出现，面向通用 `interfaces.Tool`
   - mcp/toolbox/validator.go 后出现，为 MCP 协议定制

2. **接口差异**：
   - tools/validator.go：处理 JSON Schema 字符串（`tool.ArgsSchema()` 返回 string）
   - mcp/toolbox/validator.go：处理结构化 Schema（`tool.Schema()` 返回 `*core.ToolSchema`）

3. **功能侧重**：
   - tools/validator.go：简单直接，支持自定义验证钩子
   - mcp/toolbox/validator.go：完整的 JSON Schema 验证，支持高级特性

**优势**：
- tools/validator.go：有完整测试（537行）、API 简洁、配置灵活
- mcp/toolbox/validator.go：功能完备、支持 Schema 自验证、正则和格式验证

**劣势和风险**：
- **重复代码**：核心验证逻辑重复度 80%+
- **维护成本**：bug 修复需要同步两处
- **不一致性**：两个验证器可能产生不同的验证结果
- **测试缺失**：mcp/toolbox/validator.go 缺少专门的测试文件

### 7. 关键风险点

**统一策略风险**：
1. **接口兼容性**：
   - tools/validator.go 使用 `interfaces.Tool` 接口
   - mcp/toolbox/validator.go 使用 `core.ToolSchema` 结构
   - 需要确认能否统一到一个接口

2. **功能完整性**：
   - 删除任一实现可能丢失独有功能
   - tools/validator.go 的 `ValidatableTool` 钩子
   - mcp/toolbox/validator.go 的高级验证特性

3. **测试覆盖**：
   - mcp/toolbox/validator.go 缺少独立测试
   - 统一后需要合并测试用例

4. **现有调用点**：
   - mcp/toolbox/toolbox.go 强依赖 `JSONSchemaValidator`
   - 需要适配器模式或接口统一

**性能考虑**：
- Schema 解析：字符串解析 vs 结构直接访问
- 反射使用：mcp/toolbox/validator.go 使用 reflect.ValueOf

**破坏性更改**：
- 删除 mcp/toolbox/validator.go 会影响：
  - mcp/toolbox/toolbox.go 的 validator 字段
  - 相关测试代码
- 需要提供清晰的迁移路径

### 8. 统一方案建议

**推荐方案：保留 tools/validator.go，扩展其功能，删除 mcp/toolbox/validator.go**

**理由**：
1. tools/validator.go 有完整测试覆盖（537行）
2. API 设计更灵活（配置选项、自定义验证）
3. 独立性更好（不依赖 MCP 特定类型）

**迁移步骤**：
1. 分析 mcp/toolbox/validator.go 独有功能
2. 将缺失功能合并到 tools/validator.go
3. 修改 mcp/toolbox/toolbox.go 使用 tools.InputValidator
4. 删除 mcp/toolbox/validator.go
5. 验证编译和测试通过

**需要合并的功能**：
- 正则表达式验证 (Pattern)
- 格式验证 (Format: email/URL/UUID)
- Schema 自验证 (ValidateSchema)
- AdditionalProperties 支持

**接口适配**：
```go
// mcp/toolbox/toolbox.go 需要的适配
type ValidatorAdapter struct {
	inputValidator *tools.InputValidator
}

func (a *ValidatorAdapter) ValidateSchema(schema *core.ToolSchema) error {
	// 将 core.ToolSchema 转换为 JSON 字符串
	// 调用 inputValidator.parseSchema() 验证
}

func (a *ValidatorAdapter) ValidateInput(schema *core.ToolSchema, input map[string]interface{}) error {
	// 包装为 interfaces.ToolInput
	// 调用 inputValidator.Validate()
}
```

**注意事项**：
- ❌ 禁止创建兼容层（用户要求）
- ❌ 禁止添加 type alias（用户要求）
- ✅ 直接删除重复实现
- ✅ 直接修改所有引用
