# FileDO - Build & Release

Two flows, deliberately separated so a routine local build can never publish
anything or spend a CI run.

| | **BUILD** ("сборка") | **RELEASE** ("релиз") |
| --- | --- | --- |
| Goal | compile, test locally, commit | publish: GitHub, winget, Store |
| Script | `.\build.ps1` | `.\release.ps1` |
| Runs CI? | **No** | Yes (tag `v*` triggers it) |
| Creates a tag? | **Never** | Yes - the only place that does |
| Cost | free, local only | a CI run + public artifacts |

The dividing line is the **`v*` git tag**. Pushing it is the single action that
starts `.github/workflows/release.yml`. Only `release.ps1` ever does that, so
you can build and commit all day without releasing anything.

> Note: this repo is **public**, so GitHub Actions minutes are free. The cost of
> a release is not money - it is the irreversibility of a public tag + GitHub
> Release (awkward to retract), plus the downstream winget/Store fan-out.

## Version scheme

One stamp everywhere: `yyMMddHHmm` (e.g. `2606271600` = 2026-06-27 16:00).
The git tag is `v` + that stamp. The MSIX Store package remaps it to a 4-part
`YY.(M*100+D).HHmm.0` (Store requires 4 parts, each <= 65535) - see
`msix/build-msix.ps1`.

---

## BUILD - `build.ps1`

Local only. Builds all five executables into `exe_to_download\`.

```powershell
.\build.ps1                       # build everything (default)
.\build.ps1 -Test                 # build, then smoke-run + go test gate
.\build.ps1 -Test -Commit "msg"   # build + test, commit ONLY if both pass
.\build.ps1 -SkipGui              # Go variants only (no MSBuild needed)
```

- `-Test` runs the test gate: smoke-run `filedo.exe -?` (asserts it prints the
  stamped version) + `go test ./cmd/filedo-test`. (Root `go test ./...` is
  known-broken per `AGENTS.md`, so it is intentionally not run.)
- `-Commit` implies `-Test` and commits the working tree only after the gate
  passes. It never tags and never pushes.
- For a quick single-binary dev loop, `build.bat` still builds just
  `filedo.exe` with `-race`.

**This script must never tag or push.** That rule is what keeps a build free.

---

## RELEASE - `release.ps1`

Full pipeline. Cuts from `main` only, with a clean/committed tree.

```powershell
.\release.ps1                 # version = now (yyMMddHHmm)
.\release.ps1 -Version 2606271600
.\release.ps1 -DryRun         # everything EXCEPT push tag / submit / Store
.\release.ps1 -SkipStore      # CLI + winget only
.\release.ps1 -SkipWinget     # skip winget sync + submit
```

What it does, in order:

1. **Preflight** - tools present (`git`, `gh`, `go`, `wingetcreate`), `gh`
   authenticated, on `main`, tag not already used.
2. **Gate** - `build.ps1 -Test`. Fails *before* anything is tagged.
3. **Commit** - commits refreshed binaries (tracked in `exe_to_download\`) if
   the build changed them.
4. **Push** - `git push origin main`, then create + push `v<version>`.
   **This is the point of no return** - it triggers the GitHub release.
5. **Wait** - polls until the GitHub Release and its `.zip.sha256` asset exist.
6. **winget** - downloads the SHA256, rewrites `winget/*.yaml`
   (`PackageVersion`, `InstallerUrl`, `InstallerSha256`, `ReleaseDate`,
   `ReleaseNotesUrl`), commits `Sync winget/ to v<version>`, pushes.
7. **Submit** - `wingetcreate submit` opens the PR to `microsoft/winget-pkgs`.
8. **Store** - builds the MSIX into `msix\out\`. For a real Store identity pass
   `-StoreIdentityName/-StorePublisher/-StorePublisherDisplayName` (values from
   Partner Center).
9. **Checklist** - prints what is done and the one manual step left.

### The one manual step

**Microsoft Store upload.** Partner Center has no publishing CLI, so uploading
the unsigned `msix\out\FileDO_<ver>.msix` is manual (Microsoft re-signs it
during certification - no paid cert needed). See `msix/README.md`.

### Prerequisites for a release

- `gh` authenticated (`gh auth login`) - used for push, release polling,
  asset download, and the winget submit token.
- `wingetcreate` - `winget install Microsoft.WingetCreate`.
- For the MSIX: Windows SDK (`makeappx`) + VS Build Tools (MSBuild) - see
  `msix/README.md`. Skip with `-SkipStore` if unavailable.

### If something fails mid-release

- **Tag pushed, CI failed** - fix, then re-dispatch the workflow from the
  Actions tab for the same tag (the workflow serializes per-tag), or delete the
  tag/release and re-run `release.ps1` with a new `-Version`.
- **winget submit failed** - re-run manually:
  `wingetcreate submit --token (gh auth token) winget`.
- **Verify after merge** - `winget show SerZhyAle/FileDO`.
