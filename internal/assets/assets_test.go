package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeFileHash(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.js")
	
	content := []byte("console.log('test');")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	hash1, err := computeFileHash(testFile)
	if err != nil {
		t.Fatalf("computeFileHash failed: %v", err)
	}

	if len(hash1) != 12 {
		t.Errorf("Expected hash length 12, got %d", len(hash1))
	}

	hash2, err := computeFileHash(testFile)
	if err != nil {
		t.Fatalf("computeFileHash failed on second call: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("Hash should be deterministic: %s != %s", hash1, hash2)
	}

	if err := os.WriteFile(testFile, []byte("console.log('changed');"), 0644); err != nil {
		t.Fatalf("Failed to update test file: %v", err)
	}

	hash3, err := computeFileHash(testFile)
	if err != nil {
		t.Fatalf("computeFileHash failed after file change: %v", err)
	}

	if hash1 == hash3 {
		t.Errorf("Hash should change when file content changes")
	}
}

func TestGetVersionedURL(t *testing.T) {
	hashes = map[string]string{
		"js/app.js":     "abc123def456",
		"css/styles.css": "789xyz123abc",
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"/js/app.js", "/js/app.js?v=abc123def456"},
		{"js/app.js", "/js/app.js?v=abc123def456"},
		{"/css/styles.css", "/css/styles.css?v=789xyz123abc"},
		{"/js/nonexistent.js", "/js/nonexistent.js"},
		{"", "/"},
	}

	for _, tt := range tests {
		result := GetVersionedURL(tt.input)
		if result != tt.expected {
			t.Errorf("GetVersionedURL(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestGetHash(t *testing.T) {
	hashes = map[string]string{
		"js/app.js": "abc123def456",
	}

	hash, ok := GetHash("js/app.js")
	if !ok {
		t.Error("Expected hash to be found")
	}
	if hash != "abc123def456" {
		t.Errorf("Expected hash abc123def456, got %s", hash)
	}

	hash, ok = GetHash("/js/app.js")
	if !ok {
		t.Error("Expected hash to be found with leading slash")
	}
	if hash != "abc123def456" {
		t.Errorf("Expected hash abc123def456, got %s", hash)
	}

	_, ok = GetHash("js/nonexistent.js")
	if ok {
		t.Error("Expected hash not to be found for nonexistent file")
	}
}

func TestInitWithMissingAssets(t *testing.T) {
	oldStaticRoot := staticRoot
	staticRoot = t.TempDir()
	defer func() { staticRoot = oldStaticRoot }()

	jsDir := filepath.Join(staticRoot, "js")
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js directory: %v", err)
	}

	testFile := filepath.Join(jsDir, "app.js")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := Init(); err != nil {
		t.Fatalf("Init should not fail with missing optional assets: %v", err)
	}

	hash, ok := GetHash("js/app.js")
	if !ok {
		t.Error("Expected hash for existing app.js")
	}
	if hash == "" {
		t.Error("Expected non-empty hash")
	}
}

func TestVersionedURLFormat(t *testing.T) {
	hashes = map[string]string{
		"js/app.js": "test123",
	}

	url := GetVersionedURL("/js/app.js")
	if !strings.HasPrefix(url, "/js/app.js?v=") {
		t.Errorf("Versioned URL should have ?v= query parameter: %s", url)
	}
}
