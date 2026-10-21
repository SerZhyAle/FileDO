package main

import (
	"context"
	"errors"
	"fmt"
	"syscall"
)

// ioErrorClass is theme T3 of SP-0023: the one classifier that decides what an
// I/O error says about the target.
//
// The rule it encodes: an environmental error - access denied, write
// protection, a device or share that is not there, a full disk, a sharing
// violation - says nothing about the media, so a run that met one could not
// verify (exit 2). Only a device-level error, the class a failing or lying
// controller produces, may become a defect (exit 1). A stop is neither.
type ioErrorClass int

const (
	ioErrNone ioErrorClass = iota
	// ioErrStopped: the run was asked to stop, or the operation was cancelled.
	ioErrStopped
	// ioErrEnvironment: the target could not be reached, written or read for a
	// reason that is not the media's fault.
	ioErrEnvironment
	// ioErrDevice: the device itself failed the I/O.
	ioErrDevice
	// ioErrOther: anything else - still "could not verify", never a defect.
	ioErrOther
)

func (c ioErrorClass) String() string {
	switch c {
	case ioErrNone:
		return "none"
	case ioErrStopped:
		return "stopped"
	case ioErrEnvironment:
		return "environment"
	case ioErrDevice:
		return "device"
	default:
		return "other"
	}
}

// Win32 error codes the classifier names. Most are in syscall or
// golang.org/x/sys/windows under the same names; they are spelled out here so
// the table below reads as one list.
const (
	errFileNotFound          syscall.Errno = 2
	errPathNotFound          syscall.Errno = 3
	errAccessDenied          syscall.Errno = 5
	errWriteProtect          syscall.Errno = 19
	errNotReady              syscall.Errno = 21
	errCRC                   syscall.Errno = 23
	errSeek                  syscall.Errno = 25
	errSectorNotFound        syscall.Errno = 27
	errWriteFault            syscall.Errno = 29
	errReadFault             syscall.Errno = 30
	errGenFailure            syscall.Errno = 31
	errSharingViolation      syscall.Errno = 32
	errLockViolation         syscall.Errno = 33
	errHandleDiskFull        syscall.Errno = 39
	errBadNetPath            syscall.Errno = 53
	errNetworkBusy           syscall.Errno = 54
	errDevNotExist           syscall.Errno = 55
	errUnexpNetErr           syscall.Errno = 59
	errNetnameDeleted        syscall.Errno = 64
	errNetworkAccessDenied   syscall.Errno = 65
	errBadNetName            syscall.Errno = 67
	errDiskFull              syscall.Errno = 112
	errInvalidName           syscall.Errno = 123
	errFileTooLarge          syscall.Errno = 223
	errDeviceHardwareError   syscall.Errno = 483
	errOperationAborted      syscall.Errno = 995
	errMediaChanged          syscall.Errno = 1110
	errNoMediaInDrive        syscall.Errno = 1112
	errIODevice              syscall.Errno = 1117
	errDiskOperationFailed   syscall.Errno = 1127
	errDeviceNotConnected    syscall.Errno = 1167
	errFileCorrupt           syscall.Errno = 1392
	errDiskCorrupt           syscall.Errno = 1393
	errNotEnoughQuota        syscall.Errno = 1816
	errCantAccessFile        syscall.Errno = 1920
	errNetworkUnreachable    syscall.Errno = 1231
	errHostUnreachable       syscall.Errno = 1232
	errConnectionAborted     syscall.Errno = 1236
	errUserMappedFileSection syscall.Errno = 1224
)

var deviceErrnos = map[syscall.Errno]bool{
	errCRC: true, errSeek: true, errSectorNotFound: true, errWriteFault: true,
	errReadFault: true, errGenFailure: true, errDeviceHardwareError: true,
	errIODevice: true, errDiskOperationFailed: true, errFileCorrupt: true,
	errDiskCorrupt: true,
}

var environmentErrnos = map[syscall.Errno]bool{
	errFileNotFound: true, errPathNotFound: true, errAccessDenied: true,
	errWriteProtect: true, errNotReady: true, errSharingViolation: true,
	errLockViolation: true, errHandleDiskFull: true, errBadNetPath: true,
	errNetworkBusy: true, errDevNotExist: true, errUnexpNetErr: true,
	errNetnameDeleted: true, errNetworkAccessDenied: true, errBadNetName: true,
	errDiskFull: true, errInvalidName: true, errFileTooLarge: true,
	errMediaChanged: true, errNoMediaInDrive: true, errDeviceNotConnected: true,
	errNotEnoughQuota: true, errCantAccessFile: true, errNetworkUnreachable: true,
	errHostUnreachable: true, errConnectionAborted: true,
	errUserMappedFileSection: true,
}

// classifyIOError sorts an error into the classes above.
func classifyIOError(err error) ioErrorClass {
	if err == nil {
		return ioErrNone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, errRunStopped) {
		return ioErrStopped
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ioErrEnvironment
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch {
		case deviceErrnos[errno]:
			return ioErrDevice
		case environmentErrnos[errno]:
			return ioErrEnvironment
		case errno == errOperationAborted:
			// A handle closed under a pending call - which is what a stop's
			// cleanup does. Without a stop it is just an aborted operation.
			if runStopRequested() {
				return ioErrStopped
			}
			return ioErrOther
		}
	}
	if runStopRequested() {
		return ioErrStopped
	}
	return ioErrOther
}

// isDeviceIOError: the only class that may ever be judged a defect.
func isDeviceIOError(err error) bool { return classifyIOError(err) == ioErrDevice }

// isEnvironmentalIOError: the target could not be reached; nothing proven.
func isEnvironmentalIOError(err error) bool { return classifyIOError(err) == ioErrEnvironment }

// isDiskFullError: the file system ran out of space. A controller that lies
// about its size cannot cause this - the file system believes the lie - so it
// is never evidence of fake capacity on its own.
func isDiskFullError(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errDiskFull || errno == errHandleDiskFull)
}

// errRunStopped is the typed "the user asked to stop" an operation returns
// when it noticed the stop itself rather than through a context.
var errRunStopped = errors.New("stopped by request")

// ioJudgement wraps an I/O error for the verdict: a device-level error becomes
// a defect, a stop stays a stop, and everything else is "could not verify".
// The message is the caller's; the class decides only the wrapping.
func ioJudgement(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	msg := fmt.Sprintf(format, args...)
	if classifyIOError(err) == ioErrDevice {
		return fmt.Errorf("%w: %s: %v", errDefect, msg, err)
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// isStopError reports whether err is the run being asked to stop - a stop
// request, a cancelled context, or an operation aborted by a stop's cleanup -
// rather than a failure. The copy and the capacity engines both ask it.
func isStopError(err error) bool {
	return err != nil && classifyIOError(err) == ioErrStopped
}
