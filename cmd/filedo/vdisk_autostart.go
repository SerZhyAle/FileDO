//go:build windows

package main

import (
	"errors"
	"filedo/fmsworker"
	"filedo/vdisk"
	"fmt"
	"strings"
)

// vdAutostart changes the worker's per-container mark. Providing a credential
// does not constitute consent to persist it (DISK-SHARE 12).
func vdAutostart(args []string, batch bool) error { return vdWorkerError(vdAutostartRun(args, batch)) }
func vdAutostartRun(args []string, batch bool) error {
	if len(args) < 2 {
		return vdUsagef("vd autostart needs <container> on|off [consent] [credential]")
	}
	action := strings.ToLower(args[1])
	if action != "on" && action != "off" {
		return vdUsagef("vd autostart needs on or off")
	}
	var cred credArg
	consent := false
	for i := 2; i < len(args); i++ {
		if args[i] == "consent" && !consent && action == "on" {
			consent = true
			continue
		}
		if a, ok := credentialToken(args[i]); ok && action == "on" && !cred.given() {
			cred = a
			continue
		}
		return vdUsagef("invalid autostart option at word %d", i+1)
	}
	path, e := vdSharePath(args[0], action == "on")
	if e != nil {
		return e
	}
	client, e := getFMSWorkerClient()
	if e != nil {
		return e
	}
	if _, e = client.GetDiskStatus(path); e != nil {
		if vdNotShared(e) {
			return fmt.Errorf("share the container first: %w", e)
		}
		return e
	}
	var password []byte
	encrypted := false
	if action == "on" {
		info, e := vdShareInspect(path)
		if e != nil {
			return e
		}
		if !info.Obfuscated {
			fmt.Println("Autostart stores the credential in the holder's protected store on this PC.")
			fmt.Println("After startup the disk opens for every paired device without anyone typing its password. Encryption still protects a copy of the closed container.")
			if !consent && !vdAutostartConsent("Store the credential for this disk's autostart?", batch) {
				return fmt.Errorf("%w: password storage needs explicit consent; use consent in an unattended command", vdisk.ErrUsage)
			}
			encrypted = true
			// The existing password is being used, not set: ask once (DISK-SHARE-21).
			password, e = vdAutostartCredential(cred, false)
			if e != nil {
				return e
			}
			defer clear(password)
		} else if cred.given() {
			return vdUsagef("a plain container needs no autostart credential")
		}
	}
	if e = client.SetAutostart(path, action == "on", password); e != nil {
		return e
	}
	// Say nothing the holder has not confirmed: read its record back.
	st, e := client.GetDiskStatus(path)
	if e != nil {
		return fmt.Errorf("autostart %s was sent, but the holder's state could not be read back to confirm it: %w", action, e)
	}
	if e = vdVerifyAutostart(st, action == "on", encrypted); e != nil {
		return e
	}
	if e = vdRefreshShareSnapshot(client); e != nil {
		vdSnapshotWarn("autostart changed", e)
	}
	if action == "on" {
		fmt.Printf("Autostart enabled for %s.\n", path)
	} else {
		fmt.Printf("Autostart disabled for %s; the holder destroyed its stored key.\n", path)
	}
	return nil
}

// vdVerifyAutostart checks the holder's own record against what was asked. An
// encrypted disk must also show its stored key after on, and no stored key
// after off (DISK-SHARE-22: the mark and the key go together).
func vdVerifyAutostart(st *fmsworker.SharedDiskInfo, enable, encrypted bool) error {
	switch {
	case enable && !st.Autostart:
		return errors.New("the holder did not enable autostart; its record still shows it off")
	case enable && (encrypted || st.Encrypted) && !st.HasStoredKey:
		return errors.New("the holder enabled autostart but reports no stored key for this encrypted disk, so it would not open at startup")
	case !enable && st.Autostart:
		return errors.New("the holder did not disable autostart; its record still shows it on")
	case !enable && st.HasStoredKey:
		return errors.New("the holder cleared autostart but still reports a stored key; the key was not destroyed")
	}
	return nil
}
