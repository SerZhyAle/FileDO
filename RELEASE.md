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
The git tag is `v` + that stamp, and the stamp must be a real date. Three
numeric version fields are derived from it mechanically, because they cannot
hold it verbatim:

| Where | Mapping | 2609241700 becomes |
| --- | --- | --- |
| PE `VS_VERSIONINFO` of every exe | `yy.M.d.HHmm` | `26.9.24.1700` |
| MSI `ProductVersion` and the setup EXE | `yy.M.((d-1)*1440 + H*60 + m)` | `26.9.34140` |
| MSIX `Identity/Version` | `yy.(M*100+d).HHmm.0` | `26.924.1700.0` |

The MSI mapping carries the minute of the month in its third field because
Windows Installer compares only the first three: under the old `yy.M.d.HHmm`
two releases on one day were the same version, and the older MSI stayed
installed beside the newer one. The new third field is at least `(d-1)*1440`,
so every release after `v2609241700` compares greater than it, and
`release.ps1` still reads the last published MSI and refuses a triple that is
not greater (SP-0030 PKG-01). The MSIX mapping is in `msix/build-msix.ps1`
(the Store requires 4 parts, each <= 65535).

---

## BUILD - `build.ps1`

Local only. A plain run builds **everything that is distributable**: the five
executables into `exe_to_download\`, and both installer artifacts into `dist\`.
No switch is needed for that - the switches below only subtract work, demand
the installer, or add the test gate.

```powershell
.\build.ps1                       # exes + installer (the normal case)
.\build.ps1 -Test                 # ..and run the smoke-run + go test gate
.\build.ps1 -Test -Commit "msg"   # ..and commit, ONLY if both pass
.\build.ps1 -SkipGui              # Go variants only (no MSBuild needed)
.\build.ps1 -SkipInstaller        # no dist\ artifacts (fast inner loop)
.\build.ps1 -Install              # build, then RUN the installer here
.\build.ps1 -DeployTo C:\Tools    # ..and copy the programs there, last
```

It works from any directory, and `build.bat` passes its arguments to it
unchanged for a `cmd.exe` prompt.

- **The build is the release build.** The four Go executables are built
  exactly as `release.yml` builds them: `windows/amd64`, CGO off,
  `goversioninfo -64` (PE version plus `app.manifest`), `-trimpath`, and the Go
  that `go.mod`'s `toolchain` line pins (`go1.26.8`). The gate's `go test`
  runs are amd64 too, so what `-Test` proves is a build of the same source,
  toolchain and flags as the one that ships - not the shipped bytes: the
  workflow rebuilds every executable on its runner, re-runs the gate's go
  tests on the tagged source first and smoke-checks its own build, and the
  GUI selftest and the PE-shape checks run only here (AUD-68-F1).
  `exe_to_download\` holds amd64 executables. Prerequisites that are missing
  or different end the run with exit 2 before anything is built: a local Go
  other than the pinned one (install it, or set `GOTOOLCHAIN=go1.26.8`),
  goversioninfo other than `v1.4.1`
  (`go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1`),
  or a Windows that cannot run amd64 programs. A missing MSBuild is exit 2 as
  well, unless `-SkipGui` said so.
- `-Test` runs nine gate steps: stamped-executable smoke tests, which also
  read each exe's build info (amd64, `-trimpath`, the pinned Go, the PE
  version, the manifest); the separate test-module compile check and placement
  registry; fdsec short tests; the fdsec command-surface tests plus its vet
  baseline; the GUI self-test (its log is deleted first, so a missing log is
  `COULD NOT VERIFY`, never the previous run's); `THIRD-PARTY-NOTICES.txt` against
  the Go modules each shipped executable links
  (`packaging\check-third-party-notices.ps1`); `release.yml`'s Go pin
  against `go.mod`'s; and the documentation corpus - registry, links and
  anchors, house style (`packaging\check-internal-docs.ps1`); and the published
  site and READMEs - sitemap, SEO block, locales and translation freshness,
  termbase, screenshots (`packaging\check-external-docs.ps1`). Its final `build-gate <stamp>:` line is `PASS`, `FAIL`,
  or `COULD NOT VERIFY`; exit codes are 0, 1, and 2 respectively. (Root
  `go test ./...` is known-broken per `AGENTS.md`, so it is intentionally not
  run.)
- `-Commit` implies `-Test` and commits the working tree only after the gate
  passes. It never tags and never pushes.
- **Deploying is opt-in** - `-DeployTo <folder>`, or the `FILEDO_DEPLOY_DIR`
  environment variable. It runs last, after the gate, the installer and
  `-Install` all passed, and copies only the `*.exe`, `*.bat` and `*.config`
  files. A plain run copies nothing anywhere.
- The **installer step runs by default** and builds both artifacts from the
  same wxs files the release workflow uses, so an installer defect is found
  here rather than inside a tagged CI run:

  | Artifact | What it is |
  | --- | --- |
  | `dist\FileDO-<version>-setup.exe` | the setup EXE people download - a Burn bundle carrying the MSI |
  | `dist\FileDO-<version>-windows-x64.msi` | the package itself, for deployment tools |

  `dist\` is git-ignored and holds only the current build's pair, so nothing
  is published and nothing accumulates. Without the WiX tool the step prints a
  skip and the build still succeeds - a missing packaging tool says nothing
  about the code that just compiled. `-Msi` makes it strict instead (exit 2),
  and `-SkipInstaller` drops it. WiX and its two extensions:

  ```powershell
  dotnet tool install --global wix --version 5.0.2   # the version release.yml pins
  # build.ps1 then runs, pinned to the tool's own version:
  #   wix extension add --global WixToolset.UI.wixext/<wix version>
  #   wix extension add --global WixToolset.BootstrapperApplications.wixext/<wix version>
  ```

  Pinning matters: unpinned, an extension resolves to its newest release
  (7.x), which a WiX 5 build refuses with *"Could not find expected package
  root folder wixext5"*. The check that skips the add matches the extension's
  name *and* the tool's version, so a 7.x copy in the global cache does not
  count as installed.
- `-Install` implies `-Msi` and then starts the setup EXE with its window, so
  Windows asks for elevation the usual way and the feature checkboxes are
  there to be seen. It reports the installer's exit code and reads it: `0`
  done, `3010` done-and-wants-a-restart, `1602` the user cancelled (an answer,
  not a defect), `1603` failed - and then names the `/log` command that says
  why. **This is the only switch in either script that changes the machine it
  runs on** (`-DeployTo` only copies files into a folder you named).
- `build.bat` is `build.ps1` for a `cmd.exe` prompt, with the same arguments
  and exit codes. It no longer builds a separate `-race` binary.

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
.\release.ps1 -SkipOpsMount   # operations run without its mounted-disk tier (no UAC prompt)
.\release.ps1 -OpsRebaseline  # accept this operations run as the new baseline (an intended change)
.\release.ps1 -WingetInstallTest   # also install the synced manifest here, then uninstall
.\release.ps1 -Resume -Version 2606271600 [-SkipStore]   # continue a release whose tag is out
```

What it does, in order:

1. **Preflight** - tools present (`git`, `gh`, `go`, `wingetcreate`), `gh`
   authenticated, on `main`, and **the tree clean**:
   `git status --porcelain --untracked-files=all` may list nothing outside
   `exe_to_download\`, or the release stops (exit 1) - an uncommitted edit or
   an unignored file (a key at the root, a runtime list) never rides into the
   public commit. The stamp must be a real `yyMMddHHmm` date and newer than
   every `v\d{10}` tag, and its MSI version must be greater than the
   `ProductVersion` read from the latest release's MSI. Then
   `packaging\check-winget-manifests.ps1 -NoNetwork` over `winget\` - the
   frozen anchors, checked while there is still no tag.
2. **Gate** - `build.ps1 -Test -Msi` when WiX is installed here, `-Test` alone
   when it is not, then the full fdsec run (amd64), then
   `govulncheck -mode=binary` over the four Go executables - the scan the
   workflow repeats after the tag, so it has to pass before it
   (`go install golang.org/x/vuln/cmd/govulncheck@v1.8.0`), then the
   **operations run**, `tests\prove-operations.ps1`: every kind of filedo
   operation on small data with the freshly built `exe_to_download\filedo.exe`,
   each step timed and each result checked, the whole run compared with the last
   passing runs for a loss of function or of speed (see "The operations run" below).
   Fails *before* anything is tagged, and so does a gate that changed any file
   outside `exe_to_download\`.
3. **Commit** - stages `exe_to_download\` and nothing else, and commits it as
   `Build v<version>` if the build changed it. A dry run lists what it would
   commit and does not touch the index.
4. **Push** - `git push origin main`, then create + push `v<version>`.
   **This is the point of no return** - it triggers the GitHub release.
5. **Wait** - polls until the GitHub Release carries all six assets (setup
   EXE, zip and MSI, each with its `.sha256`).
6. **winget** - downloads the SHA256, rewrites `winget/*.yaml`
   (`PackageVersion`, `InstallerUrl`, `InstallerSha256`, `ReleaseDate`,
   `ReleaseNotesUrl`), **checks them**, commits `Sync winget/ to v<version>`,
   pushes. The check is `packaging\check-winget-manifests.ps1 -Version
   <version>`: `winget validate` for the schema, the frozen
   `PackageIdentifier` and the five `PortableCommandAlias` names, the version
   in every URL, and `InstallerSha256` against the `.sha256` the release just
   published. It runs **before the commit**, because `microsoft/winget-pkgs`
   re-validates the PR in its own CI - which is after the tag is already out.
   A failure commits nothing and is recorded; the tag stands, and the
   checklist says how to resume.
   `-WingetInstallTest` adds the end-to-end proof: install from the manifest,
   run the installed `filedo`, uninstall. It writes into the profile of
   whoever runs the release, which is why it is opt-in, and it refuses if
   FileDO is already installed from winget on that machine. The manifest sets
   `ArchiveBinariesDependOnPath: true`: winget puts the package folder on PATH
   instead of a symlink per alias, because .NET Framework reads
   `filedo_win.exe.config` beside the path the GUI was launched from.
7. **Submit** - `wingetcreate submit` opens the PR to `microsoft/winget-pkgs`.
   The GitHub token reaches it in `WINGET_CREATE_GITHUB_TOKEN`, set for that
   one call - never on a command line, where the process list sees it. The
   exit codes of the winget commit, push and submit are all read. Right after
   the submit, `packaging\set-winget-pr-checklist.ps1` ticks the template
   boxes of the PR and fills its Description - each box only on evidence (CLA
   status, `winget validate`, schema, one manifest, no other open PR; the
   install box only under `-WingetInstallTest`). A box left empty is named in
   the output and is not fatal; run the script again, or tick it by hand.
8. **Store** - builds the unsigned MSIX into `msix\out\`, **pinned to this
   release's stamp** (`filedo.exe` prints it; `filedo_win.exe` carries it as
   its PE version and `BuildStamp`), and **only from sources equal to the
   tag** (T3-F2): it refuses, and records the Store channel as failed, unless
   `git diff --quiet v<stamp> HEAD -- . ':!winget'` succeeds and nothing is
   changed or untracked outside `winget\`. In the normal flow HEAD is the tag
   plus the winget commit; under `-Resume` `main` may have moved on, and a
   package built from that would carry, under the tag's version, code no gate
   judged. `-Resume` fetches the tag from origin for this check, and
   `build-msix.ps1 -Stamp <stamp>` run by hand applies the same rule.
   The identity comes from `msix\identity.json` - `SZA.FileDO`, reserved in Partner Center and a
   frozen anchor - or from `-StoreIdentityName`. There is no placeholder
   default: without a reserved identity the step refuses. The package carries
   **no Explorer command**: the packaged `File DO..` group of SP-0020
   (`FileDOShell.dll` and its manifest verb) is opt-in through
   `build-msix.ps1 -ExplorerCommand`, which `release.ps1` never passes, and
   the packed-manifest check fails a default package that carries it
   (AUD-17-F1). That is what keeps the listing's "no Explorer entries" true;
   a Store build with the switch waits for SP-0020's exit criterion, SP-0005
   G3, and a listing/README edit (`msix/README.md` section 7).
9. **Checklist** - prints what is done, what failed and why, and the one
   manual step left. A channel that failed after the tag (winget sync, push,
   submit, or the MSIX) does not stop the ones after it, is shown as `[ ]`
   with its reason, and makes the script exit 1; `release <stamp>: PASS` is
   printed only when every channel that ran succeeded.

### The operations run

`tests\prove-operations.ps1` is the part of the gate that uses the product the way a person does: every kind
of operation, on small data, each timed, each result checked. `release.ps1` runs it after `govulncheck`,
against `exe_to_download\filedo.exe` (the binary the release ships); it is also run by hand, from the repo
root, with no arguments. Its last line is `operations-run <stamp>: PASS`, `FAIL` or `COULD NOT VERIFY`, with the
same exit codes as the build gate (0, 1, 2).

- **Judged by the result, not by the console.** Each step is a `filedo` run with `--events`: its exit
  code, the `result` event's verdict and the numbers in it (files and bytes copied, speeds, files checked)
  are the verdict. Data that was moved, sealed or restored is judged by a SHA-256 manifest of the source
  compared with the result (names, sizes, content, modification times) - never by filedo's own account of it.
  `compare` is informational by design (exit 0 on a difference), so its summary is read and the trees are
  judged by the manifest. A step with no `result` event, one that outlives `-BudgetSeconds` (90 s, then it is
  killed) and a prompt that cannot be answered (stdin is closed) are failures.
- **Tier 1, a folder under `%TEMP%`, no elevation:** help, the usage errors (an unknown command, a missing
  target and path: exit 2, "Not proven"), info, speed, clean, the seven copy modes (each manifest-checked),
  `--precount`, a `--stop-file` asked before the start (Stopped, nothing half-written), a repeated copy, a copy
  that must keep a different file, compare, `cmp .. del`, `cd` (report, `del old`, `move new`, the `abc` /
  `xyz` name rules), `check` (default, `--mode deep`, `--include-ext`, `--report json`, `--resume`) and its
  companion launcher, history (a run leaves an entry, its password never), `secure` / `unsecure` /
  `fdsec info` / `fdsec verify` in suites 1, 2 and 3 with the refusals (wrong password, a flipped byte, a cut
  container), `del` after a verified seal, `reveal -rw`, `wipe` (refused without the typed word, then
  `--force`), a batch run, the network target (this machine's own admin share, `\\localhost\C$\..`; skipped
  and said so when the account cannot reach it), and the virtual-disk verbs that need no elevation (`vd new`,
  `info`, `verify`, `export`, `add` / `list` / `forget`, a vault's wrong password, and every line of
  `tests\vd_batch.lst`). A password given as `p:<word>` must not reach the event stream or the output. A
  private `FILEDO_STATE_DIR` keeps the run out of your own state.
- **The load section, still no elevation:** the same operations on data sized so that each runs for seconds.
  On a few kilobytes every step is process start-up and no loss of speed could show; at seconds a step's
  noise is a few percent of it. Every size is one number in the `$Load` block of the script, with the
  measurement behind it: 2400 small files for the seven copy modes (about 1 ms a file; `safecopy` and
  `synccopy` 2.1-2.4), `cmp .. del` over 1000 pairs, two trees of 4500 tiny files for `check` and
  `check --mode deep` (a first run over a tree costs about 1 ms a file), `cd` and `wipe` over both, 768 MiB
  in six files for `secure` / `fdsec verify` / `unsecure`, a 640 MiB suite 2 file, a 1.5 GB `speed`, and a
  1.5 GiB virtual-disk container (`vd new .. fast`, `verify`, `clone`, `export raw`). The fixtures are read
  once before anything is timed, so Defender's first scan of a fresh file is not in a figure. Each step
  prints its time, its MB/s and its milliseconds a file. Two operations cannot be made to last seconds with a
  sane amount of data - `compare` reads metadata only (0.01 ms a file) and a big-file copy runs at 2 GB/s -
  so they are timed and never gate. The section needs about 4.5 GiB free on the temp drive and says
  `COULD NOT VERIFY` below 8 GiB. A load step that runs under 1.5 s is reported ("too noisy to compare well,
  raise its size"), so a plan gone stale - an operation got faster - says so itself.
- **Tier 2, mounted virtual disks:** `test` and `fill` are sized by the free space of their volume, so on a
  folder of the system drive they would take all of it; on a small virtual disk they are small. A 320 MB
  `.fdd`, carrying a third of its size in data (2000 small files and a 96 MiB one), is mounted, its reported size checked, then `device info`, `speed`, the capacity test, `fill` /
  `fill verify` / `clean` (free space back to where it started), a copy and its manifest, `check`, `cd`,
  `compare`, `probe` (refused without the typed word, then `probe yes`) and `recover` - the data must be
  byte for byte the same after both - a folder `wipe` and the drive-root `wipe` that must be refused (a
  canary file survives it), unmount, `vd verify` and "closed clean", a read-only remount that holds the data
  and refuses a write, `vd format` (the volume empty and the same size), a second capacity test, unmount and
  `destroy`. Then an encrypted **vault** (mount, a copy, unmount, a mount under a wrong password refused with
  no drive letter, the data back under the right one) and a **ram** disk (copy, `save`, unmount, "closed
  clean", remount, the saved data back). The drive letter is read from `vd status json` for each scratch
  container and refused unless it is new and not the system drive; a disk of yours that was mounted before
  the run is never a target.
- **Elevation.** Mounting needs administrator rights. From a console that is not elevated the script asks
  once (one UAC prompt) and runs tier 2 in an elevated child; declined or timed out is `COULD NOT VERIFY` (exit
  2), which stops the release. `-SkipMount` (`release.ps1 -SkipOpsMount`) drops tier 2 and the closing line
  names it as skipped.
- **Coverage is enforced.** The script keeps an inventory of every verb: a step, a line of `vd_batch.lst`, or
  an exemption with its reason. The verbs the exe's own help lists (`filedo -?`) are read at run time, and one
  with no inventory entry fails the run - a verb added later cannot go unexercised. The closing summary
  counts what ran, what is exempt and what was not run here (tier 2 under `-SkipMount`).
- **Compared with the last passing runs.** Every pass writes `timings.json` (step, tier, time, exit code,
  verdict and the counts of its result event). The baseline is up to eight earlier *passing* runs of this
  machine in `temp\evidence\ops-*\` (a run that did not pass is never one; after a `-Rebaseline` run the older
  ones stop counting). Against it:
  - **function:** a step that used to run and is gone, or one whose exit code, verdict or counts changed,
    fails the run. A change you meant is accepted with `-Rebaseline` (`release.ps1 -OpsRebaseline`), which
    prints it and makes this run the baseline.
  - **speed:** the reference is the median of the baseline. In the load section a step that normally runs
    1.5 s or more trips the gate at 1.5x its median and 500 ms over it, a shorter one at twice and 300 ms;
    so does the process start-up figure (the best of seven `filedo -?` runs, at twice its median and 20 ms
    more - the best of seven is the figure noise cannot inflate). A tripped gate measures the load section
    once more on fresh data and fails only if the best of the two passes still trips - noise is added time,
    one slow second is not a regression. A load step at 1.25x and 250 ms, any tier 1 or tier 2 step at twice
    its median (150 ms over; 500 ms in tier 2), and a median step ratio of 1.5 across tier 1, are listed as
    advisory. The gates apply from three passing runs; before that the run says so and only records. The
    closing summary shows the spread of the load steps over the baseline, so how comparable the figures are
    is itself on the page. The thresholds are pinned by `tests\prove-operations-selftest.ps1` (a second, no
    filedo run); change one and that file in the same edit.
  - **budget:** a step over `-BudgetSeconds` (90 s) is killed and fails, whatever the baseline says.
- **Not covered, on purpose:** `ui` / `dm` (`filedo_win.exe --selftest` covers the shell), `vd auto` and
  `vd stop` (they change the machine or drop every mounted disk), `fdsec register` / `unregister` (the
  Explorer registration, proven by its own tests), `reveal` without `-rw` (it launches an application), and
  the `filedo_fill` / `filedo_test` companions (they fill). `device C: info` is not run either: it walks the
  whole drive (over a minute on a system drive), so a device's info is checked on the small disk of tier 2.
- **Evidence** - per-step output, event streams and `timings.json` - goes to `temp\evidence\ops-<run-id>\`
  (git-ignored, private; the last dozen runs are kept); the scratch data is removed after a pass and kept
  after a failure.

### The release notes on the site

The site's release notes (`docs/guides/release-notes.html`, with its `docs/de/` and `docs/fr/` pages; the
address is a published one - SITE-STRUCTURE rule 8) are the same list as the Version History in `README.md`,
written by hand in the same edit as that entry: one `<section class="section" id="v<stamp>">` per shipped
release, newest first, ru/en/ua in the runtime page and one language per locale page. `Unreleased` is not a
release and does not appear. `packaging\check-external-docs.ps1` (step `releasenotes`) fails when the three
pages and the README disagree on which releases exist or in what order; the translation fingerprints are
re-recorded with `-Record` after the translations are read.
### The one manual step

**Microsoft Store upload.** Partner Center has no publishing CLI, so the last
leg is manual (Microsoft re-signs the unsigned package during certification -
no paid cert needed): upload `msix\out\FileDO_<ver>.msix`; write this release's
"What's new" into `msix\listing\<code>.txt` (`@@ReleaseNotes`), export the
listing and run `.\msix\build-store-listing-csv.ps1 -Refresh ReleaseNotes`,
import `msix\out\store-import`; submit. Then update over a **real prior
install** and check the version and the notes. The full path - identity, build,
listing, screenshots, click path - is `msix/README.md`.

**Before that upload**, from an **ADMIN** PowerShell:

```powershell
.\msix\build-msix.ps1 -SelfSign                        # the only installable shape
.\msix\test-store-package.ps1 -TrustTestCert           # WACK + the privacy-policy URL
```

The Windows App Certification Kit runs the test families the Store runs at
certification, against the local-test package; the same script checks that the
privacy-policy URL the listing carries answers 200 (Store policy 10.5). It
proves nothing about the reserved identity or Microsoft's re-signing - the
local-test package has neither - and it never uploads anything. A test that is
not `PASS` is printed with the Store's own wording, including the optional ones:
required failures block the check, while optional findings are reported as advisories under Microsoft's
Desktop Bridge test policy. The raw overall verdict remains visible, including `WARNING`.

The release workflow does its own last check before it publishes: every
`.sha256` sidecar is re-read and its asset re-hashed
(`packaging/verify-sha256-sidecars.ps1`). That file is what the download page
tells people to compare against, and what step 6 above syncs
`InstallerSha256` from.

The workflow itself (`.github/workflows/release.yml`) guards the other end of
the tag:

- **Only a real release tag builds.** The tag reaches the scripts through the
  environment, never spliced into a script, and must be `v` + a real
  `yyMMddHHmm` date whose commit is on `origin/main`; anything else stops in
  "Resolve version", before an artifact exists. A dispatched run checks out
  `refs/tags/<input>`, never a branch of the same name.
- **The tag is bound to a gate verdict** (AUD-68-F1). "Bind the tag to the
  release gate" runs the tagged tree's own `exe_to_download\filedo.exe -?`
  and fails unless it prints the tag's stamp, and requires
  `exe_to_download\filedo_win.exe`'s FileVersion to equal the stamp's PE
  version. Only `release.ps1` puts those there: its gate builds them with the
  stamp and step 3 commits them as `Build v<stamp>`. A `v*` tag pushed by
  hand on another commit stops here.
- **The gate's go tests run before anything is built** (AUD-68-F1, owner
  decision 2026-10-01). "Go test gate (as build.ps1)" runs, with the pinned
  Go and `windows/amd64`, the same `go test` calls as `build.ps1 -Test`: the
  `cmd\filedo-test` module compile check, `./fdsec/ -short`, `./vdisk/`, and
  `./cmd/filedo/ -vet=off` with its version resource and
  `FILEDO_FDSEC_REQUIRE_REPO_ROOT=1`. A failing test stops the job. Not
  repeated there: the vet baseline, the placement check, the GUI selftest and
  the PE-shape checks - those run only in the local gate.
- **Latest only moves forward.** The release is marked Latest - what the
  site's download button serves - only when its stamp is greater than the
  current Latest's.
- **A published release is never overwritten** (AUD-20-F1). The build is not
  reproducible (zip mtimes, MSI and bundle codes and timestamps), and
  winget-pkgs and every user who compared a SHA256 hold the first run's
  hashes. So the publish job reads `gh release view <tag> --json assets`
  first and fails - "Refuse to overwrite a published release" - if the
  release already carries any of the six asset names; the upload step also
  runs with `overwrite_files: false`. There is no override input. A run that
  failed before publishing (no release, or a release with none of the six)
  still goes through.
- **Least privilege.** Every action is pinned to a commit SHA, checkout does
  not persist the token, WiX is pinned to `5.0.2`, and the Go is `1.26.8`
  (`GOTOOLCHAIN=local`, checked against `go.mod`). The Windows build job can
  only read the repository; a separate job that runs no project code takes
  the six verified files and is the only one allowed to write the release.
- **No known-vulnerable code ships.** `govulncheck -mode=binary` reads every
  Go executable before anything is published.

### Prerequisites for a release

- `gh` authenticated (`gh auth login`) - used for push, release polling,
  asset download, and the winget submit token.
- `wingetcreate` - `winget install Microsoft.WingetCreate`.
- Go `1.26.8` (the `go.mod` toolchain pin) and goversioninfo `v1.4.1` - the
  gate refuses any other, because it must build what the workflow builds.
- govulncheck - `go install golang.org/x/vuln/cmd/govulncheck@v1.8.0`.
- For the operations run's mounted-disk tier: the ability to accept one UAC prompt, and the Windows iSCSI
  initiator service (the same the Disk Manager needs). Without them pass `-SkipOpsMount`.
- For the MSIX: Windows SDK (`makeappx`) + VS Build Tools (MSBuild) - see
  `msix/README.md`. Skip with `-SkipStore` if unavailable.

### The installer, and what has to be proven about it

Two artifacts ship, and the EXE is the one to point people at:
`FileDO-<version>-setup.exe` is a Burn bundle that carries
`FileDO-<version>-windows-x64.msi` inside it, shows the MSI's own wizard, and
supports `/quiet`, `/passive`, `/uninstall` and `/log <file>`. The bare MSI
stays published for deployment tools. The bundle adds no install behaviour of
its own - what gets installed is decided by `packaging\wix\FileDO.wxs` alone.

**The asset names are a contract with the site.** The landing page
(`docs/index.html`) asks the `releases/latest` API for the release and picks its
download links by suffix: `-setup.exe`, `-windows-x64.zip`, `-windows-x64.msi`,
and `<name>.sha256` for the hash link. Rename an asset in `release.yml` and the
button quietly falls back to the plain Releases page - the page still works,
but nothing downloads from it. The site therefore never needs an edit for a
release, and a release never breaks it, as long as those suffixes hold. The
installer is also **not code-signed**, and the landing page, the release body,
the trust page, the install guide and every README say so next to the download;
the day that changes, those lines change in the same edit.

The MSI is what makes FileDO a Windows program rather than a folder of
executables, so it carries the operations an install is expected to perform:
`PATH`, a Start menu entry, a desktop icon, and the Explorer integration - the
`.fd-sec` document type and the cascading `File DO..` group on every file
(secure and its three dispositions, unsecure and its delete, wipe, check,
info). Those are one feature the user can deselect
(`WixUI_FeatureTree`, or `ADDLOCAL` for a silent install), and `MigrateFeatures`
carries that choice into the next version.

Before shipping a release whose installer changed, run these by hand and paste
the result into the PR - a fresh install alone does not prove an upgrade:

1. **Update over a real prior install.** Install the previous setup EXE, then
   the new one. `C:\Program Files\FileDO` holds one copy, Apps and features
   lists **one** FileDO, the shortcuts still work, and the feature selection
   made the first time is still in force. Do it once more with two builds
   stamped the same day (one at 12:00, one at 17:00): still one FileDO, and
   `Get-CimInstance Win32_Product -Filter "Name='FileDO'"` returns one row.
2. **Change.** Apps and features > FileDO > Modify (Uninstall/Change in the
   classic Control Panel) opens the MSI's maintenance page; Change shows the
   feature tree, and turning Explorer integration off and on there takes the
   `File DO..` group away and puts it back. The setup EXE uses
   `WixInternalUIBootstrapperApplication` with `DisableModify="button"` for
   exactly this.
3. **The wizard reads like a product, not a template.** The Welcome page says
   what FileDO is, the Customize page describes each feature as you click it,
   and the artwork is FileDO's. If any page shows WiX's stock blue graphic or
   "The Setup Wizard will install [ProductName] on your computer", the build
   lost `-loc packaging\wix\FileDO.en-us.wxl` or the two `-d ...Bmp` defines.
4. **The group.** Right-click any file -> `File DO..` (under *Show more
   options* on Windows 11) and the sub-menu opens with ten entries in this
   order: Secure, Secure and delete original, Secure and wipe original, Secure
   with a random name, [separator], Unsecure, Unsecure and delete container,
   Unsecure and start, [separator], Wipe this file, Check this file, Info.
   Click `Info`: the console must still be on screen afterwards with `Press
   Enter to close this window..`, which is what proves the pause flag
   survived into the registry.
   A `.fd-sec` file shows the FileDO icon, and a double-click asks for the
   password and then opens the original in whatever program owns its real
   extension. Nothing named `Secure with FileDO` or `Unsecure here` is left.
5. **Deselecting works.** `msiexec /i <msi> /qn ADDLOCAL=Main` leaves no
   Explorer entries, no `.fdd` type and no desktop icon. The `.fdd` feature
   (`DiskContainerIntegration`) has its own checklist - install, deselect,
   the double-click routes, uninstall with `reg query` before and after - in
   `tests/vd_manual.md`, sections 1 to 3.
6. **Uninstall leaves nothing.** After a removal, `HKLM\Software\Classes\.fd-sec`
   and `...\FileDO.SecureContainer` are gone, and so is the `PATH` entry.
7. **The portable path still works.** From the unzipped build,
   `filedo fdsec register` then `filedo fdsec unregister` - the same group and
   document type, written per user and taken back.
8. **The site button.** Once the release is public, open the landing page:
   *Download for Windows (setup.exe)* points at the new file, shows its name
   and size, and the *SHA256* link opens the `.sha256` asset. Downloading it
   from the browser and running it shows the SmartScreen prompt the page warns
   about - if it does not, the signing status changed and the copy is stale.

The registry shape has two writers - `packaging\wix\FileDO.wxs` and
`cmd\filedo\fdsec_register*.go`. They are one contract; a change to either one
is a change to both, in the same commit.
`TestRegistryParity_TheMSIAndFdsecRegisterWriteTheSameShellIntegration`
(`cmd\filedo\registry_parity_test.go`, part of the gate) runs the real
`fdsec register -all-users` into a scratch key and compares every key, value,
label and command line with what the wxs writes, in both directions.

### If something fails mid-release

A release whose tag is out and whose CI failed before publishing is resumed,
not re-cut. A release that was **published** and is broken is never repaired
in place: it is fixed by a new stamp (a new release), because its hashes are
already in winget-pkgs and on users' screens, and the workflow refuses to
overwrite it anyway.

- **Tag pushed, CI still running** - wait for the run. Do not re-dispatch
  it: the duplicate queues behind the first (the workflow serializes per
  tag) and, once the first has published, fails at "Refuse to overwrite a
  published release". `.\release.ps1 -Resume -Version <stamp>` covers a wait
  that timed out.
- **Tag pushed, CI failed before publishing** (the build job failed, or the
  publish job failed before "Create GitHub Release") - fix what failed,
  re-dispatch the workflow from the Actions tab for the same tag, and
  continue with `.\release.ps1 -Resume -Version <stamp>`: it checks that
  `v<stamp>` is on origin, skips steps 1-4, waits for the release to carry
  all six assets, and goes on with winget and the Store. A failure in "Bind
  the tag to the release gate" or "Go test gate" is in the tagged sources
  themselves, so a re-dispatch fails the same way: fix it and cut a new
  stamp.
- **Store step failed** - fix the cause, then `.\msix\build-msix.ps1 -Stamp
  <stamp>` (or `.\release.ps1 -Resume -Version <stamp> -SkipWinget`). Both
  build only from sources equal to `v<stamp>`: HEAD's tree must equal the
  tag's outside `winget\` and nothing may be changed or untracked outside
  `winget\`. When `main` has moved on since the tag, build from a worktree of
  the tag instead: `git worktree add ..\FileDO-v<stamp> v<stamp>`, run
  `.\msix\build-msix.ps1 -Stamp <stamp>` there, then
  `git worktree remove ..\FileDO-v<stamp>`.
- **A partial release exists** (the run failed mid-upload, so the release
  carries some of the six assets) - the re-dispatch is refused until that
  release is gone. Delete the **release, not the tag**, by hand
  (`gh release delete v<stamp> --repo SerZhyAle/FileDO`, no `--cleanup-tag`),
  then re-dispatch and resume as above. Only while nothing of it has reached
  winget: if `winget\` was already synced or submitted from it, cut a new
  stamp instead.
- **winget step failed** (sync check, commit, push, or submit) -
  `.\release.ps1 -Resume -Version <stamp> -SkipStore`. It re-syncs `winget\`
  (it may already be rewritten; `-Resume` allows changes there and nowhere
  else), checks it, commits and pushes if needed, and submits. When the push
  failed because `main` moved: `git pull --rebase origin main` first.
- **By hand**, when the script cannot run (steps 6-8):
  ```powershell
  $v = '<stamp>'; $tag = "v$v"; $zip = "FileDO-$v-windows-x64.zip"
  gh release download $tag --repo SerZhyAle/FileDO --pattern "$zip.sha256" --dir $env:TEMP --clobber
  # edit winget\*.yaml: PackageVersion "$v", InstallerUrl .../$tag/$zip,
  # InstallerSha256 (the first word of $env:TEMP\$zip.sha256, upper case),
  # ReleaseDate, ReleaseNotesUrl .../releases/tag/$tag - then check them:
  .\packaging\check-winget-manifests.ps1 -Version $v
  git add -- winget; git commit -m "Sync winget/ to $tag"; git push origin main
  $env:WINGET_CREATE_GITHUB_TOKEN = gh auth token
  wingetcreate submit winget
  Remove-Item Env:WINGET_CREATE_GITHUB_TOKEN
  .\msix\build-msix.ps1 -Stamp $v        # step 8, from sources equal to $tag; the upload stays manual
  ```
- **Verify after merge** - `winget show SerZhyAle.FileDO`.
