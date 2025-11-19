package main

import (
	"context"
	"fmt"

	loggercore "github.com/kart-io/logger/core"
)

// simpleLogger 简单的Logger实现用于示例
type simpleLogger struct{}

func newSimpleLogger() loggercore.Logger {
	return &simpleLogger{}
}

func (l *simpleLogger) Debug(args ...interface{}) {
	fmt.Println(append([]interface{}{"[DEBUG]"}, args...)...)
}

func (l *simpleLogger) Debugf(template string, args ...interface{}) {
	fmt.Printf("[DEBUG] "+template+"\n", args...)
}

func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {
	fmt.Println("[DEBUG]", msg, keysAndValues)
}

func (l *simpleLogger) Info(args ...interface{}) {
	fmt.Println(append([]interface{}{"[INFO]"}, args...)...)
}

func (l *simpleLogger) Infof(template string, args ...interface{}) {
	fmt.Printf("[INFO] "+template+"\n", args...)
}

func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{}) {
	fmt.Println("[INFO]", msg, keysAndValues)
}

func (l *simpleLogger) Warn(args ...interface{}) {
	fmt.Println(append([]interface{}{"[WARN]"}, args...)...)
}

func (l *simpleLogger) Warnf(template string, args ...interface{}) {
	fmt.Printf("[WARN] "+template+"\n", args...)
}

func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{}) {
	fmt.Println("[WARN]", msg, keysAndValues)
}

func (l *simpleLogger) Error(args ...interface{}) {
	fmt.Println(append([]interface{}{"[ERROR]"}, args...)...)
}

func (l *simpleLogger) Errorf(template string, args ...interface{}) {
	fmt.Printf("[ERROR] "+template+"\n", args...)
}

func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {
	fmt.Println("[ERROR]", msg, keysAndValues)
}

func (l *simpleLogger) Fatal(args ...interface{}) {
	fmt.Println(append([]interface{}{"[FATAL]"}, args...)...)
}

func (l *simpleLogger) Fatalf(template string, args ...interface{}) {
	fmt.Printf("[FATAL] "+template+"\n", args...)
}

func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	fmt.Println("[FATAL]", msg, keysAndValues)
}

func (l *simpleLogger) With(fields ...interface{}) loggercore.Logger {
	return l
}

func (l *simpleLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}

func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger {
	return l
}

func (l *simpleLogger) WithFields(fields map[string]interface{}) loggercore.Logger {
	return l
}

func (l *simpleLogger) SetLevel(level loggercore.Level) {
	// No-op for simple logger
}

func (l *simpleLogger) Sync() error {
	return nil
}

func (l *simpleLogger) Flush() error {
	return nil
}

