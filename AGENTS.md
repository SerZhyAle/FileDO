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

The plugin ships the enforcement hooks; this repo registers **none of its own** - never hand-wire a local copy
of a canon hook. **Recorded divergences** (canon `AI_USAGE.md`): **`AGENTS.md`** is the only agent-rules file
with content (`CLAUDE.md` is a bare pointer and must never grow guidance); `.claude/` is **git-ignored**, so an
edit to a skill in `.claude/skills/{build,release}/` cannot be committed - say so, never report it as landed.

## Project shape (overlay facts)
Windows-first Go **CLI** (speed test, fake-capacity detection, secure wipe, fill, duplicates) plus a co-shipped
**VB.NET GUI** (`filedo_win_vb/`, `filedo_win.exe`) - one release, one tag, a companion, *not* an edition.
Channels: GitHub Release, winget, Microsoft Store, setup EXE and MSI, own Scoop bucket (release step 7b).

- Four `cmd/<binary>/` mains (`filedo`, `filedo-check`, `filedo-fill`, `filedo-test`) + shared `fileduplicates/`,
  `helpers/`, `fsx/`, `statedir/`; 4 separate `go.mod`. **One toolchain**: root `go.mod`'s `toolchain go1.26.8`;
  any other local Go is refused. Version `yyMMddHHmm`, tag `v<stamp>` (remaps: `RELEASE.md` "Version scheme").
- `winget/` holds the three manifests and **nothing else** (`winget validate` parses every file there).
- **Frozen anchors** (changing one orphans installs or user data): winget `PackageIdentifier SerZhyAle.FileDO`
  and the 5 `PortableCommandAlias` names; MSI `UpgradeCode 4d6b3b1f-7c8e-4a25-9f1d-8e3b2c5a7d91` +
  `HKLM\Software\FileDO`; bundle `UpgradeCode 033d2348-b26c-477f-8885-c68d9cfff47f`; component GUIDs in
  `FileDO.wxs` (a component keeps its GUID for the product's life); MSIX Identity `Name` **`SZA.FileDO`** and
  `Publisher` **`CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD`** (Store ID `9PH1LPCMRG83`); Go module `filedo`;
  document type id `FileDO.SecureContainer`; extension `.fdd`, ProgId `FileDO.DiskContainer`, MSI feature
  `DiskContainerIntegration`; GPT partition type GUID `6AFB315B-8976-4841-84EC-D30AFA89A33D`.

## Where the detail lives
<!-- canon-ok: the canon owns "a build is not a release"; what is repo-specific is which of the two
     scripts holds the tag, and that is the fact an agent has to get right here. -->
**Read the named [`ENGINEERING.md`](ENGINEERING.md) section before touching that area.** The full text of this
file's short rules is there too, each in its area section or under a "(full text)" heading at the end.

- **Installer, Explorer menu** ("What the installer does"): the `File DO..` menu has **three writers** -
  `packaging/wix/FileDO.wxs`, `cmd/filedo/fdsec_register*.go`, `shellext/FileDOShell.cpp` - changed in one commit
  (`registry_parity_test.go`, `shellext_contract_test.go`). Component GUIDs, the CLSID, the bundle `UpgradeCode`,
  `FileDO.RegisteredBy`-marked removal and HKLM-wins are anchors.
- **`cmd/filedo`** ("Code map"): `dispatchLine` is the one verb switch; `runGenericCommand` handles the four target
  verbs only and probes with `os.Stat` first, so a mask target goes through `fdsecDispatchTarget` ahead of it.
  `outcome.go` alone gives verdict and exit code: a verb records, never exits; wrap a judgement about the target
  with `defectf`, or it reports "could not judge". Build on the shared mechanisms, never beside them: stop
  (`interrupt.go`), path identity (`fsx`), atomic no-replace writes, environmental errors are "could not verify",
  runtime state in `statedir`. The fake-capacity engine exists once (`main_types.go`, `capacity_*.go`); a defect
  keeps its test files as evidence; a speed anomaly alone is never a verdict.
- **`fdsec`** ("Code map"): containers carry **no marker** - "wrong password", "not a container" and "damaged
  head" are one outcome; read the reveal-sandbox, sealed-name and password-by-environment rules first. **Never
  spell a path `secret`** (`.gitignore` swallows it silently); use `fdsec` or `fd-sec`.
- **Companions** are thin launchers over `filedo.exe`; a fill, test or check fix lands in `cmd/filedo` only.
  Shared packages are main-module only, so the launcher files are copied and held by a test.
- **Build is not release** ("Build / release"): `build.ps1` contains no `git tag`; `release.ps1` is the only thing
  that tags `v*`. Build output is amd64 exactly as CI builds it; `-Resume` never cuts a new version.
- **Release ledger** `docs/releases/` (`RELEASE.md` "The release ledger"): written only through
  `packaging/release-ledger.ps1`; `channels.json` is the one file edited by hand.
- **Store** `msix/` ("Store channel"): no package from placeholders, `SZA.FileDO` is frozen, a test build cannot
  pass for a Store build, nothing in `msix/` publishes.
- **GUI** ("The GUI shell"): `Theme.vb` alone names a colour; no fixed coordinates; every glyph is a vendored
  catalog drawing; both DPI files ship; every string goes through `Localization.vb` in five languages; no
  exception text on screen; every question is a `ShellDialog` whose safe answer is the default.
- **In-app help** ("The GUI shell"): F1 and `?` go through `Help.vb` only; `docs/help` pages and `img/` are
  **generated** - edit `HelpContent.vb` or the `help_*` strings, never a page; a new window or rail destination
  needs a row in `docs/help/help-map.json`; `docs/help/help-baseline.txt` only falls.
- **Product pictures** ("The product's pictures"; `PRODUCT-IDENTITY`): never edit or add one by hand - change the
  drawing in `assets/make-product-icons.ps1` and rerun; `-Check` fails the fast gate.
- **Site** ("Site"): **full device width, never a centred column** (owner rule; read `PAGE-STYLE` section 1 before
  page CSS; the contract's kit stays byte-identical). Never hand-edit generated parts (`docs/sitemap.xml`, share images, install matrix, command
  reference - re-render it after a README Command Reference edit). Release asset names are the download buttons'
  contract; the installer is **not code-signed** and every download surface says so.
- **Site and listing text** ("Site"): an EN edit on the site or `README.md` fails
  `packaging/check-external-docs.ps1` until translations are updated and `-Record` re-records; a new `.md`/`.html`
  needs a registry record; a site address written outside the site also goes in
  `packaging/site-held-addresses.json`; a moved page leaves a forwarder; pillar order and tag/claim deny-lists live
  in `packaging/positioning-source.json` - change the source first, then every surface.
- **CLI surface** ("App commands"): `docs/CLI.md` is **generated** from `docs/cli-commands.json`, never
  hand-edited; a new command is a manifest row first (`APP-COMMANDS`). Agents read it and run
  `filedo commands --json`; for product state prefer `filedo status`, `filedo job list`, `filedo vd status json`
  to a screenshot.

## Testing
- **Between releases nothing slower than 5 s runs** (canon `TESTING_AND_QA` section 2): no full build, no
  `go test`, no `--selftest`, no slow gate step, unless the owner names that run (`/build`, `run.bat`) or a release
  runs it. Run **`build.ps1 -Fast`** (builds nothing; ends `fast-gate: PASS|FAIL|COULD NOT VERIFY`), static
  inspection and a direct call of what changed (`tests/prove-operations-selftest.ps1` is fast: run it when the
  operations thresholds change). Release tier: `check-third-party-notices`, `check-gated-packages`,
  `check-external-docs` (`-Record` stays part of an EN edit) and any check with no recorded time. A slower check
  skipped during a ticket is a `RELEASE` item in its audit naming that command, and the report says "not built,
  not tested until the release".
- Release: **`release.ps1`** - the gate `build.ps1 -Test` (twelve steps; exit **0 = pass**, **1 = a defect**,
  **2 = could not verify**, nothing proven) plus the full fdsec run, `govulncheck` and the operations run
  `tests\prove-operations.ps1`. A new verb or changed result number goes into its inventory with the change.
- Root **`go test ./...` is known-broken** - not the gate. `go test ./cmd/filedo/` needs **`-vet=off`**
  (shrink-only `cmd/filedo/vet-baseline.txt`); test `cmd/filedo-test` from inside its module; `go test ./fdsec/`
  and `./vdisk/` run plainly.
- Shell gate: `Start-Process .\filedo_win.exe --selftest -Wait -PassThru` (GUI-subsystem process).
- A verb's acceptance test is still a batch run. `tests\*.lst` name placeholders and run only through
  `tests\run-test-list.ps1`; **never point them at a real data volume**.
- **Destructive-tool safety is the pass/fail line, not friction.** `wipe` must demand typing `WIPE`; `--force` /
  `-y` skips only the *prompt*, never the safety checks on drive or share roots, reparse points and system TEMP;
  writes at `C:` redirect to `%TEMP%\FileDO_Operations`. The duplicate-finder hash cache keys on path + size +
  **modtime**.

## Private verification artifacts
Screenshots of this machine, desktop captures, raw logs, dumps, recordings and one-off capture scripts go only to
the git-ignored `temp/evidence/<run-id>/` (dated, unique), passed explicitly to capture tools - never to a tracked
directory or `tests/`. **Never stage, force-add, attach to a PR/issue or upload raw evidence**; a tracked summary
carries no personal data and no link to local evidence. Inspect any image for personal paths, names, devices,
other windows and credentials before publishing; site and Store assets are synthetic-data captures only.

## Coding style
Go defaults via `gofmt` before committing. `cmd/filedo` is **Windows-only** (owner decision D5,
2026-09-25: no supported channel targets another OS), so it carries no `*_unsupported.go` fallbacks; keep
Windows-specific behavior in `*_windows.go` anyway, and prefer small, focused files over growing `main.go`.

## Working documents (`PLAN/`)
`PLAN/` is local and git-ignored: **never `git add -f` a working document, never link to one from a tracked
file**. A spec is `PLAN/FILEDO-XXXX_name.md`; its plans, questionnaires and research live in
`PLAN/FILEDO-XXXX_name/`; implemented and proven, it moves with folder and id to `PLAN/done/`. Ids are `FILEDO-`
plus four digits, never reused or renumbered, next = one past the highest anywhere under `PLAN/` (`done/`
included); tickets closed before 2026-11-19 keep `SP-####`. State is the spec's `Status:` line plus its folder
(`PLAN/README.md` carries no status). Material with no spec goes to `PLAN/notes/` and gets no id.

## External contracts (`P:\Contracts\`)
A contract that outlives this repository - a file format, a wire or event protocol, a behaviour at a
boundary, anything a second project of ours or an outside developer implements against - is **not** kept
in this repository. It lives in `P:\Contracts\`, the shared contracts catalog: **one folder per function**,
never per product, outside git and outside every product. `P:\Contracts\README.md` is the index,
`P:\Contracts\_meta\RULES.md` is the law it runs by, `_meta\VERSIONING.md` is the compatibility law every
consumer path here obeys, and `_meta\REGISTRY.md` is where this product's rows and its dated exceptions
live. **This line is the only place in the repository that says where the catalog is.**

The table of what we produce or consume is [`docs/contracts/README.md`](docs/contracts/README.md), one pointer
file per contract; versions live in the pointers only. The contract changes before the code; comply or amend,
never deviate in silence (canon `CONTRACTS.md`).

- **Cite by id, point in one place**: "(FDSEC-FORMAT.md section 8)", "(CLI-EVENT-STREAM rule 10)"; tracked files
  never link to a contract or name its path. One found not to bind us is listed, dated, at the end of that README.
- **The vectors are the arbiter and are generated**: never hand-edit `fdsec/testdata/vectors/` or the catalog copy;
  regenerate the fixture, then copy it across (commands in `ENGINEERING.md`).

## PR specifics
Beyond the canon's commit and PR conventions: include the manual verification commands you ran with their
output. For GUI, installer, or docs changes, attach only privacy-reviewed screenshots made with
synthetic data; raw local verification captures stay in the project's ignored evidence directory above.
