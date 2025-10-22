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
	"sync/atomic"
	"time"

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
		return handleWhatsNextInServerMode(args)
	}
	wd, _ := os.Getwd()
	return acceptInput(os.Stdout, wd)
}

func acceptInput(w io.Writer, workingDir string) error {
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
			lines, err = readInputFromTerminal(ctx, &hasInput, TIMEOUT, !DISABLE_TIMER)
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
	resp, err := http.Get("http://localhost:7654/?workingDir=" + url.QueryEscape(wd))
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

func handleServe(args []string) error {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// When client makes a request, show the input prompt on server side (just like current terminal behavior)
		fmt.Println("Client connected")
		workingDir := r.URL.Query().Get("workingDir")

		w.Header().Set("Content-Type", "text/plain")
		err := acceptInput(w, workingDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			fmt.Println("Error:", err)
		} else {
			fmt.Println("Client finished")
		}
	})

	fmt.Println("Starting server on port 7654...")
	fmt.Println("Server will show input prompt when clients connect...")
	return http.ListenAndServe(":7654", nil)
}
