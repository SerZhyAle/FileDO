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
  shared top-level packages `fileduplicates/`, `helpers/`, `capacitytest/`. **Multi-module**: 4 separate
  `go.mod`/`go.sum`, mixed Go versions (1.24.4 / 1.21). Module path `filedo` (a frozen anchor).
- **Version shape.** Separator-less `yyMMddHHmm` (e.g. `2606120121`) - a sortable 10-digit integer. Git
  tag `v<stamp>`, validated by `release.ps1` as `^\d{10}$` before it prefixes the `v`, and stamped into
  the binaries via `-ldflags "-X main.version="`. Remapped mechanically for MSIX and the PE
  `VS_VERSIONINFO` only.
- **Release-mechanics** are top-level channel siblings (no `publishing/` umbrella): `winget/`, `msix/`,
  `packaging/wix/`, committed `exe_to_download/`. `winget/` holds the three manifests and **nothing else**:
  `winget validate` is pointed at the directory and parses every file in it as YAML, which is why the
  manifest checker lives in `packaging/` (below) and not beside what it checks.
- **Frozen anchors** (reserve once; changing orphans installs): winget `PackageIdentifier
  SerZhyAle.FileDO`; WiX MSI `UpgradeCode 4d6b3b1f-7c8e-4a25-9f1d-8e3b2c5a7d91` (+ `HKLM\Software\FileDO`);
  MSIX Identity `Name`/`Publisher`; Go module `filedo`; the 5 winget `PortableCommandAlias` names; the
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
own** - its UI is the MSI's feature tree (`bal:DisplayInternalUICondition="1"`, and the bootstrapper's own
theme is `none` so there is not a second wizard), so a change to what is installed is a change to the wxs
and never to the bundle. `.\build.ps1 -Install` builds both and then runs the EXE on this machine - the
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
the same commit. Two things are registered and nothing else (SP-0005 9.1, as amended 2026-09-20): the
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

The **Store (MSIX) build has no Explorer entries**: a packaged build can only declare them through a signed
shell command handler (SP-0005 9.3, still unbuilt). Say that plainly rather than implying parity.

## Code map (`cmd/filedo`)
The main CLI has **two dispatch layers**, and they are the thing that costs an hour to rediscover.

1. **Verb dispatch** - `main.go` matches raw `os.Args` against the `list_of_flags_for_*` alias slices
   (`copy`/`cp`, `wipe`/`w`, `cd`/`check-duplicates`, ..), falling back to path sniffing (drive letter,
   `\\` prefix, `os.Stat`) to infer `device`/`folder`/`file`/`network` when arg 1 is not a known verb.
   **`executeInternalCommand` in the same file is a second, near-duplicate copy of that switch**, used by
   batch mode (`from`/`batch`). A new verb therefore needs four edits: its own alias slice,
   `list_of_flags_for_all`, the `main()` switch, and the `executeInternalCommand` switch - miss the last
   and the verb works interactively but silently not from a `.lst`.
2. **Target dispatch** - only the four target verbs go on to `runGenericCommand` (`command_handlers.go`),
   which picks `DeviceHandler`/`FolderHandler`/`NetworkHandler`/`FileHandler` behind the `CommandHandler`
   interface. Sub-operations there are matched by **positional string compare** on `cmd.Arg(1)` (`speed`,
   `fill`, `test`, `clean`, `cd`, ..); the `flag.FlagSet` is parsed but defines no options, so nothing is
   flag-driven. `copy`/`compare`/`check`/`wipe` never reach this layer - they are handled straight out of
   the `main.go` switch.

`redirectSystemDrive` (`command_handlers.go`) is the single choke point sending `speed`/`fill`/`test`
writes on `C:` to `%TEMP%\FileDO_Operations`.

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

**The fake-capacity engine exists twice.** The live one is `main_types.go`: interface
`FakeCapacityTester`, driver `runGenericFakeCapacityTest`, and the `verify*` family; `DeviceTester`
(`device_windows.go`), `FolderTester` (`folder.go`) and `NetworkTester` (`network_windows.go`) implement
it. Top-level `capacitytest/` is an exported near-copy of the same code of which **only two helpers are
actually called** (`CalibrateOptimalBufferSize`, `WriteTestFileWithBufferContext`, both from
`network_windows.go`). Detection fixes belong in `main_types.go`; editing `capacitytest/` changes nothing.

Detection rests on three independent signals inside `runGenericFakeCapacityTest` - keep all three: header
== footer (`FILEDO_TEST_<name>_<ts>`), a per-file body tag `F<seq>_` woven through the payload so a
controller aliasing block ranges is caught by reading any middle offset, and write speed staying within
0.1x..10x of the 3-file baseline. File 1 is re-verified after *every* write. On failure the test files are
deliberately **kept, never cleaned up** - they are the evidence for the estimated-real-capacity report.

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

A container carries **no marker of any kind** - no magic, no version in the clear, nothing a scanner can
read (`FDSEC-FORMAT.md` sections 4, 13.4). Three consequences bind every edit here. The password is
needed before anything can be read, so `fdsec info` takes one like every other verb. Screening a file
without the password can only ever be the length test of `fdsec.ScreenLength`, never a look at its bytes.
And "wrong password", "not a container" and "damaged head" are one outcome: a message that names only one
of the three is a bug, not a wording choice. The KDF work factor lives in the format version, not in the
file, for the same reason - the mask has to be derived before the head can be read; `fdsec`'s tests lower
it through the `activeProfile` seam, and `TestVectors_Suite1` puts the real one back.

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
variable name. `cmd/filedo/fdsec_surfaces_test.go` asserts this structurally, along with the rest of the
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

**Shared packages are main-module only.** `fileduplicates/`, `helpers/` and `capacitytest/` are imported
as `filedo/...` and are reachable only from `cmd/filedo`. The three companion binaries are separate
modules (`filedo_check`, `filedo_fill`, `filedo_test`) with no `replace` back to the root, so whatever
they need is copy-pasted into their own directory - that duplication is structural, not an oversight.

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
  folder wixext5"*. `dist/` keeps only the current build's pair - every run stamps a new version, and a
  folder of near-identical 20 MB pairs is a folder nobody reads.
- **`.\release.ps1`** = RELEASE ("релиз"): the **only** thing that tags `v*` and triggers GitHub CI, then
  fans out to winget (`wingetcreate submit`) and an MSIX build; the Microsoft Store step is a manual
  Partner Center upload. Its gate re-runs `build.ps1 -Test -Msi` when WiX is present (a wxs that does not
  compile must fail before the tag, not inside the tagged run) and plain `-Test` when it is not, and it
  aborts on any non-zero exit.

**Channel pre-publication checks** (`packaging/`, run by the two flows above - never by hand at release time):
- **`packaging/check-winget-manifests.ps1`** gates `winget/`. `release.ps1` runs it **twice**: in the
  preflight with `-NoNetwork` (frozen anchors, while there is still no tag) and in step 6 against the
  *synced* manifests with `-Version` (schema via `winget validate`, the version in every URL, and
  `InstallerSha256` against the `.sha256` the release published) - **before** the commit and the
  winget-pkgs PR, because microsoft/winget-pkgs re-validates only after the tag is already out.
  `-Install` adds the end-to-end install/run-a-shim/uninstall cycle; it writes to the operator's profile,
  so it is opt-in (`release.ps1 -WingetInstallTest`) and refuses outright if that package is already
  installed. winget's own output is localized - only its exit code is read (`0`, or `-1978335192` for
  warnings, which winget-pkgs accepts).
- **`packaging/verify-sha256-sidecars.ps1`** is called by `release.yml` after the three artifacts are
  built: it re-reads every `.sha256` and re-hashes the asset it names. That file is the download page's
  integrity contract *and* the source `InstallerSha256` is synced from, so a drifted sidecar is wrong in
  two channels at once.
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
  `Package/Identity/Name` is per product, and **it is not reserved yet**. Reserving it is a manual, permanent
  Partner Center step, and the value is recorded in `identity.json` only as read from the *Product identity* page
  - the `SZA.<name>` pattern of the sibling products is an expectation, never a fact to build on.
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
- The real gate is `build.ps1 -Test`, in five steps: smoke every shipped executable for its stamped version;
  compile-check `cmd\filedo-test` and validate `packaging/check-placement.jsonl`; `go test ./fdsec/ -count=1
  -short`; `go test ./cmd/filedo/ -count=1 -vet=off` plus the shrink-only `cmd/filedo/vet-baseline.txt`; and
  `filedo_win.exe --selftest` (skipped only with `-SkipGui`). Its last line is `build-gate <stamp>: PASS`,
  `FAIL`, or `NOT VERIFIED`. Its exit codes are **0 = pass**, **1 = a defect was found**, and **2 = the gate
  could not verify**. Exit 2 is not a pass and not a defect report; treat it as "nothing was proven".
- `cmd/filedo-test` is **its own module**, so the compile-check must run from inside it
  (`cd cmd\filedo-test; go test ./...`). Addressing that module from the repository root fails with
  *main module (filedo) does not contain package* - a trap worth knowing, because the console line
  `build.ps1` used to print that failing form.
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
  `format:`, `a11y:`, `placement:`, `wipe:` and `command:` rows. Its palette switch goes through
  `Theme.UsePaletteForTest`, never through HKCU. It writes `filedo_win_selftest.log` beside the exe
  and exits 0, 1, or 2 (the last means its log could not be written). It is a GUI-subsystem process, so from PowerShell it needs
  `Start-Process .\filedo_win.exe --selftest -Wait -PassThru` to have an exit code at all.
- Add or adjust tests in `cmd\filedo-test/` for user-visible changes that the two Go suites do not reach;
  use `tests\prepare_test_env.cmd` for list-driven scenarios; note any disk, drive-letter, or admin
  requirement in the PR.
- **A verb that works interactively can silently not work from a `.lst` batch file**, because `main()` and
  `executeInternalCommand()` are two separate dispatches. Whatever the verb, its acceptance test is a batch
  run; the interactive path is the secondary check. Note that `runGenericCommand` probes the target with
  `os.Stat` before dispatching, so anything taking a mask target has to be dispatched ahead of it
  (`fdsecDispatchTarget` is the worked example, called from both entry points).
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

Three rules a change here must not break:
- **`Theme.vb` is the only file allowed to name a colour.** No `Color.`, `FromArgb` or `RGB(` anywhere
  else in the shell - and mixing two tokens counts as naming one, which is why `Theme.Blend` is private to
  it and a mix a view needs becomes a named palette role. `cmd/filedo/shell_contract_test.go` makes the
  search on every build. A mix is done in `Integer`: a `Byte` minus a larger `Byte` throws in VB, which is
  what once painted the light theme's selected rail row as a red cross.
- **No fixed coordinates.** Layout comes from `TableLayoutPanel`, `FlowLayoutPanel`, docking and
  anchoring - a `System.Drawing.Point(..)` is exactly what a per-monitor-DPI window must not have.
- **The DPI declaration is two files and both must ship.** `app.manifest` is linked into the exe;
  `app.config` becomes a separate `filedo_win.exe.config` that `build.ps1`, `packaging/wix/FileDO.wxs`,
  `msix/build-msix.ps1` and the release workflow each have to carry. Shipping the manifest without the
  config renders worse than shipping neither, because the window is told the real DPI and then ignores it.

Every user-visible string goes through `Localization.vb` in all five languages (`en`, `ru`, `uk`, `de`,
`fr`), in its `key|value` format - no `|` inside a value, `\n` for a line break, English as the fallback.
Two traps: VB rejects a **comment line inside an array initializer**, and a key added to English alone
will silently fall back rather than fail.

## Coding style
Go defaults via `gofmt` before committing. Keep Windows-specific behavior in `*_windows.go` and
cross-platform fallbacks in `*_unsupported.go`; prefer small, focused files over growing `main.go`.

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
| `FDSEC-FORMAT` - the on-disk format of a `.fd-sec` container, byte for byte | 1.1 | `secure-container/` | [`docs/contracts/FDSEC-FORMAT.md`](docs/contracts/FDSEC-FORMAT.md) |
| `FDSEC-BEHAVIOUR` - everything a port of `secure` / `unsecure` must reproduce: the read-back proof before any disposition of the original, the three outcome classes, credential hygiene, the conformance checklist | 1.1 | `secure-container/` | [`docs/contracts/FDSEC-BEHAVIOUR.md`](docs/contracts/FDSEC-BEHAVIOUR.md) |
| `CLI-EVENT-STREAM` - the `--events` JSON Lines channel, the `--stop-file`, and the rule that a verdict comes from the `result` event and never from an exit code alone | 0.9 draft | `cli-event-stream/` | [`docs/contracts/CLI-EVENT-STREAM.md`](docs/contracts/CLI-EVENT-STREAM.md) |
| `INSTALL-TRUST` - what a user reads in the thirty seconds after Windows warned them about an unsigned build | 1.0 | `install-trust/` | [`docs/contracts/INSTALL-TRUST.md`](docs/contracts/INSTALL-TRUST.md) |
| `CHECK-VERDICT` - the exit code and final machine-readable line emitted by an automated check | 0.10 draft | `automated-checks/` | [`docs/contracts/CHECK-VERDICT.md`](docs/contracts/CHECK-VERDICT.md) |
| `CHECK-BASELINE` - the shrink-only file carrying accepted check debt | 0.9 draft | `automated-checks/` | [`docs/contracts/CHECK-BASELINE.md`](docs/contracts/CHECK-BASELINE.md) |
| `CHECK-PLACEMENT` - the record mapping every check to its runner | 0.10 draft | `automated-checks/` | [`docs/contracts/CHECK-PLACEMENT.md`](docs/contracts/CHECK-PLACEMENT.md) |
| `BUILD-EVIDENCE` - the version carried by an artifact and the gate judging it | 0.9 draft | `automated-checks/` | [`docs/contracts/BUILD-EVIDENCE.md`](docs/contracts/BUILD-EVIDENCE.md) |
| `ICON-SET` - the glyph vocabulary: one meaning, one glyph, one name, on every surface that shows a picture (consumer) | 0.11 draft | `iconography/` | [`docs/contracts/ICON-SET.md`](docs/contracts/ICON-SET.md) |
| `ICON-RENDER` - how a glyph is drawn: colour role, themes, sizes, accessible name, the product mark on system surfaces (consumer) | 0.11 draft | `iconography/` | [`docs/contracts/ICON-RENDER.md`](docs/contracts/ICON-RENDER.md) |
| `ICON-EXTERNAL` - third-party marks, other apps' icons, downloaded pictures (consumer) | 0.9 draft | `iconography/` | [`docs/contracts/ICON-EXTERNAL.md`](docs/contracts/ICON-EXTERNAL.md) |
| `REPO-STAMP` - the canon adoption stamp `.sza-canon.json`, written by the canon's adoption run (producer) | 0.9 draft | `rule-adoption/` | [`docs/contracts/REPO-STAMP.md`](docs/contracts/REPO-STAMP.md) |

What this repository consumes:

| Contract | Version | Catalog folder | Pointer here |
| --- | --- | --- | --- |
| `PAGE-CONTENT` / `PAGE-STYLE` / `SITE-FAMILY-MAP` - what the product page says and in what order, how it looks, and the family footer | 1.1 / 1.1 / 1.1 | `product-web-pages/` | [`docs/contracts/PAGE-CONTENT.md`](docs/contracts/PAGE-CONTENT.md), [`PAGE-STYLE.md`](docs/contracts/PAGE-STYLE.md), [`SITE-FAMILY-MAP.md`](docs/contracts/SITE-FAMILY-MAP.md) |
| `APP-BEHAVIOUR` - what the shell does at the twelve moments a user of any SZA desktop app meets: escapable dialogs, honest progress, consent, confirmation, failure as actions, localized rendering, accessible names, window placement, first run, settings | 0.10 draft | `desktop-app-ux/` | [`docs/contracts/APP-BEHAVIOUR.md`](docs/contracts/APP-BEHAVIOUR.md) |
| `APP-STYLE` - the theme mechanism (system/light/dark, live), one palette table, the role vocabulary, declared out-of-theme surfaces | 0.10 draft | `desktop-app-ux/` | [`docs/contracts/APP-STYLE.md`](docs/contracts/APP-STYLE.md) |
| `REPO-LAYOUT` - the root and `docs/` names a shared tool may address without asking | 0.9 draft | `rule-adoption/` | [`docs/contracts/REPO-LAYOUT.md`](docs/contracts/REPO-LAYOUT.md) |
| `RULE-DELIVERY` - how the canon arrives through the `sza` plugin, and how staleness is judged | 0.9 draft | `rule-adoption/` | [`docs/contracts/RULE-DELIVERY.md`](docs/contracts/RULE-DELIVERY.md) |

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
  test fixture, produced by `FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite1`; the copy in
  the catalog's `secure-container/vectors/` is what an outside implementation checks itself against.
  Regenerate the fixture, then copy it across - never hand-edit either one.

## PR specifics
Beyond the canon's commit and PR conventions: include the manual verification commands you ran with their
output, and attach screenshots for GUI, installer, or docs changes.
