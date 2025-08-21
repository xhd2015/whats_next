package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []Section
	}{
		{
			name:     "empty content",
			content:  "",
			expected: []Section{},
		},
		{
			name:     "no sections",
			content:  "Just some text\nwithout headers",
			expected: []Section{},
		},
		{
			name: "single section",
			content: `# Header 1
Content line 1
Content line 2`,
			expected: []Section{
				{
					Title:   "# Header 1",
					Content: "Content line 1\nContent line 2",
				},
			},
		},
		{
			name: "multiple sections",
			content: `# Header 1
Content 1

# Header 2
Content 2
More content 2

## Header 3
Content 3`,
			expected: []Section{
				{
					Title:   "# Header 1",
					Content: "Content 1\n",
				},
				{
					Title:   "# Header 2",
					Content: "Content 2\nMore content 2\n",
				},
				{
					Title:   "## Header 3",
					Content: "Content 3",
				},
			},
		},
		{
			name: "sections with code blocks",
			content: `# Header 1
Some text
` + "```bash" + `
# This should not be a header
echo "hello"
` + "```" + `

# Header 2
More content`,
			expected: []Section{
				{
					Title:   "# Header 1",
					Content: "Some text\n```bash\n# This should not be a header\necho \"hello\"\n```\n",
				},
				{
					Title:   "# Header 2",
					Content: "More content",
				},
			},
		},
		{
			name: "nested code blocks",
			content: `# Header 1
` + "```go" + `
func main() {
    // # Not a header
    fmt.Println("hello")
}
` + "```" + `

# Header 2
` + "```markdown" + `
# This markdown header should not be parsed
## Neither should this
` + "```",
			expected: []Section{
				{
					Title:   "# Header 1",
					Content: "```go\nfunc main() {\n    // # Not a header\n    fmt.Println(\"hello\")\n}\n```\n",
				},
				{
					Title:   "# Header 2",
					Content: "```markdown\n# This markdown header should not be parsed\n## Neither should this\n```",
				},
			},
		},
		{
			name: "project specification sections",
			content: `# General Section
General content

# Project Section(project: /some/path)
Project specific content

# Another Section(project: /other/path)
Other project content`,
			expected: []Section{
				{
					Title:   "# General Section",
					Content: "General content\n",
				},
				{
					Title:   "# Project Section(project: /some/path)",
					Content: "Project specific content\n",
				},
				{
					Title:   "# Another Section(project: /other/path)",
					Content: "Other project content",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSections(tt.content)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d sections, got %d", len(tt.expected), len(result))
				return
			}

			for i, section := range result {
				if section.Title != tt.expected[i].Title {
					t.Errorf("Section %d title: expected %q, got %q", i, tt.expected[i].Title, section.Title)
				}
				if section.Content != tt.expected[i].Content {
					t.Errorf("Section %d content: expected %q, got %q", i, tt.expected[i].Content, section.Content)
				}
			}
		})
	}
}

func TestShouldIncludeSection(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "whats_next_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	tests := []struct {
		name     string
		heading  string
		cwd      string
		expected bool
	}{
		{
			name:     "no project specification",
			heading:  "# General Section",
			cwd:      tempDir,
			expected: true,
		},
		{
			name:     "malformed project specification - no closing paren",
			heading:  "# Section(project: /some/path",
			cwd:      tempDir,
			expected: true,
		},
		{
			name:     "exact path match",
			heading:  "# Section(project: " + tempDir + ")",
			cwd:      tempDir,
			expected: true,
		},
		{
			name:     "subdirectory match",
			heading:  "# Section(project: " + tempDir + ")",
			cwd:      subDir,
			expected: true,
		},
		{
			name:     "no match - different path",
			heading:  "# Section(project: /completely/different/path)",
			cwd:      tempDir,
			expected: false,
		},
		{
			name:     "relative path match",
			heading:  "# Section(project: .)",
			cwd:      tempDir,
			expected: true,
		},
		{
			name:     "whitespace in project path",
			heading:  "# Section(project:   " + tempDir + "   )",
			cwd:      tempDir,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, _, _ := shouldIncludeSection(tt.heading, tt.cwd, true)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for heading %q with cwd %q", tt.expected, result, tt.heading, tt.cwd)
			}
		})
	}
}

func TestFilterContentByProject(t *testing.T) {
	// Create a temporary directory for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("whats_next_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to temp directory for testing (use resolved path)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}
	defer os.Chdir(originalWd)

	content := `# General Section
This should always be shown.

# Current Project Section(project: ` + tempDir + `)
This should be shown when in the project directory.

# Other Project Section(project: /some/other/path)
This should NOT be shown.

# Another General Section
This should also be shown.`

	expected := `# General Section
This should always be shown.

# Current Project Section(project: ` + tempDir + `)
This should be shown when in the project directory.

# Another General Section
This should also be shown.`

	result, err := filterContentByProject(content)
	if err != nil {
		t.Fatalf("filterContentByProject failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}
}

func TestFilterContentByProjectWithCodeBlocks(t *testing.T) {
	// Create a temporary directory for testing using the helper
	originalTempDir, tempDir, err := mkdirTempResolved("whats_next_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(originalTempDir) // Use original path for cleanup

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to temp directory for testing (use resolved path)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}
	defer os.Chdir(originalWd)

	content := `# General Section
Code example:
` + "```bash" + `
# This should not be treated as heading
echo "hello"
` + "```" + `

# Project Section(project: ` + tempDir + `)
This should be shown.
` + "```go" + `
// # This comment should not be a heading
func main() {}
` + "```" + `

# Other Project Section(project: /other/path)
This should be filtered out.`

	expected := `# General Section
Code example:
` + "```bash" + `
# This should not be treated as heading
echo "hello"
` + "```" + `

# Project Section(project: ` + tempDir + `)
This should be shown.
` + "```go" + `
// # This comment should not be a heading
func main() {}
` + "```" + `
`

	result, err := filterContentByProject(content)
	if err != nil {
		t.Fatalf("filterContentByProject failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}
}
