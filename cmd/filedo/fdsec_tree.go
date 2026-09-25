package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"filedo/fdsec"
)

// Folder containers (SP-0009): `secure` on a folder packs the whole tree into
// one .fd-sec file (FDSEC-FORMAT suite 3), and `unsecure` on such a container
// restores the tree. The command surface is the file one - same verbs, same
// options, same destinations - and what is new lives here: the walk that
// refuses reparse points, the restore into a fresh temporary folder renamed
// into place as one unit, and the disposition of an original that is a tree.

// fdsecTreeTimes reads an entry's three timestamps the way a single file's are
// read (fdsecMetadataFromStat), so the walk and the file path cannot differ.
func fdsecTreeTimes(fi os.FileInfo) (time.Time, time.Time, time.Time) {
	m := fdsecMetadataFromStat(fi)
	return m.CreatedAt, m.AccessedAt, m.ModifiedAt
}

// fdsecScanTree walks a folder, refusing a link-like reparse point anywhere in
// it; OneDrive placeholders and other data-bearing reparse points are data
// (FDSEC-11), the same classification the single-file source uses.
func fdsecScanTree(root string) (*fdsec.Tree, error) {
	return fdsec.ScanTree(root, fdsec.ScanOptions{IsReparsePoint: fdsecRefusedReparse, Times: fdsecTreeTimes})
}

// fdsecPathInside reports whether p is root or lies under it, lexically and
// case-insensitively on Windows.
func fdsecPathInside(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func fdsecSecureTree(path string, o *fdsecOpts, cred fdsec.Credential, hl *HistoryLogger) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// The walk comes first: a reparse point anywhere in the tree refuses the
	// whole folder before a byte is read or a container name is chosen.
	tree, err := fdsecScanTree(abs)
	if err != nil {
		return err
	}

	dstPath, err := fdsecContainerPathFor(abs, filepath.Base(abs), o)
	if err != nil {
		return err
	}
	if dabs, derr := filepath.Abs(dstPath); derr == nil && fdsecPathInside(strings.ToLower(dabs), strings.ToLower(abs)) {
		return usagef("the container would be written inside the folder it packs (%s); name a destination outside it with to <path>", dstPath)
	}
	// Force exit only; a graceful stop ends the pack at a chunk boundary and
	// the pack removes its own temporary file (FDSEC-02).
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			removeFdsecPartials(dstPath)
		}
	})
	defer removeCleanup()

	var total int64
	files := 0
	for _, e := range tree.Entries {
		if !e.IsDir() {
			total += e.Size
			files++
		}
	}
	// The collision rule before the pack and again if the name was taken
	// while it ran; the final rename never replaces what it finds (FDSEC-01),
	// and an overwrite the user chose replaces the old container only after
	// the new one verified (FDSEC-03).
	var info fdsec.Info
	for attempt := 0; ; attempt++ {
		var replace bool
		dstPath, replace, err = fdsecResolveCollision(dstPath, o, false)
		if err != nil {
			return err
		}
		opts := []fdsec.StreamOption{newFdsecTracker("secure", total), fdsecStreamStop()}
		if replace {
			opts = append(opts, fdsec.AllowReplace())
		}
		info, err = fdsec.PackTreeFile(dstPath, tree, cred, fdsec.Params{}, opts...)
		if errors.Is(err, fdsec.ErrExists) && attempt < 5 {
			fmt.Printf("%s%s was created by something else while this pack ran; choosing again.\n", fdsecEndProgress(), dstPath)
			continue
		}
		if fdsecStopped(err) {
			fmt.Printf("%sStopped: nothing was written for %s; the folder is untouched.\n", fdsecEndProgress(), path)
			return errFdsecStopped
		}
		if err != nil {
			return err
		}
		break
	}
	if o.produced != nil {
		o.produced[fdsecPathKey(dstPath)] = true
	}

	secrecy := "encrypted"
	if len(cred) == 0 {
		secrecy = "obfuscation only - NO SECRECY (empty password)"
	}
	fmt.Printf("%sOK %s -> %s (%s, folder of %d entries: %d files, %d folders, %s, container %s)\n",
		fdsecEndProgress(), path, dstPath, secrecy, len(tree.Entries), files, len(tree.Entries)-files, formatBytes(uint64(info.Size)), formatBytes(uint64(info.TotalLen)))
	if o.rename {
		// The folder that holds it, never the names (CLI-21).
		hl.HideTarget(path)
		hl.SetResult("container", filepath.Dir(dstPath))
	} else {
		hl.SetResult("container", dstPath)
	}
	hl.SetResult("secrecy", secrecy)
	hl.SetResult("folder", true)
	hl.SetResult("entries", len(tree.Entries))

	if o.del || o.wipe {
		return fdsecDisposeTree(path, abs, tree, o, hl)
	}
	return nil
}

// fdsecDisposeTree carries out `del` or `wipe` on a packed folder - only now,
// after PackTreeFile read the whole container back (invariant 1), and only for
// what that read-back proved. The folder is walked again and compared with the
// packed manifest; if anything was added, removed or changed since, nothing is
// touched, because deleting a file that is not in the container is deleting it
// for good. The removal itself is entry by entry, deepest first, never a
// recursive delete: an entry it does not know stops it rather than going with
// the rest.
func fdsecDisposeTree(path, abs string, packed *fdsec.Tree, o *fdsecOpts, hl *HistoryLogger) error {
	kept := func(reason string) error {
		fmt.Printf("Original folder kept (%s): %s\n", reason, path)
		hl.SetResult("original", "kept")
		return nil
	}
	// The classification the folder wipe uses: a drive root, a share root, a
	// reparse point, the system TEMP. -y never waives it (invariant 9); here
	// there is no second confirmation to offer, so the answer is to keep it.
	if dangerous, reason := classifyWipeTarget(abs); dangerous {
		return kept(reason + "; remove it yourself if you mean it")
	}
	// Nothing is removed under a Stopped verdict (FDSEC-02).
	if runStopRequested() {
		kept("stopped before the original was removed; the container is complete and verified")
		return errFdsecStopped
	}
	now, err := fdsecScanTree(abs)
	if err != nil {
		return kept(fmt.Sprintf("it could not be walked again: %v", err))
	}
	if !fdsecSameTree(packed.Entries, now.Entries) {
		return kept("it changed after it was packed - the container holds the folder as it was, so nothing is removed")
	}

	entries := append([]fdsec.TreeEntry(nil), packed.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path > entries[j].Path }) // children before parents
	files := 0
	for _, e := range entries {
		if !e.IsDir() {
			files++
		}
	}

	if o.wipe {
		fmt.Printf("\nThe container was read back and every file verified against its digest.\n")
		fmt.Printf("WIPE will overwrite and remove every file of the original folder, then the folder itself:\n  %s (%d files, %d folders)\n", path, files, len(entries)-files)
		fmt.Printf("Honest caveat: on SSDs, copy-on-write and journaled volumes, overwrite-in-place\nlowers the odds of recovery but does not guarantee erasure.\n")
		if !o.assumeYes {
			fmt.Printf("Type WIPE to continue: ")
			line, rerr := readConsoleLine()
			if rerr != nil || strings.TrimSpace(line) != "WIPE" {
				return kept("wipe not confirmed")
			}
		}
	} else if !o.assumeYes {
		fmt.Printf("\nThe container was read back and every file verified against its digest.\n")
		fmt.Printf("Delete the original folder %s (%d files, %d folders)? This is a normal delete - the bytes stay recoverable until reused (y/N): ", path, files, len(entries)-files)
		line, rerr := readConsoleLine()
		ans := strings.ToLower(strings.TrimSpace(line))
		if rerr != nil || (ans != "y" && ans != "yes") {
			return kept("delete not confirmed")
		}
	}

	// Asked again after the prompt: the folder may have changed, or a stop
	// may have arrived, while the question waited.
	if runStopRequested() {
		kept("stopped before the original was removed; the container is complete and verified")
		return errFdsecStopped
	}
	if again, err := fdsecScanTree(abs); err != nil || !fdsecSameTree(packed.Entries, again.Entries) {
		return kept("it changed after it was packed - the container holds the folder as it was, so nothing is removed")
	}

	removed := 0
	for _, e := range entries {
		if runStopRequested() {
			return fmt.Errorf("stopped after removing %d of %d entries (the container is complete and verified): %w", removed, len(entries), errFdsecStopped)
		}
		p := filepath.Join(abs, filepath.FromSlash(e.Path))
		var rerr error
		switch {
		case e.IsDir():
			rerr = os.Remove(p)
		case o.wipe:
			if fi, serr := os.Stat(p); serr == nil && fi.Mode().Perm()&0o200 == 0 {
				_ = os.Chmod(p, fi.Mode().Perm()|0o200)
			}
			rerr = wipeFileInPlace(p)
		default:
			rerr = os.Remove(p)
		}
		if rerr != nil {
			return fmt.Errorf("removed %d of %d entries, then stopped (the container is complete and verified): %w", removed, len(entries), rerr)
		}
		removed++
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("every entry was removed but the folder itself was not (the container is complete and verified): %w", err)
	}
	if o.wipe {
		fmt.Printf("Original folder overwritten and removed: %s\n", path)
		hl.SetResult("original", "wiped")
	} else {
		fmt.Printf("Original folder removed (recoverable unlink; use wipe for the stronger form): %s\n", path)
		hl.SetResult("original", "deleted")
	}
	return nil
}

// fdsecSameTree compares two walks of one folder: same paths, kinds, sizes and
// modification times.
func fdsecSameTree(a, b []fdsec.TreeEntry) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(es []fdsec.TreeEntry) map[string]fdsec.TreeEntry {
		m := make(map[string]fdsec.TreeEntry, len(es))
		for _, e := range es {
			m[e.Path] = e
		}
		return m
	}
	mb := key(b)
	for _, e := range a {
		f, ok := mb[e.Path]
		if !ok || f.Kind != e.Kind || f.Size != e.Size || !f.ModifiedAt.Equal(e.ModifiedAt) {
			return false
		}
	}
	return true
}

// fdsecUnsecureTree restores a directory container (SP-0009 section 8). The
// tree is written into a fresh temporary folder beside the destination -
// same parent, so the same volume - and renamed into place as one directory
// only after the manifest and every file verified; an interrupted restore
// leaves an obviously-partial .fdsec-restore-* folder and never a half tree
// under the destination name. Overwrite is decided for the tree as a whole,
// before anything is written: an existing destination is never merged into.
func fdsecUnsecureTree(path string, c *fdsec.Container, src *os.File, containerSize int64, o *fdsecOpts, hl *HistoryLogger) error {
	tm, err := c.TreeMetadata()
	if err != nil {
		return err
	}
	finalPath, err := fdsecRestorePath(path, tm.Name, o)
	if err != nil {
		return fdsecScreenEventError(err)
	}
	finalPath, err = fdsecResolveTreeCollision(finalPath, o.assumeYes)
	if err != nil {
		return fdsecScreenEventError(err)
	}
	parent := filepath.Dir(finalPath)

	tmpDir, err := os.MkdirTemp(parent, ".fdsec-restore-*")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(tmpDir)
		}
	}()
	// Force exit only: a graceful stop ends the restore at a chunk boundary
	// and the defer above removes the temporary folder once nothing in it is
	// open (FDSEC-02).
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() && !done {
			os.RemoveAll(tmpDir)
		}
	})
	defer removeCleanup()

	tm, entries, err := c.UnpackTreeInto(tmpDir, newFdsecTracker("unsecure", containerSize), fdsecStreamStop())
	if fdsecStopped(err) {
		fmt.Printf("%sStopped: nothing was restored and the container is untouched.\n", fdsecEndProgress())
		return errFdsecStopped
	}
	if err != nil {
		return err
	}
	if runStopRequested() {
		fmt.Printf("%sStopped: nothing was restored and the container is untouched.\n", fdsecEndProgress())
		return errFdsecStopped
	}
	// Timestamps last, deepest first: writing a child moves its parent's
	// modification time, so a folder's own times are set after its contents.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		fdsecRestoreTimes(filepath.Join(tmpDir, filepath.FromSlash(e.Path)), fdsec.Metadata{CreatedAt: e.CreatedAt, AccessedAt: e.AccessedAt, ModifiedAt: e.ModifiedAt})
	}
	fdsecRestoreTimes(tmpDir, fdsec.Metadata{CreatedAt: tm.CreatedAt, AccessedAt: tm.AccessedAt, ModifiedAt: tm.ModifiedAt})

	// The destination was free when the question was asked; a rename onto a
	// name that appeared since would merge or fail, so ask the disk again.
	if _, serr := os.Lstat(finalPath); serr == nil {
		return fdsecScreenEventError(fmt.Errorf("%s appeared while the folder was being restored; nothing was moved into place", finalPath))
	}
	if err := os.Rename(tmpDir, finalPath); err != nil {
		return fdsecScreenEventError(err)
	}
	done = true

	files := 0
	for _, e := range entries {
		if !e.IsDir() {
			files++
		}
	}
	fmt.Printf("%sOK %s -> %s (folder of %d entries: %d files, %d folders, %s)\n", fdsecEndProgress(), path, finalPath, len(entries), files, len(entries)-files, formatBytes(uint64(tm.TotalSize)))
	// BEHAVIOUR 6.8: the folder's true name is sealed metadata, so the log
	// records the destination's parent, never the restored folder's name.
	hl.SetResult("restored", parent)
	hl.SetResult("folder", true)
	hl.SetResult("entries", len(entries))

	if o.del {
		return fdsecDeleteContainer(path, src, o, hl, "The restored folder was verified: the manifest and every file against their packed digests.")
	}
	return nil
}

// fdsecResolveTreeCollision is fdsecResolveCollision for a folder: the whole
// restore is decided before anything is written, and there is no overwrite
// answer - replacing an existing folder would mean deleting whatever is in it,
// which no restore should do on a keystroke. Under -y the name gains a
// numeric suffix; interactively the user may take the suffix or cancel.
func fdsecResolveTreeCollision(dstPath string, assumeYes bool) (string, error) {
	if _, err := os.Lstat(dstPath); os.IsNotExist(err) {
		return dstPath, nil
	} else if err != nil {
		return "", err
	}
	if assumeYes {
		suffixed, err := fdsecSuffixedName(dstPath, true)
		if err != nil {
			return "", err
		}
		fmt.Printf("Exists, -y given: restoring to %s instead\n", suffixed)
		return suffixed, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", usagef("%s already exists and stdin is not a terminal, so the question cannot be asked; pass -y to restore under a suffixed name, or name the destination with to <path>", dstPath)
	}
	fmt.Printf("\n%s already exists. A folder is never restored into or over an existing one.\n  s = restore under a suffixed name (default), anything else cancels: ", dstPath)
	line, err := readConsoleLine()
	if err != nil {
		return "", fmt.Errorf("collision at %s: %w", dstPath, err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "":
		// A folder's name has no extension, so a dot inside it is not split
		// off; the search is bounded and stops on any error (FDSEC-09).
		return fdsecSuffixedName(dstPath, true)
	default:
		return "", fmt.Errorf("cancelled: %s exists", dstPath)
	}
}
