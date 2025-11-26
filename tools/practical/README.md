# Database Query Tool

## ⚠️ 安全警告

**必须使用参数化查询！永远不要直接拼接用户输入到 SQL 查询中。**

SQL 注入是最危险的安全漏洞之一，可能导致：

- 数据泄露（窃取敏感信息）
- 数据篡改（修改或删除数据）
- 权限提升（获取管理员权限）
- 服务器控制（执行系统命令）

### ✅ 正确用法 - 参数化查询

```go
// 安全：使用参数化查询
output, err := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver": "mysql",
            "dsn":    "user:password@tcp(localhost:3306)/dbname",
        },
        "query":  "SELECT * FROM users WHERE id = ? AND status = ?",
        "params": []interface{}{userID, "active"},  // 参数独立传递
    },
})
```

### ❌ 错误用法 - 字符串拼接（危险！）

```go
// 危险！容易受到 SQL 注入攻击
userInput := "1 OR 1=1" // 恶意输入
query := fmt.Sprintf("SELECT * FROM users WHERE id = %s", userInput)

// 这会执行：SELECT * FROM users WHERE id = 1 OR 1=1
// 结果：返回所有用户数据！
```

## 概述

Database Query Tool 是一个安全的数据库查询工具，支持 MySQL、PostgreSQL 和 SQLite 数据库。通过内置的安全检查机制和参数化查询支持，帮助防止 SQL 注入攻击。

**核心特性：**

- 🔒 SQL 注入防护 - 基础的查询安全检查
- 🗄️ 多数据库支持 - MySQL, PostgreSQL, SQLite
- 🔄 连接池管理 - 自动管理数据库连接
- 💼 事务支持 - 支持多语句事务执行
- ⏱️ 超时控制 - 防止长时间查询
- 📊 结果限制 - 可配置的最大返回行数

## 支持的数据库

| 数据库 | Driver | DSN 示例 |
|--------|--------|----------|
| MySQL | `mysql` | `user:password@tcp(localhost:3306)/dbname` |
| PostgreSQL | `postgres` | `postgres://user:password@localhost/dbname?sslmode=disable` |
| SQLite | `sqlite` | `/path/to/database.db` 或 `:memory:` |

## 安全特性

### 1. 基础 SQL 注入防护

工具包含 `sanitizeQuery` 函数，提供基础的安全检查：

- ✅ 阻止多语句执行（检测 `;`）
- ✅ 阻止 SQL 注释（检测 `--` 和 `/*`）

**重要提示：** 这些检查只是基础防护，**不能完全防止所有 SQL 注入攻击**。必须配合参数化查询使用。

### 2. 操作模式隔离

不同的操作使用不同的模式，防止误用：

- `query` - 只允许 SELECT/SHOW/DESCRIBE 语句
- `execute` - 只允许 INSERT/UPDATE/DELETE 语句
- `transaction` - 事务模式，支持多语句

### 3. ���数化查询支持

通过 `params` 字段传递参数，参数会被安全地转义：

```go
// 数据库驱动会自动转义参数
"query":  "SELECT * FROM users WHERE name = ? AND age > ?",
"params": []interface{}{"Alice", 18},
```

## 使用示例

### 基本查询

```go
package main

import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools/practical"
)

func main() {
    tool := practical.NewDatabaseQueryTool()
    ctx := context.Background()

    // 执行 SELECT 查询
    output, err := tool.Execute(ctx, &interfaces.ToolInput{
        Args: map[string]interface{}{
            "connection": map[string]interface{}{
                "driver": "mysql",
                "dsn":    "user:password@tcp(localhost:3306)/mydb",
            },
            "query":     "SELECT id, name, email FROM users WHERE status = ?",
            "params":    []interface{}{"active"},
            "operation": "query",
            "max_rows":  100,
        },
    })

    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    result := output.Result.(map[string]interface{})
    columns := result["columns"].([]string)
    rows := result["rows"].([][]interface{})

    fmt.Printf("Columns: %v\n", columns)
    fmt.Printf("Row count: %d\n", len(rows))

    for i, row := range rows {
        fmt.Printf("Row %d: %v\n", i+1, row)
    }
}
```

### 插入数据

```go
output, err := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver": "postgres",
            "dsn":    "postgres://user:pass@localhost/db?sslmode=disable",
        },
        "query":     "INSERT INTO users (name, email) VALUES ($1, $2)",
        "params":    []interface{}{"Alice", "alice@example.com"},
        "operation": "execute",
    },
})

if err == nil {
    result := output.Result.(map[string]interface{})
    fmt.Printf("Rows affected: %d\n", result["rows_affected"])
    fmt.Printf("Last insert ID: %d\n", result["last_insert_id"])
}
```

### 更新数据

```go
output, err := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver": "sqlite",
            "dsn":    "/tmp/test.db",
        },
        "query":     "UPDATE users SET status = ? WHERE id = ?",
        "params":    []interface{}{"inactive", 123},
        "operation": "execute",
    },
})
```

### 事务执行

```go
output, err := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver":        "mysql",
            "dsn":           "user:pass@tcp(localhost:3306)/db",
            "connection_id": "my_connection", // 复用连接
        },
        "operation": "transaction",
        "transaction": []interface{}{
            map[string]interface{}{
                "query":  "INSERT INTO accounts (user_id, balance) VALUES (?, ?)",
                "params": []interface{}{1, 1000},
            },
            map[string]interface{}{
                "query":  "INSERT INTO transactions (from_account, amount) VALUES (?, ?)",
                "params": []interface{}{1, 1000},
            },
        },
    },
})

if err != nil {
    fmt.Println("Transaction rolled back:", err)
} else {
    result := output.Result.(map[string]interface{})
    fmt.Printf("Transaction committed: %d queries executed\n",
        result["queries_executed"])
}
```

### 连接复用

```go
// 第一次请求，创建并缓存连接
output1, _ := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver":        "mysql",
            "dsn":           "user:pass@tcp(localhost:3306)/db",
            "connection_id": "shared_conn", // 连接标识符
        },
        "query": "SELECT * FROM users LIMIT 10",
    },
})

// ��二次请求，复用已有连接
output2, _ := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver":        "mysql",
            "connection_id": "shared_conn", // 复用相同标识符
            // dsn 可以省略，使用已缓存的连接
        },
        "query": "SELECT * FROM products LIMIT 10",
    },
})

// 使用完毕后关闭所有连接
tool.Close()
```

### 超时控制

```go
output, err := tool.Execute(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "connection": map[string]interface{}{
            "driver": "postgres",
            "dsn":    "postgres://user:pass@localhost/db",
        },
        "query":   "SELECT * FROM large_table",
        "timeout": 5, // 5 秒超时
    },
})
```

## API 参考

### NewDatabaseQueryTool

```go
func NewDatabaseQueryTool() *DatabaseQueryTool
```

创建一个新的数据库查询工具实例。

**默认配置：**

- `maxRows`: 1000
- `timeout`: 30 秒

### Execute

```go
func (t *DatabaseQueryTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error)
```

执行数据库操作。

### 输入参数（input.Args）

#### connection (object, 必需)

数据库连接配置：

```go
{
    "driver": "mysql",           // 必需：数据库驱动
    "dsn": "connection_string",  // DSN 连接字符串
    "connection_id": "my_conn"   // 可选：连接标识符，用于复用
}
```

#### 其他参数

| 参数 | 类型 | 必需 | 默认值 | 说明 |
|------|------|------|--------|------|
| `query` | string | 条件必需 | - | SQL 查询语句（query/execute 模式） |
| `params` | []interface{} | 可选 | [] | 查询参数（参数化查询） |
| `operation` | string | 可选 | "query" | 操作类型：query/execute/transaction |
| `transaction` | []object | 条件必需 | - | 事务语句列表（transaction 模式） |
| `max_rows` | int | 可选 | 1000 | 最大返回行数 |
| `timeout` | int | 可选 | 30 | 超时时间（秒） |

### 输出格式

#### query 操作

```go
{
    "columns": ["id", "name", "email"],
    "rows": [
        [1, "Alice", "alice@example.com"],
        [2, "Bob", "bob@example.com"]
    ],
    "execution_time_ms": 45
}
```

#### execute 操作

```go
{
    "rows_affected": 1,
    "last_insert_id": 123,
    "execution_time_ms": 23
}
```

#### transaction 操作

```go
{
    "transaction_results": [
        {"step": 0, "rows_affected": 1, "last_insert_id": 100},
        {"step": 1, "rows_affected": 1, "last_insert_id": 200}
    ],
    "total_rows_affected": 2,
    "queries_executed": 2,
    "execution_time_ms": 67
}
```

### Close

```go
func (t *DatabaseQueryTool) Close() error
```

关闭所有缓存的数据库连接。

## 连接池配置

工具自动配置连接池参数：

```go
db.SetMaxOpenConns(10)               // 最大连接数
db.SetMaxIdleConns(5)                // 最大空闲连接
db.SetConnMaxLifetime(5 * time.Minute) // 连接最大生命周期
```

## 安全最佳实践

### 1. 始终使用参数化查询

```go
// ✅ 安全
"query":  "SELECT * FROM users WHERE email = ?",
"params": []interface{}{userEmail}

// ❌ 危险
query := fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", userEmail)
```

### 2. 验证表名和列名

参数化查询**不能**用于表名和列名：

```go
// ❌ 不生效：参数化不适用于表名
"query":  "SELECT * FROM ? WHERE id = ?",
"params": []interface{}{tableName, id}  // tableName 不会被转义

// ✅ 推荐：使用白名单验证表名
allowedTables := map[string]bool{
    "users": true,
    "products": true,
}

if !allowedTables[tableName] {
    return errors.New("invalid table name")
}

query := fmt.Sprintf("SELECT * FROM %s WHERE id = ?", tableName)
```

### 3. 限制查询结果

```go
// 防止返回过多数据
"max_rows": 100,  // 限制最多返回 100 行
```

### 4. 使用超时

```go
// 防止长时间运行的查询
"timeout": 30,  // 30 秒超时
```

### 5. 最小权限原则

```go
// 为应用创建专用数据库用户，只授予必要权限
// ✅ 推荐：只读用户
CREATE USER 'app_readonly'@'localhost' IDENTIFIED BY 'password';
GRANT SELECT ON mydb.* TO 'app_readonly'@'localhost';

// ⚠️ 避免：使用 root 或高权限账号
```

### 6. 敏感数据脱敏

```go
// 查询后脱敏敏感数据
result := output.Result.(map[string]interface{})
rows := result["rows"].([][]interface{})

for _, row := range rows {
    // 假设第 3 列是手机号
    if phone, ok := row[2].(string); ok && len(phone) > 4 {
        row[2] = phone[:3] + "****" + phone[len(phone)-4:]
    }
}
```

### 7. 记录审计日志

```go
// 记录所有数据库操作
logger.Info("database query",
    "operation", operation,
    "query", sanitizedQuery,  // 不要记录参数值！
    "user", userID,
    "timestamp", time.Now(),
)
```

## 常见 SQL 注入攻击示例

### Union-based 注入

```go
// 恶意输入
userInput := "1 UNION SELECT username, password FROM admin"

// ❌ 危险：字符串拼接
query := fmt.Sprintf("SELECT * FROM products WHERE id = %s", userInput)
// 执行：SELECT * FROM products WHERE id = 1 UNION SELECT username, password FROM admin
// 结果：泄露管理员账号密码

// ✅ 安全：参数化查询
"query":  "SELECT * FROM products WHERE id = ?",
"params": []interface{}{userInput}
// 参��会被转义为字符串 "1 UNION SELECT username, password FROM admin"
// 查询会失败或返回空结果
```

### Boolean-based 注入

```go
// 恶意输入
username := "admin' OR '1'='1"

// ❌ 危险
query := fmt.Sprintf("SELECT * FROM users WHERE username = '%s'", username)
// 执行：SELECT * FROM users WHERE username = 'admin' OR '1'='1'
// 结果：返回所有用户

// ✅ 安全
"query":  "SELECT * FROM users WHERE username = ?",
"params": []interface{}{username}
// username 会被转义，条件永远为 false
```

### Comment-based 注入

```go
// 恶意输入
password := "' OR 1=1--"

// ❌ 危险
query := fmt.Sprintf("SELECT * FROM users WHERE password = '%s' AND status = 'active'", password)
// 执行：SELECT * FROM users WHERE password = '' OR 1=1--' AND status = 'active'
// 注释掉了后面的条件

// ✅ 安全：sanitizeQuery 会检测 -- 并拒绝
```

### Stacked Queries 注入

```go
// 恶意输入
id := "1; DROP TABLE users"

// ❌ 危险
query := fmt.Sprintf("SELECT * FROM products WHERE id = %s", id)
// 执行：SELECT * FROM products WHERE id = 1; DROP TABLE users
// 结果：删除 users 表！

// ✅ 安全：sanitizeQuery 会检测多语句并拒绝
```

## 常见问题

### Q1: 为什么我的查询被拒绝了？

**A:** 检查以下几点：

1. 操作模式是否正确（query 用于 SELECT，execute 用于 INSERT/UPDATE/DELETE）
2. 查询是否包含被禁止的字符（`;`, `--`, `/*`）
3. 是否提供了必需的参数

### Q2: 如何处理动态表名或列名？

**A:** 参数化查询不适用于表名和列名，使用白名单：

```go
func buildQuery(table string, column string, value interface{}) (string, []interface{}, error) {
    // 白名单验证
    allowedTables := map[string]bool{"users": true, "products": true}
    allowedColumns := map[string]bool{"id": true, "name": true, "status": true}

    if !allowedTables[table] {
        return "", nil, errors.New("invalid table")
    }
    if !allowedColumns[column] {
        return "", nil, errors.New("invalid column")
    }

    // 安全拼接表名和列名，参数化值
    query := fmt.Sprintf("SELECT * FROM %s WHERE %s = ?", table, column)
    return query, []interface{}{value}, nil
}
```

### Q3: 事务失败后会自动回滚吗？

**A:** 是的，如果事务中的任何语句失败，整个事务会自动回滚：

```go
// 如果第二条语句失败，第一条插入也会被回滚
"transaction": [
    {"query": "INSERT INTO accounts ...", "params": [...]},
    {"query": "INSERT INTO invalid_table ...", "params": [...]}, // 失败
]
```

### Q4: 连接池是如何管理的？

**A:** 工具自动管理连接池：

- 使用 `connection_id` 时，连接会被缓存
- 未使用 `connection_id` 时，每次创建新连接
- 调用 `Close()` 关闭所有缓存的连接
- 连接会定期健康检查（ping）

### Q5: 如何处理大结果集？

**A:** 使用以下策略：

```go
// 1. 限制返回行数
"max_rows": 100

// 2. 使用分页
"query": "SELECT * FROM users LIMIT ? OFFSET ?",
"params": []interface{}{limit, offset}

// 3. 只查询需要的列
"query": "SELECT id, name FROM users"  // 而不是 SELECT *
```

## 相关文档

- [GoAgent 工具系统](../../docs/guides/TOOLS.md)
- [安全最佳实践](../../docs/guides/SECURITY.md)
- [错误处理指南](../../errors/README.md)

## 贡献

**发现安全漏洞？**

请通过私密渠道报告，不要公开披露：

1. 发送邮件至安全团队
2. 或通过 GitHub Security Advisory

**改进建议：**

1. 提交 GitHub Issue
2. 提交 Pull Request
3. 参与代码审查

## 许可证

本项目遵循 GoAgent 项目的许可证。
