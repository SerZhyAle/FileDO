package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The Explorer registration (spec SP-0005 section 9), and the contract that
// two mechanisms have to agree on.
//
// Two things are registered and nothing else:
//
//  1. one cascading group on ALL files - "File DO..", whose sub-menu carries
//     the whole working set (secure and its three dispositions, unsecure and
//     its delete and start dispositions, the plain wipe, check and info);
//  2. a document type for .fd-sec, with its own icon, whose open action is
//     exactly the group's "Unsecure and start" entry.
//
// The document type carries NO second verb (owner's decision, 2026-09-20).
// Everything a container needs is in the group, which is on every file and so
// on containers too; the extension's own job is the double-click, and a verb
// that only exists for one extension is a second way to say what the group
// already says.
//
// The open verb answered a double-click with the GUI's reveal page until
// 2026-09-22 (SP-0005 9.4): a whole window - a target card, a parameters card,
// a plan card - opened waiting to be filled in, which is not what
// "double-click, type the password" promises. `unsecure start` already keeps
// that promise on the console: one password prompt, then the restored file is
// handed straight to its registered program. The owner's decision that day was
// to give the double-click exactly that command instead of a second,
// GUI-shaped way to say the same thing.
//
// The group replaced the single "Secure with FileDO" entry of the first
// shape. `unregister` still removes that old key, so a machine that carries
// it from an earlier version is cleaned up rather than left with both menus.
//
// Two mechanisms write this exact shape and they must not drift:
//
//   - this command, for the channels where no installer runs - the portable
//     zip, winget (a `zip`+`portable` package), and `go install`;
//   - packaging/wix/FileDO.wxs, for the MSI, declaratively, so that Windows
//     itself repairs the keys and removes them on uninstall.
//
// The MSI writes the machine-wide copy under HKLM; this command writes the
// per-user copy under HKCU and STANDS DOWN when the machine-wide copy is
// present, because the same verb appearing twice in one menu is exactly the
// failure SP-0005 9.3 says must be decided. HKLM wins: it is the copy an
// uninstall can take back.
//
// Nothing here is written by installing the portable build or by running
// FileDO. Registration is an explicit act (SP-0005 9.2) - `fdsec register`,
// or the "Explorer integration" feature of the MSI, which the user can
// deselect.
const (
	// fdsecProgID is the document type. It is an anchor in the same sense as
	// the winget identifier: change it and every previously registered
	// machine keeps a dangling type that no uninstall will ever find.
	fdsecProgID = "FileDO.SecureContainer"
	// fdsecExtension is the registered extension - the one FDSEC-FORMAT.md
	// names and the one PackFile writes.
	fdsecExtension = ".fd-sec"

	// fdsecMenuGroupKey is the key name of the cascading group on all files,
	// and fdsecMenuGroupLabel is what the user reads in the menu.
	fdsecMenuGroupKey   = "FileDO"
	fdsecMenuGroupLabel = "File DO.."

	// fdsecSecureVerbKey is the key name of the single all-files verb the
	// first shape registered. It is kept for one reason only: `unregister`
	// has to be able to take it back off a machine that still carries it.
	fdsecSecureVerbKey = "FileDO.Secure"
	// fdsecUnsecureVerbKey is the retired second verb on the document type,
	// kept for the same reason.
	fdsecUnsecureVerbKey = "unsecure"

	fdsecTypeLabel     = "FileDO secure container"
	fdsecOpenVerbLabel = "Unsecure and start with FileDO"
)

// fdsecPauseFlag is appended to every menu command. Explorer closes the
// console the moment the process ends, so without it "Info" and "Check" print
// their answer into a window that is already gone. It is stripped by
// extractGlobalFlags before any verb sees it, so it can never be mistaken for
// a bare password or an operation word.
const fdsecPauseFlag = "--pause"

// fdsecNoHistoryFlag keeps Explorer-launched runs from dropping a history.json
// into whatever folder the file happens to live in. Stripped like --pause.
const fdsecNoHistoryFlag = "--no-history"

// fdsecMenuItem is one line of the "File DO.." sub-menu.
//
// key is a registry key name and the order is what it buys: Explorer
// enumerates the group's `shell` subkey and the registry keeps subkeys sorted
// by name, so the leading number is the only thing that fixes the sequence
// the user sees. The names are never shown.
type fdsecMenuItem struct {
	key string
	// label is the menu text. English, and short enough to read in a
	// context menu at a glance.
	label string
	// args is everything after the executable, with "%1" where the shell
	// substitutes the clicked file. The pause flag is added by
	// fdsecMenuCommand, so no entry can forget it.
	args string
	// separator draws a line above the item. It groups the three families -
	// pack, unpack, and the operations that have nothing to do with
	// containers - so a destructive entry never sits flush against the one
	// above it.
	separator bool
}

// fdsecMenuItems is the sub-menu, in the order it is drawn.
//
// The option set is a fan of menu entries here, and that is a reversal: the
// first shape kept one single-purpose verb per family and left del/wipe/
// rename to a dialog. The owner asked for the options to be in the menu
// (2026-09-20), because the dialog that was supposed to carry them belongs to
// a GUI stage that has not shipped, and a menu entry that then asks a
// question in a console window is not the same thing as choosing the
// operation before anything happens.
//
// Every destructive entry still prompts in the console it opens: the menu
// chooses the operation, it does not skip the confirmation (invariant 9).
var fdsecMenuItems = []fdsecMenuItem{
	{key: "10Secure", label: "Secure", args: `"%1" secure`},
	{key: "20SecureDel", label: "Secure and delete original", args: `"%1" secure del`},
	{key: "30SecureWipe", label: "Secure and wipe original", args: `"%1" secure wipe`},
	{key: "40SecureRename", label: "Secure with a random name", args: `"%1" secure rename`},
	{key: "50Unsecure", label: "Unsecure", args: `"%1" unsecure`, separator: true},
	{key: "60UnsecureDel", label: "Unsecure and delete container", args: `"%1" unsecure del -y`},
	{key: "65UnsecureStart", label: "Unsecure and start", args: `"%1" unsecure start`},
	{key: "70Wipe", label: "Wipe this file", args: `file "%1" wipe`, separator: true},
	{key: "80Check", label: "Check this file", args: `check "%1"`},
	{key: "90Info", label: "Info", args: `file "%1" info`},
}

// fdsecRegisterUsage is one string because the register and unregister
// refusals must describe the same two flags.
const fdsecRegisterUsage = "fdsec register|unregister [-all-users]"

// handleFdsecRegister runs `fdsec register` and `fdsec unregister`. It owns
// its own flag parsing rather than borrowing parseFdsecArgs: that parser is
// about credentials and dispositions, and this command takes neither.
func handleFdsecRegister(sub string, args []string) error {
	allUsers := false
	for _, a := range args {
		switch strings.ToLower(a) {
		case "-all-users", "--all-users", "-allusers":
			allUsers = true
		default:
			return usagef("unknown option %q for fdsec %s: %s", a, sub, fdsecRegisterUsage)
		}
	}
	if sub == "unregister" {
		return fdsecShellUnregister(allUsers)
	}
	return fdsecShellRegister(allUsers)
}

// fdsecRegisteredExe is the binary the registered menu commands point at:
// this executable, resolved to an absolute path, because a relative one in
// the registry means "whatever is in Explorer's working directory".
//
// The menu entries are the CLI's, and stay the CLI's: each one is a single
// operation chosen before anything happens, and a console is the honest place
// for the password it then asks for. The double-click stays on the CLI too -
// see fdsecOpenCommand.
func fdsecRegisteredExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(exe), nil
}

// fdsecRegisteredIcon picks the icon the document type shows, in order:
// FileDO.ico next to the exe (what the MSI installs and the release zip
// carries), then the GUI's own icon, then the CLI's. The last one always
// exists, so this never fails.
func fdsecRegisteredIcon(exe string) string {
	dir := filepath.Dir(exe)
	if ico := filepath.Join(dir, "FileDO.ico"); fileExistsPlain(ico) {
		return ico
	}
	if gui := filepath.Join(dir, "filedo_win.exe"); fileExistsPlain(gui) {
		return gui + ",0"
	}
	return exe + ",0"
}

func fileExistsPlain(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// fdsecMenuCommand builds one command line from the exe and a menu item, so
// the quoting is identical in every entry. "%1" is the shell's placeholder
// for the clicked file and must stay quoted: a path with a space in it is the
// common case, not the exotic one.
func fdsecMenuCommand(exe string, it fdsecMenuItem) string {
	return `"` + exe + `" ` + it.args + " " + fdsecPauseFlag + " " + fdsecNoHistoryFlag
}

// fdsecOpenCommand is what a double-click on a .fd-sec runs: the same command
// line as the "Unsecure and start" menu entry, built the same way so the two
// can never drift apart. One password prompt on the console, then the
// restored file is handed to its registered program - no sandbox, no window
// to fill in first.
func fdsecOpenCommand(exe string) string {
	return fdsecMenuCommand(exe, fdsecMenuItem{args: `"%1" unsecure start`})
}

// fdsecScopeWord names the scope in messages. The word matters: a user who
// ran the command without -all-users and then cannot see the entry from
// another account has to be told which of the two they got.
func fdsecScopeWord(allUsers bool) string {
	if allUsers {
		return "all users"
	}
	return "this user"
}

// fdsecRegisterReport prints what was written, naming every key shape. A
// registration the user cannot inspect is one they cannot undo by hand if
// this command ever fails halfway.
func fdsecRegisterReport(allUsers bool, exe string) {
	root := "HKCU"
	if allUsers {
		root = "HKLM"
	}
	fmt.Printf("Registered for %s:\n", fdsecScopeWord(allUsers))
	fmt.Printf("  %s  ->  %s\n", fdsecExtension, fdsecProgID)
	fmt.Printf("  %s\\Software\\Classes\\%s\\shell\\open\\command   (%s)\n    %s\n",
		root, fdsecProgID, fdsecOpenVerbLabel, fdsecOpenCommand(exe))
	fmt.Printf("  %s\\Software\\Classes\\*\\shell\\%s   (%s, %d entries)\n",
		root, fdsecMenuGroupKey, fdsecMenuGroupLabel, len(fdsecMenuItems))
	for _, it := range fdsecMenuItems {
		fmt.Printf("    %-30s %s\n", it.label, fdsecMenuCommand(exe, it))
	}
	fmt.Println("On Windows 11 the group is under \"Show more options\".")
	fmt.Println("Remove it with: filedo fdsec unregister" + map[bool]string{true: " -all-users", false: ""}[allUsers])
}
