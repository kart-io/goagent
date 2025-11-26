package practical

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeQuery_EnhancedPatterns tests the enhanced SQL injection patterns
func TestSanitizeQuery_EnhancedPatterns(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantError bool
		errorMsg  string
	}{
		// UNION-based injection tests
		{
			name:      "UNION SELECT injection",
			query:     "SELECT * FROM users WHERE id=1 UNION SELECT * FROM passwords",
			wantError: true,
			errorMsg:  "UNION statements not allowed",
		},
		{
			name:      "UNION ALL SELECT injection",
			query:     "SELECT * FROM users WHERE id=1 UNION ALL SELECT password FROM admin",
			wantError: true,
			errorMsg:  "UNION statements not allowed",
		},
		{
			name:      "UNION with mixed case",
			query:     "SELECT * FROM users WHERE id=1 UnIoN SeLeCt * FROM passwords",
			wantError: true,
			errorMsg:  "UNION statements not allowed",
		},
		{
			name:      "UNION ALL with mixed case",
			query:     "SELECT * FROM users WHERE id=1 uNiOn AlL SeLeCt * FROM admin",
			wantError: true,
			errorMsg:  "UNION statements not allowed",
		},

		// Boolean-based injection tests
		{
			name:      "OR 1=1 injection",
			query:     "SELECT * FROM users WHERE name='admin' OR 1=1",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "OR '1'='1' injection with single quotes",
			query:     "SELECT * FROM users WHERE name='admin' OR '1'='1'",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "OR \"1\"=\"1\" injection with double quotes",
			query:     `SELECT * FROM users WHERE name='admin' OR "1"="1"`,
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "OR `1`=`1` injection with backticks",
			query:     "SELECT * FROM users WHERE name='admin' OR `1`=`1`",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "AND 1=1 injection",
			query:     "SELECT * FROM users WHERE id=5 AND 1=1",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "AND '1'='1' injection",
			query:     "SELECT * FROM users WHERE id=5 AND '1'='1'",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "AND \"1\"=\"1\" injection",
			query:     `SELECT * FROM users WHERE id=5 AND "1"="1"`,
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "AND `1`=`1` injection",
			query:     "SELECT * FROM users WHERE id=5 AND `1`=`1`",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "OR TRUE injection",
			query:     "SELECT * FROM users WHERE name='admin' OR TRUE",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "AND TRUE injection",
			query:     "SELECT * FROM users WHERE name='admin' AND TRUE",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "Mixed case OR 1=1",
			query:     "SELECT * FROM users WHERE name='admin' oR 1=1",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},
		{
			name:      "Mixed case OR TRUE",
			query:     "SELECT * FROM users WHERE name='admin' Or TrUe",
			wantError: true,
			errorMsg:  "suspicious boolean expression detected",
		},

		// Valid queries that should pass
		{
			name:      "Valid SELECT with WHERE",
			query:     "SELECT * FROM users WHERE id = ?",
			wantError: false,
		},
		{
			name:      "Valid SELECT with multiple conditions",
			query:     "SELECT * FROM users WHERE id = ? AND status = ?",
			wantError: false,
		},
		{
			name:      "Valid SELECT with OR for legitimate reasons",
			query:     "SELECT * FROM users WHERE email = ? OR username = ?",
			wantError: false,
		},
		{
			name:      "Valid INSERT",
			query:     "INSERT INTO users (name, email) VALUES (?, ?)",
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
			name:      "Valid query with trailing semicolon",
			query:     "SELECT * FROM users WHERE id = ?;",
			wantError: false,
		},

		// Edge cases - legitimate queries that might look suspicious but are safe with parameters
		{
			name:      "Comparison with parameter (not 1=1)",
			query:     "SELECT * FROM users WHERE active = ? AND role = ?",
			wantError: false,
		},
		{
			name:      "Boolean column comparison with parameter",
			query:     "SELECT * FROM users WHERE is_admin = ?",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeQuery(tt.query)

			if tt.wantError {
				assert.Error(t, err, "Expected error for query: %s", tt.query)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg,
						"Error message should contain expected text for query: %s", tt.query)
				}
			} else {
				assert.NoError(t, err, "Unexpected error for valid query: %s", tt.query)
			}
		})
	}
}

// TestSanitizeQuery_CombinedInjectionAttempts tests multiple injection techniques combined
func TestSanitizeQuery_CombinedInjectionAttempts(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string // Expected to be blocked by this pattern
	}{
		{
			name:     "UNION with comment",
			query:    "SELECT * FROM users WHERE id=1 UNION SELECT * FROM admin -- bypass",
			expected: "SQL comments not allowed",
		},
		{
			name:     "Boolean injection with comment",
			query:    "SELECT * FROM users WHERE name='admin' OR 1=1 --",
			expected: "SQL comments not allowed",
		},
		{
			name:     "Stacked query with UNION",
			query:    "SELECT * FROM users; SELECT * FROM admin UNION SELECT * FROM passwords",
			expected: "multiple SQL statements not allowed",
		},
		{
			name:     "Boolean and comment combination",
			query:    "SELECT * FROM users WHERE id=1 /* comment */ AND '1'='1'",
			expected: "SQL comments not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeQuery(tt.query)
			assert.Error(t, err, "Combined injection attempt should be blocked: %s", tt.query)
			assert.Contains(t, err.Error(), tt.expected,
				"Error should mention specific pattern that blocked it")
		})
	}
}

// TestSanitizeQuery_CaseSensitivity verifies case-insensitive detection
func TestSanitizeQuery_CaseSensitivity(t *testing.T) {
	patterns := []string{
		"SELECT * FROM users WHERE id=1 union select * FROM admin",
		"SELECT * FROM users WHERE id=1 UNION select * FROM admin",
		"SELECT * FROM users WHERE id=1 UnIoN SeLeCt * FROM admin",
		"SELECT * FROM users WHERE name='x' or 1=1",
		"SELECT * FROM users WHERE name='x' OR 1=1",
		"SELECT * FROM users WHERE name='x' Or 1=1",
		"SELECT * FROM users WHERE name='x' oR 1=1",
		"SELECT * FROM users WHERE name='x' or true",
		"SELECT * FROM users WHERE name='x' OR TRUE",
		"SELECT * FROM users WHERE name='x' Or TrUe",
	}

	for _, query := range patterns {
		t.Run(query, func(t *testing.T) {
			err := sanitizeQuery(query)
			assert.Error(t, err, "Case variation should still be blocked: %s", query)
		})
	}
}

// TestSanitizeQuery_WhitelistSafety documents what queries are considered safe
func TestSanitizeQuery_WhitelistSafety(t *testing.T) {
	safeQueries := []string{
		"SELECT * FROM users WHERE id = ?",
		"SELECT * FROM users WHERE email = ? OR username = ?",
		"SELECT * FROM orders WHERE created_at > ? AND status = ?",
		"INSERT INTO users (name, email, role) VALUES (?, ?, ?)",
		"UPDATE users SET last_login = ? WHERE id = ?",
		"DELETE FROM sessions WHERE expires_at < ?",
		"SELECT COUNT(*) FROM users WHERE active = ?",
		"SELECT u.*, p.* FROM users u JOIN profiles p ON u.id = p.user_id WHERE u.id = ?",
	}

	for _, query := range safeQueries {
		t.Run(query, func(t *testing.T) {
			err := sanitizeQuery(query)
			assert.NoError(t, err, "Safe parameterized query should pass: %s", query)
		})
	}
}
