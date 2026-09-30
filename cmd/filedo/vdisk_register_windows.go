//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"filedo/vdisk"
)

// The Explorer registration of the .fdd disk container (SP-0004 spec 6.1-6.3,
// P6 T6.16): `filedo vd register [-all-users]` and `filedo vd unregister
// [-all-users]`.
//
// Three writers, one table (spec 6.2, R8). packaging/wix/FileDO.wxs writes the
// same keys machine-wide as the DiskContainerIntegration feature;
// this command writes them for the channels where no installer runs (winget's
// zip + portable package, the plain zip, go install); the MSIX declares .fdd
// for the GUI app in its manifest and writes nothing here.
// TestRegistryParity_TheMSIAndVdRegisterWriteTheSameDiskContainerType holds
// the first two to each other value for value.
//
// It is the sibling's machinery (fdsec_register*.go), not a copy of it: the
// scope and its test seam (FILEDO_FDSEC_REG_ROOT), the FileDO.RegisteredBy
// mark and the ownership check, the previous-owner hand-back, the package
// family check and the shell notification are all the sibling's. What differs
// is the table, and two deliberate divergences recorded in spec 6.1 and 6.4:
// the type carries its own verbs (there is no group on every file), and the
// verbs open the window, not the console.
//
// The written table (P6 section 4.1), below HKCU or HKLM \Software\Classes:
//
//	.fdd                         (Default) = FileDO.DiskContainer
//	.fdd\OpenWithProgids         FileDO.DiskContainer = ""
//	FileDO.DiskContainer         (Default), FriendlyTypeName = FileDO disk container;
//	                             FileDO.RegisteredBy = <this filedo.exe>
//	  \DefaultIcon               <install>\icons\content.disk-container.ico
//	  \shell                     (Default) = mount
//	  \shell\mount               MUIVerb = Mount with FileDO;  \command = "<install>\filedo_win.exe" "%1"
//	  \shell\mountro             MUIVerb = Mount read-only;    \command = .. --mount-ro "%1"
//	  \shell\unmount             MUIVerb = Unmount;            \command = .. --unmount "%1"
//
// Unmount is always present (spec 6.1 as amended 2026-09-26): a classic verb
// cannot be shown conditionally, and on a container that is not mounted the
// window says so.
const (
	// vdProgID is the document type. A frozen anchor from the first shipped
	// container, like fdsecProgID (spec 5.8 item 6, spec 6.2).
	vdProgID = "FileDO.DiskContainer"
	// vdExtension is the container's extension (Q14), also an anchor.
	vdExtension = ".fdd"
	// vdTypeLabel is what Explorer's Type column and properties page show.
	vdTypeLabel = "FileDO disk container"
	// vdContainerIcon is the ICON-SET meaning of the type (ICON-SET 0.16),
	// drawn by filedo_win.exe --write-menu-icons into assets\menu-icons.
	vdContainerIcon = "content.disk-container"
	// vdGUIExe answers every verb (spec 6.4, 5.8 item 8): the window asks the
	// unclean-marker question, the credential and the elevation, never a
	// console.
	vdGUIExe = "filedo_win.exe"
	// vdRegisterUsage is the one line both refusals quote.
	vdRegisterUsage = "filedo vd register [-all-users] | filedo vd unregister [-all-users]"
)

// vdShellVerb is one verb of the document type. key is the registry key name
// (and the verb's canonical name); args follow the GUI's path, with "%1"
// where the shell puts the clicked file.
type vdShellVerb struct {
	key, label, args string
}

// vdShellVerbs are the verbs of spec 6.1, the default one first.
var vdShellVerbs = []vdShellVerb{
	{key: "mount", label: "Mount with FileDO", args: `"%1"`},
	{key: "mountro", label: "Mount read-only", args: `--mount-ro "%1"`},
	{key: "unmount", label: "Unmount", args: `--unmount "%1"`},
}

// vdShellCommand is one verb's command line: the GUI beside this exe, quoted,
// then the verb's arguments. "%1" stays quoted: a path with a space in it is
// the common case.
func vdShellCommand(gui string, v vdShellVerb) string {
	return `"` + gui + `" ` + v.args
}

// vdProgIDMountCommand is the key whose presence answers "is the type
// registered in this scope" - the default verb's command, not the type key,
// so an empty key left by a half-finished removal does not count.
var vdProgIDMountCommand = vdProgID + `\shell\` + vdShellVerbs[0].key + `\command`

// vdRegisteredIn reports the default verb's command in one scope.
func vdRegisteredIn(allUsers bool) (string, bool) {
	return fdsecRegScopeFor(allUsers).getString(vdProgIDMountCommand, "")
}

// vdShellRegistration runs `vd register` and `vd unregister`.
func vdShellRegistration(sub string, args []string) error {
	allUsers := false
	for _, a := range args {
		switch strings.ToLower(a) {
		case "-all-users", "--all-users", "-allusers":
			allUsers = true
		default:
			// Never quoted back: a stray word may be a password, and this
			// message reaches history.json.
			return vdUsagef("vd %s takes no word but -all-users: %s", sub, vdRegisterUsage)
		}
	}
	// The Store build declares .fdd in its manifest (T6.27) and can write
	// nothing here: Windows keeps a packaged app's writes under
	// Software\Classes to the app itself, where Explorer never looks.
	if os.Getenv(fdsecAsPackagedEnv) == "1" || vdPackaged() {
		return fmt.Errorf("%w: vd %s is not available in the Microsoft Store build, because Windows keeps a packaged app's registry writes to the app itself and the Store build opens .fdd files through its own package instead, while the setup, winget and zip builds register them with this command", vdisk.ErrUnsupported, sub)
	}
	if sub == "unregister" {
		return vdShellUnregister(allUsers)
	}
	return vdShellRegister(allUsers)
}

// vdRequireWritable is the sibling's writable-scope check in this family's
// exit class: -all-users from an ordinary console is class 6, with the way
// out in the same sentence.
func vdRequireWritable(scope fdsecRegScope) error {
	if err := scope.writeDenied(); err != nil {
		return fmt.Errorf("%w: writing the machine-wide registration needs an elevated console (%s\\%s: %v); without -all-users the same entries are written for this user only",
			vdisk.ErrUnsupported, scope.name, scope.path(""), err)
	}
	return nil
}

func vdAllUsersFlag(allUsers bool) string {
	if allUsers {
		return " -all-users"
	}
	return ""
}

// vdShellRegister writes the table above into one scope.
func vdShellRegister(allUsers bool) error {
	exe, err := fdsecRegisteredExe()
	if err != nil {
		return fmt.Errorf("cannot resolve this executable's own path: %w", err)
	}
	// Every verb opens the window. A registration whose verbs name a program
	// that is not there is a double-click that fails with Windows' own error,
	// so it is refused rather than written (a bare filedo.exe, go install).
	gui := filepath.Join(filepath.Dir(exe), vdGUIExe)
	if !fileExistsPlain(gui) {
		return fmt.Errorf("%w: the .fdd verbs open the FileDO window, and %s is not beside this filedo.exe - register from the folder of a full FileDO install (the setup, winget or the zip)", vdisk.ErrUnsupported, vdGUIExe)
	}

	// A FileDO package installed for this user already holds .fdd (its
	// manifest declares the type for the window): the classic copy is the
	// one that yields, as the sibling's does (SP-0020 D3). A machine-wide
	// copy still serves every other account, so -all-users writes and says so.
	if fam, ok := fdsecPackagedMenuInstalled(); ok {
		if !allUsers {
			fmt.Println("The FileDO package is installed for this user (" + fam + ") and already opens")
			fmt.Println(".fdd files - nothing written.")
			fmt.Println("A classic copy beside it would give .fdd two owners. Register again after")
			fmt.Println("uninstalling the package if you want the classic type with its mount verbs.")
			vdLogf("register %s: stood down, package %s holds .fdd", "HKCU", fam)
			return nil
		}
		fmt.Println("Note: the FileDO package is installed for this user (" + fam + "), so this")
		fmt.Println("account has .fdd from both. Other accounts see only this one.")
	}
	// HKLM wins: both scopes merge into one view, and the copy an uninstall
	// can take back is the one that stays.
	if !allUsers {
		if cmd, ok := vdRegisteredIn(true); ok {
			fmt.Println("Already registered for all users on this machine - nothing written.")
			fmt.Println("  " + cmd)
			fmt.Println("A second, per-user copy would shadow the machine-wide one. Remove that one")
			fmt.Println("first (uninstall FileDO or deselect its disk container feature, or run an")
			fmt.Println("elevated `filedo vd unregister -all-users`) if you want a per-user copy.")
			vdLogf("register %s: stood down, HKLM holds .fdd", "HKCU")
			return nil
		}
	}

	scope := fdsecRegScopeFor(allUsers)
	if allUsers {
		if err := vdRequireWritable(scope); err != nil {
			return err
		}
	}
	icon := fdsecRegisteredIcon(exe)
	if ico, ok := fdsecMeaningIcon(exe, vdContainerIcon); ok {
		icon = ico
	}

	// The extension first, remembering whoever owned it, so unregister can
	// hand it back instead of leaving a file type nobody opens.
	if prev, ok := scope.getString(vdExtension, ""); ok && prev != "" && prev != vdProgID {
		if err := scope.setString(vdExtension, fdsecPreviousProgIDValue, prev); err != nil {
			return err
		}
	}
	type step struct{ sub, name, value string }
	steps := []step{
		{vdExtension, "", vdProgID},
		// OpenWithProgids as well as the default (spec 6.2): FileDO stays in
		// "Open with" when another program later claims .fdd, and is
		// displaced from the default without being erased.
		{vdExtension + `\OpenWithProgids`, vdProgID, ""},
		{vdProgID, "", vdTypeLabel},
		{vdProgID, "FriendlyTypeName", vdTypeLabel},
		{vdProgID, fdsecOwnerValue, exe},
		{vdProgID + `\DefaultIcon`, "", icon},
		{vdProgID + `\shell`, "", vdShellVerbs[0].key},
	}
	for _, v := range vdShellVerbs {
		steps = append(steps,
			step{vdProgID + `\shell\` + v.key, "MUIVerb", v.label},
			step{vdProgID + `\shell\` + v.key + `\command`, "", vdShellCommand(gui, v)},
		)
	}
	for _, st := range steps {
		if err := scope.setString(st.sub, st.name, st.value); err != nil {
			vdLogf("register %s: stopped partway: %v", scope.name, err)
			return fmt.Errorf("registration stopped partway: %w\nrun `filedo vd unregister%s` to clear what was written", err, vdAllUsersFlag(allUsers))
		}
	}

	fdsecNotifyAssociationsChanged()
	vdLogf("register %s: %s -> %s, verbs %s, exe %s", scope.name, vdExtension, vdProgID, vdVerbKeys(), exe)
	vdRegisterReport(allUsers, gui, icon)
	if allUsers {
		vdWarnPerUserCopy()
	}
	return nil
}

func vdVerbKeys() string {
	keys := make([]string, 0, len(vdShellVerbs))
	for _, v := range vdShellVerbs {
		keys = append(keys, v.key)
	}
	return strings.Join(keys, ",")
}

// vdRegisterReport prints what was written, key by key: a registration the
// user cannot read back is one they cannot undo by hand.
func vdRegisterReport(allUsers bool, gui, icon string) {
	root := "HKCU"
	if allUsers {
		root = "HKLM"
	}
	fmt.Printf("Registered the disk container type for %s:\n", fdsecScopeWord(allUsers))
	fmt.Printf("  %s\\Software\\Classes\\%s  ->  %s  (%s)\n", root, vdExtension, vdProgID, vdTypeLabel)
	fmt.Printf("  icon: %s\n", icon)
	for i, v := range vdShellVerbs {
		mark := ""
		if i == 0 {
			mark = "  (double-click)"
		}
		fmt.Printf("  %-18s %s%s\n", v.label, vdShellCommand(gui, v), mark)
	}
	fmt.Println("On Windows 11 the verbs are under \"Show more options\".")
	fmt.Println("Remove it with: filedo vd unregister" + vdAllUsersFlag(allUsers))
}

// vdWarnPerUserCopy reports a per-user registration under this account after
// a machine-wide change: the per-user one takes precedence in Explorer, so it
// keeps running whatever exe it names. Reported, never removed on another
// account's behalf (FDSEC-17, the sibling's rule).
func vdWarnPerUserCopy() {
	if cmd, ok := vdRegisteredIn(false); ok {
		fmt.Println("Note: this account also has a per-user .fdd registration, and it takes")
		fmt.Println("precedence over the machine-wide one in Explorer:")
		fmt.Println("  " + cmd)
		fmt.Println("Remove it with `filedo vd unregister` (without -all-users), run as this account.")
	}
}

// vdShellUnregister removes what vdShellRegister wrote, and only what carries
// FileDO's mark. .fdd goes back to its previous owner; another program's
// "Open with" entry under it is left where it is.
func vdShellUnregister(allUsers bool) error {
	scope := fdsecRegScopeFor(allUsers)
	if allUsers {
		if err := vdRequireWritable(scope); err != nil {
			return err
		}
	}
	if scope.keyExists(vdProgID) {
		if err := scope.requireOurs(vdProgID, vdProgIDMountCommand); err != nil {
			return fmt.Errorf("%w: %v", vdisk.ErrUsage, err)
		}
	}
	removed := 0
	openWith := vdExtension + `\OpenWithProgids`
	if _, ok := scope.getString(openWith, vdProgID); ok {
		scope.deleteValue(openWith, vdProgID)
		removed++
	}
	if owner, ok := scope.getString(vdExtension, ""); ok && owner == vdProgID {
		if prev, had := scope.getString(vdExtension, fdsecPreviousProgIDValue); had && prev != "" {
			if err := scope.setString(vdExtension, "", prev); err != nil {
				return err
			}
			fmt.Printf("%s handed back to %s.\n", vdExtension, prev)
		} else {
			scope.deleteValue(vdExtension, "")
		}
		scope.deleteValue(vdExtension, fdsecPreviousProgIDValue)
		removed++
	}
	// What is left of the extension goes only when nothing else is in it.
	scope.deleteIfEmpty(openWith)
	scope.deleteIfEmpty(vdExtension)
	if scope.keyExists(vdProgID) {
		if err := scope.deleteTree(vdProgID); err != nil {
			return err
		}
		removed++
	}

	fdsecNotifyAssociationsChanged()
	if allUsers {
		defer vdWarnPerUserCopy()
	}
	if removed == 0 {
		fmt.Printf("Nothing to remove for %s - the disk container type is not registered there.\n", fdsecScopeWord(allUsers))
		return nil
	}
	vdLogf("unregister %s: %s and %s removed", scope.name, vdExtension, vdProgID)
	fmt.Printf("Removed the %s disk container type for %s.\n", vdExtension, fdsecScopeWord(allUsers))
	if !allUsers {
		if _, ok := vdRegisteredIn(true); ok {
			fmt.Println("A machine-wide registration is still in place (it came with the installer).")
		}
	}
	return nil
}

// deleteIfEmpty removes a key that holds no value and no subkey - what is
// left of a shared key (an extension, its OpenWithProgids) once FileDO's own
// values are gone. A key another program still uses keeps its place.
func (s fdsecRegScope) deleteIfEmpty(sub string) {
	k, err := registry.OpenKey(s.hive, s.path(sub), registry.QUERY_VALUE)
	if err != nil {
		return
	}
	info, err := k.Stat()
	k.Close()
	if err != nil || info.ValueCount != 0 || info.SubKeyCount != 0 {
		return
	}
	_ = registry.DeleteKey(s.hive, s.path(sub))
}
