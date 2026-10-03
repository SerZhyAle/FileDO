//go:build windows

package fmsworker

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrUntrustedServer is the refusal to talk to a pipe whose server process FileDO could not tie to
// the FMS worker. It also satisfies errors.Is(err, ErrWorkerUnavailable): to the caller the worker is
// not there, and nothing - above all no password - was sent (DISK-SHARE 18, AUD-86-F3, AUD-84-F5).
var ErrUntrustedServer = errors.New("the worker pipe is served by a process FileDO cannot verify")

const (
	workerImageName = "fms-share-worker.exe"
	// workerServiceName is the Server edition's service (DISK-SHARE 18.1).
	workerServiceName = "FastMediaSorterCompanionSFTP"
)

// ServerVerifier checks the process behind a freshly dialled pipe before the first byte is written.
// credential is true for a request that carries a disk password.
type ServerVerifier func(conn net.Conn, credential bool) error

// identityOps are the machine facts the check reads; the tests replace them one by one.
type identityOps struct {
	pipePID      func(conn net.Conn) (uint32, error)
	servicePID   func() (uint32, error) // 0 with a nil error: the service is not installed or not running
	userSID      func(pid uint32) (string, error)
	selfSID      func() (string, error)
	image        func(pid uint32) (string, error)
	programFiles func() []string
}

var machineIdentity = identityOps{
	pipePID:      pipeServerPID,
	servicePID:   workerServicePID,
	userSID:      processUserSID,
	selfSID:      currentUserSID,
	image:        processImage,
	programFiles: programFilesRoots,
}

// verifyPipeServer is the production ServerVerifier.
func verifyPipeServer(conn net.Conn, credential bool) error {
	return checkPipeServer(conn, credential, machineIdentity)
}

// checkPipeServer accepts exactly two kinds of server, and decides without asking the server which it
// is - a squatter can claim any mode in GetStatus:
//
//   - the process the service manager runs as the FMS worker service (its process id equals the
//     service's), which only an administrator can install; or
//   - a process of the account that is running FileDO (the User edition's session worker), which is
//     inside that account's own trust boundary.
//
// Anything else - another account, a system process that is not the service, a process whose account
// cannot be read - is refused. A request that carries a password also needs the server image to be the
// FMS worker, and under Program Files when it is the service.
func checkPipeServer(conn net.Conn, credential bool, ops identityOps) error {
	pid, err := ops.pipePID(conn)
	if err != nil {
		return untrusted("the pipe's server process could not be read: %v", err)
	}
	isService := false
	if sp, e := ops.servicePID(); e == nil && sp != 0 && sp == pid {
		isService = true
	}
	if !isService {
		sid, e := ops.userSID(pid)
		if e != nil {
			return untrusted("the account of the pipe's server process could not be read: %v", e)
		}
		self, e := ops.selfSID()
		if e != nil {
			return untrusted("the current account could not be read: %v", e)
		}
		if !strings.EqualFold(sid, self) {
			return untrusted("the pipe is served by another account")
		}
	}
	if credential {
		img, e := ops.image(pid)
		if e != nil {
			return untrusted("the image of the pipe's server process could not be read: %v", e)
		}
		if !strings.EqualFold(filepath.Base(img), workerImageName) {
			return untrusted("the pipe's server is not the FMS worker")
		}
		if isService && !underAny(img, ops.programFiles()) {
			return untrusted("the service worker does not run from Program Files")
		}
	}
	return nil
}

func untrusted(format string, a ...any) error {
	return fmt.Errorf("%w: %w: %s", ErrWorkerUnavailable, ErrUntrustedServer, fmt.Sprintf(format, a...))
}

// underAny reports whether path is inside one of the roots (a root is a directory, never a prefix of a
// sibling's name).
func underAny(path string, roots []string) bool {
	p := strings.ToLower(filepath.Clean(path))
	for _, r := range roots {
		if r == "" {
			continue
		}
		root := strings.ToLower(filepath.Clean(r))
		if strings.HasPrefix(p, root+`\`) {
			return true
		}
	}
	return false
}

func programFilesRoots() []string {
	return []string{os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")}
}

func pipeServerPID(conn net.Conn) (uint32, error) {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, errors.New("the connection exposes no handle")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return 0, err
	}
	if pid == 0 {
		return 0, errors.New("the server process id is zero")
	}
	return pid, nil
}

// workerServicePID asks the service manager for the process id of the FMS worker service: 0 when the
// service is absent or stopped (the Server edition is not installed or is down).
func workerServicePID() (uint32, error) {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(m)
	name, err := windows.UTF16PtrFromString(workerServiceName)
	if err != nil {
		return 0, err
	}
	s, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return 0, nil
		}
		return 0, err
	}
	defer windows.CloseServiceHandle(s)
	var st windows.SERVICE_STATUS_PROCESS
	var need uint32
	if err = windows.QueryServiceStatusEx(s, windows.SC_STATUS_PROCESS_INFO, (*byte)(unsafe.Pointer(&st)), uint32(unsafe.Sizeof(st)), &need); err != nil {
		return 0, err
	}
	return st.ProcessId, nil
}

func processUserSID(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err = windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return "", err
	}
	defer tok.Close()
	return tokenSID(tok)
}

func currentUserSID() (string, error) {
	tok := windows.GetCurrentProcessToken()
	return tokenSID(tok)
}

func tokenSID(tok windows.Token) (string, error) {
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

func processImage(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err = windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}
