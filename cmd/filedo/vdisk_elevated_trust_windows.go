//go:build windows

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// What the elevated step may trust (SP-0064 release-queue row 5: AUD-31-F4,
// AUD-32-F3, T3-F6). The request file lives in the user's state root, which any
// process of the same user can rewrite while the consent prompt is open. The
// command line the consent covers cannot be rewritten, so it carries the
// SHA-256 of the request bytes the parent wrote; the elevated half reads the
// file once, refuses unless the bytes hash to that value, and parses only
// those bytes. Every field (port, IQN, serial, noscan path, image path,
// NeverHeld, FormatOnly, StateDir, the task's SID) is thereby the parent's.
// The task's SID must resolve to a user account, and the state root is used
// only when it is an existing directory that is not a reparse point. Neither
// is compared with the elevated token, so over-the-shoulder elevation (a
// standard user typing an administrator's credentials) keeps working.

// vdRequestDigest is the hex SHA-256 of request bytes, as the command line
// carries it.
func vdRequestDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// vdReadBoundRequest reads the request file once, removes it (it may carry the
// CHAP secret), and parses it only when its bytes hash to want. A mismatch
// means the file is not the one the person consented to run: nothing is done.
func vdReadBoundRequest(reqPath, want string) (vdRequest, error) {
	var req vdRequest
	b, err := os.ReadFile(reqPath)
	os.Remove(reqPath)
	if err != nil {
		return req, err
	}
	if want == "" || !strings.EqualFold(vdRequestDigest(b), want) {
		return req, vdUsagef("the request file changed after FileDO wrote it (its SHA-256 does not match the one on the elevated command line); nothing was done")
	}
	if err := json.Unmarshal(b, &req); err != nil {
		return req, err
	}
	return req, nil
}

// vdTaskOwnerSID is the account an automatic-mount task is created for: the
// request's TaskSID, which the digest binds to the consent, so it is the SID
// the non-elevated parent read from its own token. It is not compared with the
// elevated token: under over-the-shoulder elevation (a standard user typing an
// administrator's credentials) the elevated token is the administrator's, and
// the task still belongs to the user who asked. It must be a well-formed SID
// that resolves to a user account; anything else is refused.
func vdTaskOwnerSID(requested string) (string, error) {
	sid, err := windows.StringToSid(requested)
	if err != nil {
		return "", vdUsagef("the task's owner %q is not a valid SID; no task was created", requested)
	}
	_, _, use, err := sid.LookupAccount("")
	if err != nil {
		return "", vdUsagef("the task's owner %s is not an account on this computer (%v); no task was created", requested, err)
	}
	if use != windows.SidTypeUser {
		return "", vdUsagef("the task's owner %s is not a user account; no task was created", requested)
	}
	return sid.String(), nil
}

// vdCheckStateDir decides whether the elevated step may use dir as its state
// root (T3-F6): it must be an absolute path to an existing directory that is
// not a reparse point. The value itself is the parent's (the digest binds it).
// Its owner is not checked: under over-the-shoulder elevation the folder
// belongs to the standard user, not to the elevated administrator. The caller
// always creates its state root before it starts the step, so nothing is
// created here and no ancestor needs checking.
func vdCheckStateDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return vdUsagef("the state folder %s is not an absolute path; nothing was done", dir)
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return vdUsagef("the state folder %s is not a valid path; nothing was done", dir)
	}
	attr, err := windows.GetFileAttributes(p)
	if err != nil {
		return vdUsagef("the state folder %s cannot be read (%v); nothing was done", dir, err)
	}
	if attr&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return vdUsagef("the state folder %s is a link to another place; nothing was done", dir)
	}
	if attr&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return vdUsagef("the state folder %s is not a folder; nothing was done", dir)
	}
	return nil
}

// vdWriteLockedTemp writes data to a new file in dir and returns it held open
// for reading with FILE_SHARE_READ only, so that until release is called no
// process can write, replace, rename or delete it (AUD-31-F4 (a): the task XML
// handed to schtasks sits in the user's %TEMP%). The file is created with
// CREATE_NEW, written and closed, then reopened without write or delete
// sharing and read back through that handle: a change made in the gap between
// the two opens is caught by the comparison, and none can follow it. The
// write handle cannot stay open itself, because a reader such as schtasks
// that denies write sharing could not open a file someone holds for writing.
func vdWriteLockedTemp(dir, pattern string, data []byte) (path string, release func(), err error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", nil, err
	}
	path = filepath.Join(dir, strings.Replace(pattern, "*", hex.EncodeToString(rnd[:]), 1))
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", nil, err
	}
	w, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.CREATE_NEW,
		windows.FILE_ATTRIBUTE_TEMPORARY|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", nil, err
	}
	var n uint32
	werr := windows.WriteFile(w, data, &n, nil)
	if werr == nil && int(n) != len(data) {
		werr = fmt.Errorf("short write to %s", path)
	}
	if cerr := windows.CloseHandle(w); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return "", nil, werr
	}
	r, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		os.Remove(path)
		return "", nil, err
	}
	release = func() {
		windows.CloseHandle(r)
		os.Remove(path)
	}
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(r, &fi); err != nil {
		release()
		return "", nil, err
	}
	if fi.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || fi.NumberOfLinks != 1 {
		windows.CloseHandle(r) // not ours to delete
		return "", nil, fmt.Errorf("%s was replaced while it was written; nothing was run", path)
	}
	got := make([]byte, len(data)+1)
	var total uint32
	for int(total) < len(got) {
		var m uint32
		if err := windows.ReadFile(r, got[total:], &m, nil); err != nil || m == 0 {
			break
		}
		total += m
	}
	if !bytes.Equal(got[:total], data) {
		release()
		return "", nil, fmt.Errorf("%s changed while it was written; nothing was run", path)
	}
	return path, release, nil
}
