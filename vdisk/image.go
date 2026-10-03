package vdisk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"filedo/fsx"
)

// ImageToFile copies the container a partition carries into a new container
// file at dst (FDD-BEHAVIOUR section 3 rule 7, SP-0148 4.4): the escape route
// that gives a partition disk back the read path without elevation. The file
// has the partition's length and holds, at the same offsets, every region of
// FDD-FORMAT section 5.3 - both headers, the slots, both map copies and every
// cluster either map references; the bytes outside every region carry no
// meaning (section 5.3) and are left as zeros, so the residue a plain or vault
// partition keeps in its unused clusters is not copied. A partition image is
// a container file (FDD-FORMAT section 15): nothing is re-encrypted and no
// credential is needed.
//
// It never writes over an existing file: the copy goes to dst +
// fsx.PartialSuffix, is read back and compared, and is renamed to dst only
// then; a stop or failure removes the partial file. The carrier is only read.
func ImageToFile(ctx context.Context, src Carrier, dst string, p ProgressSink) (err error) {
	if _, err := os.Stat(dst); err == nil {
		return usagef("%s already exists; an image is never written over an existing file", dst)
	} else if !os.IsNotExist(err) {
		return ioErr(fmt.Errorf("could not check whether %s exists: %w", dst, err))
	}
	L, err := src.Size()
	if err != nil {
		return ioErr(err)
	}
	ranges, err := imageRanges(src, L)
	if err != nil {
		return err
	}
	tmp := dst + fsx.PartialSuffix
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return usagef("%s exists from an earlier interrupted image; delete it and try again", tmp)
		}
		return ioErr(err)
	}
	defer func() {
		if f != nil {
			f.Close()
		}
		if err != nil {
			if rerr := os.Remove(tmp); rerr != nil && !os.IsNotExist(rerr) {
				err = fmt.Errorf("%w; the unfinished image %s could not be removed (%v) - delete it by hand", err, tmp, rerr)
			}
		}
	}()
	if err := f.Truncate(L); err != nil {
		return ioErr(err)
	}
	var total, done int64
	for _, r := range ranges {
		total += r.end - r.start
	}
	const chunk = 4 << 20
	buf := make([]byte, chunk)
	back := make([]byte, chunk)
	copyRange := func(r span, verify bool) error {
		for off := r.start; off < r.end; off += chunk {
			if stopped(ctx) {
				return ErrStopped
			}
			n := int(min(chunk, r.end-off))
			if err := readFull(src, buf[:n], off); err != nil {
				return err
			}
			if verify {
				if _, err := f.ReadAt(back[:n], off); err != nil {
					return ioErr(err)
				}
				if !bytes.Equal(buf[:n], back[:n]) {
					return damagedf("the image differs from its partition at offset %d", off)
				}
				continue
			}
			if _, err := f.WriteAt(buf[:n], off); err != nil {
				return ioErr(err)
			}
			done += int64(n)
			report(p, done, total)
		}
		return nil
	}
	for _, r := range ranges {
		if err := copyRange(r, false); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return ioErr(err)
	}
	// Read the copy back against the partition before it gets its name: a
	// written file is not yet a proven one.
	for _, r := range ranges {
		if err := copyRange(r, true); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		f = nil
		return ioErr(err)
	}
	f = nil
	if _, err := Inspect(tmp); err != nil {
		return err
	}
	if stopped(ctx) {
		return ErrStopped
	}
	if err := fsx.RenameNoReplace(tmp, dst); err != nil {
		if errors.Is(err, fsx.ErrDestinationExists) {
			return usagef("%s appeared while the image was written; the new image was removed", dst)
		}
		return ioErr(err)
	}
	return nil
}

type span struct{ start, end int64 }

// imageRanges is every region of FDD-FORMAT section 5.3 the carrier's winning
// header names, merged and sorted: the primary, the slots, both map copies,
// the backup at L - 4096 (and at physical_size - 4096 when that differs), and
// each cluster either map copy references. The gaps between them - before
// data_offset too - are never copied: a partition keeps there whatever the
// disk held before it was created. Both copies are read because the inactive
// one may be the next writer's; a cluster only it references costs nothing
// to keep.
func imageRanges(b backing, L int64) ([]span, error) {
	res, err := resolveHeaders(b, L)
	if err != nil {
		return nil, err
	}
	h := res.hdr
	fixed, ok := h.fixedRegions(uint64(L))
	if !ok {
		return nil, damagedf("the container's regions do not fit this build")
	}
	var out []span
	for _, r := range fixed {
		if r.end > uint64(L) {
			continue // a region past the carrier is reported by Inspect, not copied
		}
		out = append(out, span{int64(r.start), int64(r.end)})
	}
	if ps := int64(h.PhysicalSize); ps >= 2*headerSize && ps < L {
		out = append(out, span{ps - headerSize, ps})
	}
	m, ok := mapSize(h.ClusterCount)
	if !ok {
		return nil, damagedf("the cluster map does not fit this build")
	}
	cs := int64(h.clusterSize())
	limit := h.clusterLimit(uint64(L))
	seen := map[uint64]bool{}
	for _, copyIndex := range []uint8{0, 1} {
		raw := make([]byte, m)
		off := int64(h.mapCopyOffset(copyIndex))
		if off+int64(m) > L {
			continue
		}
		if err := readFull(b, raw, off); err != nil {
			return nil, err
		}
		for i := uint64(0); i < h.ClusterCount; i++ {
			e := uint64(raw[i*8]) | uint64(raw[i*8+1])<<8 | uint64(raw[i*8+2])<<16 | uint64(raw[i*8+3])<<24 |
				uint64(raw[i*8+4])<<32 | uint64(raw[i*8+5])<<40 | uint64(raw[i*8+6])<<48 | uint64(raw[i*8+7])<<56
			if e == sentinel || seen[e] {
				continue
			}
			if e >= limit {
				if copyIndex == h.MapActive {
					return nil, damagedf("the active map references cluster %d past the end of the partition", e)
				}
				continue
			}
			start := int64(h.DataOffset) + int64(e)*cs
			if start+cs > L-headerSize {
				if copyIndex == h.MapActive {
					return nil, damagedf("the active map references cluster %d past the end of the partition", e)
				}
				continue
			}
			seen[e] = true
			out = append(out, span{start, start + cs})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	merged := out[:1]
	for _, r := range out[1:] {
		last := &merged[len(merged)-1]
		if r.start <= last.end {
			last.end = max(last.end, r.end)
			continue
		}
		merged = append(merged, r)
	}
	return merged, nil
}
