package vdisk

import (
	"os"
)

// fileBacking is a container file. Sparse without asking the file system for
// it: a plain container allocates clusters on first write and appends them,
// so the file holds the header, the slots, both map copies and the clusters
// written - a 20 GiB volume nobody wrote to is a file of a few hundred KiB,
// on NTFS, exFAT and FAT32 alike. The only gap ever left unwritten is the
// alignment gap before data_offset (under 1 MiB, or one cluster).
type fileBacking struct {
	f      *os.File
	unlock func()
}

// createFile makes a new container file and never replaces an existing one
// (FDD-BEHAVIOUR section 6 item 1).
func createFile(path string) (*fileBacking, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, usagef("%s already exists; a container is never written over an existing file", path)
		}
		return nil, ioErr(err)
	}
	unlock, err := lockFile(f, true)
	if err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return &fileBacking{f: f, unlock: unlock}, nil
}

// openFile opens an existing container file. A writer takes the exclusive
// lock; a reader takes the shared one, so a reader never sees a container a
// writer in another process is changing, and a second writer is refused as
// busy. Inspect takes neither: it reads the header only and says what the
// clean marker says.
func openFile(path string, write bool, lock bool) (*fileBacking, error) {
	flag := os.O_RDONLY
	if write {
		flag = os.O_RDWR
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, ioErr(err)
	}
	unlock := func() {}
	if lock {
		if unlock, err = lockFile(f, write); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &fileBacking{f: f, unlock: unlock}, nil
}

func (b *fileBacking) ReadAt(p []byte, off int64) (int, error)  { return b.f.ReadAt(p, off) }
func (b *fileBacking) WriteAt(p []byte, off int64) (int, error) { return b.f.WriteAt(p, off) }
func (b *fileBacking) Truncate(size int64) error                { return b.f.Truncate(size) }
func (b *fileBacking) Sync() error                              { return b.f.Sync() }

func (b *fileBacking) Size() (int64, error) {
	fi, err := b.f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (b *fileBacking) Close() error {
	b.unlock()
	return b.f.Close()
}
