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
(GitHub Release + winget + Microsoft Store) plus a direct-download MSI.

- **Source root.** Four `cmd/<binary>/` mains (`filedo`, `filedo-check`, `filedo-fill`, `filedo-test`) +
  shared top-level packages `fileduplicates/`, `helpers/`, `capacitytest/`. **Multi-module**: 4 separate
  `go.mod`/`go.sum`, mixed Go versions (1.24.4 / 1.21). Module path `filedo` (a frozen anchor).
- **Version shape.** Separator-less `yyMMddHHmm` (e.g. `2606120121`) - a sortable 10-digit integer. Git
  tag `v<stamp>`, validated by `release.ps1` as `^\d{10}$` before it prefixes the `v`, and stamped into
  the binaries via `-ldflags "-X main.version="`. Remapped mechanically for MSIX and the PE
  `VS_VERSIONINFO` only.
- **Release-mechanics** are top-level channel siblings (no `publishing/` umbrella): `winget/`, `msix/`,
  `packaging/wix/`, committed `exe_to_download/`.
- **Frozen anchors** (reserve once; changing orphans installs): winget `PackageIdentifier
  SerZhyAle.FileDO`; WiX MSI `UpgradeCode 4d6b3b1f-7c8e-4a25-9f1d-8e3b2c5a7d91` (+ `HKLM\Software\FileDO`);
  MSIX Identity `Name`/`Publisher`; Go module `filedo`; the 5 winget `PortableCommandAlias` names.

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

**Shared packages are main-module only.** `fileduplicates/`, `helpers/` and `capacitytest/` are imported
as `filedo/...` and are reachable only from `cmd/filedo`. The three companion binaries are separate
modules (`filedo_check`, `filedo_fill`, `filedo_test`) with no `replace` back to the root, so whatever
they need is copy-pasted into their own directory - that duplication is structural, not an oversight.

## Build / release (which script is which)
<!-- canon-ok: the canon owns "a build is not a release"; what is repo-specific is which of the two
     scripts holds the tag, and that is the fact an agent has to get right here. -->
`build.ps1` and `release.ps1` are the two halves, and the wall between them is **structural**:

- **`.\build.ps1`** = BUILD ("сборка"): compile all four exes into `exe_to_download/`, optionally test,
  optionally commit. It contains **no `git tag` call at all** - that is the enforcement, not a convention.
- **`.\release.ps1`** = RELEASE ("релиз"): the **only** thing that tags `v*` (lines 130-131) and triggers
  GitHub CI, then fans out to winget (`wingetcreate submit`) and an MSIX build; the Microsoft Store step is
  a manual Partner Center upload. It re-runs `build.ps1 -Test` first and aborts on any non-zero exit.
- Single-target builds: `go build -o filedo.exe .\cmd\filedo` (main CLI); each companion tool builds from
  its own `cmd\filedo-*` directory. The GUI is MSBuild, not Go: `MSBuild filedo_win_vb\FileDOGUI.vbproj
  /p:Configuration=Release /p:Platform=AnyCPU` (locate it with `vswhere -latest -find
  "MSBuild\**\Bin\MSBuild.exe"`); `.\build.ps1 -SkipGui` drops it when VS Build Tools are absent.

Both are PowerShell and both must be invoked through the PowerShell tool, or from Bash as
`pwsh -NoProfile -File ./build.ps1 -Test`. Prefer the in-repo skills `/build` and `/release`.

## Testing
- Root **`go test ./...` is known-broken** (existing `fmt`/vet debt) - do **not** treat it as the gate. This
  known-red is tracked on purpose so a real regression is not masked.
- The real gate is `build.ps1 -Test`: a smoke-run asserting the freshly built exe prints the stamped
  version, plus a compile-check of the test module. Its exit codes are **1 = a defect was found** and
  **2 = the gate could not verify** (Go missing from `PATH`, or the built exe absent). Exit 2 is not a
  pass and not a defect report; treat it as "nothing was proven".
- `cmd/filedo-test` is **its own module**, so the compile-check must run from inside it
  (`cd cmd\filedo-test; go test ./...`). Running `go test ./cmd/filedo-test` from the repo root fails with
  *main module (filedo) does not contain package* - a trap worth knowing, because the console line
  `build.ps1` used to print named exactly that failing form.
- The repo has **no `func Test*` anywhere**, so the run reports `[no tests to run]` and is purely a compile
  check; `go test ./... -run <Name>` only starts meaning something once the first real test lands.
- Add or adjust tests in `cmd\filedo-test/` for user-visible changes; use `tests\prepare_test_env.cmd` for
  list-driven scenarios; note any disk, drive-letter, or admin requirement in the PR.
- **Destructive-tool safety is the pass/fail line, not friction.** For `wipe`/`fill` the persona test
  inverts: `wipe` must demand typing `WIPE`; `--force`/`-y` skips only the *prompt*, never the safety
  checks on drive/share roots, reparse points, and system TEMP; writes at `C:` redirect to
  `%TEMP%\FileDO_Operations`. The duplicate-finder hash cache must key on path + size + **modtime** (size
  alone once let an edited file be deleted as a false duplicate - now a permanent invariant).

## Coding style
Go defaults via `gofmt` before committing. Keep Windows-specific behavior in `*_windows.go` and
cross-platform fallbacks in `*_unsupported.go`; prefer small, focused files over growing `main.go`.

## Site (`docs/`)
Hand-authored, **no generator** - so the pages are edited in place and are not render targets. The tree
index, the canonical page list and the redirect-stub rule are in [`docs/README.md`](docs/README.md); adding
a public page means editing `docs/sitemap.xml` in the same commit.

## Working documents (`.specs/`)
Specs, implementation plans, questionnaires and research notes live in `.specs/`, which `.gitignore`
keeps out of git. What the repository carries is documentation of the finished product, written for an
experienced user - the site, the READMEs, the listing sources. Never `git add -f` a working document,
and never link to one from a tracked file: whoever clones this repository does not have it.

## PR specifics
Beyond the canon's commit and PR conventions: include the manual verification commands you ran with their
output, and attach screenshots for GUI, installer, or docs changes.
