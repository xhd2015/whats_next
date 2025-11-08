package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

func handleWhatsNext(args []string) error {
	// Check config for mode
	config, err := readConfig()
	if err != nil {
		return err
	}

	// If mode is server, delegate to server mode handler
	if config.Mode == ModeServer {
		return handleClient(args)
	}
	wd, _ := os.Getwd()
	return createInput(os.Stdout, wd, readTerminalOptions{
		showTimer: func() bool {
			return true
		},
	})
}

// Global state for background input handling
type InputMessage struct {
	Content    string
	WorkingDir string
	Error      error
	Exit       bool
}

type serveHandler struct {
	mutex sync.Mutex

	inputChan chan InputMessage

	inputCtx    context.Context
	inputCancel context.CancelFunc

	clientConn         int64
	clientWaitDeadline time.Time
	lastInputEmptyTime time.Time
	program            *tea.Program

	httpServer *http.Server

	shutdownRequested bool

	flagHasInputContent int32
}

func (h *serveHandler) hasProcessingClient() bool {
	return atomic.LoadInt64(&h.clientConn) > 0
}

func (h *serveHandler) getClientWaitDeadline() time.Time {
	h.mutex.Lock()
	t := h.clientWaitDeadline
	h.mutex.Unlock()
	return t
}

func (h *serveHandler) setClientWaitDeadline(t time.Time) {
	h.mutex.Lock()
	h.clientWaitDeadline = t
	h.mutex.Unlock()
}

func (h *serveHandler) getLastInputEmptyTime() time.Time {
	h.mutex.Lock()
	t := h.lastInputEmptyTime
	h.mutex.Unlock()
	return t
}

func (h *serveHandler) setLastInputEmptyTime(t time.Time) {
	h.mutex.Lock()
	h.lastInputEmptyTime = t
	h.mutex.Unlock()
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

func (h *serveHandler) hasWaitingClient() bool {
	return atomic.LoadInt64(&h.clientConn) > 0
}

func (h *serveHandler) shutdown(ctx context.Context) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if h.inputCancel != nil {
		h.inputCancel()
		h.inputCancel = nil
	}
	if h.program != nil {
		h.program.Kill()
		h.program = nil
	}
	h.httpServer.Shutdown(ctx)
}

func (h *serveHandler) requestShutdown() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.shutdownRequested = true
}

func (h *serveHandler) isShutdownRequested() bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.shutdownRequested
}

func (h *serveHandler) hasInputContent() bool {
	return atomic.LoadInt32(&h.flagHasInputContent) != 0
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
			lines, err = readInputFromTerminal(ctx, &hasInput, TIMEOUT, opts.onInputUpdate, opts)
		} else {
			lines, err = readInputFromNonTerminal(&hasInput)
		}

		if err != nil {
			if err.Error() == "exit" {
				Logf("exit")
				done <- Result{}
				return
			}
			done <- Result{Error: err}
			return
		}
		q := strings.Join(lines, "\n")
		if opts.noWrapWithGuidelines {
			fmt.Fprintln(w, q)
		} else {
			questionGuidelines := wrapQuestionWithGuidelines(q, workingDir)
			fmt.Fprintln(w, questionGuidelines)
		}
		done <- Result{}
	}()

	result := <-done
	// Cancel context to stop timer when input is received
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func wrapQuestionWithGuidelines(q string, workingDir string) string {
	var s strings.Builder
	var w io.Writer = &s
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
	return s.String()
}

func isThinking() string {
	return strings.Join([]string{
		"The user is thinking, please execute `whats_next` again.",
		"",
		generalGuideline,
	}, "\n")
}

// startBackgroundInputLoop starts a background goroutine that continuously reads user input
func (h *serveHandler) startBackgroundInputLoop() {
	h.inputChan = make(chan InputMessage, 100) // up to 100 messages can be buffered
	h.inputCtx, h.inputCancel = context.WithCancel(context.Background())

	go func() {
		defer close(h.inputChan)

		for {
			if h.isShutdownRequested() {
				return
			}
			select {
			case <-h.inputCtx.Done():
				return
			default:
				// Get current working directory for context
				wd, _ := os.Getwd()

				Logf("Waiting for input...")

				// Read input using the existing acceptInput logic
				var content strings.Builder
				var isExit bool
				err := createInput(&content, wd, readTerminalOptions{
					showTimer:            h.hasProcessingClient,
					noWrapWithGuidelines: true,
					getUserPrompt: func(hasInput bool) string {
						conn := atomic.LoadInt64(&h.clientConn)
						remaining := h.getClientWaitDeadline().Sub(h.getLastInputEmptyTime())
						return renderUserPrompt(conn > 0, true, remaining, int(conn))
					},
					onCreatedProgram: func(program *tea.Program) {
						Logf("program created")
						h.setProgram(program)
					},
					onProgramFinished: func(program *tea.Program) {
						Logf("program finished")
						h.setProgram(nil)
					},
					onInputExit: func() {
						Logf("input exit")
						isExit = true
						h.requestShutdown()
					},
					onInputUpdate: func(hasInput bool) {
						if !hasInput {
							h.setLastInputEmptyTime(time.Now())
						}
						atomic.StoreInt32(&h.flagHasInputContent, toBoolInt32(hasInput))
					},
				})

				msg := InputMessage{
					Content:    content.String(),
					WorkingDir: wd,
					Error:      err,
					Exit:       isExit,
				}

				if h.isShutdownRequested() {
					if !h.hasWaitingClient() {
						Logf("exit immediately due to no active client")
						h.shutdown(context.Background())
						return
					}

					// just try to send, if failed, just return
					select {
					case h.inputChan <- msg:
						Logf("exit will be handled after client received exit")
					default:
						Logf("exit immediately due to client buffer full")
						h.shutdown(context.Background())
					}
					return
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

func toBoolInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
