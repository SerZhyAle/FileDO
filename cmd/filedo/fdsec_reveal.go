package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"filedo/fdsec"
)

// Reveal (spec 8, stage S4): open a container into a protected sandbox, hand
// the copy to the system's registered handler, and close the plaintext window
// again. The feature's single genuine risk is that a readable copy must exist
// for the handler to read; this file is what makes that window small, known
// and closed.
//
// Three mechanisms end a reveal, in the order they are trusted:
//
//	1. hold-and-watch - the copy is removed once a handler that took an
//	   exclusive hold on it lets go. An optimisation for the media case, never
//	   the sole signal: stage S0's probe P3 measured that the packaged Notepad
//	   and Explorer's own zip view never lock the file at all, so a reveal
//	   driven only by this would delete a file that is still on screen.
//	2. the visible "remove it now" - load-bearing, because P3 also measured
//	   that a single-instance player's launched process exits inside 0.8 s
//	   while the file stays held, so the launched PID can never drive this.
//	3. the startup sweep - the only backstop for a handler that never locks,
//	   and the only remedy after a power loss.
//
// The sandbox root is %LOCALAPPDATA%\FileDO\reveal, verified by probe P2 to
// survive the Storage Sense run that deletes a same-aged file from %TEMP%
// together with its directory: nothing else will tidy a leftover up for us,
// which is why mechanism 3 exists.

// fdsecRevealDirPrefix marks a directory as a reveal sandbox. The sweep
// removes what carries it and nothing else, so an unrelated directory that
// ever appears under the root is left alone rather than deleted.
const fdsecRevealDirPrefix = "rv-"

// fdsecRevealLockSuffix names the file a live reveal holds open exclusively -
// `<sandbox>.lock`, a sibling of the sandbox rather than a file inside it, so
// it can be taken before the sandbox exists. A second FileDO starting
// mid-reveal then sweeps the leftovers and steps around the live one, with no
// window in which the sandbox is visible but unclaimed.
const fdsecRevealLockSuffix = ".lock"

// Lifetime tuning. These are the shape of the wait, not the contract:
//   - a handler gets this long to take its hold before hold-and-watch decides
//     it never will (P3 measured a media player taking it in 0.01 s);
//   - once the hold is released it must stay released for the settle window,
//     so a player that closes and reopens the file between frames does not
//     lose it (P3 measured a release visible 0.12 s after the holder ended).
const (
	fdsecRevealAcquireWait = 10 * time.Second
	fdsecRevealSettle      = 3 * time.Second
	fdsecRevealPoll        = 200 * time.Millisecond
)

// fdsecNeverLaunch is invariant 7 made concrete: an executable or script
// recovered from a container is extracted and its location shown, never
// handed to the shell. The list is deliberately wider than "what Windows
// executes" - a type that only some installations run is still refused,
// because a refusal costs the user one manual double-click and a wrong
// launch costs them the machine.
var fdsecNeverLaunch = map[string]bool{
	".exe": true, ".com": true, ".scr": true, ".pif": true, ".cpl": true,
	".bat": true, ".cmd": true, ".ps1": true, ".psm1": true, ".psd1": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true,
	".wsh": true, ".hta": true, ".msi": true, ".msp": true, ".msc": true,
	".reg": true, ".inf": true, ".sct": true, ".jar": true, ".lnk": true,
	".url": true, ".application": true, ".gadget": true, ".chm": true,
}

// fdsecRevealRootEnv relocates the sandbox root. It exists so the test suite
// does not scatter sandboxes through the developer's real profile, and it
// weakens nothing: wherever the root is put, the per-reveal directory is still
// created with the access list of fdsecSandboxCreate, so a root someone points
// at a shared directory still yields a copy only this user can read.
const fdsecRevealRootEnv = "FILEDO_FDSEC_REVEAL_ROOT"

// fdsecRevealRoot is the sandbox root of spec 8.1. os.UserCacheDir is
// %LOCALAPPDATA% on Windows, which is the directory probe P2 measured
// surviving the Storage Sense run that deleted a same-aged file from %TEMP%.
func fdsecRevealRoot() (string, error) {
	if r := os.Getenv(fdsecRevealRootEnv); r != "" {
		return r, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the local application data directory: %w", err)
	}
	return filepath.Join(base, "FileDO", "reveal"), nil
}

// fdsecReservedStems are the device names Windows resolves before it ever
// looks at a directory. A container whose sealed name is one of them would
// not produce a file at all.
var fdsecReservedStems = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// fdsecSandboxName turns the sealed true name into a name that is safe to
// create inside the sandbox and safe to classify by extension. A container
// arrives from anywhere and its metadata is attacker-controlled the moment
// somebody else made it, so every rule here is refused rather than sanitised:
// a silent rename would hide what the container was trying to do, and it is
// the *user* who needs to see that their file is not what it claims.
//
// The rule that matters most is the one about trailing dots and spaces.
// Windows drops them when it creates a file, so a sealed name of "invoice.exe "
// becomes "invoice.exe" on disk - while a naive extension check sees
// ".exe " and finds it nowhere in the never-launch list. That is the launch
// policy of invariant 7 bypassed by one space, so the name is refused before
// anything else can look at it.
func fdsecSandboxName(trueName string) (string, error) {
	refuse := func(why string) error {
		// The name itself is deliberately not echoed: it is sealed metadata,
		// and this error reaches history.json, which outlives the reveal.
		return fmt.Errorf("%w: the container's sealed name %s, and is refused. The container may have been built to make a reveal write or launch something other than what it claims", fdsec.ErrDamaged, why)
	}
	if trueName == "" || trueName == "." || trueName == ".." {
		return "", refuse("is empty or a directory reference")
	}
	if strings.ContainsAny(trueName, `/\:*?"<>|`) {
		// ':' is here for the same reason as the separators: "notes:run.exe"
		// is not a file name, it is a request to write an alternate data
		// stream of a different file.
		return "", refuse("contains a path separator or a character that is not legal in a file name")
	}
	for _, r := range trueName {
		if r < 0x20 || r == 0x7f {
			return "", refuse("contains a control character")
		}
	}
	if strings.TrimRight(trueName, ". ") != trueName {
		return "", refuse("ends in a dot or a space, which Windows silently drops - so the name on disk would differ from the name the extension check saw")
	}
	if filepath.IsAbs(trueName) || filepath.Base(trueName) != trueName {
		return "", refuse("is a path, not a file name")
	}
	stem := strings.ToLower(trueName)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if fdsecReservedStems[stem] {
		return "", refuse("is a reserved device name")
	}
	return trueName, nil
}

// fdsecReveal is the target-first `reveal` handler.
func fdsecReveal(path string, args []string, hl *HistoryLogger) error {
	o, err := parseFdsecArgs(args, "reveal")
	if err != nil {
		return err
	}

	// -rw is not a writable sandbox - there is no such thing (spec 8.4, Q7).
	// It is the ordinary restore to a place the user owns, which `unsecure`
	// already implements in full, down to the collision rule and the
	// timestamp restore. Delegating is the whole of it: a second
	// implementation of a restore is exactly what this repo's "reuse, never
	// rewrite" rule exists to prevent.
	if o.rw {
		fmt.Printf("-rw is not a writable sandbox: the sandbox copy is always read-only.\n")
		fmt.Printf("Restoring to an ordinary file you own instead (the same thing `unsecure` does).\n\n")
		return fdsecUnsecure(path, fdsecArgsWithout(args, "-rw"), hl)
	}

	if err := fdsecScreenContainer(path, "reveal"); err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	root, err := fdsecRevealRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}

	// The free-space check of spec 8, before a byte is written and before the
	// credential is even asked. The original's real size is sealed and cannot
	// be known this early, so the check uses the container's size, which is a
	// strict upper bound on it - padding and tags only ever add. The refusal
	// therefore carries a number that is never too small.
	if free, ferr := fdsecFreeSpaceFor(root); ferr == nil && free < fi.Size() {
		return fmt.Errorf("not enough free space for the reveal: %s free at %s, the container is %s and the original cannot be larger",
			formatBytes(uint64(free)), root, formatBytes(uint64(fi.Size())))
	}

	cred, err := resolveFdsecCredential(o, false)
	if err != nil {
		return err
	}

	// The live-reveal lock comes first, before the sandbox directory exists at
	// all. It is a sibling of the directory rather than a file inside it, and
	// that ordering is the whole point: if the directory were created first,
	// another FileDO starting in the gap before the lock was taken would find
	// a sandbox with no lock, call it a leftover, and delete a reveal that is
	// about to be used.
	stem := fdsecRevealDirPrefix + fdsecRandomName()
	dir := filepath.Join(root, stem)
	lockPath := dir + fdsecRevealLockSuffix
	lock, lerr := fdsecHoldLock(lockPath)
	if lerr == nil {
		defer func() {
			lock.Close()
			os.Remove(lockPath)
		}()
	}

	if err := fdsecSandboxCreate(dir); err != nil {
		return fmt.Errorf("cannot create the reveal sandbox %s: %w", dir, err)
	}
	keep := false
	defer func() {
		if !keep {
			fdsecRemoveSandbox(dir)
		}
	}()
	globalInterruptHandler.AddCleanup(func() { fdsecRemoveSandbox(dir) })

	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	hl.SetResult("container", path)

	// Unpack to a temporary name inside the sandbox and rename into place
	// once every chunk and the final digest have verified (invariant 3): an
	// interrupted reveal must not leave something that looks complete.
	tmp, err := os.CreateTemp(dir, ".fdsec-reveal-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath)
	}()

	meta, err := fdsec.Unpack(tmp, src, cred, newFdsecTracker("reveal", fi.Size()))
	if err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	name, err := fdsecSandboxName(meta.Name)
	if err != nil {
		return err
	}
	copyPath := filepath.Join(dir, name)
	if err := os.Rename(tmpPath, copyPath); err != nil {
		return err
	}
	fdsecRestoreTimes(copyPath, meta)

	// Mark the copy as untrusted-origin before it is sealed read-only, so the
	// platform's own protections stay switched on for it. A failure here is
	// reported and not fatal: the mark is defence in depth, and refusing the
	// reveal over it would teach the user to avoid the feature.
	if merr := fdsecMarkUntrusted(copyPath); merr != nil {
		fmt.Printf("Note: could not mark the copy as untrusted-origin (%v)\n", merr)
	}
	// No writable sandbox (spec 8.4): the copy is read-only, so there is no
	// write-back logic that could lose an edit, because there is no edit.
	if cerr := os.Chmod(copyPath, 0o444); cerr != nil {
		fmt.Printf("Note: could not make the copy read-only (%v)\n", cerr)
	}

	fmt.Printf("%sRevealed %s -> %s (%s)\n", fdsecEndProgress(), path, copyPath, formatBytes(uint64(meta.Size)))
	// The sandbox directory, never the copy's name. The true name is sealed
	// metadata (Q3) and history.json is a permanent file on the user's disk:
	// writing the name there would leak, for good, the one thing the
	// container exists to hide - and it would outlive the reveal that showed
	// it on screen for a minute. The screen is the user asking; the log file
	// is not.
	hl.SetResult("revealed", dir)

	ext := strings.ToLower(filepath.Ext(name))
	if fdsecNeverLaunch[ext] {
		// Invariant 7. Extracted, located, and not launched - and kept,
		// because deleting it the instant we refuse to open it would leave
		// the user with nothing to act on.
		keep = true
		fmt.Printf("\n%s is an executable or script type. It is NOT launched (safety invariant 7).\n", ext)
		fmt.Printf("It is here, read-only, for you to inspect:\n  %s\n", copyPath)
		fmt.Printf("The next FileDO start removes it.\n")
		hl.SetResult("launched", "refused-executable")
		return nil
	}

	if o.keep {
		keep = true
		fmt.Printf("\n-keep: the copy is left in place and the next FileDO start removes it.\n")
		fmt.Printf("  %s\n", copyPath)
		hl.SetResult("lifetime", "keep")
		if lerr == nil {
			lock.Close()
		}
	}

	if lerr := fdsecLaunchCopy(copyPath); lerr != nil {
		fmt.Printf("Could not hand the copy to a registered handler (%v).\n", lerr)
		fmt.Printf("It is here, read-only:\n  %s\n", copyPath)
		if !o.keep {
			keep = true
			fmt.Printf("The next FileDO start removes it.\n")
		}
		return nil
	}
	// Whether a launch happened, not what was launched: the extension is part
	// of the sealed true name and belongs on screen, not in a permanent log.
	hl.SetResult("launched", true)

	if o.keep {
		return nil
	}
	fdsecAwaitDone(copyPath)
	return nil
}

// fdsecLaunchEnvOff is the test seam for the one step of a reveal that cannot
// run inside a test suite: handing the copy to the machine's real registered
// handler would open real windows on the developer's desktop and leave them
// there. Everything else - the sandbox, the access list, the read-only copy,
// the executable refusal and all three lifetime mechanisms - is exercised for
// real; only the hand-off is suppressed, and it says so when it is.
const fdsecLaunchEnvOff = "FILEDO_FDSEC_NO_LAUNCH"

func fdsecLaunchCopy(path string) error {
	if os.Getenv(fdsecLaunchEnvOff) == "1" {
		fmt.Printf("Handler launch suppressed by %s=1.\n", fdsecLaunchEnvOff)
		return nil
	}
	return fdsecLaunch(path)
}

// fdsecAwaitDone runs mechanisms 1 and 2 against each other and returns when
// either says the reveal is over. The caller's deferred removal is what
// actually closes the window; this decides when.
//
// Mechanism 2 - the visible "remove it now" - has two shapes, and which one
// exists is a property of who started this process. A console gets the Enter
// prompt. A caller that gave --stop-file (the GUI, SP-0005 S6) gets the same
// mechanism over a machine channel: the stop file is the button, the global
// interrupt handler is what notices it, and the caller's cleanup removes the
// sandbox. Without either, there is nobody to ask, and only then does
// hold-and-watch decide alone.
func fdsecAwaitDone(copyPath string) {
	watch := make(chan bool, 1)
	go func() { watch <- fdsecHoldAndWatch(copyPath) }()

	asked := globalInterruptHandler.Context().Done()

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		if !machineStopChannel {
			// No console and no machine channel - a batch run, or output
			// redirected by something that offers no way back. Mechanism 2
			// does not exist here, so hold-and-watch decides alone.
			if <-watch {
				fmt.Printf("Reveal closed (the handler let go of it).\n")
			} else {
				fmt.Printf("Reveal closed (no handler ever held it, and there is no console to ask).\n")
			}
			return
		}
		// The GUI's own window is the visible "remove it now", so a handler
		// that never locks the file must not end the reveal on a timer: the
		// copy stays until the shell says it is done, exactly as it would
		// stay until Enter on a console.
		fmt.Printf("\nWhen you are done with it, use \"remove the copy now\" in the window that started this.\n")
		fmt.Printf("  %s\n", copyPath)
		for {
			select {
			case <-asked:
				fmt.Printf("Reveal closed (you said so).\n")
				return
			case held := <-watch:
				if !held {
					continue
				}
				fmt.Printf("\nReveal closed (the handler let go of it).\n")
				return
			}
		}
	}

	done := make(chan struct{})
	go func() {
		fmt.Printf("\nWhen you are done with it, press Enter to remove the copy.\n")
		fmt.Printf("  %s\n", copyPath)
		_, _ = readConsoleLine()
		close(done)
	}()

	for {
		select {
		case <-done:
			fmt.Printf("Reveal closed (you said so).\n")
			return
		case <-asked:
			fmt.Printf("Reveal closed (you said so).\n")
			return
		case held := <-watch:
			if !held {
				// Nobody ever took a hold - S0's P3 measured two such
				// handlers, with the file open and visible. Mechanism 1 is
				// blind here, so it steps aside and the prompt decides.
				continue
			}
			fmt.Printf("\nReveal closed (the handler let go of it).\n")
			return
		}
	}
}

// fdsecHoldAndWatch is mechanism 1. It reports true only for a hold it
// actually saw taken and then released: a handler that never locks the file -
// S0's P3 measured the packaged Notepad and Explorer's zip view doing exactly
// that - returns false instead, because deleting a file that is open on
// screen is worse than waiting for the user or for the next start's sweep.
func fdsecHoldAndWatch(path string) bool {
	deadline := time.Now().Add(fdsecRevealAcquireWait)
	for !fdsecIsHeld(path) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(fdsecRevealPoll)
	}
	// Held. Now wait for it to be let go - and to stay let go for the settle
	// window, so a handler that closes and reopens the file between tracks
	// does not lose it.
	for {
		if fdsecIsHeld(path) {
			time.Sleep(fdsecRevealPoll)
			continue
		}
		settled := true
		for until := time.Now().Add(fdsecRevealSettle); time.Now().Before(until); {
			time.Sleep(fdsecRevealPoll)
			if fdsecIsHeld(path) {
				settled = false
				break
			}
		}
		if settled {
			return true
		}
	}
}

// fdsecRemoveSandbox removes a reveal sandbox and everything in it. The copy
// is read-only by design, so the attributes come off first.
func fdsecRemoveSandbox(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			_ = os.Chmod(filepath.Join(dir, e.Name()), 0o666)
		}
	}
	_ = os.RemoveAll(dir)
}

// fdsecSweepReveals is mechanism 3, and it is the only remedy for the two
// cases nothing else covers: a handler that never locks the copy, and a power
// loss while one exists. It runs at every FileDO start, before anything else,
// and it is safe to fail - a sandbox it cannot remove is reported and skipped,
// because a locked leftover must never stop an ordinary `filedo device C:
// info` from running.
func fdsecSweepReveals() {
	root, err := fdsecRevealRoot()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return // no root, nothing ever revealed - the common case, silently
	}
	for _, e := range entries {
		kind, ours := fdsecSandboxKind(e.Name())
		if !ours {
			continue
		}
		// An orphaned lock whose sandbox is already gone: the run that held
		// it died between removing its directory and removing this.
		if !e.IsDir() {
			if !strings.HasSuffix(e.Name(), fdsecRevealLockSuffix) {
				continue
			}
			p := filepath.Join(root, e.Name())
			if !fdsecIsHeld(p) {
				if _, serr := os.Stat(strings.TrimSuffix(p, fdsecRevealLockSuffix)); os.IsNotExist(serr) {
					os.Remove(p)
				}
			}
			continue
		}
		dir := filepath.Join(root, e.Name())
		// A sandbox whose lock is held belongs to a run happening right now,
		// in another FileDO. Stepping around it is the whole reason the lock
		// exists.
		if fdsecIsHeld(dir + fdsecRevealLockSuffix) {
			continue
		}
		fdsecRemoveSandbox(dir)
		os.Remove(dir + fdsecRevealLockSuffix)
		if _, serr := os.Stat(dir); serr == nil {
			fmt.Printf("Note: a leftover %s could not be removed and stays for the next start: %s\n", kind, dir)
			continue
		}
		fmt.Printf("Removed a leftover %s: %s\n", kind, dir)
	}
}

// fdsecSandboxKind names the mechanism a directory under the root belongs to,
// and reports whether it belongs to FileDO at all. The sweep removes what
// carries one of the two prefixes and nothing else, so an unrelated directory
// that ever appears under the root is left alone rather than deleted.
//
// The two prefixes stay distinct on purpose (SP-0008 4.1): a rv- copy is
// read-only, marked untrusted-origin and never an executable, a us- copy is
// none of those, and a person reading a directory listing - or a message from
// this sweep - should be able to tell which contract a leftover was made
// under without opening it.
func fdsecSandboxKind(name string) (string, bool) {
	switch {
	case strings.HasPrefix(name, fdsecRevealDirPrefix):
		return "reveal", true
	case strings.HasPrefix(name, fdsecStartDirPrefix):
		return "unsecure-start copy", true
	}
	return "", false
}

// fdsecArgsWithout drops one flag from an argument list, so -rw can be
// forwarded to the restore path without it.
func fdsecArgsWithout(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.EqualFold(a, flag) {
			continue
		}
		out = append(out, a)
	}
	return out
}
