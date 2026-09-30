package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"filedo/fdsec"
	"filedo/statedir"
	"filedo/vdisk"
)

// Virtual disks (SP-0004): the command surface - create a container, mount it
// as a drive letter, read it, copy it, change it and remove it. The grammar is
// spec 5.2's as the owner fixed it for P6; every verb that acts on an existing
// container has both forms, `filedo <file.fdd> <verb> ..` and
// `filedo vd <verb> <name|path> ..`:
//
//	filedo vd new <path> <size> [plain|fast|ram|vault] [label <text>] [password]
//	filedo <file.fdd> mount [ro] [noscan] [as <X:>] [password]
//	filedo <file.fdd|X:> unmount [force] [nosave]
//	filedo <file.fdd ..|mask> info
//	filedo <file.fdd ..|mask> verify [password]      - read-only; damage is class 4
//	filedo <file.fdd> export <dest> [raw|vhd] [password]   (also: to <dest>)
//	filedo <file.fdd|X:> save                        - ram: save now
//	filedo <file.fdd> compact [password]
//	filedo <file.fdd> grow <size> [password]
//	filedo <file.fdd> format [fs ntfs|exfat] [label <text>] [force] [password]
//	filedo <file.fdd> seal <new.fdd> [nopass] [password]   - a sealed, read-only copy
//	filedo <file.fdd> clone <new.fdd> [nopass] [password]  - a writable copy
//	filedo <file.fdd> pass [<old credential>] [new <new credential>]
//	filedo <file.fdd> destroy [wipe] [force]
//	filedo vd list | status [json] | stop
//	filedo vd add <file.fdd> [as <name>] | forget <name|path>
//	filedo vd auto <name|path> logon | auto off <name|path>
//	filedo vd guard on | off | run | status       - the shutdown guard (SP-0080)
//	filedo vd register [-all-users] | unregister [-all-users]   - the .fdd type in Explorer
//
// A password is the shared grammar of credential_args.go: omitted is a
// prompt, or p:, pf:, pe:, k:; a bare token only for mount. An empty
// credential is obfuscation, never encryption, and nopass writes an
// obfuscated copy - never called decrypted or unprotected. The verbs and
// their words are the table in vdisk_verbs.go.
//
// Mounting is opt-in by its nature: nothing listens and nothing is elevated
// until a person types mount, and every mount asks Windows for administrator
// consent for the one step that needs it (S0 G0 decision (b)). The block
// server itself never runs elevated. The packaged (Store) build carries no
// mount path and refuses the verbs that need it with class 6 (T6.26).

// errVdTransport is exit class 7: the mount path exists in this build but
// cannot run now (FDD-BEHAVIOUR section 4).
var errVdTransport = errors.New("the transport is unavailable")

func errTransport(msg string) error { return fmt.Errorf("%w: %s", errVdTransport, msg) }
func errBusy(msg string) error      { return fmt.Errorf("%w: %s", vdisk.ErrBusy, msg) }
func vdUsagef(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", vdisk.ErrUsage, fmt.Sprintf(format, args...))
}

const vdExitTransport = 7

func vdExitClass(err error) int {
	switch {
	case errors.Is(err, errVdTransport):
		return vdExitTransport
	case errors.Is(err, errFdsecUsage):
		// The shared credential parser's usage errors (credential_args.go).
		return vdisk.ExitUsage
	}
	return vdisk.ExitClass(err)
}

// vdKeyfileError is a wrong credential that came from a keyfile: the same
// sentence, plus the keyfile's path, because a wrong keyfile is a wrong
// credential and pretending otherwise would be a hint (P5 section 5).
type vdKeyfileError struct {
	err  error
	path string
}

func (e *vdKeyfileError) Error() string { return vdisk.Explain(e.err) + "\nKeyfile: " + e.path }
func (e *vdKeyfileError) Unwrap() error { return e.err }

// vdExplain is the text a failed vd verb prints: the named sentence of a door
// failure (vdisk.Explain), and the error's own text for anything else.
func vdExplain(err error) string {
	var kf *vdKeyfileError
	var many *vdManyError
	switch {
	case errors.As(err, &many):
		return many.Error()
	case errors.As(err, &kf):
		return kf.Error()
	}
	return vdisk.Explain(err)
}

// vdCredentialErr attaches the keyfile path to a wrong credential from k:.
func vdCredentialErr(err error, a credArg) error {
	if a.keyfile() && errors.Is(err, vdisk.ErrCredential) {
		return &vdKeyfileError{err: err, path: a.val}
	}
	return err
}

// reportVdError ends a vd verb that failed, from either entry point.
func reportVdError(err error, hl *HistoryLogger) {
	hl.SetError(err)
	class := vdExitClass(err)
	if class == vdisk.ExitStopped {
		fmt.Printf("Stopped: %v\n", err)
		return
	}
	fdsecExitCode = class
	runContainerClass(class)
	runFailure(err)
	fmt.Fprintf(os.Stderr, "Error: %s\n", vdExplain(err))
}

func isFddPath(p string) bool { return strings.EqualFold(filepath.Ext(p), ".fdd") }

// vdDispatchTarget claims the target-first container verbs before the generic
// chain reads a sub-operation (spec 5.3 row 3): `x.fdd info` answers from the
// container, and `X: unmount` unmounts a container mounted there.
func vdDispatchTarget(argv []string, hl *HistoryLogger, batch bool) (bool, error) {
	if !vdClaims(argv) {
		return false, nil
	}
	op := strings.ToLower(argv[1])
	return true, runVd(append([]string{op, argv[0]}, argv[2:]...), hl, batch)
}

// vdClaims reports whether a target-first line belongs to the vd family:
// every container verb on a .fdd path (a mask included - `*.fdd info`), mount
// and unmount on a foreign image, and unmount or save on a drive letter.
func vdClaims(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	target := argv[0]
	v, ok := vdVerbOf(argv[1])
	if !ok {
		return false
	}
	switch {
	case isFddPath(target) && v.container:
		return true
	case vdImageKind(target) != "" && (v.name == "mount" || v.name == "unmount"):
		return true
	case (v.name == "unmount" || v.name == "save") && driveRootSpelling.MatchString(target):
		return true
	}
	return false
}

// handleVdCommand is the namespace entry: filedo vd <verb> ..
func handleVdCommand(args []string, hl *HistoryLogger, batch bool) error {
	if len(args) == 0 {
		beginRun(runActs, "vd", "", []string{"vd"})
		return vdUsagef("vd needs a verb: %s", vdVerbList())
	}
	return runVd(args, hl, batch)
}

// vdPackaged reports whether this process runs inside an MSIX package, where
// the mount path does not exist (T6.26): decided at run time, one binary. A
// variable, so the refusal is testable without a package.
var vdPackaged = fdsecHasPackageIdentity

// errVdPackaged is the packaged build's refusal of a verb that needs the mount
// path: class 6, one sentence.
var errVdPackaged = fmt.Errorf("%w: the Microsoft Store build of FileDO cannot mount - a packaged app can neither configure the Windows iSCSI initiator nor ask for administrator rights - so mount, unmount, save, format and auto need the setup or the portable build from GitHub, while new, info, verify, export, compact, grow, seal, clone, pass, destroy, list, status, add and forget work here", vdisk.ErrUnsupported)

func runVd(args []string, hl *HistoryLogger, batch bool) error {
	word := strings.ToLower(args[0])
	rest := args[1:]
	target := ""
	if len(rest) > 0 {
		target = rest[0]
	}
	v, known := vdVerbOf(word)
	// An unknown word is not recorded as the command: it may be a password.
	name := "unknown"
	switch {
	case known:
		name = v.name
	case strings.HasPrefix(word, "_"):
		name = "internal"
	}
	if !known || vdSubjectless[v.name] {
		target = "" // nor is the word after it; these verbs take no target
	}
	kind := runActs
	if v.judges {
		kind = runJudges
	}
	beginRun(kind, "vd "+name, target, append([]string{"vd"}, args...))
	hl.SetCommand("vd", target, "vd-"+name)
	if strings.HasPrefix(word, "_") {
		return vdUsagef("%s is an internal step of mount and unmount, not a command", word)
	}
	if !known {
		// Never quoted back: a mistyped verb may be followed by, or be, a
		// password, and this message reaches history.json.
		return vdUsagef("unknown vd verb: want %s", vdVerbList())
	}
	// `vd status json` is the one exception, and json is a bare word (the
	// flag-spelling rule of SP-0004: no third spelling): the machine-readable
	// snapshot of SP-0063 8.1.
	statusJSON := v.name == "status" && len(rest) == 1 && strings.EqualFold(rest[0], "json")
	if len(rest) > 0 && (v.name == "list" || v.name == "status" || v.name == "stop") && !statusJSON {
		// Never quoted back: the extra word may be a password.
		if v.name == "status" {
			return vdUsagef("vd status takes no other word but json")
		}
		return vdUsagef("vd %s takes no other word", v.name)
	}
	if len(rest) > 0 {
		if err := vdRefuseCredentialSlot("container slot", rest[0]); err != nil {
			return err
		}
	}
	// A registered name stands for its path in the namespace form; a path
	// that exists always wins (P4 section 6).
	if v.container && len(rest) > 0 {
		rest = append([]string{vdResolve(rest[0])}, rest[1:]...)
	}
	if v.transport && vdPackaged() {
		return errVdPackaged
	}
	switch v.name {
	case "new":
		return vdNew(rest)
	case "mount":
		return vdMount(rest, batch)
	case "unmount":
		return vdUnmount(rest, batch)
	case "info":
		return vdInfo(rest)
	case "verify":
		return vdVerify(rest)
	case "export":
		return vdExport(rest)
	case "save":
		return vdSave(rest, batch)
	case "compact":
		return vdCompact(rest)
	case "grow":
		return vdGrow(rest)
	case "format":
		return vdFormat(rest, batch)
	case "seal":
		return vdSeal(rest)
	case "clone":
		return vdClone(rest)
	case "pass":
		return vdPass(rest)
	case "destroy":
		return vdDestroy(rest, batch)
	case "add":
		return vdAdd(rest)
	case "forget":
		return vdForget(rest)
	case "list":
		return vdList()
	case "auto":
		return vdAuto(rest, batch)
	case "guard":
		return vdGuard(rest, batch)
	case "status":
		if statusJSON {
			return vdStatusJSON()
		}
		return vdStatus()
	case "register", "unregister":
		return vdShellRegistration(v.name, rest)
	}
	return vdStop()
}

// ---------------------------------------------------------------- new, seal, info

func vdWriterStamp() {
	if n, err := strconv.ParseUint(version, 10, 64); err == nil {
		vdisk.WriterStamp = n
	}
}

func vdNew(args []string) error {
	if len(args) < 2 {
		return vdUsagef("vd new needs a path and a size: filedo vd new <path> <size> [plain|fast|ram] [label <text>]")
	}
	path, sizeWord := args[0], args[1]
	o := vdisk.CreateOptions{Path: path, Profile: vdisk.ProfilePlain}
	var cred credArg
	for i := 2; i < len(args); i++ {
		if a, ok := credentialToken(args[i]); ok {
			if cred.given() {
				return vdUsagef("one credential per container: give p:, pf:, pe: or k: once")
			}
			cred = a
			continue
		}
		switch w := strings.ToLower(args[i]); w {
		case "plain":
			o.Profile = vdisk.ProfilePlain
		case "fast":
			o.Profile = vdisk.ProfileFast
		case "ram":
			o.Profile = vdisk.ProfileRAM
		case "vault":
			o.Profile = vdisk.ProfileVault
		case "sealed":
			return vdUsagef("a sealed container is made from an existing one: filedo <file.fdd> seal <new.fdd>")
		case "label":
			if i+1 >= len(args) {
				return vdUsagef("label needs a text")
			}
			o.FriendlyName = args[i+1]
			i++
		default:
			// Never quoted back: an unknown word here may be a password
			// typed without p:, and this message reaches history.json.
			return vdUsagef("unknown word %d for vd new: want plain, fast, ram, vault, label <text>, or a credential as p:, pf:, pe: or k:", i+1)
		}
	}
	mb, err := parseSize(sizeWord)
	if err != nil {
		// Never quoted back: a password typed in the size slot must not
		// reach history.json through this message.
		return vdUsagef("the second word of vd new is the size, like 20G, 512M or 1.5T")
	}
	o.LogicalSize = int64(mb) << 20
	if !isFddPath(path) {
		return vdUsagef("a container file ends in .fdd: %s", path)
	}
	if _, err := os.Stat(path); err == nil {
		return vdUsagef("%s exists; vd new never overwrites a file", path)
	}
	// The credential. A vault asks for one - twice, since a typo would lock
	// the volume behind a credential nobody meant - and says what it costs
	// before the container exists (P5 section 6). Any other profile takes one
	// only when it is given, and is then encrypted too.
	if o.Profile == vdisk.ProfileVault {
		fmt.Println(vdVaultWarning)
	}
	if o.Profile == vdisk.ProfileVault || cred.given() {
		c, err := vdResolveCredential(cred, true)
		if err != nil {
			return err
		}
		defer clear(c)
		o.Credential, o.Keyfile = c, cred.keyfile()
	}
	if o.Profile == vdisk.ProfileVault && len(o.Credential) == 0 {
		return vdisk.ErrVaultNeedsCredential
	}
	vdWriterStamp()
	ctx := context.Background()
	if globalInterruptHandler != nil {
		ctx = globalInterruptHandler.Context()
	}
	c, err := vdisk.Create(ctx, o)
	if err != nil {
		return err
	}
	info := c.Info()
	if err := c.Close(); err != nil {
		return err
	}
	fmt.Printf("Created %s: %s, %s volume; %s.\n", path, info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info))
	for _, l := range vdLimits(info, cred.given() && len(o.Credential) == 0) {
		fmt.Println(l)
	}
	fmt.Printf("Mount it with: filedo %s mount  (the first mount formats the volume NTFS)\n", path)
	return nil
}

// vdVaultWarning is the one sentence a vault's creation says before the
// container exists (P5 section 6).
const vdVaultWarning = "A vault is encrypted: its data key lives only under your credential, so losing every credential loses the data. There is no recovery."

// vdLimits are the printed limits of P5 section 6 (T5.11): what the platform
// cannot guarantee that a person might assume. emptyGiven is an empty
// credential given on purpose, which selects obfuscation and is not refused.
func vdLimits(i vdisk.Info, emptyGiven bool) []string {
	var out []string
	if emptyGiven {
		out = append(out, "The credential given is empty: an empty credential selects obfuscation only. It keeps the volume from a casual look and from tools that scan for disk images; it keeps it from nobody who has the file and FileDO.")
	}
	if !i.Obfuscated {
		out = append(out, "Encrypted: the file is unreadable without the credential. While the container is mounted it is as open as any drive to every program on this machine; encryption protects the file, not a mounted volume.")
		if i.Profile == vdisk.ProfileRAM {
			out = append(out, "ram with a credential: the volume lives in memory while it is mounted, and Windows may page that memory out, so plaintext can reach the page file. This is allowed, and it is not prevented.")
		}
	}
	return out
}

// vdSeal writes a sealed copy: filedo <file.fdd> seal <new.fdd> [nopass]. The
// source is never changed, so this verb destroys nothing.
func vdSeal(args []string) error { return vdCopy(args, true) }

// vdClone writes a writable copy: filedo <file.fdd> clone <new.fdd> [nopass].
// The source is never changed.
func vdClone(args []string) error { return vdCopy(args, false) }

// vdNopassNote is what nopass on an encrypted source says: the copy is
// obfuscated - never "decrypted", never "unprotected" (FDD-BEHAVIOUR 7 rule 6).
const vdNopassNote = "nopass: the copy is obfuscated, not encrypted - anyone who has the file and FileDO reads it. The source keeps its credential."

// vdCopy is seal and clone: vdisk.SealWith or vdisk.CopyWith, the same
// grammar, the same checks, and the same promise that the source is unchanged.
func vdCopy(args []string, seal bool) error {
	verb, act, form := "clone", "Copying", "clone <new.fdd> [nopass]"
	if seal {
		verb, act, form = "seal", "Sealing", "seal <new.fdd> [nopass]"
	}
	if len(args) < 2 {
		return vdUsagef("%s needs a container and a new file: filedo <file.fdd> %s", verb, form)
	}
	src, dst := args[0], args[1]
	var cred credArg
	nopass := false
	for _, w := range args[2:] {
		if strings.EqualFold(w, "nopass") {
			nopass = true
			continue
		}
		a, ok := credentialToken(w)
		if !ok || cred.given() {
			// Never quoted back: the word may be a password.
			return vdUsagef("%s takes a container, a new file, nopass and at most one credential (p:, pf:, pe: or k:)", verb)
		}
		cred = a
	}
	if err := vdRefuseCredentialSlot("new file slot", dst); err != nil {
		return err
	}
	if !isFddPath(dst) {
		// Never quoted back: a password typed in the new file slot must not
		// reach history.json through this message.
		return vdUsagef("the new file of %s is a container file, whose name ends in .fdd", verb)
	}
	if _, err := os.Stat(dst); err == nil {
		return vdUsagef("%s exists; a container is never written over an existing file", dst)
	}
	si, err := vdisk.Inspect(src)
	if err != nil {
		return err
	}
	o := vdisk.SealOptions{Src: src, Dst: dst, Obfuscate: nopass}
	if si.Obfuscated {
		if cred.given() {
			fmt.Println(vdCredentialNotUsed)
			if cred.src == "pe" {
				fdsecLookupCredentialEnv(cred.val)
			}
		}
		if nopass {
			fmt.Println("nopass: the source is obfuscated already, and so is the copy.")
		}
	} else {
		c, err := vdResolveCredential(cred, false)
		if err != nil {
			return err
		}
		defer clear(c)
		o.Credential, o.Keyfile = c, cred.keyfile()
	}
	vdWriterStamp()
	line := vdPercent(act)
	o.Progress = line
	if seal {
		err = vdisk.SealWith(vdContext(), o)
	} else {
		err = vdisk.CopyWith(vdContext(), o)
	}
	line.end()
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	info, err := vdisk.Inspect(dst)
	if err != nil {
		return err
	}
	if seal {
		fmt.Printf("Sealed %s into %s: %s volume, read-only for good; %s. %s is unchanged.\n", src, dst, vdSize(info.LogicalSize), vdProtectionNote(info), src)
	} else {
		fmt.Printf("Cloned %s into %s: %s, %s volume; %s. %s is unchanged.\n", src, dst, info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info), src)
		fmt.Printf("New container id: %s (a container of its own: both can be mounted side by side).\n", info.ContainerID)
	}
	if nopass && !si.Obfuscated {
		fmt.Println(vdNopassNote)
	}
	vdLogf("%s %s into %s (nopass=%v)", verb, src, dst, nopass)
	return nil
}

// vdPercentLine is the one progress line of the long vd verbs: "<act>: n%" on
// one console line, and a progress event per percent for a supervisor.
type vdPercentLine struct {
	act   string
	last  int
	shown bool
}

func vdPercent(act string) *vdPercentLine { return &vdPercentLine{act: act, last: -1} }

func (l *vdPercentLine) Progress(done, total int64) {
	if pct := int(done * 100 / max(total, 1)); pct != l.last {
		l.last, l.shown = pct, true
		fmt.Printf("\r%s: %d%%", l.act, pct)
		EmitProgressEvent(done, total, 0, 0, 0, l.act)
	}
}

// end closes the line, when one was begun.
func (l *vdPercentLine) end() {
	if l.shown {
		fmt.Println()
		l.shown = false
	}
}

// vdContext is the stop model's context: Ctrl+C and the stop file end a long
// vd operation at its next step.
func vdContext() context.Context {
	if globalInterruptHandler != nil {
		return globalInterruptHandler.Context()
	}
	return context.Background()
}

// vdCredentialNotUsed answers a credential given for an obfuscated container
// (FDD-BEHAVIOUR 7 rule 4): not an error, and never called encryption.
const vdCredentialNotUsed = "The credential is not used: this container is obfuscated, not encrypted, and opens without one."

func vdProtectionNote(i vdisk.Info) string {
	if i.Obfuscated {
		return "obfuscated, not encrypted: anyone holding the file and the published format can read it"
	}
	return "encrypted"
}

func vdSize(n int64) string {
	switch {
	case n >= 1<<40 && n%(1<<40) == 0:
		return fmt.Sprintf("%d TiB", n>>40)
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GiB", n>>30)
	default:
		return fmt.Sprintf("%d MiB", n>>20)
	}
}

func vdTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// vdInfo reports what the header says, for one container or several (T6.12):
// filedo <file.fdd ..|mask> info. It needs no credential.
func vdInfo(args []string) error {
	if len(args) < 1 {
		return vdUsagef("info needs a container path")
	}
	for _, w := range args[1:] {
		if isCredentialToken(w) {
			return vdUsagef("info takes no credential: the header it reads opens without one")
		}
	}
	targets, err := vdTargets(args)
	if err != nil {
		return err
	}
	return vdEach(targets, "info", vdInfoOne)
}

func vdInfoOne(path string) error {
	i, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	args := []string{path}
	clean := "yes"
	if !i.Clean {
		clean = "NO - it was not closed cleanly, or it is mounted now"
	}
	fmt.Printf("Container:     %s\n", args[0])
	fmt.Printf("Format:        FDD %d.%d\n", i.VersionMajor, i.VersionMinor)
	fmt.Printf("Profile:       %s\n", i.Profile)
	fmt.Printf("Protection:    %s (%s)\n", i.Protection(), vdProtectionNote(i))
	fmt.Printf("Volume size:   %s\n", vdSize(i.LogicalSize))
	fmt.Printf("File size:     %d bytes, %d of %d clusters allocated\n", i.FileSize, i.AllocatedClusters, i.ClusterCount)
	fmt.Printf("Closed clean:  %s\n", clean)
	fmt.Printf("Last good save: %s\n", vdTime(i.LastGoodSave))
	fmt.Printf("Created:       %s, mounted %d times\n", vdTime(i.Created), i.MountCount)
	fmt.Printf("Container id:  %s\n", i.ContainerID)
	if m, ok := vdFindMount(i.ContainerID); ok {
		fmt.Printf("Mounted now:   %s\n", m.Letter)
	}
	return nil
}

// ---------------------------------------------------------------- state and log

// vdMount is one mounted container in vdisk-state.json.
type vdMountRow struct {
	ContainerID   string    `json:"container_id"`
	Path          string    `json:"path"`
	Letter        string    `json:"letter"`
	ReadOnly      bool      `json:"read_only"`
	MountedAt     time.Time `json:"mounted_at"`
	ServerPID     int       `json:"server_pid"`
	ServerStarted int64     `json:"server_started"`
	Port          int       `json:"port"`
	IQN           string    `json:"iqn"`
	Serial        string    `json:"serial"`
	Session       string    `json:"session"`
	Profile       string    `json:"profile,omitempty"`
	// ScanExcluded: the mount added a Defender exclusion that the unmount removes.
	ScanExcluded bool `json:"scan_excluded,omitempty"`
}

type vdState struct {
	Version int          `json:"version"`
	Mounts  []vdMountRow `json:"mounts"`
	Images  []vdImageRow `json:"images,omitempty"`
}

// vdImageRow is a foreign image FileDO mounted: Windows can say which letter
// an image has, but not which image a letter belongs to, so unmount by letter
// reads it from here - after checking with Windows that it is still attached.
type vdImageRow struct {
	Letter    string    `json:"letter"`
	Path      string    `json:"path"`
	MountedAt time.Time `json:"mounted_at"`
}

const vdStateFile = "vdisk-state.json"

func vdStatePath() (string, error) { return statedir.Path(vdStateFile) }

func vdLoadState() (vdState, error) {
	var s vdState
	p, err := vdStatePath()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return vdState{Version: 1}, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("the mount state %s is unreadable: %w", p, err)
	}
	return s, nil
}

// vdUpdateState is a locked read-modify-write of the state file. A lock that
// cannot be taken is busy, class 8.
func vdUpdateState(change func(*vdState) error) error {
	p, err := vdStatePath()
	if err != nil {
		return err
	}
	unlock, err := statedir.Lock(p, 10*time.Second)
	if err != nil {
		return errBusy("another FileDO command holds the mount state: " + err.Error())
	}
	defer unlock()
	s, err := vdLoadState()
	if err != nil {
		return err
	}
	if err := change(&s); err != nil {
		return err
	}
	s.Version = 1
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return statedir.WriteFileAtomic(p, b, 0o600)
}

func vdFindMount(containerID string) (vdMountRow, bool) {
	s, err := vdLoadState()
	if err != nil {
		return vdMountRow{}, false
	}
	for _, m := range s.Mounts {
		if strings.EqualFold(m.ContainerID, containerID) {
			return m, true
		}
	}
	return vdMountRow{}, false
}

// vdLogf appends one line to vdisk.log, the mount log (plan S3 T3.5): every
// bind, connect, attach, detach and server exit.
func vdLogf(format string, args ...interface{}) {
	d, err := statedir.Dir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(d, "vdisk.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line := fmt.Sprintf("%s pid=%d %s\n", time.Now().Format("2006-01-02T15:04:05.000-07:00"), os.Getpid(), fmt.Sprintf(format, args...))
	f.WriteString(line)
}

// vdIQN is the target name of a container: one target per container, named
// from its id (plan S3 open question 2).
func vdIQN(containerID string) string {
	return "iqn.2026-09.ua.od.sza:filedo.vd." + strings.ToLower(strings.ReplaceAll(containerID, "-", ""))
}

// vdFiles are the per-mount handoff files in the state root.
func vdFiles(containerID string) (handoff, stop string, err error) {
	if handoff, err = vdSidePath(containerID, ".handoff.json"); err != nil {
		return "", "", err
	}
	stop, err = vdSidePath(containerID, ".stop")
	return handoff, stop, err
}

// vdSidePath is one of a mount's files in the state root: the handoff, the
// stop file, and for ram the save request (.save), its answer (.saved.json)
// and the server's status (.status.json).
func vdSidePath(containerID, ext string) (string, error) {
	d, err := statedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "vd-"+strings.ToLower(strings.ReplaceAll(containerID, "-", ""))+ext), nil
}

// vdRAMStatus is what a ram container's block server reports every few
// seconds, and vdSaved its answer to a save request.
type vdRAMStatus struct {
	At           time.Time `json:"at"`
	DirtyBytes   int64     `json:"dirty_bytes"`
	Saving       bool      `json:"saving"`
	LastGoodSave time.Time `json:"last_good_save"`
}

type vdSaved struct {
	Request string    `json:"request"`
	Error   string    `json:"error,omitempty"`
	Class   int       `json:"class,omitempty"`
	At      time.Time `json:"at"`
}

func vdReadRAMStatus(containerID string) (vdRAMStatus, bool) {
	var s vdRAMStatus
	p, err := vdSidePath(containerID, ".status.json")
	if err != nil {
		return s, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &s) != nil {
		return s, false
	}
	return s, true
}

// vdConfirm asks a yes-or-no question on the console. A batch never answers
// yes: nobody is there to read the question.
func vdConfirm(question string, batch bool) bool {
	if batch {
		return false
	}
	fmt.Printf("%s [y/N]: ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// ---------------------------------------------------------------- status, stop

func vdStatus() error {
	s, err := vdLoadState()
	if err != nil {
		return err
	}
	for _, im := range s.Images {
		fmt.Printf("%s  %s  (image, mounted %s)\n", im.Letter, im.Path, vdTime(im.MountedAt))
	}
	if len(s.Mounts) == 0 {
		if len(s.Images) == 0 {
			fmt.Println("Nothing is mounted. No block server is running.")
		} else {
			fmt.Println("No container is mounted. No block server is running.")
		}
		return nil
	}
	sort.Slice(s.Mounts, func(a, b int) bool { return s.Mounts[a].Letter < s.Mounts[b].Letter })
	for _, m := range s.Mounts {
		state := fmt.Sprintf("served on 127.0.0.1:%d by process %d", m.Port, m.ServerPID)
		if !vdServerAlive(m) {
			state = "BLOCK SERVER GONE - the volume is offline; run: filedo " + m.Letter + " unmount"
		}
		ro := ""
		if m.ReadOnly {
			ro = ", read-only"
		}
		fmt.Printf("%s  %s  (mounted %s%s), %s\n", m.Letter, m.Path, vdTime(m.MountedAt), ro, state)
		if r, ok := vdReadRAMStatus(m.ContainerID); ok && vdServerAlive(m) {
			saving := ""
			if r.Saving {
				saving = ", a save is running"
			}
			fmt.Printf("    ram: %s not saved yet%s; last complete save %s\n", vdSize(r.DirtyBytes), saving, vdTime(r.LastGoodSave))
		}
	}
	return nil
}

func vdStop() error {
	s, err := vdLoadState()
	if err != nil {
		return err
	}
	var live []string
	for _, m := range s.Mounts {
		live = append(live, m.Letter)
	}
	if len(live) > 0 {
		return errBusy("containers are mounted at " + strings.Join(live, ", ") + "; unmount them first (each block server exits with its own unmount)")
	}
	fmt.Println("No block server is running.")
	return nil
}

// vdMaxCredential bounds a credential: mount hands it to the block server
// through a pipe written in full before the server starts, so it has to fit
// the pipe's buffer, and a container made with a longer one could never be
// mounted.
const vdMaxCredential = 64 << 10

// vdResolveCredential is the shared resolveCredential with vd's own limits:
// the length bound, an empty pf: file refused as pe: already is (an empty
// file is a mistake, never a choice), and differing prompt answers as usage.
func vdResolveCredential(a credArg, confirm bool) (fdsec.Credential, error) {
	return vdResolveCredentialAs(a, confirm, "Credential (no echo): ")
}

// vdResolveCredentialAs is vdResolveCredential with its own first question:
// pass asks for the current credential and the new one by name.
func vdResolveCredentialAs(a credArg, confirm bool, prompt string) (fdsec.Credential, error) {
	c, err := resolveCredential(a, confirm, prompt)
	switch {
	case errors.Is(err, errCredentialMismatch):
		return nil, vdUsagef("%v", err)
	case err != nil:
		return nil, err
	case len(c) > vdMaxCredential:
		clear(c)
		return nil, vdUsagef("the credential is longer than %d bytes", vdMaxCredential)
	case a.src == "pf" && len(c) == 0:
		return nil, vdUsagef("the password file %s is empty, so no password was given; nothing was done", a.val)
	}
	return c, nil
}

// vdRefuseCredentialSlot refuses a credential-shaped token where a path goes:
// every path reaches an error message, and that message reaches history.json.
// The token itself is never quoted.
func vdRefuseCredentialSlot(slot string, words ...string) error {
	for _, w := range words {
		if fdsecCredentialToken(w) {
			return vdUsagef("the %s holds a password (p:..); name the container first and give the password after it", slot)
		}
	}
	return nil
}
