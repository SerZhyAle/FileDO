package fileduplicates

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filedo/statedir"
)

// cacheLockTimeout bounds how long a save waits for another FileDO saving the
// same cache. A save that cannot get the lock is skipped: the cache is an
// optimisation, and losing one run's entries costs a rehash, nothing more.
const cacheLockTimeout = 10 * time.Second

// cacheMaxAge is how long an entry nobody has looked at is kept.
const cacheMaxAge = 30 * 24 * time.Hour

// GetHashCachePath returns where the hash cache lives: the per-user state root
// (%LOCALAPPDATA%\FileDO\state\, SP-0024 section 4), never beside the
// executable, which an installed build cannot write (DUP-12). A cache an
// older build left beside the executable is imported once, as a copy.
func GetHashCachePath() (string, error) {
	return statedir.Path(HASH_CACHE_FILE, statedir.LegacyBesideExe(HASH_CACHE_FILE))
}

// LoadHashCache reads the hash cache. A missing file is an empty cache; a
// file that does not parse is an empty cache and an error (fail closed: a lost
// cache costs a rehash, a misread one could match the wrong files).
func LoadHashCache() (*HashCache, error) {
	cache := &HashCache{
		Entries:  make(map[string]CacheEntry),
		loadedAt: time.Now(),
	}

	path, err := GetHashCachePath()
	if err != nil {
		return cache, fmt.Errorf("failed to locate hash cache: %w", err)
	}
	err = readCacheFile(path, func(key string, e CacheEntry) {
		if usableEntryKey(key) {
			cache.Entries[key] = e
		}
	})
	if err != nil {
		cache.Entries = make(map[string]CacheEntry)
		if errors.Is(err, os.ErrNotExist) {
			return cache, nil
		}
		return cache, fmt.Errorf("failed to read hash cache: %w", err)
	}
	return cache, nil
}

// usableEntryKey keeps entries keyed by an absolute path. Older builds keyed
// on whatever path the scan was given, so `filedo . cd` in two folders wrote
// two different files under one relative key (DUP-05).
func usableEntryKey(key string) bool {
	return filepath.IsAbs(key)
}

// readCacheFile streams the cache file's entries to fn without holding the
// whole file in memory next to the map (DUP-13).
func readCacheFile(path string, fn func(key string, e CacheEntry)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<16))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("hash cache is not a JSON object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return errors.New("hash cache key is not a string")
		}
		var e CacheEntry
		if err := dec.Decode(&e); err != nil {
			return err
		}
		fn(key, e)
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}

// entryMatches is the whole validity rule for a cached hash: the permanent
// invariant (size + modification time) as its floor, plus the object identity
// and change time, plus the digest it was computed with.
func entryMatches(entry CacheEntry, file DuplicateFileInfo) bool {
	return file.identified &&
		entry.Algo == hashAlgo &&
		entry.Size == file.Size &&
		entry.ModTime.Equal(file.ModTime) &&
		entry.VolSerial == file.VolSerial &&
		entry.FileIndex == file.FileIndex &&
		entry.ChangeTime.Equal(file.ChangeTime)
}

// LookupHash returns a cached hash for the file without ever computing it.
// The entry is valid only while entryMatches holds, so a file rewritten with
// the same length and its modification time put back still misses the cache
// - its change time moved. Nothing is deleted on a cached hash alone anyway:
// every delete and move is preceded by a byte comparison.
func (c *HashCache) LookupHash(file DuplicateFileInfo, hashType FileHashType) (string, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry, ok := c.Entries[file.Path]
	if !ok || !entryMatches(entry, file) {
		return "", false
	}
	if hashType == QuickHash && entry.QuickHash != "" {
		return entry.QuickHash, true
	}
	if hashType == FullHash && entry.FullHash != "" {
		return entry.FullHash, true
	}
	return "", false
}

// StoreHash records a freshly computed hash for the file. If an existing entry
// describes a different version of the file it is replaced so stale hashes
// never linger. LastSeen is always refreshed.
func (c *HashCache) StoreHash(file DuplicateFileInfo, hashType FileHashType) {
	if !file.identified || !usableEntryKey(file.Path) {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()

	entry, ok := c.Entries[file.Path]
	if !ok || !entryMatches(entry, file) {
		entry = CacheEntry{
			Path:       file.Path,
			Size:       file.Size,
			ModTime:    file.ModTime,
			VolSerial:  file.VolSerial,
			FileIndex:  file.FileIndex,
			ChangeTime: file.ChangeTime,
			Algo:       hashAlgo,
		}
	}
	if hashType == QuickHash {
		entry.QuickHash = file.QuickHash
	} else {
		entry.FullHash = file.FullHash
	}
	entry.LastSeen = time.Now()
	c.Entries[file.Path] = entry
}

// cachePruner decides which entries a save keeps. It never touches a volume
// that is not there (an unplugged stick keeps its entries until they age out)
// and never stats a network path (a share that went offline would stall the
// save for every entry); an entry this run already validated is not stat'ed
// again (DUP-13).
type cachePruner struct {
	threshold time.Time
	runStart  time.Time
	volumes   map[string]bool
}

func newCachePruner(runStart time.Time) *cachePruner {
	return &cachePruner{
		threshold: time.Now().Add(-cacheMaxAge),
		runStart:  runStart,
		volumes:   make(map[string]bool),
	}
}

func (p *cachePruner) keep(key string, e CacheEntry) bool {
	if !usableEntryKey(key) || e.Algo != hashAlgo || e.LastSeen.Before(p.threshold) {
		return false
	}
	if !p.runStart.IsZero() && !e.LastSeen.Before(p.runStart) {
		return true // validated by this run
	}
	vol := filepath.VolumeName(key)
	if vol == "" || strings.HasPrefix(vol, `\\`) {
		return true // network path: kept by age alone
	}
	present, known := p.volumes[strings.ToUpper(vol)]
	if !known {
		_, err := os.Stat(vol + `\`)
		present = err == nil
		p.volumes[strings.ToUpper(vol)] = present
	}
	if !present {
		return true
	}
	info, err := os.Stat(key)
	return err == nil && info.Size() == e.Size && info.ModTime().Equal(e.ModTime)
}

// Save writes the cache to disk.
//
// Two FileDO processes may save at once, so the read-modify-write happens
// under a lock file, entries another process saved since this one loaded are
// merged in rather than overwritten, and the new file is written to a unique
// temporary name and renamed into place - a reader sees the old file or the
// new one, never half of either (DUP-13). The JSON is streamed, entry by
// entry, instead of being marshalled into one buffer first.
func (c *HashCache) Save() error {
	path, err := GetHashCachePath()
	if err != nil {
		return fmt.Errorf("failed to locate hash cache: %w", err)
	}
	unlock, err := statedir.Lock(path, cacheLockTimeout)
	if err != nil {
		return fmt.Errorf("hash cache is busy: %w", err)
	}
	defer unlock()

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	pruner := newCachePruner(c.loadedAt)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("error writing hash cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}

	w := bufio.NewWriterSize(tmp, 1<<16)
	kept, removed := 0, 0
	first := true
	writeEntry := func(key string, e CacheEntry) error {
		k, err := json.Marshal(key)
		if err != nil {
			return err
		}
		v, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if !first {
			if _, err := w.WriteString(",\n"); err != nil {
				return err
			}
		}
		first = false
		if _, err := w.Write(k); err != nil {
			return err
		}
		if _, err := w.WriteString(": "); err != nil {
			return err
		}
		_, err = w.Write(v)
		return err
	}

	if _, err := w.WriteString("{\n"); err != nil {
		return fail(err)
	}
	for key, e := range c.Entries {
		if !pruner.keep(key, e) {
			removed++
			continue
		}
		if err := writeEntry(key, e); err != nil {
			return fail(fmt.Errorf("error writing hash cache: %w", err))
		}
		kept++
	}
	// Entries only the file on disk has - another run's work - are kept.
	var mergeErr error
	rerr := readCacheFile(path, func(key string, e CacheEntry) {
		if mergeErr != nil {
			return
		}
		if _, mine := c.Entries[key]; mine || !pruner.keep(key, e) {
			return
		}
		if err := writeEntry(key, e); err != nil {
			mergeErr = err
			return
		}
		kept++
	})
	if mergeErr != nil {
		return fail(fmt.Errorf("error writing hash cache: %w", mergeErr))
	}
	_ = rerr // an unreadable old file has nothing worth merging
	if _, err := w.WriteString("\n}\n"); err != nil {
		return fail(err)
	}
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// The lock is held, so replacing the file is the whole point here.
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("error finalizing hash cache: %w", err)
	}
	if removed > 0 {
		fmt.Printf("Hash cache saved: %d entries (%d stale removed).\n", kept, removed)
	}
	return nil
}
