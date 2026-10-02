package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"filedo/fdsec"
)

// The one credential grammar of both container families (FD-SEC-CONTRACT
// section 8.1, FDD-BEHAVIOUR section 7 rule 1): a second grammar for the same
// act is a defect. fdsec's parser and vd's parser each read their own options
// and hand every credential-shaped token here, and both resolve through
// resolveCredential, so the prefixes, the pf: newline rule, the pe: removal on
// read, the keyfile digest and the no-terminal usage error exist once.

// credArg is one credential source as the command line names it. src is ""
// (none given: prompt), "p", "pf", "pe", "k" or "bare".
type credArg struct {
	src string
	val string
}

func (a credArg) given() bool { return a.src != "" }

// keyfile reports whether the credential is a keyfile's digest.
func (a credArg) keyfile() bool { return a.src == "k" }

// credentialToken recognises a prefixed credential token. Prefixes are
// case-sensitive: `P:x` is never one.
func credentialToken(t string) (credArg, bool) {
	switch {
	case strings.HasPrefix(t, "p:"):
		return credArg{"p", t[2:]}, true
	case strings.HasPrefix(t, "pf:"):
		return credArg{"pf", t[3:]}, true
	case strings.HasPrefix(t, "pe:"):
		return credArg{"pe", t[3:]}, true
	case strings.HasPrefix(t, "k:"):
		return credArg{"k", t[2:]}, true
	}
	return credArg{}, false
}

// resolveCredential resolves a credential from its source, prompting without
// echo when none was given - twice when confirm is set, because a typo would
// lock the data behind a credential the owner never meant. prompt is the first
// question, which is where each family says what an empty answer means.
func resolveCredential(a credArg, confirm bool, prompt string) (fdsec.Credential, error) {
	switch a.src {
	case "stdin":
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20+1))
		if err != nil {
			return nil, err
		}
		defer clear(b)
		if len(b) > 1<<20 {
			return nil, usagef("stdin credential is too long")
		}
		return fdsec.Credential(append([]byte(nil), b...)), nil
	case "p", "bare":
		return fdsec.NewCredential(a.val), nil
	case "pf":
		return fdsecCredentialFromFile(a.val, false)
	case "pe":
		v, ok := fdsecLookupCredentialEnv(a.val)
		// A named variable that is unset or empty is a mistake, never a
		// choice: the GUI's "Open in Command" once lost the password this way
		// and `secure wipe -y` then wrote a container with no secrecy and
		// overwrote the original (FDSEC-19, GUI-02). An empty password is
		// still possible - typed at the prompt or as p: - where it is visible.
		if !ok {
			return nil, usagef("environment variable %s is not set, so no password was given; nothing was written", a.val)
		}
		if v == "" {
			return nil, usagef("environment variable %s is empty, so no password was given; nothing was written (an empty password is typed at the prompt or given as p: on purpose)", a.val)
		}
		return fdsec.NewCredential(v), nil
	case "k":
		return fdsecCredentialFromFile(a.val, true)
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
	first, err := read(prompt)
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
		return nil, errCredentialMismatch
	}
	return fdsec.NewCredential(first), nil
}

func isCredentialToken(t string) bool {
	_, ok := credentialToken(t)
	return ok
}

// errCredentialMismatch is the double prompt's two answers differing. The
// sibling keeps its historical class for it; vd reports it as usage.
var errCredentialMismatch = errors.New("the two passwords differ; nothing was written")
