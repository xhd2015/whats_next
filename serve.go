package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/xhd2015/less-gen/flags"
)

const (
	SERVER_PORT = 7654

	// TODO: let's propose a dialog when this is completed
	//
	//   osascript -e 'display dialog "Task completed!" buttons {"OK"} default button "OK" with title "Task"'

	// tested: 2m works fine,
	// tested: 3m works fine
	// let's try 3m
	TIMEOUT = 3 * time.Minute
	// TIMEOUT = 1 * time.Second
	// TIMEOUT = 5 * time.Second // for testing
)

func handleClient(args []string) error {
	wd, _ := os.Getwd()

	addr := getServerAddr()
	resp, err := http.Get(fmt.Sprintf("http://%s/?workingDir=%s", addr, url.QueryEscape(wd)))
	if err != nil {
		// if is connection refused, ask the client to retry again in 10 seconds, this retry could be repeated up to 10 times
		// check if the server is running
		if !isAddrReachable(addr) {
			// this is a way to tell LLM that it should wait user to spin the server up
			return fmt.Errorf("the server is busy, please `sleep 10` and try again. This retry could be repeated up to 10 times")
		}
		return fmt.Errorf("failed to connect to server (is it running?): %v\nStart server with: whats_next serve", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	// Read and display the response from server
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	fmt.Print(string(body))
	return nil
}

func handleServer(args []string) error {
	var logFlag bool
	var kill bool
	args, err := flags.
		Bool("--log", &logFlag).
		Bool("--kill", &kill).
		Parse(args)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		return fmt.Errorf("unrecognized extra args: %s", strings.Join(args, " "))
	}

	if logFlag {
		if err := initLoggers(); err != nil {
			return err
		}
		defer closeLoggers()
	}
	serverAddr := getServerAddr()
	if kill {
		// get to /kill and send a POST request
		resp, err := http.Get(fmt.Sprintf("http://%s/kill", serverAddr))
		if err != nil {
			if !isAddrReachable(serverAddr) {
				fmt.Fprintf(os.Stderr, "Server is not running\n")
				return nil
			}
			return fmt.Errorf("failed to send kill request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to kill server: %d", resp.StatusCode)
		}
		fmt.Printf("Server %s killed\n", serverAddr)
		return nil
	}

	if isAddrReachable(serverAddr) {
		fmt.Printf("Server %s is already running\n", serverAddr)
		return nil
	}

	mux := http.NewServeMux()
	server := &http.Server{Addr: serverAddr, Handler: mux}

	h := &serveHandler{
		httpServer: server,
	}

	// Start the background input loop
	h.startBackgroundInputLoop()

	// Ensure cleanup on exit
	defer h.shutdown(context.Background())

	mux.HandleFunc("/kill", func(w http.ResponseWriter, r *http.Request) {
		h.requestShutdown()
		ctx := context.Background()

		// must be handled in a goroutine
		// otherwise the serve won't be closed due to graceful shutdown
		go h.shutdown(ctx)
		Logf("Server killed")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if h.isShutdownRequested() {
			http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
			return
		}
		h.notifyRequestAccepted()
		defer h.notifyRequestFinished()

		Logf("Client connected")

		idleDeadline := time.Now().Add(TIMEOUT)
		h.setClientWaitDeadline(idleDeadline)

		w.Header().Set("Content-Type", "text/plain")

		deadline := time.Now().Add(10 * time.Minute)

		handleRequest(h, w, r, idleDeadline, deadline)

		if h.isShutdownRequested() {
			Logf("Client request finished, shutting down server")
			go h.shutdown(context.Background())
		}
	})

	fmt.Printf("Starting server on port %d...", SERVER_PORT)
	serverErr := server.ListenAndServe()
	if h.isShutdownRequested() {
		return nil
	}
	return serverErr
}

func handleRequest(h *serveHandler, w http.ResponseWriter, r *http.Request, idleDeadline time.Time, hardDeadline time.Time) {
	workingDir := r.URL.Query().Get("workingDir")

	finalWorkingDir := workingDir

	// Wait for input from the background goroutine

	// for the first message, wait forever
	// for subsequent messages, try read as many as possible
	var msgs []InputMessage

	waitForFirstMsg := true
	for waitForFirstMsg {
		waitForFirstMsg = false
		select {
		case msg, ok := <-h.inputChan:
			Logf("Client received input")
			if !ok {
				http.Error(w, "Input channel closed", http.StatusInternalServerError)
				Errorf("Input channel closed")
				return
			}
			if msg.Exit {
				fmt.Fprintln(w, "exit")
				return
			}
			msgs = append(msgs, msg)
		case <-time.After(time.Until(hardDeadline)): // Timeout for client requests
			http.Error(w, "Timeout waiting for input", http.StatusRequestTimeout)
			Logf("Client request timed out")
			return
		case <-time.After(time.Until(idleDeadline)):
			if !h.hasInputContent() {
				Logf("input idle for %v, send thinking", TIMEOUT)
				fmt.Fprintln(w, isThinking())
				return
			} else {
				waitForFirstMsg = true
			}
		}
	}

	// read subsequent messages
	more := true
	for more {
		select {
		case msg := <-h.inputChan:
			msgs = append(msgs, msg)
		default:
			more = false
		}
	}

	Logf("Client request received %d messages", len(msgs))

	var contents []string
	var errors []string
	for _, msg := range msgs {
		if msg.Exit {
			fmt.Fprintln(w, "exit")
			return
		}
		// Use the working directory from the client request if provided,
		// otherwise use the one from the input message
		if finalWorkingDir == "" {
			finalWorkingDir = msg.WorkingDir
		}
		if msg.Error != nil {
			errors = append(errors, msg.Error.Error())
			continue
		}
		contents = append(contents, msg.Content)
	}
	if len(errors) > 0 {
		fmt.Fprintln(w, "error:"+strings.Join(errors, "\n"))
		return
	}

	content := strings.Join(contents, "\n")
	Logf("Client request content: %s", content)
	// Process the content and add the appropriate context

	if content != "" {
		resp := wrapQuestionWithGuidelines(content, finalWorkingDir)
		fmt.Fprintln(w, resp)
	} else {
		fmt.Fprintln(w, isThinking())
	}

	Logf("Client request finished")
}

func getServerAddr() string {
	return fmt.Sprintf("localhost:%d", SERVER_PORT)
}

func isAddrReachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
