# Repository Guidelines

## Shared rules (canon)
FileDO follows the **SZA Unified Rules** - the cross-project source of truth, read-only from a work
session:

`P:\WEB\sites.google.comsiteszaodua\Unified_Rules` (start at `README.md`, then Overlay A + the Go-CLI
notes). This repo's per-project record - overlay facts, channel rows, and every recorded divergence - is
`contrib/filedo.md` there. Consume by **reference**: the canon owns the universal rules (evidence over
confidence, the build/release wall, git/commit discipline, the testing evidence ladder); only
FileDO-specific deltas live below. Rule fixes go back to the canon in their own session, never from here.

Recorded divergences (canon `AI_USAGE.md`): this repo's only agent-rules file is **`AGENTS.md`** (no
`CLAUDE.md`); the in-repo skills under `.claude/skills/{build,release}/` are **git-ignored** - local-only,
not team-shared through git.

## Project shape (overlay facts)
Windows-first Go **CLI** (storage speed test, fake-capacity/counterfeit-flash detection, secure wipe,
fill, duplicate management) plus a co-shipped **VB.NET GUI** (`filedo_win_vb/`, `filedo_win.exe`) - one
release, one tag, a companion binary, *not* a separate edition. Distributed on the desktop channels
(GitHub Release + winget + Microsoft Store) plus a direct-download MSI.

- **Source root.** Four `cmd/<binary>/` mains (`filedo`, `filedo-check`, `filedo-fill`, `filedo-test`) +
  shared top-level packages `fileduplicates/`, `helpers/`, `capacitytest/`. **Multi-module**: 4 separate
  `go.mod`/`go.sum`, mixed Go versions (1.24.4 / 1.21). Module path `filedo` (a frozen anchor).
- **Version shape.** Separator-less `yyMMddHHmm` (e.g. `2606120121`) - a sortable 10-digit integer. Git
  tag `v<stamp>`, stamped via `-ldflags "-X main.version="`. Remapped mechanically for MSIX and the PE
  `VS_VERSIONINFO` only.
- **Release-mechanics** are top-level channel siblings (no `publishing/` umbrella): `winget/`, `msix/`,
  `packaging/wix/`, committed `exe_to_download/`.
- **Frozen anchors** (reserve once; changing orphans installs): winget `PackageIdentifier
  SerZhyAle.FileDO`; WiX MSI `UpgradeCode 4d6b3b1f-7c8e-4a25-9f1d-8e3b2c5a7d91` (+ `HKLM\Software\FileDO`);
  MSIX Identity `Name`/`Publisher`; Go module `filedo`; the 5 winget `PortableCommandAlias` names.

## Build / release (the two-flow wall)
`build.ps1` and `release.ps1` are the two halves, and the wall between them is **structural**:

- **`.\build.ps1`** = BUILD ("сборка"): compile all four exes into `exe_to_download/`, optionally test,
  optionally commit. **HARD RULE: never creates or pushes a `v*` tag** - it spends no CI and ships nothing.
- **`.\release.ps1`** = RELEASE ("релиз"): the **only** thing that tags `v*` and triggers GitHub CI, then
  fans out to winget (`wingetcreate submit`) and an MSIX build; the Microsoft Store step is a manual
  Partner Center upload.
- Single-target builds: `go build -o filedo.exe .\cmd\filedo` (main CLI); each companion tool builds from
  its own `cmd\filedo-*` directory.

Prefer the in-repo skills for these flows: `/build` and `/release`.

## Testing
- Root **`go test ./...` is known-broken** (existing `fmt`/vet debt) - do **not** treat it as the gate. The
  real gate (in `build.ps1`) is a scoped `go test ./cmd/filedo-test` plus a smoke-run asserting the built
  exe prints the stamped version. This known-red is tracked on purpose so a real regression is not masked.
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

## Commit & PR
English, short imperative subjects naming the affected area (`Fix WiX icon path`, `Add MSI installer`,
`Sync winget/ ...`); add a co-author trailer. PRs: a brief summary, the manual verification commands you
ran with their output (no "done" without a fresh run and its evidence), linked issues, and screenshots for
GUI, installer, or docs changes. Full git/release discipline lives in canon `GITHUB_INTERACTION.md`.
