//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"

	"filedo/vdisk"
)

// The destructive pair (SP-0004 spec 5.7, P6 T6.13), never confused: format
// destroys the volume INSIDE a container and keeps the container; destroy
// removes the container FILE. Both refuse a mounted container, and no option
// skips that check. Both ask y/N; force (or -y) skips the question and never a
// check, and a batch - which never answers a question yes - needs force.
// destroy wipe overwrites through the existing single-file wipe
// (handleFileWipeCommand, wipe_handler.go), with its guards and its honest
// caveat.

// ---------------------------------------------------------------- format

// vdFormat: filedo <file.fdd> format [fs ntfs|exfat] [label <text>] [force]
// [password]. The volume is formatted through the mount path's own first-format
// route (formatBlankDisk), in one elevated step that attaches the disk, clears
// and formats it, and detaches it again (_format).
func vdFormat(args []string, batch bool) error {
	o, err := vdParseFormat(args)
	defer vdForgetEnv(o.cred)
	if err != nil {
		return err
	}
	path, fs, label, force, cred := o.path, o.fs, o.label, o.force, o.cred
	if abs, err := absPath(path); err == nil {
		path = abs
	}
	info, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	if info.Profile == vdisk.ProfileSealed {
		return fmt.Errorf("%w: %s is sealed and is never written again; clone it into a writable copy and format that", vdisk.ErrUnsupported, path)
	}
	if err := vdRefuseMounted(path, info, "format"); err != nil {
		return err
	}
	if batch && !force {
		return vdUsagef("format destroys the volume's contents, and a batch never answers the question; a batch line says force")
	}
	if label == "" {
		label = info.FriendlyName
	}
	fmt.Printf("format destroys everything on the volume inside %s (%s, %s) and leaves an empty %s volume. The container file stays, with its id and its credential.\n",
		path, vdSize(info.LogicalSize), vdProtectionNote(info), vdFileSystem(fs))
	if !force && !vdConfirm("Format it?", batch) {
		return vdUsagef("nothing was formatted: the question was not answered yes")
	}
	if batch && !windows.GetCurrentProcessToken().IsElevated() {
		return errTransport("formatting needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
	}
	key, err := vdCredentialFor(info, cred)
	if err != nil {
		return err
	}
	defer clear(key)
	srv, err := vdLaunchServer(path, info, false, key, cred)
	if err != nil {
		return err
	}
	if !windows.GetCurrentProcessToken().IsElevated() && !batch {
		fmt.Println("Formatting connects the disk through the Windows iSCSI initiator, which needs administrator rights; Windows will ask for consent now.")
	}
	req := vdRequest{Port: srv.h.Port, IQN: srv.h.IQN, Secret: srv.h.Secret, Serial: srv.h.Serial,
		Label: label, FormatOnly: true, FS: fs}
	_, err = vdRunElevated("_format", req, batch, nil)
	os.Remove(srv.handoff) // the secret is not needed any more
	srv.stop("clean")
	if err != nil {
		return err
	}
	shown := strings.TrimSpace(labelUnsafe.ReplaceAllString(label, ""))
	if shown == "" {
		shown = "FileDO"
	}
	fmt.Printf("Formatted the volume of %s: an empty %s volume labelled %s. The container file, its id and its credential are unchanged.\n", path, vdFileSystem(fs), shown)
	vdLogf("format %s as %s", path, vdFileSystem(fs))
	return nil
}

// vdFormatOpts is a parsed format line.
type vdFormatOpts struct {
	path, fs, label string
	force           bool
	cred            credArg
}

// vdParseFormat reads format's words in any order: fs ntfs|exfat,
// label <text>, force (or -y) and the credential.
func vdParseFormat(args []string) (vdFormatOpts, error) {
	o := vdFormatOpts{fs: "ntfs"}
	if len(args) < 1 {
		return o, vdUsagef("format needs a container: filedo <file.fdd> format [fs ntfs|exfat] [label <text>] [force]")
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
		case "force", "-y":
			o.force = true
		case "fs":
			if i+1 >= len(args) || (!strings.EqualFold(args[i+1], "ntfs") && !strings.EqualFold(args[i+1], "exfat")) {
				return o, vdUsagef("fs takes ntfs or exfat")
			}
			o.fs = strings.ToLower(args[i+1])
			i++
		case "label":
			if i+1 >= len(args) {
				return o, vdUsagef("label needs a text")
			}
			o.label = args[i+1]
			i++
		default:
			// Never quoted back: the word may be a password without p:.
			return o, vdUsagef("unknown word %d for format: want fs ntfs|exfat, label <text>, force, or a credential as p:, pf:, pe: or k:", i+1)
		}
	}
	return o, nil
}

// vdFormatStep is the elevated half of format: the attach with FormatOnly
// (which clears and formats the disk and gives it no letter), then the
// ordinary detach. A failed attach has rolled itself back already.
func vdFormatStep(req vdRequest, cancelPath string) (vdResult, error) {
	res, err := vdAttach(req, cancelPath)
	if err != nil {
		return res, err
	}
	if _, derr := vdDetach(vdRequest{Port: req.Port, IQN: req.IQN, Serial: req.Serial, Session: res.Session}); derr != nil {
		return res, fmt.Errorf("the volume was formatted, but the disk could not be detached: %w", derr)
	}
	return res, nil
}

// ---------------------------------------------------------------- destroy

// vdDestroy: filedo <file.fdd> destroy [wipe] [force]. It takes no credential:
// removing a file needs none. A container that does not read as one (damaged,
// or not a container at all) is still removed when named, and says so.
func vdDestroy(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("destroy needs a container: filedo <file.fdd> destroy [wipe] [force]")
	}
	path := args[0]
	wipe, force := false, false
	for i, w := range args[1:] {
		switch strings.ToLower(w) {
		case "wipe":
			wipe = true
		case "force", "-y":
			force = true
		default:
			if isCredentialToken(w) {
				return vdUsagef("destroy takes no credential: removing the file needs none")
			}
			// Never quoted back: the word may be a password.
			return vdUsagef("unknown word %d for destroy: want wipe, force", i+2)
		}
	}
	if !isFddPath(path) {
		return vdUsagef("destroy removes a container file, whose name ends in .fdd (or a name registered with vd add)")
	}
	abs, err := absPath(path)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("%w: %v", vdisk.ErrIO, err)
	}
	if !fi.Mode().IsRegular() {
		return vdUsagef("%s is not a regular file, and destroy removes only a container file", path)
	}
	info, ierr := vdisk.Inspect(abs)
	if ierr != nil && errors.Is(ierr, vdisk.ErrIO) {
		return ierr
	}
	if ierr == nil {
		if err := vdRefuseMounted(path, info, "destroy"); err != nil {
			return err
		}
	}
	// Even a damaged file can still be in use. If the state cannot be read,
	// the absence of a mount cannot be established; force only skips prompts.
	s, err := vdLoadState()
	if err != nil {
		return errBusy(fmt.Sprintf("the mount state cannot be read, so %s cannot be proved unused: %v", path, err))
	}
	for _, m := range s.Mounts {
		if strings.EqualFold(m.Path, abs) || (ierr == nil && strings.EqualFold(m.ContainerID, info.ContainerID)) {
			return errBusy(fmt.Sprintf("%s is mounted at %s; destroy needs it unmounted first (filedo %s unmount), and no option skips that", path, m.Letter, m.Letter))
		}
	}
	// The registry: a name for this file is forgotten with it, but a name
	// that mounts automatically is refused - the task would fail at every
	// logon, and removing it needs its own consent.
	reg, err := vdLoadRegistry()
	if err != nil {
		return err
	}
	var names []string
	for _, e := range reg.Containers {
		if strings.EqualFold(e.Path, abs) {
			names = append(names, e.Name)
		}
	}
	if len(names) > 0 {
		tasks := vdAutoTasks()
		for _, n := range names {
			if tasks[strings.ToLower(n)] {
				return vdUsagef("%s mounts automatically at logon as %s; run: filedo vd auto off %s first", path, n, n)
			}
		}
	}
	if batch && !force {
		return vdUsagef("destroy removes the container file, and a batch never answers the question; a batch line says force")
	}
	fmt.Printf("destroy removes the container file %s, %s: the volume inside and everything on it are gone for good.\n", abs, formatBytes(uint64(fi.Size())))
	if ierr != nil {
		fmt.Printf("It does not read as a container (%s). It is removed all the same, as named.\n", vdExplain(ierr))
	}
	if wipe {
		// The existing single-file wipe: its guards, its caveat, and - without
		// force - its typed WIPE, which is this verb's question then.
		wargs := []string{}
		if force {
			wargs = append(wargs, "--force")
		}
		if err := handleFileWipeCommand(abs, wargs); err != nil {
			if _, serr := os.Stat(abs); serr == nil && strings.Contains(err.Error(), "cancelled") {
				return vdUsagef("nothing was removed: WIPE was not typed")
			}
			return fmt.Errorf("%w: %v", vdisk.ErrIO, err)
		}
	} else {
		if !force && !vdConfirm("Remove it?", batch) {
			return vdUsagef("nothing was removed: the question was not answered yes")
		}
		if err := os.Remove(abs); err != nil {
			if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
				return errBusy(fmt.Sprintf("%s is open in another program, and nothing was removed", path))
			}
			return fmt.Errorf("%w: %v", vdisk.ErrIO, err)
		}
	}
	fmt.Printf("Destroyed %s.\n", abs)
	if len(names) > 0 {
		if err := vdUpdateRegistry(func(r *vdRegistry) error {
			kept := r.Containers[:0]
			for _, e := range r.Containers {
				if !strings.EqualFold(e.Path, abs) {
					kept = append(kept, e)
				}
			}
			r.Containers = kept
			return nil
		}); err != nil {
			return fmt.Errorf("%s is removed, but its registered name could not be forgotten (filedo vd forget %s does it): %w", abs, names[0], err)
		}
		fmt.Printf("Forgot the registered name %s.\n", strings.Join(names, ", "))
	}
	vdLogf("destroy %s (wipe=%v)", abs, wipe)
	return nil
}
