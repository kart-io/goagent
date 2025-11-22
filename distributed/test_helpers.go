//go:build test || !prod
// +build test !prod

package distributed

import (
	"github.com/kart-io/logger"
	"github.com/kart-io/logger/core"
	"github.com/kart-io/logger/option"
)

// createTestLogger creates a logger for testing
func createTestLogger() core.Logger {
	log, _ := logger.New(&option.LogOption{
		Engine: "zap",
		Level:  "ERROR",
	})
	return log
}

// createTestLoggerRegistry creates a logger for registry testing
func createTestLoggerRegistry() core.Logger {
	return createTestLogger()
}
