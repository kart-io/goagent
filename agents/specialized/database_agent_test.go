package specialized

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger"
)

// TestModel for database testing
type TestModel struct {
	ID   uint `gorm:"primaryKey"`
	Name string
	Age  int
}

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Create test table
	err = db.AutoMigrate(&TestModel{})
	require.NoError(t, err)

	return db
}

func TestNewDatabaseAgent(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()

	agent := NewDatabaseAgent(db, l)

	assert.Equal(t, "database-agent", agent.Name())
	assert.Contains(t, agent.Capabilities(), "query_execution")
	assert.Contains(t, agent.Capabilities(), "data_retrieval")
	assert.Contains(t, agent.Capabilities(), "transaction_management")
	assert.Contains(t, agent.Capabilities(), "connection_pooling")
}

func TestDatabaseAgent_Execute_Query_Success(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Alice", Age: 30})
	db.Create(&TestModel{Name: "Bob", Age: 25})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       "SELECT * FROM test_models WHERE age > ?",
			"args":      []interface{}{20},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Len(t, output.ToolCalls, 1)
	assert.True(t, output.ToolCalls[0].Success)

	result := output.Result.(map[string]interface{})
	assert.Greater(t, result["count"], 0)
}

func TestDatabaseAgent_Execute_Query_NoResults(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       "SELECT * FROM test_models WHERE age > ?",
			"args":      []interface{}{100},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.Equal(t, 0, result["count"])
}

func TestDatabaseAgent_Execute_Query_MissingSQL(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sql is required")
}

func TestDatabaseAgent_Execute_Exec_Success(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "exec",
			"sql":       "INSERT INTO test_models (name, age) VALUES (?, ?)",
			"args":      []interface{}{"Charlie", 35},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.Equal(t, int64(1), result["rows_affected"])

	// Verify data was inserted
	var count int64
	db.Model(&TestModel{}).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestDatabaseAgent_Execute_Exec_Update(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "David", Age: 40})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "exec",
			"sql":       "UPDATE test_models SET age = ? WHERE name = ?",
			"args":      []interface{}{41, "David"},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.Equal(t, int64(1), result["rows_affected"])
}

func TestDatabaseAgent_Execute_Exec_MissingSQL(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "exec",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sql is required")
}

func TestDatabaseAgent_Execute_Create_Success(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "create",
			"table":     "test_models",
			"data": map[string]interface{}{
				"name": "Eve",
				"age":  28,
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["created"].(bool))

	// Verify data was inserted
	var model TestModel
	err = db.Where("name = ?", "Eve").First(&model).Error
	assert.NoError(t, err)
	assert.Equal(t, "Eve", model.Name)
	assert.Equal(t, 28, model.Age)
}

func TestDatabaseAgent_Execute_Create_MissingTable(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "create",
			"data": map[string]interface{}{
				"name": "Frank",
			},
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table is required")
}

func TestDatabaseAgent_Execute_Create_MissingData(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "create",
			"table":     "test_models",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data is required")
}

func TestDatabaseAgent_Execute_Update_Success(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Grace", Age: 26})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "update",
			"table":     "test_models",
			"data": map[string]interface{}{
				"age": 27,
			},
			"where": map[string]interface{}{
				"name": "Grace",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["updated"].(bool))
	assert.Equal(t, int64(1), result["rows_affected"])

	// Verify update
	var model TestModel
	db.Where("name = ?", "Grace").First(&model)
	assert.Equal(t, 27, model.Age)
}

func TestDatabaseAgent_Execute_Update_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "update",
			"table":     "test_models",
			"data": map[string]interface{}{
				"age": 30,
			},
			"where": map[string]interface{}{
				"name": "NonExistent",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.Equal(t, int64(0), result["rows_affected"])
}

func TestDatabaseAgent_Execute_Update_MissingTable(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "update",
			"data": map[string]interface{}{
				"age": 30,
			},
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table is required")
}

func TestDatabaseAgent_Execute_Update_MissingData(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "update",
			"table":     "test_models",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data is required")
}

func TestDatabaseAgent_Execute_Delete_Success(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Henry", Age: 32})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"table":     "test_models",
			"where": map[string]interface{}{
				"name": "Henry",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["deleted"].(bool))
	assert.Equal(t, int64(1), result["rows_affected"])

	// Verify deletion
	var count int64
	db.Model(&TestModel{}).Where("name = ?", "Henry").Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestDatabaseAgent_Execute_Delete_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"table":     "test_models",
			"where": map[string]interface{}{
				"name": "NonExistent",
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	result := output.Result.(map[string]interface{})
	assert.Equal(t, int64(0), result["rows_affected"])
}

func TestDatabaseAgent_Execute_Delete_MissingTable(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"where": map[string]interface{}{
				"id": 1,
			},
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table is required")
}

func TestDatabaseAgent_Execute_Delete_MissingWhere(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"table":     "test_models",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "where is required")
}

func TestDatabaseAgent_Execute_InvalidOperation(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "invalid",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown operation")
}

func TestDatabaseAgent_Execute_MissingOperation(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestDatabaseAgent_Query(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Ivy", Age: 29})

	output, err := agent.Query(ctx, "SELECT * FROM test_models WHERE name = ?", "Ivy")

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.Greater(t, result["count"], 0)
}

func TestDatabaseAgent_Create(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	output, err := agent.Create(ctx, "test_models", map[string]interface{}{
		"name": "Jack",
		"age":  33,
	})

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestDatabaseAgent_Update(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Karen", Age: 31})

	output, err := agent.Update(ctx, "test_models",
		map[string]interface{}{"age": 32},
		map[string]interface{}{"name": "Karen"},
	)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestDatabaseAgent_Delete(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Liam", Age: 34})

	output, err := agent.Delete(ctx, "test_models", map[string]interface{}{
		"name": "Liam",
	})

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestDatabaseAgent_Execute_WithTimeout(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       "SELECT * FROM test_models",
		},
		Options: agentcore.AgentOptions{
			Timeout: 5 * time.Second,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestDatabaseAgent_Execute_OutputStructure(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	db.Create(&TestModel{Name: "Mike", Age: 36})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       "SELECT * FROM test_models",
		},
	}

	output, err := agent.Execute(ctx, input)

	require.NoError(t, err)

	// Verify output structure
	assert.NotZero(t, output.Latency)
	assert.NotZero(t, output.Timestamp)
	assert.Len(t, output.ToolCalls, 1)

	toolCall := output.ToolCalls[0]
	assert.Equal(t, "database", toolCall.ToolName)
	assert.NotZero(t, toolCall.Duration)
	assert.True(t, toolCall.Success)
	assert.NotEmpty(t, toolCall.Input)
	assert.NotEmpty(t, toolCall.Output)
}

func TestDatabaseAgent_Execute_MultipleWhereConditions(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Nancy", Age: 37})
	db.Create(&TestModel{Name: "Oscar", Age: 38})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"table":     "test_models",
			"where": map[string]interface{}{
				"name": "Nancy",
				"age":  37,
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestDatabaseAgent_Execute_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel immediately
	cancel()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       "SELECT * FROM test_models",
		},
	}

	output, _ := agent.Execute(ctx, input)

	// May succeed or fail depending on timing
	assert.NotNil(t, output)
}

func TestDatabaseAgent_Execute_EmptyQueryResult(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       "SELECT * FROM test_models WHERE 1=0",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	result := output.Result.(map[string]interface{})
	assert.Equal(t, 0, result["count"])
}

func TestDatabaseAgent_Execute_ComplexWhereClause(t *testing.T) {
	db := setupTestDB(t)
	l, _ := logger.NewWithDefaults()
	agent := NewDatabaseAgent(db, l)
	ctx := context.Background()

	// Insert test data
	db.Create(&TestModel{Name: "Peter", Age: 39})
	db.Create(&TestModel{Name: "Quinn", Age: 40})

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "update",
			"table":     "test_models",
			"data": map[string]interface{}{
				"age": 41,
			},
			"where": map[string]interface{}{
				"age": 40,
			},
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	result := output.Result.(map[string]interface{})
	assert.Equal(t, int64(1), result["rows_affected"])
}
