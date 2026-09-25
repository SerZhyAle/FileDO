package main

// launcher.go is identical in cmd/filedo-check, cmd/filedo-fill and
// cmd/filedo-test. The companions are separate modules with no shared package
// (AGENTS.md, "Shared packages are main-module only"), so this file is copied,
// not imported - change the three copies together.
//
// A companion is a shortcut, not an engine (SP-0028 section 5, option A). It
// turns its own command line into the equivalent filedo.exe command line, runs
// the filedo.exe that sits beside it with the console inherited, and ends with
// that process's exit code. Everything the run does - the work, the prompts,
// the safety guards, the verdict - is filedo.exe's, so a fix there reaches the
// companions without being copied into them.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

// mainExeName is the program every companion delegates to.
const mainExeName = "filedo.exe"

// exitCouldNotVerify is filedo.exe's code 2: nothing was done or checked, so
// nothing is claimed (CLI-EVENT-STREAM rule 11). A companion ends with it only
// for its own usage errors and for a filedo.exe it cannot find or start; every
// other exit code is filedo.exe's, passed on unchanged.
const exitCouldNotVerify = 2

// usageError is a command line this companion does not accept.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

func main() {
	os.Exit(companionMain(os.Args[1:]))
}

// companionMain is the whole program: the usage text, the mapping, then
// filedo.exe. It returns the exit code.
func companionMain(args []string) int {
	if len(args) == 0 {
		fmt.Print(usageText())
		return exitCouldNotVerify
	}
	if isHelpRequest(args) {
		fmt.Print(usageText())
		return 0
	}
	mainArgs, err := mapArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\nRun %s -? for the usage.\n", err, selfName)
		return exitCouldNotVerify
	}
	mainPath, err := locateMain()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitCouldNotVerify
	}
	return runMain(mainPath, mainArgs)
}

// isHelpRequest reports whether the command line asks for the usage text: a
// help word in the first place, as the companions always accepted, or a help
// switch anywhere - so "filedo_fill D: --help" shows help instead of running.
func isHelpRequest(args []string) bool {
	switch strings.ToLower(args[0]) {
	case "?", "/?", "-?", "--help", "help", "h", "/help":
		return true
	}
	for _, a := range args[1:] {
		switch strings.ToLower(a) {
		case "/?", "-?", "--help", "/help":
			return true
		}
	}
	return false
}

// globalSwitches are filedo.exe's global options that take no value: the ones
// its extractGlobalFlags removes before any verb sees the command line, and
// the two words its history logger reads. --events and --stop-file, which take
// a file name, are handled in splitGlobalOptions.
var globalSwitches = map[string]bool{
	"--pause":      true,
	"-pause":       true,
	"/pause":       true,
	"--no-history": true,
	"--no-ui":      true,
	"nohist":       true,
	"no_history":   true,
}

// splitGlobalOptions separates filedo.exe's global options from the
// companion's own words. The options come back unchanged and in the order
// given, to be appended to the mapped command line; extra names the options a
// companion hands over on top of the global ones. Matching is exact, the way
// filedo.exe matches them, so an option filedo.exe would not recognise is not
// silently passed along.
func splitGlobalOptions(args []string, extra map[string]bool) (words, forwarded []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--events" || a == "--stop-file":
			if i+1 >= len(args) {
				return nil, nil, usagef("%s needs a file name after it", a)
			}
			forwarded = append(forwarded, a, args[i+1])
			i++
		case strings.HasPrefix(a, "--events=") || strings.HasPrefix(a, "--stop-file="):
			forwarded = append(forwarded, a)
		case globalSwitches[a] || extra[a]:
			forwarded = append(forwarded, a)
		default:
			words = append(words, a)
		}
	}
	return words, forwarded, nil
}

// takeTarget splits the target off the companion's words. A word that looks
// like an option and was not recognised as one is refused here, before it
// could be read as a target or an operation word.
func takeTarget(words []string) (target string, rest []string, err error) {
	for _, w := range words {
		if len(w) > 1 && strings.HasPrefix(w, "-") {
			return "", nil, usagef("unknown option %s", w)
		}
	}
	if len(words) == 0 || strings.TrimSpace(words[0]) == "" {
		return "", nil, usagef("no target given: name a drive (D:), a folder or a network share first")
	}
	return normalizeTarget(strings.TrimSpace(words[0])), words[1:], nil
}

// normalizeTarget makes a target mean to filedo.exe what it meant to the
// companion. Two spellings need help:
//   - a lone letter ("E") was always the drive E:, but filedo.exe reads several
//     single letters as verbs (d is device, f is file, n is network);
//   - a bare relative name ("wipe", "copy", "secure") can be one of filedo.exe's
//     verbs too, so it is spelled .\name - the same folder, never a verb.
//
// Everything else - D:, D:\, D:\Temp, \\server\share - is passed exactly as
// typed, so filedo.exe's own target rules, and its system-drive guard, see
// what the user wrote.
func normalizeTarget(t string) string {
	if len(t) == 1 && isASCIILetter(t[0]) {
		return t + ":"
	}
	if !strings.ContainsAny(t, `\/:`) {
		return `.\` + t
	}
	return t
}

func isASCIILetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// locateMain finds the filedo.exe that ships beside this program, and only
// that one. It never searches the current directory or PATH: a companion that
// ran whichever filedo.exe it met first would run a planted one. Its own path
// is resolved through symlinks first, because a winget portable install starts
// it through a link in ...\WinGet\Links while the files live in the package
// folder.
func locateMain() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot tell where %s is: %v", selfName, err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	dir := filepath.Dir(self)
	path := filepath.Join(dir, mainExeName)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is missing from %s. %s runs the %s in its own folder; reinstall FileDO, or keep the two files together", mainExeName, dir, selfName, mainExeName)
	}
	// A launcher saved as filedo.exe would find itself, and a command line
	// that maps onto itself ("<target> clean") would start it again, and
	// again. Refuse rather than recurse.
	if selfInfo, err := os.Stat(self); err == nil && os.SameFile(selfInfo, info) {
		return "", fmt.Errorf("%s in %s is this launcher itself, not FileDO's main program; reinstall FileDO", mainExeName, dir)
	}
	return path, nil
}

// runMain runs filedo.exe with this console's standard handles and returns
// its exit code unchanged.
func runMain(mainPath string, args []string) int {
	cmd := exec.Command(mainPath, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// filedo.exe shares this console, so Ctrl+C reaches it directly and it
	// stops the way it always does - cleaning up, recording the result and
	// choosing the exit code. The launcher only has to outlive it: left
	// unhandled, Ctrl+C would end this process at once, give the prompt back
	// while filedo.exe is still writing, and lose its exit code. It has to be
	// signal.Notify: on Windows signal.Ignore leaves the default handler in
	// place, and that one ends the process.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not start %s: %v\n", mainPath, err)
		return exitCouldNotVerify
	}
	err := cmd.Wait()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "Error: lost track of %s: %v\n", mainPath, err)
	return exitCouldNotVerify
}
