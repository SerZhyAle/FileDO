package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// hardLinkedError is a wipe refused because the file has more than one name.
// An overwrite in place writes into the data stream, and every hard link to
// the file shares that stream: the other names would keep existing and hold
// random bytes (SP-0034 AUD-03-F1). The original is left as it was.
type hardLinkedError struct {
	Path  string
	Links uint32
}

func (e *hardLinkedError) Error() string {
	return fmt.Sprintf("%s has %d hard links: overwriting it would also destroy the other %d name(s), so it is left as it is (remove the extra links, or use del)",
		e.Path, e.Links, e.Links-1)
}

// hardLinkCount is the number of names of the file behind an open handle.
func hardLinkCount(f *os.File) (uint32, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return 0, &os.PathError{Op: "GetFileInformationByHandle", Path: f.Name(), Err: err}
	}
	return info.NumberOfLinks, nil
}

// wipeLinkRefusal is the question every wipe asks before it prompts: does the
// file have another name? A non-nil error means it could not be answered, and
// the wipe must not go on. wipeFileInPlace asks again on the handle it
// overwrites, so a link made after this answer is still caught.
func wipeLinkRefusal(path string) (*hardLinkedError, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := hardLinkCount(f)
	if err != nil {
		return nil, err
	}
	if n > 1 {
		return &hardLinkedError{Path: path, Links: n}, nil
	}
	return nil, nil
}

// openForWipe opens the file an overwrite in place is about to write with no
// sharing at all, so the wipe never runs over a file another program holds
// open. A mounted disk container is held by its block server, which shares it
// for reading and writing: a wipe that shared it too overwrote the live disk
// and failed only at the remove (SP-0064 T2-F1). A file in use is refused
// with nothing written.
func openForWipe(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == windows.ERROR_SHARING_VIOLATION || err == windows.ERROR_LOCK_VIOLATION {
		return nil, fmt.Errorf("%s is open in another program (a mounted disk container is), nothing was overwritten - close it or unmount it first: %w", path, err)
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
