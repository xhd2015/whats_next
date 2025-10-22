package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	infoLogger  *slog.Logger
	errorLogger *slog.Logger
	infoFile    *os.File
	errorFile   *os.File
)

// initLoggers initializes the slog loggers for info and error logging
func initLoggers() error {
	// Create logs directory if it doesn't exist
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		// Fallback to current directory if logs dir creation fails
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Setup info logger
	tmpInfoFile, err := os.OpenFile(filepath.Join(logsDir, "info.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fallback to stdout if file creation fails
		return fmt.Errorf("failed to create info file: %w", err)
	}
	infoFile = tmpInfoFile

	infoLogger = slog.New(slog.NewTextHandler(tmpInfoFile, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Setup error logger
	tmpErrorFile, err := os.OpenFile(filepath.Join(logsDir, "error.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fallback to stderr if file creation fails
		return fmt.Errorf("failed to create error file: %w", err)
	}
	errorLogger = slog.New(slog.NewTextHandler(tmpErrorFile, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	errorFile = tmpErrorFile

	return nil
}

func closeLoggers() error {
	if infoFile != nil {
		if err := infoFile.Close(); err != nil {
			return fmt.Errorf("failed to close info file: %w", err)
		}
	}
	if errorFile != nil {
		if err := errorFile.Close(); err != nil {
			return fmt.Errorf("failed to close error file: %w", err)
		}
	}
	return nil
}

// Logf logs an info message to info.txt using slog
func Logf(format string, args ...interface{}) {
	if infoLogger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	infoLogger.Info(message)
}

// Errorf logs an error message to error.txt using slog
func Errorf(format string, args ...interface{}) {
	if errorLogger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	errorLogger.Error(message)
}
