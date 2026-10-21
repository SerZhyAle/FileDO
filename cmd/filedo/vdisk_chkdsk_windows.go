//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/windows"

	"filedo/vdisk"
)

// chkdsk on a container's volume (the window's Check and Repair on a disk that was not closed
// cleanly). The verb is a scan by default - the disk is attached read-only and chkdsk runs without
// /f, so nothing is written - and a repair with the word fix: the disk is attached for writing,
// chkdsk /f /x runs on its volume, and the disk is detached again, all in one elevated step with
// one consent prompt (_chkdsk), the way format works.
//
// The container's marker follows the outcome. A repair that chkdsk finished closes the session
// clean, which is when "not closed cleanly" goes away; a repair that failed, or a detach that did
// not go through, leaves the marker as it was. A scan opens the container read-only and changes
// nothing at all. The verb never formats: there is no fallback to it, whatever chkdsk says.

// vdChkdskMaxText is how much of chkdsk's report the elevated step hands back: the tail, which is
// where the summary is.
const vdChkdskMaxText = 16 << 10

// chkdsk's exit codes (docs: "chkdsk" exit codes). 0 no errors; 1 errors found and fixed; 2 a
// cleanup was performed, or - without /f - one was not; 3 could not check the disk, or errors that
// were not fixed (always so without /f).
const (
	chkdskClean    = 0
	chkdskFixed    = 1
	chkdskCleanup  = 2
	chkdskNotFixed = 3
)

// vdChkdskVerdict reads chkdsk's exit code for a scan or a repair: ok, and the sentence that
// says what happened. A code this table does not know is a failure: it is never read as success.
func vdChkdskVerdict(code int, fix bool) (ok bool, what string) {
	switch {
	case code == chkdskClean || code == chkdskCleanup:
		return true, "found no problems"
	case code == chkdskFixed && fix:
		return true, "found problems and repaired them"
	case code == chkdskNotFixed && fix:
		return false, "could not repair the volume"
	case code == chkdskNotFixed || code == chkdskFixed:
		return false, "found problems, or could not finish the scan"
	}
	return false, fmt.Sprintf("ended with the unexpected exit code %d", code)
}

// vdChkdskOpts is a parsed chkdsk line.
type vdChkdskOpts struct {
	path  string
	fix   bool
	force bool
	cred  credArg
}

// vdParseChkdsk reads chkdsk's words in any order: fix, force (or -y) and the credential.
func vdParseChkdsk(args []string) (vdChkdskOpts, error) {
	var o vdChkdskOpts
	if len(args) < 1 {
		return o, vdUsagef("chkdsk needs a container: filedo <file.fdd> chkdsk [fix] [force]")
	}
	o.path = args[0]
	for i := 1; i < len(args); i++ {
		w := args[i]
		if a, ok := credentialToken(w); ok {
			if o.cred.given() {
				return o, vdUsagef("one credential per command: give p:, pf:, pe: or k: once")
			}
			o.cred = a
			continue
		}
		switch strings.ToLower(w) {
		case "fix":
			o.fix = true
		case "force", "-y":
			o.force = true
		default:
			// Never quoted back: the word may be a password without p:.
			return o, vdUsagef("unknown word %d for chkdsk: want fix, force, or a credential as p:, pf:, pe: or k:", i+1)
		}
	}
	return o, nil
}

// vdChkdsk: filedo <file.fdd> chkdsk [fix] [force] [password].
func vdChkdsk(args []string, batch bool) error {
	o, err := vdParseChkdsk(args)
	defer vdForgetEnv(o.cred)
	if err != nil {
		return err
	}
	path := o.path
	if abs, err := absPath(path); err == nil {
		path = abs
	}
	info, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	if o.fix && info.Profile == vdisk.ProfileSealed {
		return fmt.Errorf("%w: %s is sealed and is never written again, so a repair cannot run; chkdsk without fix scans it, or clone it into a writable copy and repair that", vdisk.ErrUnsupported, path)
	}
	if err := vdRefuseMounted(path, info, "chkdsk"); err != nil {
		return err
	}
	if o.fix {
		if batch && !o.force {
			return vdUsagef("chkdsk fix writes to the volume, and a batch never answers the question; a batch line says force")
		}
		fmt.Printf("chkdsk fix attaches %s (%s, %s), runs chkdsk /f on its volume and detaches it. Windows repairs the file system, which can drop files it cannot recover; copy the container file first if its contents matter.\n",
			path, vdSize(info.LogicalSize), vdProtectionNote(info))
		if !o.force && !vdConfirm("Repair it?", batch) {
			return vdUsagef("nothing was repaired: the question was not answered yes")
		}
	}
	if batch && !windows.GetCurrentProcessToken().IsElevated() {
		return errTransport("chkdsk needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
	}
	key, err := vdCredentialFor(info, o.cred)
	if err != nil {
		return err
	}
	defer clear(key)
	srv, err := vdLaunchServer(path, info, !o.fix, key, o.cred)
	if err != nil {
		return err
	}
	if !windows.GetCurrentProcessToken().IsElevated() && !batch {
		fmt.Println("chkdsk connects the disk through the Windows iSCSI initiator, which needs administrator rights; Windows will ask for consent now.")
	}
	req := vdRequest{Port: srv.h.Port, IQN: srv.h.IQN, Secret: srv.h.Secret, Serial: srv.h.Serial,
		Label: srv.h.Label, Chkdsk: true, Fix: o.fix, ReadOnly: !o.fix}
	res, rerr := vdRunElevated("_chkdsk", req, batch, nil)
	os.Remove(srv.handoff) // the secret is not needed any more
	if res.ChkdskText != "" {
		fmt.Println(strings.TrimRight(res.ChkdskText, "\n"))
	}
	ok, what := vdChkdskVerdict(res.ChkdskExit, o.fix)
	ran := rerr == nil
	// A repair that did not finish leaves the container marked as it was: the close is "unclean",
	// which keeps the marker (a server nobody logged in to closes the same either way).
	how := "clean"
	if o.fix && (!ran || !ok) {
		how = "unclean"
	}
	srv.stop(how)
	if rerr != nil {
		return rerr
	}
	where := vdQuoteArg(path)
	if !ok {
		if o.fix {
			vdLogf("chkdsk fix %s: %s (exit %d)", path, what, res.ChkdskExit)
			return fmt.Errorf("%w: chkdsk %s of %s (exit %d). The container stays marked not closed cleanly. Copy the file before anything else is done to it", vdisk.ErrDamaged, what, path, res.ChkdskExit)
		}
		vdLogf("chkdsk %s: %s (exit %d)", path, what, res.ChkdskExit)
		return fmt.Errorf("%w: chkdsk %s in the volume of %s (exit %d). Repair it with: filedo %s chkdsk fix", vdisk.ErrDamaged, what, path, res.ChkdskExit, where)
	}
	// The window words its outcome line from this number, not from the sentences below.
	runNumber("chkdsk_exit", res.ChkdskExit)
	if o.fix {
		fmt.Printf("chkdsk %s in the volume of %s, and the container is closed cleanly.\n", what, path)
		vdLogf("chkdsk fix %s: %s (exit %d)", path, what, res.ChkdskExit)
		return nil
	}
	fmt.Printf("chkdsk %s in the volume of %s. Nothing was written.\n", what, path)
	vdLogf("chkdsk %s: %s (exit %d)", path, what, res.ChkdskExit)
	return nil
}

// vdChkdskStep is the elevated half: the attach with Chkdsk (the disk online, the volume given a
// letter), chkdsk on that letter, and the ordinary detach - which runs whatever chkdsk said, so
// no failed run leaves a disk attached.
func vdChkdskStep(req vdRequest, cancelPath string) (vdResult, error) {
	req.Chkdsk = true
	res, err := vdAttach(req, cancelPath)
	if err != nil {
		return res, err
	}
	code, text, rerr := vdChkdskRun(res.Letter, req.Fix)
	res.ChkdskExit, res.ChkdskText = code, text
	_, derr := vdDetach(vdRequest{Port: req.Port, IQN: req.IQN, Serial: req.Serial, Session: res.Session})
	switch {
	case rerr != nil && derr != nil:
		return res, fmt.Errorf("%v; and the disk could not be detached: %w", rerr, derr)
	case rerr != nil:
		return res, rerr
	case derr != nil:
		return res, fmt.Errorf("chkdsk finished (exit %d), but the disk could not be detached: %w", code, derr)
	}
	return res, nil
}

// vdChkdskRun runs chkdsk on a drive letter and returns its exit code and its report. The
// executable is the system one by full path: this process is elevated, and a PATH lookup is not
// something to trust there. Without fix the run is read-only; with it, /f /x - /x dismounts the
// volume first, so chkdsk never asks to schedule the check for the next restart.
func vdChkdskRun(letter string, fix bool) (int, string, error) {
	if len(letter) != 2 || letter[1] != ':' {
		return -1, "", fmt.Errorf("chkdsk was not run: the volume has no drive letter (%q)", letter)
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return -1, "", fmt.Errorf("chkdsk was not run: %w", err)
	}
	args := []string{letter}
	if fix {
		args = append(args, "/f", "/x")
	}
	cmd := exec.Command(filepath.Join(sys, "chkdsk.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	text := vdChkdskTidy(vdOEMToString(out))
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, text, nil
	case errors.As(err, &ee):
		return ee.ExitCode(), text, nil
	}
	return -1, text, fmt.Errorf("chkdsk could not be run: %w", err)
}

// vdOEMToString decodes the console code page chkdsk writes in.
func vdOEMToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const cpOEM = 1 // CP_OEMCP
	n, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return strings.ToValidUTF8(string(b), "?")
	}
	w := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), &w[0], n); err != nil {
		return strings.ToValidUTF8(string(b), "?")
	}
	return windows.UTF16ToString(w)
}

// vdChkdskTidy makes chkdsk's report readable once it is not on a terminal: a progress line that
// chkdsk redraws with a carriage return keeps only its last drawing, and the whole is cut to its
// tail (at a character boundary), which is where the summary is.
func vdChkdskTidy(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if j := strings.LastIndex(l, "\r"); j >= 0 {
			lines[i] = l[j+1:]
		}
	}
	s = strings.TrimSpace(strings.Join(lines, "\n"))
	if len(s) > vdChkdskMaxText {
		s = s[len(s)-vdChkdskMaxText:]
		for len(s) > 1 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return s
}
