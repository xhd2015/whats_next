package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// Section represents a markdown section with a title and content
type Section struct {
	Title   string
	Content string
}

// MatchReason represents why a section was included
type MatchReason int

const (
	MatchReasonNone MatchReason = iota
	MatchReasonNoProject
	MatchReasonPathMatch
	MatchReasonGlobMatch
	MatchReasonGitWorktree
)

// SectionMatch represents a section that matches with its specificity information
type SectionMatch struct {
	Section     Section
	MatchReason MatchReason
	ProjectPath string // The resolved absolute project path
	Specificity int    // Higher number means more specific (deeper path)
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
	var matches []SectionMatch

	// Collect all matching sections with their specificity information
	for _, section := range sections {
		include, matchReason, projectPath, specificity := shouldIncludeSection(section.Title, dir, isCursor)
		if include {
			matches = append(matches, SectionMatch{
				Section:     section,
				MatchReason: matchReason,
				ProjectPath: projectPath,
				Specificity: specificity,
			})
		}
	}

	// Group matches by project path and find the most specific ones
	filteredMatches := selectMostSpecificMatches(matches)

	// Convert back to sections and apply project path replacement
	var filteredSections []Section
	for _, match := range filteredMatches {
		section := match.Section
		// Replace project path if matched via git worktree
		if match.MatchReason == MatchReasonGitWorktree {
			section.Title = replaceProjectPath(section.Title, dir)
		}
		filteredSections = append(filteredSections, section)
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

// selectMostSpecificMatches filters matches to only include those from the most specific project paths
// while preserving the original order of sections
func selectMostSpecificMatches(matches []SectionMatch) []SectionMatch {
	if len(matches) == 0 {
		return matches
	}

	// Separate exact path matches from glob matches
	var exactMatches []SectionMatch
	var globMatches []SectionMatch
	var noProjectMatches []SectionMatch

	for _, match := range matches {
		if match.MatchReason == MatchReasonNoProject {
			noProjectMatches = append(noProjectMatches, match)
		} else if match.MatchReason == MatchReasonGlobMatch {
			globMatches = append(globMatches, match)
		} else {
			exactMatches = append(exactMatches, match)
		}
	}

	var result []SectionMatch

	// Always include sections without project specifications
	result = append(result, noProjectMatches...)

	// For exact path matches, find the most specific ones
	if len(exactMatches) > 0 {
		maxExactSpecificity := 0
		for _, match := range exactMatches {
			if match.Specificity > maxExactSpecificity {
				maxExactSpecificity = match.Specificity
			}
		}

		// Include only exact matches with maximum specificity
		for _, match := range exactMatches {
			if match.Specificity == maxExactSpecificity {
				result = append(result, match)
			}
		}
	}

	// Always include all glob matches (they serve different purposes)
	result = append(result, globMatches...)

	// Sort result to preserve original order by iterating through original matches
	var orderedResult []SectionMatch
	for _, originalMatch := range matches {
		for _, resultMatch := range result {
			// Compare by title and content since these are the unique identifiers
			if originalMatch.Section.Title == resultMatch.Section.Title &&
				originalMatch.Section.Content == resultMatch.Section.Content {
				orderedResult = append(orderedResult, resultMatch)
				break
			}
		}
	}

	return orderedResult
}

// replaceProjectPath replaces the project path specification in a heading with the actual current directory
func replaceProjectPath(heading, actualDir string) string {
	// Look for pattern like "(project: /path/to/project)"
	projectStart := strings.Index(heading, "(project:")
	if projectStart == -1 {
		return heading
	}

	projectEnd := strings.Index(heading[projectStart:], ")")
	if projectEnd == -1 {
		return heading
	}
	projectEnd += projectStart

	// Replace the project specification with the actual directory
	before := heading[:projectStart]
	after := heading[projectEnd+1:]
	newProjectSpec := "(project: " + actualDir + ")"

	return before + newProjectSpec + after
}

// shouldIncludeSection checks if a section heading should be included
// based on project path matching and cursor-only directive
// Returns whether to include, the reason for matching, project path, and specificity
func shouldIncludeSection(heading, cwd string, isCursor bool) (bool, MatchReason, string, int) {
	// Check for (cursor-only) directive
	if hasCursorOnlyDirective(heading) && !isCursor {
		return false, MatchReasonNone, "", 0
	}
	// Look for pattern like "(project: /path/to/project)"
	projectStart := strings.Index(heading, "(project:")
	if projectStart == -1 {
		// No project specification, include the section
		return true, MatchReasonNoProject, "", 0
	}

	projectEnd := strings.Index(heading[projectStart:], ")")
	if projectEnd == -1 {
		// Malformed project specification, include the section
		return true, MatchReasonNoProject, "", 0
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
		return true, MatchReasonNoProject, "", 0
	}

	// Calculate specificity based on path depth (more path segments = more specific)
	// For glob patterns, use a different calculation to avoid conflicts
	var specificity int
	if containsGlobPattern(projectPath) {
		// For glob patterns, count non-glob segments to determine specificity
		// This allows different glob patterns to coexist
		segments := strings.Split(strings.Trim(absProjectPath, string(filepath.Separator)), string(filepath.Separator))
		nonGlobSegments := 0
		for _, segment := range segments {
			if !containsGlobPattern(segment) {
				nonGlobSegments++
			}
		}
		// Use a base specificity for globs plus non-glob segments
		// This ensures glob patterns don't compete with exact path matches
		specificity = 1000 + nonGlobSegments
	} else {
		// For exact paths, use path depth
		specificity = len(strings.Split(strings.Trim(absProjectPath, string(filepath.Separator)), string(filepath.Separator)))
	}

	// Check if project path contains glob patterns
	if containsGlobPattern(projectPath) {
		// Use the gobwas/glob library for pattern matching
		g, err := glob.Compile(absProjectPath, filepath.Separator)
		if err != nil {
			// If pattern compilation fails, include the section
			return true, MatchReasonNoProject, "", 0
		}
		if g.Match(absCwd) {
			return true, MatchReasonGlobMatch, absProjectPath, specificity
		}
		return false, MatchReasonNone, "", 0
	}

	// Check if current working directory is the project directory or a subdirectory
	if strings.HasPrefix(absCwd, absProjectPath) {
		return true, MatchReasonPathMatch, absProjectPath, specificity
	}

	// Check if current directory is a git worktree of the specified project
	if isGitWorktree(absCwd, absProjectPath) {
		return true, MatchReasonGitWorktree, absProjectPath, specificity
	}

	return false, MatchReasonNone, "", 0
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

// getGitRemoteOriginURL returns the origin remote URL for a git repository
func getGitRemoteOriginURL(dir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// isGitWorktree checks if the current directory is a git worktree of the specified project
func isGitWorktree(currentDir, projectDir string) bool {
	// First, try using git worktree command to check direct relationship
	if isWorktreeRelated(currentDir, projectDir) {
		return true
	}

	// Fallback to remote origin URL comparison
	return hasSameGitOrigin(currentDir, projectDir)
}

// isWorktreeRelated checks if two directories are related through git worktree
func isWorktreeRelated(currentDir, projectDir string) bool {
	// Check if currentDir is a worktree of projectDir
	if isWorktreeOf(currentDir, projectDir) {
		return true
	}

	// Check if projectDir is a worktree of currentDir
	if isWorktreeOf(projectDir, currentDir) {
		return true
	}

	// Check if both are worktrees of the same main repository
	return haveSameMainWorktree(currentDir, projectDir)
}

// isWorktreeOf checks if targetDir is a worktree of mainDir
func isWorktreeOf(targetDir, mainDir string) bool {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = mainDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	targetAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return false
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			worktreePath := strings.TrimPrefix(line, "worktree ")
			worktreeAbs, err := filepath.Abs(worktreePath)
			if err != nil {
				continue
			}
			if worktreeAbs == targetAbs {
				return true
			}
		}
	}

	return false
}

// haveSameMainWorktree checks if two directories belong to the same git repository
func haveSameMainWorktree(dir1, dir2 string) bool {
	main1 := getMainWorktreePath(dir1)
	main2 := getMainWorktreePath(dir2)

	if main1 == "" || main2 == "" {
		return false
	}

	abs1, err1 := filepath.Abs(main1)
	abs2, err2 := filepath.Abs(main2)

	return err1 == nil && err2 == nil && abs1 == abs2
}

// getMainWorktreePath returns the path to the main worktree for a given directory
func getMainWorktreePath(dir string) string {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			// The first worktree entry is the main worktree
			return strings.TrimPrefix(line, "worktree ")
		}
	}

	return ""
}

// hasSameGitOrigin checks if two directories have the same git remote origin
func hasSameGitOrigin(currentDir, projectDir string) bool {
	currentOrigin, err := getGitRemoteOriginURL(currentDir)
	if err != nil {
		return false
	}

	projectOrigin, err := getGitRemoteOriginURL(projectDir)
	if err != nil {
		return false
	}

	// Normalize URLs for comparison (handle different formats like SSH vs HTTPS)
	return normalizeGitURL(currentOrigin) == normalizeGitURL(projectOrigin)
}

// normalizeGitURL normalizes git URLs for comparison
// Converts SSH format to HTTPS-like format for consistent comparison
func normalizeGitURL(url string) string {
	url = strings.TrimSpace(url)

	// Convert SSH format (git@github.com:user/repo.git) to normalized format
	if strings.HasPrefix(url, "git@") {
		// Extract host and path
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			host := strings.TrimPrefix(parts[0], "git@")
			path := parts[1]
			url = "https://" + host + "/" + path
		}
	}

	// Remove .git suffix
	url = strings.TrimSuffix(url, ".git")

	// Remove trailing slash
	url = strings.TrimSuffix(url, "/")

	return strings.ToLower(url)
}
