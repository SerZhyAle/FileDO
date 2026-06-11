package fileduplicates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GetHashCachePath returns the single, stable location of the hash cache file.
// Both load and save use this resolver so the cache is never "lost" between
// runs. The cache lives next to the executable, which makes the path
// predictable regardless of the current working directory.
func GetHashCachePath() string {
	if exe, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
		return filepath.Join(filepath.Dir(exe), HASH_CACHE_FILE)
	}
	// Fallback: directory of os.Args[0].
	return filepath.Join(filepath.Dir(os.Args[0]), HASH_CACHE_FILE)
}

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

// LookupHash returns a cached hash for the file without ever computing it.
// The cache entry is only considered valid when both Size and ModTime still
// match the file, so a changed file that kept the same size will miss the cache.
func (c *HashCache) LookupHash(file DuplicateFileInfo, hashType FileHashType) (string, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry, ok := c.Entries[file.Path]
	if !ok {
		return "", false
	}
	// Validate file identity: size + modification time must match.
	if entry.Size != file.Size || !entry.ModTime.Equal(file.ModTime) {
		return "", false
	}
	if hashType == QuickHash && entry.QuickHash != "" {
		return entry.QuickHash, true
	}
	if hashType == FullHash && entry.FullHash != "" {
		return entry.FullHash, true
	}
	return "", false
}

// StoreHash records a freshly computed hash for the file. If an existing entry
// describes a different version of the file (size or modtime changed) it is
// replaced so stale hashes never linger. LastSeen and ModTime are always
// refreshed.
func (c *HashCache) StoreHash(file DuplicateFileInfo, hashType FileHashType) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	entry, ok := c.Entries[file.Path]
	if !ok || entry.Size != file.Size || !entry.ModTime.Equal(file.ModTime) {
		entry = CacheEntry{
			Path:    file.Path,
			Size:    file.Size,
			ModTime: file.ModTime,
		}
	}
	if hashType == QuickHash {
		entry.QuickHash = file.QuickHash
	} else {
		entry.FullHash = file.FullHash
	}
	entry.LastSeen = time.Now()
	c.Entries[file.Path] = entry
}

// Store a hash in the cache
func (c *HashCache) Store(file DuplicateFileInfo) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.Entries[file.Path] = CacheEntry{
		Path:      file.Path,
		Size:      file.Size,
		ModTime:   file.ModTime,
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

		// Keep entry if it's recent and the file is unchanged (same size + modtime).
		if entry.LastSeen.After(threshold) {
			if info, err := os.Stat(path); err == nil &&
				info.Size() == entry.Size && info.ModTime().Equal(entry.ModTime) {
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

	// Write to disk atomically: write to a temp file then rename, so an
	// interrupted process never leaves a corrupted cache behind.
	cachePath := GetHashCachePath()
	tmpPath := cachePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("error writing hash cache temp file: %w", err)
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("error finalizing hash cache: %w", err)
	}
	return nil
}

// Load cache from disk
func (c *HashCache) Load() error {
	cachePath := GetHashCachePath()

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
