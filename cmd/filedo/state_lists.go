package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"filedo/fsx"
	"filedo/statedir"
)

// The per-file lists FileDO keeps for itself (SP-0027 COPY-05, CHK-04, CHK-10;
// SP-0023 theme T5): the copy engine's skip list of damaged sources, check's
// damaged list and check's good list. They live under the state root, never
// in the current directory, and copy and check no longer share one file - a
// locked NTUSER.DAT that check could not read must not make a later copy skip
// it.
//
// An entry names a file as it was: path, size, modification time and the
// identity of the volume and the file it was recorded on. A file that changed
// since it was recorded is a different file and is tried again, which is what
// keeps one slow read during a disk spin-up from poisoning a healthy file
// forever; and a file at the same path on another volume - the next SD card at
// the same drive letter, holding a copy with its mtime kept - is a different
// file too (AUD-14-F1).
//
// The line format is written down in statedir/STATE-LISTS.md, format 2:
//
//	# filedo-state-list 2
//	<path> TAB <size> TAB <mtime, RFC 3339 UTC> TAB <volume serial, 8 hex> TAB <file ID, 16 hex>
//
// Format 1 lines (path, size, mtime) and path-only lines are read safely and
// never trusted; the first write after such a load rewrites the file with the
// trusted entries only. A file declaring a newer format is not trusted and
// not written.
const (
	copySkipListName    = "skip_files.list"    // copy's damaged sources
	checkDamagedName    = "check_damaged.list" // check's damaged files
	checkGoodListName   = "check_files.list"   // check's files read cleanly
	stateListTimeLayout = time.RFC3339Nano

	// stateListFormat is the line format this build writes and trusts.
	stateListFormat = 2
	// stateListHeaderPrefix opens the header line every list this build
	// writes: the prefix, then the format number.
	stateListHeaderPrefix = "# filedo-state-list "
)

// fileStamp is what a list remembers about one file.
type fileStamp struct {
	size int64
	mod  time.Time
	// vol and fid are the volume serial number and the file ID of the
	// recorded file (fsx.IdentityOf).
	vol uint32
	fid uint64
}

// fileStateList is one of the lists above, loaded once per run and appended
// to as the run finds files. It is safe for concurrent use.
type fileStateList struct {
	path string

	mu        sync.Mutex
	entries   map[string]fileStamp
	out       *os.File
	bw        *bufio.Writer
	unflushed int
	// legacyIgnored counts lines this build does not trust: path-only lines
	// and format 1 lines (path, size, time) that carry no volume identity,
	// and anything that does not parse. They cannot say whether the file is
	// still the one that was recorded, or on the same volume, so those files
	// are read again.
	legacyIgnored int
	// needsRewrite is set when the file on disk holds untrusted lines or no
	// format header: the first write replaces it with the trusted entries.
	needsRewrite bool
	// newerFormat is set when the file declares a format this build does not
	// know: nothing in it is trusted and it is never written.
	newerFormat bool
	readOnly    bool
	// identity resolves a path to its volume serial and file ID; a test may
	// replace it.
	identity func(string) (uint32, uint64, error)
}

// stateListLoads counts list loads; a test holds a run to one load (COPY-16).
var stateListLoads atomic.Int64

// fileIdentity is the volume serial and file ID of p, as the filesystem
// reports them. It opens the file for attributes only, which reads no data.
func fileIdentity(p string) (uint32, uint64, error) {
	fid, err := fsx.FileIDOf(p)
	if err != nil {
		return 0, 0, err
	}
	return fid.VolSerial, fid.FileIndex, nil
}

// openStateList loads the named list from the state root, importing a legacy
// copy from the current directory once when the state root has none.
func openStateList(name string, importLegacy bool) (*fileStateList, error) {
	var legacy []string
	if importLegacy {
		legacy = append(legacy, statedir.LegacyInCwd(name))
	}
	p, err := statedir.Path(name, legacy...)
	if err != nil {
		return &fileStateList{entries: make(map[string]fileStamp), identity: fileIdentity}, err
	}
	return loadStateList(p)
}

// loadStateList reads the list at an explicit path. A missing file is an
// empty list. The content never makes it fail: a line it cannot trust is
// counted and skipped.
func loadStateList(path string) (*fileStateList, error) {
	stateListLoads.Add(1)
	l := &fileStateList{path: path, entries: make(map[string]fileStamp), identity: fileIdentity}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return l, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	sawHeader, sawLine := false, false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, stateListHeaderPrefix) {
			v, verr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, stateListHeaderPrefix)))
			if verr == nil {
				if v > stateListFormat {
					l.newerFormat = true
				}
				sawHeader = v == stateListFormat
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		sawLine = true
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || len(fields[3]) != 8 || len(fields[4]) != 16 {
			l.legacyIgnored++
			continue
		}
		size, serr := strconv.ParseInt(fields[1], 10, 64)
		mod, terr := time.Parse(stateListTimeLayout, fields[2])
		vol, verr := strconv.ParseUint(fields[3], 16, 32)
		fid, ferr := strconv.ParseUint(fields[4], 16, 64)
		if serr != nil || terr != nil || verr != nil || ferr != nil {
			l.legacyIgnored++
			continue
		}
		l.entries[stateListKey(fields[0])] = fileStamp{size: size, mod: mod, vol: uint32(vol), fid: fid}
	}
	if l.newerFormat {
		// Written by a newer FileDO: its lines may look like ours and mean
		// something else. Trust none of them and leave the file alone.
		l.legacyIgnored += len(l.entries)
		l.entries = make(map[string]fileStamp)
		l.readOnly = true
		return l, sc.Err()
	}
	l.needsRewrite = l.legacyIgnored > 0 || (sawLine && !sawHeader)
	return l, sc.Err()
}

// stateListKey is the case-folded absolute spelling a list is keyed on. The
// path only finds the candidate entry; the volume serial and the file ID
// decide whether it is the same file.
func stateListKey(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return strings.ToLower(filepath.Clean(p))
}

// Path is where the list lives.
func (l *fileStateList) Path() string { return l.path }

// Len is the number of trusted entries.
func (l *fileStateList) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// LegacyIgnored is the number of lines that were not trusted: older lines
// without a volume identity, and anything that did not parse.
func (l *fileStateList) LegacyIgnored() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.legacyIgnored
}

// Has reports whether the list names this exact file: same path, same size,
// same modification time, and the same volume and file ID as when it was
// recorded. The identity is resolved only for a path the list names, so a
// sweep over files the list does not know opens nothing extra. A file whose
// identity cannot be resolved is not trusted: it is read again.
func (l *fileStateList) Has(p string, size int64, mod time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	st, ok := l.entries[stateListKey(p)]
	identity := l.identity
	l.mu.Unlock()
	if !ok || st.size != size || !st.mod.Equal(mod) {
		return false
	}
	if identity == nil {
		identity = fileIdentity
	}
	vol, fid, err := identity(p)
	if err != nil {
		return false
	}
	return st.vol == vol && st.fid == fid
}

// HasInfo is Has for a FileInfo.
func (l *fileStateList) HasInfo(p string, info os.FileInfo) bool {
	if info == nil {
		return false
	}
	return l.Has(p, info.Size(), info.ModTime())
}

// Add records a file and appends it to the list on disk at once, so a run
// that is killed still leaves what it learned. A list opened read-only (a
// dry run) remembers in memory only. The file's volume and file ID are
// resolved now; a file whose identity cannot be resolved is not recorded,
// because an entry that cannot name its volume would never be trusted.
func (l *fileStateList) Add(p string, size int64, mod time.Time) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	identity := l.identity
	l.mu.Unlock()
	if identity == nil {
		identity = fileIdentity
	}
	vol, fid, err := identity(p)
	if err != nil {
		return fmt.Errorf("cannot identify the file's volume: %w", err)
	}
	return l.addStamp(p, fileStamp{size: size, mod: mod, vol: vol, fid: fid})
}

// AddStamp records a file whose identity is already known.
func (l *fileStateList) AddStamp(p string, size int64, mod time.Time, vol uint32, fid uint64) error {
	if l == nil {
		return nil
	}
	return l.addStamp(p, fileStamp{size: size, mod: mod, vol: vol, fid: fid})
}

// addStamp records an entry whose identity is already known.
func (l *fileStateList) addStamp(p string, st fileStamp) error {
	key := stateListKey(p)
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.entries[key]; ok && old.size == st.size && old.mod.Equal(st.mod) && old.vol == st.vol && old.fid == st.fid {
		return nil
	}
	l.entries[key] = st
	if l.readOnly || l.path == "" {
		return nil
	}
	if l.out == nil {
		rewrote := l.needsRewrite
		if err := l.openForAppend(); err != nil {
			return err
		}
		if rewrote {
			// The rewrite already wrote this entry with the others.
			return nil
		}
	}
	abs := p
	if a, err := filepath.Abs(p); err == nil {
		abs = a
	}
	_, err := io.WriteString(l.bw, formatStateLine(filepath.Clean(abs), st))
	l.unflushed++
	if l.unflushed >= 64 {
		if ferr := l.bw.Flush(); ferr != nil && err == nil {
			err = ferr
		}
		l.unflushed = 0
	}
	return err
}

// formatStateLine is one format 2 line.
func formatStateLine(abs string, st fileStamp) string {
	return fmt.Sprintf("%s\t%d\t%s\t%08X\t%016X\n", abs, st.size, st.mod.UTC().Format(stateListTimeLayout), st.vol, st.fid)
}

// openForAppend opens the list for appending. A new or empty file starts with
// the format header; a file holding untrusted lines is first replaced by one
// holding the header and the trusted entries only. Called with l.mu held.
func (l *fileStateList) openForAppend() error {
	if l.needsRewrite {
		if err := l.rewriteLocked(); err != nil {
			return err
		}
		l.needsRewrite = false
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if fi, serr := f.Stat(); serr == nil && fi.Size() == 0 {
		if _, werr := fmt.Fprintf(f, "%s%d\n", stateListHeaderPrefix, stateListFormat); werr != nil {
			f.Close()
			return werr
		}
	}
	l.out = f
	l.bw = bufio.NewWriterSize(f, 64*1024)
	l.unflushed = 0
	return nil
}

// rewriteLocked replaces the list with the header and the trusted entries,
// through a temporary file, so a crash leaves the old list or the new one.
// The entries are written under their case-folded key, which names the same
// file. Losing an entry here only means a file is read again.
func (l *fileStateList) rewriteLocked() error {
	tmp, err := os.CreateTemp(filepath.Dir(l.path), filepath.Base(l.path)+".rewrite-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	fmt.Fprintf(w, "%s%d\n", stateListHeaderPrefix, stateListFormat)
	for key, st := range l.entries {
		w.WriteString(formatStateLine(key, st))
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// A state file of FileDO's own, not user data: replacing it is the point.
	if err := os.Rename(tmpName, l.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// AddInfo is Add for a FileInfo.
func (l *fileStateList) AddInfo(p string, info os.FileInfo) error {
	if info == nil {
		return nil
	}
	return l.Add(p, info.Size(), info.ModTime())
}

// Flush writes any pending buffered entries to disk.
func (l *fileStateList) Flush() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bw != nil {
		l.unflushed = 0
		return l.bw.Flush()
	}
	return nil
}

// Close releases the append handle.
func (l *fileStateList) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var flushErr error
	if l.bw != nil {
		flushErr = l.bw.Flush()
		l.bw = nil
		l.unflushed = 0
	}
	if l.out == nil {
		return flushErr
	}
	err := l.out.Close()
	l.out = nil
	if flushErr != nil {
		return flushErr
	}
	return err
}
