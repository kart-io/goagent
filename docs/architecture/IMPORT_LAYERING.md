# GoAgent 导入层级说明

## 概述

自动检查通过 Go AST 读取当前模块的真实 import，扫描仓库生产 Go 文件（排除测试与 vendor）。检查覆盖基础包隔离、core/builder 的禁止依赖、tools/parsers 的禁止依赖，以及非示例代码导入 examples；不宣称覆盖以下所有概念层级组合。Go 编译器另外检查循环依赖。违反已实现规则将导致 CI 失败。

## 层级定义

### 第 1 层：基础层

**包列表：**

```text
interfaces/  - 所有公共接口定义
errors/      - 错误类型和辅助函数
cache/       - 基础缓存工具
utils/       - 工具函数
```

**导入规则：**

- `interfaces`、`errors` 仅允许标准库和外部依赖。
- `utils` 及其子包可导入自己的子包和 `errors`（`errors` 是不依赖任何内部包的叶子包，不会成环）。`cache` 可导入自己的子包及既有的 `utils/json`。
- 禁止这些基础包导入其他 GoAgent 业务包。

**示例：**

```go
// 正确
package interfaces

import (
    "context"
    "time"
)

// 错误
package interfaces

import "github.com/kart-io/goagent/core"  // 禁止！
```

### 第 2 层：业务逻辑层

**包列表：**

```text
core/          - 基础实现 (BaseAgent, BaseChain)
core/execution/ - 执行引擎
core/state/    - 状态管理
core/checkpoint/ - 检查点逻辑
core/middleware/ - 中间件框架
builder/       - 流式 API 构建器 (AgentBuilder)
llm/           - LLM 客户端实现
memory/        - 内存管理
store/         - 存储实现
observability/ - 遥测和监控
workflow/      - 基于 core.Runnable 的通用图编排
```

**导入规则：**

- 可以导入第 1 层
- 第 2 层内部可以相互导入，但需注意避免循环依赖

**示例：**

```go
// 正确
package core

import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
)

// 错误
package core

import "github.com/kart-io/goagent/middleware"  // 禁止导入第 3 层！
```

### 第 3 层：实现层

**包列表：**

```text
tools/        - 工具实现
middleware/   - 中间件实现
parsers/      - 输出解析器
mcp/          - 模型上下文协议
```

**导入规则：**

- 可以导入第 1 层和第 2 层
- 同层有限的交叉导入（如 middleware 可以导入 tools）
- **tools/ 禁止导入 agents/、middleware/ 或 parsers/**
- **parsers/ 禁止导入 agents/、tools/ 或 middleware/**

**示例：**

```go
// 正确
package middleware

import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"  // 同层允许
)

// 错误
package parsers

import "github.com/kart-io/goagent/middleware"  // 禁止！
```

### 第 4 层：示例和测试

**包列表：**

```text
examples/     - 使用示例
*_test.go     - 测试文件
```

**导入规则：**

- 可以导入所有层
- 生产代码禁止导入第 4 层

## 关键导入限制

### 禁止的导入模式

```go
// 第 1 层导入 GoAgent 内部包
package interfaces
import "github.com/kart-io/goagent/core"  // 禁止

// 第 2 层导入第 3 层
package core
import "github.com/kart-io/goagent/middleware"  // 禁止

// parsers 导入 middleware
package parsers
import "github.com/kart-io/goagent/middleware"  // 禁止

// 循环依赖
package core
import "github.com/kart-io/goagent/builder"
// 同时在 builder 中：
import "github.com/kart-io/goagent/core"  // 可能造成循环

// 生产代码导入 examples
package tools
import "github.com/kart-io/goagent/examples"  // 禁止
```

### 正确的导入模式

```go
// 第 2 层导入第 1 层
package core
import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
)

// 第 3 层导入第 1 和第 2 层
package middleware
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"  // 同层允许
)

// 测试可以导入任何层
package middleware_test
import (
    "github.com/kart-io/goagent/middleware"
    "github.com/kart-io/goagent/tools"
)
```

## 验证流程

### 运行验证脚本

**提交前必须运行：**

```bash
./scripts/verify_imports.sh
```

脚本检查内容：

- 第 1 层没有 GoAgent 导入
- 第 2 层不导入第 3 层
- tools/ 不导入 agents/
- parsers/ 不导入 agents/ 或 middleware/
- 生产代码不导入 examples/
- 无循环依赖

### 严格模式

将警告视为错误：

```bash
./scripts/verify_imports.sh --strict
```

## 导入组织规范

### 标准导入顺序

```go
import (
    // 标准库优先
    "context"
    "fmt"
    "time"

    // 外部依赖
    "github.com/google/uuid"
    "github.com/stretchr/testify/assert"

    // 内部包（遵循层级规则）
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
)
```

## 常见问题解决

### 循环依赖

**问题**：两个包相互导入导致编译失败。

**解决方案**：

1. 将共享类型提取到第 1 层（interfaces/）
2. 使用依赖注入
3. 重新评估包的职责划分

### 跨层重构

如果需要在层之间移动代码：

1. 根据功能确定正确的层
2. 移动代码到新位置
3. 更新依赖包的导入
4. 如需向后兼容，在旧位置添加类型别名
5. 运行 `./scripts/verify_imports.sh`
6. 更新测试和文档

### 打破依赖循环

```go
// 不好的做法：直接依赖
package a
import "github.com/kart-io/goagent/b"

package b
import "github.com/kart-io/goagent/a"

// 好的做法：通过接口解耦
package interfaces
type ServiceA interface { ... }
type ServiceB interface { ... }

package a
import "github.com/kart-io/goagent/interfaces"
func New(b interfaces.ServiceB) *A { ... }

package b
import "github.com/kart-io/goagent/interfaces"
func New(a interfaces.ServiceA) *B { ... }
```

## CI/CD 集成

### 提交前检查清单

```bash
make fmt              # 格式化代码
make lint             # 检查问题
./scripts/verify_imports.sh   # 验证导入层级
make test             # 运行测试
```

### CI 失败原因

CI 会在以下情况失败：

- Lint 错误（零容忍）
- 导入层级违规
- 测试失败
- 覆盖率不足（低于 80%）

## 相关文档

- [架构概述](ARCHITECTURE.md)
- [测试最佳实践](../development/TESTING_BEST_PRACTICES.md)
## 已知兼容例外与检查修复

v0.17.0 删除推理策略及 `builder/reasoning_*.go` 后，检查器不再有兼容例外，Builder 导入 `agents` 或 `middleware` 一律拒绝。

2026-10-27 修复了旧脚本把扫描根目录设为 scripts、匹配旧 pkg/agent 路径的问题。旧报告中的“导入检查通过”不能作为分层已验证证据。新入口 `bash scripts/verify_imports.sh` 委派 `scripts/apicheck`，有违规规则和精确例外的回归测试，当前真实扫描为 0 findings。
