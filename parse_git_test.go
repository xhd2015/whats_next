package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitWorktreeDetection tests the git worktree detection functionality
// using actual git repositories and worktrees
func TestGitWorktreeDetection(t *testing.T) {
	// Create temporary directory for test repositories
	tmpDir, err := os.MkdirTemp("", "whats_next_git_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("same_repository_different_worktrees", func(t *testing.T) {
		testSameRepoWorktrees(t, tmpDir)
	})

	t.Run("different_repositories_same_origin", func(t *testing.T) {
		testDifferentReposSameOrigin(t, tmpDir)
	})

	t.Run("no_origin_remote_configured", func(t *testing.T) {
		testNoOriginRemote(t, tmpDir)
	})

	t.Run("non_git_directories", func(t *testing.T) {
		testNonGitDirectories(t, tmpDir)
	})

	t.Run("main_repo_and_worktree", func(t *testing.T) {
		testMainRepoAndWorktree(t, tmpDir)
	})
}

// testSameRepoWorktrees tests detection between different worktrees of the same repository
func testSameRepoWorktrees(t *testing.T, tmpDir string) {
	// Create main repository
	mainRepo := filepath.Join(tmpDir, "main_repo")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("Failed to create main repo dir: %v", err)
	}

	// Initialize git repo
	runGitCmd(t, mainRepo, "init")
	runGitCmd(t, mainRepo, "config", "user.email", "test@example.com")
	runGitCmd(t, mainRepo, "config", "user.name", "Test User")

	// Create initial commit
	testFile := filepath.Join(mainRepo, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	runGitCmd(t, mainRepo, "add", "test.txt")
	runGitCmd(t, mainRepo, "commit", "-m", "Initial commit")

	// Create a worktree
	worktree1 := filepath.Join(tmpDir, "worktree1")
	runGitCmd(t, mainRepo, "worktree", "add", worktree1)

	// Create another worktree
	worktree2 := filepath.Join(tmpDir, "worktree2")
	runGitCmd(t, mainRepo, "worktree", "add", worktree2)

	// Test: main repo should detect worktree1 as related
	if !isGitWorktree(worktree1, mainRepo) {
		t.Error("Expected worktree1 to be detected as related to main repo")
	}

	// Test: worktree1 should detect main repo as related
	if !isGitWorktree(mainRepo, worktree1) {
		t.Error("Expected main repo to be detected as related to worktree1")
	}

	// Test: worktree1 should detect worktree2 as related (same main repo)
	if !isGitWorktree(worktree1, worktree2) {
		t.Error("Expected worktree1 to be detected as related to worktree2")
	}

	// Test: worktree2 should detect worktree1 as related (same main repo)
	if !isGitWorktree(worktree2, worktree1) {
		t.Error("Expected worktree2 to be detected as related to worktree1")
	}
}

// testDifferentReposSameOrigin tests detection between different clones with same origin
func testDifferentReposSameOrigin(t *testing.T, tmpDir string) {
	// Create bare repository to act as origin
	bareRepo := filepath.Join(tmpDir, "bare_repo.git")
	if err := os.MkdirAll(bareRepo, 0755); err != nil {
		t.Fatalf("Failed to create bare repo dir: %v", err)
	}
	runGitCmd(t, bareRepo, "init", "--bare")

	// Create first clone
	clone1 := filepath.Join(tmpDir, "clone1")
	runGitCmd(t, tmpDir, "clone", bareRepo, "clone1")
	runGitCmd(t, clone1, "config", "user.email", "test@example.com")
	runGitCmd(t, clone1, "config", "user.name", "Test User")

	// Create initial commit in clone1
	testFile := filepath.Join(clone1, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	runGitCmd(t, clone1, "add", "test.txt")
	runGitCmd(t, clone1, "commit", "-m", "Initial commit")
	runGitCmd(t, clone1, "push", "origin", "master")

	// Create second clone
	clone2 := filepath.Join(tmpDir, "clone2")
	runGitCmd(t, tmpDir, "clone", bareRepo, "clone2")

	// Test: different clones with same origin should be detected as related
	if !isGitWorktree(clone1, clone2) {
		t.Error("Expected clone1 to be detected as related to clone2 (same origin)")
	}

	if !isGitWorktree(clone2, clone1) {
		t.Error("Expected clone2 to be detected as related to clone1 (same origin)")
	}
}

// testNoOriginRemote tests behavior when no origin remote is configured
func testNoOriginRemote(t *testing.T, tmpDir string) {
	// Create repository without origin remote
	repo1 := filepath.Join(tmpDir, "no_origin_repo1")
	if err := os.MkdirAll(repo1, 0755); err != nil {
		t.Fatalf("Failed to create repo1 dir: %v", err)
	}
	runGitCmd(t, repo1, "init")
	runGitCmd(t, repo1, "config", "user.email", "test@example.com")
	runGitCmd(t, repo1, "config", "user.name", "Test User")

	// Create initial commit
	testFile := filepath.Join(repo1, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	runGitCmd(t, repo1, "add", "test.txt")
	runGitCmd(t, repo1, "commit", "-m", "Initial commit")

	// Create worktree from repo1
	worktree := filepath.Join(tmpDir, "no_origin_worktree")
	runGitCmd(t, repo1, "worktree", "add", worktree)

	// Create another separate repository without origin
	repo2 := filepath.Join(tmpDir, "no_origin_repo2")
	if err := os.MkdirAll(repo2, 0755); err != nil {
		t.Fatalf("Failed to create repo2 dir: %v", err)
	}
	runGitCmd(t, repo2, "init")
	runGitCmd(t, repo2, "config", "user.email", "test@example.com")
	runGitCmd(t, repo2, "config", "user.name", "Test User")

	// Test: repo1 and its worktree should be detected as related (no origin needed)
	if !isGitWorktree(repo1, worktree) {
		t.Error("Expected repo1 to be detected as related to its worktree (no origin)")
	}

	if !isGitWorktree(worktree, repo1) {
		t.Error("Expected worktree to be detected as related to repo1 (no origin)")
	}

	// Test: repo1 and repo2 should NOT be detected as related (different repos, no origin)
	if isGitWorktree(repo1, repo2) {
		t.Error("Expected repo1 to NOT be detected as related to repo2 (different repos, no origin)")
	}

	if isGitWorktree(repo2, repo1) {
		t.Error("Expected repo2 to NOT be detected as related to repo1 (different repos, no origin)")
	}
}

// testNonGitDirectories tests behavior with non-git directories
func testNonGitDirectories(t *testing.T, tmpDir string) {
	// Create non-git directories
	nonGitDir1 := filepath.Join(tmpDir, "non_git1")
	nonGitDir2 := filepath.Join(tmpDir, "non_git2")

	if err := os.MkdirAll(nonGitDir1, 0755); err != nil {
		t.Fatalf("Failed to create non-git dir1: %v", err)
	}
	if err := os.MkdirAll(nonGitDir2, 0755); err != nil {
		t.Fatalf("Failed to create non-git dir2: %v", err)
	}

	// Create a git repository
	gitRepo := filepath.Join(tmpDir, "git_repo")
	if err := os.MkdirAll(gitRepo, 0755); err != nil {
		t.Fatalf("Failed to create git repo dir: %v", err)
	}
	runGitCmd(t, gitRepo, "init")

	// Test: non-git directories should not be detected as related
	if isGitWorktree(nonGitDir1, nonGitDir2) {
		t.Error("Expected non-git directories to NOT be detected as related")
	}

	// Test: non-git directory and git repo should not be detected as related
	if isGitWorktree(nonGitDir1, gitRepo) {
		t.Error("Expected non-git directory to NOT be detected as related to git repo")
	}

	if isGitWorktree(gitRepo, nonGitDir1) {
		t.Error("Expected git repo to NOT be detected as related to non-git directory")
	}
}

// testMainRepoAndWorktree tests the relationship between main repo and its worktree
func testMainRepoAndWorktree(t *testing.T, tmpDir string) {
	// Create main repository
	mainRepo := filepath.Join(tmpDir, "main_with_worktree")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("Failed to create main repo dir: %v", err)
	}

	runGitCmd(t, mainRepo, "init")
	runGitCmd(t, mainRepo, "config", "user.email", "test@example.com")
	runGitCmd(t, mainRepo, "config", "user.name", "Test User")

	// Create initial commit
	testFile := filepath.Join(mainRepo, "main.txt")
	if err := os.WriteFile(testFile, []byte("main content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	runGitCmd(t, mainRepo, "add", "main.txt")
	runGitCmd(t, mainRepo, "commit", "-m", "Initial commit")

	// Create feature branch and worktree
	runGitCmd(t, mainRepo, "branch", "feature")
	featureWorktree := filepath.Join(tmpDir, "feature_worktree")
	runGitCmd(t, mainRepo, "worktree", "add", featureWorktree, "feature")

	// Test shouldIncludeSection with project specification
	heading := "# Test Section (project: " + mainRepo + ")"

	// From main repo, should include section
	if include, _, _, _ := shouldIncludeSection(heading, mainRepo, true); !include {
		t.Error("Expected section to be included when in main repo")
	}

	// From worktree, should include section (related to main repo)
	if include, _, _, _ := shouldIncludeSection(heading, featureWorktree, true); !include {
		t.Error("Expected section to be included when in worktree of specified project")
	}

	// Test with worktree path in heading
	worktreeHeading := "# Test Section (project: " + featureWorktree + ")"

	// From main repo, should include section (related to worktree)
	if include, _, _, _ := shouldIncludeSection(worktreeHeading, mainRepo, true); !include {
		t.Error("Expected section to be included when main repo is related to specified worktree")
	}

	// From worktree, should include section
	if include, _, _, _ := shouldIncludeSection(worktreeHeading, featureWorktree, true); !include {
		t.Error("Expected section to be included when in specified worktree")
	}
}

// TestProjectPathReplacement tests that project paths are replaced when matched via git worktree
func TestProjectPathReplacement(t *testing.T) {
	// Create temporary directory for test repositories
	tmpDir, err := os.MkdirTemp("", "whats_next_path_replacement_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create main repository
	mainRepo := filepath.Join(tmpDir, "main_repo")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("Failed to create main repo dir: %v", err)
	}

	// Initialize git repo
	runGitCmd(t, mainRepo, "init")
	runGitCmd(t, mainRepo, "config", "user.email", "test@example.com")
	runGitCmd(t, mainRepo, "config", "user.name", "Test User")

	// Create initial commit
	testFile := filepath.Join(mainRepo, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	runGitCmd(t, mainRepo, "add", "test.txt")
	runGitCmd(t, mainRepo, "commit", "-m", "Initial commit")

	// Create a worktree
	worktree := filepath.Join(tmpDir, "feature_worktree")
	runGitCmd(t, mainRepo, "worktree", "add", worktree)

	// Test content with project specification pointing to main repo
	content := `# Section 1
Some content here

# Section 2 (project: ` + mainRepo + `)
This section should have its project path replaced when viewed from worktree

# Section 3
More content
`

	// Filter content from worktree perspective
	filteredContent := filterContentByDir(content, worktree, true)

	// The project path should be replaced with the worktree path
	expectedContent := `# Section 1
Some content here

# Section 2 (project: ` + worktree + `)
This section should have its project path replaced when viewed from worktree

# Section 3
More content
`

	if filteredContent != expectedContent {
		t.Errorf("Project path replacement failed.\nExpected:\n%q\n\nGot:\n%q", expectedContent, filteredContent)
		t.Errorf("Expected length: %d, Got length: %d", len(expectedContent), len(filteredContent))
	}

	// Test that path replacement does NOT happen for direct path matches
	// Create a subdirectory of main repo
	subDir := filepath.Join(mainRepo, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Filter content from subdirectory (should be direct path match, no replacement)
	filteredFromSubdir := filterContentByDir(content, subDir, true)

	// Should keep original project path since it's a direct path match
	expectedFromSubdir := `# Section 1
Some content here

# Section 2 (project: ` + mainRepo + `)
This section should have its project path replaced when viewed from worktree

# Section 3
More content
`

	if filteredFromSubdir != expectedFromSubdir {
		t.Errorf("Path should NOT be replaced for direct path matches.\nExpected:\n%q\n\nGot:\n%q", expectedFromSubdir, filteredFromSubdir)
		t.Errorf("Expected length: %d, Got length: %d", len(expectedFromSubdir), len(filteredFromSubdir))
	}
}

// TestNestedProjectSpecificity tests that the most specific (innermost) project takes precedence
func TestNestedProjectSpecificity(t *testing.T) {
	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "whats_next_nested_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested directory structure: parent/child/grandchild
	parentDir := filepath.Join(tmpDir, "parent")
	childDir := filepath.Join(parentDir, "child")
	grandchildDir := filepath.Join(childDir, "grandchild")

	if err := os.MkdirAll(grandchildDir, 0755); err != nil {
		t.Fatalf("Failed to create nested dirs: %v", err)
	}

	// Test content with sections for different project levels
	content := `# General Section
This section has no project specification

# Parent Project Section (project: ` + parentDir + `)
Instructions for parent project

# Child Project Section (project: ` + childDir + `)
Instructions for child project

# Grandchild Project Section (project: ` + grandchildDir + `)
Instructions for grandchild project

# Another General Section
Another section with no project specification
`

	// Test from grandchild directory - should only show grandchild-specific sections
	filteredFromGrandchild := filterContentByDir(content, grandchildDir, true)
	expectedFromGrandchild := `# General Section
This section has no project specification

# Grandchild Project Section (project: ` + grandchildDir + `)
Instructions for grandchild project

# Another General Section
Another section with no project specification
`

	if filteredFromGrandchild != expectedFromGrandchild {
		t.Errorf("From grandchild dir, expected most specific match.\nExpected:\n%q\n\nGot:\n%q", expectedFromGrandchild, filteredFromGrandchild)
	}

	// Test from child directory - should only show child-specific sections
	filteredFromChild := filterContentByDir(content, childDir, true)
	expectedFromChild := `# General Section
This section has no project specification

# Child Project Section (project: ` + childDir + `)
Instructions for child project

# Another General Section
Another section with no project specification
`

	if filteredFromChild != expectedFromChild {
		t.Errorf("From child dir, expected most specific match.\nExpected:\n%q\n\nGot:\n%q", expectedFromChild, filteredFromChild)
	}

	// Test from parent directory - should only show parent-specific sections
	filteredFromParent := filterContentByDir(content, parentDir, true)
	expectedFromParent := `# General Section
This section has no project specification

# Parent Project Section (project: ` + parentDir + `)
Instructions for parent project

# Another General Section
Another section with no project specification
`

	if filteredFromParent != expectedFromParent {
		t.Errorf("From parent dir, expected most specific match.\nExpected:\n%q\n\nGot:\n%q", expectedFromParent, filteredFromParent)
	}

	// Test from outside directory - should only show general sections
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}

	filteredFromOutside := filterContentByDir(content, outsideDir, true)
	expectedFromOutside := `# General Section
This section has no project specification

# Another General Section
Another section with no project specification
`

	if filteredFromOutside != expectedFromOutside {
		t.Errorf("From outside dir, expected only general sections.\nExpected:\n%q\n\nGot:\n%q", expectedFromOutside, filteredFromOutside)
	}
}

// runGitCmd runs a git command in the specified directory
func runGitCmd(t *testing.T, dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Git command failed in %s: git %s\nOutput: %s\nError: %v",
			dir, strings.Join(args, " "), string(output), err)
	}
}
