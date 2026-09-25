package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filedo/fsx"
	"filedo/statedir"
)

// wipeCountCap bounds the pre-wipe object count so the confirmation prompt
// stays fast even on very large trees.
const wipeCountCap = 100000

// WipeProgress tracks wipe operation progress
type WipeProgress struct {
	TotalItems     int64
	ProcessedItems int64
	StartTime      time.Time
	CurrentItem    string
}

// handleFileWipeCommand is `filedo file <path> wipe`: a secure erase of one
// file - overwritten in place with random bytes, then removed.
//
// The word means two different strengths in this program, and the difference
// is not sloppiness. On a folder, `wipe` empties it: the entries go, the bytes
// stay recoverable until the space is reused. On a single file it means the
// stronger thing, because that is the only reading under which a verb of its
// own earns a place beside an ordinary delete - which Explorer already has.
//
// There is one implementation of the overwrite, wipeFileInPlace, shared with
// SP-0005's `secure wipe`; the Explorer group's "Wipe this file" is this entry
// point.
//
// --yes / --force skips the question, never the caveat: what an
// overwrite-in-place is worth on modern storage is information the user needs
// whether or not they automated the answer.
func handleFileWipeCommand(path string, args []string) error {
	force := false
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--yes", "-y", "--force", "--force-wipe", "/y":
			force = true
		}
	}
	if os.Getenv("FILEDO_AUTO_CONFIRM") == "1" {
		force = true
	}

	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%s is a folder: `folder %s wipe` empties a folder, this verb erases one file", path, path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	// A reparse point is a name pointing somewhere else. Overwriting through
	// one writes over whatever it happens to point at today, which is never
	// what the person clicking "Wipe this file" meant.
	if hasReparsePoint(path) {
		return fmt.Errorf("%s is a reparse point (junction/symlink/mount point) and is refused: it would overwrite whatever it points at", path)
	}

	fmt.Printf("\nWIPE will overwrite and then remove:\n  %s (%s)\n", wipeDisplayPath(path), formatBytes(uint64(fi.Size())))
	fmt.Printf("There is no container and no copy - the content is gone.\n")
	fmt.Printf("Honest caveat: on SSDs, copy-on-write and journaled volumes, overwrite-in-place\nlowers the odds of recovery but does not guarantee erasure.\n")
	if force {
		fmt.Printf("Confirmation skipped (--yes/--force).\n")
	} else {
		fmt.Printf("Type WIPE to continue: ")
		line, rerr := readConsoleLine()
		if rerr != nil || strings.TrimSpace(line) != "WIPE" {
			return fmt.Errorf("wipe cancelled by user")
		}
	}

	// A read-only file cannot be opened for writing, and the attribute is not
	// a permission decision - it is a flag the owner can clear themselves in
	// two clicks. Refusing over it would only teach them to clear it and try
	// again, having read the caveat once less.
	if fi.Mode().Perm()&0o200 == 0 {
		_ = os.Chmod(path, fi.Mode().Perm()|0o200)
	}
	if err := wipeFileInPlace(path); err != nil {
		return err
	}
	fmt.Printf("Overwritten and removed: %s\n", path)
	return nil
}

// The device verb means the root of the drive: DeviceHandler.Wipe passes its
// target through deviceRootPath (sysdrive_windows.go), because `D:` alone is
// D:'s per-process current directory to Windows, and `device D: wipe` after
// an earlier `cd D:\Work` used to empty D:\Work - with no prompt under
// --force, because that folder is not a root (WIPE-05).

func isDriveLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// wipeDisplayPath is the path a wipe shows and asks about: absolute and
// canonical - junctions, subst letters and 8.3 names resolved - never the
// relative or drive-relative words as typed (WIPE-05).
func wipeDisplayPath(p string) string {
	if canon, err := resolvedPath(p); err == nil && canon != "" {
		return canon
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// handleWipeCommand empties a folder: every entry inside it goes, the folder
// itself stays - with its access list, owner, encryption, compression,
// attributes and alternate streams (WIPE-04). Deleting and recreating it
// handed a private folder the parent's access list and stopped an EFS folder
// from encrypting what was written into it next.
func handleWipeCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no target specified for wipe operation")
	}

	// Parse optional automation flags; the first non-flag argument is the target.
	// --yes / -y / --force / --force-wipe skip the interactive prompt for normal
	// targets, but never bypass the dangerous-target guardrails below.
	force := false
	targetPath := ""
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--yes", "-y", "--force", "--force-wipe", "/y":
			force = true
		default:
			if targetPath == "" {
				targetPath = a
			}
		}
	}
	if targetPath == "" {
		return fmt.Errorf("no target specified for wipe operation")
	}
	if os.Getenv("FILEDO_AUTO_CONFIRM") == "1" {
		force = true
	}

	// Check if target exists
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("target path does not exist: %s", targetPath)
		}
		return fmt.Errorf("error accessing target path: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target must be a directory: %s", targetPath)
	}

	// A reparse point is refused outright, as the file wipe refuses one:
	// wiping it removed the link, left the data behind it untouched and
	// reported success (WIPE-03).
	if hasReparsePoint(trimTrailingSeparators(targetPath)) {
		return fmt.Errorf("%s is a reparse point (junction/symlink/mount point) and is refused: a wipe would act on the link, not on the folder it points at - wipe that folder by its own path", targetPath)
	}

	// Safety guardrails: require explicit confirmation before any destruction.
	if err := confirmWipe(targetPath, force); err != nil {
		return err
	}

	fmt.Printf("Wiping contents of: %s\n", wipeDisplayPath(targetPath))
	startTime := time.Now()
	if err := wipeContents(targetPath); err != nil {
		return err
	}
	fmt.Printf("\nWipe completed in %s - the folder itself is kept\n", formatDuration(time.Since(startTime)))
	return nil
}

// trimTrailingSeparators drops trailing separators except from a root, so an
// attribute query names the entry itself.
func trimTrailingSeparators(p string) string {
	t := strings.TrimRight(p, `\/`)
	if t == "" || (len(t) == 2 && t[1] == ':') {
		return p
	}
	return t
}

// wipeContents deletes every entry inside targetPath and never the folder
// itself. Every entry that survives is counted, and a wipe that left any
// behind is an error naming the count (WIPE-06) - a locked file used to
// survive a wipe that reported success.
func wipeContents(targetPath string) error {
	progress := &WipeProgress{StartTime: time.Now()}
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return fmt.Errorf("cannot list %s: %v", targetPath, err)
	}
	progress.TotalItems = int64(len(entries))
	fmt.Printf("Deleting %d top-level entries..\n", len(entries))

	failed := 0
	var firstFailures []string
	for _, e := range entries {
		if runStopRequested() {
			fmt.Printf("\nStopped: %d of %d entries processed.\n", progress.ProcessedItems, progress.TotalItems)
			return nil
		}
		p := filepath.Join(targetPath, e.Name())
		progress.CurrentItem = p
		progress.ProcessedItems++
		if progress.ProcessedItems%100 == 0 {
			showWipeProgress(progress)
		}
		// RemoveAll does not follow links inside the tree: a junction in
		// the folder goes, what it points at stays.
		if err := removeAllClearingReadOnly(p); err != nil {
			failed++
			if len(firstFailures) < 10 {
				firstFailures = append(firstFailures, fmt.Sprintf("%s: %v", p, err))
			}
		}
	}
	if failed > 0 {
		fmt.Printf("\nCould not delete %d of %d entries:\n", failed, len(entries))
		for _, f := range firstFailures {
			fmt.Printf("  %s\n", f)
		}
		return fmt.Errorf("wipe incomplete: %d of %d entries could not be deleted (in use, locked or access denied)", failed, len(entries))
	}
	return nil
}

// removeAllClearingReadOnly is os.RemoveAll, retried once after clearing the
// read-only attribute in the tree - the attribute is a flag, not a refusal.
func removeAllClearingReadOnly(p string) error {
	err := os.RemoveAll(p)
	if err == nil {
		return nil
	}
	_ = filepath.Walk(p, func(path string, info os.FileInfo, werr error) error {
		if werr == nil && info != nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o200 == 0 {
			_ = os.Chmod(path, info.Mode().Perm()|0o200)
		}
		return nil
	})
	return os.RemoveAll(p)
}

// showWipeProgress displays current wipe progress
func showWipeProgress(progress *WipeProgress) {
	currentItem := progress.CurrentItem
	if len(currentItem) > 60 {
		currentItem = "..." + string(os.PathSeparator) + filepath.Base(currentItem)
	}

	elapsed := time.Since(progress.StartTime)
	itemsPerSecond := float64(progress.ProcessedItems) / elapsed.Seconds()

	eta := "unknown"
	if itemsPerSecond > 0 {
		remainingItems := progress.TotalItems - progress.ProcessedItems
		etaSeconds := int64(float64(remainingItems) / itemsPerSecond)
		eta = formatETA(time.Duration(etaSeconds) * time.Second)
	}

	fmt.Printf("\rWiping: %s [%d/%d entries, %.0f entries/sec, ETA: %s]",
		currentItem, progress.ProcessedItems, progress.TotalItems, itemsPerSecond, eta)
}

// confirmWipe shows the target, a best-effort object count and requires explicit
// confirmation before a wipe proceeds. Dangerous targets (drive/share roots,
// reparse points, system and profile folders) always require strong,
// interactive confirmation and are never bypassed by the --force flag.
func confirmWipe(targetPath string, force bool) error {
	dangerous, reason := classifyWipeTarget(targetPath)
	display := wipeDisplayPath(targetPath)

	fmt.Printf("\nWIPE will permanently delete everything inside:\n  %s\n", display)
	fmt.Printf("The folder itself, its access list and its attributes are kept.\n")

	// Best-effort, bounded count so huge trees do not stall the prompt.
	if count, capped := quickCountWipeItems(targetPath); count >= 0 {
		suffix := ""
		if capped {
			suffix = "+"
		}
		fmt.Printf("Found %d%s item(s) to delete.\n", count, suffix)
	}

	if dangerous {
		fmt.Printf("\n!!! DANGEROUS TARGET: %s\n", reason)
		fmt.Printf("This is a high-risk location. Extra confirmation is required.\n")
		// --force intentionally does NOT bypass the dangerous-target guardrail.
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Type WIPE to continue: ")
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) != "WIPE" {
			return fmt.Errorf("wipe cancelled by user")
		}
		fmt.Printf("Type the exact target path to confirm (%s): ", display)
		line, _ = reader.ReadString('\n')
		typed := strings.TrimSpace(line)
		if typed == "" || !(typed == strings.TrimSpace(targetPath) || strings.EqualFold(typed, display)) {
			return fmt.Errorf("wipe cancelled: target path confirmation did not match")
		}
		return nil
	}

	if force {
		fmt.Printf("Confirmation skipped (--yes/--force).\n")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Type WIPE to continue: ")
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != "WIPE" {
		return fmt.Errorf("wipe cancelled by user")
	}
	return nil
}

// classifyWipeTarget reports whether a wipe target is a high-risk location and
// why (WIPE-01, WIPE-02).
//
// Every rule is decided on what the path names, not on how it was spelled:
// first on the spelling alone, for the roots that need no file system to
// recognise - `D:`, `D:/`, `D:\.`, `D:\ `, `\\srv\share`, `//srv/share`,
// `\\?\C:\`, `\\.\C:\`, `\\?\UNC\srv\share\`, `\\?\GLOBALROOT\Device\..`,
// `\\?\Volume{..}\` - and then on the canonical path and the file ID, which
// catch the second spellings of a protected folder: `\\localhost\C$\..`, an
// 8.3 short name, a subst letter, a junction.
func classifyWipeTarget(targetPath string) (dangerous bool, reason string) {
	if r := wipeRootBySpelling(targetPath); r != "" {
		return true, r
	}

	// Reparse point / junction / symlink / mount point.
	if hasReparsePoint(trimTrailingSeparators(targetPath)) {
		return true, "target is a reparse point (junction/symlink/mount point)"
	}

	// The root of a volume or of a mount point, whatever the spelling.
	if id, err := pathIdentityOf(targetPath); err == nil {
		if id.IsVolumeRoot() {
			return true, "target is the root of a volume (" + id.Volume + ")"
		}
		if r := protectedByIdentity(id); r != "" {
			return true, r
		}
	}
	if canon, err := resolvedPath(targetPath); err == nil {
		if vol, verr := fsx.VolumePathName(canon); verr == nil && strings.EqualFold(ensureSep(canon), vol) {
			return true, "target is the root of a volume (" + vol + ")"
		}
		if r := protectedByCanonical(canon); r != "" {
			return true, r
		}
	}
	return false, ""
}

func ensureSep(p string) string {
	if strings.HasSuffix(p, `\`) {
		return p
	}
	return p + `\`
}

// wipeRootBySpelling recognises a root from its spelling alone. Windows drops
// trailing spaces and dots from a name, so `D:\ ` and `D:\.` are `D:\`.
func wipeRootBySpelling(p string) string {
	s := strings.ReplaceAll(strings.TrimSpace(p), "/", `\`)
	s = strings.TrimRight(s, " .")
	upper := strings.ToUpper(s)

	// Device-namespace prefixes: \\?\ and \\.\ .
	if strings.HasPrefix(upper, `\\?\`) || strings.HasPrefix(upper, `\\.\`) {
		rest := s[4:]
		restUpper := upper[4:]
		switch {
		case strings.HasPrefix(restUpper, `UNC\`):
			s = `\\` + rest[4:]
			upper = strings.ToUpper(s)
		case strings.HasPrefix(restUpper, `GLOBALROOT`):
			parts := splitNonEmpty(rest[len(`GLOBALROOT`):])
			// \\?\GLOBALROOT\Device\HarddiskVolumeN is a volume itself.
			if len(parts) <= 2 {
				return "target is the root of a volume (" + p + ")"
			}
			return ""
		case strings.HasPrefix(restUpper, `VOLUME{`):
			if len(splitNonEmpty(rest)) <= 1 {
				return "target is the root of a volume (" + p + ")"
			}
			return ""
		case len(rest) >= 2 && rest[1] == ':' && isDriveLetter(rest[0]):
			s = rest
			upper = strings.ToUpper(s)
		default:
			if len(splitNonEmpty(rest)) <= 1 {
				return "target is a device path (" + p + ")"
			}
			return ""
		}
	}

	// `D:` alone means D:'s current directory to Windows; to a wipe it is
	// the drive, and a drive is a root.
	if len(s) == 2 && s[1] == ':' && isDriveLetter(s[0]) {
		return "target is the root of a drive"
	}
	if len(s) >= 2 && s[1] == ':' && isDriveLetter(s[0]) {
		c := filepath.Clean(s)
		if len(c) <= 3 {
			return "target is the root of a drive"
		}
		return ""
	}

	// \\server\share or \\server
	if strings.HasPrefix(s, `\\`) {
		if len(splitNonEmpty(s[2:])) <= 2 {
			return "target is the root of a network share"
		}
	}
	return ""
}

func splitNonEmpty(p string) []string {
	var out []string
	for _, part := range strings.Split(p, `\`) {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

// wipeProtectedPath is a folder whose loss breaks the system, the profile or
// FileDO itself; a wipe of it, or of any folder above it, is dangerous.
type wipeProtectedPath struct {
	path  string
	label string
	// inside: a wipe of anything below it is dangerous too.
	inside bool
}

// wipeProtectedPaths lists the protected folders of this machine and user.
func wipeProtectedPaths() []wipeProtectedPath {
	var out []wipeProtectedPath
	add := func(p, label string, inside bool) {
		if strings.TrimSpace(p) != "" {
			out = append(out, wipeProtectedPath{path: p, label: label, inside: inside})
		}
	}
	add(os.Getenv("TEMP"), "the TEMP folder", false)
	add(os.Getenv("TMP"), "the TMP folder", false)
	add(os.TempDir(), "the system TEMP folder", false)
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = os.Getenv("windir")
	}
	add(sysRoot, "the Windows folder", true)
	if sysRoot != "" {
		add(filepath.Join(sysRoot, "Temp"), "the Windows TEMP folder", false)
	}
	profile := os.Getenv("USERPROFILE")
	add(profile, "the user profile", false)
	if profile != "" {
		add(filepath.Dir(profile), "the profiles folder", false)
	}
	if sd := os.Getenv("SystemDrive"); sd != "" {
		add(sd+`\Users`, "the profiles folder", false)
	}
	add(os.Getenv("ProgramFiles"), "Program Files", true)
	add(os.Getenv("ProgramFiles(x86)"), "Program Files (x86)", true)
	add(os.Getenv("ProgramW6432"), "Program Files", true)
	if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
		add(filepath.Join(lad, "FileDO"), "FileDO's own data folder", true)
	}
	if st, err := statedir.Dir(); err == nil {
		add(st, "FileDO's state folder", false)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		add(filepath.Dir(exe), "the folder FileDO runs from", false)
	}
	return out
}

// protectedByCanonical compares canonical spellings: the target is the
// protected folder or above it (or, for some, below it).
func protectedByCanonical(canonTarget string) string {
	for _, pp := range wipeProtectedPaths() {
		canon, err := resolvedPath(pp.path)
		if err != nil {
			continue
		}
		if canonicalWithin(canon, canonTarget) {
			if strings.EqualFold(strings.TrimRight(canon, `\`), strings.TrimRight(canonTarget, `\`)) {
				return "target is " + pp.label + " (" + canon + ")"
			}
			return "target contains " + pp.label + " (" + canon + ")"
		}
		if pp.inside && canonicalWithin(canonTarget, canon) {
			return "target is inside " + pp.label + " (" + canon + ")"
		}
	}
	return ""
}

// protectedByIdentity compares file IDs: the target is a protected folder or
// one of its ancestors under any spelling at all.
func protectedByIdentity(target PathIdentity) string {
	for _, pp := range wipeProtectedPaths() {
		cur := pp.path
		if abs, err := filepath.Abs(cur); err == nil {
			cur = abs
		}
		first := true
		for {
			if id, err := pathIdentityOf(cur); err == nil && id.SameObject(target) {
				if first {
					return "target is " + pp.label + " (" + pp.path + ")"
				}
				return "target contains " + pp.label + " (" + pp.path + ")"
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
			first = false
		}
	}
	return ""
}

// quickCountWipeItems returns a bounded count of items under targetPath. The
// count is capped at wipeCountCap so the confirmation prompt stays responsive on
// very large trees; capped is true when the cap was reached.
func quickCountWipeItems(targetPath string) (count int, capped bool) {
	_ = filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore errors while counting
		}
		if path == targetPath {
			return nil // don't count the root itself
		}
		count++
		if count >= wipeCountCap {
			capped = true
			return filepath.SkipAll
		}
		return nil
	})
	return count, capped
}
