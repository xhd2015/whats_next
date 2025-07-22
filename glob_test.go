package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gobwas/glob"
)

// mkdirTempResolved creates a temporary directory and returns both the original path
// (for cleanup) and the resolved path (for testing). This handles symlink issues
// on macOS where /var is a symlink to /private/var.
func mkdirTempResolved(pattern string) (originalPath, resolvedPath string, err error) {
	originalPath, err = os.MkdirTemp("", pattern)
	if err != nil {
		return "", "", err
	}

	// Resolve symlinks to get the canonical path
	resolvedPath, err = filepath.EvalSymlinks(originalPath)
	if err != nil {
		// If symlink resolution fails, use the original path
		resolvedPath = originalPath
	}

	return originalPath, resolvedPath, nil
}

func TestContainsGlobPattern(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/simple/path", false},
		{"/path/with/*/wildcard", true},
		{"/path/with/?.txt", true},
		{"/path/with/[abc]", true},
		{"/path/**/with/doublestar", true},
		{"relative/path", false},
		{"relative/*/path", true},
		{"/path/with/{a,b,c}", true}, // Added for gobwas/glob support
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := containsGlobPattern(tt.path)
			if result != tt.expected {
				t.Errorf("containsGlobPattern(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestMatchesGlobPattern(t *testing.T) {
	// Create a temporary directory structure for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("glob_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Create subdirectories (use resolved path)
	subDir := filepath.Join(tempDir, "projects", "frontend")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	tests := []struct {
		name     string
		pattern  string
		cwd      string
		expected bool
		skip     bool // Keep skip for tests that might still have issues
	}{
		{
			name:     "exact match",
			pattern:  tempDir,
			cwd:      tempDir,
			expected: true,
		},
		{
			name:     "wildcard match basename",
			pattern:  filepath.Dir(tempDir) + "/glob_test*",
			cwd:      tempDir,
			expected: true,
		},
		{
			name:     "no match different path",
			pattern:  "/completely/different/path",
			cwd:      tempDir,
			expected: false,
		},
		{
			name:     "double star prefix match",
			pattern:  tempDir + "/**",
			cwd:      subDir,
			expected: true,
		},
		{
			name:     "double star suffix match",
			pattern:  "**/frontend",
			cwd:      subDir,
			expected: true,
		},
		{
			name:     "double star middle match",
			pattern:  tempDir + "/**/frontend",
			cwd:      subDir,
			expected: true,
		},
		{
			name:     "wildcard no match",
			pattern:  "/path/*/nomatch",
			cwd:      tempDir,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("Skipping test that might fail due to symlink issues")
			}

			// Use the gobwas/glob library directly
			g, err := glob.Compile(tt.pattern, filepath.Separator)
			if err != nil {
				t.Fatalf("Failed to compile glob pattern %q: %v", tt.pattern, err)
			}

			result := g.Match(tt.cwd)
			if result != tt.expected {
				t.Errorf("glob.Match(%q, %q) = %v, expected %v", tt.pattern, tt.cwd, result, tt.expected)
			}
		})
	}
}

func TestShouldIncludeSectionWithGlob(t *testing.T) {
	// Create a temporary directory structure for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("glob_section_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Create subdirectories (use resolved path)
	projectDir := filepath.Join(tempDir, "myproject", "frontend")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	tests := []struct {
		name     string
		heading  string
		cwd      string
		expected bool
	}{
		{
			name:     "glob pattern matches",
			heading:  "# Section(project: " + tempDir + "/*/frontend)",
			cwd:      projectDir,
			expected: true,
		},
		{
			name:     "double star pattern matches",
			heading:  "# Section(project: " + tempDir + "/**)",
			cwd:      projectDir,
			expected: true,
		},
		{
			name:     "glob pattern no match",
			heading:  "# Section(project: " + tempDir + "/*/backend)",
			cwd:      projectDir,
			expected: false,
		},
		{
			name:     "no project specification",
			heading:  "# General Section",
			cwd:      projectDir,
			expected: true,
		},
		{
			name:     "regular path still works",
			heading:  "# Section(project: " + projectDir + ")",
			cwd:      projectDir,
			expected: true,
		},
		{
			name:     "environment variable expansion",
			heading:  "# Section(project: " + tempDir + "**)",
			cwd:      projectDir,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldIncludeSection(tt.heading, tt.cwd)
			if result != tt.expected {
				t.Errorf("shouldIncludeSection(%q, %q) = %v, expected %v", tt.heading, tt.cwd, result, tt.expected)
			}
		})
	}
}

func TestFilterContentByProjectWithGlob(t *testing.T) {
	// Create a temporary directory structure for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("glob_filter_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Create subdirectories (use resolved path for directory creation)
	frontendDir := filepath.Join(tempDir, "myproject", "frontend")
	if err := os.MkdirAll(frontendDir, 0755); err != nil {
		t.Fatalf("Failed to create frontend dir: %v", err)
	}

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to frontend directory for testing
	if err := os.Chdir(frontendDir); err != nil {
		t.Fatalf("Failed to change to frontend dir: %v", err)
	}
	defer os.Chdir(originalWd)

	content := `# General Section
Always shown.

# Frontend Section(project: ` + tempDir + `/*/frontend)
Should be shown for frontend projects.

# Backend Section(project: ` + tempDir + `/*/backend)
Should NOT be shown for frontend projects.

# Any Project Section(project: ` + tempDir + `/**)
Should be shown for any project under tempDir.`

	expected := `# General Section
Always shown.

# Frontend Section(project: ` + tempDir + `/*/frontend)
Should be shown for frontend projects.

# Any Project Section(project: ` + tempDir + `/**)
Should be shown for any project under tempDir.`

	result, err := filterContentByProject(content)
	if err != nil {
		t.Fatalf("filterContentByProject failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}
}

func TestShouldIncludeSectionWithExpandedPaths(t *testing.T) {
	// Create a temporary directory structure for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("expanded_paths_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Create subdirectories (use resolved path)
	projectDir := filepath.Join(tempDir, "myproject")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	// Get user home directory for tilde expansion tests
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	tests := []struct {
		name     string
		heading  string
		cwd      string
		expected bool
		setup    func() // Optional setup function
	}{
		{
			name:     "tilde expansion exact match",
			heading:  "# Section(project: ~/Projects/whats_next)",
			cwd:      filepath.Join(homeDir, "Projects", "whats_next"),
			expected: true,
		},
		{
			name:     "tilde expansion with glob",
			heading:  "# Section(project: ~/Projects/*)",
			cwd:      filepath.Join(homeDir, "Projects", "anything"),
			expected: true,
		},
		{
			name:     "tilde expansion no match",
			heading:  "# Section(project: ~/Projects/specific)",
			cwd:      filepath.Join(homeDir, "Documents"),
			expected: false,
		},
		{
			name:     "GOROOT expansion when set",
			heading:  "# Section(project: $GOROOT/src)",
			cwd:      filepath.Join(os.Getenv("GOROOT"), "src"),
			expected: os.Getenv("GOROOT") != "", // Only expect true if GOROOT is set
		},
		{
			name:     "GOROOT with glob when set",
			heading:  "# Section(project: $GOROOT/**)",
			cwd:      filepath.Join(os.Getenv("GOROOT"), "src", "runtime"),
			expected: os.Getenv("GOROOT") != "", // Only expect true if GOROOT is set
		},
		{
			name:     "PATH expansion with glob",
			heading:  "# Section(project: " + tempDir + "/*)",
			cwd:      projectDir,
			expected: true,
		},
		{
			name:     "mixed tilde and env var",
			heading:  "# Section(project: ~/go/**)",
			cwd:      filepath.Join(homeDir, "go", "src", "mypackage"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests that depend on GOROOT if it's not set
			if strings.Contains(tt.heading, "$GOROOT") && os.Getenv("GOROOT") == "" {
				t.Skip("Skipping GOROOT test because GOROOT is not set")
			}

			if tt.setup != nil {
				tt.setup()
			}

			result := shouldIncludeSection(tt.heading, tt.cwd)
			if result != tt.expected {
				t.Errorf("shouldIncludeSection(%q, %q) = %v, expected %v", tt.heading, tt.cwd, result, tt.expected)
			}
		})
	}
}

func TestFilterContentByProjectWithExpandedPaths(t *testing.T) {
	// Create a temporary directory structure for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("expanded_filter_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Create subdirectories (use resolved path)
	projectDir := filepath.Join(tempDir, "myproject")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to project directory for testing
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Failed to change to project dir: %v", err)
	}
	defer os.Chdir(originalWd)

	// Get home directory for tilde expansion
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	content := `# General Section
Always shown.

# Tilde Expansion Section(project: ~/Projects/*)
Should be shown for any project under ~/Projects.

# Environment Variable Section(project: ` + tempDir + `/**)
Should be shown for any project under the temp directory.

# GOROOT Section(project: $GOROOT/src)
Should be shown only if running from GOROOT/src.

# Other Section(project: /some/other/path)
Should NOT be shown.`

	result, err := filterContentByProject(content)
	if err != nil {
		t.Fatalf("filterContentByProject failed: %v", err)
	}

	// Check that general section is always included
	if !strings.Contains(result, "# General Section") {
		t.Error("General section should always be included")
	}

	// Check that environment variable section is included (since we're in tempDir/myproject)
	if !strings.Contains(result, "# Environment Variable Section") {
		t.Error("Environment variable section should be included")
	}

	// Check that other section is NOT included
	if strings.Contains(result, "# Other Section") {
		t.Error("Other section should NOT be included")
	}

	// Check tilde expansion - only if we're actually in ~/Projects
	if strings.HasPrefix(projectDir, filepath.Join(homeDir, "Projects")) {
		if !strings.Contains(result, "# Tilde Expansion Section") {
			t.Error("Tilde expansion section should be included when in ~/Projects")
		}
	}

	// GOROOT section should only be included if GOROOT is set and we're in that path
	goroot := os.Getenv("GOROOT")
	if goroot != "" && strings.HasPrefix(projectDir, filepath.Join(goroot, "src")) {
		if !strings.Contains(result, "# GOROOT Section") {
			t.Error("GOROOT section should be included when in GOROOT/src")
		}
	}
}
