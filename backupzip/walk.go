// Package backupzip is the archive half of `filedo backup` and `filedo restore`
// (SP-0198): the walk that lists what a backup carries, the density probe that
// picks the zip level, the packer that writes the zip through a partial name,
// and the screening and unpacking of a zip on restore. It knows nothing of the
// command line, the credential or the container: cmd/filedo wires those.
//
// The zip is the universal one - deflate or store, UTF-8 names, the
// modification time, zip64 when a size needs it - because a plain backup must
// open in any zip tool on any machine (SP-0198 D8). ACLs, alternate streams and
// links are not carried.
package backupzip

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one file or folder of the tree being backed up.
type Entry struct {
	// Rel is the path below the source root, separated by '/', as the zip
	// stores it. A folder's Rel carries no trailing slash.
	Rel     string
	Path    string // the absolute path on disk
	Dir     bool
	Size    int64
	ModTime time.Time
}

// WalkOptions tunes Walk.
type WalkOptions struct {
	// IsLink reports whether a path is a link-like reparse point (a junction,
	// a symbolic link, a mount point). The caller supplies the platform's own
	// classification, so there is one test of it in the program, not two; a
	// data reparse point (a cloud or deduplicated file) is not a link.
	IsLink func(path string) bool
	// SkipLinks lets the walk step over a link and list it instead of
	// refusing the whole backup (SP-0198 D7, the `skip-links` word).
	SkipLinks bool
	// SkipSystem drops the system files Windows keeps at a volume root
	// (SystemFiles) - set for a volume or share root only (D6).
	SkipSystem bool
	// Stop is asked between entries; true ends the walk with ErrStopped.
	Stop func() bool
}

// Walked is what the walk found.
type Walked struct {
	Entries       []Entry // folders and files, parents before children
	Files         int64
	Bytes         int64
	SkippedLinks  []string // Rel of each link stepped over
	SkippedSystem []string // Rel of each system file stepped over
}

// SystemFiles are the names skipped at the top of a volume or share root: the
// paging, hibernation and swap files and the volume's own system folder. They
// cannot be read while Windows runs, and none of them is user data (D6).
var SystemFiles = []string{"pagefile.sys", "hiberfil.sys", "swapfile.sys", "System Volume Information"}

// ErrStopped is a stop requested while the archive work ran.
var ErrStopped = errors.New("backupzip: stopped by request")

// LinkError is a link inside the source, which a backup refuses rather than
// follows: following one can walk in a circle or out of the tree the user
// named, and silently skipping one would pack a tree that is not the one on
// disk.
type LinkError struct{ Path string }

func (e *LinkError) Error() string {
	return fmt.Sprintf("%s is a junction or a symbolic link; a backup does not follow links - pass skip-links to leave it out (it is listed), or back up the folder it points to", e.Path)
}

// Walk lists root's tree. root must be an existing folder; the caller has
// already screened it (a file, a link and a refused root never reach here).
func Walk(root string, o WalkOptions) (Walked, error) {
	var w Walked
	isSystem := func(name string) bool {
		for _, s := range SystemFiles {
			if strings.EqualFold(s, name) {
				return true
			}
		}
		return false
	}
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		des, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		sort.Slice(des, func(i, j int) bool { return des[i].Name() < des[j].Name() })
		for _, de := range des {
			if o.Stop != nil && o.Stop() {
				return ErrStopped
			}
			full := filepath.Join(dir, de.Name())
			r := de.Name()
			if rel != "" {
				r = rel + "/" + de.Name()
			}
			if rel == "" && o.SkipSystem && isSystem(de.Name()) {
				w.SkippedSystem = append(w.SkippedSystem, r)
				continue
			}
			fi, err := os.Lstat(full)
			if err != nil {
				return err
			}
			link := fi.Mode()&fs.ModeSymlink != 0 || (o.IsLink != nil && o.IsLink(full))
			if link {
				if !o.SkipLinks {
					return &LinkError{Path: full}
				}
				w.SkippedLinks = append(w.SkippedLinks, r)
				continue
			}
			if fi.IsDir() {
				w.Entries = append(w.Entries, Entry{Rel: r, Path: full, Dir: true, ModTime: fi.ModTime()})
				if err := walk(full, r); err != nil {
					return err
				}
				continue
			}
			// A regular file, or a data reparse point Go reports as irregular
			// (a cloud or deduplicated file): both are read as files. Anything
			// else - a device, a pipe - is refused by name.
			if !fi.Mode().IsRegular() && fi.Mode()&fs.ModeIrregular == 0 {
				return fmt.Errorf("%s is neither an ordinary file nor a folder, and a backup refuses it", full)
			}
			w.Entries = append(w.Entries, Entry{Rel: r, Path: full, Size: fi.Size(), ModTime: fi.ModTime()})
			w.Files++
			w.Bytes += fi.Size()
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return Walked{}, err
	}
	return w, nil
}
