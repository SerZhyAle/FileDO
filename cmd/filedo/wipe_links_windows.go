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
