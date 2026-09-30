package vdisk

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
)

// refVolume is the deliberately naive container every property and crash
// test compares against: a map of logical clusters, no encryption, no map, no
// file. It is obviously correct by inspection, and it is never compiled into
// the product.
type refVolume struct {
	size    int64
	cluster int64
	data    map[int64][]byte
}

func newRef(size, cluster int64) *refVolume {
	return &refVolume{size: size, cluster: cluster, data: map[int64][]byte{}}
}

func (r *refVolume) writeAt(p []byte, off int64) {
	for len(p) > 0 {
		c, in := off/r.cluster, off%r.cluster
		if r.data[c] == nil {
			r.data[c] = make([]byte, r.cluster)
		}
		n := copy(r.data[c][in:], p)
		p, off = p[n:], off+int64(n)
	}
}

func (r *refVolume) readAt(p []byte, off int64) {
	for len(p) > 0 {
		c, in := off/r.cluster, off%r.cluster
		n := int(min(int64(len(p)), r.cluster-in))
		if d := r.data[c]; d != nil {
			copy(p[:n], d[in:])
		} else {
			clear(p[:n])
		}
		p, off = p[n:], off+int64(n)
	}
}

func (r *refVolume) image() []byte {
	b := make([]byte, r.size)
	r.readAt(b, 0)
	return b
}

func (r *refVolume) grow(size int64) { r.size = size }

func (r *refVolume) clone() *refVolume {
	c := newRef(r.size, r.cluster)
	for k, v := range r.data {
		c.data[k] = append([]byte(nil), v...)
	}
	return c
}

// memBacking is a backing in memory: what the property tests run on, so that
// comparing the whole image after every step stays affordable.
type memBacking struct {
	mu   sync.Mutex
	data []byte
}

func (m *memBacking) ReadAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *memBacking) WriteAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if end := off + int64(len(p)); end > int64(len(m.data)) {
		m.data = append(m.data, make([]byte, end-int64(len(m.data)))...)
	}
	return copy(m.data[off:], p), nil
}

func (m *memBacking) Truncate(size int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if size <= int64(len(m.data)) {
		m.data = m.data[:size]
	} else {
		m.data = append(m.data, make([]byte, size-int64(len(m.data)))...)
	}
	return nil
}

func (m *memBacking) Sync() error { return nil }
func (m *memBacking) Size() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.data)), nil
}
func (m *memBacking) Close() error { return nil }

func (m *memBacking) snapshot() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.data...)
}

var errCrash = errors.New("simulated power loss")

// failBacking wraps a memBacking and dies deterministically: at the Nth write
// (optionally landing half of it, a torn write), at the Nth flush, or at a
// truncate. After it dies every call fails, as a process that lost power
// makes no more calls. durable is the image as of the last successful flush:
// what a file system that loses every unflushed write would leave.
type failBacking struct {
	inner        *memBacking
	durable      []byte
	writes       int
	syncs        int
	failWrite    int
	torn         bool
	failSync     int
	failTruncate bool
	dead         bool
}

func newFailBacking(inner *memBacking) *failBacking {
	return &failBacking{inner: inner, durable: inner.snapshot()}
}

func (f *failBacking) ReadAt(p []byte, off int64) (int, error) {
	if f.dead {
		return 0, errCrash
	}
	return f.inner.ReadAt(p, off)
}

func (f *failBacking) WriteAt(p []byte, off int64) (int, error) {
	if f.dead {
		return 0, errCrash
	}
	f.writes++
	if f.writes == f.failWrite {
		f.dead = true
		if f.torn && len(p) > 1 {
			f.inner.WriteAt(p[:len(p)/2], off)
		}
		return 0, errCrash
	}
	return f.inner.WriteAt(p, off)
}

func (f *failBacking) Truncate(size int64) error {
	if f.dead {
		return errCrash
	}
	if f.failTruncate {
		f.dead = true
		return errCrash
	}
	return f.inner.Truncate(size)
}

func (f *failBacking) Sync() error {
	if f.dead {
		return errCrash
	}
	f.syncs++
	if f.syncs == f.failSync {
		f.dead = true
		return errCrash
	}
	f.durable = f.inner.snapshot()
	return nil
}

func (f *failBacking) Size() (int64, error) {
	if f.dead {
		return 0, errCrash
	}
	return f.inner.Size()
}

func (f *failBacking) Close() error { return nil }

// sameVolume compares a container with the reference cluster by cluster over
// every cluster either side holds, which is the whole image for any volume in
// which the rest reads as zeros - and is how a multi-gigabyte sparse volume is
// compared without reading gigabytes of zeros.
func sameVolume(t testing.TB, c *Container, r *refVolume) {
	t.Helper()
	if got := c.Info().LogicalSize; got != r.size {
		t.Fatalf("volume size %d, reference %d", got, r.size)
	}
	cs := r.cluster
	check := map[int64]bool{}
	for cl := range r.data {
		check[cl] = true
	}
	for cl, e := range c.entries {
		if e != sentinel {
			check[int64(cl)] = true
		}
	}
	check[0], check[(r.size-1)/cs] = true, true
	for cl := range check {
		n := min(cs, r.size-cl*cs)
		want, got := make([]byte, n), make([]byte, n)
		r.readAt(want, cl*cs)
		if _, err := c.ReadAt(got, cl*cs); err != nil {
			t.Fatalf("read cluster %d: %v", cl, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("cluster %d differs from the reference", cl)
		}
	}
}
