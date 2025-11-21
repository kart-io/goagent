# GoAgent 导入层级说明

## 概述

GoAgent 强制执行严格的 4 层架构，通过自动化验证确保导入规则的正确性。违反导入规则将导致 CI 失败。

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

- 只能导入标准库和外部依赖
- **禁止**导入任何 GoAgent 内部包

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
retrieval/     - 文档检索
observability/ - 遥测和监控
performance/   - 性能工具
planning/      - 规划工具
prompt/        - 提示工程
reflection/    - 反射工具
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

import "github.com/kart-io/goagent/agents"  // 禁止导入第 3 层！
```

### 第 3 层：实现层

**包列表：**

```text
agents/       - Agent 实现
tools/        - 工具实现
middleware/   - 中间件实现
parsers/      - 输出解析器
stream/       - 流处理
multiagent/   - 多 Agent 编排
distributed/  - 分布式执行
mcp/          - 模型上下文协议
document/     - 文档处理
toolkits/     - 工具集合
```

**导入规则：**

- 可以导入第 1 层和第 2 层
- 同层有限的交叉导入（如 agents 可以导入 tools）
- **tools/ 禁止导入 agents/、middleware/ 或 parsers/**
- **parsers/ 禁止导入 agents/、tools/ 或 middleware/**

**示例：**

```go
// 正确
package agents

import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"  // 同层允许
)

// 错误
package tools

import "github.com/kart-io/goagent/agents"  // 禁止！
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
import "github.com/kart-io/goagent/agents"  // 禁止

// tools 导入 agents
package tools
import "github.com/kart-io/goagent/agents"  // 禁止

// 循环依赖
package core
import "github.com/kart-io/goagent/builder"
// 同时在 builder 中：
import "github.com/kart-io/goagent/core"  // 可能造成循环

// 生产代码导入 examples
package agents
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
package agents
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"  // 同层允许
)

// 测试可以导入任何层
package agents_test
import (
    "github.com/kart-io/goagent/agents"
    "github.com/kart-io/goagent/tools"
)
```

## 验证流程

### 运行验证脚本

**提交前必须运行：**

```bash
./verify_imports.sh
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
./verify_imports.sh --strict
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
5. 运行 `./verify_imports.sh`
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
./verify_imports.sh   # 验证导入层级
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
