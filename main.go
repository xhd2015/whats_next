package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xhd2015/less-gen/flags"
	"github.com/xhd2015/xgo/support/cmd"
	"golang.org/x/term"
)

const generalGuideline = `
# Follow-up with ` + "`whats_next`" + `

You don't need to output any summary after you finished a task.

Every time you finished a task given by the user, you must always run ` + "`whats_next`" + ` in the terminal verbatim(don't use +` + "`echo`" + `). User will type his follow-up in the terminal, and you MUST repeat that follow-up yourself, and then proceed.

You will only end the conversation when user type 'exit'. Never ask if the user would like to proceed, just do it.

Before calling ` + "`whats_next`" + `, you must show the number of tool calls you've used so far.
`

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

const runningCommand = `
# Command line
When running command line like ` + "`" + "cd some_path && do somthing...`" + `, always wrap in sub shell adding enclosing ` + "`(...)`" + `, e.g. ` + "`(cd some_path && do somthing...)`" + `
`

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

const help = `
whats_next [command]

Commands:
  show
  edit
  add
  group

Sub commands for group:
  list
  show
  edit
  use
  rm, remove
  mv, rename
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
		switch cmd {
		case "show":
			return show(args[1:])
		case "edit":
			return edit(args[1:])
		case "add":
			return add(args[1:])
		case "group":
			return group(args[1:])
		default:
			return fmt.Errorf("unrecognized command: %s", cmd)
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
					fmt.Println(string(profileContent))
				}
			}
		}
		if !printSelectedProfile {
			fmt.Print(generalGuideline)
			fmt.Print(toolCallAwareness)
			fmt.Print(runningCommand)
		}

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
	return showW(os.Stdout)
}

func showW(w io.Writer) error {
	fmt.Fprintln(w, strings.TrimPrefix(generalGuideline, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(toolCallAwareness, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(runningCommand, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(noTest, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(dontIgnoreLint, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(serverImplementation, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(ignoreLint, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(verify, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(pattern, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(recover, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(goCompileInstruction, "\n"))

	fmt.Fprintln(w, strings.TrimPrefix(dumpPrompt, "\n"))

	customFile, err := getCustomFile(false)
	if err != nil {
		return err
	}
	custom, readErr := os.ReadFile(customFile)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return readErr
		}
	}
	if len(custom) > 0 {
		fmt.Fprintf(w, "---- from: %s ----\n", customFile)
		fmt.Fprintln(w, string(custom))
	}

	return nil
}

func edit(args []string) error {
	file, err := getCustomFile(true)
	if err != nil {
		return err
	}
	return cmd.Debug().Run("code", file)
}

func group(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("requries cmd: list, edit, use")
	}
	groupCmd := args[0]
	args = args[1:]

	if groupCmd == "use" {
		return groupShow(true, args)
	}
	if groupCmd == "show" {
		var use bool
		args, err := flags.Bool("--use", &use).Parse(args)
		if err != nil {
			return err
		}
		return groupShow(use, args)
	}

	switch groupCmd {
	case "list":
		groupDir, err := getConfigPath(true, "group")
		if err != nil {
			return err
		}
		names, err := getGroupNames(groupDir)
		if err != nil {
			return err
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	case "edit":
		groupDir, err := getConfigPath(true, "group")
		if err != nil {
			return err
		}
		name, err := selectGroupName(groupDir, args)
		if err != nil {
			return err
		}
		name = addMDSuffix(name)
		groupFile := filepath.Join(groupDir, name)

		stat, statErr := os.Stat(groupFile)
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				return statErr
			}
			if err := os.MkdirAll(groupDir, 0755); err != nil {
				return err
			}
			// write new content
			var b strings.Builder
			if err := showW(&b); err != nil {
				return err
			}
			if err := os.WriteFile(groupFile, []byte(b.String()), 0644); err != nil {
				return err
			}
		}
		if stat != nil && stat.IsDir() {
			return fmt.Errorf("group config is a dir, not a file: %s", groupFile)
		}
		return cmd.Debug().Run("code", groupFile)
	case "rename", "mv":
		if len(args) != 2 {
			return fmt.Errorf("requires old name and new name")
		}
		oldName, newName := args[0], args[1]
		groupDir, err := getConfigPath(false, "group")
		if err != nil {
			return err
		}
		oldName = addMDSuffix(oldName)
		newName = addMDSuffix(newName)

		oldFile := filepath.Join(groupDir, oldName)
		_, statErr := os.Stat(oldFile)
		if statErr != nil {
			return statErr
		}
		newFile := filepath.Join(groupDir, newName)
		if _, statErr := os.Stat(newFile); statErr == nil {
			return fmt.Errorf("new name already exists: %s", newFile)
		}
		if err := os.Rename(oldFile, newFile); err != nil {
			return err
		}
		return nil
	case "rm", "remove":
		if len(args) != 1 {
			return fmt.Errorf("requires name")
		}
		name := args[0]
		groupDir, err := getConfigPath(false, "group")
		if err != nil {
			return err
		}
		name = addMDSuffix(name)
		groupFile := filepath.Join(groupDir, name)
		if err := os.Remove(groupFile); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unrecognized %s", groupCmd)
	}
}

func groupShow(use bool, args []string) error {
	groupDir, err := getConfigPath(false, "group")
	if err != nil {
		return err
	}
	name, err := selectGroupName(groupDir, args)
	if err != nil {
		return err
	}
	name = addMDSuffix(name)

	groupFile := filepath.Join(groupDir, name)
	group, readErr := os.ReadFile(groupFile)
	if readErr != nil {
		return readErr
	}
	fmt.Print(string(group))

	if use {
		// Save selected profile to config
		config, err := readConfig()
		if err != nil {
			return err
		}
		config.SelectedProfile = strings.TrimSuffix(name, ".md")
		if err := writeConfig(config); err != nil {
			return err
		}

		return nil
	}
	return nil
}
func addMDSuffix(name string) string {
	if strings.HasSuffix(name, ".md") {
		return name
	}
	return name + ".md"
}

func selectGroupName(groupDir string, args []string) (string, error) {
	var name string
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
		if len(args) > 0 {
			return "", fmt.Errorf("unrecognized extra args: %s", strings.Join(args, ","))
		}
		return name, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("requires name")
	}

	names, err := getGroupNames(groupDir)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("nothing to show or edit, requires name")
	}
	// let user select
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stdout, "groups: \n")
		n := len(names)
		for i := 0; i < n; i++ {
			fmt.Printf(" %d. %s\n", i+1, names[i])
		}
		fmt.Fprintf(os.Stdout, "select> ")
		index, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		index = strings.TrimSpace(index)
		if index == "" {
			continue
		}
		indexInt, err := strconv.Atoi(index)
		if err != nil {
			continue
		}
		if indexInt < 1 || indexInt > n {
			continue
		}
		name = names[indexInt-1]
		break
	}
	return name, nil
}

func getGroupNames(groupDir string) ([]string, error) {
	entries, readErr := os.ReadDir(groupDir)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return nil, readErr
		}
		return nil, nil
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		result = append(result, strings.TrimSuffix(entry.Name(), ".md"))
	}
	return result, nil
}

const addHelp = `
whats_next add [content]

Options:
  --title TITLE

`

func add(args []string) error {
	var title string
	args, readErr := flags.String("--title", &title).
		Help("-h,--help", addHelp).
		Parse(args)
	if readErr != nil {
		return readErr
	}
	if len(args) == 0 {
		return fmt.Errorf("requires content")
	}
	content := args[0]
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("requires non-empty content")
	}
	args = args[1:]

	if len(args) > 0 {
		return fmt.Errorf("unrecognized extra arguments: %v", strings.Join(args, ","))
	}

	customFile, readErr := getCustomFile(true)
	if readErr != nil {
		return readErr
	}

	custom, readErr := os.ReadFile(customFile)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return readErr
		}
	}

	if title != "" {
		if !strings.HasPrefix(title, "# ") {
			title = "# " + title
		}
		custom = append(custom, []byte(title)...)
		custom = append(custom, []byte("\n")...)
	}

	custom = append(custom, []byte(content)...)
	custom = append(custom, []byte("\n")...)

	if err := os.WriteFile(customFile, custom, 0644); err != nil {
		return err
	}

	return nil
}

func getConfigDir(createDir bool) (string, error) {
	conf, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	configDir := filepath.Join(conf, "whats_next")
	if createDir {
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return "", err
		}
	}
	return configDir, nil
}

func getConfigPath(createDir bool, name string) (string, error) {
	configDir, err := getConfigDir(createDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, name), nil
}

func getGroupConfigPath(createDir bool) (string, error) {
	return getConfigPath(createDir, "group")
}

func getCustomFile(createDir bool) (string, error) {
	return getConfigPath(createDir, "custom.md")
}
