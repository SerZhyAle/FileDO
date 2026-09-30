//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows iSCSI initiator, through its documented discovery API
// (iscsidsc.dll). iscsicli.exe is manifested requireAdministrator and cannot
// start from a standard token at all (SP-0004 S0 section 6.3), so the product
// calls the API the tool is built on. Every call here needs an elevated token:
// a standard token is refused on the read calls too (S0 section 6.3.2).

var (
	iscsidsc             = windows.NewLazySystemDLL("iscsidsc.dll")
	procAddSendPortal    = iscsidsc.NewProc("AddIScsiSendTargetPortalW")
	procRemoveSendPortal = iscsidsc.NewProc("RemoveIScsiSendTargetPortalW")
	procLoginTarget      = iscsidsc.NewProc("LoginIScsiTargetW")
	procLogoutTarget     = iscsidsc.NewProc("LogoutIScsiTarget")
	procGetSessionList   = iscsidsc.NewProc("GetIScsiSessionListW")
)

const iscsiAllPorts = 0xFFFFFFFF

// Discovery API status codes this code reacts to (iscsierr.h).
const (
	isdscServiceNotRunning = 0xEFFF003E
	isdscDeviceBusy        = 0xEFFF0040 // "a device on that session is currently being used"
	isdscInvalidSessionID  = 0xEFFF001C
)

type iscsiPortal struct {
	SymbolicName [256]uint16
	Address      [256]uint16
	Socket       uint16
}

func loopbackPortal(port int) *iscsiPortal {
	p := &iscsiPortal{Socket: uint16(port)}
	a, _ := windows.UTF16FromString("127.0.0.1")
	copy(p.Address[:], a)
	return p
}

// iscsiSessionID is ISCSI_UNIQUE_SESSION_ID.
type iscsiSessionID struct {
	AdapterUnique   uint64
	AdapterSpecific uint64
}

func (s iscsiSessionID) String() string {
	return fmt.Sprintf("%016X-%016X", s.AdapterUnique, s.AdapterSpecific)
}

func parseSessionID(s string) (iscsiSessionID, bool) {
	var id iscsiSessionID
	_, err := fmt.Sscanf(s, "%016X-%016X", &id.AdapterUnique, &id.AdapterSpecific)
	return id, err == nil
}

// iscsiLoginOptions is ISCSI_LOGIN_OPTIONS; Go's alignment of the two
// pointers matches the C layout on both 386 and amd64.
type iscsiLoginOptions struct {
	Version              uint32
	InformationSpecified uint32
	LoginFlags           uint32
	AuthType             int32
	HeaderDigest         int32
	DataDigest           int32
	MaximumConnections   uint32
	DefaultTime2Wait     uint32
	DefaultTime2Retain   uint32
	UsernameLength       uint32
	PasswordLength       uint32
	Username             *byte
	Password             *byte
}

const (
	iscsiInfoPassword = 0x40
	iscsiInfoAuthType = 0x80
	iscsiChapAuthType = 1
)

// iscsiSessionInfo is ISCSI_SESSION_INFOW.
type iscsiSessionInfo struct {
	SessionID       iscsiSessionID
	InitiatorName   *uint16
	TargetNodeName  *uint16
	TargetName      *uint16
	ISID            [6]byte
	TSID            [2]byte
	ConnectionCount uint32
	Connections     uintptr
}

// iscsiError is a failed discovery API call, with the API's own text.
type iscsiError struct {
	call   string
	status uint32
}

func (e *iscsiError) Error() string {
	return fmt.Sprintf("%s failed: %s", e.call, iscsiStatusText(e.status))
}

func iscsiStatusText(code uint32) string {
	buf := make([]uint16, 512)
	var n uint32
	if h, err := windows.LoadLibraryEx("iscsidsc.dll", 0, windows.LOAD_LIBRARY_AS_DATAFILE); err == nil {
		n, _ = windows.FormatMessage(windows.FORMAT_MESSAGE_FROM_HMODULE|windows.FORMAT_MESSAGE_IGNORE_INSERTS, uintptr(h), code, 0, buf, nil)
		windows.FreeLibrary(h)
	}
	if n == 0 {
		n, _ = windows.FormatMessage(windows.FORMAT_MESSAGE_FROM_SYSTEM|windows.FORMAT_MESSAGE_IGNORE_INSERTS, 0, code, 0, buf, nil)
	}
	msg := strings.TrimSpace(windows.UTF16ToString(buf[:n]))
	if msg == "" {
		msg = "no message text"
	}
	return fmt.Sprintf("0x%08X, %s", code, msg)
}

func iscsiCheck(call string, st uintptr) error {
	if st == 0 {
		return nil
	}
	return &iscsiError{call: call, status: uint32(st)}
}

func iscsiStatusOf(err error) uint32 {
	if e, ok := err.(*iscsiError); ok {
		return e.status
	}
	return 0
}

// securityFlags is the ISCSI_SECURITY_FLAGS argument (a ULONGLONG): one
// stack slot on amd64, two on 386.
func securityFlags() []uintptr {
	if unsafe.Sizeof(uintptr(0)) == 4 {
		return []uintptr{0, 0}
	}
	return []uintptr{0}
}

func iscsiLoad() error {
	if err := iscsidsc.Load(); err != nil {
		return fmt.Errorf("the iSCSI initiator library is not available: %w", err)
	}
	return nil
}

// iscsiAddPortal adds 127.0.0.1:port as a SendTargets portal, which also runs
// discovery against it. Discovery needs no authentication.
func iscsiAddPortal(port int) error {
	args := []uintptr{0, iscsiAllPorts, 0}
	args = append(args, securityFlags()...)
	args = append(args, uintptr(unsafe.Pointer(loopbackPortal(port))))
	st, _, _ := procAddSendPortal.Call(args...)
	return iscsiCheck("AddIScsiSendTargetPortalW", st)
}

func iscsiRemovePortal(port int) error {
	st, _, _ := procRemoveSendPortal.Call(0, iscsiAllPorts, uintptr(unsafe.Pointer(loopbackPortal(port))))
	return iscsiCheck("RemoveIScsiSendTargetPortalW", st)
}

// iscsiLogin logs in to iqn at 127.0.0.1:port with one-way CHAP, not
// persistently: a reboot never reconnects a container (plan S3, open question
// 3). The secret travels only in this call.
func iscsiLogin(iqn string, port int, secret string) (iscsiSessionID, error) {
	var sid, cid iscsiSessionID
	name, err := windows.UTF16PtrFromString(iqn)
	if err != nil {
		return sid, err
	}
	pw := []byte(secret)
	opts := &iscsiLoginOptions{
		InformationSpecified: iscsiInfoAuthType | iscsiInfoPassword,
		AuthType:             iscsiChapAuthType,
		PasswordLength:       uint32(len(pw)),
		Password:             &pw[0],
	}
	args := []uintptr{
		uintptr(unsafe.Pointer(name)), 0, 0, iscsiAllPorts,
		uintptr(unsafe.Pointer(loopbackPortal(port))),
	}
	args = append(args, securityFlags()...)
	args = append(args, 0, uintptr(unsafe.Pointer(opts)), 0, 0, 0,
		uintptr(unsafe.Pointer(&sid)), uintptr(unsafe.Pointer(&cid)))
	st, _, _ := procLoginTarget.Call(args...)
	clear(pw)
	return sid, iscsiCheck("LoginIScsiTargetW", st)
}

func iscsiLogout(sid iscsiSessionID) error {
	st, _, _ := procLogoutTarget.Call(uintptr(unsafe.Pointer(&sid)))
	return iscsiCheck("LogoutIScsiTarget", st)
}

// iscsiSessionsFor lists the sessions whose target is iqn - the way to find a
// session left behind by a block server that died.
func iscsiSessionsFor(iqn string) ([]iscsiSessionID, error) {
	var size, count uint32
	st, _, _ := procGetSessionList.Call(uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if st != 0 && syscall.Errno(st) != windows.ERROR_INSUFFICIENT_BUFFER {
		return nil, iscsiCheck("GetIScsiSessionListW", st)
	}
	if size == 0 || count == 0 {
		return nil, nil
	}
	buf := make([]uint64, (size+7)/8+1)
	st, _, _ = procGetSessionList.Call(uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if err := iscsiCheck("GetIScsiSessionListW", st); err != nil {
		return nil, err
	}
	infos := unsafe.Slice((*iscsiSessionInfo)(unsafe.Pointer(&buf[0])), count)
	var out []iscsiSessionID
	for _, s := range infos {
		for _, name := range []*uint16{s.TargetNodeName, s.TargetName} {
			if name != nil && strings.EqualFold(windows.UTF16PtrToString(name), iqn) {
				out = append(out, s.SessionID)
				break
			}
		}
	}
	return out, nil
}
