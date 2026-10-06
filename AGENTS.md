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
  `go.mod`'s `toolchain go1.26.8` is what every channel builds with - `release.yml` installs it,
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
  document type id **`FileDO.SecureContainer`**; the disk container's extension **`.fdd`**, its ProgId
  **`FileDO.DiskContainer`** and its MSI feature id **`DiskContainerIntegration`** (SP-0004 spec 6.2: an
  association in users' registries outlives any rename, and a renamed feature drops the user's choice
  across an upgrade); the **FileDO GPT partition type GUID `6AFB315B-8976-4841-84EC-D30AFA89A33D`**
  ("FileDO disk container", FDD-FORMAT section 3.1: every partition carrier on a user's disk carries it, so a
  changed value would orphan them); and the fixed component GUIDs in `FileDO.wxs` (an MSI
  component keeps its GUID for the product's life, or an upgrade leaves the old copy installed); the
  **bundle `UpgradeCode 033d2348-b26c-477f-8885-c68d9cfff47f`** in `FileDO-bundle.wxs` - a separate
  identity from the MSI's, and what lets a setup EXE replace the previously installed one instead of
  adding a second entry to Apps and features.

## Where the detail lives
<!-- canon-ok: the canon owns "a build is not a release"; what is repo-specific is which of the two
     scripts holds the tag, and that is the fact an agent has to get right here. -->
The detail behind each area is in [`ENGINEERING.md`](ENGINEERING.md) (an internal document, registered in
`docs/DOCUMENT_REGISTRY.jsonl`); read its section before editing that area. One line per trap:

- **Installer and Explorer menu** ("What the installer does"): the `File DO..` menu has **three writers** -
  `packaging/wix/FileDO.wxs`, `cmd/filedo/fdsec_register*.go`, `shellext/FileDOShell.cpp` - and they change in
  one commit; `registry_parity_test.go` and `shellext_contract_test.go` hold them together. Component GUIDs,
  the CLSID, the bundle `UpgradeCode`, `FileDO.RegisteredBy`-marked removal and HKLM-wins are anchors.
- **Two dispatch layers** ("Code map"): `dispatchLine` is the one verb switch; `runGenericCommand` handles
  the four target verbs only and probes with `os.Stat` first, so a mask target goes through
  `fdsecDispatchTarget` ahead of it.
- **One verdict and one exit code** ("Code map"): `outcome.go` alone; a verb records, never exits. A judgement
  about the target must be wrapped with `defectf`, or it reports as "could not judge".
- **Shared mechanisms** ("Code map"): stop (`interrupt.go`), path identity (`fsx`), atomic no-replace writes,
  environmental errors are "could not verify", runtime state in `statedir`. Build on them, never beside them.
- **The fake-capacity engine exists once** (`main_types.go`, `capacity_*.go`); a defect keeps its test files as
  evidence; a speed anomaly alone is never a verdict.
- **`fdsec` containers carry no marker**, so "wrong password", "not a container" and "damaged head" are one
  outcome; the reveal sandbox, the sealed-name screening and the password-by-environment rules are in "Code map".
  Never spell a path `secret` (`.gitignore` swallows it silently); use `fdsec` or `fd-sec`.
- **Companions are thin launchers** over `filedo.exe`; a fill, test or check fix lands in `cmd/filedo` only.
  Shared packages are main-module only, so the launcher files are copied and held by a test.
- **Build is not release** ("Build / release"): `build.ps1` contains no `git tag`; `release.ps1` is the only
  thing that tags `v*`. Build output is amd64 exactly as CI builds it; `-Resume` never cuts a new version.
- **Store channel** ("Store channel"): no package from placeholders, `SZA.FileDO` is frozen, a test build cannot
  pass for a Store build, and nothing in `msix/` publishes.
- **GUI shell** ("The GUI shell"): `Theme.vb` alone names a colour; no fixed coordinates; every glyph is a
  vendored catalog drawing; both DPI files ship; every string goes through `Localization.vb` in five languages;
  no exception text on screen; every question is a `ShellDialog` whose safe answer is the default.
- **Site** ("Site"): hand-authored, no generator; release asset names are the download buttons' contract, and
  the installer is **not code-signed**, which every download surface says.

## Testing
- Root **`go test ./...` is known-broken** (existing `fmt`/vet debt) - do **not** treat it as the gate. The
  real gate is **`build.ps1 -Test`**, ten steps (the list is in "Testing - the gate in full"); its last line is
  `build-gate <stamp>: PASS`, `FAIL` or `NOT VERIFIED`, and its exit codes are **0 = pass**, **1 = a defect was
  found**, **2 = it could not verify** (a missing prerequisite). Exit 2 is "nothing was proven".
- `go test ./cmd/filedo/` needs **`-vet=off`**: package main carries recorded vet debt
  (`cmd/filedo/vet-baseline.txt`, shrink-only). `cmd/filedo-test` is its own module - run its tests from
  inside it. `go test ./fdsec/` and `./vdisk/` are vet-clean and run plainly.
- **Every operation, run on small data, is the other pre-release step**: `tests\prove-operations.ps1`
  (called by `release.ps1` before the tag; "The operations run" in `RELEASE.md`). Its mounted-disk tier needs
  elevation (one UAC prompt, or `release.ps1 -SkipOpsMount`). It compares itself with the last passing runs
  (a vanished step or a changed result fails; a confirmed loss of speed in its load section fails;
  `release.ps1 -OpsRebaseline` accepts an intended change) and fails on a verb in `filedo -?` that its
  inventory does not know: a new verb or result number is added to it with the change.
- **The shell has its own gate**: `filedo_win.exe --selftest` (rows named in "The GUI shell"), run with
  `Start-Process .\filedo_win.exe --selftest -Wait -PassThru` because it is a GUI-subsystem process.
- **An EN edit on the site or in `README.md` fails `packaging/check-external-docs.ps1`** until the translations
  are updated and `-Record` re-records the fingerprints; a new `.md`/`.html` needs a record in
  `docs/DOCUMENT_REGISTRY.jsonl` or the internal-docs step fails.
- **Every site page uses the full device width - never a centred column** (owner rule, `PAGE-STYLE` section 1
  "Use the available width"; the kit stays byte-identical, the override lives in `guides/guide.css` and the
  trailing block of each inline `<style>`; no px `max-width` caps on hero, lead, intro or sections). The
  landing's header carries the mark, the full name, Guides/Documentation, one language group; its footer the
  author and the other SZA tools. Pinned by `TestSurfaces_ThePagesUseTheFullWidth` and
  `TestSurfaces_TheFrontPageCarriesItsIdentity`; read the contract before touching page CSS.
- **A site address written outside the site** (README, Store listing source, workflow, `Links.vb`) goes in
  `packaging/site-held-addresses.json` too, or `check-external-docs.ps1` fails; a page that moves leaves a forwarder.
- **What the product is for is written once, in order**: `packaging/positioning-source.json` (check, tidy and move,
  erase, protect, virtual disks). The landing and its de/fr pages, the guides hub, the five READMEs, the Store listing
  source and the winget manifest list those pillars in that order or name the first ones; `check-external-docs.ps1`
  (step `positioning`) fails on a surface out of order. Change the order in the source first, then in every surface.
- A verb's acceptance test is still a batch run (`.lst`): the batch adds its own tokenizer and stop rule.
  List-driven scenarios (`tests\*.lst`) name placeholders and run only through `tests\run-test-list.ps1`;
  never point them at a real data volume.
- **Destructive-tool safety is the pass/fail line, not friction.** `wipe` must demand typing `WIPE`;
  `--force` / `-y` skips only the *prompt*, never the safety checks on drive or share roots, reparse points and
  system TEMP; writes at `C:` redirect to `%TEMP%\FileDO_Operations`. The duplicate-finder hash cache keys on
  path + size + **modtime**.

## Private verification artifacts

Screenshots of this machine, desktop or Explorer captures, raw test logs, dumps, recordings and
one-off capture scripts are local working material. Store them in this project's git-ignored
`temp/evidence/<run-id>/` directory (a dated, unique run id), relative to the repository root.
Never create or accumulate them in `tests/evidence/`,
`tests/`, the repository root, or another tracked directory. `tests/` is for reusable tests,
fixtures and checklists. Pass the project's temporary output directory explicitly to capture tools;
for example, `msix/make-screenshots.ps1 -Live -OutDir temp/evidence/<run-id>` for manual proof.

Raw evidence stays private: never stage it, force-add an ignored artifact, attach it to a PR/issue,
or upload it as a release asset. A tracked verification summary may record the build, commands,
exit codes, findings and a run id, after removing personal data; do not link to local evidence
from tracked documents or register it in `docs/DOCUMENT_REGISTRY.jsonl`. Before publishing any
image, inspect it for personal paths, filenames, account/device names, other windows and credentials.
Only deliberate product assets for the site or Store, captured with synthetic data and checked
for privacy, belong in their established asset directories. Do not use those directories for
manual verification output. `.gitignore` excludes `temp/`, plus `tests/evidence/` and `tmp/` as fallbacks;
ignore rules do not remove files already tracked by Git or erase prior commits.

## Coding style
Go defaults via `gofmt` before committing. `cmd/filedo` is **Windows-only** (owner decision D5,
2026-09-25: no supported channel targets another OS), so it carries no `*_unsupported.go` fallbacks; keep
Windows-specific behavior in `*_windows.go` anyway, and prefer small, focused files over growing `main.go`.

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
never per product, outside git and outside every product. `P:\Contracts\README.md` is the index,
`P:\Contracts\_meta\RULES.md` is the law it runs by, `_meta\VERSIONING.md` is the compatibility law every
consumer path here obeys, and `_meta\REGISTRY.md` is where this product's rows and its dated exceptions
live. **This line is the only place in the repository that says where the catalog is.**

What this repository produces or consumes - each contract's id, version, role and what we owe it - is the
table in [`docs/contracts/README.md`](docs/contracts/README.md), one pointer file per contract beside it;
the versions live in the pointers and nowhere else. The universal law (the contract changes before the code,
comply or amend, never deviate in silence) is the canon's `CONTRACTS.md`. Two rules are this repository's:

- **Cite by id, point in one place.** The comments in `fdsec/*.go` say "(FDSEC-FORMAT.md section 8)" and
  nothing more; elsewhere it is "(CLI-EVENT-STREAM rule 10)". Tracked files may cite a contract; they must not
  link to it or name its path - whoever clones this repository does not have `P:\`. A contract read against
  this repository and found not to bind it is listed, dated, at the end of the `docs/contracts/README.md`.
- **The vectors are the arbiter, and they are generated.** `fdsec/testdata/vectors/` is this repository's
  test fixture, produced by `FDSEC_WRITE_VECTORS=1 go test ./fdsec -run "TestVectors_Suite1|TestVectors_Suite3"`
  (suite 3 in its `suite3/` subfolder); the copy in the catalog's `secure-container/vectors/` is what an outside
  implementation checks itself against. Regenerate the fixture, then copy it across - never hand-edit either one.

## PR specifics
Beyond the canon's commit and PR conventions: include the manual verification commands you ran with their
output. For GUI, installer, or docs changes, attach only privacy-reviewed screenshots made with
synthetic data; raw local verification captures stay in the project's ignored evidence directory above.
