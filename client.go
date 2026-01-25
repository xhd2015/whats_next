package main

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/xhd2015/less-gen/flags"
)

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

	logf := func(format string, args ...interface{}) {
		dateTime :="["+ time.Now().Format("2006-01-02T15:04:05") +"]"
		fmt.Printf(dateTime + " " + format + "\n", args...)
	}

	startTime := time.Now()
	addr := getServerAddrWithPort(port)
	if !isAddrReachable(addr) {
		for i := 0; i < 10; i++ {
			logf("waiting for server to be ready...")
			time.Sleep(10 * time.Second)
			if isAddrReachable(addr) {
				break
			}
		}
	}

	hints := []string{
		"User is typing...",
		"User is thinking...",
		"User is checking the code...",
		"User is debugging...",
		"User is reviewing the code...",
		"User is reading the doc...",
		"User is building the project...",
		"User is running the tests...",
		"User is fixing the tests...",
		"User is updating the CHANGELOG...",
	}
	randHint := func() string {
		return hints[rand.Intn(len(hints))]
	}

	done := make(chan struct{})
	go func() {
		for {
			// between 5-30s
			randSec := rand.Intn(25) + 5

			select {
			case <-done:
				logf("User done thinking.")
				return
			case <-time.After(time.Duration(randSec) * time.Second):
				hint := randHint()
				logf(hint)
			}
		}
	}()
	params := make(url.Values)
	params.Set("workingDir", wd)
	params.Set("programName", GetProgramName())
	resp, err := http.Get(fmt.Sprintf("http://%s/?%s", addr, params.Encode()))
	close(done)
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

	reply = replaceWhatsNextWithProgramName(reply)

	fmt.Print(reply)
	return nil
}

func replaceWhatsNextWithProgramName(reply string) string {
	return strings.ReplaceAll(reply, "`whats_next`", "`" + GetProgramName() + "`")
}