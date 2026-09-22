//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"filedo/fdsec"
)

// The Windows half of the Explorer registration. The shape it writes is
// documented once, in fdsec_register.go, because packaging\wix\FileDO.wxs
// writes the same shape for the MSI and the two must stay identical.
//
// Everything here goes through fdsecRegScope, never through a literal hive,
// so the tests can point the whole registration at a scratch key instead of
// the developer's own shell - the same seam discipline the reveal sandbox
// uses (FILEDO_FDSEC_REVEAL_ROOT). The seam moves WHERE it is written and
// nothing else: the key names, the values and the ownership checks are the
// ones a real registration uses.

// fdsecRegRootEnv points the registration at a scratch subkey of HKCU. Tests
// only; unset in every shipped run.
const fdsecRegRootEnv = "FILEDO_FDSEC_REG_ROOT"

// fdsecPreviousProgIDValue holds whatever owned .fd-sec before FileDO did, so
// that unregister gives it back rather than leaving the extension orphaned.
const fdsecPreviousProgIDValue = "FileDO.PreviousProgID"

// fdsecOwnerValue is the mark register leaves on every key it creates, and
// the only thing unregister accepts as proof that FileDO wrote them. The
// alternative - recognising our own command line - fails the moment the
// binary is renamed or a later version changes the verb, and failing that way
// means either refusing to clean up after ourselves or deleting a file type
// somebody else owns. A mark cannot be wrong in either direction.
const fdsecOwnerValue = "FileDO.RegisteredBy"

type fdsecRegScope struct {
	hive registry.Key
	base string
	name string
}

func fdsecRegScopeFor(allUsers bool) fdsecRegScope {
	if seam := os.Getenv(fdsecRegRootEnv); seam != "" {
		part := "user"
		if allUsers {
			part = "machine"
		}
		return fdsecRegScope{registry.CURRENT_USER, seam + `\` + part + `\Classes`, "HKCU(test)"}
	}
	if allUsers {
		return fdsecRegScope{registry.LOCAL_MACHINE, `Software\Classes`, "HKLM"}
	}
	return fdsecRegScope{registry.CURRENT_USER, `Software\Classes`, "HKCU"}
}

func (s fdsecRegScope) path(sub string) string {
	if sub == "" {
		return s.base
	}
	return s.base + `\` + sub
}

// setString creates the key if needed and sets one value. name "" is the
// key's default value, which is what a verb label and a command line are.
func (s fdsecRegScope) setString(sub, name, value string) error {
	k, _, err := registry.CreateKey(s.hive, s.path(sub), registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("%s\\%s: %w", s.name, s.path(sub), err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, value); err != nil {
		return fmt.Errorf("%s\\%s: %w", s.name, s.path(sub), err)
	}
	return nil
}

// setDWord is setString's numeric twin. Exactly one value in the whole
// registration needs it - CommandFlags, the separator above a menu item -
// and Explorer reads that one as a DWORD or not at all.
func (s fdsecRegScope) setDWord(sub, name string, value uint32) error {
	k, _, err := registry.CreateKey(s.hive, s.path(sub), registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("%s\\%s: %w", s.name, s.path(sub), err)
	}
	defer k.Close()
	if err := k.SetDWordValue(name, value); err != nil {
		return fmt.Errorf("%s\\%s: %w", s.name, s.path(sub), err)
	}
	return nil
}

func (s fdsecRegScope) getString(sub, name string) (string, bool) {
	k, err := registry.OpenKey(s.hive, s.path(sub), registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}

// keyExists asks whether the key is there at all. Unregister has to reason
// about the key rather than about its default value: a half-written
// registration, or a foreign one that labels its verb somewhere else, still
// has to be seen and refused rather than silently skipped.
func (s fdsecRegScope) keyExists(sub string) bool {
	k, err := registry.OpenKey(s.hive, s.path(sub), registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

func (s fdsecRegScope) deleteValue(sub, name string) {
	k, err := registry.OpenKey(s.hive, s.path(sub), registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(name)
}

// deleteTree removes a key and everything under it. registry.DeleteKey only
// removes a leaf, so the walk is ours to do; a missing key is success, never
// an error, because unregister has to be safe to run twice.
func (s fdsecRegScope) deleteTree(sub string) error {
	full := s.path(sub)
	k, err := registry.OpenKey(s.hive, full, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) || os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s\\%s: %w", s.name, full, err)
	}
	children, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return fmt.Errorf("%s\\%s: %w", s.name, full, err)
	}
	for _, child := range children {
		if err := s.deleteTree(sub + `\` + child); err != nil {
			return err
		}
	}
	if err := registry.DeleteKey(s.hive, full); err != nil &&
		!errors.Is(err, registry.ErrNotExist) && !os.IsNotExist(err) {
		return fmt.Errorf("%s\\%s: %w", s.name, full, err)
	}
	return nil
}

// fdsecProgIDShellOpen is the key whose presence answers "is FileDO
// registered in this scope" - the command, not the type, because an empty
// type key left by a half-finished removal must not count as a registration.
var fdsecProgIDShellOpen = fdsecProgID + `\shell\open\command`

func fdsecShellRegisteredIn(allUsers bool) (string, bool) {
	return fdsecRegScopeFor(allUsers).getString(fdsecProgIDShellOpen, "")
}

// fdsecShellRegister writes the three registrations of SP-0005 9.1.
//
// A per-user registration stands down when the machine already carries one:
// both would be merged into the same Explorer menu and the user would see
// "Secure with FileDO" twice, with no way to tell which copy an uninstall
// will take away.
func fdsecShellRegister(allUsers bool) error {
	exe, err := fdsecRegisteredExe()
	if err != nil {
		return fmt.Errorf("cannot resolve this executable's own path: %w", err)
	}
	if !allUsers {
		if cmd, ok := fdsecShellRegisteredIn(true); ok {
			fmt.Println("Already registered for all users on this machine - nothing written.")
			fmt.Println("  " + cmd)
			fmt.Println("A second, per-user copy would show every entry twice. Remove the")
			fmt.Println("machine-wide one first (uninstall FileDO, or run an elevated")
			fmt.Println("`filedo fdsec unregister -all-users`) if you want a per-user copy.")
			return nil
		}
	}

	scope := fdsecRegScopeFor(allUsers)
	if allUsers {
		if err := fdsecRequireWritableScope(scope); err != nil {
			return err
		}
	}
	icon := fdsecRegisteredIcon(exe)

	// The extension first, remembering whoever owned it, so unregister can
	// hand it back instead of leaving a file type nobody opens.
	if prev, ok := scope.getString(fdsecExtension, ""); ok && prev != "" && prev != fdsecProgID {
		if err := scope.setString(fdsecExtension, fdsecPreviousProgIDValue, prev); err != nil {
			return err
		}
	}
	group := `*\shell\` + fdsecMenuGroupKey
	steps := []struct{ sub, name, value string }{
		{fdsecExtension, "", fdsecProgID},
		{fdsecProgID, "", fdsecTypeLabel},
		{fdsecProgID, "FriendlyTypeName", fdsecTypeLabel},
		{fdsecProgID, fdsecOwnerValue, exe},
		{fdsecProgID + `\DefaultIcon`, "", icon},
		{fdsecProgID + `\shell`, "", "open"},
		{fdsecProgID + `\shell\open`, "", fdsecOpenVerbLabel},
		{fdsecProgID + `\shell\open`, "Icon", icon},
		{fdsecProgIDShellOpen, "", fdsecOpenCommand(exe)},

		// The cascading group. SubCommands set to an EMPTY string is what
		// turns this key into a sub-menu whose entries are the `shell`
		// subkey below it - the one registry shape that produces a cascade
		// without a shell extension of any kind. A non-empty SubCommands
		// would instead be a list of canonical verb names, which only works
		// for verbs registered machine-wide under their own key, so it is
		// not an option for a per-user registration.
		//
		// The group's own (Default) value is deliberately NOT written. With
		// SubCommands present a (Default) on the group makes the shell build
		// the sub-menu empty - the arrow is drawn and nothing opens, in
		// Explorer's classic menu and in every file manager that asks the shell
		// for the menu. MUIVerb alone carries the label.
		{group, "MUIVerb", fdsecMenuGroupLabel},
		{group, "Icon", icon},
		{group, "SubCommands", ""},
		{group, fdsecOwnerValue, exe},
	}
	for _, it := range fdsecMenuItems {
		sub := group + `\shell\` + it.key
		steps = append(steps,
			struct{ sub, name, value string }{sub, "", it.label},
			struct{ sub, name, value string }{sub, "MUIVerb", it.label},
			struct{ sub, name, value string }{sub, "Icon", icon},
			// One invocation per selected file, which is Q10's "a multi-file
			// selection packs one container per file" said in the only place
			// the shell reads it.
			struct{ sub, name, value string }{sub, "MultiSelectModel", "Player"},
			struct{ sub, name, value string }{sub + `\command`, "", fdsecMenuCommand(exe, it)},
		)
	}
	for _, st := range steps {
		if err := scope.setString(st.sub, st.name, st.value); err != nil {
			return fmt.Errorf("registration stopped partway: %w\nrun `filedo fdsec unregister%s` to clear what was written", err,
				map[bool]string{true: " -all-users", false: ""}[allUsers])
		}
	}
	// ECF_SEPARATORBEFORE. It is written after the strings rather than among
	// them so the one numeric value in the registration does not need a
	// second field in the table above.
	const ecfSeparatorBefore = 0x20
	for _, it := range fdsecMenuItems {
		if !it.separator {
			continue
		}
		if err := scope.setDWord(group+`\shell\`+it.key, "CommandFlags", ecfSeparatorBefore); err != nil {
			return err
		}
	}

	// An earlier build wrote a (Default) on the group, which is what made the
	// sub-menu open empty. Writing the new shape over the old one does not
	// remove it, so a re-register takes it away.
	scope.deleteValue(group, "")

	// The first shape's single verb, and the document type's second verb,
	// are both retired. A machine that still carries them would show the old
	// entry beside the new group, so a re-register takes them away - but
	// only when FileDO's own mark is on them.
	fdsecRemoveRetiredKeys(scope)

	fdsecNotifyAssociationsChanged()
	fdsecRegisterReport(allUsers, exe)
	return nil
}

// fdsecShellUnregister removes exactly what fdsecShellRegister wrote, and
// only if FileDO still owns it. A document type that now points at another
// program belongs to that program: taking it away would break an association
// this command never made.
func fdsecShellUnregister(allUsers bool) error {
	scope := fdsecRegScopeFor(allUsers)
	if allUsers {
		if err := fdsecRequireWritableScope(scope); err != nil {
			return err
		}
	}
	removed := 0

	if scope.keyExists(fdsecProgID) {
		if err := scope.requireOurs(fdsecProgID, fdsecProgIDShellOpen); err != nil {
			return err
		}
	}
	// The extension goes back to its previous owner when there was one.
	if owner, ok := scope.getString(fdsecExtension, ""); ok && owner == fdsecProgID {
		if prev, had := scope.getString(fdsecExtension, fdsecPreviousProgIDValue); had && prev != "" {
			if err := scope.setString(fdsecExtension, "", prev); err != nil {
				return err
			}
			scope.deleteValue(fdsecExtension, fdsecPreviousProgIDValue)
			fmt.Printf("%s handed back to %s.\n", fdsecExtension, prev)
		} else if err := scope.deleteTree(fdsecExtension); err != nil {
			return err
		}
		removed++
	}
	if scope.keyExists(fdsecProgID) {
		if err := scope.deleteTree(fdsecProgID); err != nil {
			return err
		}
		removed++
	}
	group := `*\shell\` + fdsecMenuGroupKey
	if scope.keyExists(group) {
		if err := scope.requireOurs(group, group+`\shell\`+fdsecMenuItems[0].key+`\command`); err != nil {
			return err
		}
		if err := scope.deleteTree(group); err != nil {
			return err
		}
		removed++
	}
	// The retired single verb, for a machine that was registered by an
	// earlier version and never re-registered since.
	legacy := `*\shell\` + fdsecSecureVerbKey
	if scope.keyExists(legacy) {
		if err := scope.requireOurs(legacy, legacy+`\command`); err != nil {
			return err
		}
		if err := scope.deleteTree(legacy); err != nil {
			return err
		}
		removed++
	}

	fdsecNotifyAssociationsChanged()
	if removed == 0 {
		fmt.Printf("Nothing to remove for %s - FileDO is not registered there.\n", fdsecScopeWord(allUsers))
		return nil
	}
	fmt.Printf("Removed the FileDO Explorer entries for %s.\n", fdsecScopeWord(allUsers))
	if !allUsers {
		if _, ok := fdsecShellRegisteredIn(true); ok {
			fmt.Println("A machine-wide registration is still in place (it came with the installer).")
		}
	}
	return nil
}

// fdsecRemoveRetiredKeys takes away the two entries the first shape wrote and
// this one no longer does: the single "Secure with FileDO" verb on all files,
// and "Unsecure here" on the document type. Both are now inside the group.
//
// It is called from register rather than from unregister because that is when
// the damage would show: an upgrade that only added the group would leave the
// old entry sitting next to it, saying the same thing in fewer words. A key
// without FileDO's mark is left alone, exactly as unregister leaves it.
func fdsecRemoveRetiredKeys(scope fdsecRegScope) {
	legacy := `*\shell\` + fdsecSecureVerbKey
	if scope.keyExists(legacy) {
		if owner, ok := scope.getString(legacy, fdsecOwnerValue); ok && owner != "" {
			if err := scope.deleteTree(legacy); err == nil {
				fmt.Printf("Removed the retired \"Secure with FileDO\" entry - it is now inside the group.\n")
			}
		}
	}
	retiredVerb := fdsecProgID + `\shell\` + fdsecUnsecureVerbKey
	if scope.keyExists(retiredVerb) {
		if err := scope.deleteTree(retiredVerb); err == nil {
			fmt.Printf("Removed the retired \"Unsecure here\" entry on %s - it is now inside the group.\n", fdsecExtension)
		}
	}
}

// requireOurs passes only when the key carries the mark register leaves, and
// otherwise reports who does hold it - quoting the command line, because that
// is the sentence a user needs to decide whether to overrule this by hand.
func (s fdsecRegScope) requireOurs(key, commandKey string) error {
	if owner, ok := s.getString(key, fdsecOwnerValue); ok && owner != "" {
		return nil
	}
	held, _ := s.getString(commandKey, "")
	if held == "" {
		held = "(no command)"
	}
	return fmt.Errorf("%s\\%s carries no FileDO registration mark and runs %q - refusing to remove an entry FileDO did not write",
		s.name, s.path(key), held)
}

// fdsecRequireWritableScope turns the raw access-denied into the sentence
// that says what to do about it. -all-users writes under HKLM, and an
// elevation refusal is the expected outcome of running it from an ordinary
// console, not a defect.
func fdsecRequireWritableScope(scope fdsecRegScope) error {
	k, _, err := registry.CreateKey(scope.hive, scope.path(""), registry.CREATE_SUB_KEY)
	if err == nil {
		k.Close()
		return nil
	}
	return fmt.Errorf("%w: writing the machine-wide registration needs an elevated console (%s\\%s: %v)\nwithout -all-users the same entries are written for this user only",
		fdsec.ErrUnsupported, scope.name, scope.path(""), err)
}

// fdsecNotifyAssociationsChanged tells the running Explorer that file
// associations moved. Without it the new entries appear only after a sign-out,
// and the user reasonably concludes the command did nothing.
func fdsecNotifyAssociationsChanged() {
	const (
		shcneAssocChanged = 0x08000000
		shcnfIDList       = 0x0000
	)
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")
	if err := proc.Find(); err != nil {
		return
	}
	_, _, _ = proc.Call(uintptr(shcneAssocChanged), uintptr(shcnfIDList), 0, 0)
}
