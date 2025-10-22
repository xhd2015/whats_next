package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xhd2015/less-gen/flags"
	"golang.org/x/term"
)

const (
	SERVER_PORT = 7654
)

var clientConn int64

// Global state for background input handling
type InputMessage struct {
	Content    string
	WorkingDir string
	Error      error
}

var (
	inputChan   chan InputMessage
	inputOnce   sync.Once
	inputCtx    context.Context
	inputCancel context.CancelFunc
)

func handleWhatsNext(args []string) error {
	// Check config for mode
	config, err := readConfig()
	if err != nil {
		return err
	}

	// If mode is server, delegate to server mode handler
	if config.Mode == ModeServer {
		return handleWhatsNextInServerMode(args)
	}
	wd, _ := os.Getwd()
	return acceptInput(os.Stdout, wd, nil)
}

func acceptInput(w io.Writer, workingDir string, getContentAfterUser func() string) error {
	// Default to native mode (current logic)
	// wait for user input
	type Result struct {
		Error error
	}
	done := make(chan Result)

	var hasInput int32

	// Create context for timeout cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// Check if stdin is a terminal for enhanced editing
		isTerminal := term.IsTerminal(int(os.Stdin.Fd()))

		var lines []string
		var err error

		if isTerminal && !FORCE_NON_TERMINAL {
			lines, err = readInputFromTerminal(ctx, &hasInput, TIMEOUT, !DISABLE_TIMER, getContentAfterUser)
		} else {
			lines, err = readInputFromNonTerminal(&hasInput)
		}

		if err != nil {
			if err.Error() == "exit" {
				fmt.Fprintln(w, "exit")
				done <- Result{}
				return
			}
			done <- Result{Error: err}
			return
		}
		q := strings.Join(lines, "\n")
		fmt.Fprintf(w, "the user is asking: \n<question>\n%s\n</question>\nplease think step by step and give your answer\n", q)

		fmt.Fprintln(w, "----")

		var printSelectedProfile bool
		// Check for selected profile and print its content
		config, err := readConfig()
		if err == nil && config.SelectedProfile != "" {
			groupDir, err := getGroupConfigPath(false)
			if err == nil {
				profileName := addMDSuffix(config.SelectedProfile)
				groupFile := filepath.Join(groupDir, profileName)
				if profileContent, readErr := os.ReadFile(groupFile); readErr == nil {
					printSelectedProfile = true
					printContent := string(profileContent)
					if workingDir != "" {
						printContent = filterContentByDir(printContent, workingDir, isCursor())
					}
					fmt.Fprintln(w, printContent)
				}
			}
		}
		if !printSelectedProfile {
			fmt.Fprint(w, generalGuideline)
			fmt.Fprint(w, toolCallAwareness)
			fmt.Fprint(w, runningCommand)
		}

		done <- Result{}
	}()

	select {
	case <-time.After(TIMEOUT):
		if atomic.LoadInt32(&hasInput) == 0 {
			// Cancel the context to gracefully quit any running terminal program
			cancel()
			// Wait for the goroutine to finish after cancellation
			<-done
			fmt.Fprintln(w, "The user is thinking, please execute `whats_next` again.")
			fmt.Fprintln(w)
			printlnContent(w, generalGuideline)
			return nil
		}
		// wait forever for done
		<-done
	case result := <-done:
		// Cancel context to stop timer when input is received
		cancel()
		if result.Error != nil {
			return result.Error
		}
		return nil
	}

	return nil
}
func handleWhatsNextInServerMode(args []string) error {
	// Make a request to the server, which will trigger input prompt on server side
	fmt.Println("Processing...")

	wd, _ := os.Getwd()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/?workingDir=%s", SERVER_PORT, url.QueryEscape(wd)))
	if err != nil {
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

// startBackgroundInputLoop starts a background goroutine that continuously reads user input
func startBackgroundInputLoop() {
	inputOnce.Do(func() {
		inputChan = make(chan InputMessage, 1) // Buffered to prevent blocking
		inputCtx, inputCancel = context.WithCancel(context.Background())

		go func() {
			defer close(inputChan)

			for {
				select {
				case <-inputCtx.Done():
					return
				default:
					// Get current working directory for context
					wd, _ := os.Getwd()

					Logf("Waiting for input...")

					// Read input using the existing acceptInput logic
					var content strings.Builder
					err := acceptInput(&content, wd, func() string {
						conn := atomic.LoadInt64(&clientConn)
						if conn == 0 {
							return "(staging)"
						}
						if conn == 1 {
							return "(client connected)"
						}
						return fmt.Sprintf("(%d clients connected)", conn)
					})

					msg := InputMessage{
						Content:    content.String(),
						WorkingDir: wd,
						Error:      err,
					}

					// Send the input to the channel (non-blocking)
					select {
					case inputChan <- msg:
						Logf("Input captured and ready for clients")
					case <-inputCtx.Done():
						return
					}
				}
			}
		}()
	})
}

// stopBackgroundInputLoop stops the background input goroutine
func stopBackgroundInputLoop() {
	if inputCancel != nil {
		inputCancel()
	}
}

func handleServe(args []string) error {
	fmt.Printf("Starting server on port %d...", SERVER_PORT)

	var logFlag bool
	args, err := flags.
		Bool("--log", &logFlag).
		Parse(args)
	if err != nil {
		return err
	}
	if logFlag {
		if err := initLoggers(); err != nil {
			return err
		}
		defer closeLoggers()
	}

	// Start the background input loop
	startBackgroundInputLoop()

	// Ensure cleanup on exit
	defer stopBackgroundInputLoop()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&clientConn, 1)
		defer atomic.AddInt64(&clientConn, -1)

		Logf("Client connected")
		workingDir := r.URL.Query().Get("workingDir")

		w.Header().Set("Content-Type", "text/plain")

		// Wait for input from the background goroutine
		select {
		case msg, ok := <-inputChan:
			if !ok {
				http.Error(w, "Input channel closed", http.StatusInternalServerError)
				Errorf("Input channel closed")
				return
			}

			if msg.Error != nil {
				if msg.Error.Error() == "exit" {
					fmt.Fprint(w, "exit")
					Logf("Client received exit command")
					return
				}
				http.Error(w, msg.Error.Error(), http.StatusInternalServerError)
				Errorf("Error: %v", msg.Error)
				return
			}

			// Use the working directory from the client request if provided,
			// otherwise use the one from the input message
			finalWorkingDir := workingDir
			if finalWorkingDir == "" {
				finalWorkingDir = msg.WorkingDir
			}

			// Process the content and add the appropriate context
			content := msg.Content
			if content != "" {
				// Extract the user question from the content if it follows the expected format
				if strings.Contains(content, "<question>") {
					fmt.Fprint(w, content)
				} else {
					// Format it as a question if it's raw input
					fmt.Fprintf(w, "the user is asking: \n<question>\n%s\n</question>\nplease think step by step and give your answer\n", content)
					fmt.Fprintln(w, "----")

					// Add the guidelines and context
					var printSelectedProfile bool
					config, err := readConfig()
					if err == nil && config.SelectedProfile != "" {
						groupDir, err := getGroupConfigPath(false)
						if err == nil {
							profileName := addMDSuffix(config.SelectedProfile)
							groupFile := filepath.Join(groupDir, profileName)
							if profileContent, readErr := os.ReadFile(groupFile); readErr == nil {
								printSelectedProfile = true
								printContent := string(profileContent)
								if finalWorkingDir != "" {
									printContent = filterContentByDir(printContent, finalWorkingDir, isCursor())
								}
								fmt.Fprintln(w, printContent)
							}
						}
					}
					if !printSelectedProfile {
						fmt.Fprint(w, generalGuideline)
						fmt.Fprint(w, toolCallAwareness)
						fmt.Fprint(w, runningCommand)
					}
				}
			} else {
				fmt.Fprintln(w, "The user is thinking, please execute `whats_next` again.")
				fmt.Fprintln(w)
				printlnContent(w, generalGuideline)
			}

			Logf("Client finished")

		case <-time.After(10 * time.Minute): // Timeout for client requests
			http.Error(w, "Timeout waiting for input", http.StatusRequestTimeout)
			Logf("Client request timed out")
		}
	})

	return http.ListenAndServe(fmt.Sprintf(":%d", SERVER_PORT), nil)
}
