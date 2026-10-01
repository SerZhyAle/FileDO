package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Credential redaction for history, the batch-line console echo and any other
// surface that repeats the command line (spec section 12, safety invariant 8).
// This runs BEFORE the first verb that can receive a credential ships: a
// credential written to history.json is permanent on the user's disk, with no
// recall.

// fdsecFamilyToken lists the tokens (operations and the fdsec command, in any
// argument position) that mark a command line as belonging to the fdsec
// family. Everything credential-shaped after such a token gets redacted.
var fdsecFamilyToken = map[string]bool{
	"secure": true, "sec": true,
	"unsecure": true, "uns": true, "unsec": true,
	"reveal": true, "rev": true,
	"fdsec": true, "fds": true,
}

// fdsecSubVerb lists the sub-verbs of the verb-first form. Each takes a
// container path as its next token, and that path is not the secret.
func fdsecSubVerb(s string) bool {
	switch s {
	case "info", "verify", "register", "unregister":
		return true
	}
	return false
}

// fdsecRedactKeeps reports whether an option word survives redaction. The
// vocabulary is the parser's own (fdsecOptionWords), so a word the parser
// would take as the password is never one kept here (FDSEC-18); -all-users
// belongs to register and unregister.
func fdsecRedactKeeps(s string) bool {
	return fdsecOptionWords[s] || s == "-all-users"
}

// fdsecCredentialToken reports whether t is the parser's inline password on
// its own: the `p:` prefix exactly as parseFdsecArgs reads it (case-sensitive,
// so `P:x` is never one) followed by a value that does not begin with `\` or
// `/`. `p:\dir\a.fd-sec` is a path on drive P: and stays one; `p:hunter2` is
// a password wherever it stands, because the prefix is credential syntax by
// itself (FDSEC-BEHAVIOUR 1.4 section 8.2; AUD-09-F3, AUD-29-F2).
func fdsecCredentialToken(t string) bool {
	if !strings.HasPrefix(t, "p:") {
		return false
	}
	v := t[2:]
	return v == "" || (v[0] != '\\' && v[0] != '/')
}

// redactCredentialTarget screens one recorded value - the target field of
// history.json and of the `run` and `step` events - by the same rule: a
// credential-shaped `p:` token is `p:***` there too.
func redactCredentialTarget(t string) string {
	if fdsecCredentialToken(t) {
		return "p:***"
	}
	return t
}

// redactCredentialArgs returns a copy of args with credential material
// removed. A credential-shaped `p:` token (fdsecCredentialToken) is redacted
// on every command line, whatever the verb and whatever its position: a
// transposed verb (`a.txt secrue p:pw`) or a swapped order
// (`fdsec verify p:pw a.fd-sec`) must not turn a password into a logged word.
// The rest of the screening applies to fdsec-family command lines only; every
// other command line otherwise passes through unchanged. The shape stays
// legible: p:<password> becomes p:***, a
// bare password becomes ***. pf:, pe: and k: keep their path or variable
// name - the name is not the secret, and the value never enters a command
// line. Anything unrecognized after an fdsec token is redacted too: redacting
// garbage costs a history line, missing a password costs the user's disk.
//
// Two tokens are deliberately kept because they are paths, not secrets, and a
// history entry without them says nothing: the destination after `to`, and
// the container path after a sub-verb of the verb-first form
// (`filedo fdsec verify <container> <password>`). A sub-verb is one only
// right after `fdsec`: in the target-first form (`a.txt secure verify`) the
// parser takes `verify` as the password, so it is redacted like one. In the
// target-first form the target sits ahead of the verb and is left alone
// unless it is itself a credential-shaped `p:` token. The container slot of
// the verb-first form is the same: kept as a path, but a credential-shaped
// `p:` there is redacted (and the dispatch refuses the line, fdsec_cmd.go).
func redactCredentialArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, t := range out {
		if fdsecCredentialToken(t) {
			out[i] = "p:***"
		}
	}
	// The scan starts at 0, not at 1: a batch line is echoed as its own
	// fields, so the family token can BE the first one (`fdsec verify c p:..`)
	// where an os.Args slice would carry the executable's path there instead.
	verbAt := -1
	for i := 0; i < len(out); i++ {
		if fdsecFamilyToken[strings.ToLower(out[i])] {
			verbAt = i
			break
		}
	}
	// The disk-container family: whichever family's verb stands first owns
	// the line.
	if vdVerb, vdTarget := vdLocate(out); vdVerb >= 0 && (verbAt < 0 || vdVerb <= verbAt) {
		redactVdArgs(out, vdVerb, vdTarget)
		return out
	}
	if verbAt < 0 {
		return out
	}
	start := verbAt + 1
	fam := strings.ToLower(out[verbAt])
	if (fam == "fdsec" || fam == "fds") && start < len(out) && fdsecSubVerb(strings.ToLower(out[start])) {
		sub := strings.ToLower(out[start])
		start++
		// info and verify name the container next; it is a path.
		if (sub == "info" || sub == "verify") && start < len(out) {
			if !fdsecCredentialToken(out[start]) && fdsecContainerSlotUnrecognised(out[start], start+1 < len(out)) {
				out[start] = "***"
			}
			start++
		}
	}
	keepNext := false // set after "to": a path follows
	for i := start; i < len(out); i++ {
		t := out[i]
		lt := strings.ToLower(t)
		if keepNext {
			keepNext = false
			continue
		}
		switch {
		case lt == "to":
			keepNext = true
		case strings.HasPrefix(t, "p:"):
			out[i] = "p:***"
		case strings.HasPrefix(t, "pf:"), strings.HasPrefix(t, "pe:"), strings.HasPrefix(t, "k:"):
			// path or variable name: kept, it is not the secret
		case fdsecRedactKeeps(lt):
			// option word
		default:
			out[i] = "***"
		}
	}
	return out
}

// ---------------------------------------------------------------- the vd family

// The disk-container family joins the same redaction (SP-0004 P5 T5.3,
// FDD-BEHAVIOUR 7 rule 2) before any vd verb takes a credential. Its lines
// are recognised in both forms - `vd <verb> <target> ..` and
// `<file.fdd> <verb> ..` - and screened by the same principle as fdsec's:
// what is known to be safe is kept, and anything unrecognised after the verb
// is redacted.

// vdRedactVerbs are the verbs whose line is screened. Every vd verb is here,
// credential-taking or not, so a verb that learns to take one later is
// already covered.
var vdRedactVerbs = map[string]bool{
	"new": true, "create": true, "mount": true, "mnt": true, "attach": true,
	"unmount": true, "umount": true, "detach": true, "save": true, "seal": true,
	"info": true, "i": true, "pass": true, "export": true, "extract": true, "ext": true,
	"verify": true, "vfy": true, "compact": true, "shrink": true, "grow": true, "resize": true,
	"format": true, "destroy": true, "erase": true, "clone": true, "add": true, "forget": true,
	"list": true, "ls": true, "auto": true, "guard": true, "status": true, "stop": true,
	"register": true, "unregister": true,
}

// vdRedactKeeps are the option words of the vd verbs. None of them is ever
// taken as a credential by a vd parser, except by mount's bare trailing token,
// which redactVdArgs screens by mount's own rule first. `new` is pass's word
// before the new credential, which is itself prefixed.
var vdRedactKeeps = map[string]bool{
	"plain": true, "fast": true, "ram": true, "vault": true, "sealed": true,
	"ro": true, "readonly": true, "noscan": true, "force": true, "-y": true, "y": true,
	"nosave": true, "off": true, "on": true, "logon": true, "short": true, "-all-users": true,
	"nopass": true, "new": true, "raw": true, "vhd": true, "wipe": true,
	"fs": true, "ntfs": true, "exfat": true, "run": true,
}

// vdRedactValueWords take a value that is not a secret: a drive letter, a
// label, a registry name, a destination.
var vdRedactValueWords = map[string]bool{"as": true, "label": true, "to": true}

// vdSubjectless are the vd verbs that take no container or name: their
// line has no subject slot to keep.
var vdSubjectless = map[string]bool{
	"list": true, "ls": true, "status": true, "stop": true,
	"register": true, "unregister": true,
}

func vdContainerLike(t string) bool {
	switch strings.ToLower(filepath.Ext(t)) {
	case ".fdd", ".vhd", ".vhdx", ".iso":
		return true
	}
	return false
}

// vdDriveSpelling is X:, X:\ or X:/ - the spelling of a drive, never a
// credential to any vd parser.
func vdDriveSpelling(t string) bool {
	if len(t) < 2 || len(t) > 3 || t[1] != ':' {
		return false
	}
	c := t[0] | 0x20
	return c >= 'a' && c <= 'z' && (len(t) == 2 || t[2] == '\\' || t[2] == '/')
}

// vdMountWordSet mirrors vdMountOptionWord for the bare-token rule.
func vdMountWordSet(t string) bool {
	switch strings.ToLower(t) {
	case "ro", "readonly", "noscan", "as":
		return true
	}
	return vdDriveSpelling(t)
}

// vdLocate finds a vd line's verb: the word after vd or vdisk, or a vd verb
// right after a container path. target is the index kept as the line's
// subject: the path or name after a namespace verb, or the container ahead of
// a target-first verb. verbAt is -1 on a line of another family.
func vdLocate(out []string) (verbAt, target int) {
	for i := 0; i+1 < len(out); i++ {
		lt := strings.ToLower(out[i])
		// Any word after vd is its verb, known or not: a mistyped verb
		// must not turn the password after it into a logged word.
		if lt == "vd" || lt == "vdisk" {
			return i + 1, i + 2
		}
		if vdContainerLike(out[i]) && vdRedactVerbs[strings.ToLower(out[i+1])] {
			return i + 1, i
		}
	}
	return -1, -1
}

// redactVdArgs screens a vd line in place, from its verb on.
func redactVdArgs(out []string, verbAt, target int) {
	verb := strings.ToLower(out[verbAt])
	start := verbAt + 1
	if !vdRedactVerbs[verb] {
		// An unknown verb keeps nothing after it but option words.
		target = -1
	}
	if vdSubjectless[verb] {
		// A verb that takes no subject keeps no word as one: the word after
		// it is screened like any other (vd list <password> is refused, and
		// its line still reaches history.json).
		target = -1
	}
	if target > verbAt && target < len(out) {
		if fdsecCredentialToken(out[target]) {
			target = verbAt // the subject slot holds a credential: screened below
		} else {
			start = target + 1
		}
	}
	// new: the path and the size are positional.
	if (verb == "new" || verb == "create") && target > verbAt {
		start = target + 2
		// The size slot is kept only when it reads as a size, which is all
		// vd new ever takes it for.
		if s := target + 1; s < len(out) && !vdSizeShaped(out[s]) {
			out[s] = "***"
		}
	}
	// mount's bare trailing token (vdParseMountOpts): the one word after the
	// container, when it is neither an option nor a prefixed credential, is
	// the password.
	switch verb {
	case "mount", "mnt", "attach":
		if start == len(out)-1 {
			t := out[start]
			if !isCredentialToken(t) && !vdMountWordSet(t) {
				out[start] = "***"
				return
			}
		}
	}
	// One positional word is kept by what the verb's parser takes it for:
	// export's destination (the first word that is no option, unless `to`
	// named it) and grow's size, when it reads as one.
	destFree := verb == "export" || verb == "extract" || verb == "ext"
	sizeFree := verb == "grow" || verb == "resize"
	keepNext := false
	for i := start; i < len(out); i++ {
		t := out[i]
		lt := strings.ToLower(t)
		if keepNext {
			keepNext = false
			continue
		}
		switch {
		case destFree && !isCredentialToken(t) && !vdRedactKeeps[lt] && !vdRedactValueWords[lt]:
			// export's destination: a path, which the parser never takes as a credential
			destFree = false
		case strings.HasPrefix(t, "p:"):
			out[i] = "p:***"
		case strings.HasPrefix(t, "pf:"), strings.HasPrefix(t, "pe:"), strings.HasPrefix(t, "k:"):
			// a path or a variable name: kept, it is not the secret
		case vdRedactValueWords[lt]:
			keepNext = true
			if lt == "to" {
				destFree = false
			}
		case vdRedactKeeps[lt], vdContainerLike(t), vdDriveSpelling(t):
			// an option word, a container path, a drive
		case sizeFree && vdSizeShaped(t):
			sizeFree = false
		case verb == "status" && lt == "json":
			// the bare word of `vd status json` (SP-0063 8.1), and only there
		default:
			out[i] = "***"
		}
	}
}

// vdSizeShaped is a size as vd new reads one (parseSize): a decimal number,
// then at most one unit - k, m, g, t, with or without a b (20G, 1.5T, 2048).
func vdSizeShaped(t string) bool {
	i, dots := 0, 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.') {
		if t[i] == '.' {
			dots++
		}
		i++
	}
	if i == 0 || dots > 1 || i == dots {
		return false
	}
	switch strings.ToLower(t[i:]) {
	case "", "k", "m", "g", "t", "kb", "mb", "gb", "tb":
		return true
	}
	return false
}

// redactCredentialText screens a recorded message - an error's text for the
// finding event and history.json - by the same rule as the arguments: every
// word that is a credential-shaped `p:` token becomes `p:***`. An error
// quotes the word it failed on, and a password typed into a path or
// operation slot is that word (`Unknown command "p:.."`, `GetFileAttributesEx
// p:..: ..`); the arguments were redacted and the error was not (SP-0064
// T1-F2). A word starts after a space, a quote or an opening bracket and runs
// to the next space or quote; a `p:\..` or `p:/..` path is not a credential.
func redactCredentialText(s string) string {
	s = redactRememberedCredentials(s)
	if !strings.Contains(s, "p:") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "p:") && (i == 0 || strings.ContainsRune(" \t\r\n\"'`([{=", rune(s[i-1]))) {
			end := i + 2
			for end < len(s) && !strings.ContainsRune(" \t\r\n\"'`", rune(s[end])) {
				end++
			}
			if fdsecCredentialToken(s[i:end]) {
				b.WriteString("p:***")
				i = end
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// fdsecContainerSlotUnrecognised reports whether the container slot of the
// verb-first form (`fdsec info|verify <container> [password]`) holds a word
// that is not a container: nothing by that name exists, it is not written like
// a path (no separator, no .fd-sec name), and another token follows it. That is
// the bare form of the slip `fdsec verify <password> <container>`: the word may
// be the password, so it is refused like a `p:` token there and recorded as
// `***` - anything unrecognised is redacted (FDSEC-BEHAVIOUR section 8.2,
// SP-0064 AUD-52-F1). A slot with nothing after it is a path the user meant,
// however it is spelled, and a missing file there stays an I/O error.
func fdsecContainerSlotUnrecognised(slot string, followed bool) bool {
	if !followed || slot == "" || strings.ContainsAny(slot, `\/`) || strings.HasSuffix(strings.ToLower(slot), ".fd-sec") {
		return false
	}
	_, err := os.Stat(slot)
	return err != nil
}

// credentialValues are the values of the `p:` tokens this process was given,
// on its command line and on every batch line it began. redactCredentialText
// removes each one whole before its word rule runs: a password that holds a
// space is one argument, and the word rule alone kept everything after the
// first space (SP-0064 R-F1). A value shorter than three bytes is left to the
// word rule - replacing it everywhere would garble every message.
var credentialValues struct {
	sync.Mutex
	v []string
}

func rememberCredentialValues(args []string) {
	credentialValues.Lock()
	defer credentialValues.Unlock()
	for _, a := range args {
		if !fdsecCredentialToken(a) || len(a) < 5 {
			continue
		}
		v := a[2:]
		known := false
		for _, k := range credentialValues.v {
			if k == v {
				known = true
				break
			}
		}
		if !known {
			credentialValues.v = append(credentialValues.v, v)
		}
	}
}

func redactRememberedCredentials(s string) string {
	credentialValues.Lock()
	defer credentialValues.Unlock()
	for _, v := range credentialValues.v {
		s = strings.ReplaceAll(s, v, "***")
		if q := strconv.Quote(v); q[1:len(q)-1] != v {
			s = strings.ReplaceAll(s, q[1:len(q)-1], "***")
		}
	}
	return s
}
