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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xhd2015/less-gen/flags"
	"golang.org/x/term"
)

const (
	SERVER_PORT = 7654
)

// Global state for background input handling
type InputMessage struct {
	Content    string
	WorkingDir string
	Error      error
}

type serveHandler struct {
	mutex       sync.Mutex
	inputChan   chan InputMessage
	inputCtx    context.Context
	inputCancel context.CancelFunc

	clientConn int64
	program    *tea.Program
}

func (h *serveHandler) hasProcessingClient() bool {
	return atomic.LoadInt64(&h.clientConn) > 0
}

func (h *serveHandler) setProgram(program *tea.Program) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.program = program

	if program != nil {
		if h.clientConn == 0 {
			go program.Send(disableTimerMsg{})
		} else {
			go program.Send(enableTimerMsg{})
		}
	}
}

func (h *serveHandler) notifyRequestAccepted() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.clientConn++

	if h.program == nil {
		return
	}
	// send message to enable timer
	h.program.Send(enableTimerMsg{})
}

func (h *serveHandler) notifyRequestFinished() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.clientConn--

	if h.program == nil {
		return
	}
	if h.clientConn == 0 {
		h.program.Send(disableTimerMsg{})
	}
}

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
	return createInput(os.Stdout, wd, readTerminalOptions{
		showTimer: true,
	})
}

func createInput(w io.Writer, workingDir string, opts readTerminalOptions) error {
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

		if isTerminal {
			lines, err = readInputFromTerminal(ctx, &hasInput, TIMEOUT, opts)
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
func startBackgroundInputLoop(h *serveHandler) {
	h.inputChan = make(chan InputMessage, 1) // Buffered to prevent blocking
	h.inputCtx, h.inputCancel = context.WithCancel(context.Background())

	go func() {
		defer close(h.inputChan)

		for {
			select {
			case <-h.inputCtx.Done():
				return
			default:
				// Get current working directory for context
				wd, _ := os.Getwd()

				Logf("Waiting for input...")

				// Read input using the existing acceptInput logic
				var content strings.Builder
				err := createInput(&content, wd, readTerminalOptions{
					showTimer: h.hasProcessingClient(),
					getContentAfterUser: func() string {
						conn := atomic.LoadInt64(&h.clientConn)
						if conn == 0 {
							return "(staging)"
						}
						if conn == 1 {
							return "(client connected)"
						}
						return fmt.Sprintf("(%d clients connected)", conn)
					},
					onCreatedProgram: func(program *tea.Program) {
						Logf("program created")
						h.setProgram(program)
					},
					onProgramFinished: func(program *tea.Program) {
						Logf("program finished")
						h.setProgram(nil)
					},
				})

				msg := InputMessage{
					Content:    content.String(),
					WorkingDir: wd,
					Error:      err,
				}

				// Send the input to the channel (non-blocking)
				select {
				case h.inputChan <- msg:
					Logf("Input captured and ready for clients")
				case <-h.inputCtx.Done():
					return
				}
			}
		}
	}()
}

// stopBackgroundInputLoop stops the background input goroutine
func stopBackgroundInputLoop(h *serveHandler) {
	if h.inputCancel != nil {
		h.inputCancel()
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

	h := &serveHandler{}

	// Start the background input loop
	startBackgroundInputLoop(h)

	// Ensure cleanup on exit
	defer stopBackgroundInputLoop(h)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.notifyRequestAccepted()
		defer h.notifyRequestFinished()

		Logf("Client connected")

		w.Header().Set("Content-Type", "text/plain")

		deadline := time.Now().Add(10 * time.Minute)

		handleRequest(h, w, r, deadline)
	})

	return http.ListenAndServe(fmt.Sprintf(":%d", SERVER_PORT), nil)
}

func handleRequest(h *serveHandler, w http.ResponseWriter, r *http.Request, deadline time.Time) {
	workingDir := r.URL.Query().Get("workingDir")

	// Wait for input from the background goroutine
	select {
	case msg, ok := <-h.inputChan:
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
	case <-time.After(time.Until(deadline)): // Timeout for client requests
		http.Error(w, "Timeout waiting for input", http.StatusRequestTimeout)
		Logf("Client request timed out")
	}
}
