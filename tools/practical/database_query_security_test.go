package practical

import (
	"context"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3" // Import SQLite driver for tests
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

// TestDatabaseQuery_SQLInjectionPrevention tests SQL injection protection
func TestDatabaseQuery_SQLInjectionPrevention(t *testing.T) {
	tool := NewDatabaseQueryTool()
	ctx := context.Background()

	injectionAttempts := []struct {
		name        string
		query       string
		expectBlock bool
		reason      string
	}{
		{
			name:        "Union injection",
			query:       "SELECT * FROM users WHERE id=1 UNION SELECT * FROM passwords",
			expectBlock: true,
			reason:      "UNION statements might be legitimate in some cases",
		},
		{
			name:        "Comment injection with --",
			query:       "SELECT * FROM users WHERE name='admin'--'",
			expectBlock: true,
			reason:      "SQL comments with -- should be blocked",
		},
		{
			name:        "Block comment injection",
			query:       "SELECT * FROM users WHERE id=1 /* comment */ OR 1=1",
			expectBlock: true,
			reason:      "Block comments /* */ should be blocked",
		},
		{
			name:        "Stacked queries",
			query:       "SELECT * FROM users; DROP TABLE users",
			expectBlock: true,
			reason:      "Multiple statements (semicolon in middle) should be blocked",
		},
		{
			name:        "Stacked queries with spaces",
			query:       "SELECT * FROM users ; DROP TABLE users",
			expectBlock: true,
			reason:      "Multiple statements with spaces should be blocked",
		},
		{
			name:        "Valid query with trailing semicolon",
			query:       "SELECT * FROM users WHERE id = ?;",
			expectBlock: false,
			reason:      "Valid queries with trailing semicolon should be allowed",
		},
		{
			name:        "Valid parameterized query",
			query:       "SELECT * FROM users WHERE id = ?",
			expectBlock: false,
			reason:      "Valid parameterized queries should be allowed",
		},
		{
			name:        "Valid INSERT query",
			query:       "INSERT INTO users (name, email) VALUES (?, ?)",
			expectBlock: false,
			reason:      "Valid INSERT statements should be allowed",
		},
	}

	for _, tt := range injectionAttempts {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{
				Args: map[string]interface{}{
					"connection": map[string]interface{}{
						"driver": "sqlite",
						"dsn":    ":memory:",
					},
					"query":     tt.query,
					"operation": "query",
				},
			}

			output, err := tool.Execute(ctx, input)

			if tt.expectBlock {
				// Should be blocked by security checks BEFORE connection attempt
				assert.Error(t, err, "%s: Expected security error but got none", tt.reason)
				if err != nil {
					errMsg := err.Error()
					// Verify it's a security-related error (not a connection error)
					hasSecurityError := strings.Contains(errMsg, "not allowed") ||
						strings.Contains(errMsg, "security") ||
						strings.Contains(errMsg, "suspicious")
					// Skip connection errors - they happen after sanitization
					if !strings.Contains(errMsg, "connection error") && !strings.Contains(errMsg, "unknown driver") {
						assert.True(t, hasSecurityError,
							"%s: Error message should indicate security issue, got: %s", tt.reason, errMsg)
					}
				}
			} else {
				// Should not be blocked by security checks
				// Note: The query may still fail for other reasons (e.g., table doesn't exist)
				// but it shouldn't fail due to sanitization
				if err != nil {
					errMsg := err.Error()
					assert.False(t,
						strings.Contains(errMsg, "not allowed") && strings.Contains(errMsg, "security"),
						"%s: Valid query was incorrectly blocked: %s", tt.reason, tt.query)
				}
				// If no error, verify we got output
				if err == nil {
					assert.NotNil(t, output, "%s: Expected output for valid query", tt.reason)
				}
			}
		})
	}
}

// TestDatabaseQuery_SanitizeQuery directly tests the sanitizeQuery function
func TestDatabaseQuery_SanitizeQuery(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "Valid SELECT",
			query:     "SELECT * FROM users WHERE id = ?",
			wantError: false,
		},
		{
			name:      "Valid INSERT",
			query:     "INSERT INTO users (name) VALUES (?)",
			wantError: false,
		},
		{
			name:      "Valid UPDATE",
			query:     "UPDATE users SET name = ? WHERE id = ?",
			wantError: false,
		},
		{
			name:      "Valid DELETE",
			query:     "DELETE FROM users WHERE id = ?",
			wantError: false,
		},
		{
			name:      "Query with trailing semicolon",
			query:     "SELECT * FROM users;",
			wantError: false,
		},
		{
			name:      "Multiple statements",
			query:     "SELECT * FROM users; DROP TABLE users",
			wantError: true,
			errorMsg:  "multiple SQL statements not allowed",
		},
		{
			name:      "SQL comment with --",
			query:     "SELECT * FROM users WHERE name='admin'--",
			wantError: true,
			errorMsg:  "SQL comments not allowed",
		},
		{
			name:      "SQL comment with -- and text",
			query:     "SELECT * FROM users -- get all users",
			wantError: true,
			errorMsg:  "SQL comments not allowed",
		},
		{
			name:      "Block comment /**/",
			query:     "SELECT * FROM users /* comment */",
			wantError: true,
			errorMsg:  "SQL comments not allowed",
		},
		{
			name:      "Complex block comment",
			query:     "SELECT * FROM users WHERE id=1 /* bypass */ OR 1=1",
			wantError: true,
			errorMsg:  "SQL comments not allowed",
		},
		{
			name:      "Semicolon in middle",
			query:     "SELECT * FROM users WHERE name='test;value'",
			wantError: true,
			errorMsg:  "multiple SQL statements not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeQuery(tt.query)

			if tt.wantError {
				require.Error(t, err, "Expected error for query: %s", tt.query)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg,
						"Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error for valid query: %s", tt.query)
			}
		})
	}
}

// TestDatabaseQuery_ParameterizedVsStringConcat demonstrates the importance of parameterized queries
func TestDatabaseQuery_ParameterizedVsStringConcat(t *testing.T) {
	// This test demonstrates why parameterized queries are essential

	maliciousInput := "' OR '1'='1"

	// Dangerous: String concatenation (would create SQL injection vulnerability)
	// We don't actually execute this, just show what NOT to do
	dangerousQuery := "SELECT * FROM users WHERE name='" + maliciousInput + "'"
	t.Logf("DANGEROUS (never do this): %s", dangerousQuery)
	// This would result in: SELECT * FROM users WHERE name='' OR '1'='1'
	// Which would return all users!

	// Safe: Parameterized query (the RIGHT way)
	safeQuery := "SELECT * FROM users WHERE name = ?"
	t.Logf("SAFE (always do this): %s with params: [%s]", safeQuery, maliciousInput)

	// Verify that the safe query passes sanitization
	err := sanitizeQuery(safeQuery)
	assert.NoError(t, err, "Safe parameterized query should pass sanitization")

	// Note: The dangerous query would only be caught if it contains blocked patterns
	// The best defense is ALWAYS using parameterized queries with the 'params' field
}

// TestDatabaseQuery_SecurityBestPractices documents security best practices
func TestDatabaseQuery_SecurityBestPractices(t *testing.T) {
	t.Run("Always use parameterized queries", func(t *testing.T) {
		tool := NewDatabaseQueryTool()
		// CORRECT: Using parameterized query with params field
		input := &interfaces.ToolInput{
			Args: map[string]interface{}{
				"connection": map[string]interface{}{
					"driver": "sqlite",
					"dsn":    ":memory:",
				},
				"query":     "SELECT * FROM users WHERE name = ? AND email = ?",
				"params":    []interface{}{"john", "john@example.com"},
				"operation": "query",
			},
		}

		// This should pass sanitization
		params, err := tool.parseDBInput(input.Args)
		require.NoError(t, err, "Valid input should parse successfully")
		err = sanitizeQuery(params.Query)
		assert.NoError(t, err, "Parameterized query should pass sanitization")
	})

	t.Run("Reject queries with inline values", func(t *testing.T) {
		// INCORRECT: Embedding user input directly in query string
		// This example shows a query that tries to bypass security with comments
		query := "SELECT * FROM users WHERE name='admin'-- AND password='fake'"

		err := sanitizeQuery(query)
		assert.Error(t, err, "Query with comment injection should be rejected")
		if err != nil {
			assert.True(t,
				strings.Contains(err.Error(), "security") || strings.Contains(err.Error(), "not allowed"),
				"Error should mention security or be blocked, got: %s", err.Error())
		}
	})

	t.Run("Transaction security", func(t *testing.T) {
		// Ensure transactions also validate each query - test sanitization directly
		maliciousQuery := "SELECT * FROM users; DROP TABLE users"

		err := sanitizeQuery(maliciousQuery)
		assert.Error(t, err, "Transaction with malicious query should fail sanitization")
		assert.Contains(t, err.Error(), "not allowed", "Should indicate multiple statements not allowed")
	})
}

// TestDatabaseQuery_EdgeCases tests edge cases in sanitization
func TestDatabaseQuery_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantError bool
	}{
		{
			name:      "Empty query",
			query:     "",
			wantError: false, // sanitizeQuery allows empty, but parseDBInput will catch it
		},
		{
			name:      "Whitespace only",
			query:     "   \t\n   ",
			wantError: false, // Trimmed to empty
		},
		{
			name:      "Semicolon only",
			query:     ";",
			wantError: false, // Trailing semicolon is allowed
		},
		{
			name:      "Multiple semicolons at end",
			query:     "SELECT * FROM users;;",
			wantError: false, // The second semicolon makes it "end with semicolon"
		},
		{
			name:      "Semicolon with whitespace at end",
			query:     "SELECT * FROM users ;  ",
			wantError: false, // Trailing semicolon with whitespace
		},
		{
			name:      "Double dash in string literal (theoretical)",
			query:     "SELECT * FROM users WHERE name LIKE '%--test%'",
			wantError: true, // Still blocked - can't distinguish from comment
		},
		{
			name:      "Slash star in string (theoretical)",
			query:     "SELECT * FROM posts WHERE content LIKE '%/* comment */%'",
			wantError: true, // Still blocked - can't distinguish from real comment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeQuery(tt.query)
			if tt.wantError {
				assert.Error(t, err, "Expected error for: %s", tt.query)
			} else {
				assert.NoError(t, err, "Unexpected error for: %s", tt.query)
			}
		})
	}
}

// TestDatabaseQuery_RealWorldScenarios tests realistic usage patterns
func TestDatabaseQuery_RealWorldScenarios(t *testing.T) {
	t.Run("Valid complex SELECT with JOINs", func(t *testing.T) {
		query := `
			SELECT u.id, u.name, o.order_id
			FROM users u
			INNER JOIN orders o ON u.id = o.user_id
			WHERE u.created_at > ?
			ORDER BY u.name ASC
			LIMIT 100
		`

		err := sanitizeQuery(query)
		assert.NoError(t, err, "Valid complex SELECT should pass")
	})

	t.Run("Valid INSERT with multiple values", func(t *testing.T) {
		query := "INSERT INTO logs (timestamp, level, message) VALUES (?, ?, ?)"

		err := sanitizeQuery(query)
		assert.NoError(t, err, "Valid INSERT should pass")
	})

	t.Run("Valid UPDATE with multiple conditions", func(t *testing.T) {
		query := `
			UPDATE users
			SET last_login = ?, status = ?
			WHERE id = ? AND active = ?
		`

		err := sanitizeQuery(query)
		assert.NoError(t, err, "Valid UPDATE should pass")
	})

	t.Run("Attempt to bypass with case variations", func(t *testing.T) {
		queries := []string{
			"SELECT * FROM users WHERE id=1--",
			"SELECT * FROM users WHERE id=1 --",
			"SELECT * FROM users WHERE id=1-- comment",
			"SELECT * FROM users /* comment */ WHERE id=1",
		}

		for _, query := range queries {
			err := sanitizeQuery(query)
			assert.Error(t, err, "Should block comment pattern: %s", query)
		}
	})
}

// TestDatabaseQuery_SQLiteInMemory tests with actual SQLite in-memory database
// This test requires CGO and the SQLite driver to be available
func TestDatabaseQuery_SQLiteInMemory(t *testing.T) {
	tool := NewDatabaseQueryTool()
	ctx := context.Background()

	// Create a table
	createTableInput := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"connection": map[string]interface{}{
				"driver":        "sqlite",
				"dsn":           ":memory:",
				"connection_id": "test_conn",
			},
			"query":     "CREATE TABLE IF NOT EXISTS test_users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)",
			"operation": "execute",
		},
	}

	output, err := tool.Execute(ctx, createTableInput)
	if err != nil && strings.Contains(err.Error(), "unknown driver") {
		t.Skip("SQLite driver not available (requires CGO)")
	}
	require.NoError(t, err, "Table creation should succeed")
	require.NotNil(t, output)

	// Try SQL injection in INSERT
	t.Run("Injection attempt in parameterized INSERT", func(t *testing.T) {
		// Even with malicious input, parameterized queries are safe
		maliciousName := "'; DROP TABLE test_users; --"

		insertInput := &interfaces.ToolInput{
			Args: map[string]interface{}{
				"connection": map[string]interface{}{
					"connection_id": "test_conn",
					"driver":        "sqlite",
				},
				"query":     "INSERT INTO test_users (name, email) VALUES (?, ?)",
				"params":    []interface{}{maliciousName, "evil@example.com"},
				"operation": "execute",
			},
		}

		_, err := tool.Execute(ctx, insertInput)
		require.NoError(t, err, "Parameterized INSERT should succeed safely")

		// Verify the table still exists and data is safe
		selectInput := &interfaces.ToolInput{
			Args: map[string]interface{}{
				"connection": map[string]interface{}{
					"connection_id": "test_conn",
					"driver":        "sqlite",
				},
				"query":     "SELECT * FROM test_users WHERE name = ?",
				"params":    []interface{}{maliciousName},
				"operation": "query",
			},
		}

		output, err = tool.Execute(ctx, selectInput)
		require.NoError(t, err, "SELECT should succeed")

		// The malicious string should be stored as literal data, not executed
		result, ok := output.Result.(map[string]interface{})
		require.True(t, ok, "Result should be a map")
		rows, ok := result["rows"].([][]interface{})
		require.True(t, ok, "Should have rows")
		require.Len(t, rows, 1, "Should have exactly one row")
		assert.Equal(t, maliciousName, rows[0][1], "Name should be stored literally")
	})

	// Cleanup
	err = tool.Close()
	assert.NoError(t, err, "Tool cleanup should succeed")
}

// TestDatabaseQuery_TransactionSecurity tests transaction security
func TestDatabaseQuery_TransactionSecurity(t *testing.T) {
	t.Run("Reject transaction with comment injection", func(t *testing.T) {
		// Test that comment injection is caught during validation
		maliciousQuery := "UPDATE users SET admin=1 WHERE id=1--"

		err := sanitizeQuery(maliciousQuery)
		assert.Error(t, err, "Transaction query with comment injection should fail sanitization")
		assert.True(t,
			strings.Contains(err.Error(), "security") || strings.Contains(err.Error(), "not allowed"),
			"Error should mention security or be blocked, got: %s", err.Error())
	})

	t.Run("Reject transaction with multiple statements", func(t *testing.T) {
		// Test that stacked queries are caught
		maliciousQuery := "INSERT INTO users (name) VALUES ('Bob'); DROP TABLE users"

		err := sanitizeQuery(maliciousQuery)
		assert.Error(t, err, "Transaction with stacked queries should fail sanitization")
		assert.Contains(t, err.Error(), "not allowed", "Should indicate multiple statements not allowed")
	})
}
