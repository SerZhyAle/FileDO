# Repository Guidelines

## Shared rules (canon)
FileDO follows the **SZA Unified Rules**, consumed by **reference** through the `sza` Claude Code plugin
(`github.com/SerZhyAle/sza-unified-rules`). Start at `rules/INVARIANTS.md`, then Overlay C. Adoption is
stamped in `.sza-canon.json`; this repo's per-project record - overlay facts, channel rows and every
recorded divergence - is `rules/contrib/filedo.md` in the canon repo.

**The canon owns the universal rules and this file does not repeat them**: evidence over confidence, when
to commit and push, the co-author trailer, English artifacts and Russian chat, the build/release wall,
mechanical versioning, the changelog shape, the house text style, secrets, and Bash/tooling safety. Only
FileDO-specific deltas live below. Rule fixes go back to the canon in their own session, never from here.

The plugin also **ships the enforcement hooks** described in canon `GITHUB_INTERACTION.md` section 6 and
`AI_USAGE.md` section 5 - the `find` guard, the `.ps1`- and cmdlet-in-Bash command-head guards, the
missing-interpreter and slash-argument checks, and the fire-and-forget guard. This repo registers **no
hooks of its own** (`.claude/settings.json` carries a permission entry only), so there is nothing here to
disarm and nothing to duplicate. Do not hand-wire a local copy of a canon hook.

**Recorded divergences** (canon `AI_USAGE.md`):
- The only agent-rules file with content is **`AGENTS.md`**; `CLAUDE.md` exists but is a bare pointer to
  this file and must never grow guidance of its own.
- The in-repo skills under `.claude/skills/{build,release}/` are **git-ignored** (`.gitignore` line 76
  ignores all of `.claude/`) - local-only, never team-shared through git. An edit to a skill therefore
  cannot be committed; say so rather than reporting it as landed.

## Project shape (overlay facts)
Windows-first Go **CLI** (storage speed test, fake-capacity/counterfeit-flash detection, secure wipe,
fill, duplicate management) plus a co-shipped **VB.NET GUI** (`filedo_win_vb/`, `filedo_win.exe`) - one
release, one tag, a companion binary, *not* a separate edition. Distributed on the desktop channels
(GitHub Release + winget + Microsoft Store) plus a direct-download setup EXE (and its bare MSI).

- **Source root.** Four `cmd/<binary>/` mains (`filedo`, `filedo-check`, `filedo-fill`, `filedo-test`) +
  shared top-level packages `fileduplicates/`, `helpers/`, `fsx/`, `statedir/`. **Multi-module**: 4
  separate `go.mod`/`go.sum`, mixed `go` language lines (1.25.0 / 1.21) but **one toolchain**: the root
  `go.mod`'s `toolchain go1.26.1` is what every channel builds with - `release.yml` installs it,
  `build.ps1` and `msix/build-msix.ps1` refuse any other local Go. Module path `filedo` (a frozen anchor).
- **Version shape.** Separator-less `yyMMddHHmm` (e.g. `2606120121`) - a sortable 10-digit integer. Git
  tag `v<stamp>`, validated by `release.ps1` as a real date newer than every `v\d{10}` tag before it
  prefixes the `v`, and stamped into the binaries via `-ldflags "-X main.version="`. Remapped
  mechanically for the PE `VS_VERSIONINFO` (`yy.M.d.HHmm`), the MSI and setup EXE
  (`yy.M.<minute of the month>` - Windows Installer compares three fields) and MSIX only; the table is
  in [`RELEASE.md`](RELEASE.md) "Version scheme".
- **Release-mechanics** are top-level channel siblings (no `publishing/` umbrella): `winget/`, `msix/`,
  `packaging/wix/`, committed `exe_to_download/`. `winget/` holds the three manifests and **nothing else**:
  `winget validate` is pointed at the directory and parses every file in it as YAML, which is why the
  manifest checker lives in `packaging/` (below) and not beside what it checks.
- **Frozen anchors** (reserve once; changing orphans installs): winget `PackageIdentifier
  SerZhyAle.FileDO`; WiX MSI `UpgradeCode 4d6b3b1f-7c8e-4a25-9f1d-8e3b2c5a7d91` (+ `HKLM\Software\FileDO`);
  the MSIX Identity `Name` **`SZA.FileDO`** and `Publisher` **`CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD`**
  (Store ID `9PH1LPCMRG83`, family `SZA.FileDO_fdk7e19xt9z9j`; reserved in Partner Center and recorded in
  `msix/identity.json`, live in the Store since 2026-09-23); Go module `filedo`; the 5 winget `PortableCommandAlias` names; the
  document type id **`FileDO.SecureContainer`** and the fixed component GUIDs in `FileDO.wxs` (an MSI
  component keeps its GUID for the product's life, or an upgrade leaves the old copy installed); the
  **bundle `UpgradeCode 033d2348-b26c-477f-8885-c68d9cfff47f`** in `FileDO-bundle.wxs` - a separate
  identity from the MSI's, and what lets a setup EXE replace the previously installed one instead of
  adding a second entry to Apps and features.

## What the installer does (and who else writes the same keys)
FileDO's core is a CLI, but what it *installs* is a Windows program: a window opened from a shortcut, a
document type, and two Explorer verbs. The MSI (`packaging/wix/FileDO.wxs`) is where that happens, and it
is built by a plain `.\build.ps1` locally and by the release workflow on the runner - one wxs, one shape.

**Two artifacts, one package.** `FileDO-<version>-setup.exe` is a WiX Burn bundle
(`packaging/wix/FileDO-bundle.wxs`) that carries `FileDO-<version>-windows-x64.msi` inside it; both ship on
every release and the EXE is the one a person downloads. The bundle defines **no install behaviour of its
own** - its UI is the MSI's feature tree (`bal:WixInternalUIBootstrapperApplication` shows the primary
MSI's own UI and draws no wizard of its own), so a change to what is installed is a change to the wxs
and never to the bundle. It is the one FileDO entry in Apps and features, and `DisableModify="button"`
makes that entry's Uninstall/Change run the bundle in maintenance mode, where the same bootstrapper opens
the MSI's maintenance page - Change is the feature tree (SP-0030 PKG-02; the standard bootstrapper it
replaced planned nothing for an installed MSI, which is why Modify used to be disabled). `.\build.ps1 -Install` builds both and then runs the EXE on this machine - the
only switch in either script that changes the machine it runs on, which is why it is a switch and not the
default that building them is.

**What the wizard says lives in two files, not in the wxs.** `packaging/wix/FileDO.en-us.wxl` replaces
WixUI's stock sentences (which describe nothing) with text that says what FileDO is and what the install
does; `packaging/wix/banner.bmp` and `dialog.bmp` are its artwork, regenerated from `assets/icon.png` by
`packaging/wix/make-art.ps1` and committed. Both the local build and the release workflow must pass
`-loc` and the two `-d ...Bmp` defines - a build that forgets them still succeeds and quietly ships the
stock blue dialogs. A wrong string id in the wxl is silent in the same way: the stock sentence simply
stays, so a change there is checked by reading the built MSI's `Control` table, never by trusting the
build to fail. The feature **descriptions** are in the wxs, because the Customize page shows the
description of whatever the user clicks - that is where "what is this and why would I want it" is
answered.

Three features, and the user chooses: **Main** (binaries, `PATH`, Start menu entry - `AllowAbsent="no"`),
**ExplorerIntegration**, **DesktopShortcut**. The checkboxes come from `WixUI_FeatureTree`, so there is no
hand-written dialog to maintain; silent installs pick with `ADDLOCAL`. `MigrateFeatures="yes"` is what
keeps a user's choice across an upgrade, and `ARPNOMODIFY`/`ARPNOREPAIR` are deliberately *gone* - Change
and Repair are the supported way to turn the integration on and off later.

**The registry shape has two writers and they must not drift**: `packaging/wix/FileDO.wxs` (HKLM, for the
MSI) and `cmd/filedo/fdsec_register*.go` (`filedo fdsec register`, HKCU or HKLM, for the channels where no
installer runs - the portable zip, winget's `zip`+`portable` package, `go install`). Key names, labels,
command lines and the `FileDO.RegisteredBy` mark are one contract; change one file and the other changes in
the same commit. `TestRegistryParity_TheMSIAndFdsecRegisterWriteTheSameShellIntegration`
(`cmd/filedo/registry_parity_test.go`) holds them together value for value: it parses the wxs's
`ShellIntegration` group, runs the real `fdsec register -all-users` into a scratch key, and compares every
key, value name, type and data in both directions. Two things are registered and nothing else (SP-0005 9.1, as amended 2026-09-20): the
cascading `File DO..` group on all files - `*\shell\FileDO`, `SubCommands` an **empty** string, entries in
the `shell` subkey named `10Secure` .. `90Info` so the digits fix the order - and the `.fd-sec` document
type with its icon, whose open verb is exactly the group's `65UnsecureStart` entry. The document type
carries **no** verb of its own: everything a container needs is in the group, which is on every file. Every
entry ends in `--pause`, or Explorer closes the console before its output can be read. `register` also
deletes the retired `FileDO.Secure` verb and the retired `Unsecure here` verb when FileDO's mark is on
them, so an upgrade never shows both shapes.

Four rules that are not style:
- **HKLM wins.** A per-user `fdsec register` stands down when the machine-wide copy is there, because both
  merge into one Explorer menu and the user would see every entry twice.
- **Removal is by mark, never by guessing.** `unregister` deletes only keys carrying `FileDO.RegisteredBy`
  and hands `.fd-sec` back to whoever owned it before. Recognising our own command line instead looks
  equivalent and is not: rename the exe and the tool either refuses to clean up after itself or deletes a
  file type somebody else owns.
- **Every menu verb, including the double-click, points at `filedo.exe`.** Each one is a single operation
  chosen before anything happens, so a console is the honest place for the password it then asks for. The
  `.fd-sec` open verb pointed at `filedo_win.exe` from SP-0005 S6 (spec 9.4) until 2026-09-22: a
  double-click opened the GUI's reveal page - a target card, a parameters card, a plan card - waiting to be
  filled in, which is not what "double-click, type the password" promises. It now runs the identical
  command line `fdsecMenuCommand` builds for `65UnsecureStart` (`fdsecOpenCommand` in fdsec_register.go),
  so the two can never drift apart on their own. Both writers still move together.
- **`DefaultIcon` needs a file on disk** - `FileDO.ico` beside the exe, staged from `assets/icon.ico` by
  both the workflow and `build.ps1`. The icon embedded in the MSI is gone once the installer is.

**The Store (MSIX) build carries the group as a packaged Explorer command, not as registry keys** (SP-0020,
built 2026-09-25, **not released**). `shellext/FileDOShell.cpp` is the product's **third codebase**: a native C++
`IExplorerCommand`, one COM class, static CRT, importing `kernel32` and `ole32` only (`shellext/build-shellext.ps1`
fails the build on any other import), declared in `msix/AppxManifest.xml` through
`desktop4:FileExplorerContextMenus` on `Type="*"` and hosted by a COM surrogate (`com:SurrogateServer`), so it runs
in a `dllhost.exe` and a fault in it never takes Explorer down. It is the only way to the Windows 11 first-level
menu. Four rules that are not style:
- **It does nothing but hand a path to `filedo.exe`**, the one beside it in the package - no file I/O, no state,
  `GetState` always enabled. It is called on every right-click.
- **Its menu is `fdsecMenuItems`, entry for entry.** The table between `BEGIN/END MENU TABLE` in the .cpp is read
  by `TestShellExtMenuMatchesClassic` (`cmd/filedo/shellext_contract_test.go`): key, label, arguments, separators,
  the `--pause --no-history` suffix and the group label. That makes **three writers** of the one menu - the wxs,
  `fdsec_register*.go`, and the .cpp - and all three change in the same commit.
- **The CLSID `7B369FE8-B2BC-41EA-AA71-5427ACAD6E8E` is an anchor** and lives in three places (the verb, the COM
  class, the .cpp); `TestShellExtClsidAgrees` and `build-msix.ps1`'s packed-manifest check both hold them together.
- **The classic copy yields to the package** (SP-0020 D3): a per-user `fdsec register` stands down when
  `GetPackagesByPackageFamily` finds either FileDO family (`fdsecPackagedMenuFamilies` - the Store PFN and the
  local-test one, both derived from their identities by `TestPackagedMenuFamiliesMatchTheIdentities`);
  `-all-users` still writes, because the package is per-user, and says this account sees both. Tests pin the
  answer with `FILEDO_FDSEC_PACKAGED_MENU=0|1`, because a sideloaded test package on the developer's machine
  would otherwise make every registration test stand down.

Only the MSIX ships the DLL (SP-0020 D2). The MSI, winget and the zip keep the classic registration, which on
Windows 11 lives under "Show more options" - say that plainly rather than implying parity. Building the MSIX
therefore needs the Visual Studio C++ x64 tools (`Microsoft.VisualStudio.Component.VC.Tools.x86.x64`).

## Code map (`cmd/filedo`)
The main CLI has **two dispatch layers**, and they are the thing that costs an hour to rediscover.

1. **Verb dispatch** - `dispatchLine` (`dispatch.go`) is the **one** verb switch, and both entry points
   call it: `main()` with the command line, and the `from`/`batch` path (`batch.go`) with each line of a
   `.lst` through the thin `executeInternalCommand`. `verbOf` maps an alias of the `list_of_flags_for_*`
   slices (`copy`/`cp`, `cd`/`check-duplicates`, ..) to its canonical verb; when the first word is not a
   verb, `classifyTarget` decides `device`/`folder`/`file`/`network` (only an ASCII letter with an
   optional `:`, `:\` or `:/` is a device; `\\` and `//` are shares; `\\?\X:\..` and `\\.\X:` name the
   local path; `.` is a folder). A new verb is **two edits**: its alias slice (plus `list_of_flags_for_all`)
   and its entry in `verbOf` with a case in `dispatchLine`. Until SP-0024 (CLI-30) there were two copies of
   the switch and they drifted; there is nothing left to drift. A batch line is never run as an external
   program, a batch file that includes itself is refused, and no line starts after a stop.
2. **Target dispatch** - only the four target verbs go on to `runGenericCommand` (`command_handlers.go`),
   which picks `DeviceHandler`/`FolderHandler`/`NetworkHandler`/`FileHandler` behind the `CommandHandler`
   interface. Sub-operations there are matched by **positional string compare** on `cmd.Arg(1)` (`speed`,
   `fill`, `test`, `clean`, `cd`, ..); the `flag.FlagSet` defines no options and is parsed after a `--`, so
   a target starting with a dash is a target. `copy`/`compare`/`check` never reach this layer - they are
   two-path verbs of `dispatchLine`.

`effectiveTarget` (`sysdrive_windows.go`) is the single choke point for the system drive: `speed`/`fill`/
`test` on the root of the volume holding Windows - in any spelling, resolved by the filesystem, never by
comparing text - go through the confirming redirect to `%TEMP%\FileDO_Operations`, and `clean` there
cleans that folder. A folder below the root is the user's explicit choice and is not redirected.

**Shared mechanisms every verb uses** (SP-0023 themes; build on them, never beside them):
- **Stop** - `interrupt.go`. State is read through atomics, cleanups run outside the lock after the
  context is cancelled, and `AddCleanup` returns the function that unregisters it: a per-item
  registration is removed when its item completes. A graceful stop is observed by the operation itself
  (`runStopRequested()`, the context, `fdsec.WithStop`); interrupt cleanups that touch files act only on a
  forced exit (`IsForceExit()`), so they never race an operation that still owns the handle.
- **Path identity** - root package `fsx/` (`fsx.IdentityOf`, `SameFile`, `Resolve`, `Overlap`,
  `IsVolumeRoot`): final path, volume, volume serial + file ID. "Same file", "inside", "a root" and "the
  system drive" are decided here and never on strings.
- **Atomic writes** - `fsx.CreatePartial`/`Commit` and `fsx.RenameNoReplace`: `<name>.filedo-partial`,
  flushed, timed, renamed with **no replace**. Go's `os.Rename` on Windows replaces its target, so it is
  never the final step for user data.
- **Error classes** - `errclass_windows.go`: environmental errors (access denied, write-protected, not
  ready, disk full, sharing violation, network gone) are "could not verify"; only device-level I/O errors
  may become a defect (`ioJudgement`).
- **Runtime state** - root package `statedir/`: everything the CLI writes for itself (history.json, the
  copy skip list, check's lists and state, the hash cache) lives in `%LOCALAPPDATA%\FileDO\state\`, with a
  one-time import of a legacy copy from the current directory or beside the exe (copied, never moved).
  `FILEDO_STATE_DIR` moves it and is the tests' seam; the black-box harness points it at each test's
  working directory. history.json is written under a lock, atomically, only by a run that did something,
  and a file that does not parse is kept as `history.json.corrupt-<time>`.

**The verdict and the exit code have one site, and it is `outcome.go`.** A verb never writes an exit code
and never emits a `result`: it *records* - `beginRun` when it starts, `runStep` per phase, `runDefect`
when it judges its target and finds it wanting, `runFailure` when it could not judge at all, `runNumber`
for a measurement. `finishRun`, deferred once in `main()` ahead of the history flush, turns what was
recorded into exactly one `result` event and exactly one code: **0** passed or done, **1** ran and found a
defect, **2** could not be verified (CLI-EVENT-STREAM rule 11). The container verbs keep the separate
classes of `FDSEC-BEHAVIOUR` 7.1 - `finishRun` leaves `fdsecExitCode` alone and only maps it to a verdict,
which is the seam rule 12 bridges. Two consequences bind an edit here: an error that is a *judgement*
about the target must be wrapped with `defectf` or it reports as "could not judge", and a branch that
prints an error and returns must reach `reportOpError`/`reportRunError` or the run silently exits 0 -
which is exactly what every branch did before SP-0010.

**The fake-capacity engine exists once** (SP-0026 retired the `capacitytest/` near-copy, whose writer the
verifier could not read). The driver is `main_types.go`: interface `FakeCapacityTester`,
`runGenericFakeCapacityTest` and the `verify*` family; `DeviceTester` (`device_windows.go`),
`FolderTester` (`folder.go`) and `NetworkTester` (`network_windows.go`) implement it and differ only in how
they reach the target. The test-file format is `capacity_format.go`, the one writer and the unbuffered
read-back `capacity_io_windows.go`, the plan arithmetic `capacity_plan.go`, and `fill` / `fill verify` /
`clean` for all three target kinds `capacity_fill_windows.go`. Every tester and every fill writes through
the same writer, so the verifier always reads what it was written for. The companion `filedo_test.exe`
still carries an old copy (SP-0028 COMP-02).

Detection rests on three independent signals - keep all three: header == footer (`FILEDO_TEST2 <name>
<run nonce> <size> <ts>`); a body that names its file, its run and its offset - every 4 KiB block starts
`F<seq>_<nonce>_<block>` and the rest is xorshift output keyed by the same three - so a controller that
aliases block ranges, returns another address's data or an earlier run's leftovers is caught at any
offset, and a compressing or deduplicating target cannot shrink the body; and write speed staying within
0.1x..10x of the 3-file baseline. Every read-back opens with `FILE_FLAG_NO_BUFFERING` (`openForVerify`),
file 1 is re-verified after *every* write, and a final pass re-reads every file before a PASS. A speed
anomaly alone is never a verdict: it re-reads every file written so far, and only data that does not read
back - or a device-class I/O error from the shared classifier - is a defect; an environmental error
(access denied, write protection, a vanished device or share, a full disk) is "could not verify". On a
defect the test files are deliberately **kept, never cleaned up** - they are the evidence for the
estimated-real-capacity report - and a stop removes them whenever it comes. `clean` removes only files
with a FileDO name *and* FileDO content, lists them and asks (`--yes` skips the question); `fill verify`
still reads the old `FILEDO_TEST_<name>_<ts>` header for one release and names it as the weaker check.
The in-process tests stub the volume through `diskSpaceQuery` and `capacityVolumeFacts`; the black-box
ones cap the free space the capacity verbs believe in with `FILEDO_TEST_FREE_BYTES`, which can only ever
make a run write less.

**Secret files (`fdsec`) are a third shape.** The format core is the root package `fdsec/` (pack, unpack,
KDF, AEAD, the sealed metadata; `FDSEC-FORMAT.md`, in the shared contracts folder outside git, is its
on-disk contract - see "External contracts" below). The
command surface is `cmd/filedo/fdsec_cmd.go`, and it does **not** go through `runGenericCommand`: that
function probes the target with `os.Stat` first, and a mask target (`*.txt secure`) never passes the
probe. Instead `fdsecDispatchTarget` runs ahead of the probe, called from both `main()` and
`executeInternalCommand()` - one function, so the interactive and the batch paths cannot drift. The
verb-first `fdsec info|verify` command did take the four edits of layer 1. Two rules here are not style:
every credential-bearing surface passes through `redactCredentialArgs` (`fdsec_redact.go`) before it is
written or echoed, and nothing is removed before `PackFile`/`Unpack` has verified the bytes end to end.

**A folder is a target too** (SP-0009, FDSEC-FORMAT suite 3): `secure` on a folder packs the whole tree
into one container (`fdsec.ScanTree` + `PackTreeFile`), and `unsecure` restores it through
`cmd/filedo/fdsec_tree.go`. `fdsec.Open` is the one key derivation that learns which suite a container
holds; `Unpack` refuses a folder container with `ErrTreeContainer`, which is how `reveal` and `unsecure
start` refuse it. Three rules there are not style: the walk refuses a reparse point anywhere in the tree
(the caller passes `hasReparsePoint`, so there is one classification); a restore goes into a fresh
`.fdsec-restore-*` folder beside the destination and is renamed into place only when every file verified,
never merged into an existing folder; and `del`/`wipe` of the original tree re-walk it first and touch
nothing if it changed since the pack, then remove entry by entry from the manifest - never a recursive
delete.

**Suite 2, the quiet suite** (SP-0019, FDSEC-FORMAT section 18), is the third shape and the only one
chosen by an option: `secure suite2` writes it, the default stays suite 1 (`fdsec/suite2.go`,
`PackFileSuite2`). It has no head at all - salt, nonce, then XChaCha20-Poly1305 frames of 64 KiB under a
pepper-folded Argon2id key - so `fdsec.Open` finds it by opening in a fixed order: the suite-1/3 head
first, suite 2's try-list only when that head does not authenticate. Three rules there are not style: a
refusal from an authenticated suite-1 head (damaged, unsupported) never falls through to suite 2; the
pepper and every pinned try-list entry are frozen for the life of every file written under them
(`TestVectors_Suite2` fails if either drifts, and a new profile is appended, never swapped in); and
`ScreenLength` must accept any length a suite-2 file can have, because suite 2 has no cluster rule. The
tests lower the try-list through the `suite2Profiles` seam, the same kind as `activeProfile`.

A container carries **no marker of any kind** - no magic, no version in the clear, nothing a scanner can
read (`FDSEC-FORMAT.md` sections 4, 13.4). Three consequences bind every edit here. The password is
needed before anything can be read, so `fdsec info` takes one like every other verb. Screening a file
without the password can only ever be the length test of `fdsec.ScreenLength`, never a look at its bytes.
And "wrong password", "not a container" and "damaged head" are one outcome: a message that names only one
of the three is a bug, not a wording choice. The KDF work factor lives in the format version, not in the
file, for the same reason - the mask has to be derived before the head can be read; `fdsec`'s tests lower
it through the `activeProfile` seam, and `TestVectors_Suite1` puts the real one back. A wrong password
therefore pays suite 1's derivation and suite 2's - about 0.3 s on the owner's machine, 1.2 s on one core
(the SP-0019 D3 measurement: `FDSEC_MEASURE_SUITE2=1 go test ./fdsec -run TestMeasure_Suite2Profile -v`).

**The reveal (`cmd/filedo/fdsec_reveal*.go`) is where the plaintext lives, so it has rules of its own.**
It copies the original into `%LOCALAPPDATA%\FileDO\reveal\<random>\`, a directory created with an access
list naming this user and SYSTEM only and inheritance off - the repository's only security-descriptor
code. Three things there are load-bearing and each has a test that fails loudly if it is undone:
- **A sweep runs at the top of `main()`, before argument dispatch**, and removes leftover sandboxes; a
  live reveal holds `<sandbox>.lock` open exclusively and is stepped around. The lock is taken *before*
  the sandbox directory exists, or a concurrently starting FileDO would delete a reveal about to be used.
- **The sealed true name is never trusted and never logged.** It is refused if it carries a path
  separator, a colon, a wildcard, a control character, a reserved device name, or a trailing dot or space
  - that last one because Windows drops it, so `invoice.exe ` would land as `invoice.exe` while the
  extension check saw something that is on no list. Refused, never sanitised. And the name goes to the
  screen only: `history.json` records the sandbox directory, because the name is the thing the format
  exists to hide.
- **Executable and script types are extracted and never handed to the shell** (safety invariant 7), and a
  reveal takes exactly one container - never a mask, because a batch of plaintext copies is the shape of
  malware staging.

**`unsecure start` borrows that same sandbox** (SP-0008, 2026-09-22) - the "Unsecure and start" menu entry
and the `.fd-sec` double-click. It restores into `%LOCALAPPDATA%\FileDO\reveal\us-<random>\` instead of
beside the container, screens the sealed name through the same `fdsecSandboxName`, logs the directory and
never the name, and makes **one** best-effort removal when the `--pause` wait ends (`fdsec_start.go`, and
`addAfterConsoleHold` in `main.go`; with no console to hold, the attempt follows the launch call). Four
things there are load-bearing:
- **Two prefixes now live under that root and the startup sweep knows both**: `rv-` is reveal's read-only,
  untrusted-marked, never-executable copy; `us-` is the user's own file with none of those protections, so
  a leftover says which contract it was made under without being opened.
- **The removal is allowed to fail.** A handler still holding the file refuses the unlink, the failure goes
  to stderr, the lock is released anyway, and the next start's sweep is what reclaims it. There is no
  polling loop and no prompt - that is the trade the owner chose over reveal's three mechanisms.
- **A named destination keeps the old behaviour.** `here` and `to` are the user asking for a permanent file
  where they said, so those restore exactly as before; only the default location moved.
- **`start del` is refused** as a usage error: a copy the end of the run takes away plus a deleted container
  is neither, and the refusal names `to <dest> start del`, which is the form that keeps the file.

**The reveal's second lifetime mechanism has two shapes, and which one exists depends on the caller.** A
console gets the Enter prompt. A caller that passed `--stop-file` - the GUI always does - gets the same
mechanism over a machine channel: the page's "remove the copy now" button writes the stop file, the global
interrupt handler notices it, and the caller's cleanup removes the sandbox. `machineStopChannel` in
`main.go` is what tells a reveal that such a window exists, and it matters more than it looks: without it
`fdsecAwaitDone` falls into the branch where hold-and-watch decides alone, which for a handler that never
locks the file (the packaged Notepad, Explorer's zip view) means deleting a copy that is open on screen
ten seconds after it opened.

**Nothing this feature adds may be spelled `secret`.** `.gitignore` carries `*secret*` as a secrets guard,
so a file with that word in its path is written, built, rendered - and silently never committed. It has
caught a site page once already. Every path here is spelled `fdsec` or `fd-sec`, source and docs alike;
check a new one with `git check-ignore -v <path>` rather than with a `git status` that says nothing.

**The shell's password never reaches a command line.** `JobView` passes `pe:FILEDO_SHELL_CRED` and sets
that variable on the child process alone, so the process list, the run report and `history.json` see a
variable name. `filedo.exe` removes the variable from its own environment the moment it reads it, so the
program a reveal or `unsecure start` launches never inherits the password, and a `pe:` naming an unset or
empty variable is a usage error - never an empty password (SP-0025 FDSEC-07, FDSEC-19).
Four more rules from SP-0025 bind the container verbs: a final rename never replaces what it finds
(`fdsec.ErrExists` sends the collision rule round again); "overwrite" replaces the old destination only
through that final rename, after the new bytes verified (`fdsec.AllowReplace`); a stop ends a pack or an
unpack at a chunk boundary (`fdsec.WithStop`) and nothing is deleted, wiped or launched after one; and
the original is compared with its pre-pack snapshot (size, time, file ID) immediately before it is
removed. `cmd/filedo/fdsec_surfaces_test.go` asserts the credential rule structurally, along with the rest of the
ship-together surface set - the four claims in every README locale, the site guide in all three authored
locales plus its sitemap row, and all of the shell's fdsec strings in all five locale tables.

Tests use three seams, all honest about what they suppress: `FILEDO_FDSEC_NO_LAUNCH=1` skips only the
hand-off to the real registered handler, `FILEDO_FDSEC_REVEAL_ROOT` moves the sandbox root out of the
developer's profile, and `FILEDO_FDSEC_REG_ROOT` points `fdsec register` at a scratch key under HKCU
instead of `Software\Classes`, so the registration tests exercise the shipped code path without touching
the machine's own shell. None of them weakens what it moves: the access list is applied wherever the
sandbox root is, and the key names, labels and ownership checks are the real ones wherever they are written.
The `fdsec` package adds two of its own, in code rather than in the environment: `randSource` for fixed
randomness and `activeProfile` for the KDF work factor. Both are test-only by convention, and a container
written under a lowered profile opens only under that same profile - which is why no shipped path assigns
either one.

**Shared packages are main-module only.** `fileduplicates/`, `helpers/`, `fsx/` and `statedir/` are
imported as `filedo/...` and are reachable only from the root module (`cmd/filedo` and each other). The
three companion binaries are separate
modules (`filedo_check`, `filedo_fill`, `filedo_test`) with no `replace` back to the root, so whatever
they need is copy-pasted into their own directory - that duplication is structural, not an oversight.
Since SP-0028 (option A) they are **thin launchers** and carry no engine: each maps its own command
line onto `filedo.exe`'s (`filedo_fill D: 500` runs `filedo D: fill 500`) and runs the `filedo.exe` in
its own folder - its path resolved through symlinks for winget's `Links`, never the current folder or
`PATH` - with the console inherited, ending with that exit code. A fill, test or check fix therefore
lands in `cmd/filedo` only. What the three share is `launcher.go` and its two test files, identical in
every module and held together by `TestSharedFilesMatchTheOtherCompanions`.

## Build / release (which script is which)
<!-- canon-ok: the canon owns "a build is not a release"; what is repo-specific is which of the two
     scripts holds the tag, and that is the fact an agent has to get right here. -->
`build.ps1` and `release.ps1` are the two halves, and the wall between them is **structural**:

- **`.\build.ps1`** = BUILD ("сборка"): compile all five exes into `exe_to_download/` **and build both
  installer artifacts into `dist/`** (git-ignored) - a plain run needs no switch to produce what is
  distributable. It optionally tests (`-Test`) and commits (`-Commit`), and contains **no `git tag` call
  at all** - that is the enforcement, not a convention. The switches only subtract or demand:
  **`-SkipInstaller`** drops the installer for a fast loop, **`-Msi`** turns a missing WiX from a skip
  into exit 2 (what `release.ps1` wants before it tags), **`-Install`** implies `-Msi` and then runs the
  setup EXE here. Missing WiX on a plain run is a **printed skip, not a failure**: a packaging tool that
  is absent says nothing about the code that just compiled. The step adds `WixToolset.UI.wixext` and
  `WixToolset.BootstrapperApplications.wixext` **pinned to the tool's own version**: unpinned they resolve
  to the newest release (7.x), which a WiX 5 build refuses with *"Could not find expected package root
  folder wixext5"*; the skip matches name **and** version, so a 7.x copy in the cache does not count.
  `dist/` keeps only the current build's pair - every run stamps a new version, and a folder of
  near-identical 20 MB pairs is a folder nobody reads.
  **What it builds is what ships** (SP-0030 REL-04): the Go exes exactly as `release.yml` builds them -
  `windows/amd64`, CGO off, the pinned toolchain, `goversioninfo -64` (PE version + `app.manifest`),
  `-trimpath` - and the gate's `go test` runs as amd64 too. So `exe_to_download/` holds **amd64** exes
  (they were 386 before), and a host that cannot run amd64, a different local Go, or a goversioninfo other
  than `v1.4.1` is exit 2 before anything is built. It works from any directory. **Deploying is opt-in**:
  `-DeployTo <folder>` or `FILEDO_DEPLOY_DIR`, run last, after the gate and the installer, copying only
  `*.exe`, `*.bat` and `*.config` - a plain run writes nowhere outside the repository.
- **`.\release.ps1`** = RELEASE ("релиз"): the **only** thing that tags `v*` and triggers GitHub CI, then
  fans out to winget (`wingetcreate submit`) and an MSIX build; the Microsoft Store step is a manual
  Partner Center upload. Its preflight refuses a tree with anything uncommitted or untracked outside
  `exe_to_download/`, a stamp that is not a real date or not newer than every `v*` tag, and an MSI
  version not greater than the one the latest release's MSI carries. Its gate re-runs
  `build.ps1 -Test -Msi` when WiX is present (a wxs that does not compile must fail before the tag, not
  inside the tagged run) and plain `-Test` when it is not, then the full fdsec run and `govulncheck
  -mode=binary` over the four Go exes (the workflow repeats that scan after the tag, so it must pass
  before), and it aborts on any non-zero exit. It commits
  `exe_to_download/` and nothing else. After the tag a failed channel (winget sync, push, submit, the
  MSIX) is reported `[ ]` with its reason and makes the exit code 1; **`-Resume -Version <stamp>`**
  continues a release whose tag is already on origin, from the wait for the GitHub Release on - never cut
  a new version to repair one.

**Channel pre-publication checks** (`packaging/`, run by the two flows above - never by hand at release time):
- **`packaging/check-winget-manifests.ps1`** gates `winget/`. `release.ps1` runs it **twice**: in the
  preflight with `-NoNetwork` (frozen anchors, while there is still no tag) and in step 6 against the
  *synced* manifests with `-Version` (schema via `winget validate`, the version in every URL, and
  `InstallerSha256` against the `.sha256` the release published) - **before** the commit and the
  winget-pkgs PR, because microsoft/winget-pkgs re-validates only after the tag is already out.
  `-Install` adds the end-to-end install/run/uninstall cycle; it writes to the operator's profile,
  so it is opt-in (`release.ps1 -WingetInstallTest`) and refuses outright if that package is already
  installed. winget's own output is localized - only its exit code is read (`0`, or `-1978335192` for
  warnings, which winget-pkgs accepts). The installer manifest carries `ArchiveBinariesDependOnPath: true`
  (checked here): winget puts the package folder on PATH instead of a symlink per alias, because .NET
  Framework reads `filedo_win.exe.config` beside the path the GUI was started from (SP-0030 PKG-04).
- **`packaging/verify-sha256-sidecars.ps1`** is called by `release.yml` after the three artifacts are
  built: it re-reads every `.sha256` and re-hashes the asset it names. That file is the download page's
  integrity contract *and* the source `InstallerSha256` is synced from, so a drifted sidecar is wrong in
  two channels at once.
- **`packaging/check-third-party-notices.ps1`** is a `build.ps1 -Test` step: every Go module each shipped
  executable links (`go list -deps`, windows/amd64) must have a section in `THIRD-PARTY-NOTICES.txt` at
  that version, and every module section must still be linked by something. The file is written by hand;
  this is what notices a dependency added or bumped without its license (SP-0030 REL-18).
- **`release.yml` validates before it publishes** (SP-0030 CI-01..03): the tag reaches its scripts only
  through `env:`, must be `v` + a real `yyMMddHHmm` date whose commit is on `origin/main`, and there is no
  `0.0.0.0` fallback; the release becomes Latest only when its stamp is newer than the current Latest's.
  Every action is pinned by commit SHA (version in a comment), checkout does not persist the token, WiX is
  `5.0.2`, Go is `1.26.1` with `GOTOOLCHAIN=local` and checked against `go.mod`, `govulncheck
  -mode=binary` reads every Go exe, and the Windows build job has `contents: read` - only a separate
  publish job, which runs no project code, may write the release. Bumping Go means `go.mod`'s toolchain
  line and the workflow's `go-version` together; the gate fails when they differ.
- **`msix/test-store-package.ps1`** is the Store-submission path, not the release gate: WACK
  (`appcert.exe`, its own kit directory - **not** the SDK `bin\<ver>\x64` that `Find-SdkTool` searches)
  against the `-SelfSign` package, and a GET on the published privacy-policy URL. It needs an **elevated**
  session (appcert refuses even `/?` without one) and the local-test certificate trusted (`-TrustTestCert`).
  The verdict is read from the report's `OVERALL_RESULT`, never from the exit code: appcert returns 0 for
  a session that ran and found failures.
- Single-target builds: `go build -o filedo.exe .\cmd\filedo` (main CLI); each companion tool builds from
  its own `cmd\filedo-*` directory. The GUI is MSBuild, not Go: `MSBuild filedo_win_vb\FileDOGUI.vbproj
  /p:Configuration=Release /p:Platform=AnyCPU` (locate it with `vswhere -latest -find
  "MSBuild\**\Bin\MSBuild.exe"`); `.\build.ps1 -SkipGui` drops it when VS Build Tools are absent.

Both are PowerShell and both must be invoked through the PowerShell tool, or from Bash as
`pwsh -NoProfile -File ./build.ps1 -Test`. Prefer the in-repo skills `/build` and `/release`;
[`RELEASE.md`](RELEASE.md) is the long-form manual for both flows.

## Store channel (`msix/`)
[`msix/README.md`](msix/README.md) is the runbook; this section keeps only what must not be got wrong.
- **No Store package is ever built from placeholders.** `msix/build-msix.ps1` refuses without a reserved
  identity (`-IdentityName`, or `msix/identity.json`) and without a `CN=<GUID>` Publisher. The Publisher
  (`CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD`) and `PublisherDisplayName` (`SZA`) are account-wide; only
  `Package/Identity/Name` is per product. FileDO's is **reserved: `SZA.FileDO`** (Store ID `9PH1LPCMRG83`,
  family `SZA.FileDO_fdk7e19xt9z9j`), recorded in `msix/identity.json` as read from Partner Center's
  *Product identity* page, and live since 2026-09-23 - so it is a **frozen anchor** (listed with the others
  in "Project shape"): a package under another name is a different app, and every install of this one is
  orphaned. `identity.json` changes only if that page says something else.
- **The Store GUI carries the release stamp** (SP-0030 PKG-03): `build-msix.ps1` builds `filedo_win.exe` with
  `/p:BuildStamp` and `/p:AssemblyVersion` exactly as `build.ps1` does and reads its FileVersion back from the
  stage, and it refuses a local Go other than `go.mod`'s toolchain pin.
- **A test build cannot pass for a Store build**: `-SelfSign` and `-Register` carry `SZA.FileDO.LocalTest` and
  write `_LOCALTEST` files. `-Register` (Developer Mode) is the only switch that changes the machine.
- **The console application is visible.** The Store rejects a hidden (`AppListEntry="none"`) application as
  headless - measured on this account by doc-html-translate. `-Cli hidden|none` are the escape hatches.
- **`msix/listing/<code>.txt` is the one source of listing copy** (en ru uk de fr - the GUI's languages, kept in
  step with the manifest `Resources` and the builder's locale map). The Partner Center console is a render
  target and `store-listing.md` carries only what the CSV cannot. `msix/test-store-tools.ps1` gates both.
- **`msix/make-screenshots.ps1` takes the foreground**: run it on an idle machine. It chooses rail rows
  through UI Automation (each row is named `rail:<key>` and has a default action), captures either theme
  (`-Theme light|dark`), and refuses to save a shot in which a control painted as a WinForms red-cross
  placeholder rather than ship it.
- **Nothing in `msix/` publishes.** The upload, the listing import and the submission are the owner's.

## Testing
- Root **`go test ./...` is known-broken** (existing `fmt`/vet debt) - do **not** treat it as the gate. This
  known-red is tracked on purpose so a real regression is not masked.
- The real gate is `build.ps1 -Test`, in nine steps: smoke every shipped executable for its stamped version
  and its release build shape (amd64, `-trimpath`, the pinned Go, PE version, manifest); compile-check
  `cmd\filedo-test` and validate `packaging/check-placement.jsonl`; `go test ./fdsec/ -count=1 -short`;
  `go test ./cmd/filedo/ -count=1 -vet=off` plus the shrink-only `cmd/filedo/vet-baseline.txt`;
  `filedo_win.exe --selftest` (skipped only with `-SkipGui`; its old log is deleted first, and a missing log
  is exit 2); `packaging/check-third-party-notices.ps1`; `release.yml`'s Go pin against `go.mod`'s; and
  `packaging/check-internal-docs.ps1` - every document declared in `docs/DOCUMENT_REGISTRY.jsonl` (a new
  `.md`/`.html` needs a record there, or the gate fails), every relative link and anchor resolving, and
  the internal corpus in house style with no remote embeds; and packaging/check-external-docs.ps1 - the
  published site and the READMEs against `DOC-EXTERNAL-QUALITY`: glossary and subject index, `sitemap.xml`
  against the page set, the full SEO block, every `data-l` group in ru/en/ua, translation freshness against
  `docs/translation-fingerprints.json`, the `docs/termbase.json` forbidden synonyms, screenshots and prose
  hygiene. **An EN edit on the site or in `README.md` fails it** until the translations are updated and
  `check-external-docs.ps1 -Record` re-records the fingerprints. The
  Go builds and tests all run as windows/amd64 - the binary that ships - and the `cmd/filedo` suite's own
  exe is linked with the same version resource and manifest. Its last line is `build-gate <stamp>: PASS`,
  `FAIL`, or `NOT VERIFIED`. Its exit codes are **0 = pass**, **1 = a defect was found**, and **2 = the gate
  could not verify** (a missing prerequisite - Go other than the pin, goversioninfo, MSBuild without
  `-SkipGui` - is 2 as well). Exit 2 is not a pass and not a defect report; treat it as "nothing was proven".
- `cmd/filedo-test` is **its own module**, so the compile-check must run from inside it
  (`cd cmd\filedo-test; go test ./...`). Addressing that module from the repository root fails with
  *main module (filedo) does not contain package* - a trap worth knowing, because the console line
  `build.ps1` used to print that failing form. Since the launcher conversion (SP-0028) that run is a real
  test and not only a compile: each companion's `main_test.go` pins every documented command line to the
  exact `filedo.exe` argv, and the shared `launcher_test.go` builds the launcher beside a stub `filedo.exe`
  and proves exit-code and argv forwarding, exit 2 with no `filedo.exe`, no current-folder/`PATH` lookup,
  the symlink case and Ctrl+Break. `cmd\filedo-check` and `cmd\filedo-fill` carry the same suite, run the
  same way from inside each module.
- **Two packages carry real tests** and both are part of the gate:
  - `fdsec/` - the container format, its boundary corpus, every tampering class and the committed vectors.
    It is vet-clean, so it runs plainly. `-short` drops the >4 GiB round trip (about 90 s); `release.ps1`
    runs it without `-short` before tagging.
  - `cmd/filedo/` - the black-box acceptance suite for the fdsec command surface. It builds its own exe and
    asserts exit codes, `history.json` redaction, the batch `.lst` path and the destructive rules. It needs
    **`-vet=off`**, because package main carries the pre-existing vet debt recorded, never suppressed, in
    `cmd/filedo/vet-baseline.txt`; without the flag `go test` fails at the vet step before a test even runs.
    `outcome_cli_test.go` is the exit-code table - one verb of each kind, its code and the verdict its
    `result` event must carry - and `fdsec_surfaces_test.go` is the cross-surface gate, which now also
    holds the install-trust page to its contract.
- **The shell has its own gate and it is not `go test`**: `filedo_win.exe --selftest` builds every job
  page, then runs the consumer half of `CLI-EVENT-STREAM` - the `verdict:` rows (every verdict/exit-code/
  stop-file/schema-version combination `Runner.Judge` can meet) and the `tailer:` rows (a line written in
  two halves, an unparseable timestamp, a newer MAJOR) - and the `APP-BEHAVIOUR`/`APP-STYLE` rungs:
  `palette:`, `theme:`, `rail-paint:`, `rail-label:` (every label in five languages), `progress:`,
  `format:`, `a11y:`, `placement:`, `wipe:` and `command:` rows - and the iconography ones: `icons:`
  (every vendored drawing hashed against `PROVENANCE.txt`, mapped, on its grid, painting), `rail-glyph:`
  (the rail's glyph map, with a shrink-only count of Segoe stand-ins), `glyph:` (the verdicts),
  `state-tone:` (the shared `state.*` tones) and `contrast:` (every glyph against its surface). Its palette switch goes through
  `Theme.UsePaletteForTest`, never through HKCU. It writes `filedo_win_selftest.log` beside the exe
  and exits 0, 1, or 2 (the last means its log could not be written). It is a GUI-subsystem process, so from PowerShell it needs
  `Start-Process .\filedo_win.exe --selftest -Wait -PassThru` to have an exit code at all.
- A change to a companion's command line is tested in that companion's `main_test.go`; the companions
  cannot reach the main CLI's code, so a fill, test or check behaviour is tested in `cmd/filedo`. Note
  any disk, drive-letter, or admin requirement in the PR. List-driven scenarios (`tests\*.lst`) fill,
  wipe and scan what they name, so they name placeholders (`<TARGET_DRIVE>`, `<TARGET_DIR>`) and run only
  through `tests\run-test-list.ps1`, which takes the target from `FILEDO_TEST_TARGET` and refuses anything
  but a mounted VHD's drive letter or a folder under `%TEMP%` (`tests\prepare_test_env.cmd` = its
  `-PrepareOnly`). Never point them at a real data volume.
- **A verb's acceptance test is still a batch run**, even though `main()` and the batch now share one
  dispatch (`dispatchLine`): the batch adds its own tokenizer (BOM, UTF-16, quotes, a `filedo.exe`
  prefix), its stop rule and its per-line history, and a regression there is only seen from a `.lst`.
  Note that `runGenericCommand` probes the target with `os.Stat` before dispatching, so anything taking a
  mask target has to be dispatched ahead of it (`fdsecDispatchTarget` is the worked example).
- **Destructive-tool safety is the pass/fail line, not friction.** For `wipe`/`fill` the persona test
  inverts: `wipe` must demand typing `WIPE`; `--force`/`-y` skips only the *prompt*, never the safety
  checks on drive/share roots, reparse points, and system TEMP; writes at `C:` redirect to
  `%TEMP%\FileDO_Operations`. The duplicate-finder hash cache must key on path + size + **modtime** (size
  alone once let an edited file be deleted as a false duplicate - now a permanent invariant).

## The GUI shell (`filedo_win_vb/`)
The VB.NET project holds one window, `ShellForm` - the shell built under SP-0006 - and `Program.Main` is
its entry point. The command builder that shipped before it (`MainForm`, `AboutForm`) was **retired**;
`--legacy-builder` is still accepted and simply opens the shell, so an old shortcut keeps working. The
shell's Command page is where a hand-written command line lives. `filedo_win.exe` is a frozen anchor (one
name, one winget alias, one MSIX `Application Id`), so the shell is a **form**, never a second program.

**The program can be started on a file.** `Program.StartupTarget` takes the first argument that is not a
switch and is a file on disk, and hands it to `ShellForm`, which opens the reveal page for a `.fd-sec` and
the secure page for anything else (SP-0005 9.4). Two consequences are load-bearing: a copy started on a
file does **not** stand down for the single-instance check - focusing another window would drop the file
the start was about - and each double-clicked container therefore gets its own window, which is what makes
"remove the copy now" belong to one reveal rather than to whichever ran last.

**The shell consumes `APP-BEHAVIOUR` and `APP-STYLE`** (see "External contracts"). What that means in
code, each held by a gate:
- **No exception text on screen.** A caught exception goes to `ShellLog` (`%LOCALAPPDATA%\FileDO\
  filedo_win.log`, which Send logs packs), and the user gets `ShellDialog.Problem` - a cause named from the
  exception's *type* (`Problems.CauseKey`) and actions. `Program.Main` handles every exception nothing else
  caught the same way.
- **No system message box and no owner-less dialog.** Every question is a `ShellDialog` (owned, themed,
  Escape and the close box give the no-action answer); every common dialog gets `FindForm()`.
- **No `String.Format` on a translation** - `Localization.Format`, which never throws.
- **A running job is never lost.** A job row clicked mid-run keeps the running page; closing mid-run asks.

**The child-process contract** (SP-0023 theme T7; `Runner.vb`): the child's stdin is redirected and closed
right after `Start()`, so every CLI prompt reads end-of-input and takes its safe branch (the system-drive
redirect's default is "redirect"; a delete, a wipe or an overwrite is "no"); a page that needs a "yes"
confirms it itself and passes the explicit token (`-y`, typed `WIPE`/`DELETE`/`MOVE`). Output is read as
UTF-8, batched into the pane every 100 ms with a bounded tail, and spooled to the report rather than held
whole. Non-elevated children run in a Job Object with kill-on-close, so a GUI that dies takes its
filedo.exe with it (programs a reveal opens break away and stay). The run report's `Command:` line goes
through the same redaction as history.json - `cmd/filedo/testdata/redaction/vectors.tsv` is read by both
the Go test and `--selftest` - and a reveal, `fdsec info|verify` or `unsecure .. start` report keeps the
verdict and the exit code only, never the output that names a sealed file. An empty password on a
Protect page travels as the visible `p:`; a real one only as `pe:FILEDO_SHELL_CRED`.

Four rules a change here must not break:
- **`Theme.vb` is the only file allowed to name a colour.** No `Color.`, `FromArgb` or `RGB(` anywhere
  else in the shell - and mixing two tokens counts as naming one, which is why `Theme.Blend` is private to
  it and a mix a view needs becomes a named palette role. `cmd/filedo/shell_contract_test.go` makes the
  search on every build. A mix is done in `Integer`: a `Byte` minus a larger `Byte` throws in VB, which is
  what once painted the light theme's selected rail row as a red cross.
- **No fixed coordinates.** Layout comes from `TableLayoutPanel`, `FlowLayoutPanel`, docking and
  anchoring - a `System.Drawing.Point(..)` is exactly what a per-monitor-DPI window must not have.
- **A glyph is the vocabulary's drawing, and the drawing is the catalog's file.** The shell's icons are
  the `ICON-SET` Material filled paths vendored under `assets/glyphs/` by `assets/sync-icon-glyphs.ps1`
  and embedded in the exe; `Glyphs.vb` hashes each against `PROVENANCE.txt` before drawing it. A control
  names its meaning - `GlyphRef.Vocabulary("<id>")`, or `GlyphRef.Waiting("<proposed id>", ..)` with a
  Segoe stand-in while the catalog has no record - never a codepoint of its own. Mapping a new meaning is
  three edits: the id in the sync script's list, the import, and the control; never hand-edit the folder,
  and never drop `.gitattributes`' `-text` on it, or a checkout's line endings change every hash. The
  Explorer entries' and the `.fd-sec` type's icons are the same drawings as `.ico` files in
  `assets/menu-icons/`, written by `filedo_win.exe --write-menu-icons assets\menu-icons` (`MenuIcons.vb`)
  and never by hand; `--selftest` redraws each and fails on a stale one. The MSI, `fdsec register` and the
  packaged `FileDOShell.dll` all name them as `icons\<id>.ico` beside the exe.
- **The DPI declaration is two files and both must ship.** `app.manifest` is linked into the exe;
  `app.config` becomes a separate `filedo_win.exe.config` that `build.ps1`, `packaging/wix/FileDO.wxs`,
  `msix/build-msix.ps1` and the release workflow each have to carry. Shipping the manifest without the
  config renders worse than shipping neither, because the window is told the real DPI and then ignores it.

Every user-visible string goes through `Localization.vb` in all five languages (`en`, `ru`, `uk`, `de`,
`fr`), in its `key|value` format - no `|` inside a value, `\n` for a line break, English as the fallback.
Two traps: VB rejects a **comment line inside an array initializer**, and a key added to English alone
will silently fall back rather than fail.

## Coding style
Go defaults via `gofmt` before committing. `cmd/filedo` is **Windows-only** (owner decision D5,
2026-09-25: no supported channel targets another OS), so it carries no `*_unsupported.go` fallbacks; keep
Windows-specific behavior in `*_windows.go` anyway, and prefer small, focused files over growing `main.go`.

## Site (`docs/`)
Hand-authored, **no generator** - so the pages are edited in place and are not render targets. The tree
index, the canonical page list and the redirect-stub rule are in [`docs/README.md`](docs/README.md); adding
a public page means editing `docs/sitemap.xml` in the same commit.

**The download buttons are resolved at runtime, and the release asset names are the contract.** The
landing page (`docs/index.html`, the install band `#get`) starts every link on `releases/latest` and, when
GitHub's `releases/latest` API answers, upgrades it to the direct file by asset-name suffix:
`-setup.exe` (the main button, with its name, size and `.sha256` link), `-windows-x64.zip`,
`-windows-x64.msi`. So a release never needs a site edit - and renaming an asset in `release.yml` silently
turns a download button back into a plain link to the Releases page. The installer is **not code-signed**,
and the landing page, the install guide and every README say so next to the download; if that ever
changes, those lines change in the same edit. The install band is compact by design: CSS hides
`.quickstart`, `.install-example` and any `.note` placed directly in `.install-panel`, so a caveat that
has to be seen goes inside `.download-block`.

## Working documents (`PLAN/`)
Specs, implementation plans, questionnaires and research notes live in `PLAN/` at the repository root,
which `.gitignore` keeps out of git. What the repository carries is documentation of the finished
product, written for an experienced user - the site, the READMEs, the listing sources. Never
`git add -f` a working document, and never link to one from a tracked file (this section included):
`PLAN/` is local to this machine, so whoever clones the repository does not have it.

**The id scheme** - this repo's answer to "ticket system and id scheme". A top-level specification is
`PLAN/SP-XXXX name.md`; everything downstream of it - work plan, tactical documents, questionnaires,
research and measurement output - lives in `PLAN/SP-XXXX name/`; an implemented and proven specification
moves, with its folder and its id, to `PLAN/done/`. Ids are four digits, assigned once, never reused and
never renumbered - the next one is one past the highest id anywhere under `PLAN/`, `done/` included.
State is the `Status:` line inside each specification plus which of the two folders it sits in: the index
at `PLAN/README.md` carries ids, titles and links and deliberately no status. Material with no
specification behind it goes to `PLAN/notes/` and gets no id.

## External contracts (`P:\Contracts\`)
A contract that outlives this repository - a file format, a wire or event protocol, a behaviour at a
boundary, anything a second project of ours or an outside developer implements against - is **not** kept
in this repository. It lives in `P:\Contracts\`, the shared contracts catalog: **one folder per function**,
never per product, outside git and outside every product, because two projects that each carry their own
copy of a shared contract have two contracts. `P:\Contracts\README.md` is the index,
`P:\Contracts\_meta\RULES.md` is the law it runs by, `_meta\VERSIONING.md` is the compatibility law every
consumer path here obeys, and `_meta\REGISTRY.md` is where this product's rows and its dated exceptions
live. **This line is the only place in the repository that says where the catalog is.**

What this repository implements, with its pointer file:

| Contract | Version | Catalog folder | Pointer here |
| --- | --- | --- | --- |
| `FDSEC-FORMAT` - the on-disk format of a `.fd-sec` container, byte for byte (suite 1 one file, suite 3 one directory tree) | 1.2 | `secure-container/` | [`docs/contracts/FDSEC-FORMAT.md`](docs/contracts/FDSEC-FORMAT.md) |
| `FDSEC-BEHAVIOUR` - everything a port of `secure` / `unsecure` must reproduce: the read-back proof before any disposition of the original, the three outcome classes, credential hygiene, the conformance checklist | 1.2 | `secure-container/` | [`docs/contracts/FDSEC-BEHAVIOUR.md`](docs/contracts/FDSEC-BEHAVIOUR.md) |
| `CLI-EVENT-STREAM` - the `--events` JSON Lines channel, the `--stop-file`, and the rule that a verdict comes from the `result` event and never from an exit code alone | 0.9 draft | `cli-event-stream/` | [`docs/contracts/CLI-EVENT-STREAM.md`](docs/contracts/CLI-EVENT-STREAM.md) |
| `INSTALL-TRUST` - what a user reads in the thirty seconds after Windows warned them about an unsigned build | 1.0 | `install-trust/` | [`docs/contracts/INSTALL-TRUST.md`](docs/contracts/INSTALL-TRUST.md) |
| `CHECK-VERDICT` - the exit code and final machine-readable line emitted by an automated check | 0.10 draft | `automated-checks/` | [`docs/contracts/CHECK-VERDICT.md`](docs/contracts/CHECK-VERDICT.md) |
| `CHECK-BASELINE` - the shrink-only file carrying accepted check debt | 0.9 draft | `automated-checks/` | [`docs/contracts/CHECK-BASELINE.md`](docs/contracts/CHECK-BASELINE.md) |
| `CHECK-PLACEMENT` - the record mapping every check to its runner | 0.10 draft | `automated-checks/` | [`docs/contracts/CHECK-PLACEMENT.md`](docs/contracts/CHECK-PLACEMENT.md) |
| `BUILD-EVIDENCE` - the version carried by an artifact and the gate judging it | 0.9 draft | `automated-checks/` | [`docs/contracts/BUILD-EVIDENCE.md`](docs/contracts/BUILD-EVIDENCE.md) |
| `ICON-SET` - the glyph vocabulary: one meaning, one glyph, one name, on every surface that shows a picture (consumer) | 0.15 draft | `iconography/` | [`docs/contracts/ICON-SET.md`](docs/contracts/ICON-SET.md) |
| `ICON-RENDER` - how a glyph is drawn: colour role, themes, sizes, accessible name, the product mark on system surfaces (consumer) | 0.13 draft | `iconography/` | [`docs/contracts/ICON-RENDER.md`](docs/contracts/ICON-RENDER.md) |
| `ICON-EXTERNAL` - third-party marks, other apps' icons, downloaded pictures (consumer) | 0.10 draft | `iconography/` | [`docs/contracts/ICON-EXTERNAL.md`](docs/contracts/ICON-EXTERNAL.md) |
| `REPO-STAMP` - the canon adoption stamp `.sza-canon.json`, written by the canon's adoption run (producer) | 0.9 draft | `rule-adoption/` | [`docs/contracts/REPO-STAMP.md`](docs/contracts/REPO-STAMP.md) |

What this repository consumes:

| Contract | Version | Catalog folder | Pointer here |
| --- | --- | --- | --- |
| `PAGE-CONTENT` / `PAGE-STYLE` / `SITE-FAMILY-MAP` - what the product page says and in what order, how it looks, and the family footer | 1.1 / 1.1 / 1.1 | `product-web-pages/` | [`docs/contracts/PAGE-CONTENT.md`](docs/contracts/PAGE-CONTENT.md), [`PAGE-STYLE.md`](docs/contracts/PAGE-STYLE.md), [`SITE-FAMILY-MAP.md`](docs/contracts/SITE-FAMILY-MAP.md) |
| `APP-BEHAVIOUR` - what the shell does at the twelve moments a user of any SZA desktop app meets: escapable dialogs, honest progress, consent, confirmation, failure as actions, localized rendering, accessible names, window placement, first run, settings | 0.10 draft | `desktop-app-ux/` | [`docs/contracts/APP-BEHAVIOUR.md`](docs/contracts/APP-BEHAVIOUR.md) |
| `APP-STYLE` - the theme mechanism (system/light/dark, live), one palette table, the role vocabulary, declared out-of-theme surfaces | 0.10 draft | `desktop-app-ux/` | [`docs/contracts/APP-STYLE.md`](docs/contracts/APP-STYLE.md) |
| `REPO-LAYOUT` - the root and `docs/` names a shared tool may address without asking | 0.9 draft | `rule-adoption/` | [`docs/contracts/REPO-LAYOUT.md`](docs/contracts/REPO-LAYOUT.md) |
| `RULE-DELIVERY` - how the canon arrives through the `sza` plugin, and how staleness is judged | 0.9 draft | `rule-adoption/` | [`docs/contracts/RULE-DELIVERY.md`](docs/contracts/RULE-DELIVERY.md) |
| `DOC-INTERNAL-QUALITY` / `DOC-EXTERNAL-QUALITY` - what the internal docs and the published site plus READMEs owe their readers: registry, links, style, freshness, termbase, SEO | 0.9 draft / 0.9 draft | `documentation-quality/` | [`docs/contracts/DOC-INTERNAL-QUALITY.md`](docs/contracts/DOC-INTERNAL-QUALITY.md), [`DOC-EXTERNAL-QUALITY.md`](docs/contracts/DOC-EXTERNAL-QUALITY.md) |

Four rules, because a shared contract breaks differently from ordinary code:

- **Cite by id, point in one place.** The comments in `fdsec/*.go` say "(FDSEC-FORMAT.md section 8)" and
  nothing more; elsewhere it is "(CLI-EVENT-STREAM rule 10)". This section is the one place that says
  where those documents live, so moving the catalog costs one edit rather than forty. Tracked files may
  cite a contract; they must not link to it, for the same reason `PLAN/` may not be linked - whoever
  clones this repository does not have `P:\`. `docs/contracts/` holds a pointer per contract, and a
  pointer is an id, a version, a role and what we owe it - never a copy of the text.
- **The contract changes before the code does.** A format or protocol edit is agreed in the catalog, the
  version is bumped, the vectors are regenerated, the registry row is updated, and only then does an
  implementation follow. Code that ships ahead of its contract is how two projects stop being able to
  read each other's files.
- **Comply, or amend - never deviate in silence.** A deviation that is written into `_meta\REGISTRY.md`
  with a reason and an `until` date is a plan; the same deviation unrecorded is a violation. Never weaken
  a contract to match this code: if the code is right, write the amendment with the evidence.
- **The vectors are the arbiter, and they are generated.** `fdsec/testdata/vectors/` is this repository's
  test fixture, produced by `FDSEC_WRITE_VECTORS=1 go test ./fdsec -run "TestVectors_Suite1|TestVectors_Suite3"` (suite 3 in its `suite3/` subfolder); the copy in
  the catalog's `secure-container/vectors/` is what an outside implementation checks itself against.
  Regenerate the fixture, then copy it across - never hand-edit either one.

## PR specifics
Beyond the canon's commit and PR conventions: include the manual verification commands you ran with their
output, and attach screenshots for GUI, installer, or docs changes.
