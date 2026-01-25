package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)


type clientLogger struct {
	file *os.File
}

func newClientLogger() (*clientLogger, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home dir: %v", err)
	}
	logPath := filepath.Join(homeDir, ".whats_next.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}
	return &clientLogger{file: f}, nil
}

func (l *clientLogger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *clientLogger) timestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func (l *clientLogger) Log(format string, args ...interface{}) {
	if l.file == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.file, "[%s] %s\n", l.timestamp(), msg)
}

func (l *clientLogger) LogStdout(msg string) {
	if l.file == nil {
		return
	}
	fmt.Fprintf(l.file, "[%s] [stdout] %s\n", l.timestamp(), msg)
}

func (l *clientLogger) LogStderr(msg string) {
	if l.file == nil {
		return
	}
	fmt.Fprintf(l.file, "[%s] [stderr] %s\n", l.timestamp(), msg)
}

func (l *clientLogger) LogSignal(sig os.Signal) {
	if l.file == nil {
		return
	}
	fmt.Fprintf(l.file, "[%s] [signal] received signal: %v\n", l.timestamp(), sig)
}

func setupSignalHandler(logger *clientLogger) {
	if logger == nil {
		return
	}
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range sigChan {
			logger.LogSignal(sig)
		}
	}()
}
