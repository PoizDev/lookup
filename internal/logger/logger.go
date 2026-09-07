package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var globalLogger = zap.NewNop()

// Init initializes the global logger.
func Init(debug bool) {
	var err error
	if debug {
		config := zap.NewDevelopmentConfig()
		config.DisableStacktrace = true
		globalLogger, err = config.Build()
	} else {
		// Production config that is essentially silent (only errors).
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
		globalLogger, err = config.Build()
	}
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

// Debug logs a debug message with the given fields.
func Debug(msg string, fields ...zap.Field) {
	globalLogger.Debug(msg, fields...)
}

// Info logs an info message with the given fields.
func Info(msg string, fields ...zap.Field) {
	globalLogger.Info(msg, fields...)
}

// Warn logs a warning message with the given fields.
func Warn(msg string, fields ...zap.Field) {
	globalLogger.Warn(msg, fields...)
}

// Error logs an error message with the given fields.
func Error(msg string, fields ...zap.Field) {
	globalLogger.Error(msg, fields...)
}

// Fatal logs a fatal message with the given fields.
func Fatal(msg string, fields ...zap.Field) {
	globalLogger.Fatal(msg, fields...)
}

// Sync flushes the logger.
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

// L returns the underlying zap Logger.
func L() *zap.Logger {
	return globalLogger
}
