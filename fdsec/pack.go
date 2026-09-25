package fdsec

import (
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/blake2b"
)

// Params are the per-container layout parameters a writer chooses
// (FDSEC-FORMAT.md sections 4, 6). A zero field takes its default; the chosen
// values are recorded in the masked header and a reader always obeys the file.
// The KDF work factor is not here: it belongs to the format version, because a
// masked header cannot carry the parameters of its own mask (section 6).
type Params struct {
	ClusterAlignment uint32
	ChunkSize        uint32
}

func DefaultParams() Params {
	return Params{
		ClusterAlignment: DefaultClusterAlignment,
		ChunkSize:        DefaultChunkSize,
	}
}

func (p Params) withDefaults() (Params, error) {
	if p.ClusterAlignment == 0 {
		p.ClusterAlignment = DefaultClusterAlignment
	}
	if p.ChunkSize == 0 {
		p.ChunkSize = DefaultChunkSize
	}
	switch {
	case !isPow2(p.ClusterAlignment) || p.ClusterAlignment < 512 || p.ClusterAlignment > 1<<21:
		return p, fmt.Errorf("fdsec: cluster alignment %d must be a power of two in [512, 2097152]", p.ClusterAlignment)
	case p.ChunkSize < p.ClusterAlignment || p.ChunkSize%p.ClusterAlignment != 0:
		return p, fmt.Errorf("fdsec: chunk size %d must be a multiple of cluster alignment %d", p.ChunkSize, p.ClusterAlignment)
	}
	return p, nil
}

// Info reports what a pack produced.
type Info struct {
	HeaderDigest [digestSize]byte
	Size         int64 // real size N of the original
	Chunks       int64
	TotalLen     int64 // total container length, cluster-aligned
	Params       Params
}

// StreamOption tunes a Pack or Unpack call, or one of the file-level calls
// built on them: WithProgress for the progress line on large files, WithStop
// for the caller's stop request, AllowReplace for an overwrite the user chose.
type StreamOption func(*streamOpts)

type streamOpts struct {
	progress func(done, total int64)
	stop     func() bool
	replace  bool
}

// WithProgress registers a callback invoked at chunk boundaries with the
// plaintext bytes processed so far and the total. It must not block.
func WithProgress(fn func(done, total int64)) StreamOption {
	return func(o *streamOpts) { o.progress = fn }
}

// WithStop registers the caller's stop request. It is asked at every chunk
// boundary (and while the source is digested); when it answers true the call
// returns ErrStopped and produces nothing complete. It must not block.
func WithStop(fn func() bool) StreamOption {
	return func(o *streamOpts) { o.stop = fn }
}

// AllowReplace lets a file-level pack (PackFile, PackFileSuite2,
// PackTreeFile) replace an existing destination - the overwrite the user
// chose for that exact path. The old destination is replaced only by the
// final rename, after the new container was read back in full; until then it
// is untouched, so a failed pack never costs the old file (FDSEC-03).
func AllowReplace() StreamOption {
	return func(o *streamOpts) { o.replace = true }
}

// stopped reports whether the caller asked the stream to end.
func (o streamOpts) stopped() bool { return o.stop != nil && o.stop() }

// stopReader returns ErrStopped from the first Read after a stop request, so
// a whole-file pass (the digest of pass 1) is interruptible too.
type stopReader struct {
	r  io.Reader
	so streamOpts
}

func (s stopReader) Read(p []byte) (int, error) {
	if s.so.stopped() {
		return 0, ErrStopped
	}
	return s.r.Read(p)
}

func newStreamOpts(opts []StreamOption) streamOpts {
	var o streamOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Pack streams src into a complete suite-1 container written to dst,
// implementing the writer algorithm of FDSEC-FORMAT.md section 10: digest the
// original in one pass, then write the masked head, sealed metadata, alignment
// pad, sealed chunks and tail pad. src is read twice (it must be seekable);
// meta.Size must equal the number of bytes src yields. The empty credential is
// accepted and produces obfuscation with no secrecy - callers must say so at
// the point of entry.
func Pack(dst io.Writer, src io.ReadSeeker, meta Metadata, cred Credential, p Params, opts ...StreamOption) (Info, error) {
	var info Info
	so := newStreamOpts(opts)
	p, err := p.withDefaults()
	if err != nil {
		return info, err
	}
	info.Params = p
	if meta.Size < 0 {
		return info, fmt.Errorf("fdsec: negative metadata size %d", meta.Size)
	}
	if meta.EncodedAt.IsZero() {
		meta.EncodedAt = time.Now().UTC()
	}

	// Pass 1: the original's digest, and a size check against the caller's claim.
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return info, fmt.Errorf("fdsec: seek source: %w", err)
	}
	dh, err := blake2b.New256(nil)
	if err != nil {
		return info, err
	}
	n, err := io.Copy(dh, stopReader{r: src, so: so})
	if errors.Is(err, ErrStopped) {
		return info, ErrStopped
	}
	if err != nil {
		return info, fmt.Errorf("fdsec: digest source: %w", err)
	}
	if n != meta.Size {
		return info, fmt.Errorf("fdsec: source yielded %d bytes, metadata says %d", n, meta.Size)
	}
	var digest [digestSize]byte
	copy(digest[:], dh.Sum(nil))
	info.Size = n

	salt, err := randBytes(saltSize)
	if err != nil {
		return info, fmt.Errorf("fdsec: salt: %w", err)
	}
	fileKey, err := randBytes(fileKeySize)
	if err != nil {
		return info, fmt.Errorf("fdsec: file key: %w", err)
	}
	defer clear(fileKey) // FDSEC-15
	wrapNonce, err := randBytes(nonceSize)
	if err != nil {
		return info, fmt.Errorf("fdsec: wrap nonce: %w", err)
	}

	h := header{
		Version:          FormatVersion,
		Suite:            SuiteID1,
		Flags:            flagV1Conform,
		ChunkSize:        p.ChunkSize,
		ClusterAlignment: p.ClusterAlignment,
	}
	copy(h.Salt[:], salt)
	h.setDigest()
	info.HeaderDigest = h.HeaderDigest

	// Key slot 0: the file key wrapped under the credential-derived key
	// (FDSEC-FORMAT.md section 5). Wrapping, not direct encryption. The same
	// derivation yields the mask that hides everything after the salt.
	maskKey, kek, err := deriveKeys(cred, salt)
	if err != nil {
		return info, err
	}
	defer clear(maskKey) // FDSEC-15
	defer clear(kek)
	kekAEAD, err := newXAEAD(kek)
	if err != nil {
		return info, err
	}
	wrapped := kekAEAD.Seal(nil, wrapNonce, fileKey, adSlot0(h.HeaderDigest))
	var slot0 slotRecord
	slot0.Type = typeCredWrap
	copy(slot0.Payload[wrapNonceOff:], wrapNonce)
	copy(slot0.Payload[nonceSize:], wrapped)

	// Sealed metadata, under the file key.
	metaPlain, err := buildMetadataBlock(meta, digest, randBytes)
	if err != nil {
		return info, err
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		return info, err
	}
	metaNonce, err := deriveNonce(fileKey, metaCtx())
	if err != nil {
		return info, err
	}
	sealedMeta := fileAEAD.Seal(nil, metaNonce, metaPlain, adMeta(h.HeaderDigest))

	preLen := h.preLen()
	k := h.chunkCount(n)
	last := h.lastLen(n)
	info.Chunks = k

	// The head - salt in the clear, everything after it masked - then the
	// sealed metadata and random bytes to the cluster boundary
	// (FDSEC-FORMAT.md section 3).
	head, err := buildHead(&h, &slot0, maskKey)
	if err != nil {
		return info, err
	}
	if _, err := dst.Write(head); err != nil {
		return info, fmt.Errorf("fdsec: write head: %w", err)
	}
	if _, err := dst.Write(sealedMeta); err != nil {
		return info, fmt.Errorf("fdsec: write metadata: %w", err)
	}
	alignmentPad, err := randBytes(int(preLen) - preMeta - metaSize)
	if err != nil {
		return info, err
	}
	if _, err := dst.Write(alignmentPad); err != nil {
		return info, fmt.Errorf("fdsec: write alignment pad: %w", err)
	}

	// Pass 2: seal the payload chunk by chunk, each bound to its index, the
	// chunk count, the header digest, its length and its last-or-not position
	// (FDSEC-FORMAT.md section 8).
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return info, fmt.Errorf("fdsec: rewind source: %w", err)
	}
	capacity := h.chunkCapacity()
	pt := make([]byte, capacity)
	written := int64(0)
	for i := int64(0); i < k; i++ {
		if so.stopped() {
			return info, ErrStopped
		}
		want := capacity
		if i == k-1 {
			want = last
		}
		if _, err := io.ReadFull(src, pt[:want]); err != nil {
			return info, fmt.Errorf("fdsec: read source chunk %d: %w", i, err)
		}
		nonce, err := deriveNonce(fileKey, string(chunkCtx(i)))
		if err != nil {
			return info, err
		}
		ct := fileAEAD.Seal(nil, nonce, pt[:want], adChunk(h.HeaderDigest, i, k, i == k-1, want))
		if _, err := dst.Write(ct); err != nil {
			return info, fmt.Errorf("fdsec: write chunk %d: %w", i, err)
		}
		written += int64(len(ct))
		if so.progress != nil {
			so.progress(int64(i)*capacity+int64(want), n)
		}
	}
	if written != (k-1)*int64(p.ChunkSize)+last+tagSize {
		return info, fmt.Errorf("fdsec: internal: payload length arithmetic is wrong")
	}

	// Tail pad: random bytes to a cluster boundary; the real size stays sealed
	// in the metadata (FDSEC-FORMAT.md sections 8, 14).
	total := preLen + written
	tail := int64(p.ClusterAlignment) - total%int64(p.ClusterAlignment)
	if tail == int64(p.ClusterAlignment) {
		tail = 0
	}
	if tail > 0 {
		tp, err := randBytes(int(tail))
		if err != nil {
			return info, err
		}
		if _, err := dst.Write(tp); err != nil {
			return info, fmt.Errorf("fdsec: write tail pad: %w", err)
		}
	}
	info.TotalLen = total + tail
	return info, nil
}
