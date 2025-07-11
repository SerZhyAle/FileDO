package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Create a new hash worker pool
func NewHashWorker(workerCount int) *HashWorker {
	if workerCount <= 0 {
		workerCount = MAX_WORKERS
	}

	hw := &HashWorker{
		jobs:        make(chan hashJob, workerCount*4),    // Увеличиваем буфер
		results:     make(chan hashResult, workerCount*4), // Увеличиваем буфер
		workerCount: workerCount,
	}

	// Start workers
	for i := 0; i < workerCount; i++ {
		go hw.worker()
	}

	return hw
}

// Add a hash job to the pool
func (hw *HashWorker) AddJob(file DuplicateFileInfo, mode FileHashType) {
	select {
	case hw.jobs <- hashJob{file: file, mode: mode}:
		hw.wg.Add(1)
	default:
		// Если канал заполнен, подождем немного
		time.Sleep(10 * time.Millisecond)
		hw.AddJob(file, mode) // Рекурсивная попытка
	}
}

// Wait for all jobs to complete and close job channel
func (hw *HashWorker) Wait() {
	hw.wg.Wait()
	close(hw.jobs)
	// Не закрываем канал результатов здесь,
	// он будет закрыт автоматически, когда все горутины обработают задания
}

// Worker goroutine for calculating hashes
func (hw *HashWorker) worker() {
	for job := range hw.jobs {
		var err error
		file := job.file

		// Add error recovery to prevent worker crashes
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("hash worker panicked: %v", r)
				}
			}()

			switch job.mode {
			case QuickHash:
				file.QuickHash, err = calculateQuickHash(file.Path)
			case FullHash:
				file.FullHash, err = calculateFullHash(file.Path)
			}
		}()

		// Try to send result with timeout
		select {
		case hw.results <- hashResult{file: file, err: err}:
			hw.wg.Done()
		case <-time.After(100 * time.Millisecond):
			// Если не удалось отправить за таймаут, пробуем снова
			go func(result hashResult) {
				hw.results <- result
				hw.wg.Done()
			}(hashResult{file: file, err: err})
		}
	}
}

// Calculate a quick hash of just the first few KB of a file
func calculateQuickHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("quick hash error: %w", err)
	}
	defer file.Close()

	// Only read the first N bytes
	buffer := make([]byte, QUICK_HASH_SIZE)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("quick hash read error: %w", err)
	}

	hash := md5.Sum(buffer[:n])
	return fmt.Sprintf("%x", hash), nil
}

// Calculate full file hash using streaming for large files
func calculateFullHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("full hash error: %w", err)
	}
	defer file.Close()

	// Get file info for size
	fileInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("file stat error: %w", err)
	}

	// For very large files, use a buffered approach
	hash := md5.New()

	// Use a 1MB buffer for efficient streaming
	const bufferSize = 1024 * 1024
	buffer := make([]byte, bufferSize)

	// If the file is very large (>100MB), show progress periodically
	showProgress := fileInfo.Size() > 100*1024*1024
	var totalRead int64

	for {
		n, err := file.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])

			if showProgress {
				totalRead += int64(n)
				if totalRead%(10*1024*1024) == 0 { // Show every 10MB
					progress := float64(totalRead) / float64(fileInfo.Size()) * 100
					fmt.Printf("\r  Hashing %s: %.1f%%", filepath.Base(filePath), progress)
				}
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			return "", fmt.Errorf("read error during hashing: %w", err)
		}
	}

	if showProgress {
		fmt.Print("\r                                                \r") // Clear progress line
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
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
