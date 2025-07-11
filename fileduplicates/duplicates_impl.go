package fileduplicates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Initialize the hash cache
func NewHashCache() *HashCache {
	cache := &HashCache{
		Entries: make(map[string]CacheEntry),
	}

	// Try to load cached hashes
	cache.Load()

	return cache
}

// Get a hash from cache or return empty string if not found
func (c *HashCache) Get(path string, size int64, hashType FileHashType) string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if entry, ok := c.Entries[path]; ok && entry.Size == size {
		// Return the requested hash type
		if hashType == QuickHash {
			return entry.QuickHash
		} else if hashType == FullHash {
			return entry.FullHash
		}
	}

	return ""
}

// Store a hash in the cache
func (c *HashCache) Store(file DuplicateFileInfo) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.Entries[file.Path] = CacheEntry{
		Path:      file.Path,
		Size:      file.Size,
		QuickHash: file.QuickHash,
		FullHash:  file.FullHash,
		LastSeen:  time.Now(),
	}
}

// Save the cache to disk
func (c *HashCache) Save() error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Clean up any entries older than 30 days and remove entries for files that no longer exist
	threshold := time.Now().AddDate(0, 0, -30)
	filtered := make(map[string]CacheEntry)

	entriesCount := len(c.Entries)
	processedCount := 0
	removedCount := 0

	fmt.Printf("Optimizing hash cache (%d entries)...\n", entriesCount)

	for path, entry := range c.Entries {
		processedCount++

		// Show periodic progress for large caches
		if processedCount%1000 == 0 || processedCount == entriesCount {
			fmt.Printf("\r  Cache cleanup progress: %d/%d entries (removed: %d)...",
				processedCount, entriesCount, removedCount)
		}

		// Keep entry if it's recent and file still exists
		if entry.LastSeen.After(threshold) {
			// Check if file still exists and has same size
			if info, err := os.Stat(path); err == nil && info.Size() == entry.Size {
				filtered[path] = entry
				continue
			}
		}

		removedCount++
	}

	fmt.Printf("\r  Cache cleanup complete: kept %d entries, removed %d stale entries.\n",
		len(filtered), removedCount)

	// Marshal to JSON
	data, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling hash cache: %w", err)
	}

	// Write to disk
	cachePath := filepath.Join(filepath.Dir(os.Args[0]), HASH_CACHE_FILE)
	return os.WriteFile(cachePath, data, 0644)
}

// Load cache from disk
func (c *HashCache) Load() error {
	cachePath := filepath.Join(filepath.Dir(os.Args[0]), HASH_CACHE_FILE)

	// If file doesn't exist, just return with empty cache
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return nil
	}

	fmt.Println("Loading hash cache...")

	// Read cache file
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return fmt.Errorf("error reading hash cache: %w", err)
	}

	// Parse JSON
	var entries map[string]CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("error parsing hash cache: %w", err)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.Entries = entries
	fmt.Printf("Cache loaded with %d entries.\n", len(entries))
	return nil
}
