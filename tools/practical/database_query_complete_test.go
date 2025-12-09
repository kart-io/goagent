package practical

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	_ "github.com/mattn/go-sqlite3"
)

// TestDatabaseQueryTool_SanitizeQuery_BooleanInjection 测试布尔注入检测
func TestDatabaseQueryTool_SanitizeQuery_BooleanInjection(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		shouldError bool
	}{
		{
			name:        "正常查询",
			query:       "SELECT * FROM users WHERE id = 1",
			shouldError: false,
		},
		{
			name:        "布尔注入 OR 1=1",
			query:       "SELECT * FROM users WHERE id = 1 OR 1=1",
			shouldError: true,
		},
		{
			name:        "布尔注入 AND 1=1",
			query:       "SELECT * FROM users WHERE id = 1 AND 1=1",
			shouldError: true,
		},
		{
			name:        "布尔注入 OR TRUE",
			query:       "SELECT * FROM users WHERE active = 1 OR TRUE",
			shouldError: true,
		},
		{
			name:        "布尔注入 OR '1'='1'",
			query:       "SELECT * FROM users WHERE name = 'admin' OR '1'='1'",
			shouldError: true,
		},
		{
			name:        "多语句注入",
			query:       "SELECT * FROM users; DROP TABLE users",
			shouldError: true,
		},
		{
			name:        "注释注入--",
			query:       "SELECT * FROM users WHERE id = 1 -- comment",
			shouldError: true,
		},
		{
			name:        "注释注入/**/",
			query:       "SELECT * FROM users WHERE id = 1 /* comment */",
			shouldError: true,
		},
		{
			name:        "UNION注入",
			query:       "SELECT * FROM users UNION SELECT * FROM admin",
			shouldError: true,
		},
		{
			name:        "UNION ALL注入",
			query:       "SELECT * FROM users UNION ALL SELECT password FROM credentials",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeQuery(tt.query)
			if tt.shouldError && err == nil {
				t.Errorf("期望错误，但未发生：%s", tt.query)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("不期望错误，但发生了：%v", err)
			}
		})
	}
}

// TestDatabaseQueryTool_Execute_QueryOperation 测试查询操作
func TestDatabaseQueryTool_Execute_QueryOperation(t *testing.T) {
	// 创建测试数据库
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败：%v", err)
	}
	defer db.Close()

	// 创建测试表
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT,
			email TEXT
		)
	`)
	if err != nil {
		t.Fatalf("创建表失败：%v", err)
	}

	// 插入测试数据
	_, err = db.Exec(`
		INSERT INTO users (id, name, email) VALUES
		(1, 'Alice', 'alice@example.com'),
		(2, 'Bob', 'bob@example.com'),
		(3, 'Charlie', 'charlie@example.com')
	`)
	if err != nil {
		t.Fatalf("插入数据失败：%v", err)
	}

	tool := NewDatabaseQueryTool()
	// 直接设置连接用于测试
	tool.connections["test_db"] = db

	ctx := context.Background()

	tests := []struct {
		name          string
		args          map[string]interface{}
		shouldError   bool
		expectedRows  int
		checkFunction func(t *testing.T, result interface{})
	}{
		{
			name: "简单 SELECT 查询",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "SELECT * FROM users",
				"operation": "query",
			},
			shouldError:  false,
			expectedRows: 3,
			checkFunction: func(t *testing.T, result interface{}) {
				resultMap, ok := result.(map[string]interface{})
				if !ok {
					t.Fatal("结果应该是 map")
				}
				// 实现返回 []string 类型的 columns
				columns, ok := resultMap["columns"].([]string)
				if !ok {
					t.Fatal("columns 应该存在并且是 []string 类型")
				}
				if len(columns) != 3 {
					t.Errorf("期望 3 列，得到 %d", len(columns))
				}
			},
		},
		{
			name: "带参数的 SELECT 查询",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "SELECT * FROM users WHERE id = ?",
				"params":    []interface{}{1},
				"operation": "query",
			},
			shouldError:  false,
			expectedRows: 1,
		},
		{
			name: "非 SELECT 查询应该失败",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "INSERT INTO users (id, name, email) VALUES (4, 'Dave', 'dave@example.com')",
				"operation": "query",
			},
			shouldError: true,
		},
		{
			name: "DESCRIBE 查询应该成功",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				// SQLite 不支持 DESCRIBE，但支持 SHOW 和 SELECT
				// 实现只支持 SELECT/SHOW/DESCRIBE，使用 SELECT 语法代替
				"query":     "SELECT name, type FROM sqlite_master WHERE type='table' AND name='users'",
				"operation": "query",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			output, err := tool.Execute(ctx, input)

			if tt.shouldError && err == nil {
				t.Error("期望错误，但未发生")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("不期望错误，但发生了：%v", err)
			}

			if !tt.shouldError && output != nil && output.Result != nil {
				if tt.checkFunction != nil {
					tt.checkFunction(t, output.Result)
				}
			}
		})
	}
}

// TestDatabaseQueryTool_Execute_ExecuteOperation 测试 INSERT/UPDATE/DELETE 操作
func TestDatabaseQueryTool_Execute_ExecuteOperation(t *testing.T) {
	// 创建测试数据库
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败：%v", err)
	}
	defer db.Close()

	// 创建测试表
	_, err = db.Exec(`
		CREATE TABLE products (
			id INTEGER PRIMARY KEY,
			name TEXT,
			price REAL
		)
	`)
	if err != nil {
		t.Fatalf("创建表失败：%v", err)
	}

	tool := NewDatabaseQueryTool()
	tool.connections["test_db"] = db
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		shouldError bool
		checkRows   func(t *testing.T, result interface{})
	}{
		{
			name: "INSERT 操作",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "INSERT INTO products (id, name, price) VALUES (1, 'Laptop', 999.99)",
				"operation": "execute",
			},
			shouldError: false,
			checkRows: func(t *testing.T, result interface{}) {
				resultMap, ok := result.(map[string]interface{})
				if !ok {
					t.Fatal("结果应该是 map")
				}
				rows, ok := resultMap["rows_affected"].(int64)
				if !ok {
					t.Fatal("rows_affected 应该存在")
				}
				if rows != 1 {
					t.Errorf("期望 1 行受影响，得到 %d", rows)
				}
			},
		},
		{
			name: "UPDATE 操作",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "UPDATE products SET price = 899.99 WHERE id = 1",
				"operation": "execute",
			},
			shouldError: false,
		},
		{
			name: "DELETE 操作",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "DELETE FROM products WHERE id = 1",
				"operation": "execute",
			},
			shouldError: false,
		},
		{
			name: "SELECT 操作应该失败",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"query":     "SELECT * FROM products",
				"operation": "execute",
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			output, err := tool.Execute(ctx, input)

			if tt.shouldError && err == nil {
				t.Error("期望错误，但未发生")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("不期望错误，但发生了：%v", err)
			}

			if !tt.shouldError && output != nil && tt.checkRows != nil {
				tt.checkRows(t, output.Result)
			}
		})
	}
}

// TestDatabaseQueryTool_Execute_TransactionOperation 测试事务操作
func TestDatabaseQueryTool_Execute_TransactionOperation(t *testing.T) {
	// 创建测试数据库
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败：%v", err)
	}
	defer db.Close()

	// 创建测试表
	_, err = db.Exec(`
		CREATE TABLE accounts (
			id INTEGER PRIMARY KEY,
			balance REAL
		)
	`)
	if err != nil {
		t.Fatalf("创建表失败：%v", err)
	}

	// 插入初始数据
	_, err = db.Exec(`
		INSERT INTO accounts (id, balance) VALUES
		(1, 1000),
		(2, 500)
	`)
	if err != nil {
		t.Fatalf("插入数据失败：%v", err)
	}

	tool := NewDatabaseQueryTool()
	tool.connections["test_db"] = db
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		shouldError bool
	}{
		{
			name: "成功的事务",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"operation": "transaction",
				"transaction": []map[string]interface{}{
					{
						"query":  "UPDATE accounts SET balance = balance - 100 WHERE id = 1",
						"params": []interface{}{},
					},
					{
						"query":  "UPDATE accounts SET balance = balance + 100 WHERE id = 2",
						"params": []interface{}{},
					},
				},
			},
			shouldError: false,
		},
		{
			name: "空事务应该失败",
			args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver":        "sqlite3",
					"connection_id": "test_db",
				},
				"operation":   "transaction",
				"transaction": []interface{}{},
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			output, err := tool.Execute(ctx, input)

			if tt.shouldError && err == nil {
				t.Error("期望错误，但未发生")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("不期望错误，但发生了：%v", err)
			}

			if !tt.shouldError && output != nil {
				resultMap, ok := output.Result.(map[string]interface{})
				if !ok {
					t.Fatal("结果应该是 map")
				}
				if _, ok := resultMap["transaction_results"]; !ok {
					t.Error("transaction_results 应该存在")
				}
			}
		})
	}
}

// TestDatabaseQueryTool_ConvertValue 测试值类型转换
func TestDatabaseQueryTool_ConvertValue(t *testing.T) {
	tool := NewDatabaseQueryTool()

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "字符串",
			input:    []byte("hello"),
			expected: "hello",
		},
		{
			name:     "时间转 RFC3339",
			input:    time.Date(2025, 12, 2, 14, 0, 0, 0, time.UTC),
			expected: "2025-12-02T14:00:00Z",
		},
		{
			name:     "nil 值",
			input:    nil,
			expected: nil,
		},
		{
			name:     "数字",
			input:    42,
			expected: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.convertValue(tt.input)
			if result != tt.expected {
				t.Errorf("期望 %v，得到 %v", tt.expected, result)
			}
		})
	}
}

// TestDatabaseQueryTool_ParseDBInput_Validation 测试输入验证
func TestDatabaseQueryTool_ParseDBInput_Validation(t *testing.T) {
	tool := NewDatabaseQueryTool()

	tests := []struct {
		name        string
		input       interface{}
		shouldError bool
	}{
		{
			name: "缺少 driver",
			input: map[string]interface{}{
				"connection": map[string]interface{}{
					"dsn": "file::memory:",
				},
				"query":     "SELECT 1",
				"operation": "query",
			},
			shouldError: true,
		},
		{
			name: "缺少 DSN 和 connection_id",
			input: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver": "sqlite3",
				},
				"query":     "SELECT 1",
				"operation": "query",
			},
			shouldError: true,
		},
		{
			name: "query 操作缺少 query 字段",
			input: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver": "sqlite3",
					"dsn":    "file::memory:",
				},
				"operation": "query",
			},
			shouldError: true,
		},
		{
			name: "有效的查询输入",
			input: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver": "sqlite3",
					"dsn":    "file::memory:",
				},
				"query":     "SELECT 1",
				"operation": "query",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.parseDBInput(tt.input)
			if tt.shouldError && err == nil {
				t.Error("期望错误，但未发生")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("不期望错误，但发生了：%v", err)
			}
		})
	}
}

// TestDatabaseQueryTool_Timeout 测试超时机制
func TestDatabaseQueryTool_Timeout(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败：%v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER)")
	if err != nil {
		t.Fatalf("创建表失败：%v", err)
	}

	tool := NewDatabaseQueryTool()
	tool.connections["test_db"] = db

	// 创建一个已取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"connection": map[string]interface{}{
				"driver":        "sqlite3",
				"connection_id": "test_db",
			},
			"query":     "SELECT 1",
			"operation": "query",
		},
	}

	// 超时或取消的上下文应该导致错误
	_, err = tool.Execute(ctx, input)
	if err == nil {
		t.Error("期望在取消的上下文中出错")
	}
}

// TestDatabaseQueryTool_MaxRows 测试行限制
func TestDatabaseQueryTool_MaxRows(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败：%v", err)
	}
	defer db.Close()

	// 创建测试表和数据
	_, err = db.Exec(`
		CREATE TABLE numbers (id INTEGER PRIMARY KEY)
	`)
	if err != nil {
		t.Fatalf("创建表失败：%v", err)
	}

	for i := 1; i <= 100; i++ {
		_, err = db.Exec("INSERT INTO numbers (id) VALUES (?)", i)
		if err != nil {
			t.Fatalf("插入数据失败：%v", err)
		}
	}

	tool := NewDatabaseQueryTool()
	tool.connections["test_db"] = db

	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"connection": map[string]interface{}{
				"driver":        "sqlite3",
				"connection_id": "test_db",
			},
			"query":     "SELECT * FROM numbers",
			"max_rows":  10,
			"operation": "query",
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	resultMap, ok := output.Result.(map[string]interface{})
	if !ok {
		t.Fatal("结果应该是 map")
	}

	rows, ok := resultMap["rows"].([][]interface{})
	if !ok {
		t.Fatal("rows 应该存在")
	}

	if len(rows) > 10 {
		t.Errorf("期望最多 10 行，得到 %d", len(rows))
	}
}

// TestDatabaseQueryTool_Close 测试连接关闭
func TestDatabaseQueryTool_Close(t *testing.T) {
	tool := NewDatabaseQueryTool()

	// 创建一些虚拟连接
	db1, _ := sql.Open("sqlite3", ":memory:")
	db2, _ := sql.Open("sqlite3", ":memory:")

	tool.connections["conn1"] = db1
	tool.connections["conn2"] = db2

	err := tool.Close()
	if err != nil {
		t.Errorf("关闭失败：%v", err)
	}

	if len(tool.connections) != 0 {
		t.Error("关闭后连接映射应该为空")
	}
}
