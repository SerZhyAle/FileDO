package fileduplicates

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"filedo/fsx"

	"golang.org/x/sys/windows"
)

// fileBasicInfo is FILE_BASIC_INFO (winbase.h). x/sys/windows names the class
// but not the struct. The explicit tail keeps the size at 40 bytes on 386 too.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32
}

// filetimeToTime converts a FILETIME count (100 ns since 1601) to time.Time.
func filetimeToTime(ft int64) time.Time {
	f := windows.Filetime{LowDateTime: uint32(ft), HighDateTime: uint32(uint64(ft) >> 32)}
	return time.Unix(0, f.Nanoseconds())
}

// statTimes reads the creation and last access time the directory listing
// already carries (Win32FileAttributeData), so the walk costs no extra call.
func statTimes(info fs.FileInfo) (created, accessed time.Time) {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && d != nil {
		return time.Unix(0, d.CreationTime.Nanoseconds()), time.Unix(0, d.LastAccessTime.Nanoseconds())
	}
	return info.ModTime(), info.ModTime()
}

// identify fills in the object identity of file: volume serial and file index
// (one file under two names shares them) and the change time, which a write
// moves even when it restores the size and the modification time.
func identify(file *DuplicateFileInfo) error {
	name, err := windows.UTF16PtrFromString(fsx.LongPath(file.Path))
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: file.Path, Err: err}
	}
	defer windows.CloseHandle(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return &os.PathError{Op: "GetFileInformationByHandle", Path: file.Path, Err: err}
	}
	var basic fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return &os.PathError{Op: "GetFileInformationByHandleEx", Path: file.Path, Err: err}
	}

	file.VolSerial = info.VolumeSerialNumber
	file.FileIndex = uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	file.ChangeTime = filetimeToTime(basic.ChangeTime)
	file.CreatedTime = filetimeToTime(basic.CreationTime)
	file.identified = true
	return nil
}

// sameObject reports whether two existing paths name one file (B1 identity:
// volume serial + file ID, so a case variant, a second spelling and a hard
// link all count). A path that cannot be resolved is never the same.
func sameObject(a, b string) bool {
	same, err := fsx.SameFile(a, b)
	return err == nil && same
}

// forceCopyMove makes every move take the cross-volume path (copy, verify,
// delete). It exists for the tests, which have one volume.
var forceCopyMove = false

// isDestinationExists reports a no-replace move that found its name taken.
func isDestinationExists(err error) bool { return errors.Is(err, fsx.ErrDestinationExists) }

// moveNoReplace moves src to dst and never replaces an existing dst. On the
// same volume it is a rename without MOVEFILE_REPLACE_EXISTING; across volumes
// it is a copy through a partial file, a byte comparison of the copy against
// the source, and only then the removal of the source (DUP-04, SP-0027 B2).
// A destination that already exists yields fsx.ErrDestinationExists.
func moveNoReplace(ctx context.Context, src, dst string, stop func() bool) error {
	if !forceCopyMove {
		err := fsx.RenameNoReplace(src, dst)
		if err == nil || !errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) {
			return err
		}
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := fsx.CopyFile(ctx, src, dst, fsx.CopyOptions{}); err != nil {
		return err
	}
	same, err := sameContent(src, dst, stop)
	if err != nil || !same {
		os.Remove(dst)
		if err == nil {
			err = errors.New("the copy does not match the original")
		}
		return fmt.Errorf("copy of %s to %s did not verify: %w", src, dst, err)
	}
	if err := os.Remove(src); err != nil {
		// Leave things as they were: the original stays, the copy goes.
		os.Remove(dst)
		return fmt.Errorf("copied %s but could not remove it: %w", src, err)
	}
	return nil
}

// protectedPlace is a folder where deleting or moving duplicates needs an
// interactive confirmation, and which a scan skips unless it starts inside it.
type protectedPlace struct {
	path     string
	reason   string
	excluded bool // skipped by a scan that does not start inside it
}

// protectedPlaces is read from the environment on every call, which is also
// how the tests point it at a scratch folder.
func protectedPlaces() []protectedPlace {
	var places []protectedPlace
	seen := map[string]bool{}
	add := func(p, reason string, excluded bool) {
		if p == "" || seen[strings.ToLower(filepath.Clean(p))] {
			return
		}
		seen[strings.ToLower(filepath.Clean(p))] = true
		places = append(places, protectedPlace{path: p, reason: reason, excluded: excluded})
	}
	windir := os.Getenv("SystemRoot")
	if windir == "" {
		windir = os.Getenv("windir")
	}
	add(windir, "the Windows folder", true)
	for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		add(os.Getenv(v), "a Program Files folder", true)
	}
	add(os.TempDir(), "the system TEMP folder", false)
	return places
}

// relToVolume is id's final path below its volume root, with a leading
// separator: `\Windows\System32` for both `C:\Windows\System32` and
// `\\localhost\C$\Windows\System32`.
func relToVolume(id fsx.Identity) string {
	vol := strings.TrimRight(id.Volume, `\`)
	rel := id.Final
	if fsx.HasPrefixFold(rel, vol) {
		rel = rel[len(vol):]
	}
	return `\` + strings.TrimLeft(rel, `\`)
}

// identityWithin reports whether child is parent itself or lies below it, on
// the same volume, however either was spelled.
func identityWithin(child, parent fsx.Identity) bool {
	if child.VolSerial != parent.VolSerial {
		return false
	}
	return fsx.CanonicalWithin(relToVolume(child), relToVolume(parent))
}

// resolvedPlace is a protected place with its identity looked up.
type resolvedPlace struct {
	protectedPlace
	id fsx.Identity
}

// resolvePlaces looks every protected place up once; a place that does not
// exist on this machine is simply not there to protect.
func resolvePlaces() []resolvedPlace {
	var out []resolvedPlace
	for _, place := range protectedPlaces() {
		id, err := fsx.IdentityOf(place.path)
		if err != nil {
			continue
		}
		out = append(out, resolvedPlace{protectedPlace: place, id: id})
	}
	return out
}

// classifyProtected reports whether deleting or moving duplicates found at p
// needs an interactive confirmation, and why: the root of a drive or share,
// the Windows folder or anything in it, a Program Files folder or anything in
// it, or the system TEMP folder itself (DUP-06, the SP-0027 root classifier).
func classifyProtected(p string) (bool, string) {
	id, err := fsx.IdentityOf(p)
	if err != nil {
		return false, ""
	}
	if id.IsVolumeRoot() {
		if strings.HasPrefix(id.Volume, `\\`) && !strings.HasPrefix(id.Volume, `\\?\`) {
			return true, "the root of a network share"
		}
		return true, "the root of a drive"
	}
	for _, place := range resolvePlaces() {
		if place.excluded {
			if identityWithin(id, place.id) {
				return true, place.reason
			}
		} else if id.SameObject(place.id) {
			return true, place.reason
		}
	}
	return false, ""
}

// systemFolderClassifier returns a test for "is this file inside the Windows
// folder or a Program Files folder", with the places resolved once for all
// the files of a list.
func systemFolderClassifier() func(p string) (bool, string) {
	places := resolvePlaces()
	return func(p string) (bool, string) {
		id, err := fsx.IdentityOf(p)
		if err != nil {
			return false, ""
		}
		for _, place := range places {
			if place.excluded && identityWithin(id, place.id) {
				return true, place.reason
			}
		}
		return false, ""
	}
}

// scanExclusions names, in the spelling the walk of root produces, the
// protected folders a scan skips: the Windows folder and Program Files, when
// they lie below root. A scan that starts inside one of them is the user's
// explicit choice and skips nothing.
func scanExclusions(root string) []string {
	rootID, err := fsx.IdentityOf(root)
	if err != nil {
		return nil
	}
	rootRel := strings.TrimRight(relToVolume(rootID), `\`)
	var out []string
	for _, place := range resolvePlaces() {
		if !place.excluded || !identityWithin(place.id, rootID) || identityWithin(rootID, place.id) {
			continue
		}
		tail := relToVolume(place.id)[len(rootRel):]
		out = append(out, filepath.Join(root, tail))
	}
	return out
}
