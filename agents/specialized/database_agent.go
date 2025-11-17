package specialized

import (
	"context"
	"time"

	"gorm.io/gorm"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/logger/core"
)

// DatabaseAgent 数据库操作 Agent
// 提供安全的数据库查询和操作能力
type DatabaseAgent struct {
	*agentcore.BaseAgent
	db     *gorm.DB
	logger core.Logger
}

// NewDatabaseAgent 创建数据库 Agent
func NewDatabaseAgent(db *gorm.DB, logger core.Logger) *DatabaseAgent {
	return &DatabaseAgent{
		BaseAgent: agentcore.NewBaseAgent(
			"database-agent",
			"Executes database queries and operations with safety controls",
			[]string{
				"query_execution",
				"data_retrieval",
				"transaction_management",
				"connection_pooling",
			},
		),
		db:     db,
		logger: logger.With("agent", "database"),
	}
}

// Execute 执行数据库操作
func (a *DatabaseAgent) Execute(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	start := time.Now()

	// 解析参数
	operation, ok := input.Context["operation"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "operation is required").
			WithComponent("database_agent").
			WithOperation("Execute")
	}

	a.logger.Info("Executing database operation",
		"operation", operation)

	var result interface{}
	var err error

	// 应用超时
	if input.Options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, input.Options.Timeout)
		defer cancel()
	}

	// 根据操作类型执行
	switch operation {
	case "query":
		result, err = a.executeQuery(ctx, input)
	case "exec":
		result, err = a.executeExec(ctx, input)
	case "create":
		result, err = a.executeCreate(ctx, input)
	case "update":
		result, err = a.executeUpdate(ctx, input)
	case "delete":
		result, err = a.executeDelete(ctx, input)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unknown operation").
			WithComponent("database_agent").
			WithOperation("Execute").
			WithContext("operation", operation)
	}

	// 构建输出
	output := &agentcore.AgentOutput{
		Status: "success",
		Result: result,
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "database",
				Input: map[string]interface{}{
					"operation": operation,
				},
				Output:   result,
				Duration: time.Since(start),
				Success:  err == nil,
			},
		},
		Latency:   time.Since(start),
		Timestamp: start,
	}

	if err != nil {
		output.Status = "failed"
		output.Message = "Database operation failed"
		output.ToolCalls[0].Error = err.Error()
		return output, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "database operation failed").
			WithComponent("database_agent").
			WithOperation("Execute").
			WithContext("operation", operation)
	}

	output.Message = "Database operation completed successfully"

	return output, nil
}

// executeQuery 执行查询
func (a *DatabaseAgent) executeQuery(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	sql, ok := input.Context["sql"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "sql is required").
			WithComponent("database_agent").
			WithOperation("executeQuery")
	}

	args, _ := input.Context["args"].([]interface{})

	var results []map[string]interface{}
	if err := a.db.WithContext(ctx).Raw(sql, args...).Scan(&results).Error; err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"rows":  results,
		"count": len(results),
	}, nil
}

// executeExec 执行 SQL 语句
func (a *DatabaseAgent) executeExec(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	sql, ok := input.Context["sql"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "sql is required").
			WithComponent("database_agent").
			WithOperation("executeExec")
	}

	args, _ := input.Context["args"].([]interface{})

	result := a.db.WithContext(ctx).Exec(sql, args...)
	if result.Error != nil {
		return nil, result.Error
	}

	return map[string]interface{}{
		"rows_affected": result.RowsAffected,
	}, nil
}

// executeCreate 创建记录
func (a *DatabaseAgent) executeCreate(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	tableName, ok := input.Context["table"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "table is required").
			WithComponent("database_agent").
			WithOperation("executeCreate")
	}

	data, ok := input.Context["data"].(map[string]interface{})
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "data is required").
			WithComponent("database_agent").
			WithOperation("executeCreate")
	}

	if err := a.db.WithContext(ctx).Table(tableName).Create(data).Error; err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"created": true,
		"data":    data,
	}, nil
}

// executeUpdate 更新记录
func (a *DatabaseAgent) executeUpdate(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	tableName, ok := input.Context["table"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "table is required").
			WithComponent("database_agent").
			WithOperation("executeUpdate")
	}

	data, ok := input.Context["data"].(map[string]interface{})
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "data is required").
			WithComponent("database_agent").
			WithOperation("executeUpdate")
	}

	where, _ := input.Context["where"].(map[string]interface{})

	query := a.db.WithContext(ctx).Table(tableName)
	for k, v := range where {
		query = query.Where(k+" = ?", v)
	}

	result := query.Updates(data)
	if result.Error != nil {
		return nil, result.Error
	}

	return map[string]interface{}{
		"updated":       true,
		"rows_affected": result.RowsAffected,
	}, nil
}

// executeDelete 删除记录
func (a *DatabaseAgent) executeDelete(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	tableName, ok := input.Context["table"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "table is required").
			WithComponent("database_agent").
			WithOperation("executeDelete")
	}

	where, ok := input.Context["where"].(map[string]interface{})
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "where is required").
			WithComponent("database_agent").
			WithOperation("executeDelete")
	}

	query := a.db.WithContext(ctx).Table(tableName)
	for k, v := range where {
		query = query.Where(k+" = ?", v)
	}

	result := query.Delete(nil)
	if result.Error != nil {
		return nil, result.Error
	}

	return map[string]interface{}{
		"deleted":       true,
		"rows_affected": result.RowsAffected,
	}, nil
}

// Query 执行查询
func (a *DatabaseAgent) Query(ctx context.Context, sql string, args ...interface{}) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "query",
			"sql":       sql,
			"args":      args,
		},
	})
}

// Create 创建记录
func (a *DatabaseAgent) Create(ctx context.Context, table string, data map[string]interface{}) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "create",
			"table":     table,
			"data":      data,
		},
	})
}

// Update 更新记录
func (a *DatabaseAgent) Update(ctx context.Context, table string, data, where map[string]interface{}) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "update",
			"table":     table,
			"data":      data,
			"where":     where,
		},
	})
}

// Delete 删除记录
func (a *DatabaseAgent) Delete(ctx context.Context, table string, where map[string]interface{}) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"table":     table,
			"where":     where,
		},
	})
}
