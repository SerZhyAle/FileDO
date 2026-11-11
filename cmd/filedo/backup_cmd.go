package main

import (
	"archive/zip"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"filedo/backupzip"
	"filedo/fdsec"
	"filedo/fsx"
)

// The backup pair (SP-0198):
//
//	filedo backup <folder|drive|\\server\share\folder> [to <dir-or-file>] [level <0-9>]
//	             [sec] [keep-zip] [root] [skip-links] [p:<pw>|pf:<file>|pe:<VAR>|k:<keyfile>] [y]
//	filedo restore <archive.zip|archive.fd-sec> [to <dir>] [suffix] [del]
//	              [p:<pw>|pf:<file>|pe:<VAR>|k:<keyfile>] [y]
//
// backup makes a restorable copy, not a backup product: no schedule, no
// catalogue, each copy whole (spec 3.2). The result is a plain zip that opens
// in any zip tool, or - with sec - that zip sealed into one .fd-sec. Both verbs
// speak the supervisor vocabulary of CLI-EVENT-STREAM rule 11; a container that
// does not open ends with its own class, 3, 4 or 6, which rule 11 does not use
// (FDSEC-BEHAVIOUR 1.9 section 21 item 4).

// backupOptionWords are the words the two verbs take as options. The redactor
// reads the same table, so a word kept in history is one the parser knows.
var backupOptionWords = map[string]bool{
	"sec": true, "keep-zip": true, "root": true, "skip-links": true,
	"suffix": true, "del": true, "delete": true,
	"y": true, "-y": true, "--force": true, "force": true,
	// Read by the history logger, not by the verb (NewHistoryLogger).
	"nohist": true, "no_history": true,
}

// backupOpts is the parsed command line of either verb.
type backupOpts struct {
	src       string
	to        string
	haveTo    bool
	level     int // -1: the probe chooses
	sec       bool
	keepZip   bool
	root      bool
	skipLinks bool
	suffix    bool
	del       bool
	yes       bool
	cred      credArg
}

// parseBackupArgs reads the words after the verb. The first word that is not
// an option is the source; any other word is refused without being quoted,
// because it may be a password typed without its p: (CLI-EVENT-STREAM rule 13).
func parseBackupArgs(verb string, args []string) (*backupOpts, error) {
	o := &backupOpts{level: -1}
	for i := 0; i < len(args); i++ {
		t := args[i]
		lt := strings.ToLower(t)
		switch {
		case lt == "to":
			if i+1 >= len(args) {
				return nil, exitClassf(10, "to needs a destination")
			}
			o.to, o.haveTo = args[i+1], true
			i++
		case lt == "level" && verb == "backup":
			if i+1 >= len(args) {
				return nil, exitClassf(10, "level needs a number from 0 (store, no compression) to 9 (densest)")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 || n > 9 {
				return nil, exitClassf(10, "level takes a number from 0 (store, no compression) to 9 (densest)")
			}
			o.level = n
			i++
		case backupOptionWords[lt]:
			switch lt {
			case "sec":
				o.sec = true
			case "keep-zip":
				o.keepZip = true
			case "root":
				o.root = true
			case "skip-links":
				o.skipLinks = true
			case "suffix":
				o.suffix = true
			case "del", "delete":
				o.del = true
			case "nohist", "no_history":
			default:
				o.yes = true
			}
		case backupCredentialWord(t, o.src == ""):
			if o.cred.given() {
				return nil, exitClassf(10, "one password source only: p:, pf:, pe: or k:")
			}
			a, _ := credentialToken(t)
			o.cred = a
		case o.src == "":
			o.src = t
		default:
			return nil, exitClassf(10, "%s takes one %s and the words listed by filedo -?; a second word that is not an option was given (it is not repeated here, in case it is a password - a password is written p:<password>)", verb, map[string]string{"backup": "source", "restore": "archive"}[verb])
		}
	}
	if o.src == "" {
		if verb == "backup" {
			return nil, exitClassf(10, "backup needs a source: filedo backup <folder|drive|\\\\server\\share\\folder>")
		}
		return nil, exitClassf(10, "restore needs an archive: filedo restore <backup.zip|backup.fd-sec>")
	}
	if verb == "backup" {
		switch {
		case o.suffix || o.del:
			return nil, exitClassf(10, "suffix and del are restore words; backup never changes the source")
		case o.keepZip && !o.sec:
			return nil, exitClassf(10, "keep-zip keeps the zip a sealed backup was made from; it needs sec")
		case o.cred.given() && !o.sec:
			return nil, exitClassf(10, "a password seals the backup, which needs sec: filedo backup <source> sec pe:<VAR>")
		}
	} else if o.sec || o.keepZip || o.root || o.skipLinks {
		return nil, exitClassf(10, "sec, keep-zip, root and skip-links are backup words; restore finds out by itself whether the archive is sealed")
	}
	return o, nil
}

// backupCredentialWord reports whether t is a password source. `p:\..` and
// `p:/..` are paths on drive P: wherever they stand (FDSEC-BEHAVIOUR section
// 8.2), and while the source is still missing, `k:\..` is drive K: too - the
// source comes first, so a drive spelled there is the source.
func backupCredentialWord(t string, sourceMissing bool) bool {
	if !isCredentialToken(t) {
		return false
	}
	if strings.HasPrefix(t, "p:") {
		return fdsecCredentialToken(t)
	}
	driveShaped := len(t) >= 3 && t[1] == ':' && (t[2] == '\\' || t[2] == '/')
	return !(sourceMissing && driveShaped)
}

// handleBackupCommand is `filedo backup ..`.
func handleBackupCommand(args []string, hl *HistoryLogger) error {
	o, err := parseBackupArgs("backup", args)
	target := ""
	if o != nil {
		target = o.src
	}
	hl.SetCommand("backup", target, "backup")
	beginRun(runActs, "backup", target, append([]string{"backup"}, args...))
	if err != nil {
		return err
	}
	return runBackup(o, hl)
}

// handleRestoreCommand is `filedo restore ..`.
func handleRestoreCommand(args []string, hl *HistoryLogger) error {
	o, err := parseBackupArgs("restore", args)
	target := ""
	if o != nil {
		target = o.src
	}
	hl.SetCommand("restore", target, "restore")
	beginRun(runActs, "restore", target, append([]string{"restore"}, args...))
	if err != nil {
		return err
	}
	return runRestore(o, hl)
}

// backupStopped is the error a stop ends either verb with: the verdict is
// Stopped and nothing is left under a final name (spec 3.7).
func backupStopped(what string) error {
	fmt.Printf("\nStopped: %s\n", what)
	return fmt.Errorf("%w: %s", errRunStopped, what)
}

// backupIsRoot reports whether a folder is the root of a volume or a share,
// by spelling and then by identity.
func backupIsRoot(p string) bool {
	if wipeRootBySpelling(p) != "" {
		return true
	}
	id, err := pathIdentityOf(p)
	return err == nil && id.IsVolumeRoot()
}

// backupSourceName is the name a backup records and is named after: the
// folder's own name, `D-drive` for a drive root, the share's name for a share
// root.
func backupSourceName(abs string, root bool) string {
	if root {
		if v := filepath.VolumeName(abs); len(v) == 2 && v[1] == ':' {
			return strings.ToUpper(v[:1]) + "-drive"
		}
		if parts := splitNonEmpty(strings.TrimPrefix(abs, `\\`)); len(parts) >= 2 {
			return parts[1]
		}
		return "share"
	}
	return filepath.Base(abs)
}

func runBackup(o *backupOpts, hl *HistoryLogger) error {
	src := deviceRootPath(o.src)
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("the source %s cannot be read: %w", abs, err)
	}
	if !fi.IsDir() {
		return exitClassf(10, "%s is a file: backup packs a folder, a drive or a network folder; one file is protected with: filedo \"%s\" secure", abs, abs)
	}
	if fdsecRefusedReparse(abs) {
		return exitClassf(10, "%s is a junction or a symbolic link: back up the folder it points to", abs)
	}
	root := backupIsRoot(abs)
	switch {
	case root && isSystemVolumeRoot(abs):
		return exitClassf(10, "%s is the drive Windows runs from: a whole system drive is not backed up (files Windows holds open cannot be copied consistently) - back up the folders you need, such as your user folder", abs)
	case root && !o.root:
		return exitClassf(10, "%s is the root of a drive or a share: add the word root to back it up whole - the size is shown and confirmed before anything is written", abs)
	case !root && o.root:
		return exitClassf(10, "root is for the root of a drive or a share; %s is a folder - drop root", abs)
	}
	name := backupSourceName(abs, root)

	// Where the zip goes: beside the folder, in the current folder for a root,
	// or wherever `to` says (D2). An existing name is never replaced.
	zipName := name + "-" + time.Now().Format("0601021504") + ".zip"
	outDir := filepath.Dir(abs)
	if root {
		if outDir, err = os.Getwd(); err != nil {
			return err
		}
	}
	zipPath := filepath.Join(outDir, zipName)
	if o.haveTo {
		to, err := filepath.Abs(o.to)
		if err != nil {
			return err
		}
		st, serr := os.Stat(to)
		switch {
		case serr == nil && st.IsDir():
			zipPath = filepath.Join(to, zipName)
		case serr == nil:
			return exitClassf(10, "%s already exists; a backup never replaces a file - name a folder or a new file", to)
		case errors.Is(serr, os.ErrNotExist):
			if pst, perr := os.Stat(filepath.Dir(to)); perr != nil || !pst.IsDir() {
				return exitClassf(11, "the folder %s does not exist; to names an existing folder, or a new file inside one", filepath.Dir(to))
			}
			zipPath, err = backupZipPath(to, o.sec)
			if err != nil {
				return err
			}
		default:
			return serr
		}
	}
	containerPath := strings.TrimSuffix(zipPath, filepath.Ext(zipPath)) + fdsecExtension
	final := zipPath
	if o.sec {
		final = containerPath
	}
	if !o.sec || o.keepZip {
		if _, err := os.Lstat(zipPath); err == nil {
			return exitClassf(10, "%s already exists; a backup never replaces a file - name another destination with to", zipPath)
		}
	}
	if o.sec {
		if _, err := os.Lstat(containerPath); err == nil {
			return exitClassf(10, "%s already exists; a backup never replaces a file - name another destination with to", containerPath)
		}
	}
	// A backup written into the folder it packs would pack itself (spec 3.3).
	if inside, err := fsx.Within(filepath.Dir(zipPath), abs); err != nil {
		return err
	} else if inside {
		return exitClassf(10, "the backup would be written inside %s, the folder it packs; name a place outside it with to", abs)
	}

	runStep("walk", "Listing the files the backup carries")
	w, err := backupzip.Walk(abs, backupzip.WalkOptions{
		IsLink: fdsecRefusedReparse, SkipLinks: o.skipLinks, SkipSystem: root, Stop: runStopRequested,
	})
	if errors.Is(err, backupzip.ErrStopped) {
		return backupStopped("nothing was written; the source is untouched")
	}
	var linkErr *backupzip.LinkError
	if errors.As(err, &linkErr) {
		return exitClassf(10, "%v", err)
	}
	if err != nil {
		return err
	}
	for _, s := range w.SkippedSystem {
		fmt.Printf("Skipped (a Windows system file, not user data): %s\n", s)
	}
	for _, l := range w.SkippedLinks {
		fmt.Printf("Skipped link (skip-links; what it points to is not in the backup): %s\n", l)
		EmitFindingEvent("info", "Link skipped; what it points to is not in the backup: "+l, map[string]interface{}{"skippedLink": l})
	}
	if n := len(w.SkippedLinks); n > 0 {
		fmt.Printf("%d link(s) left out: this backup is not the whole tree on disk.\n", n)
	}

	level, saving := o.level, 0.0
	if level < 0 {
		runStep("probe", "Choosing the zip density")
		p, err := backupzip.Probe(w, nil, runStopRequested)
		if errors.Is(err, backupzip.ErrStopped) {
			return backupStopped("nothing was written; the source is untouched")
		}
		if err != nil {
			return err
		}
		level, saving = p.Level, p.ChosenSaving()
		note := ""
		if p.TimedOut {
			note = "; the 10 s probe ended before every level was tried"
		}
		fmt.Printf("density: level %d (probe %.1f s, %d files, saving %.0f %%%s)\n", level, p.Seconds, p.Files, saving, note)
		runNumber("probeSeconds", p.Seconds)
	} else {
		fmt.Printf("density: level %d (named with level; no probe)\n", level)
	}
	if saving < 0 || level == 0 {
		saving = 0
	}

	// The estimate, and the space it needs: the zip, and with sec the
	// container beside it until the zip is removed (spec 3.5).
	projected := int64(float64(w.Bytes)*(1-saving/100)) + int64(len(w.Entries))*256 + 4096
	need := projected
	if o.sec {
		need = 2*projected + 64<<10
	}
	fmt.Printf("estimate: %s files, %s, about %s written to %s\n", formatCount(w.Files), formatBytes(uint64(w.Bytes)), formatBytes(uint64(need)), final)
	if free, ferr := fdsecFreeSpaceFor(filepath.Dir(zipPath)); ferr == nil && free < need {
		return exitClassf(13, "%s has %s free and this backup needs about %s; free some space or name another destination with to", filepath.Dir(zipPath), formatBytes(uint64(free)), formatBytes(uint64(need)))
	}

	// Consent for a whole drive or share, after the estimate (D6); y skips
	// the question, never a check above.
	if root && !o.yes {
		fmt.Printf("Back up the whole of %s into %s? (y/N): ", abs, final)
		line, rerr := readConsoleLine()
		if rerr != nil {
			return exitClassf(10, "backing up a whole drive or share needs a confirmation: nobody could answer the question, nothing was written - pass y to go ahead without it")
		}
		if ans := strings.ToLower(strings.TrimSpace(line)); ans != "y" && ans != "yes" {
			fmt.Println("Cancelled: nothing was written.")
			hl.SetResult("backup", "cancelled")
			return nil
		}
	}

	// The credential is asked last, after every check: a refusal that comes
	// after the password was typed wastes a secret (FDSEC-BEHAVIOUR section 8.1).
	var cred fdsec.Credential
	if o.sec {
		cred, err = resolveCredential(o.cred, true, "Password (no echo; empty = obfuscation only, no secrecy): ")
		if err != nil {
			return err
		}
		if len(cred) == 0 {
			fmt.Println("Note: the password is EMPTY - the sealed backup is obfuscation only, with no secrecy: anyone can open it.")
		}
	}

	runStep("pack", "Packing the zip")
	tracker := NewProgressTracker(w.Files, w.Bytes)
	res, err := backupzip.Pack(zipPath, w, backupzip.PackOptions{
		Level: level, Name: name, Stop: runStopRequested, Keep: o.sec,
		RegisterCleanup: func(cleanup func()) func() {
			return globalInterruptHandler.AddCleanup(func() {
				if globalInterruptHandler.IsForceExit() {
					cleanup()
				}
			})
		},
		Progress: func(files, bytes int64) {
			tracker.Update(files, bytes)
			tracker.PrintProgress("pack")
		},
	})
	if errors.Is(err, backupzip.ErrStopped) {
		return backupStopped("nothing was written; the source is untouched")
	}
	if err != nil {
		return err
	}
	fmt.Println()
	runNumber("files", res.Files)
	runNumber("bytes", res.Bytes)
	runNumber("level", level)
	if !o.sec {
		runNumber("archiveBytes", res.Size)
		fmt.Printf("OK %s -> %s (level %d, %s files, %s -> %s)\n", abs, res.Path, level, formatCount(res.Files), formatBytes(uint64(res.Bytes)), formatBytes(uint64(res.Size)))
		hl.SetResult("archive", res.Path)
		return nil
	}
	return backupSeal(res, zipPath, containerPath, cred, o, hl)
}

// backupZipPath keeps the zip's sealed name usable by restore, including when
// the requested output is the final .fd-sec name rather than the zip name.
func backupZipPath(path string, sealed bool) (string, error) {
	ext := filepath.Ext(path)
	switch {
	case ext == "":
		return path + ".zip", nil
	case strings.EqualFold(ext, ".zip"):
		return path, nil
	case sealed && strings.EqualFold(ext, fdsecExtension):
		return strings.TrimSuffix(path, ext) + ".zip", nil
	default:
		return "", exitClassf(10, "a backup filename must end in .zip%s", map[bool]string{true: " or .fd-sec when sealing", false: ""}[sealed])
	}
}

// backupSeal seals the finished zip into one container (spec 3.5): the
// container goes through a temporary name and the read-back of
// fdsec.PackFile, and the zip is removed only after that proof, unless
// keep-zip asked to keep it (FDSEC-BEHAVIOUR 1.9 section 21 item 1).
func backupSeal(res backupzip.PackResult, zipPath, containerPath string, cred fdsec.Credential, o *backupOpts, hl *HistoryLogger) error {
	runStep("seal", "Sealing the zip into a .fd-sec container")
	dropZip := func() {
		os.Remove(res.Path)
	}
	zfi, err := os.Stat(res.Path)
	if err != nil {
		dropZip()
		return err
	}
	meta := fdsecMetadataFromStat(zfi)
	// The sealed name is the zip's own, never the partial name it waited under.
	meta.Name = filepath.Base(zipPath)
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			removeFdsecPartials(containerPath)
			os.Remove(res.Path)
		}
	})
	defer removeCleanup()
	info, err := fdsec.PackFile(containerPath, res.Path, meta, cred, fdsec.Params{}, newFdsecTracker("seal", zfi.Size()), fdsecStreamStop())
	if fdsecStopped(err) {
		dropZip()
		return backupStopped(fdsecEndProgress() + "no container was written, and the unsealed zip was removed")
	}
	if err != nil {
		dropZip()
		return err
	}
	if o.keepZip {
		if err := fsx.RenameNoReplace(res.Path, zipPath); err != nil {
			dropZip()
			return fmt.Errorf("the sealed backup was written to %s, but the requested zip could not be kept: %w", containerPath, err)
		}
		res.Path = zipPath
	} else {
		dropZip()
	}
	secrecy := "encrypted"
	if len(cred) == 0 {
		secrecy = "obfuscation only - NO SECRECY (empty password)"
	}
	runNumber("archiveBytes", info.TotalLen)
	fmt.Printf("%sOK -> %s (sealed, suite 1, %s; %s files, %s -> container %s)\n", fdsecEndProgress(), containerPath, secrecy, formatCount(res.Files), formatBytes(uint64(res.Bytes)), formatBytes(uint64(info.TotalLen)))
	if o.keepZip {
		fmt.Printf("The zip was kept, as asked: %s - it is NOT protected; anyone who has it can open it.\n", res.Path)
		hl.SetResult("zip", res.Path)
	}
	hl.SetResult("archive", containerPath)
	hl.SetResult("secrecy", secrecy)
	return nil
}

// errNotBackupZip is a container that opened and holds something other than a
// zip: restore refuses it by name before writing, never printing the sealed
// name (FDSEC-BEHAVIOUR 1.9 section 21 item 3).
var errNotBackupZip = errors.New("the file sealed in the container is not a backup zip")

// backupContainerClass gives a container that did not open its own class, 3,
// 4 or 6; every other error keeps rule 11's (section 21 item 4).
func backupContainerClass(err error) {
	if errors.Is(err, fdsec.ErrCredentialOrTamper) || errors.Is(err, fdsec.ErrDamaged) || errors.Is(err, fdsec.ErrUnsupported) {
		fdsecSetExit(err)
	}
}

func runRestore(o *backupOpts, hl *HistoryLogger) error {
	if strings.ContainsAny(filepath.Base(o.src), "*?") {
		return exitClassf(10, "restore takes one archive, never a mask")
	}
	path, err := filepath.Abs(o.src)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the archive %s cannot be read: %w", path, err)
	}
	if fi.IsDir() {
		return exitClassf(10, "%s is a folder: restore takes a backup .zip or .fd-sec file", path)
	}
	sealed := strings.EqualFold(filepath.Ext(path), fdsecExtension)
	if !sealed && o.cred.given() {
		return exitClassf(10, "%s is a plain zip and needs no password; drop the password", path)
	}
	parent, exact, err := restoreDestination(o)
	if err != nil {
		return err
	}
	if parent == "" {
		parent = filepath.Dir(path)
	}

	zipPath := path
	if sealed {
		tmp, err := restoreUnseal(path, parent, o)
		if err != nil {
			return err
		}
		// Registered before the reader below is closed, so it runs after it:
		// an open file cannot be removed on Windows.
		defer os.Remove(tmp)
		zipPath = tmp
	}
	// An insecure name is reported by the reader under GODEBUG
	// zipinsecurepath=0 with the archive still open; the screening below
	// refuses it in its own words.
	zr, err := zip.OpenReader(zipPath)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return defectf("%s is not a zip archive that can be read (%v); nothing was written", path, err)
	}
	defer zr.Close()
	entries, err := backupzip.ScreenEntries(zr.File)
	if err != nil {
		return defectf("%s: %v", path, err)
	}
	name := backupzip.SourceName(zr.Comment)
	if name == "" {
		// A zip FileDO did not write: the folder is named after the archive,
		// through the same name screening as a recorded name.
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if backupzip.SourceName(backupzip.CommentPrefix+name) != name {
			name = "restored"
		}
	}
	dest := exact
	if dest == "" {
		dest = filepath.Join(parent, name)
	}
	dest, reuseEmpty, err := restorePickFree(dest, o)
	if err != nil {
		return err
	}
	if dest == "" {
		fmt.Println("Cancelled: nothing was written.")
		hl.SetResult("restore", "cancelled")
		return nil
	}

	files, bytes := backupzip.TotalSize(entries)
	if free, ferr := fdsecFreeSpaceFor(filepath.Dir(dest)); ferr == nil && uint64(free) < bytes+1<<20 {
		return exitClassf(13, "%s has %s free and the archive holds %s; free some space or name another place with to", filepath.Dir(dest), formatBytes(uint64(free)), formatBytes(bytes))
	}
	fmt.Printf("restore: %s files, %s -> %s\n", formatCount(files), formatBytes(bytes), dest)

	// The tree is written under a partial name and renamed into place when it
	// is complete, so a stop or a failure leaves nothing under the final name.
	partial := dest + fsx.PartialSuffix
	if err := os.Mkdir(partial, 0o755); err != nil {
		return fmt.Errorf("cannot create the temporary restore folder %s; an existing folder is never removed: %w", partial, err)
	}
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			os.RemoveAll(partial)
		}
	})
	defer removeCleanup()
	runStep("unpack", "Restoring the files")
	tracker := NewProgressTracker(files, int64(bytes))
	n, b, err := backupzip.Unpack(partial, entries, backupzip.UnpackOptions{
		Stop: runStopRequested,
		Progress: func(f, bb int64) {
			tracker.Update(f, bb)
			tracker.PrintProgress("restore")
		},
	})
	if err != nil {
		os.RemoveAll(partial)
		switch {
		case errors.Is(err, backupzip.ErrStopped):
			return backupStopped("nothing was restored; the archive is untouched")
		case errors.Is(err, backupzip.ErrChecksum), errors.Is(err, backupzip.ErrHostileEntry):
			return defectf("%s: %v; nothing was restored", path, err)
		}
		return err
	}
	if runStopRequested() {
		os.RemoveAll(partial)
		return backupStopped("nothing was restored; the archive is untouched")
	}
	if reuseEmpty {
		// An empty folder of that name is taken over; os.Remove refuses a
		// folder that gained a file meanwhile, and then nothing is replaced.
		if err := os.Remove(dest); err != nil {
			os.RemoveAll(partial)
			return fmt.Errorf("%s is no longer empty; nothing was replaced and nothing restored: %w", dest, err)
		}
	}
	if err := fsx.RenameNoReplace(partial, dest); err != nil {
		os.RemoveAll(partial)
		return err
	}
	fmt.Println()
	runNumber("files", n)
	runNumber("bytes", b)
	fmt.Printf("OK %s -> %s (%s files, %s)\n", path, dest, formatCount(n), formatBytes(uint64(b)))
	hl.SetResult("restored", dest)

	if o.del {
		if !o.yes {
			fmt.Printf("The folder was restored in full. Delete the archive %s? This is a normal delete - the bytes stay recoverable until reused (y/N): ", path)
			line, rerr := readConsoleLine()
			if rerr != nil {
				fmt.Printf("Archive kept (nobody could answer the question): %s\n", path)
				return exitClassf(10, "the delete needs a confirmation: nobody could answer the question, the archive was kept - pass y to delete it without one")
			}
			if ans := strings.ToLower(strings.TrimSpace(line)); ans != "y" && ans != "yes" {
				fmt.Printf("Archive kept: %s\n", path)
				return nil
			}
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Printf("Archive removed (a normal, recoverable delete): %s\n", path)
		hl.SetResult("archive", "deleted")
	}
	return nil
}

// restoreDestination reads `to`: an existing folder is the place the restored
// folder goes into; a path that does not exist yet, inside an existing
// folder, is the restored folder itself. Without `to` both are empty and the
// folder goes beside the archive (D5).
func restoreDestination(o *backupOpts) (parent, exact string, err error) {
	if !o.haveTo {
		return "", "", nil
	}
	to, err := filepath.Abs(o.to)
	if err != nil {
		return "", "", err
	}
	st, serr := os.Stat(to)
	switch {
	case serr == nil && st.IsDir():
		return to, "", nil
	case serr == nil:
		return "", "", exitClassf(10, "%s is a file; to names a folder to restore into", to)
	case errors.Is(serr, os.ErrNotExist):
		p := filepath.Dir(to)
		if pst, perr := os.Stat(p); perr != nil || !pst.IsDir() {
			return "", "", exitClassf(11, "the folder %s does not exist; to names an existing folder, or a new folder inside one", p)
		}
		return p, to, nil
	}
	return "", "", serr
}

// restorePickFree applies suffix-or-cancel to a destination that exists and
// is not empty (SP-0009 D2, SP-0198 D7): never merged into, never replaced.
// An empty folder is taken over (reuseEmpty). dest "" means cancelled.
func restorePickFree(dest string, o *backupOpts) (final string, reuseEmpty bool, err error) {
	st, err := os.Lstat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return dest, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if st.IsDir() && dirIsEmpty(dest) {
		return dest, true, nil
	}
	next := ""
	for i := 1; i < 1000; i++ {
		c := fmt.Sprintf("%s-%d", dest, i)
		_, e1 := os.Lstat(c)
		_, e2 := os.Lstat(c + fsx.PartialSuffix)
		if errors.Is(e1, os.ErrNotExist) && errors.Is(e2, os.ErrNotExist) {
			next = c
			break
		}
	}
	if next == "" {
		return "", false, exitClassf(10, "%s and its first 999 numbered neighbours exist; name another place with to", dest)
	}
	if o.suffix || o.yes {
		return next, false, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", false, exitClassf(10, "%s already exists; a restore never merges or replaces - add suffix to restore beside it as %s, or name another place with to", dest, filepath.Base(next))
	}
	fmt.Printf("%s already exists; a restore never merges or replaces. Restore beside it as %s? (y/N): ", dest, filepath.Base(next))
	line, rerr := readConsoleLine()
	if rerr != nil {
		return "", false, exitClassf(10, "%s already exists and nobody could answer the question; add suffix, or name another place with to", dest)
	}
	if ans := strings.ToLower(strings.TrimSpace(line)); ans == "y" || ans == "yes" {
		return next, false, nil
	}
	return "", false, nil
}

func dirIsEmpty(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return err == io.EOF
}

// restoreUnseal opens a sealed backup and writes the zip inside it under a
// partial name on the destination volume (FDSEC-BEHAVIOUR 1.9 section 21 item
// 3). The zip is complete when this returns: the container's final digest was
// checked. On any failure nothing is left behind.
func restoreUnseal(container, destDir string, o *backupOpts) (string, error) {
	if err := fdsecScreenContainer(container, "restore"); err != nil {
		// A length no container can have is damage, class 4, like unsecure's.
		backupContainerClass(err)
		return "", err
	}
	cred, err := resolveCredential(o.cred, false, "Password (no echo): ")
	if err != nil {
		return "", err
	}
	f, err := os.Open(container)
	if err != nil {
		return "", err
	}
	defer f.Close()
	runStep("unseal", "Opening the sealed backup")
	c, err := fdsec.Open(f, cred)
	if err != nil {
		backupContainerClass(err)
		return "", fdsecHumanizeError(err)
	}
	if c.IsTree() {
		return "", exitClassf(10, "%s holds a folder sealed by secure, not a backup zip: restore it with: filedo \"%s\" unsecure", container, container)
	}
	suffix := make([]byte, 6)
	if _, err := io.ReadFull(rand.Reader, suffix); err != nil {
		return "", err
	}
	tmp := filepath.Join(destDir, fmt.Sprintf("filedo-restore-%x.zip%s", suffix, fsx.PartialSuffix))
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			os.Remove(tmp)
		}
	})
	defer removeCleanup()
	st, _ := f.Stat()
	var size int64
	if st != nil {
		size = st.Size()
	}
	_, err = c.Unpack(out, fdsec.WithNameCheck(func(n string) error {
		if !strings.EqualFold(filepath.Ext(n), ".zip") {
			return errNotBackupZip
		}
		return nil
	}), newFdsecTracker("unseal", size), fdsecStreamStop())
	cerr := out.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		switch {
		case errors.Is(err, errNotBackupZip):
			return "", exitClassf(10, "%s opened, and what is sealed in it is not a backup zip: restore it with: filedo \"%s\" unsecure", container, container)
		case fdsecStopped(err):
			return "", backupStopped(fdsecEndProgress() + "nothing was restored; the container is untouched")
		}
		backupContainerClass(err)
		return "", fdsecHumanizeError(err)
	}
	fmt.Print(fdsecEndProgress())
	return tmp, nil
}
