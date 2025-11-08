package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// readInputFromTerminal reads multiline input from terminal with rich editing capabilities.
// Requirements:
// - Support arrow keys for navigation (left, right, up, down)
// - Support delete/backspace for editing
// - Support multiline input without overlay (works in chat terminals)
// - Support special commands: END (submit), CLEAR (reset), exit (quit)
// - Must work inline in terminal, not as vim-like overlay

type readTerminalOptions struct {
	showTimer     func() bool
	getUserPrompt func(hasInput bool) string

	noWrapWithGuidelines bool

	onCreatedProgram  func(program *tea.Program)
	onProgramFinished func(program *tea.Program)
	onInputExit       func()
	onInputUpdate     func(hasInput bool)
}

func readInputFromTerminal(ctx context.Context, hasInput *int32, timeout time.Duration, onInputUpdate func(hasInput bool), opts readTerminalOptions) ([]string, error) {
	showTimer := opts.showTimer
	userPrompt := opts.getUserPrompt
	onCreatedProgram := opts.onCreatedProgram
	onProgramFinished := opts.onProgramFinished
	onInputExit := opts.onInputExit

	ta := textarea.New()
	ta.Placeholder = "Type your message here... (multi-line supported)"
	ta.Focus()
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(4)
	ta.ShowLineNumbers = false

	model := multiLineEditorModel{
		textarea:         ta,
		hasInput:         hasInput,
		timeoutBeginTime: time.Now(),
		timeout:          timeout,
		showTimer:        showTimer,
		getUserPrompt:    userPrompt,
		onInputExit:      onInputExit,
		onInputUpdate:    onInputUpdate,
	}

	// Use WITHOUT AltScreen to work inline in terminal
	program := tea.NewProgram(model, tea.WithContext(ctx))
	if onCreatedProgram != nil {
		onCreatedProgram(program)
	}
	finalModel, err := program.Run()
	if onProgramFinished != nil {
		// clear
		onProgramFinished(nil)
	}
	Logf("readInputFromTerminal program returned: err: %v", err)
	if err != nil {
		Logf("readInputFromTerminal error: %v", err)
		// Check if it was cancelled due to timeout
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timeout")
		}
		return nil, err
	}

	m := finalModel.(multiLineEditorModel)
	if m.cancelled {
		Logf("readInputFromTerminal cancelled")
		return nil, fmt.Errorf("exit")
	}

	content := m.content
	if strings.TrimSpace(content) == "" {
		Logf("readInputFromTerminal empty content")
		return []string{}, nil
	}

	// Split content into logical lines for the existing logic
	lines := strings.Split(content, "\n")
	var result []string
	var currentBuffer strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && currentBuffer.Len() > 0 {
			// Empty line ends current buffer
			result = append(result, currentBuffer.String())
			currentBuffer.Reset()
		} else if trimmed != "" {
			if currentBuffer.Len() > 0 {
				currentBuffer.WriteString("\n")
			}
			currentBuffer.WriteString(line)
		}
	}

	// Add any remaining content
	if currentBuffer.Len() > 0 {
		result = append(result, currentBuffer.String())
	}

	if len(result) == 0 && content != "" {
		result = []string{content}
	}
	Logf("readInputFromTerminal result: %v", result)

	return result, nil
}

func readInputFromNonTerminal(hasInput *int32) ([]string, error) {
	var lines []string

	// Fallback to basic bufio.Reader for non-terminal input
	reader := bufio.NewReader(os.Stdin)
	var isContinue bool
	for {
		if !isContinue {
			fmt.Fprintf(os.Stdout, "user> ")
		} else {
			fmt.Fprintf(os.Stdout, "...")
		}
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		atomic.StoreInt32(hasInput, 1)
		isContinue = false
		in := strings.TrimSpace(input)
		if in == "" {
			continue
		}
		if in == "exit" && len(lines) == 0 {
			return nil, fmt.Errorf("exit")
		}
		if !USE_BACKSLAHS {
			// must see an end
			if prefix, ok := strings.CutSuffix(in, "END"); ok {
				if prefix != "" {
					lines = append(lines, prefix)
				}
				break
			}
			if in == "CLEAR" {
				lines = nil
			} else {
				lines = append(lines, in)
			}
			isContinue = true
		} else {
			var hasNextLine bool
			inContent := in
			if strings.HasSuffix(in, "\\") {
				inContent = in[:len(in)-1]
				hasNextLine = true
			}
			if inContent == "" {
				continue
			}
			lines = append(lines, inContent)
			if !hasNextLine {
				break
			}
			isContinue = true
		}
	}
	return lines, nil
}
