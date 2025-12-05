// Package common 提供 multiagent 示例的公共组件
package common

import (
	"context"
	"fmt"
	"os"

	loggercore "github.com/kart-io/logger/core"
)

// SimpleLogger 提供简单的日志实现，用于示例演示
// 实现 loggercore.Logger 接口
type SimpleLogger struct {
	level loggercore.Level
}

// NewSimpleLogger 创建 SimpleLogger 实例
func NewSimpleLogger() *SimpleLogger {
	return &SimpleLogger{
		level: loggercore.InfoLevel,
	}
}

// Debug 级别日志
func (l *SimpleLogger) Debug(args ...interface{}) {
	if l.level <= loggercore.DebugLevel {
		fmt.Print("[DEBUG] ")
		fmt.Println(args...)
	}
}

// Info 级别日志
func (l *SimpleLogger) Info(args ...interface{}) {
	if l.level <= loggercore.InfoLevel {
		fmt.Print("[INFO] ")
		fmt.Println(args...)
	}
}

// Warn 级别日志
func (l *SimpleLogger) Warn(args ...interface{}) {
	if l.level <= loggercore.WarnLevel {
		fmt.Print("[WARN] ")
		fmt.Println(args...)
	}
}

// Error 级别日志
func (l *SimpleLogger) Error(args ...interface{}) {
	if l.level <= loggercore.ErrorLevel {
		fmt.Print("[ERROR] ")
		fmt.Println(args...)
	}
}

// Fatal 级别日志（打印后退出程序）
func (l *SimpleLogger) Fatal(args ...interface{}) {
	fmt.Print("[FATAL] ")
	fmt.Println(args...)
	os.Exit(1)
}

// Debugf 格式化 Debug 日志
func (l *SimpleLogger) Debugf(template string, args ...interface{}) {
	if l.level <= loggercore.DebugLevel {
		fmt.Printf("[DEBUG] "+template+"\n", args...)
	}
}

// Infof 格式化 Info 日志
func (l *SimpleLogger) Infof(template string, args ...interface{}) {
	if l.level <= loggercore.InfoLevel {
		fmt.Printf("[INFO] "+template+"\n", args...)
	}
}

// Warnf 格式化 Warn 日志
func (l *SimpleLogger) Warnf(template string, args ...interface{}) {
	if l.level <= loggercore.WarnLevel {
		fmt.Printf("[WARN] "+template+"\n", args...)
	}
}

// Errorf 格式化 Error 日志
func (l *SimpleLogger) Errorf(template string, args ...interface{}) {
	if l.level <= loggercore.ErrorLevel {
		fmt.Printf("[ERROR] "+template+"\n", args...)
	}
}

// Fatalf 格式化 Fatal 日志（打印后退出程序）
func (l *SimpleLogger) Fatalf(template string, args ...interface{}) {
	fmt.Printf("[FATAL] "+template+"\n", args...)
	os.Exit(1)
}

// Debugw 结构化 Debug 日志
func (l *SimpleLogger) Debugw(msg string, keysAndValues ...interface{}) {
	if l.level <= loggercore.DebugLevel {
		fmt.Print("[DEBUG] ", msg, " ", formatKeysAndValues(keysAndValues), "\n")
	}
}

// Infow 结构化 Info 日志
func (l *SimpleLogger) Infow(msg string, keysAndValues ...interface{}) {
	if l.level <= loggercore.InfoLevel {
		fmt.Print("[INFO] ", msg, " ", formatKeysAndValues(keysAndValues), "\n")
	}
}

// Warnw 结构化 Warn 日志
func (l *SimpleLogger) Warnw(msg string, keysAndValues ...interface{}) {
	if l.level <= loggercore.WarnLevel {
		fmt.Print("[WARN] ", msg, " ", formatKeysAndValues(keysAndValues), "\n")
	}
}

// Errorw 结构化 Error 日志
func (l *SimpleLogger) Errorw(msg string, keysAndValues ...interface{}) {
	if l.level <= loggercore.ErrorLevel {
		fmt.Print("[ERROR] ", msg, " ", formatKeysAndValues(keysAndValues), "\n")
	}
}

// Fatalw 结构化 Fatal 日志（打印后退出程序）
func (l *SimpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	fmt.Print("[FATAL] ", msg, " ", formatKeysAndValues(keysAndValues), "\n")
	os.Exit(1)
}

// With 返回带字段的 logger
func (l *SimpleLogger) With(keyValues ...interface{}) loggercore.Logger {
	// 简化实现：直接返回自身
	return l
}

// WithCtx 返回带 context 和字段的 logger
func (l *SimpleLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	// 简化实现：直接返回自身
	return l
}

// WithCallerSkip 返回调整 caller skip 的 logger
func (l *SimpleLogger) WithCallerSkip(skip int) loggercore.Logger {
	// 简化实现：直接返回自身
	return l
}

// SetLevel 设置日志级别
func (l *SimpleLogger) SetLevel(level loggercore.Level) {
	l.level = level
}

// Sync 同步日志缓冲区
func (l *SimpleLogger) Sync() error {
	return nil
}

// Flush 刷新日志缓冲区
func (l *SimpleLogger) Flush() error {
	return nil
}

// formatKeysAndValues 格式化键值对为字符串
func formatKeysAndValues(keysAndValues []interface{}) string {
	if len(keysAndValues) == 0 {
		return ""
	}
	result := "["
	for i := 0; i < len(keysAndValues); i += 2 {
		if i > 0 {
			result += " "
		}
		if i+1 < len(keysAndValues) {
			result += fmt.Sprintf("%v %v", keysAndValues[i], keysAndValues[i+1])
		} else {
			result += fmt.Sprintf("%v", keysAndValues[i])
		}
	}
	result += "]"
	return result
}

// 确保 SimpleLogger 实现 loggercore.Logger 接口
var _ loggercore.Logger = (*SimpleLogger)(nil)
