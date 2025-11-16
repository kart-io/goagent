package practical

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/tools"
)

// DatabaseQueryTool executes SQL queries against various databases
type DatabaseQueryTool struct {
	connections map[string]*sql.DB
	maxRows     int
	timeout     time.Duration
}

// NewDatabaseQueryTool creates a new database query tool
func NewDatabaseQueryTool() *DatabaseQueryTool {
	return &DatabaseQueryTool{
		connections: make(map[string]*sql.DB),
		maxRows:     1000,
		timeout:     30 * time.Second,
	}
}

// Name returns the tool name
func (t *DatabaseQueryTool) Name() string {
	return "database_query"
}

// Description returns the tool description
func (t *DatabaseQueryTool) Description() string {
	return "Executes SQL queries against databases with support for MySQL, PostgreSQL, and SQLite"
}

// ArgsSchema returns the arguments schema as a JSON string
func (t *DatabaseQueryTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"connection": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"driver": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"mysql", "postgres", "sqlite"},
						"description": "Database driver",
					},
					"dsn": map[string]interface{}{
						"type":        "string",
						"description": "Data source name (connection string)",
					},
					"connection_id": map[string]interface{}{
						"type":        "string",
						"description": "Reusable connection identifier",
					},
				},
				"required": []string{"driver"},
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "SQL query to execute",
			},
			"params": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": []interface{}{"string", "number", "boolean", "null"}},
				"description": "Query parameters for prepared statements",
			},
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"query", "execute", "transaction"},
				"default":     "query",
				"description": "Operation type",
			},
			"transaction": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query":  map[string]interface{}{"type": "string"},
						"params": map[string]interface{}{"type": "array"},
					},
				},
				"description": "Multiple queries to execute in a transaction",
			},
			"max_rows": map[string]interface{}{
				"type":        "integer",
				"default":     100,
				"description": "Maximum rows to return",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"default":     30,
				"description": "Query timeout in seconds",
			},
		},
		"required": []string{"connection"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// OutputSchema returns the output schema

// Execute runs the database query
func (t *DatabaseQueryTool) Execute(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	params, err := t.parseDBInput(input.Args)
	if err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// Get or create connection
	db, err := t.getConnection(params.Connection)
	if err != nil {
		return nil, fmt.Errorf("connection error: %w", err)
	}

	// Create context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
	defer cancel()

	startTime := time.Now()

	// Execute based on operation type
	var result interface{}
	switch params.Operation {
	case "query":
		result, err = t.executeQuery(queryCtx, db, params)
	case "execute":
		result, err = t.executeStatement(queryCtx, db, params)
	case "transaction":
		result, err = t.executeTransaction(queryCtx, db, params)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", params.Operation)
	}

	executionTime := time.Since(startTime).Milliseconds()

	if err != nil {
		return &tools.ToolOutput{
			Result: map[string]interface{}{
				"error":             err.Error(),
				"execution_time_ms": executionTime,
			},
			Error: err.Error(),
		}, err
	}

	// Add execution time to result
	if resultMap, ok := result.(map[string]interface{}); ok {
		resultMap["execution_time_ms"] = executionTime
	}

	return &tools.ToolOutput{
		Result: result,
	}, nil
}

// Implement Runnable interface
func (t *DatabaseQueryTool) Invoke(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	return t.Execute(ctx, input)
}

func (t *DatabaseQueryTool) Stream(ctx context.Context, input *tools.ToolInput) (<-chan agentcore.StreamChunk[*tools.ToolOutput], error) {
	ch := make(chan agentcore.StreamChunk[*tools.ToolOutput])
	go func() {
		defer close(ch)
		output, err := t.Execute(ctx, input)
		if err != nil {
			ch <- agentcore.StreamChunk[*tools.ToolOutput]{Error: err}
		} else {
			ch <- agentcore.StreamChunk[*tools.ToolOutput]{Data: output}
		}
	}()
	return ch, nil
}

func (t *DatabaseQueryTool) Batch(ctx context.Context, inputs []*tools.ToolInput) ([]*tools.ToolOutput, error) {
	outputs := make([]*tools.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := t.Execute(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (t *DatabaseQueryTool) Pipe(next agentcore.Runnable[*tools.ToolOutput, any]) agentcore.Runnable[*tools.ToolInput, any] {
	return nil
}

func (t *DatabaseQueryTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*tools.ToolInput, *tools.ToolOutput] {
	return t
}

func (t *DatabaseQueryTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*tools.ToolInput, *tools.ToolOutput] {
	return t
}

// getConnection gets or creates a database connection
func (t *DatabaseQueryTool) getConnection(config connectionConfig) (*sql.DB, error) {
	// Check for existing connection
	if config.ConnectionID != "" {
		if db, exists := t.connections[config.ConnectionID]; exists {
			// Verify connection is still alive
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err == nil {
				return db, nil
			}
			// Connection is dead, remove it
			delete(t.connections, config.ConnectionID)
		}
	}

	// Create new connection
	db, err := sql.Open(config.Driver, config.DSN)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	// Store connection if ID provided
	if config.ConnectionID != "" {
		t.connections[config.ConnectionID] = db
	}

	return db, nil
}

// executeQuery executes a SELECT query
func (t *DatabaseQueryTool) executeQuery(ctx context.Context, db *sql.DB, params *dbParams) (interface{}, error) {
	// Validate query is SELECT
	query := strings.TrimSpace(params.Query)
	if !strings.HasPrefix(strings.ToUpper(query), "SELECT") &&
		!strings.HasPrefix(strings.ToUpper(query), "SHOW") &&
		!strings.HasPrefix(strings.ToUpper(query), "DESCRIBE") {
		return nil, fmt.Errorf("query operation only supports SELECT/SHOW/DESCRIBE statements")
	}

	// Execute query
	rows, err := db.QueryContext(ctx, query, params.Params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Prepare result
	result := map[string]interface{}{
		"columns": columns,
		"rows":    make([][]interface{}, 0),
	}

	// Scan rows
	rowCount := 0
	for rows.Next() && rowCount < params.MaxRows {
		// Create slice for row values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan row
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		// Convert values to proper types
		rowData := make([]interface{}, len(columns))
		for i, val := range values {
			rowData[i] = t.convertValue(val)
		}

		result["rows"] = append(result["rows"].([][]interface{}), rowData)
		rowCount++
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// executeStatement executes INSERT/UPDATE/DELETE statements
func (t *DatabaseQueryTool) executeStatement(ctx context.Context, db *sql.DB, params *dbParams) (interface{}, error) {
	// Validate query is not SELECT
	query := strings.TrimSpace(params.Query)
	if strings.HasPrefix(strings.ToUpper(query), "SELECT") {
		return nil, fmt.Errorf("execute operation does not support SELECT statements")
	}

	// Execute statement
	result, err := db.ExecContext(ctx, query, params.Params...)
	if err != nil {
		return nil, err
	}

	// Get affected rows
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()

	return map[string]interface{}{
		"rows_affected":  rowsAffected,
		"last_insert_id": lastInsertID,
	}, nil
}

// executeTransaction executes multiple queries in a transaction
func (t *DatabaseQueryTool) executeTransaction(ctx context.Context, db *sql.DB, params *dbParams) (interface{}, error) {
	if len(params.Transaction) == 0 {
		return nil, fmt.Errorf("transaction requires at least one query")
	}

	// Start transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// Track results
	results := make([]map[string]interface{}, 0)
	totalRowsAffected := int64(0)

	// Execute each query
	for i, query := range params.Transaction {
		result, err := tx.ExecContext(ctx, query.Query, query.Params...)
		if err != nil {
			tx.Rollback()
			return map[string]interface{}{
				"error":       err.Error(),
				"failed_step": i,
			}, err
		}

		rowsAffected, _ := result.RowsAffected()
		lastInsertID, _ := result.LastInsertId()
		totalRowsAffected += rowsAffected

		results = append(results, map[string]interface{}{
			"step":           i,
			"rows_affected":  rowsAffected,
			"last_insert_id": lastInsertID,
		})
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"transaction_results": results,
		"total_rows_affected": totalRowsAffected,
		"queries_executed":    len(params.Transaction),
	}, nil
}

// convertValue converts database values to appropriate Go types
func (t *DatabaseQueryTool) convertValue(val interface{}) interface{} {
	switch v := val.(type) {
	case []byte:
		// Try to parse as JSON
		var jsonVal interface{}
		if err := json.Unmarshal(v, &jsonVal); err == nil {
			return jsonVal
		}
		// Return as string
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339)
	case nil:
		return nil
	default:
		return v
	}
}

// parseDBInput parses the tool input
func (t *DatabaseQueryTool) parseDBInput(input interface{}) (*dbParams, error) {
	var params dbParams

	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}

	// Set defaults
	if params.Operation == "" {
		params.Operation = "query"
	}
	if params.MaxRows == 0 {
		params.MaxRows = t.maxRows
	}
	if params.Timeout == 0 {
		params.Timeout = int(t.timeout.Seconds())
	}

	// Validate required fields
	if params.Connection.Driver == "" {
		return nil, fmt.Errorf("database driver is required")
	}
	if params.Connection.DSN == "" && params.Connection.ConnectionID == "" {
		return nil, fmt.Errorf("either DSN or connection_id is required")
	}

	// Validate operation-specific requirements
	switch params.Operation {
	case "query", "execute":
		if params.Query == "" {
			return nil, fmt.Errorf("query is required for %s operation", params.Operation)
		}
	case "transaction":
		if len(params.Transaction) == 0 {
			return nil, fmt.Errorf("transaction queries are required")
		}
	}

	return &params, nil
}

// Close closes all database connections
func (t *DatabaseQueryTool) Close() error {
	for id, db := range t.connections {
		if err := db.Close(); err != nil {
			return fmt.Errorf("failed to close connection %s: %w", id, err)
		}
	}
	t.connections = make(map[string]*sql.DB)
	return nil
}

type dbParams struct {
	Connection  connectionConfig  `json:"connection"`
	Query       string            `json:"query"`
	Params      []interface{}     `json:"params"`
	Operation   string            `json:"operation"`
	Transaction []transactionStep `json:"transaction"`
	MaxRows     int               `json:"max_rows"`
	Timeout     int               `json:"timeout"`
}

type connectionConfig struct {
	Driver       string `json:"driver"`
	DSN          string `json:"dsn"`
	ConnectionID string `json:"connection_id"`
}

type transactionStep struct {
	Query  string        `json:"query"`
	Params []interface{} `json:"params"`
}

// DatabaseQueryRuntimeTool extends DatabaseQueryTool with runtime support
type DatabaseQueryRuntimeTool struct {
	*DatabaseQueryTool
}

// NewDatabaseQueryRuntimeTool creates a runtime-aware database query tool
func NewDatabaseQueryRuntimeTool() *DatabaseQueryRuntimeTool {
	return &DatabaseQueryRuntimeTool{
		DatabaseQueryTool: NewDatabaseQueryTool(),
	}
}

// ExecuteWithRuntime executes with runtime support
func (t *DatabaseQueryRuntimeTool) ExecuteWithRuntime(ctx context.Context, input *tools.ToolInput, runtime *tools.ToolRuntime) (*tools.ToolOutput, error) {
	// Stream status
	if runtime != nil && runtime.StreamWriter != nil {
		runtime.StreamWriter(map[string]interface{}{
			"status": "executing_query",
			"tool":   t.Name(),
		})
	}

	// Get connection details from runtime if needed
	if runtime != nil {
		params, _ := t.parseDBInput(input.Args)
		if params != nil && params.Connection.DSN == "" {
			// Try to get DSN from runtime state
			key := fmt.Sprintf("db_%s_dsn", params.Connection.Driver)
			if dsn, err := runtime.GetState(key); err == nil {
				params.Connection.DSN = dsn.(string)
			}
		}
	}

	// Execute the query
	result, err := t.Execute(ctx, input)

	// Store query results in runtime for analysis
	if err == nil && runtime != nil {
		params, _ := t.parseDBInput(input.Args)
		if params != nil && params.Operation == "query" {
			// Store recent query results
			runtime.PutToStore([]string{"query_results"}, time.Now().Format(time.RFC3339), result)
		}
	}

	// Stream completion
	if runtime != nil && runtime.StreamWriter != nil {
		runtime.StreamWriter(map[string]interface{}{
			"status": "completed",
			"tool":   t.Name(),
			"error":  err,
		})
	}

	return result, err
}
