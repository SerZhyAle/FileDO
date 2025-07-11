package fileduplicates

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Create a new hash worker pool
func NewHashWorker(workerCount int) *HashWorker {
	if workerCount <= 0 {
		workerCount = MAX_WORKERS
	}

	hw := &HashWorker{
		jobs:        make(chan hashJob, workerCount*4),    // Increase buffer size
		results:     make(chan hashResult, workerCount*4), // Increase buffer size
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
	// Use a loop instead of recursion to avoid stack overflow
	for {
		select {
		case hw.jobs <- hashJob{file: file, mode: mode}:
			hw.wg.Add(1)
			return
		default:
			// If channel is full, wait a little bit
			time.Sleep(10 * time.Millisecond)
			// Continue the loop and try again
		}
	}
}

// Wait for all jobs to complete and close channels
func (hw *HashWorker) Wait() {
	hw.wg.Wait()
	close(hw.jobs)
	close(hw.results)
}

// Worker goroutine to process hash jobs
func (hw *HashWorker) worker() {
	for job := range hw.jobs {
		var result hashResult
		result.file = job.file

		if job.mode == QuickHash {
			hash, err := calculateQuickHash(job.file.Path)
			if err != nil {
				result.err = err
			} else {
				result.file.QuickHash = hash
			}
		} else {
			hash, err := calculateFullHash(job.file.Path)
			if err != nil {
				result.err = err
			} else {
				result.file.FullHash = hash
			}
		}

		// Send the result to the results channel, blocking if necessary
		// This ensures we never lose results even if the channel is temporarily full
		hw.results <- result
		hw.wg.Done()
	}
}

// Calculate a quick hash of just the first few KB of a file
func calculateQuickHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for quick hash: %w", err)
	}
	defer file.Close()

	hasher := md5.New()
	buffer := make([]byte, QUICK_HASH_SIZE)

	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file for quick hash: %w", err)
	}

	hasher.Write(buffer[:n])
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// Calculate a hash of the entire file
func calculateFullHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for full hash: %w", err)
	}
	defer file.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to read file for full hash: %w", err)
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// Load hash cache from disk
func LoadHashCache() (*HashCache, error) {
	cache := &HashCache{
		Entries: make(map[string]CacheEntry),
	}

	data, err := os.ReadFile(HASH_CACHE_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			return cache, nil // Not an error if file doesn't exist
		}
		return cache, fmt.Errorf("failed to read hash cache file: %w", err)
	}

	if err = json.Unmarshal(data, &cache.Entries); err != nil {
		return cache, fmt.Errorf("failed to parse hash cache file: %w", err)
	}

	return cache, nil
}

// Get a hash from cache or calculate it
func (cache *HashCache) GetHash(file DuplicateFileInfo, hashType FileHashType) (string, error) {
	cacheKey := fmt.Sprintf("%s:%d", file.Path, file.Size)

	cache.mutex.RLock()
	entry, exists := cache.Entries[cacheKey]
	cache.mutex.RUnlock()

	// Check if we have a valid cache entry
	if exists {
		if hashType == QuickHash && entry.QuickHash != "" {
			return entry.QuickHash, nil
		}
		if hashType == FullHash && entry.FullHash != "" {
			return entry.FullHash, nil
		}
	}

	// Need to calculate the hash
	var hash string
	var err error

	if hashType == QuickHash {
		hash, err = calculateQuickHash(file.Path)
	} else {
		hash, err = calculateFullHash(file.Path)
	}

	if err != nil {
		return "", err
	}

	// Update cache
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if !exists {
		entry = CacheEntry{
			Path:     file.Path,
			Size:     file.Size,
			LastSeen: time.Now(),
		}
	}

	if hashType == QuickHash {
		entry.QuickHash = hash
	} else {
		entry.FullHash = hash
	}

	cache.Entries[cacheKey] = entry
	return hash, nil
}

// Get file information for duplicate detection
func GetFileInfo(path string) (DuplicateFileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return DuplicateFileInfo{}, err
	}

	fileInfo := DuplicateFileInfo{
		Path:       path,
		Size:       info.Size(),
		ModTime:    info.ModTime(),
		IsOriginal: false,
	}

	// Get create time and access time on Windows
	// This implementation varies by platform
	fileInfo.CreatedTime = info.ModTime() // Default to mod time for non-Windows
	fileInfo.LastAccess = info.ModTime()  // Default to mod time for non-Windows

	return fileInfo, nil
}
