package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

// TestClassifyIOError is the T3 table: only device-level errors may become a
// defect; environmental ones are "could not verify"; a cancelled context is a
// stop.
func TestClassifyIOError(t *testing.T) {
	saved := globalInterruptHandler
	globalInterruptHandler = nil
	defer func() { globalInterruptHandler = saved }()

	wrap := func(e error) error { return &os.PathError{Op: "write", Path: `E:\FILL_001.tmp`, Err: e} }
	cases := []struct {
		err  error
		want ioErrorClass
	}{
		{nil, ioErrNone},
		{context.Canceled, ioErrStopped},
		{fmt.Errorf("copy: %w", context.Canceled), ioErrStopped},
		{errRunStopped, ioErrStopped},
		{context.DeadlineExceeded, ioErrEnvironment},
		{wrap(errAccessDenied), ioErrEnvironment},
		{wrap(errWriteProtect), ioErrEnvironment},
		{wrap(errNotReady), ioErrEnvironment},
		{wrap(errDeviceNotConnected), ioErrEnvironment},
		{wrap(errNetnameDeleted), ioErrEnvironment},
		{wrap(errDiskFull), ioErrEnvironment},
		{wrap(errFileTooLarge), ioErrEnvironment},
		{wrap(errSharingViolation), ioErrEnvironment},
		{wrap(errFileNotFound), ioErrEnvironment},
		{wrap(errIODevice), ioErrDevice},
		{wrap(errCRC), ioErrDevice},
		{wrap(errWriteFault), ioErrDevice},
		{wrap(errReadFault), ioErrDevice},
		{wrap(errGenFailure), ioErrDevice},
		{wrap(errDeviceHardwareError), ioErrDevice},
		{errors.New("something else"), ioErrOther},
		{wrap(errOperationAborted), ioErrOther},
	}
	for _, c := range cases {
		if got := classifyIOError(c.err); got != c.want {
			t.Errorf("classifyIOError(%v) = %v, want %v", c.err, got, c.want)
		}
	}

	if !isDiskFullError(wrap(errDiskFull)) || isDiskFullError(wrap(errIODevice)) {
		t.Error("isDiskFullError misclassifies")
	}
	if err := ioJudgement(wrap(errCRC), "reading file 3"); !errors.Is(err, errDefect) {
		t.Errorf("a CRC error must judge as a defect: %v", err)
	}
	for _, e := range []error{wrap(errAccessDenied), wrap(errDiskFull), context.Canceled} {
		if err := ioJudgement(e, "writing file 3"); errors.Is(err, errDefect) {
			t.Errorf("%v must never judge as a defect: %v", e, err)
		}
	}
}
