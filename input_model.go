package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type multiLineEditorModel struct {
	textarea         textarea.Model
	finished         bool
	cancelled        bool
	content          string
	hasInput         *int32
	timeoutBeginTime time.Time
	timeout          time.Duration
	// showTimer        bool
	frozenTime  time.Duration // Time remaining when user first typed
	timerFrozen bool          // Whether timer is frozen due to user input

	getUserPrompt func(hasInput bool) string

	showTimer func() bool

	onInputExit   func()
	onInputUpdate func(hasInput bool)
}

type timerTickMsg time.Time

type enableTimerMsg struct{}
type disableTimerMsg struct{}

func (m multiLineEditorModel) Init() tea.Cmd {
	if /* m.showTimer != nil && m.showTimer()  */ true {
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
	inputLength := m.textarea.Length()
	// Logf("input model update: %T ", msg)
	var cmd tea.Cmd

	if m.hasInput != nil {
		var hasValue int32
		if inputLength > 0 {
			hasValue = 1
		}
		atomic.StoreInt32(m.hasInput, hasValue)
		if m.onInputUpdate != nil {
			m.onInputUpdate(hasValue > 0)
		}
	}

	// showTimer := m.needShowTimer()

	var needProcessTick bool
	switch msg.(type) {
	case enableTimerMsg:
		m.timeoutBeginTime = time.Now()
		needProcessTick = true
		Logf("enable timer")
	case disableTimerMsg:
	case timerTickMsg:
		needProcessTick = true
	case tea.QuitMsg:
		Logf("quit")
		return m, tea.Quit
	}

	if needProcessTick {
		// if showTimer && atomic.LoadInt32(m.hasInput) == 0 {
		// 	elapsed := time.Since(m.timeoutBeginTime)
		// 	if elapsed >= m.timeout {
		// 		// Timeout reached
		// 		m.cancelled = true
		// 		return m, tea.Quit
		// 	}
		// 	// Continue ticking
		// 	return m, timerTick()
		// }
		// Freeze timer if user has input
		if atomic.LoadInt32(m.hasInput) > 0 && !m.timerFrozen {
			elapsed := time.Since(m.timeoutBeginTime)
			m.frozenTime = m.timeout - elapsed
			m.timerFrozen = true
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Set hasInput when user types any content (except control keys that don't add content)
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlS, tea.KeyEsc:
			// Control keys - don't set hasInput
		default:
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
				// this is an active exit, not a cancelled exit
				return m, nil
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
					if m.onInputExit != nil {
						m.onInputExit()
					}
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

	if m.getUserPrompt != nil {
		userPrompt = m.getUserPrompt(m.textarea.Length() > 0)
	} else {
		userPrompt = "user> "
	}

	helpText := "\n\nType 'END'(Ctrl+S) to submit • Type 'CLEAR'(Ctrl+D) to reset • Type 'exit'(esc) to quit"
	return fmt.Sprintf("%s\n%s%s", userPrompt, m.textarea.View(), helpText)
}

func renderUserPrompt(showTimer bool, showClient bool, remaining time.Duration, waitingClient int) string {
	var timer string
	if showTimer {
		if remaining > 0 {
			minutes := int(remaining.Minutes())
			seconds := int(remaining.Seconds()) % 60

			timer = fmt.Sprintf(" (%dm %02ds)", minutes, seconds)
		} else {
			timer = " (0m0s)"
		}
	}

	var client string
	if showClient {
		if waitingClient == 0 {
			client = " (staging)"
		} else if waitingClient == 1 {
			client = " (client connected)"
		} else {
			client = fmt.Sprintf(" (%d clients connected)", waitingClient)
		}
	}

	return "user" + timer + ">" + client
}
