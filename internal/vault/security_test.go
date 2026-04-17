package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePath_ValidPath(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "vault-security-*")
	defer os.RemoveAll(tmpDir)

	sec := NewSecurityLayer(tmpDir)
	validPath := filepath.Join(tmpDir, "notes", "test.md")

	result, err := sec.ValidatePath(validPath)
	if err != nil {
		t.Fatalf("Expected no error for valid path, got %v", err)
	}

	expected := filepath.Clean(validPath)
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestValidatePath_RelativePath(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "vault-security-*")
	defer os.RemoveAll(tmpDir)

	sec := NewSecurityLayer(tmpDir)
	relativePath := "notes/test.md"

	result, err := sec.ValidatePath(relativePath)
	if err != nil {
		t.Fatalf("Expected no error for relative path, got %v", err)
	}

	expected := filepath.Join(tmpDir, "notes", "test.md")
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestValidatePath_TraversalAttempt(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "vault-security-*")
	defer os.RemoveAll(tmpDir)

	sec := NewSecurityLayer(tmpDir)
	maliciousPath := filepath.Join(tmpDir, "..", "..", "..", "etc", "passwd")

	_, err := sec.ValidatePath(maliciousPath)
	if err == nil {
		t.Fatal("Expected error for traversal attempt")
	}

	if !strings.Contains(err.Error(), "path_violation") {
		t.Errorf("Expected path_violation error, got %v", err)
	}
}

func TestValidatePath_ObsidianDirectory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "vault-security-*")
	defer os.RemoveAll(tmpDir)

	sec := NewSecurityLayer(tmpDir)
	protectedPath := filepath.Join(tmpDir, ".obsidian", "config.json")

	_, err := sec.ValidatePath(protectedPath)
	if err == nil {
		t.Fatal("Expected error for .obsidian access")
	}

	if !strings.Contains(err.Error(), "protected directory") {
		t.Errorf("Expected protected directory error, got %v", err)
	}
}

func TestValidatePath_GitDirectory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "vault-security-*")
	defer os.RemoveAll(tmpDir)

	sec := NewSecurityLayer(tmpDir)
	protectedPath := filepath.Join(tmpDir, ".git", "config")

	_, err := sec.ValidatePath(protectedPath)
	if err == nil {
		t.Fatal("Expected error for .git access")
	}

	if !strings.Contains(err.Error(), "protected directory") {
		t.Errorf("Expected protected directory error, got %v", err)
	}
}

func TestValidatePath_RelativeTraversal(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "vault-security-*")
	defer os.RemoveAll(tmpDir)

	sec := NewSecurityLayer(tmpDir)
	relativePath := "../outside.md"

	_, err := sec.ValidatePath(relativePath)
	if err == nil {
		t.Fatal("Expected error for relative traversal")
	}

	if !strings.Contains(err.Error(), "path_violation") {
		t.Errorf("Expected path_violation error, got %v", err)
	}
}
