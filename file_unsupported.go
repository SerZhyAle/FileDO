//go:build !windows

package main

import (
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func getFileInfo(path string, fullScan bool) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to get file information for '%s': %w", path, err)
	}

	if info.IsDir() {
		return FileInfo{}, fmt.Errorf("'%s' is a directory, not a file", path)
	}

	extension := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(extension)

	// Check if file is executable
	isExecutable := isExecutableFile(extension, info.Mode())

	// Check if file is hidden (Unix-style hidden files start with .)
	isHidden := isHiddenFile(path)

	return FileInfo{
		Path:         path,
		Size:         uint64(info.Size()),
		ModTime:      info.ModTime(),
		CreationTime: time.Time{}, // Creation time not easily available on Unix systems
		Mode:         info.Mode(),
		Extension:    extension,
		MimeType:     mimeType,
		IsExecutable: isExecutable,
		IsHidden:     isHidden,
		IsReadOnly:   false, // Unix doesn't have Windows-style readonly attribute
		IsSystem:     false, // Unix doesn't have Windows-style system attribute
		IsArchive:    false, // Unix doesn't have Windows-style archive attribute
		IsTemporary:  false, // Unix doesn't have Windows-style temporary attribute
		IsCompressed: false, // Unix doesn't have Windows-style compressed attribute
		IsEncrypted:  false, // Unix doesn't have Windows-style encrypted attribute
	}, nil
}

func isExecutableFile(extension string, mode fs.FileMode) bool {
	// Check if file has execute permission
	if mode&0111 != 0 {
		return true
	}

	// Also check for common executable extensions
	executableExtensions := map[string]bool{
		".sh":  true,
		".py":  true,
		".pl":  true,
		".rb":  true,
		".js":  true,
		".jar": true,
	}
	return executableExtensions[extension]
}

func isHiddenFile(path string) bool {
	fileName := filepath.Base(path)
	return strings.HasPrefix(fileName, ".")
}
