package capacitytest

import (
	"context"
)

// Global variable to control verification mode
var DeepVerificationMode = false

// Buffer optimization cache
var optimalBuffers = make(map[string]int)

// Tester interface defines the operations needed for fake capacity testing
type Tester interface {
	// GetTestInfo returns the test type name and target path for display
	GetTestInfo() (testType, targetPath string)

	// GetAvailableSpace returns the available space in bytes for testing
	GetAvailableSpace() (int64, error)

	// CreateTestFile creates a test file with the given size and returns the file path
	CreateTestFile(fileName string, fileSize int64) (filePath string, err error)

	// CreateTestFileContext creates a test file with context for cancellation support
	// If not implemented, should return the same as CreateTestFile
	CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error)

	// VerifyTestFile verifies that a test file contains the expected header
	VerifyTestFile(filePath string) error

	// CleanupTestFile removes a test file
	CleanupTestFile(filePath string) error

	// GetCleanupCommand returns the command to clean test files manually
	GetCleanupCommand() string
}

// TestResult holds the results of a fake capacity test
type TestResult struct {
	TestPassed        bool
	FilesCreated      int
	TotalDataBytes    int64
	BaselineSpeedMBps float64
	AverageSpeedMBps  float64
	MinSpeedMBps      float64
	MaxSpeedMBps      float64
	FailureReason     string
	CreatedFiles      []string
}

// External interfaces for compatibility with main package
type HistoryLogger interface {
	SetCommand(command, target, action string)
	SetParameter(name string, value interface{})
	SetError(err error)
	SetResult(name string, value interface{})
}

type InterruptHandler interface {
	IsCancelled() bool
	Context() context.Context
	CheckContext() error
}

type ProgressTracker interface {
	Update(currentItems int64, currentBytes int64)
	PrintProgress(operation string)
}
