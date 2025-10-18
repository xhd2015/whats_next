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

type multiLineEditorModel struct {
	textarea    textarea.Model
	finished    bool
	cancelled   bool
	content     string
	hasInput    *int32
	startTime   time.Time
	timeout     time.Duration
	showTimer   bool
	frozenTime  time.Duration // Time remaining when user first typed
	timerFrozen bool          // Whether timer is frozen due to user input
}

type multiLineMsg struct {
	content string
	exit    bool
}

type timerTickMsg time.Time

func (m multiLineEditorModel) Init() tea.Cmd {
	if m.showTimer {
		return tea.Batch(textarea.Blink, timerTick())
	}
	return textarea.Blink
}

func timerTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return timerTickMsg(t)
	})
}

func (m multiLineEditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case timerTickMsg:
		if m.showTimer && atomic.LoadInt32(m.hasInput) == 0 {
			elapsed := time.Since(m.startTime)
			if elapsed >= m.timeout {
				// Timeout reached
				m.cancelled = true
				return m, tea.Quit
			}
			// Continue ticking
			return m, timerTick()
		}
		// Freeze timer if user has input
		if atomic.LoadInt32(m.hasInput) > 0 && !m.timerFrozen {
			elapsed := time.Since(m.startTime)
			m.frozenTime = m.timeout - elapsed
			m.timerFrozen = true
		}
		return m, nil
	case tea.KeyMsg:
		// Set hasInput when user types any content (except control keys that don't add content)
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlS, tea.KeyEsc:
			// Control keys - don't set hasInput
		default:
			// Any other key (including printable chars, enter, backspace) indicates user input
			if m.hasInput != nil {
				atomic.StoreInt32(m.hasInput, 1)
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyCtrlD:
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.cancelled = true
				return m, tea.Quit
			}
			fallthrough
		case tea.KeyCtrlS:
			// Submit with Ctrl+S or Ctrl+D (if content exists)
			content := m.textarea.Value()
			// Check for END command
			if strings.HasSuffix(strings.TrimSpace(content), "END") {
				content = strings.TrimSuffix(strings.TrimSpace(content), "END")
				content = strings.TrimSpace(content)
			}
			// Check for CLEAR command
			if strings.TrimSpace(content) == "CLEAR" {
				m.textarea.Reset()
				return m, nil
			}
			// Check for exit command
			if strings.TrimSpace(content) == "exit" {
				m.cancelled = true
				return m, tea.Quit
			}

			m.content = content
			m.finished = true
			return m, tea.Quit
		case tea.KeyEnter:
			content := m.textarea.Value()
			lines := strings.Split(content, "\n")
			if len(lines) > 0 {
				lastLine := strings.TrimSpace(lines[len(lines)-1])

				// Check for CLEAR command on last line
				if lastLine == "CLEAR" {
					m.textarea.Reset()
					return m, nil
				}

				// Check for exit command on last line
				if lastLine == "exit" {
					m.cancelled = true
					return m, tea.Quit
				}

				// Check if the current line ends with END for shortcut submission
				if strings.HasSuffix(lastLine, "END") {
					// Remove the END from the last line and submit
					if lastLine == "END" {
						// If the last line is just "END", remove the entire line
						if len(lines) > 1 {
							content = strings.Join(lines[:len(lines)-1], "\n")
						} else {
							content = ""
						}
					} else {
						// Remove "END" from the end of the last line
						newLastLine := strings.TrimSuffix(lastLine, "END")
						newLastLine = strings.TrimSpace(newLastLine)
						lines[len(lines)-1] = newLastLine
						content = strings.Join(lines, "\n")
					}
					content = strings.TrimSpace(content)

					m.content = content
					m.finished = true
					return m, tea.Quit
				}
			}
		case tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		}
	}

	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m multiLineEditorModel) View() string {
	var userPrompt string
	if m.showTimer {
		var remaining time.Duration
		if m.timerFrozen {
			// Show frozen time when user has typed
			remaining = m.frozenTime
		} else if atomic.LoadInt32(m.hasInput) == 0 {
			// Show live countdown when user hasn't typed
			elapsed := time.Since(m.startTime)
			remaining = m.timeout - elapsed
		} else {
			// Fallback case
			userPrompt = "user> "
		}

		if remaining > 0 {
			minutes := int(remaining.Minutes())
			seconds := int(remaining.Seconds()) % 60
			userPrompt = fmt.Sprintf("user (%dm %02ds)> ", minutes, seconds)
		} else {
			userPrompt = "user> "
		}
	} else {
		userPrompt = "user> "
	}

	helpText := "\n\nType 'END'(Ctrl+S) to submit • Type 'CLEAR'(Ctrl+D) to reset • Type 'exit'(esc) to quit"
	return fmt.Sprintf("%s\n%s%s", userPrompt, m.textarea.View(), helpText)
}

// readInputFromTerminal reads multiline input from terminal with rich editing capabilities.
// Requirements:
// - Support arrow keys for navigation (left, right, up, down)
// - Support delete/backspace for editing
// - Support multiline input without overlay (works in chat terminals)
// - Support special commands: END (submit), CLEAR (reset), exit (quit)
// - Must work inline in terminal, not as vim-like overlay

func readInputFromTerminal(ctx context.Context, hasInput *int32, timeout time.Duration, showTimer bool, initialContent string) ([]string, error) {
	ta := textarea.New()
	ta.Placeholder = "Type your message here... (multi-line supported)"
	ta.Focus()
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(4)
	ta.ShowLineNumbers = false

	// Set initial content if provided
	if initialContent != "" {
		ta.SetValue(initialContent)
	}

	model := multiLineEditorModel{
		textarea:  ta,
		hasInput:  hasInput,
		startTime: time.Now(),
		timeout:   timeout,
		showTimer: showTimer,
	}

	// Use WITHOUT AltScreen to work inline in terminal
	program := tea.NewProgram(model, tea.WithContext(ctx))
	finalModel, err := program.Run()
	if err != nil {
		// Check if it was cancelled due to timeout
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timeout")
		}
		return nil, err
	}

	m := finalModel.(multiLineEditorModel)
	if m.cancelled {
		return nil, fmt.Errorf("exit")
	}

	content := m.content
	if strings.TrimSpace(content) == "" {
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
