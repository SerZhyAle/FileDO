// Package fsx holds the filesystem primitives every destructive or
// data-bearing verb shares: path identity (B1 of SP-0027, theme T2 of
// SP-0023) and atomic, non-replacing writes (B2, theme T4). It is Windows
// code, used by cmd/filedo and by fileduplicates, so the rules are written
// once for both.
package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// FileID holds the filesystem-level volume serial number and 64-bit file index,
// plus basic metadata (size, modTime, isDir, isRegular) obtained in a single handle query.
type FileID struct {
	VolSerial uint32
	FileIndex uint64
	Size      int64
	ModTime   time.Time
	IsDir     bool
	IsRegular bool
}

// FileIDOf opens p with FILE_READ_ATTRIBUTES and reads ByHandleFileInformation in one call,
// without querying the volume path or canonical final path name.
func FileIDOf(p string) (FileID, error) {
	var fid FileID
	if strings.TrimSpace(p) == "" {
		return fid, errors.New("empty path")
	}
	h, err := openForIdentity(p)
	if err != nil {
		return fid, err
	}
	defer windows.CloseHandle(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return fid, &os.PathError{Op: "GetFileInformationByHandle", Path: p, Err: err}
	}
	fid.VolSerial = info.VolumeSerialNumber
	fid.FileIndex = uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	fid.Size = int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	fid.ModTime = time.Unix(0, info.LastWriteTime.Nanoseconds())
	fid.IsDir = info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	fid.IsRegular = info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) == 0
	return fid, nil
}

// Identity is what a path names, as the filesystem sees it, rather than as it
// was spelled.
//
// Every decision about "the same file", "inside", "a root" or "the system
// drive" used to be made on strings, and every one of them was bypassed by a
// second spelling - `d:\photos\` against `D:\Photos`, a subst letter, a
// junction, `\\localhost\D$`, `\\?\UNC\..`, `\\?\GLOBALROOT\..`. The identity
// below is the answer the filesystem gives after all of those are resolved:
// the final path of an open handle, the volume that holds it, and the volume
// serial number plus file ID, which two names of one file always share.
type Identity struct {
	// Abs is filepath.Abs of the input as typed.
	Abs string
	// Final is GetFinalPathNameByHandle of the object (links followed), in DOS
	// form: `C:\..` for a local volume, `\\server\share\..` for a share, and
	// `\\?\Volume{..}\..` for a volume with no drive letter.
	Final string
	// Volume is the root of the volume holding the object, as
	// GetVolumePathName reports it, with a trailing separator.
	Volume string
	// VolSerial and FileIndex identify the object on its volume.
	VolSerial uint32
	FileIndex uint64
	IsDir     bool
}

// IdentityOf resolves p. The object must exist.
func IdentityOf(p string) (Identity, error) {
	var id Identity
	if strings.TrimSpace(p) == "" {
		return id, errors.New("empty path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return id, err
	}
	id.Abs = abs

	h, err := openForIdentity(p)
	if err != nil {
		return id, err
	}
	defer windows.CloseHandle(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return id, &os.PathError{Op: "GetFileInformationByHandle", Path: p, Err: err}
	}
	id.VolSerial = info.VolumeSerialNumber
	id.FileIndex = uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	id.IsDir = info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0

	final, err := finalPathOfHandle(h)
	if err != nil {
		return id, &os.PathError{Op: "GetFinalPathNameByHandle", Path: p, Err: err}
	}
	id.Final = final

	vol, err := VolumePathName(final)
	if err != nil {
		return id, &os.PathError{Op: "GetVolumePathName", Path: final, Err: err}
	}
	id.Volume = vol
	return id, nil
}

// openForIdentity opens a file or a directory for attribute queries only,
// sharing everything, following reparse points to their target.
func openForIdentity(p string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(LongPath(p))
	if err != nil {
		return windows.InvalidHandle, err
	}
	h, err := windows.CreateFile(name,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return windows.InvalidHandle, &os.PathError{Op: "open", Path: p, Err: err}
	}
	return h, nil
}

// LongPath makes an absolute path safe for the wide Win32 calls without
// changing what it names. A path that already carries a `\\?\` or `\\.\`
// prefix is passed through untouched.
func LongPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// GetFinalPathNameByHandle flags (fileapi.h); x/sys/windows does not name them.
const (
	fileNameNormalized = 0x0
	volumeNameDOS      = 0x0
	volumeNameGUID     = 0x1
)

// finalPathOfHandle is GetFinalPathNameByHandle in DOS form, with the
// extended-length prefixes removed so the result compares with ordinary paths.
func finalPathOfHandle(h windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameDOS)
		if err != nil {
			// A volume without a drive letter has no DOS name; fall back to
			// its GUID name, which is still a stable identity.
			n, err = windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameGUID)
			if err != nil {
				return "", err
			}
		}
		if int(n) < len(buf) {
			return stripExtendedPrefix(windows.UTF16ToString(buf[:n])), nil
		}
		buf = make([]uint16, n+1)
	}
}

// stripExtendedPrefix turns `\\?\C:\x` into `C:\x` and `\\?\UNC\srv\share`
// into `\\srv\share`; a `\\?\Volume{..}` path keeps its prefix, because
// without it the name means nothing.
func stripExtendedPrefix(p string) string {
	switch {
	case HasPrefixFold(p, `\\?\UNC\`):
		return `\\` + p[len(`\\?\UNC\`):]
	case HasPrefixFold(p, `\\?\Volume{`):
		return p
	case strings.HasPrefix(p, `\\?\`) && len(p) >= 6 && p[5] == ':':
		return p[4:]
	}
	return p
}

// VolumePathName is GetVolumePathName: the root of the volume (or mount
// point) that holds p, with a trailing separator.
func VolumePathName(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(LongPath(p))
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if err := windows.GetVolumePathName(name, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}
	v := stripExtendedPrefix(windows.UTF16ToString(buf))
	if !strings.HasSuffix(v, `\`) {
		v += `\`
	}
	return v, nil
}

// SameObject reports whether two identities name one file or directory.
func (id Identity) SameObject(other Identity) bool {
	return id.VolSerial == other.VolSerial && id.FileIndex == other.FileIndex
}

// IsVolumeRoot reports whether the object is the root of its volume or of a
// mount point: the case every "refuse a drive or share root" rule means.
func (id Identity) IsVolumeRoot() bool {
	return strings.EqualFold(ensureTrailingSep(id.Final), id.Volume)
}

// SameFile answers "do these two paths name the same object?" when both
// exist. A missing path is never the same as anything.
func SameFile(a, b string) (bool, error) {
	ia, err := IdentityOf(a)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	ib, err := IdentityOf(b)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return ia.SameObject(ib), nil
}

// resolveIdentity is the identity of p even when p does not exist yet: that of
// its nearest existing ancestor, whose Final has the missing tail appended.
// found is false when no ancestor could be opened at all (Final is then the
// absolute path as typed, and the volume fields are empty). The object fields
// - FileIndex, IsDir - describe the ancestor, not the tail.
func resolveIdentity(p string) (id Identity, found bool, err error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return Identity{}, false, err
	}
	abs = filepath.Clean(abs)
	var tail []string
	cur := abs
	for {
		got, err := IdentityOf(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				got.Final = filepath.Join(got.Final, tail[i])
			}
			got.Abs = abs
			return got, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return Identity{}, false, err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return Identity{Abs: abs, Final: abs}, false, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// Resolve is the canonical spelling of p even when p does not exist yet:
// the final path of its nearest existing ancestor with the missing tail
// appended. A copy target is usually a folder the copy has still to create.
// It is a spelling: two paths that name one place through different volume
// names (`C:\Data` and `\\localhost\C$\Data`) resolve to different strings, so
// a containment decision goes through Within, Overlap or LiesWithin instead.
func Resolve(p string) (string, error) {
	id, _, err := resolveIdentity(p)
	if err != nil {
		return "", err
	}
	return id.Final, nil
}

// RelToVolume is the object's final path below its volume root, with a leading
// separator: `\Windows\System32` for both `C:\Windows\System32` and
// `\\localhost\C$\Windows\System32`. The volume root itself is a lone `\`.
func (id Identity) RelToVolume() string {
	vol := strings.TrimRight(id.Volume, `\`)
	rel := id.Final
	if vol != "" && HasPrefixFold(rel, vol) {
		rel = rel[len(vol):]
	}
	return `\` + strings.TrimLeft(rel, `\`)
}

// LiesWithin reports whether id is parent itself or lies below it on the same
// volume, however either was spelled: the volume serial number says which
// volume, RelToVolume says where on it. A path whose volume could not be
// determined (no serial) is never matched here; the spelling test covers it.
func (id Identity) LiesWithin(parent Identity) bool {
	if id.VolSerial == 0 || id.VolSerial != parent.VolSerial {
		return false
	}
	return CanonicalWithin(id.RelToVolume(), parent.RelToVolume())
}

// withinResolved is the containment decision on two resolved paths: identity
// first (the volume and the path below its root), then the canonical spelling,
// which can only add a "yes" for a path no volume could be found for.
func withinResolved(child Identity, childFound bool, parent Identity, parentFound bool) bool {
	if childFound && parentFound && child.LiesWithin(parent) {
		return true
	}
	return CanonicalWithin(child.Final, parent.Final)
}

// Within reports whether child is parent itself or lies below it, on the same
// volume however either was spelled (a drive letter, an administrative or
// local share, a mapped drive, a mount point, a junction). Neither needs to
// exist: the nearest existing ancestor of each is what is compared.
func Within(child, parent string) (bool, error) {
	c, cFound, err := resolveIdentity(child)
	if err != nil {
		return false, err
	}
	p, pFound, err := resolveIdentity(parent)
	if err != nil {
		return false, err
	}
	return withinResolved(c, cFound, p, pFound), nil
}

// CanonicalWithin compares two canonical paths case-insensitively, the way
// NTFS and SMB name them by default.
func CanonicalWithin(child, parent string) bool {
	c := strings.TrimRight(child, `\/`)
	p := strings.TrimRight(parent, `\/`)
	if strings.EqualFold(c, p) {
		return true
	}
	return HasPrefixFold(c, p+`\`)
}

// OverlapError is the one message for a source and target that are the same
// object or nest inside each other - the shape of COPY-10, CHK-01 and COPY-01.
func OverlapError(verb, source, target string) error {
	return fmt.Errorf("%s: source %q and target %q are the same location or one contains the other; refusing", verb, source, target)
}

// Overlap reports whether a and b are the same location or either lies
// within the other. It is the refusal test for every two-path verb that
// writes to or deletes from its second path.
func Overlap(a, b string) (bool, error) {
	ia, aFound, err := resolveIdentity(a)
	if err != nil {
		return false, err
	}
	ib, bFound, err := resolveIdentity(b)
	if err != nil {
		return false, err
	}
	if withinResolved(ia, aFound, ib, bFound) || withinResolved(ib, bFound, ia, aFound) {
		return true, nil
	}
	// Two spellings the final path does not unify (a hard link to a file,
	// or a second share name of one folder) still share a file ID.
	return SameFile(a, b)
}

func HasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func ensureTrailingSep(p string) string {
	if strings.HasSuffix(p, `\`) {
		return p
	}
	return p + `\`
}
