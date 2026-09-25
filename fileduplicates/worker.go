package fileduplicates

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
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
	// Register the job BEFORE sending it so a worker can never call wg.Done()
	// (after processing) before this Add runs, which would drive the WaitGroup
	// counter negative and panic.
	hw.wg.Add(1)
	// Use a loop instead of recursion to avoid stack overflow
	for {
		select {
		case hw.jobs <- hashJob{file: file, mode: mode}:
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

		if hw.stop != nil && hw.stop() {
			// A stopped run drains its queue without reading another byte.
			result.err = ErrStopped
		} else if job.mode == QuickHash {
			hash, err := calculateQuickHash(job.file.Path)
			if err != nil {
				result.err = err
			} else {
				result.file.QuickHash = hash
			}
		} else {
			hash, err := calculateFullHash(job.file.Path, hw.stop)
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

	hasher := sha256.New()
	buffer := make([]byte, QUICK_HASH_SIZE)

	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("failed to read file for quick hash: %w", err)
	}

	hasher.Write(buffer[:n])
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// hashChunk is how much of a file is read between two looks at the stop
// request, so a stop is honoured inside a large file too.
const hashChunk = 1 << 20

// Calculate a hash of the entire file
func calculateFullHash(filePath string, stop func() bool) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for full hash: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	buf := make([]byte, hashChunk)
	for {
		if stop != nil && stop() {
			return "", ErrStopped
		}
		n, rerr := file.Read(buf)
		if n > 0 {
			hasher.Write(buf[:n])
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("failed to read file for full hash: %w", rerr)
		}
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// Get file information for duplicate detection
func GetFileInfo(path string) (DuplicateFileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return DuplicateFileInfo{}, err
	}
	return fileInfoFrom(path, info), nil
}

// fileInfoFrom builds the record for one file from what the walk or a Stat
// already returned. The creation time is the one "keep newest/oldest" compares
// (DUP-09): an Explorer copy keeps the source's modification time, so the
// modification times of every copy tie.
func fileInfoFrom(path string, info fs.FileInfo) DuplicateFileInfo {
	created, accessed := statTimes(info)
	return DuplicateFileInfo{
		Path:        path,
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		CreatedTime: created,
		LastAccess:  accessed,
	}
}
