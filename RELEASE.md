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
```

- `-Test` runs the test gate: smoke-run `filedo.exe -?` (asserts it prints the
  stamped version) + `go test ./cmd/filedo-test`. (Root `go test ./...` is
  known-broken per `AGENTS.md`, so it is intentionally not run.)
- `-Commit` implies `-Test` and commits the working tree only after the gate
  passes. It never tags and never pushes.
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
  dotnet tool install --global wix --version 5.*
  # build.ps1 then runs, pinned to the tool's own version:
  #   wix extension add --global WixToolset.UI.wixext/<wix version>
  #   wix extension add --global WixToolset.BootstrapperApplications.wixext/<wix version>
  ```

  Pinning matters: unpinned, an extension resolves to its newest release
  (7.x), which a WiX 5 build refuses with *"Could not find expected package
  root folder wixext5"*.
- `-Install` implies `-Msi` and then starts the setup EXE with its window, so
  Windows asks for elevation the usual way and the feature checkboxes are
  there to be seen. It reports the installer's exit code and reads it: `0`
  done, `3010` done-and-wants-a-restart, `1602` the user cancelled (an answer,
  not a defect), `1603` failed - and then names the `/log` command that says
  why. **This is the only switch in either script that changes the machine it
  runs on.**
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
.\release.ps1 -WingetInstallTest   # also install the synced manifest here, then uninstall
```

What it does, in order:

1. **Preflight** - tools present (`git`, `gh`, `go`, `wingetcreate`), `gh`
   authenticated, on `main`, tag not already used, and
   `packaging\check-winget-manifests.ps1 -NoNetwork` over `winget\` - the
   frozen anchors, checked while there is still no tag.
2. **Gate** - `build.ps1 -Test -Msi` when WiX is installed here, `-Test` alone
   when it is not. Fails *before* anything is tagged.
3. **Commit** - commits refreshed binaries (tracked in `exe_to_download\`) if
   the build changed them.
4. **Push** - `git push origin main`, then create + push `v<version>`.
   **This is the point of no return** - it triggers the GitHub release.
5. **Wait** - polls until the GitHub Release and its `.zip.sha256` asset exist.
6. **winget** - downloads the SHA256, rewrites `winget/*.yaml`
   (`PackageVersion`, `InstallerUrl`, `InstallerSha256`, `ReleaseDate`,
   `ReleaseNotesUrl`), **checks them**, commits `Sync winget/ to v<version>`,
   pushes. The check is `packaging\check-winget-manifests.ps1 -Version
   <version>`: `winget validate` for the schema, the frozen
   `PackageIdentifier` and the five `PortableCommandAlias` names, the version
   in every URL, and `InstallerSha256` against the `.sha256` the release just
   published. It runs **before the commit**, because `microsoft/winget-pkgs`
   re-validates the PR in its own CI - which is after the tag is already out.
   A failure aborts here and commits nothing; the tag stands, so fix `winget\`
   and re-run with `-SkipStore`, or submit by hand.
   `-WingetInstallTest` adds the end-to-end proof: install from the manifest,
   run the `filedo` shim, uninstall. It writes into the profile of whoever
   runs the release, which is why it is opt-in, and it refuses if FileDO is
   already installed from winget on that machine.
7. **Submit** - `wingetcreate submit` opens the PR to `microsoft/winget-pkgs`.
8. **Store** - builds the unsigned MSIX into `msix\out\`, **pinned to this
   release's stamp** (the exe inside prints the tag's version). The identity
   comes from `msix\identity.json`, recorded once after the app name is
   reserved in Partner Center, or from `-StoreIdentityName`. There is no
   placeholder default: without a reserved identity the step refuses and the
   checklist says so - the tag and the winget PR are already out by then, so a
   refusal is reported and never fatal. Preflight warns about it up front.
9. **Checklist** - prints what is done and the one manual step left.

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
those are what a submission has to be argued through, not noise.

The release workflow does its own last check before it publishes: every
`.sha256` sidecar is re-read and its asset re-hashed
(`packaging/verify-sha256-sidecars.ps1`). That file is what the download page
tells people to compare against, and what step 6 above syncs
`InstallerSha256` from.

### Prerequisites for a release

- `gh` authenticated (`gh auth login`) - used for push, release polling,
  asset download, and the winget submit token.
- `wingetcreate` - `winget install Microsoft.WingetCreate`.
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
installer is also **not code-signed**, and the landing page, the install guide
and every README say so next to the download; the day that changes, those lines
change in the same edit.

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
   made the first time is still in force.
2. **The wizard reads like a product, not a template.** The Welcome page says
   what FileDO is, the Customize page describes each feature as you click it,
   and the artwork is FileDO's. If any page shows WiX's stock blue graphic or
   "The Setup Wizard will install [ProductName] on your computer", the build
   lost `-loc packaging\wix\FileDO.en-us.wxl` or the two `-d ...Bmp` defines.
3. **The group.** Right-click any file -> `File DO..` (under *Show more
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
4. **Deselecting works.** `msiexec /i <msi> /qn ADDLOCAL=Main` leaves no
   Explorer entries and no desktop icon.
5. **Uninstall leaves nothing.** After a removal, `HKLM\Software\Classes\.fd-sec`
   and `...\FileDO.SecureContainer` are gone, and so is the `PATH` entry.
6. **The portable path still works.** From the unzipped build,
   `filedo fdsec register` then `filedo fdsec unregister` - the same group and
   document type, written per user and taken back.
7. **The site button.** Once the release is public, open the landing page:
   *Download for Windows (setup.exe)* points at the new file, shows its name
   and size, and the *SHA256* link opens the `.sha256` asset. Downloading it
   from the browser and running it shows the SmartScreen prompt the page warns
   about - if it does not, the signing status changed and the copy is stale.

The registry shape has two writers - `packaging\wix\FileDO.wxs` and
`cmd\filedo\fdsec_register*.go`. They are one contract; a change to either one
is a change to both, in the same commit.

### If something fails mid-release

- **Tag pushed, CI failed** - fix, then re-dispatch the workflow from the
  Actions tab for the same tag (the workflow serializes per-tag), or delete the
  tag/release and re-run `release.ps1` with a new `-Version`.
- **winget submit failed** - re-run manually:
  `wingetcreate submit --token (gh auth token) winget`.
- **Verify after merge** - `winget show SerZhyAle/FileDO`.
