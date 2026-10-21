//go:build windows

package main

import (
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// Windows file attributes
const (
	FILE_ATTRIBUTE_READONLY            = 0x01
	FILE_ATTRIBUTE_HIDDEN              = 0x02
	FILE_ATTRIBUTE_SYSTEM              = 0x04
	FILE_ATTRIBUTE_DIRECTORY           = 0x10
	FILE_ATTRIBUTE_ARCHIVE             = 0x20
	FILE_ATTRIBUTE_DEVICE              = 0x40
	FILE_ATTRIBUTE_NORMAL              = 0x80
	FILE_ATTRIBUTE_TEMPORARY           = 0x100
	FILE_ATTRIBUTE_SPARSE_FILE         = 0x200
	FILE_ATTRIBUTE_REPARSE_POINT       = 0x400
	FILE_ATTRIBUTE_COMPRESSED          = 0x800
	FILE_ATTRIBUTE_OFFLINE             = 0x1000
	FILE_ATTRIBUTE_NOT_CONTENT_INDEXED = 0x2000
	FILE_ATTRIBUTE_ENCRYPTED           = 0x4000
)

func getFileInfo(path string, fullScan bool) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to get file information for '%s': %w", path, err)
	}

	if info.IsDir() {
		return FileInfo{}, fmt.Errorf("'%s' is a directory, not a file", path)
	}

	creationTime := getFileCreationTime(info)
	extension := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(extension)

	// Check if file is executable
	isExecutable := isExecutableFile(extension)

	// Get Windows file attributes
	attrs := getWindowsFileAttributes(path)

	return FileInfo{
		Path:         path,
		Size:         uint64(info.Size()),
		ModTime:      info.ModTime(),
		CreationTime: creationTime,
		Mode:         info.Mode(),
		Extension:    extension,
		MimeType:     mimeType,
		IsExecutable: isExecutable,
		IsHidden:     attrs&FILE_ATTRIBUTE_HIDDEN != 0,
		IsReadOnly:   attrs&FILE_ATTRIBUTE_READONLY != 0,
		IsSystem:     attrs&FILE_ATTRIBUTE_SYSTEM != 0,
		IsArchive:    attrs&FILE_ATTRIBUTE_ARCHIVE != 0,
		IsTemporary:  attrs&FILE_ATTRIBUTE_TEMPORARY != 0,
		IsCompressed: attrs&FILE_ATTRIBUTE_COMPRESSED != 0,
		IsEncrypted:  attrs&FILE_ATTRIBUTE_ENCRYPTED != 0,
	}, nil
}

func getFileCreationTime(info fs.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, stat.CreationTime.Nanoseconds())
	}
	return time.Time{}
}

func isExecutableFile(extension string) bool {
	executableExtensions := map[string]bool{
		".exe": true,
		".com": true,
		".bat": true,
		".cmd": true,
		".ps1": true,
		".vbs": true,
		".js":  true,
		".jar": true,
		".msi": true,
		".scr": true,
	}
	return executableExtensions[extension]
}

func getWindowsFileAttributes(path string) uint32 {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}

	attrs, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return 0
	}

	return attrs
}
