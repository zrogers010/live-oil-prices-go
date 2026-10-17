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
	staticRoot = "web/static"
)

func Init() error {
	hashesMu.Lock()
	defer hashesMu.Unlock()

	hashes = make(map[string]string)

	assetPaths := []string{
		"js/app.js",
		"js/detail.js",
		"js/share.js",
		"css/styles.css",
	}

	for _, assetPath := range assetPaths {
		fullPath := filepath.Join(staticRoot, assetPath)
		hash, err := computeFileHash(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("Warning: asset %s not found, skipping hash", assetPath)
				continue
			}
			return fmt.Errorf("failed to hash %s: %w", assetPath, err)
		}
		hashes[assetPath] = hash
	}

	log.Printf("Asset hashes initialized: %d assets", len(hashes))
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
