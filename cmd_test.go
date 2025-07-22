package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWhere(t *testing.T) {
	// Test that where command works without arguments
	err := where([]string{})
	if err != nil {
		t.Errorf("where() with no args should not error, got: %v", err)
	}
}

func TestWhereWithArgs(t *testing.T) {
	// Test that where command rejects arguments
	err := where([]string{"some-arg"})
	if err == nil {
		t.Error("where() with args should return error")
	}

	expectedError := "where command does not accept arguments"
	if err.Error() != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, err.Error())
	}
}

func TestWhereOutputsCorrectPath(t *testing.T) {
	// Test that the path returned by getConfigDir matches what where would print
	configDir, err := getConfigDir(false)
	if err != nil {
		t.Fatalf("getConfigDir failed: %v", err)
	}

	// The path should be under the user's config directory
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir failed: %v", err)
	}

	expectedPath := filepath.Join(userConfigDir, "whats_next")
	if configDir != expectedPath {
		t.Errorf("Expected config dir %q, got %q", expectedPath, configDir)
	}
}
