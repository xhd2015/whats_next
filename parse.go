package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// Section represents a markdown section with a title and content
type Section struct {
	Title   string
	Content string
}

// parseSections parses markdown content into a list of sections
// Each section starts with a heading (line starting with #) and contains
// all content until the next heading
func parseSections(content string) []Section {
	lines := strings.Split(content, "\n")
	var sections []Section
	var currentSection *Section
	var inCodeBlock bool

	for _, line := range lines {
		// Track code block state
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "```") {
			inCodeBlock = !inCodeBlock
		}

		// Check if this is a heading line (only if not in a code block)
		if !inCodeBlock && strings.HasPrefix(line, "#") {
			// If we have a current section, save it
			if currentSection != nil {
				sections = append(sections, *currentSection)
			}

			// Start new section
			currentSection = &Section{
				Title:   line,
				Content: "",
			}
		} else {
			// Add line to current section content
			if currentSection != nil {
				if currentSection.Content != "" {
					currentSection.Content += "\n"
				}
				currentSection.Content += line
			}
		}
	}

	// Add the last section if it exists
	if currentSection != nil {
		sections = append(sections, *currentSection)
	}

	return sections
}

// filterContentByProject filters markdown content to only show sections
// that match the current working directory when the section title contains
// a project path specification like "# Some title(project: /path/to/project)"
// (cursor-only)
func filterContentByProject(content string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	filteredContent := filterContentByDir(content, cwd, isCursor())
	return filteredContent, nil
}

func isCursor() bool {
	claudeCodeEnv := os.Getenv("CLAUDECODE")
	if claudeCodeEnv == "1" || claudeCodeEnv == "true" {
		return false
	}
	return true
}

func filterContentByDir(content string, dir string, isCursor bool) string {
	sections := parseSections(content)
	var filteredSections []Section

	for _, section := range sections {
		if shouldIncludeSection(section.Title, dir, isCursor) {
			filteredSections = append(filteredSections, section)
		}
	}

	// Reconstruct the content from filtered sections
	var result []string
	for _, section := range filteredSections {
		result = append(result, section.Title)
		if section.Content != "" {
			result = append(result, section.Content)
		}
	}

	return strings.Join(result, "\n")
}

// shouldIncludeSection checks if a section heading should be included
// based on project path matching and cursor-only directive
func shouldIncludeSection(heading, cwd string, isCursor bool) bool {
	// Check for (cursor-only) directive
	if hasCursorOnlyDirective(heading) && !isCursor {
		return false
	}
	// Look for pattern like "(project: /path/to/project)"
	projectStart := strings.Index(heading, "(project:")
	if projectStart == -1 {
		// No project specification, include the section
		return true
	}

	projectEnd := strings.Index(heading[projectStart:], ")")
	if projectEnd == -1 {
		// Malformed project specification, include the section
		return true
	}

	// Extract the project path
	projectSpec := heading[projectStart+len("(project:") : projectStart+projectEnd]
	projectPath := strings.TrimSpace(projectSpec)

	// Expand tilde to home directory
	if strings.HasPrefix(projectPath, "~/") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			projectPath = filepath.Join(homeDir, projectPath[2:])
		}
	}

	// Expand environment variables in the project path
	projectPath = os.ExpandEnv(projectPath)

	// Convert to absolute path - handle relative paths relative to cwd
	var absProjectPath string
	if filepath.IsAbs(projectPath) {
		absProjectPath = projectPath
	} else {
		absProjectPath = filepath.Join(cwd, projectPath)
	}
	absProjectPath = filepath.Clean(absProjectPath)

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		// If we can't resolve cwd, include the section
		return true
	}

	// Check if project path contains glob patterns
	if containsGlobPattern(projectPath) {
		// Use the gobwas/glob library for pattern matching
		g, err := glob.Compile(absProjectPath, filepath.Separator)
		if err != nil {
			// If pattern compilation fails, include the section
			return true
		}
		return g.Match(absCwd)
	}

	// Check if current working directory is the project directory or a subdirectory
	return strings.HasPrefix(absCwd, absProjectPath)
}

// containsGlobPattern checks if a path contains glob pattern characters
func containsGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[]{}")
}

// hasCursorOnlyDirective checks if a heading contains the (cursor-only) directive
// Handles arbitrary whitespace and multiple directives
func hasCursorOnlyDirective(heading string) bool {
	// Look for pattern like "(cursor-only)" with potential whitespace
	start := 0
	for {
		parenStart := strings.Index(heading[start:], "(")
		if parenStart == -1 {
			break
		}
		parenStart += start
		
		parenEnd := strings.Index(heading[parenStart:], ")")
		if parenEnd == -1 {
			break
		}
		parenEnd += parenStart
		
		// Extract content inside parentheses
		content := heading[parenStart+1 : parenEnd]
		// Trim whitespace and check if it contains "cursor-only"
		trimmedContent := strings.TrimSpace(content)
		if strings.Contains(trimmedContent, "cursor-only") {
			return true
		}
		
		start = parenEnd + 1
	}
	return false
}
