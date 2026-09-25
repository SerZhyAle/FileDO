package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/term"

	"filedo/fdsec"
)

// The fdsec command surface (spec 5.1, owner grammar 2026-09-20):
//
//	filedo <file>         secure   [options] [password]
//	filedo <file.fd-sec>  unsecure [options] [password]
//	filedo <file.fd-sec>  reveal   [password] [-rw] [-keep]
//	filedo fdsec info     <file.fd-sec> [password]
//	filedo fdsec verify   <file.fd-sec> [password]
//	filedo fdsec register   [-all-users]
//	filedo fdsec unregister [-all-users]
//
// Exit codes - the distinctions are the contract (FD-SEC-CONTRACT.md section 7.1),
// the digits are this stage's choice; filedo had no nonzero convention
// before. main() honours fdsecExitCode after the history entry is flushed.
const (
	fdsecExitUsage              = 2 // D: bad option, missing target
	fdsecExitCredentialOrTamper = 3 // A: wrong credential, or tamper on an intact file
	fdsecExitDamaged            = 4 // B: damaged / truncated / impossible container
	fdsecExitIO                 = 5 // C: I/O and filesystem errors
	fdsecExitUnsupported        = 6 // unsupported: unknown suite/version/flag, an operation this platform has no equivalent for
)

var fdsecExitCode int

var errFdsecUsage = errors.New("fdsec usage")

// fdsecEventSafeError keeps the full error for the console while giving the
// event stream a message that cannot contain a sealed filename (rule 13).
type fdsecEventSafeError struct {
	err          error
	eventMessage string
}

func (e *fdsecEventSafeError) Error() string { return e.err.Error() }
func (e *fdsecEventSafeError) Unwrap() error { return e.err }

func fdsecScreenEventError(err error) error {
	if err == nil {
		return nil
	}
	return &fdsecEventSafeError{
		err:          err,
		eventMessage: "Could not restore the container to its requested destination; see the console for details.",
	}
}

// fdsecSetExit maps an error to its exit class. Wrong-credential-or-tamper
// and damaged never map to each other (invariant 9).
func fdsecSetExit(err error) {
	if err == nil {
		return
	}
	// A stop is neither a usage error nor a failure: the run ends Stopped and
	// its code is the supervisor's (rule 15), so no container class is set.
	if fdsecStopped(err) {
		return
	}
	switch {
	case errors.Is(err, fdsec.ErrCredentialOrTamper):
		fdsecExitCode = fdsecExitCredentialOrTamper
	case errors.Is(err, fdsec.ErrDamaged):
		fdsecExitCode = fdsecExitDamaged
	case errors.Is(err, fdsec.ErrUnsupported):
		fdsecExitCode = fdsecExitUnsupported
	case errors.Is(err, errFdsecUsage), errors.Is(err, fdsec.ErrTreeContainer), errors.Is(err, fdsec.ErrFileContainer):
		fdsecExitCode = fdsecExitUsage
	default:
		fdsecExitCode = fdsecExitIO
	}
}

// reportFdsecError is how a container verb's error ends a run, from either
// entry point - main() and the batch path call the same function, so a line
// in a .lst ends with the same code and verdict as the same command typed
// (CLI-10). A stop is reported as a stop: no failure is recorded, and the
// verdict comes out Stopped (FDSEC-02, rule 15).
func reportFdsecError(err error, hl *HistoryLogger) {
	if err == nil {
		return
	}
	hl.SetError(err)
	if fdsecStopped(err) {
		fmt.Printf("Stopped: %v\n", err)
		return
	}
	fdsecSetExit(err)
	runFailure(err)
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

func fdsecHumanizeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fdsec.ErrUnsupported) {
		msg := err.Error()
		const prefix = "fdsec: unsupported container: "
		if idx := strings.Index(msg, prefix); idx >= 0 {
			detail := msg[idx+len(prefix):]
			return fmt.Errorf("unsupported container (FDSEC %s): written by a newer program - update FileDO to open it: %w", detail, fdsec.ErrUnsupported)
		}
	}
	return err
}

func usagef(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", errFdsecUsage, fmt.Sprintf(format, args...))
}

// isFdsecOp reports whether an operation token (target-first grammar,
// position after the file) belongs to the fdsec family.
func isFdsecOp(op string) bool {
	switch strings.ToLower(op) {
	case "secure", "sec", "unsecure", "uns", "unsec", "reveal", "rev":
		return true
	}
	return false
}

// fdsecTypeWord reports whether s is one of the explicit target-type prefixes
// (device / folder / file / network and their aliases).
func fdsecTypeWord(s string) bool {
	l := strings.ToLower(s)
	return contains(list_of_flags_for_device, l) || contains(list_of_flags_for_folder, l) ||
		contains(list_of_flags_for_file, l) || contains(list_of_flags_for_network, l)
}

// fdsecTargetKind classifies a target the way main() would, without letting a
// failed os.Stat decide: a mask target (secure *.txt) never passes os.Stat,
// so the generic path probe can never be what dispatches it.
func fdsecTargetKind(target string) string {
	if len(target) > 2 && (target[0:2] == `\\` || target[0:2] == "//") {
		return "network"
	}
	// An existing regular file wins over the one-letter drive heuristic: a
	// file called `a` is a file, not drive A: (FDSEC-14).
	if fi, err := os.Stat(target); err == nil && fi.Mode().IsRegular() && !driveRootSpelling.MatchString(target) {
		return "file"
	}
	if driveRootSpelling.MatchString(target) || (len([]rune(target)) == 1 && isASCIILetter(target)) {
		return "device"
	}
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		return "folder"
	}
	return "file"
}

func isASCIILetter(s string) bool {
	return len(s) == 1 && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z'))
}

// fdsecExplicitKind maps an explicit type word to the kind it names.
func fdsecExplicitKind(word string) string {
	l := strings.ToLower(word)
	switch {
	case contains(list_of_flags_for_device, l):
		return "device"
	case contains(list_of_flags_for_folder, l):
		return "folder"
	case contains(list_of_flags_for_network, l):
		return "network"
	default:
		return "file"
	}
}

// fdsecDispatchTarget runs the target-first grammar
//
//	<target> secure|unsecure|reveal [options] [password]
//
// for both entry points - main() and executeInternalCommand(), the batch
// path - so one implementation serves the interactive run and the `.lst`
// file alike. It reports whether the line belonged to the fdsec family; the
// caller returns as soon as it did.
func fdsecDispatchTarget(argv []string, hl *HistoryLogger) (bool, error) {
	// The explicit type prefix this repo documents for every other operation
	// (filedo file data.zip info) reaches the same place - but only when
	// dropping it actually yields an fdsec op, so a file literally named
	// "file" is still its own target. An explicit type word wins over every
	// guess (FDSEC-14): `file a secure` packs the file a.
	explicitKind := ""
	if len(argv) > 2 && isFdsecOp(argv[2]) && fdsecTypeWord(argv[0]) {
		explicitKind = fdsecExplicitKind(argv[0])
		argv = argv[1:]
	}
	// A verb with nothing after it is a usage error, not silence - unless a
	// path of that name actually exists, in which case it is somebody's file
	// and the ordinary target dispatch owns it.
	if len(argv) == 1 && isFdsecOp(argv[0]) {
		if _, err := os.Stat(argv[0]); err != nil {
			return true, usagef("%s needs a target: filedo <file> %s [options] [password]", argv[0], strings.ToLower(argv[0]))
		}
	}
	if len(argv) < 2 || !isFdsecOp(argv[1]) {
		return false, nil
	}
	target := argv[0]
	opArgs := make([]string, 0, len(argv)-1)
	opArgs = append(opArgs, strings.ToLower(argv[1]))
	opArgs = append(opArgs, argv[2:]...)
	kind := explicitKind
	if kind == "" {
		kind = fdsecTargetKind(target)
	}
	return true, runFdsecTargetOp(kind, target, opArgs, hl)
}

// runFdsecTargetOp is the target-first entry. A file is a valid source, and so
// is an ordinary folder for `secure` - it packs to one container holding the
// whole tree (SP-0009, FDSEC-FORMAT suite 3). A drive or a network share is
// refused: a whole volume is not a folder anybody means to pack in one go.
func runFdsecTargetOp(cmdTypeName, path string, opArgs []string, hl *HistoryLogger) error {
	op := strings.ToLower(opArgs[0])
	hl.SetCommand(cmdTypeName, path, "fdsec-"+op)
	// A container verb's exit codes are FDSEC-BEHAVIOUR section 7.1 and mean
	// something else than the supervisor's three digits, so the `result` event
	// is the only thing a supervisor may read its outcome from - which is why
	// these verbs carry one (CLI-EVENT-STREAM rule 12). The arguments reach
	// the channel through redactCredentialArgs like every other surface
	// (rule 13, and safety invariant 8 is the same sentence).
	beginRun(runActs, op, path, opArgs)
	secureOp := op == "secure" || op == "sec"
	switch {
	case cmdTypeName == "folder" && !secureOp:
		return fmt.Errorf("%s is a folder, not a container: %s opens a .fd-sec file (a folder is packed with: filedo %s secure)", path, op, path)
	case cmdTypeName != "file" && cmdTypeName != "folder":
		return fmt.Errorf("%s is a %s: secure packs one file or one folder into a container, never a whole drive or a network share", path, cmdTypeName)
	}
	var err error
	switch op {
	case "secure", "sec":
		err = fdsecSecure(path, opArgs[1:], hl)
	case "unsecure", "uns", "unsec":
		err = fdsecUnsecure(path, opArgs[1:], hl)
	default: // reveal, rev
		err = fdsecReveal(path, opArgs[1:], hl)
	}
	return fdsecHumanizeError(err)
}

// handleFdsecCommand is the verb-first entry: filedo fdsec <sub-verb> ..
// info and verify read a container; register and unregister write and remove
// the Explorer integration (fdsec_register.go).
func handleFdsecCommand(args []string, hl *HistoryLogger) error {
	if len(args) < 1 {
		return usagef("fdsec needs a sub-verb: info <container> [password], verify <container> [password], register, unregister")
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "info", "verify":
		if len(args) < 2 {
			return usagef("fdsec %s needs a container path", sub)
		}
		path := args[1]
		hl.SetCommand("fdsec", path, sub)
		// info and verify answer a question about the container, so their
		// success is `Passed` rather than `Done` (rule 10); their digits stay
		// FDSEC-BEHAVIOUR's (rule 12). The arguments go to the `run` event
		// with the family word in front, so the redactor knows the line is a
		// container command and screens the password after the path - without
		// it `verify <path> <password>` reached the event file in clear
		// (FDSEC-04).
		beginRun(runJudges, "fdsec "+sub, path, append([]string{"fdsec"}, args...))
		var credArgs []string
		if len(args) > 2 {
			credArgs = args[2:]
		}
		if sub == "info" {
			return fdsecHumanizeError(fdsecInfo(path, credArgs, hl))
		}
		return fdsecHumanizeError(fdsecVerify(path, credArgs, hl))
	case "register", "unregister":
		hl.SetCommand("fdsec", "", sub)
		beginRun(runActs, "fdsec "+sub, "", append([]string{"fdsec"}, args...))
		return handleFdsecRegister(sub, args[1:])
	}
	return usagef("unknown fdsec sub-verb %q: want info, verify, register or unregister", sub)
}

// fdsecOpts is the parsed, order-independent option set (spec 5.1).
type fdsecOpts struct {
	del       bool
	wipe      bool
	rename    bool
	here      bool
	rw        bool // reveal: not a writable sandbox - the ordinary restore
	keep      bool // reveal: leave the copy for the next start's sweep
	suite2    bool // secure: write the quiet suite 2 instead of suite 1 (SP-0019 D2, D6)
	start     bool // unsecure: hand the restored file to its registered handler
	assumeYes bool
	to        string
	haveTo    bool
	credSrc   string // "", "p", "pf", "pe", "k", "bare"
	credVal   string
	// produced holds the containers this run has written so far (a mask
	// writes several), so the collision rule never offers one of them for
	// overwrite (FDSEC-03).
	produced map[string]bool
}

func parseFdsecArgs(args []string, verb string) (*fdsecOpts, error) {
	o := &fdsecOpts{}
	bare := ""
	for i := 0; i < len(args); i++ {
		t := args[i]
		lt := strings.ToLower(t)
		switch {
		case fdsecOptionWords[lt]:
			// One vocabulary for the parser and the redactor (FDSEC-18).
			switch lt {
			case "del", "delete":
				o.del = true
			case "wipe":
				o.wipe = true
			case "rename", "ren":
				o.rename = true
			case "here":
				o.here = true
			case "suite2":
				o.suite2 = true
			case "start":
				o.start = true
			case "-rw", "rw":
				o.rw = true
			case "-keep", "keep":
				o.keep = true
			case "-y", "y", "--force", "force":
				o.assumeYes = true
			}
		case lt == "to":
			if i+1 >= len(args) {
				return nil, usagef("to requires a destination")
			}
			o.to, o.haveTo = args[i+1], true
			i++
		case strings.HasPrefix(t, "p:"):
			o.credSrc, o.credVal = "p", t[2:]
		case strings.HasPrefix(t, "pf:"):
			o.credSrc, o.credVal = "pf", t[3:]
		case strings.HasPrefix(t, "pe:"):
			o.credSrc, o.credVal = "pe", t[3:]
		case strings.HasPrefix(t, "k:"):
			o.credSrc, o.credVal = "k", t[2:]
		default:
			if bare != "" {
				// Neither token is quoted back: either one may be the password,
				// and this message reaches history.json and the event file
				// (FDSEC-05).
				return nil, usagef("two words that are not options: a bare password is valid only as the single trailing token; use p:<password> when anything else is present")
			}
			bare = t
		}
	}
	hasOption := o.del || o.wipe || o.rename || o.here || o.haveTo || o.assumeYes || o.rw || o.keep || o.start || o.suite2
	if bare != "" {
		// p: is required whenever any option is present, so a password that
		// happens to equal an option word is not eaten as that option.
		if hasOption || o.credSrc != "" {
			return nil, usagef("a bare password is valid only as the sole trailing token; use p:<password>")
		}
		o.credSrc, o.credVal = "bare", bare
	}
	if verb == "secure" && o.here {
		return nil, usagef("here is an unsecure option")
	}
	if verb == "unsecure" && o.wipe {
		return nil, usagef("wipe is a secure option; unsecure del removes the container (a normal, recoverable unlink)")
	}
	// The suite is the writer's choice and named nowhere on disk: a reader
	// finds it by opening (FDSEC-FORMAT.md section 13.4), so there is
	// nothing for any other verb to select.
	if verb != "secure" && o.suite2 {
		return nil, usagef("suite2 is a secure option: every other verb finds the suite by opening the container")
	}
	if verb != "unsecure" && o.start {
		return nil, usagef("start is an unsecure option: it hands the restored file to its registered handler")
	}
	if verb == "verify" && (o.del || o.wipe || o.rename || o.here || o.haveTo) {
		return nil, usagef("verify takes a password, not options")
	}
	// -rw and -keep belong to reveal and to nothing else: they describe where
	// the plaintext lives and how long, which is a question only a reveal
	// asks.
	if verb != "reveal" && (o.rw || o.keep) {
		which := "-rw"
		if o.keep {
			which = "-keep"
		}
		return nil, usagef("%s is a reveal option; %s does not open a sandbox", which, verb)
	}
	if o.rw && o.keep {
		return nil, usagef("-rw and -keep do not combine: -rw leaves no sandbox copy for -keep to keep")
	}
	if o.rename && o.haveTo {
		return nil, usagef("rename and to do not combine: rename writes a random name beside the source; to names the destination exactly")
	}
	return o, nil
}

// resolveFdsecCredential resolves the credential from its source, prompting
// without echo when none was given - twice on secure, because a typo would
// lock the data behind a password the owner never meant. An empty credential
// is accepted; the honest label is printed by the caller.
func resolveFdsecCredential(o *fdsecOpts, confirm bool) (fdsec.Credential, error) {
	switch o.credSrc {
	case "p", "bare":
		return fdsec.NewCredential(o.credVal), nil
	case "pf":
		return fdsecCredentialFromFile(o.credVal, false)
	case "pe":
		v, ok := os.LookupEnv(o.credVal)
		// The variable leaves this process's environment the moment it is
		// read, before anything can start a child: a reveal or `unsecure
		// start` hands the copy to Word or a player, and every program those
		// start would otherwise inherit the password (FDSEC-07).
		if ok {
			_ = os.Unsetenv(o.credVal)
		}
		// A named variable that is unset or empty is a mistake, never a
		// choice: the GUI's "Open in Command" once lost the password this way
		// and `secure wipe -y` then wrote a container with no secrecy and
		// overwrote the original (FDSEC-19, GUI-02). An empty password is
		// still possible - typed at the prompt or as p: - where it is visible.
		if !ok {
			return nil, usagef("environment variable %s is not set, so no password was given; nothing was written", o.credVal)
		}
		if v == "" {
			return nil, usagef("environment variable %s is empty, so no password was given; nothing was written (an empty password is typed at the prompt or given as p: on purpose)", o.credVal)
		}
		return fdsec.NewCredential(v), nil
	case "k":
		return fdsecCredentialFromFile(o.credVal, true)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, usagef("no password given and stdin is not a terminal; use p:<password>, pf:<file>, pe:<VAR> or k:<keyfile>")
	}
	read := func(prompt string) (string, error) {
		fmt.Print(prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	first, err := read("Password (no echo; empty = obfuscation only, no secrecy): ")
	if err != nil {
		return nil, err
	}
	if !confirm {
		return fdsec.NewCredential(first), nil
	}
	second, err := read("Password again: ")
	if err != nil {
		return nil, err
	}
	if first != second {
		return nil, fmt.Errorf("the two passwords differ; nothing was written")
	}
	return fdsec.NewCredential(first), nil
}

func fdsecCredentialFromFile(path string, keyfile bool) (fdsec.Credential, error) {
	if keyfile {
		// The keyfile's credential is the digest of its bytes (spec 5.1):
		// hex-encoded it is 64 characters, high-entropy - the fast branch of
		// the threshold derivation takes it by design. The bytes are
		// streamed into the hash rather than read whole, so a multi-gigabyte
		// keyfile costs a buffer, not the address space (FDSEC-16); the
		// digest is the same.
		sum, err := fdsecKeyfileDigest(path)
		if err != nil {
			return nil, fmt.Errorf("read keyfile: %w", err)
		}
		return fdsec.NewCredential(fmt.Sprintf("%x", sum)), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read password file: %w", err)
	}
	defer clear(b)
	// One trailing newline is the convention of generated password files and
	// almost never part of the intended password; anything more is kept.
	s := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	return fdsec.NewCredential(s), nil
}

// fdsecKeyfileDigest is BLAKE2b-256 of a keyfile's bytes, streamed.
func fdsecKeyfileDigest(path string) ([32]byte, error) {
	var sum [32]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h, err := blake2b.New256(nil)
	if err != nil {
		return sum, err
	}
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// fdsecSecure packs one file or folder (or every one a mask matches - one
// container per target, Q10; a folder is one target, packed whole) into a
// verified container. The credential is resolved once
// for the whole mask, after every target has been screened: a prompt that
// repeats per file would train the user to type the password into whatever
// asks, and a refusal that arrives after the password was typed is a refusal
// that wasted a secret.
func fdsecSecure(path string, args []string, hl *HistoryLogger) error {
	o, err := parseFdsecArgs(args, "secure")
	if err != nil {
		return err
	}
	masked := strings.ContainsAny(filepath.Base(path), "*?")
	targets := []string{path}
	if masked {
		matches, gerr := filepath.Glob(path)
		if gerr != nil {
			return fmt.Errorf("expand mask %s: %w", path, gerr)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no file matches %s", path)
		}
		targets = matches
		// A mask with `to` naming one exact file would send every match to
		// that one path - the second overwriting or colliding with the first,
		// whose original may already be gone (FDSEC-03). A mask goes to a
		// folder or beside its sources, never to one file.
		if o.haveTo {
			if st, err := os.Stat(o.to); err != nil || !st.IsDir() {
				return usagef("a mask packs one container per file, so to must name an existing folder, not %s", o.to)
			}
		}
	}

	var firstErr error
	screened := make([]string, 0, len(targets))
	for _, t := range targets {
		if serr := fdsecScreenSource(t); serr != nil {
			if !masked {
				return serr
			}
			if firstErr == nil {
				firstErr = serr
			}
			fmt.Fprintf(os.Stderr, "Skipped: %v\n", serr)
			continue
		}
		screened = append(screened, t)
	}
	if len(screened) == 0 {
		return fmt.Errorf("nothing to pack under %s: %w", path, firstErr)
	}

	cred, err := resolveFdsecCredential(o, true)
	if err != nil {
		return err
	}
	if len(cred) == 0 {
		fmt.Println("Note: the password is EMPTY - the container is obfuscation only, with no secrecy: anyone can open it.")
	}

	packed := 0
	o.produced = map[string]bool{}
	for _, t := range screened {
		// No further target is started after a stop (FDSEC-02).
		if runStopRequested() {
			fmt.Printf("Stopped: %d of %d packed; the rest were not touched.\n", packed, len(screened))
			return errFdsecStopped
		}
		if perr := fdsecSecureOne(t, o, cred, hl); perr != nil {
			if fdsecStopped(perr) {
				fmt.Printf("Stopped: %d of %d packed; the rest were not touched.\n", packed, len(screened))
				return perr
			}
			if !masked {
				return perr
			}
			if firstErr == nil {
				firstErr = perr
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", perr)
			continue
		}
		packed++
	}
	if firstErr != nil {
		return fmt.Errorf("%d of %d files packed; first failure: %w", packed, len(targets), firstErr)
	}
	return nil
}

// fdsecScreenSource refuses everything that is not a plain, not-yet-packed
// file or an ordinary folder, naming the alternative rather than the rule
// (invariant 5). A folder's own entries are screened by the walk
// (fdsec.ScanTree), which refuses a reparse point anywhere inside it; here the
// folder itself is screened, the way a file is.
func fdsecScreenSource(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if fdsecRefusedReparse(path) {
			return fmt.Errorf("%s is a reparse point (junction/symlink/mount point) and is refused as a source", path)
		}
		if vol := filepath.VolumeName(path); vol != "" {
			if abs, aerr := filepath.Abs(path); aerr == nil && filepath.Clean(abs) == filepath.Clean(vol+`\`) {
				return fmt.Errorf("%s is the root of a drive: secure packs one folder, never a whole drive", path)
			}
		}
		return nil
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if fdsecRefusedReparse(path) {
		return fmt.Errorf("%s is a reparse point (junction/symlink/mount point) and is refused as a source", path)
	}
	if strings.EqualFold(filepath.Ext(path), ".fd-sec") {
		return fmt.Errorf("%s is already a .fd-sec container: packing a container again is refused; unsecure it first if that is what you meant", path)
	}
	return nil
}

func fdsecSecureOne(path string, o *fdsecOpts, cred fdsec.Credential, hl *HistoryLogger) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if o.suite2 {
			return usagef("%s is a folder, and suite 2 seals exactly one file; a folder packs as suite 3 - drop suite2", path)
		}
		return fdsecSecureTree(path, o, cred, hl)
	}

	// Destination: beside the source by default, original extension dropped
	// from the visible name (Q3); rename = a random name with no extension at
	// all - a nameless blob (spec 5.6).
	dstPath, err := fdsecContainerPath(path, fi.Name(), o)
	if err != nil {
		return err
	}
	// The container can never be the original itself: `secure to <source>`
	// used to delete the only copy on "o = overwrite" and then fail to open
	// it (FDSEC-03). Refused before a byte moves.
	if same, _ := sameFilePaths(path, dstPath); same {
		return usagef("the destination %s is the file being packed; name another destination", dstPath)
	}
	snapshot := takeFdsecSnapshot(path, fi)

	// Force exit only: a graceful stop ends PackFile at a chunk boundary and
	// PackFile removes its own temporary file after closing it, so the
	// cleanup never acts on a handle the pack still owns (FDSEC-02). The
	// registration ends with this file.
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			removeFdsecPartials(dstPath)
		}
	})
	defer removeCleanup()

	meta := fdsecMetadataFromStat(fi)
	suite := fdsec.SuiteID1
	var info fdsec.Info
	// The collision rule runs before the pack and again whenever the name
	// turns out to be taken at the final rename - another secure of a file
	// with the same stem (IMG_0001.JPG and IMG_0001.CR2 from one Explorer
	// multi-select) may have claimed it in the meantime. The rename never
	// replaces what it finds (FDSEC-01); only an overwrite the user chose for
	// that exact path does, and only after the new container verified
	// (FDSEC-03).
	for attempt := 0; ; attempt++ {
		var replace bool
		dstPath, replace, err = fdsecResolveCollision(dstPath, o, false)
		if err != nil {
			return err
		}
		opts := []fdsec.StreamOption{newFdsecTracker("secure", fi.Size()), fdsecStreamStop()}
		if replace {
			opts = append(opts, fdsec.AllowReplace())
		}
		if o.suite2 {
			// Suite 2 seals the name, the size and the time of encryption only;
			// the original's own timestamps are not carried (FDSEC-FORMAT.md 18.9).
			suite = fdsec.SuiteID2
			info, err = fdsec.PackFileSuite2(dstPath, path, meta, cred, opts...)
		} else {
			info, err = fdsec.PackFile(dstPath, path, meta, cred, fdsec.Params{}, opts...)
		}
		if errors.Is(err, fdsec.ErrExists) && attempt < 5 {
			fmt.Printf("%s%s was created by something else while this pack ran; choosing again.\n", fdsecEndProgress(), dstPath)
			continue
		}
		if fdsecStopped(err) {
			fmt.Printf("%sStopped: nothing was written for %s; the original is untouched.\n", fdsecEndProgress(), path)
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
	fmt.Printf("%sOK %s -> %s (suite %d, %s, %s, %d chunks, container %s)\n",
		fdsecEndProgress(), path, dstPath, suite, secrecy, formatBytes(uint64(info.Size)), info.Chunks, formatBytes(uint64(info.TotalLen)))
	if o.rename {
		// Neither the original's name nor the blob's: the folder is all the
		// history may know, or it becomes the map from one to the other
		// (CLI-21, decision recorded 2026-09-25).
		hl.HideTarget(path)
		hl.SetResult("container", filepath.Dir(dstPath))
	} else {
		hl.SetResult("container", dstPath)
	}
	hl.SetResult("suite", suite)
	hl.SetResult("secrecy", secrecy)

	// Disposition of the original - only now: PackFile already reopened the
	// container and verified every chunk against the original's own digest
	// (invariant 1). Offered, prompted, never assumed (Q9).
	if o.del || o.wipe {
		kept := func(reason string) { fmt.Printf("Original kept (%s): %s\n", reason, path) }
		if fdsecBeforeDisposition != nil {
			fdsecBeforeDisposition(path)
		}
		// Nothing is deleted under a Stopped verdict (FDSEC-02).
		if runStopRequested() {
			kept("stopped before the original was removed; the container is complete and verified")
			hl.SetResult("original", "kept")
			return errFdsecStopped
		}
		// The container holds the original as it was when it was packed; an
		// edit made since - during the read-back, or while the question below
		// waits - would be deleted with it (FDSEC-12). Checked here and again
		// right before the removal.
		if why := snapshot.changedSince(path); why != "" {
			kept(why)
			hl.SetResult("original", "kept")
			return nil
		}
		// lastCheck is the same two questions asked immediately before the
		// removal, after any prompt: the answer it returns is what the run
		// reports when the original is kept.
		lastCheck := func() (proceed bool, err error) {
			if runStopRequested() {
				kept("stopped before the original was removed; the container is complete and verified")
				hl.SetResult("original", "kept")
				return false, errFdsecStopped
			}
			if why := snapshot.changedSince(path); why != "" {
				kept(why)
				hl.SetResult("original", "kept")
				return false, nil
			}
			return true, nil
		}
		switch {
		case o.wipe:
			// The caveat is information, not a prompt, so -y does not skip it
			// (invariant 9): a user who automates the wipe still has to be
			// told what an overwrite-in-place is worth on modern storage.
			fmt.Printf("\nThe container was verified against the original's digest.\n")
			fmt.Printf("WIPE will overwrite and remove the original %s.\n", path)
			fmt.Printf("Honest caveat: on SSDs, copy-on-write and journaled volumes, overwrite-in-place\nlowers the odds of recovery but does not guarantee erasure.\n")
			if !o.assumeYes {
				fmt.Printf("Type WIPE to continue: ")
				line, rerr := readConsoleLine()
				if rerr != nil || strings.TrimSpace(line) != "WIPE" {
					kept("wipe not confirmed")
					return nil
				}
			}
			if ok, cerr := lastCheck(); !ok {
				return cerr
			}
			if werr := wipeFileInPlace(path); werr != nil {
				return werr
			}
			fmt.Printf("Original overwritten and removed: %s\n", path)
			hl.SetResult("original", "wiped")
		case o.del:
			if !o.assumeYes {
				fmt.Printf("\nThe container was verified against the original's digest.\n")
				fmt.Printf("Delete the original %s? This is a normal delete - the bytes stay recoverable until reused (y/N): ", path)
				line, rerr := readConsoleLine()
				ans := strings.ToLower(strings.TrimSpace(line))
				if rerr != nil || (ans != "y" && ans != "yes") {
					kept("delete not confirmed")
					return nil
				}
			}
			if ok, cerr := lastCheck(); !ok {
				return cerr
			}
			if rerr := os.Remove(path); rerr != nil {
				return rerr
			}
			fmt.Printf("Original removed (recoverable unlink; use wipe for the stronger form): %s\n", path)
			hl.SetResult("original", "deleted")
		}
	}
	return nil
}

// fdsecContainerPath resolves where a container lands: beside the source
// (default), inside `to <dir>`, or exactly at `to <path>` whose parent must
// exist - a folder is never guessed into being (spec 5.6).
func fdsecContainerPath(srcPath, srcName string, o *fdsecOpts) (string, error) {
	return fdsecContainerPathFor(srcPath, strings.TrimSuffix(srcName, filepath.Ext(srcName)), o)
}

// fdsecContainerPathFor is fdsecContainerPath with the visible stem already
// chosen: a file drops its extension, a folder keeps its whole name - a
// folder's name has no extension to drop (FDSEC-FORMAT section 1).
func fdsecContainerPathFor(srcPath, stem string, o *fdsecOpts) (string, error) {
	if stem == "" {
		stem = "_"
	}
	name := stem + ".fd-sec"
	if o.rename {
		name = fdsecRandomName()
	}
	if !o.haveTo {
		return filepath.Join(filepath.Dir(srcPath), name), nil
	}
	if st, err := os.Stat(o.to); err == nil {
		if st.IsDir() {
			return filepath.Join(o.to, name), nil
		}
		return o.to, nil // exact container path; the caller's collision check handles existence
	}
	if _, perr := os.Stat(filepath.Dir(o.to)); perr != nil {
		return "", fmt.Errorf("destination parent %s does not exist; refusing to invent a folder (create it first if you want it)", filepath.Dir(o.to))
	}
	return o.to, nil
}

// fdsecResolveCollision never clobbers silently (invariant 2, spec 5.6):
// under -y the name gains a numeric suffix; interactively the user may
// overwrite, suffix or cancel.
//
// "Overwrite" deletes nothing here. It returns replace=true, and the caller
// hands that to the one rename that puts the new, verified bytes in place -
// so a pack that fails after the answer, a source that cannot be opened, or a
// destination that is the source itself costs the user nothing (FDSEC-03).
// A container this very run produced (the first match of a mask whose second
// match has the same stem) is never offered for overwrite: its original may
// already be gone. With hideName the message names the folder rather than the
// file, because a restored file's name is the container's sealed name
// (FDSEC-06).
func fdsecResolveCollision(dstPath string, o *fdsecOpts, hideName bool) (string, bool, error) {
	return fdsecResolveCollisionWith(dstPath, o, hideName, fdsecAskCollision)
}

// fdsecAskCollision is the console half of the collision rule, a variable so
// the rule can be tested without a console.
var fdsecAskCollision = func(shown string) (string, error) {
	// Off a terminal there is nobody to answer, and an unanswerable prompt
	// must not come back as an I/O error: say what to pass instead.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", usagef("%s already exists and stdin is not a terminal, so the overwrite question cannot be asked; pass -y to write a suffixed name instead, or name the destination with to <path>", shown)
	}
	fmt.Printf("\n%s already exists.\n  o = overwrite it, s = write a suffixed name (default), anything else cancels: ", shown)
	line, err := readConsoleLine()
	if err != nil {
		return "", fmt.Errorf("collision at %s: %w", shown, err)
	}
	return line, nil
}

func fdsecResolveCollisionWith(dstPath string, o *fdsecOpts, hideName bool, ask func(shown string) (string, error)) (string, bool, error) {
	shown := dstPath
	if hideName {
		shown = "a file with the restored name in " + filepath.Dir(dstPath)
	}
	if _, err := os.Lstat(dstPath); os.IsNotExist(err) {
		return dstPath, false, nil
	} else if err != nil {
		if hideName {
			return "", false, fmt.Errorf("cannot examine the destination in %s: %w", filepath.Dir(dstPath), errors.Unwrap(err))
		}
		return "", false, err
	}
	producedHere := o != nil && o.produced != nil && o.produced[fdsecPathKey(dstPath)]
	if (o != nil && o.assumeYes) || producedHere {
		suffixed, err := fdsecSuffixedName(dstPath, false)
		if err != nil {
			return "", false, err
		}
		if hideName {
			fmt.Printf("Exists, -y given: writing a suffixed name in %s instead\n", filepath.Dir(dstPath))
		} else {
			fmt.Printf("Exists, -y given: writing %s instead\n", suffixed)
		}
		return suffixed, false, nil
	}
	line, err := ask(shown)
	if err != nil {
		return "", false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "o":
		return dstPath, true, nil
	case "s", "":
		suffixed, err := fdsecSuffixedName(dstPath, false)
		return suffixed, false, err
	default:
		return "", false, fmt.Errorf("cancelled: %s exists", shown)
	}
}

func fdsecIsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// fdsecPathKey is a path in the form two spellings of one path share.
func fdsecPathKey(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return strings.ToLower(filepath.Clean(p))
}

// fdsecRandomName: Latin letters and digits, up to 24 characters, no
// extension (spec 5.1, the rename option).
func fdsecRandomName() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 24)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic("fdsec: crypto/rand failed: " + err.Error())
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

// fdsecRefuseMask turns a wildcard target into the usage error it actually is.
// Only `secure` takes a mask (Q10: one container per matched file); every verb
// that *opens* a container takes exactly one. Without this the mask reaches
// the filesystem and comes back as "The filename, directory name, or volume
// label syntax is incorrect" at exit 5 - an I/O class for what is plainly a
// usage error, which is the one thing the error taxonomy exists to prevent.
//
// For `reveal` there is a second reason, and it is not ergonomics: a batch of
// reveals would put many plaintext copies on disk at once, which is the shape
// of malware staging rather than of opening a file. The reveal is deliberately
// one file at a time.
func fdsecRefuseMask(path, verb string) error {
	if !strings.ContainsAny(path, "*?") {
		return nil
	}
	if verb == "reveal" {
		return usagef("%s is a mask, and reveal takes exactly one container. A reveal writes a readable copy, so a batch of them would put many plaintext copies on disk at once - that is deliberately not offered. Reveal them one at a time", path)
	}
	return usagef("%s is a mask, and %s takes exactly one container (a mask is a secure option: one container per matched file). Name the container, or loop over them from a .lst batch file", path, verb)
}

// fdsecScreenContainer is the screening every path that opens a container
// performs before it asks for a credential: a folder is not a container, a
// reparse point is refused as a source (invariant 6), and a file too small or
// wrongly sized to hold one is refused before a password is ever asked for.
// That length test is all the screening there can be: a container carries no
// marker, so nothing short of the password can say whether this file is one.
// Shared by unsecure and reveal so the two cannot drift apart on what they
// accept.
func fdsecScreenContainer(path, verb string) error {
	if err := fdsecRefuseMask(path, verb); err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%s is a folder, not a container", path)
	}
	if fdsecRefusedReparse(path) {
		return fmt.Errorf("%s is a reparse point (junction/symlink/mount point) and is refused as a source", path)
	}
	if err := fdsec.ScreenLength(fi.Size()); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// fdsecUnsecure restores the original from a verified container.
func fdsecUnsecure(path string, args []string, hl *HistoryLogger) error {
	o, err := parseFdsecArgs(args, "unsecure")
	if err != nil {
		return err
	}
	if err := fdsecScreenContainer(path, "unsecure"); err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	// The restored file can never land on the container itself: `unsecure to
	// <the container> del` answered "o" then "y" used to remove both
	// (FDSEC-03). Refused before a byte moves.
	if o.haveTo {
		if same, _ := sameFilePaths(path, o.to); same {
			return usagef("the destination %s is the container being restored; name another destination", o.to)
		}
	}

	// SP-0008: `start` restores into a protected per-run sandbox and removes
	// the copy when the console window closes, instead of leaving the
	// decrypted original beside the container for good. `here` and `to` are
	// the user naming a destination of their own, so those keep the ordinary
	// restore - a permanent file where they asked for it - and only the
	// default location changes.
	sandboxed := o.start && !o.here && !o.haveTo
	if sandboxed && o.del {
		return usagef("start and del do not combine: start now restores into a temporary sandbox and removes the copy when the window closes, so deleting the container would leave you with neither. Name a destination you keep - unsecure <container> to <dest> start del - or unsecure it first and start it yourself")
	}
	var startRoot string
	if sandboxed {
		startRoot, err = fdsecStartRoot(fi.Size())
		if err != nil {
			return err
		}
	}

	cred, err := resolveFdsecCredential(o, false)
	if err != nil {
		return err
	}

	// One key derivation opens the head, and only then is it known whether the
	// container holds one file or a folder (the suite is inside the mask).
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	hl.SetResult("container", path)
	c, err := fdsec.Open(src, cred)
	if err != nil {
		return err
	}
	if c.IsTree() {
		if o.start {
			return usagef("%s holds a folder, and start hands one file to its registered handler. Restore the folder instead: filedo %s unsecure [here | to <dest>]", path, path)
		}
		return fdsecUnsecureTree(path, c, src, fi.Size(), o, hl)
	}

	// Restore to a temporary sibling, then rename into the final name once
	// every chunk and the final digest have verified (invariant 3: an
	// interrupted restore leaves nothing that looks complete). The sibling is
	// inside the sandbox for a `start`: a temporary plaintext beside the
	// container would leak exactly what the sandbox exists to hide, however
	// briefly.
	var dir string
	var sb *fdsecStartSandbox
	if sandboxed {
		sb, err = fdsecStartSandboxOpen(startRoot)
		if err != nil {
			return err
		}
		defer sb.closeUnlessScheduled()
		dir = sb.dir
	} else {
		dir, err = fdsecRestoreDir(o, path)
		if err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".fdsec-restore-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	remove := true
	defer func() {
		if remove {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()
	// A graceful stop ends the unpack at a chunk boundary and the defer above
	// closes and removes the temporary file. A forced exit skips every defer,
	// so this cleanup closes the handle itself - an open file cannot be
	// deleted on Windows, which is how a second Ctrl+C used to leave a
	// plaintext .fdsec-restore-* beside the container (FDSEC-02).
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			tmp.Close()
			os.Remove(tmpPath)
		}
	})
	defer removeCleanup()

	meta, err := c.Unpack(tmp, newFdsecTracker("unsecure", fi.Size()), fdsecStreamStop())
	if fdsecStopped(err) {
		fmt.Printf("%sStopped: nothing was restored and the container is untouched.\n", fdsecEndProgress())
		return errFdsecStopped
	}
	if err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runStopRequested() {
		fmt.Printf("%sStopped: nothing was restored and the container is untouched.\n", fdsecEndProgress())
		return errFdsecStopped
	}

	// Final name: the true name by default (here = the current directory,
	// rename = a random one, to = an exact path or directory). A sandboxed
	// start resolves its own, because the sealed name has to be screened
	// before it names anything - see fdsecStartSandbox.path.
	var finalPath string
	if sandboxed {
		finalPath, err = sb.path(meta.Name, o.rename)
		if err != nil {
			return err
		}
		if err := renameNoReplace(tmpPath, finalPath); err != nil {
			return fdsecScreenEventError(fdsecRenameError(err))
		}
	} else {
		finalPath, err = fdsecRestorePath(path, meta.Name, o)
		if err != nil {
			return fdsecScreenEventError(err)
		}
		// The collision rule, the same-file refusal and a rename that never
		// replaces what it finds: a name that appeared since the question was
		// asked sends the rule round again rather than being overwritten
		// (FDSEC-01).
		for attempt := 0; ; attempt++ {
			var replace bool
			finalPath, replace, err = fdsecResolveCollision(finalPath, o, true)
			if err != nil {
				return fdsecScreenEventError(err)
			}
			if same, _ := sameFilePaths(finalPath, path); same {
				return usagef("the restored file would replace the container it comes from; name another destination with to <path>")
			}
			if replace {
				err = renameReplace(tmpPath, finalPath)
			} else {
				err = renameNoReplace(tmpPath, finalPath)
			}
			if errors.Is(err, errDestinationExists) && attempt < 5 {
				continue
			}
			if err != nil {
				return fdsecScreenEventError(fdsecRenameError(err))
			}
			break
		}
	}
	remove = false
	fdsecRestoreTimes(finalPath, meta)

	fmt.Printf("%sOK %s -> %s (%s, %s)\n", fdsecEndProgress(), path, finalPath, meta.Name, formatBytes(uint64(meta.Size)))
	if sandboxed {
		sb.copyPath = finalPath
		// The sandbox directory, never the copy's name. The true name is
		// sealed metadata and history.json is a permanent file on the user's
		// disk, while this copy is meant to last minutes - the same rule
		// reveal already follows (SP-0008 5).
		hl.SetResult("restored", sb.dir)
	} else {
		// BEHAVIOUR §6.8: never write the sealed true name into a log.
		// Record the destination folder rather than finalPath.
		hl.SetResult("restored", dir)
	}

	// start hands the restored file straight to its registered handler - the
	// owner's choice (2026-09-22) is to launch it exactly as a double-click
	// would, with no executable/script refusal: this is the file the user
	// just chose to unpack, not reveal's not-quite-trusted sandbox copy, so
	// the same trust decision as an ordinary double-click applies.
	if o.start {
		// Nothing is launched after a stop (FDSEC-02); the copy goes the way
		// every unlaunched sandbox copy goes.
		if runStopRequested() {
			fmt.Printf("Stopped: the copy was not handed to any program.\n")
			hl.SetResult("launched", false)
			sb.scheduleClose()
			return errFdsecStopped
		}
		if lerr := fdsecLaunchCopy(finalPath); lerr != nil {
			fmt.Printf("Could not hand %s to a registered handler (%v).\n", finalPath, lerr)
			hl.SetResult("launched", false)
		} else {
			hl.SetResult("launched", true)
		}
		// One attempt, made when the window that started this closes
		// (SP-0008 4.3). It follows the launch either way: a copy nothing
		// could open is exactly the one with no reason to survive the run.
		sb.scheduleClose()
	}

	// The container is removed only after the restored file verified (the
	// digest check inside Unpack is that verification).
	if o.del {
		return fdsecDeleteContainer(path, src, o, hl, "The restored file was verified against the packed digest.")
	}
	return nil
}

// fdsecDeleteContainer is unsecure's `del`: offered after the restore
// verified, prompted unless -y, a normal unlink.
func fdsecDeleteContainer(path string, src *os.File, o *fdsecOpts, hl *HistoryLogger, verified string) error {
	// Windows refuses to unlink a file this process still holds open.
	src.Close()
	// Nothing is deleted under a Stopped verdict (FDSEC-02).
	if runStopRequested() {
		fmt.Printf("Stopped: container kept: %s\n", path)
		return errFdsecStopped
	}
	if !o.assumeYes {
		fmt.Printf("\n%s\n", verified)
		fmt.Printf("Delete the container %s? (y/N): ", path)
		line, rerr := readConsoleLine()
		ans := strings.ToLower(strings.TrimSpace(line))
		if rerr != nil || (ans != "y" && ans != "yes") {
			fmt.Printf("Container kept: %s\n", path)
			return nil
		}
	}
	if runStopRequested() {
		fmt.Printf("Stopped: container kept: %s\n", path)
		return errFdsecStopped
	}
	if rerr := os.Remove(path); rerr != nil {
		return rerr
	}
	fmt.Printf("Container removed: %s\n", path)
	hl.SetResult("container", "removed")
	return nil
}

// fdsecRestoreDir resolves the destination directory for the restored file
// before unpacking, so temporary files are always created on the target volume.
func fdsecRestoreDir(o *fdsecOpts, containerPath string) (string, error) {
	switch {
	case o.here:
		return os.Getwd()
	case o.haveTo:
		if st, err := os.Stat(o.to); err == nil && st.IsDir() {
			return o.to, nil
		}
		parent := filepath.Dir(o.to)
		if _, perr := os.Stat(parent); perr != nil {
			return "", fmt.Errorf("destination parent %s does not exist; refusing to invent a folder (create it first if you want it)", parent)
		}
		return parent, nil
	default:
		return filepath.Dir(containerPath), nil
	}
}

// fdsecRestorePath resolves where the restored original lands.
func fdsecRestorePath(containerPath, trueName string, o *fdsecOpts) (string, error) {
	name := trueName
	if o.rename {
		name = fdsecRandomName()
	} else if utf16Len(name) > fdsecMaxNameUnits && !(o.haveTo && !fdsecIsDir(o.to)) {
		// A sealed name longer than any file system allows would fail at the
		// rename with the name in the message (FDSEC-06); say so without it.
		return "", usagef("the container's sealed name is longer than %d characters, which no file system accepts; restore it with rename or to <path>", fdsecMaxNameUnits)
	}
	switch {
	case o.here:
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(wd, name), nil
	case o.haveTo:
		if st, err := os.Stat(o.to); err == nil && st.IsDir() {
			return filepath.Join(o.to, name), nil
		}
		if _, perr := os.Stat(filepath.Dir(o.to)); perr != nil {
			return "", fmt.Errorf("destination parent %s does not exist; refusing to invent a folder (create it first if you want it)", filepath.Dir(o.to))
		}
		return o.to, nil
	default:
		return filepath.Join(filepath.Dir(containerPath), name), nil
	}
}

// fdsecInfo reports the container's layout. It asks for the password like
// every other read: a container carries no marker and its head is masked, so
// there is nothing a password-free look could report - not even whether the
// file is a container at all.
func fdsecInfo(path string, args []string, hl *HistoryLogger) error {
	o, err := parseFdsecArgs(args, "info") // password only: info writes nothing
	if err != nil {
		return err
	}
	if err := fdsecRefuseMask(path, "info"); err != nil {
		return err
	}
	cred, err := resolveFdsecCredential(o, false)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hi, err := fdsec.ReadHeaderInfo(f, cred)
	if err != nil {
		return err
	}
	fmt.Printf("FileDO container (suite %d, format version %d)\n", hi.SuiteID, hi.FormatVersion)
	fmt.Printf("  file:            %s (%s)\n", path, formatBytes(uint64(hi.ContainerSize)))
	if hi.IsTree {
		fmt.Printf("  holds:           a folder - %d entries, %s in files\n", hi.TreeEntries, formatBytes(uint64(hi.TreeSize)))
	} else {
		fmt.Printf("  holds:           one file\n")
	}
	if hi.SuiteID == fdsec.SuiteID2 {
		// Suite 2 has no head to report: its frame is a suite constant and its
		// profile is the try-list entry that opened it (FDSEC-FORMAT.md 18).
		fmt.Printf("  frame:           %s, no cluster alignment\n", formatBytes(uint64(hi.ChunkSize)))
		fmt.Printf("  KDF (suite 2 try-list, the profile that opened it): pepper fold, then Argon2id %d MiB, T=%d, P=%d; no fast branch\n", hi.KDFMemoryKiB/1024, hi.KDFTime, hi.KDFLanes)
	} else {
		fmt.Printf("  chunk slot:      %s, cluster alignment %s\n", formatBytes(uint64(hi.ChunkSize)), formatBytes(uint64(hi.ClusterAlignment)))
		fmt.Printf("  KDF (fixed by format version %d): Argon2id %d MiB, T=%d, P=%d; fast branch at %d password bytes\n", hi.FormatVersion, hi.KDFMemoryKiB/1024, hi.KDFTime, hi.KDFLanes, hi.Threshold)
	}
	switch {
	case hi.SuiteID == fdsec.SuiteID2:
		fmt.Printf("  sealed (needs the password): true name, real size, time of encryption - suite 2 does not carry the original's timestamps\n")
	case hi.IsTree:
		fmt.Printf("  sealed (needs the password): the folder's name, every entry's path, size and timestamps\n")
		hl.SetResult("folder", true)
		hl.SetResult("entries", hi.TreeEntries)
	default:
		fmt.Printf("  sealed (needs the password): true name, real size, timestamps\n")
	}
	fmt.Printf("  nothing in the file says it is one: no marker, no version in the clear\n")
	fmt.Printf("  an EMPTY password means obfuscation only, no secrecy\n")
	hl.SetResult("suite", hi.SuiteID)
	return nil
}

// fdsecVerify proves a container end to end: every chunk authenticated and
// the recovered bytes hashed to the packed digest.
func fdsecVerify(path string, args []string, hl *HistoryLogger) error {
	o, err := parseFdsecArgs(args, "verify") // password only: verify writes nothing
	if err != nil {
		return err
	}
	if err := fdsecRefuseMask(path, "verify"); err != nil {
		return err
	}
	cred, err := resolveFdsecCredential(o, false)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	started := time.Now()
	c, err := fdsec.Open(f, cred)
	if err != nil {
		return err
	}
	if c.IsTree() {
		tm, entries, err := c.VerifyTree()
		if err != nil {
			return err
		}
		files := 0
		for _, e := range entries {
			if !e.IsDir() {
				files++
			}
		}
		fmt.Printf("OK %s verified in %s: the manifest and every entry authenticated, every file hashes to its packed digest\n", path, formatDuration(time.Since(started)))
		fmt.Printf("  folder: %s\n  entries: %d (%d files, %d folders)\n  total size: %s\n", tm.Name, tm.Entries, files, len(entries)-files, formatBytes(uint64(tm.TotalSize)))
		hl.SetResult("verified", true)
		hl.SetResult("folder", true)
		hl.SetResult("entries", tm.Entries)
		return nil
	}
	meta, err := c.Unpack(io.Discard)
	if err != nil {
		return err
	}
	if c.Suite() == fdsec.SuiteID2 {
		// Suite 2 stores no digest: every frame's tag, the index and the
		// last-frame flag bound into each, are the whole proof (18.6).
		fmt.Printf("OK %s verified in %s: suite 2, every frame authenticated in order through the flagged last one\n", path, formatDuration(time.Since(started)))
	} else {
		fmt.Printf("OK %s verified in %s: every chunk authenticated, the recovered bytes hash to the packed digest\n", path, formatDuration(time.Since(started)))
	}
	fmt.Printf("  true name: %s\n  real size: %s\n", meta.Name, formatBytes(uint64(meta.Size)))
	hl.SetResult("verified", true)
	return nil
}

// wipeFileInPlace overwrites the file's bytes with one pass of random data,
// syncs, and removes it - the single-file form of the wipe persona (spec
// 5.5), with the honest caveat printed by the caller.
func wipeFileInPlace(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	block := make([]byte, 1<<20)
	if _, err := io.ReadFull(rand.Reader, block); err != nil {
		f.Close()
		return err
	}
	written := int64(0)
	for written < fi.Size() {
		n := int64(len(block))
		if rem := fi.Size() - written; rem < n {
			n = rem
		}
		if _, err := f.Write(block[:n]); err != nil {
			f.Close()
			return err
		}
		written += n
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}

// fdsecRenameError says what failed without repeating the two paths: the
// destination carries the restored file's sealed name.
func fdsecRenameError(err error) error {
	var le *os.LinkError
	if errors.As(err, &le) {
		return fmt.Errorf("could not move the restored file into place: %w", le.Err)
	}
	return fmt.Errorf("could not move the restored file into place: %w", err)
}

// removeFdsecPartials removes the temporary files PackFile may have left at
// an interrupted destination (registered as a Ctrl+C cleanup).
func removeFdsecPartials(dstPath string) {
	dir, base := filepath.Split(dstPath)
	matches, _ := filepath.Glob(filepath.Join(dir, base+".fdsec-partial-*"))
	for _, m := range matches {
		os.Remove(m)
	}
}

func readConsoleLine() (string, error) {
	var line string
	if _, err := fmt.Scanln(&line); err != nil && err.Error() != "unexpected newline" {
		return "", err
	}
	return line, nil
}

// fdsecProgressThreshold is the size below which a progress line is noise:
// the operation is over before the first update would be due.
const fdsecProgressThreshold = 16 << 20

// fdsecProgressShown records whether the progress line was engaged, so the
// result line opens with the newline that clears it - and only then.
var fdsecProgressShown bool

// newFdsecTracker wires the shared progress line (progress.go) to a pack or
// unpack stream, reusing the repository's tracker rather than a second format.
func newFdsecTracker(op string, total int64) fdsec.StreamOption {
	if total < fdsecProgressThreshold {
		return fdsec.WithProgress(nil)
	}
	capacity := int64(fdsec.DefaultChunkSize) - 16
	chunks := (total + capacity - 1) / capacity
	if chunks < 1 {
		chunks = 1
	}
	pt := NewProgressTrackerWithInterval(chunks, total, 500*time.Millisecond)
	i := int64(0)
	fdsecProgressShown = true
	return fdsec.WithProgress(func(done, _ int64) {
		i++
		pt.Update(i, done)
		pt.PrintProgress(op)
	})
}

// fdsecEndProgress closes the progress line if there was one, so a small file
// does not get a stray blank line before its result.
func fdsecEndProgress() string {
	if fdsecProgressShown {
		fdsecProgressShown = false
		return "\n"
	}
	return ""
}
