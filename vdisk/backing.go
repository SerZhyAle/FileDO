package vdisk

// backing is the store under the container: a file on disk here, a memory
// buffer that saves back to one later (SP-0004 S4). The container is the only
// caller, and it addresses the backing in file offsets; no layer above the
// container knows the backing exists.
type backing interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	Truncate(size int64) error
	Sync() error
	Size() (int64, error)
	Close() error
}
