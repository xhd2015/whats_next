package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xhd2015/less-gen/flags"
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

func handleClient(args []string) error {
	logger, err := newClientLogger()
	if err != nil {
		// Log to stderr but continue without file logging
		fmt.Fprintf(os.Stderr, "Warning: failed to create logger: %v\n", err)
	}
	if logger != nil {
		defer logger.Close()
		setupSignalHandler(logger)
	}

	var port int
	args, err = flags.Int("--port", &port).Parse(args)
	if err != nil {
		return err
	}
	if port == 0 {
		port = SERVER_PORT
	}

	wd, _ := os.Getwd()
	pid := os.Getpid()

	// Log initial request
	if logger != nil {
		logger.Log("client: dir=%s, request to port %d, pid=%d", wd, port, pid)
	}

	startTime := time.Now()
	addr := getServerAddrWithPort(port)

	params := make(url.Values)
	params.Set("workingDir", wd)
	params.Set("programName", GetProgramName())
	resp, err := http.Get(fmt.Sprintf("http://%s/?%s", addr, params.Encode()))
	if err != nil {
		errMsg := ""
		// if is connection refused, ask the client to retry again in 10 seconds, this retry could be repeated up to 10 times
		// check if the server is running
		if !isAddrReachable(addr) {
			// this is a way to tell LLM that it should wait user to spin the server up
			errMsg = "the server is busy, please `sleep 10` and try again. This retry could be repeated up to 10 times"
		} else {
			errMsg = fmt.Sprintf("failed to connect to server (is it running?): %v\nStart server with: %s serve", err, GetProgramName())
		}
		if logger != nil {
			logger.LogStderr(errMsg)
		}
		return fmt.Errorf("%s", errMsg)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("server returned status: %d", resp.StatusCode)
		if logger != nil {
			logger.LogStderr(errMsg)
		}
		return fmt.Errorf("%s", errMsg)
	}

	// Read and display the response from server
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errMsg := fmt.Sprintf("failed to read response: %v", err)
		if logger != nil {
			logger.LogStderr(errMsg)
		}
		return fmt.Errorf("%s", errMsg)
	}

	waitTime := time.Since(startTime)
	reply := string(body)

	// Log response
	if logger != nil {
		logger.Log("client: dir=%s, request to port %d, pid=%d, wait time=%v, response len=%d, server response: %s", wd, port, pid, waitTime, len(reply), reply)
	}

	// Log stdout
	if logger != nil {
		logger.LogStdout(reply)
	}

	fmt.Print(reply)
	return nil
}