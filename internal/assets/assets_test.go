package assets

import (
	"os"
	"path/filepath"
	"regexp"
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
	tempDir := t.TempDir()
	
	// Create only partial assets
	jsDir := filepath.Join(tempDir, "js")
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js directory: %v", err)
	}

	testFile := filepath.Join(jsDir, "app.js")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Init should fail when required assets are missing
	err := InitWithRoot(tempDir)
	if err == nil {
		t.Fatal("Expected Init to fail with missing required assets")
	}
	
	if !strings.Contains(err.Error(), "missing required assets") {
		t.Errorf("Expected 'missing required assets' error, got: %v", err)
	}
}

func TestInitWithAllAssets(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create all required assets
	jsDir := filepath.Join(tempDir, "js")
	cssDir := filepath.Join(tempDir, "css")
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js directory: %v", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		t.Fatalf("Failed to create css directory: %v", err)
	}

	// Create test files with different content
	files := map[string]string{
		"js/app.js":       "console.log('app');",
		"js/detail.js":    "console.log('detail');",
		"css/styles.css":  "body { margin: 0; }",
	}
	
	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", path, err)
		}
	}

	// Init should succeed with all required assets
	if err := InitWithRoot(tempDir); err != nil {
		t.Fatalf("Init should succeed with all required assets: %v", err)
	}

	// Verify all assets have hashes
	for path := range files {
		hash, ok := GetHash(path)
		if !ok {
			t.Errorf("Expected hash for %s", path)
		}
		if hash == "" {
			t.Errorf("Expected non-empty hash for %s", path)
		}
		if len(hash) != 12 {
			t.Errorf("Expected hash length 12 for %s, got %d", path, len(hash))
		}
	}
}

func TestInitNonExistentRoot(t *testing.T) {
	err := InitWithRoot("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("Expected Init to fail with non-existent root")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected 'does not exist' error, got: %v", err)
	}
}

func TestVersionedURLFormat(t *testing.T) {
	hashes = map[string]string{
		"js/app.js": "abc123def456",
	}

	url := GetVersionedURL("/js/app.js")
	if !strings.HasPrefix(url, "/js/app.js?v=") {
		t.Errorf("Versioned URL should have ?v= query parameter: %s", url)
	}
	
	// Verify it's a 12-character hex hash
	re := regexp.MustCompile(`\?v=([0-9a-f]{12})$`)
	if !re.MatchString(url) {
		t.Errorf("Expected 12-character hex hash in URL, got: %s", url)
	}
}
