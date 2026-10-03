//go:build windows

package main

import (
	"fmt"

	"filedo/vdisk"
)

// vdShareShipped says whether the FMS share surfaces of SP-0121 are part of this build: the verbs
// `vd share`, `vd autostart`, `vd open` and `vd close`, the mount words noletter, worker and stdin, and the
// shared-disk state that `vd status` and its JSON read.
//
// History: the owner held the surface back from the SP-0004 / SP-0063 release on 2026-10-02, at the SP-0064
// final audit, because the audit found open defects on it (AUD-86-F3 sent the disk password to whoever owned
// the pipe name, AUD-82-F2 showed a held disk as free). The tickets that gated it - AUD-82-F2..F4, AUD-83-F2,
// AUD-84-F1..F5 and F7..F11 (F6 with them), AUD-86-F2..F5, AUD-87-F8, AUD-90-F2 and F3, AUD-92-F1 - were
// closed the same day, each with a test that failed before its fix, and the owner then asked for the flip.
// Proven here: the unit and exchange tests, the GUI self-test and the build gate. NOT proven here, and
// required before a release that carries this surface (SP-0121 section 13, stage S4): a live mount of a
// no-letter disk by the FMS worker, the V3 folder mount seen from Explorer, a session-0 and reboot run, an
// unmodified FMS Android client against a shared disk, and the joint regression of DISK-SHARE-33..37, which
// exists only as a skeleton that skips without an installed worker.
//
// The code still reads vdShareOn, a variable only so the tests can put the surface off and prove every
// refusal; a release that must hold the surface back again flips this constant together with
// TestVDShare_ShippedOn.
const vdShareShipped = true

// vdShareOn is what the code reads. It is a variable only so the tests can run the surface itself and the
// refusals of the held-back build; the shipped default is the constant above.
var vdShareOn = vdShareShipped

// vdShareOffMountWord names the mount words only the FMS share surface uses. A password typed without p:
// may spell one of them, so the refusal says how to give it and never names the word.
const vdShareOffMountWord = "this mount option (a password that spells an option word is given as p:<password>)"

// vdShareOffError is the refusal of an entry this build does not carry: class 6, one fixed sentence.
// It never quotes the word the user typed, which may be a password.
func vdShareOffError(what string) error {
	return fmt.Errorf("%w: %s is not available in this build of FileDO", vdisk.ErrUnsupported, what)
}
