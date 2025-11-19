package testhelpers

import (
	"os"
	"testing"
)

// SkipIfNoEnv skips the test if the specified environment variable is not set
func SkipIfNoEnv(t *testing.T, envVar string) {
	t.Helper()
	if os.Getenv(envVar) == "" {
		t.Skipf("%s environment variable not set", envVar)
	}
}

// RequireEnv requires the specified environment variable to be set, returns its value
func RequireEnv(t *testing.T, envVar string) string {
	t.Helper()
	value := os.Getenv(envVar)
	if value == "" {
		t.Fatalf("%s environment variable not set", envVar)
	}
	return value
}
