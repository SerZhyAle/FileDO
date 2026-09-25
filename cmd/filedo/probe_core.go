package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The probe's decision logic, separated from the raw device so it can be
// proven against fake devices (CLI-01, CLI-07, CLI-08).

// sectorDevice is the raw sector I/O the probe needs.
type sectorDevice interface {
	ReadAt(p []byte, off int64) error
	WriteAt(p []byte, off int64) error
}

// probeResult is what a probe observed.
type probeResult struct {
	planned     int   // markers the plan asked for
	written     int   // markers actually written
	unreadable  int   // positions skipped because their original could not be read
	mismatches  int   // a marker read back as something else (lost, or garbage)
	aliases     int   // a marker read back as ANOTHER marker - the wrap-around signature
	readErrors  int   // verification reads that failed
	firstBadOff int64 // lowest offset whose marker failed, -1 when none
	lastGoodOff int64 // highest offset below firstBadOff that verified
	restoreErrs []error
	offsets     []int64
}

// probePlan chooses where markers go.
//
// Two families (CLI-08). Evenly spread markers across 5%..95% of the claimed
// size catch a controller that drops writes above its real capacity. They do
// not catch one that wraps addresses (a -> a mod R): every marker reads back
// intact at its own offset, because each one is read from where it was just
// written. So there are also anchors low on the volume and, above each anchor,
// markers at anchor + 2^n for every n that fits: on a counterfeit whose real
// capacity R is a power of two - flash chips are - the marker at A + 2^n with
// 2^n >= R lands exactly on the anchor A and overwrites it, and the anchor then
// reads back as someone else's marker.
func probePlan(totalBytes, sector int64) []int64 {
	if sector <= 0 {
		sector = 512
	}
	align := func(x int64) int64 { return (x / sector) * sector }
	end := align(int64(float64(totalBytes) * 0.95))
	start := align(int64(float64(totalBytes) * 0.05))
	if start < 1<<20 {
		start = align(1 << 20)
	}

	seen := map[int64]bool{}
	var out []int64
	add := func(o int64) {
		o = align(o)
		if o < align(1<<20) || o+sector > end || seen[o] {
			return
		}
		seen[o] = true
		out = append(out, o)
	}

	// Anchors first: they are written before anything that could alias them.
	anchorBase := int64(16 << 20)
	if anchorBase+4*(1<<20)+sector > end {
		anchorBase = align(1 << 20)
	}
	var anchors []int64
	for i := int64(0); i < 4; i++ {
		a := align(anchorBase + i*(1<<20))
		if a+sector <= end {
			anchors = append(anchors, a)
			add(a)
		}
	}
	// Evenly spread markers.
	const spread = 32
	if end > start {
		step := float64(end-start) / float64(spread-1)
		for i := 0; i < spread; i++ {
			add(start + int64(float64(i)*step))
		}
	}
	// Anchor + 2^n.
	for _, a := range anchors {
		for n := uint(24); n < 62; n++ {
			o := a + int64(1)<<n
			if o+sector > end || o < 0 {
				break
			}
			add(o)
		}
	}
	// Anchors stay first; the rest is written in ascending order.
	rest := out[len(anchors):]
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	return out
}

// probeMarker is the content written at an offset: a run token that no
// earlier probe shares, and the offset itself, so a sector read back from the
// wrong place names where it really came from.
func probeMarker(buf []byte, token string, off int64) {
	for i := range buf {
		buf[i] = 0
	}
	copy(buf, fmt.Sprintf("FILEDO_PROBE %s %x\n", token, off))
}

// parseProbeMarker returns the offset a sector's marker names, if the sector
// holds a marker of this run.
func parseProbeMarker(buf []byte, token string) (int64, bool) {
	prefix := "FILEDO_PROBE " + token + " "
	if !bytes.HasPrefix(buf, []byte(prefix)) {
		return 0, false
	}
	rest := buf[len(prefix):]
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		return 0, false
	}
	off, err := strconv.ParseInt(strings.TrimSpace(string(rest[:nl])), 16, 64)
	if err != nil {
		return 0, false
	}
	return off, true
}

// probeRun is one probe in flight. restore puts every original back exactly
// once, whoever calls it first - the probe itself, or the force-exit cleanup
// (CLI-07).
type probeRun struct {
	dev       sectorDevice
	sector    int
	offsets   []int64
	originals map[int64][]byte
	written   []int64
	once      sync.Once
	restoreEr []error
	newBuf    func(int) []byte
}

func (p *probeRun) restore() []error {
	p.once.Do(func() {
		// Newest first: on a device that aliases addresses, a physical sector
		// then ends holding its own original whichever alias wrote it last.
		for i := len(p.written) - 1; i >= 0; i-- {
			off := p.written[i]
			orig, ok := p.originals[off]
			if !ok {
				continue // never written, because its original was unreadable
			}
			if err := p.dev.WriteAt(orig, off); err != nil {
				p.restoreEr = append(p.restoreEr, fmt.Errorf("restore at %d: %w", off, err))
			}
		}
	})
	return p.restoreEr
}

// errProbeStopped is returned when a stop request ended a probe; everything it
// wrote has been restored.
var errProbeStopped = fmt.Errorf("probe %w; every sector it wrote was restored", errRunStopped)

// probeCore runs the probe on dev: save originals, write markers, read them
// back, restore. A position whose original could not be read is never
// written (CLI-07a), restore errors are returned (CLI-07b), and register is
// given the restore so a forced exit still restores (CLI-07c). stop is asked
// between writes.
func probeCore(dev sectorDevice, totalBytes int64, sector int, newBuf func(int) []byte, register func(restore func()) (unregister func()), stop func() bool) (probeResult, error) {
	res := probeResult{firstBadOff: -1, lastGoodOff: -1}
	if newBuf == nil {
		newBuf = func(n int) []byte { return make([]byte, n) }
	}
	offsets := probePlan(totalBytes, int64(sector))
	res.planned = len(offsets)
	res.offsets = offsets
	if len(offsets) == 0 {
		return res, fmt.Errorf("the volume is too small to probe (%d bytes)", totalBytes)
	}

	tokenBytes := make([]byte, 8)
	if _, err := rand.Read(tokenBytes); err != nil {
		return res, err
	}
	token := hex.EncodeToString(tokenBytes)

	run := &probeRun{dev: dev, sector: sector, originals: map[int64][]byte{}, newBuf: newBuf}
	for _, off := range offsets {
		orig := newBuf(sector)
		if err := dev.ReadAt(orig, off); err != nil {
			res.unreadable++
			continue
		}
		run.originals[off] = orig
	}
	if register != nil {
		unregister := register(func() { run.restore() })
		defer unregister()
	}
	finish := func() {
		res.restoreErrs = run.restore()
	}

	writeBuf := newBuf(sector)
	for _, off := range offsets {
		if _, ok := run.originals[off]; !ok {
			continue
		}
		if stop != nil && stop() {
			finish()
			return res, errProbeStopped
		}
		probeMarker(writeBuf, token, off)
		run.written = append(run.written, off)
		if err := dev.WriteAt(writeBuf, off); err != nil {
			finish()
			return res, fmt.Errorf("write failed at offset %d: %w", off, err)
		}
		res.written++
	}

	readBuf := newBuf(sector)
	for _, off := range run.written {
		bad := false
		if err := dev.ReadAt(readBuf, off); err != nil {
			res.readErrors++
			bad = true
		} else if named, ok := parseProbeMarker(readBuf, token); ok {
			if named != off {
				// Another marker of this run: the device answered this address
				// with the sector it stored for a different one.
				res.aliases++
				bad = true
			}
		} else {
			res.mismatches++
			bad = true
		}
		if bad {
			if res.firstBadOff < 0 || off < res.firstBadOff {
				res.firstBadOff = off
			}
		}
	}
	for _, off := range run.written {
		if (res.firstBadOff < 0 || off < res.firstBadOff) && off > res.lastGoodOff {
			res.lastGoodOff = off
		}
	}
	finish()
	return res, nil
}

// probeVerdict turns what the probe saw into the run's answer (CLI-01):
// markers that came back wrong are a defect; reads that failed, or a restore
// that failed, mean the probe could not verify; everything intact passes.
func probeVerdict(res probeResult) error {
	var restoreErr error
	if len(res.restoreErrs) > 0 {
		restoreErr = fmt.Errorf("%d sector(s) could not be restored - the file system may need `chkdsk /f`: %w", len(res.restoreErrs), errors.Join(res.restoreErrs...))
	}
	if res.aliases+res.mismatches > 0 {
		msg := fmt.Sprintf("fake capacity: %d of %d probe markers came back wrong (%d as another marker - the device wraps addresses)", res.aliases+res.mismatches, res.written, res.aliases)
		if restoreErr != nil {
			return errors.Join(defectf("%s", msg), restoreErr)
		}
		return defectf("%s", msg)
	}
	if restoreErr != nil {
		return restoreErr
	}
	if res.readErrors > 0 {
		return fmt.Errorf("%d of %d probe markers could not be read back, so the probe could not verify the device", res.readErrors, res.written)
	}
	if res.written == 0 {
		return fmt.Errorf("no probe position could be read, so nothing was written and nothing was verified")
	}
	return nil
}
