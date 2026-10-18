package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	hashes     map[string]string
	hashesMu   sync.RWMutex
	staticRoot string
)

func Init() error {
	return InitWithRoot("web/static")
}

// InitWithRoot allows tests to provide a custom static root
func InitWithRoot(root string) error {
	hashesMu.Lock()
	defer hashesMu.Unlock()

	// Make the path absolute
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve static root path: %w", err)
	}
	
	// Verify the directory exists
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		return fmt.Errorf("static root directory does not exist: %s", absRoot)
	}
	
	staticRoot = absRoot
	hashes = make(map[string]string)

	// List of required assets (share.js removed - it doesn't exist)
	assetPaths := []string{
		"js/app.js",
		"js/detail.js",
		"css/styles.css",
	}

	missingAssets := []string{}
	
	for _, assetPath := range assetPaths {
		fullPath := filepath.Join(staticRoot, assetPath)
		hash, err := computeFileHash(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				missingAssets = append(missingAssets, assetPath)
				log.Printf("ERROR: Required asset not found: %s (full path: %s)", assetPath, fullPath)
				continue
			}
			return fmt.Errorf("failed to hash %s: %w", assetPath, err)
		}
		hashes[assetPath] = hash
	}

	if len(missingAssets) > 0 {
		return fmt.Errorf("missing required assets: %v (static root: %s)", missingAssets, staticRoot)
	}

	log.Printf("Asset hashes initialized: %d assets from %s", len(hashes), staticRoot)
	return nil
}

func computeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

func GetVersionedURL(path string) string {
	hashesMu.RLock()
	defer hashesMu.RUnlock()

	path = strings.TrimPrefix(path, "/")

	hash, ok := hashes[path]
	if !ok {
		return "/" + path
	}

	return fmt.Sprintf("/%s?v=%s", path, hash)
}

func GetHash(path string) (string, bool) {
	hashesMu.RLock()
	defer hashesMu.RUnlock()

	path = strings.TrimPrefix(path, "/")
	hash, ok := hashes[path]
	return hash, ok
}
