package main

import (
	"errors"
	"net"
	"os"
	"syscall"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// appCmdPipeName is the command pipe of the running filedo_win.exe: the third
// name shape of APP-ACTIVATION rule 3, `<App>_<purpose>_<userSid>_<sessionId>`,
// because the single-instance mutex is session-local and a pipe name is
// machine-global. The first instance admits the current user only and never the
// network (APP-COMMANDS rule 15); filedo_win.exe builds the same name
// (AppCommands.vb). Once shipped the name is a frozen anchor.
func appCmdPipeName() string {
	sid := "unknown"
	if tok := windows.GetCurrentProcessToken(); tok != 0 {
		if u, err := tok.GetTokenUser(); err == nil && u.User.Sid != nil {
			sid = u.User.Sid.String()
		}
	}
	var session uint32
	_ = windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session)
	return "FileDO_Commands_" + sid + "_" + itoaU32(session)
}

func itoaU32(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func appCmdDefaultPipePath() string {
	return `\\.\pipe\` + appCmdPipeName()
}

// appCmdDial connects inside the delivery bound of rule 8 (1 s). A pipe that is
// not there is no first instance; one that stays taken past the bound is busy
// (rule 10: one command at a time); one that refuses this caller belongs to an
// elevated first instance.
func appCmdDial(path string) (net.Conn, appCmdState) {
	timeout := appCmdDeliveryBound
	conn, err := winio.DialPipe(path, &timeout)
	if err == nil {
		return conn, appCmdAnswered
	}
	switch {
	case errors.Is(err, winio.ErrTimeout):
		return nil, appCmdBusy
	case errors.Is(err, windows.ERROR_ACCESS_DENIED), errors.Is(err, os.ErrPermission):
		return nil, appCmdElevated
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND), errors.Is(err, os.ErrNotExist):
		return nil, appCmdNotRunning
	}
	// Anything else - a pipe closing under us - is nobody to ask right now.
	return nil, appCmdNotRunning
}

var procAllowSetForegroundWindow = syscall.NewLazyDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowForeground lets the first instance take the foreground for this caller's
// gesture: `show` brings its window forward, and Windows gives the foreground
// only to a process the foreground process allows (APP-ACTIVATION rule 5).
func allowForeground() {
	const asfwAny = ^uintptr(0) // ASFW_ANY, (DWORD)-1
	if procAllowSetForegroundWindow.Find() == nil {
		_, _, _ = procAllowSetForegroundWindow.Call(asfwAny)
	}
}
