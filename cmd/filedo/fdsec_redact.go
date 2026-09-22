package main

import "strings"

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

// fdsecStructureWord lists option words - tokens that are never the
// credential.
func fdsecStructureWord(s string) bool {
	switch s {
	case "del", "delete", "wipe", "rename", "ren", "here", "to",
		"-y", "y", "--force", "force", "-rw", "rw", "-keep", "keep",
		"-all-users":
		return true
	}
	return fdsecSubVerb(s)
}

// redactCredentialArgs returns a copy of args with credential material
// removed from fdsec-family command lines; every other command line passes
// through unchanged. The shape stays legible: p:<password> becomes p:***, a
// bare password becomes ***. pf:, pe: and k: keep their path or variable
// name - the name is not the secret, and the value never enters a command
// line. Anything unrecognized after an fdsec token is redacted too: redacting
// garbage costs a history line, missing a password costs the user's disk.
//
// Two tokens are deliberately kept because they are paths, not secrets, and a
// history entry without them says nothing: the destination after `to`, and
// the container path after a sub-verb of the verb-first form
// (`filedo fdsec verify <container> <password>`). In the target-first form
// the target sits ahead of the verb and is never touched at all.
func redactCredentialArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
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
	if verbAt < 0 {
		return out
	}
	keepNext := false // set after "to" and after a sub-verb: a path follows
	for i := verbAt + 1; i < len(out); i++ {
		t := out[i]
		lt := strings.ToLower(t)
		if keepNext {
			keepNext = false
			continue
		}
		switch {
		case lt == "to", fdsecSubVerb(lt):
			keepNext = true
		case strings.HasPrefix(t, "p:"):
			out[i] = "p:***"
		case strings.HasPrefix(t, "pf:"), strings.HasPrefix(t, "pe:"), strings.HasPrefix(t, "k:"):
			// path or variable name: kept, it is not the secret
		case fdsecStructureWord(lt):
			// option word
		default:
			out[i] = "***"
		}
	}
	return out
}
