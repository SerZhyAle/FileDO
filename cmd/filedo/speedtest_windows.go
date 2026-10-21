//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The speed test, one implementation for device, folder and network targets.
// All three measure past the operating system's cache: the upload is written
// with FILE_FLAG_NO_BUFFERING|WRITE_THROUGH and the download read with
// FILE_FLAG_NO_BUFFERING. The device path used to copy through the cache and
// reported RAM speed for a 20 MB/s stick (SP-0026 CAP-09).

// speedUploadCopy and speedDownloadCopy are the two measured copies; variables
// so a test can prove every speed test goes through them.
var (
	speedUploadCopy   = copySpeedTestUpload
	speedDownloadCopy = copySpeedTestDownload
)

// createSpeedSource makes the local source file: in the current directory, as
// it always was, or in %TEMP% when the current directory takes no writes.
func createSpeedSource(name string, sizeMB int, showProgress bool) (string, error) {
	dirs := []string{}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	dirs = append(dirs, os.TempDir())
	var lastErr error
	for _, dir := range dirs {
		p := filepath.Join(dir, name)
		if err := createRandomFile(p, sizeMB, showProgress); err != nil {
			os.Remove(p)
			if errors.Is(err, errRunStopped) {
				return "", err
			}
			lastErr = err
			continue
		}
		return p, nil
	}
	return "", lastErr
}

// runSpeedTest measures one target. kind is "Device", "Folder" or "Network".
func runSpeedTest(kind, targetPath, sizeMBStr string, noDelete, shortFormat bool, logger *HistoryLogger) error {
	lower := strings.ToLower(kind)
	if logger != nil {
		logger.SetCommand(lower, targetPath, "speed")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("noDelete", noDelete)
		logger.SetParameter("shortFormat", shortFormat)
	}
	fail := func(err error) error {
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	sizeMB, err := parseSizeMB(sizeMBStr, 1, 10240)
	if err != nil {
		return fail(err)
	}

	if !shortFormat {
		fmt.Printf("%s Speed Test\n", kind)
		fmt.Printf("Target: %s\n", describeCapacityTarget(kind, targetPath))
		fmt.Printf("Test file size: %d MB\n\n", sizeMB)
		fmt.Printf("Step 1: Checking %s accessibility..\n", lower)
	}

	st, err := os.Stat(targetPath)
	if err != nil {
		return fail(fmt.Errorf("%s path is not accessible: %w", lower, err))
	}
	if !st.IsDir() {
		return fail(fmt.Errorf("path is not a directory: %s", targetPath))
	}
	if err := probeWritable(targetPath); err != nil {
		return fail(err)
	}

	if !shortFormat {
		fmt.Printf("✓ %s is accessible and writable\n\n", kind)
		fmt.Printf("Step 2: Creating test file (%d MB)..\n", sizeMB)
	}

	stamp := time.Now().Unix()
	startCreate := time.Now()
	localPath, err := createSpeedSource(fmt.Sprintf("speedtest_local_%d_%d.txt", sizeMB, stamp), sizeMB, !shortFormat)
	if err != nil {
		return fail(fmt.Errorf("failed to create test file: %w", err))
	}
	defer func() {
		if err := os.Remove(localPath); err != nil && !shortFormat {
			fmt.Printf("⚠ Warning: Could not remove local file: %v\n", err)
		}
	}()
	createDuration := time.Since(startCreate)

	targetFile := filepath.Join(targetPath, fmt.Sprintf("speedtest_%d_%d.txt", sizeMB, stamp))
	downloadPath := filepath.Join(filepath.Dir(localPath), fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, stamp))

	if !shortFormat {
		fmt.Printf("✓ Test file created in %s\n\n", formatDuration(createDuration))
		fmt.Printf("Step 3: Upload Speed Test - Copying file to the %s..\n", lower)
		fmt.Printf("Source: %s\n", localPath)
		fmt.Printf("Target: %s\n", targetFile)
		fmt.Printf("Mode: unbuffered write (FILE_FLAG_NO_BUFFERING|WRITE_THROUGH - bypasses OS page cache)\n")
	}

	startUpload := time.Now()
	bytesUploaded, err := speedUploadCopy(localPath, targetFile)
	if err != nil {
		os.Remove(targetFile)
		return fail(fmt.Errorf("failed to copy file to the %s: %w", lower, err))
	}
	uploadDuration := time.Since(startUpload)
	defer func() {
		if noDelete {
			return
		}
		if err := os.Remove(targetFile); err != nil && !shortFormat {
			fmt.Printf("⚠ Warning: Could not remove the %s's test file: %v\n", lower, err)
		}
	}()
	uploadSpeedMBps := speedMBps(bytesUploaded, uploadDuration)

	if !shortFormat {
		fmt.Printf("\n✓ File uploaded successfully\n")
		fmt.Printf("Upload completed in %s\n", formatDuration(uploadDuration))
		fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMBps*8)
		fmt.Printf("Step 4: Download Speed Test - Copying file from the %s..\n", lower)
		fmt.Printf("Source: %s\n", targetFile)
		fmt.Printf("Target: %s\n", downloadPath)
		fmt.Printf("Mode: unbuffered read (FILE_FLAG_NO_BUFFERING - bypasses OS page cache)\n")
	}

	startDownload := time.Now()
	bytesDownloaded, err := speedDownloadCopy(targetFile, downloadPath)
	if err != nil {
		os.Remove(downloadPath)
		return fail(fmt.Errorf("failed to copy file from the %s: %w", lower, err))
	}
	downloadDuration := time.Since(startDownload)
	defer func() {
		if err := os.Remove(downloadPath); err != nil && !shortFormat {
			fmt.Printf("⚠ Warning: Could not remove downloaded file: %v\n", err)
		}
	}()
	downloadSpeedMBps := speedMBps(bytesDownloaded, downloadDuration)

	if shortFormat {
		fmt.Printf("Upload completed in   %s, Speed: %6.1f MB/s (%6.1f Mbps)\n",
			formatDuration(uploadDuration), uploadSpeedMBps, uploadSpeedMBps*8)
		fmt.Printf("Download completed in %s, Speed: %6.1f MB/s (%6.1f Mbps)\n",
			formatDuration(downloadDuration), downloadSpeedMBps, downloadSpeedMBps*8)
	} else {
		fmt.Printf("\n✓ File downloaded successfully\n")
		fmt.Printf("Download completed in %s\n", formatDuration(downloadDuration))
		fmt.Printf("Download Speed: %.2f MB/s (%.2f Mbps)\n\n", downloadSpeedMBps, downloadSpeedMBps*8)
		fmt.Printf("Step 5: Cleaning up test files..\n")
		if noDelete {
			fmt.Printf("✓ %s test file kept: %s\n", kind, targetFile)
		} else {
			fmt.Printf("✓ Test files will be removed\n")
		}
		fmt.Printf("\nSpeed Test Summary:\n")
		fmt.Printf("File size: %d MB\n", sizeMB)
		fmt.Printf("Upload time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(uploadDuration), uploadSpeedMBps, uploadSpeedMBps*8)
		fmt.Printf("Download time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(downloadDuration), downloadSpeedMBps, downloadSpeedMBps*8)
	}

	runNumber("fileSizeMB", sizeMB)
	runNumber("uploadMBps", uploadSpeedMBps)
	runNumber("downloadMBps", downloadSpeedMBps)
	if logger != nil {
		logger.SetResult("fileSizeMB", sizeMB)
		logger.SetResult("uploadSpeedMBps", uploadSpeedMBps)
		logger.SetResult("downloadSpeedMBps", downloadSpeedMBps)
		logger.SetResult("uploadTimeSec", uploadDuration.Seconds())
		logger.SetResult("downloadTimeSec", downloadDuration.Seconds())
		logger.SetSuccess()
	}
	return nil
}

// speedMBps is bytes over a duration in MB/s; a zero duration is one
// nanosecond, so the number stays finite.
func speedMBps(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		d = time.Nanosecond
	}
	return float64(bytes) / float64(capMiB) / d.Seconds()
}
