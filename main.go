package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const optionalToolcall = `Before calling ` + "`whats_next`, you must show the number of tool calls you've used so far."

const generalGuideline = `
# General Guidelines

You don't need to output any summary after you finished a task.

Every time you finished a task given by the user, you must always run ` + "`whats_next` in the terminal verbatim(don't use `echo`)" + `. User will type his question in the terminal, and you MUST repeat that question yourself, and then reply to user with proper answer.

You will only end the conversation when user type 'exit'. Never ask if the user would like to proceed, just do it.
` + optionalToolcall + "\n"

// the output is something like:
//
//	Tool calls used: 18/25
const toolCallAwarenessv1 = `
# Tool Call Awareness

you *MUST* always show how many individual tool calls you've used, before making any tool call, since the session begins. Before first tool call you should show 0.
`
const toolCallAwareness = `
# CRITICAL: ALWAYS show tool call count before EVERY tool call (always_applied_workspace_rules)

you *MUST* always show how many individual tool calls you've used, before making any tool call, since the session begins. Before first tool call you should show 0.
`

// const toolCallAwareness = ""

const recover = `
# Recover from last edit

Previously I asked you to do the following work, but was interrupted. Let's resume the work. You need to first find what was done, then figure out the remaining works, and finish them.

<previous_prompt>

</previous_prompt>
`

const noTest = `
# No build or test
You don't need to add or run any build or test command
`
const ignoreLint = `
# Ignore lint errors for now
You can ignore lint error for now, I'll fix them later.
`

const dontIgnoreLint = `
# Don't ignore lint errors
You should not ignore lint errors for now, you should fix them.
`

const verify = `
# Verify the build
You can verify swift building with ` + "`go run ./script build-swift`" + `, You don't need to run any ` + "`go test`" + `.
`

const pattern = `
# Follow existing patterns
When changing code, you must follow existing patterns.
`

const serverImplementation = `
# Implement in server_go
You also need to implement this in server_go:
- server_go/src/route/router.go line xxx
- server_go/src/handler/<xxx>
- server_go/src/repo/daov2/<xxx>
- api bridge: src/api/<xxx>.ts or src/api/<xxx>/api.ts

Following patterns in server_go/doc/PATTERN.md
`

const goCompileInstruction = `
# Use correct go version
if you encounter error like: ` + "`" + `compile: version "go1.23.6" does not match go tool version "go1.24.0"` + "`" + `, you can use ` + "`kool with-go1.24 go <reminder...>`" + ` to run go with the correct go version.

`

const dumpPrompt = `
# Dump the prompt to md
dump the prompt in markdown format, you need to replace three-backquote with CODE_BEGIN and CODE_END so the output is not destroyed by browser. The whole response should be wrapped in 
` + "```md" + `
.... 
` + "```" + `
`

func main() {
	err := handleWhatsNext(os.Args[1:])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// TODO: let's propose a dialog when this is completed
//
//   osascript -e 'display dialog "Task completed!" buttons {"OK"} default button "OK" with title "Task"'

// tested: 2m works fine,
// tested: 3m works fine
// let's try 3m
var TIMEOUT = 3 * time.Minute

var USE_BACKSLAHS = false

func handleWhatsNext(args []string) error {
	if len(args) > 0 {
		cmd := args[0]
		if cmd == "show" {
			return show(args[1:])
		}
	}
	// wait for user input

	type Result struct {
		Error error
	}
	done := make(chan Result)

	var hasInput int32

	go func() {
		reader := bufio.NewReader(os.Stdin)
		var lines []string

		var isContinue bool
		for {
			if !isContinue {
				fmt.Fprintf(os.Stdout, "user> ")
			} else {
				fmt.Fprintf(os.Stdout, "...")
			}
			// TODO: detect is any character is on the way
			input, err := reader.ReadString('\n')
			if err != nil {
				done <- Result{Error: err}
				return
			}
			atomic.StoreInt32(&hasInput, 1)
			isContinue = false
			in := strings.TrimSpace(input)
			if in == "" {
				continue
			}
			if in == "exit" && len(lines) == 0 {
				fmt.Println("exit")
				done <- Result{}
				return
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
		q := strings.Join(lines, "\n")
		fmt.Printf("the user is asking: \n<question>\n%s\n</question>\nplease think step by step and give your answer\n", q)

		fmt.Println("----")
		fmt.Print(generalGuideline)
		fmt.Print(toolCallAwareness)
		done <- Result{}
	}()

	select {
	case <-time.After(TIMEOUT):
		if atomic.LoadInt32(&hasInput) == 0 {
			fmt.Println("The user is thinking, please execute `whats_next` again.")
			fmt.Println()
			fmt.Print(generalGuideline)
			return nil
		}
		// wait forever for done
		<-done
	case result := <-done:
		if result.Error != nil {
			return result.Error
		}
		return nil
	}

	return nil
}

func show(args []string) error {
	fmt.Println(strings.TrimPrefix(generalGuideline, "\n"))

	fmt.Println(strings.TrimPrefix(toolCallAwareness, "\n"))

	fmt.Println(strings.TrimPrefix(noTest, "\n"))

	fmt.Println(strings.TrimPrefix(dontIgnoreLint, "\n"))

	fmt.Println(strings.TrimPrefix(serverImplementation, "\n"))

	fmt.Println(strings.TrimPrefix(ignoreLint, "\n"))

	fmt.Println(strings.TrimPrefix(verify, "\n"))

	fmt.Println(strings.TrimPrefix(pattern, "\n"))

	fmt.Println(strings.TrimPrefix(recover, "\n"))

	fmt.Println(strings.TrimPrefix(goCompileInstruction, "\n"))

	fmt.Println(strings.TrimPrefix(dumpPrompt, "\n"))

	return nil
}
