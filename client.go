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

type ReplyStyle string

const (
	ReplyStyleUser ReplyStyle = "user"
	ReplyStyleBuild ReplyStyle = "build"
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

	logfNoTime := func(format string, args ...interface{}) {
		fmt.Printf(format + "\n", args...)
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

	done := make(chan struct{})
	startHintLoop(ReplyStyleBuild, options{
		logf: logf,
		logfNoTime: logfNoTime,
		done: done,
	})
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
	return strings.ReplaceAll(reply, "`whats_next`", "`"+GetProgramName()+"`")
}

type options struct {
	logf func(format string, args ...interface{})
	logfNoTime func(format string, args ...interface{})
	done chan struct{}
}

func startHintLoop(style ReplyStyle, opts options) {
	if style == ReplyStyleBuild {
		go runBuildHintLoop(opts)
	} else {
		go runUserHintLoop(opts)
	}
}

func runUserHintLoop(opts options) {
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

	for {
		// between 5-30s
		randSec := rand.Intn(25) + 5

		select {
		case <-opts.done:
			opts.logf("User done thinking.")
			return
		case <-time.After(time.Duration(randSec) * time.Second):
			hint := randHint()
			opts.logf(hint)
		}
	}
}

func runBuildHintLoop(opts options) {
	buildSteps := []struct {
		name    string
		percent int
	}{
		{"Reading files", 0},
		{"Parsing configuration", 2},
		{"Build Dependency github.com/gin-gonic/gin", 5},
		{"Build Dependency github.com/stretchr/testify", 10},
		{"Build Dependency github.com/spf13/cobra", 15},
		{"Build Dependency github.com/spf13/viper", 20},
		{"Build Dependency google.golang.org/grpc", 28},
		{"Build Dependency google.golang.org/protobuf", 35},
		{"Build Dependency github.com/go-redis/redis", 42},
		{"Build Dependency github.com/jackc/pgx", 50},
		{"Build Dependency github.com/aws/aws-sdk-go", 58},
		{"Compiling internal packages", 65},
		{"Compiling main package", 72},
		{"Linking dependencies", 80},
		{"Generating binary", 88},
		{"Running post-build hooks", 90},
		{"Verifying checksums", 91},
		{"Optimizing binary", 92},
		{"Stripping debug symbols", 93},
		{"Compressing assets", 94},
		{"Generating metadata", 95},
		{"Updating cache", 96},
		{"Cleaning temp files", 97},
		{"Writing output", 98},
		{"Finalizing build", 99},
	}

	stepIndex := 0
	finalPercent := 99.0 // Track progress after reaching 99%
	for {
		select {
		case <-opts.done:
			opts.logfNoTime("Build completed.")
			return
		case <-time.After(time.Duration(rand.Intn(16)+5) * time.Second):
			step := buildSteps[stepIndex]

			if stepIndex < len(buildSteps)-1 {
				// Normal steps: show integer percent
				opts.logfNoTime("%s... %d%%", step.name, step.percent)
				stepIndex++
			} else {
				// Final step (99%): show incremental decimal progress
				// Format with varying precision to look natural
				var percentStr string
				if finalPercent == 99.0 {
					percentStr = "99"
				} else if finalPercent < 99.1 {
					percentStr = fmt.Sprintf("%.2f", finalPercent)
				} else {
					percentStr = fmt.Sprintf("%.1f", finalPercent)
				}
				opts.logfNoTime("%s... %s%%", step.name, percentStr)

				// Increment by a small random amount (0.01 to 0.15), but never reach 100
				increment := 0.01 + rand.Float64()*0.14
				if finalPercent+increment >= 100.0 {
					increment = 0.01 // Tiny increment if close to 100
				}
				finalPercent += increment
				if finalPercent >= 99.99 {
					finalPercent = 99.99 // Cap at 99.99
				}
			}
		}
	}
}