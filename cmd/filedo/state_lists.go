package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"filedo/statedir"
)

// The per-file lists FileDO keeps for itself (SP-0027 COPY-05, CHK-04, CHK-10;
// SP-0023 theme T5): the copy engine's skip list of damaged sources, check's
// damaged list and check's good list. They live under the state root, never
// in the current directory, and copy and check no longer share one file - a
// locked NTUSER.DAT that check could not read must not make a later copy skip
// it.
//
// An entry names a file as it was: path, size and modification time. A file
// that changed since it was recorded is a different file and is tried again,
// which is what keeps one slow read during a disk spin-up from poisoning a
// healthy file forever.
const (
	copySkipListName    = "skip_files.list"    // copy's damaged sources
	checkDamagedName    = "check_damaged.list" // check's damaged files
	checkGoodListName   = "check_files.list"   // check's files read cleanly
	stateListTimeLayout = time.RFC3339Nano
)

// fileStamp is what a list remembers about one file.
type fileStamp struct {
	size int64
	mod  time.Time
}

// fileStateList is one of the lists above, loaded once per run and appended
// to as the run finds files. It is safe for concurrent use.
type fileStateList struct {
	path string

	mu      sync.Mutex
	entries map[string]fileStamp
	out     *os.File
	// legacyIgnored counts path-only lines written by versions that did not
	// record size and time. They cannot say whether the file is still the
	// one that was recorded - and the old lists were polluted by the very
	// defects this format fixes - so they are not trusted.
	legacyIgnored int
	readOnly      bool
}

// stateListLoads counts list loads; a test holds a run to one load (COPY-16).
var stateListLoads atomic.Int64

// openStateList loads the named list from the state root, importing a legacy
// copy from the current directory once when the state root has none.
func openStateList(name string, importLegacy bool) (*fileStateList, error) {
	var legacy []string
	if importLegacy {
		legacy = append(legacy, statedir.LegacyInCwd(name))
	}
	p, err := statedir.Path(name, legacy...)
	if err != nil {
		return &fileStateList{entries: make(map[string]fileStamp)}, err
	}
	return loadStateList(p)
}

// loadStateList reads the list at an explicit path. A missing file is an
// empty list.
func loadStateList(path string) (*fileStateList, error) {
	stateListLoads.Add(1)
	l := &fileStateList{path: path, entries: make(map[string]fileStamp)}
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
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			l.legacyIgnored++
			continue
		}
		size, serr := strconv.ParseInt(fields[1], 10, 64)
		mod, terr := time.Parse(stateListTimeLayout, fields[2])
		if serr != nil || terr != nil {
			l.legacyIgnored++
			continue
		}
		l.entries[stateListKey(fields[0])] = fileStamp{size: size, mod: mod}
	}
	return l, sc.Err()
}

// stateListKey is the case-folded absolute spelling a list is keyed on.
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

// LegacyIgnored is the number of path-only lines that were not trusted.
func (l *fileStateList) LegacyIgnored() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.legacyIgnored
}

// Has reports whether the list names this exact file: same path, same size,
// same modification time.
func (l *fileStateList) Has(p string, size int64, mod time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	st, ok := l.entries[stateListKey(p)]
	l.mu.Unlock()
	return ok && st.size == size && st.mod.Equal(mod)
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
// dry run) remembers in memory only.
func (l *fileStateList) Add(p string, size int64, mod time.Time) error {
	if l == nil {
		return nil
	}
	key := stateListKey(p)
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, ok := l.entries[key]; ok && st.size == size && st.mod.Equal(mod) {
		return nil
	}
	l.entries[key] = fileStamp{size: size, mod: mod}
	if l.readOnly || l.path == "" {
		return nil
	}
	if l.out == nil {
		f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		l.out = f
	}
	abs := p
	if a, err := filepath.Abs(p); err == nil {
		abs = a
	}
	_, err := fmt.Fprintf(l.out, "%s\t%d\t%s\n", filepath.Clean(abs), size, mod.UTC().Format(stateListTimeLayout))
	return err
}

// AddInfo is Add for a FileInfo.
func (l *fileStateList) AddInfo(p string, info os.FileInfo) error {
	if info == nil {
		return nil
	}
	return l.Add(p, info.Size(), info.ModTime())
}

// Close releases the append handle.
func (l *fileStateList) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.out == nil {
		return nil
	}
	err := l.out.Close()
	l.out = nil
	return err
}
