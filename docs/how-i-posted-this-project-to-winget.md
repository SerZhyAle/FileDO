# How I posted FileDO to winget

A walkthrough of getting **FileDO** into the official Windows Package Manager, with every command, every blocker, and every fix — written so you can do the same for your own project.

The end result: anyone on Windows can now run

```powershell
winget install SerZhyAle.FileDO
```

and get all four FileDO binaries on their `PATH`, with no admin opt-in, no manual download, no PowerShell flags.

---

## 1. What winget actually is

The Windows Package Manager (`winget`) ships with Windows 10 1809+ and Windows 11. It pulls its package catalog from a single curated GitHub repository: **[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs)**. To get a package listed, you submit a pull request adding three small YAML files describing your release. Microsoft moderators review and merge. Once merged, every winget client on the planet sees your package on the next index rebuild (~30–60 minutes).

There is no developer account, no signing certificate requirement, no annual fee — just a GitHub PR.

---

## 2. The decision before any code

FileDO ships **four** independent executables, not one:

- `filedo.exe` — main CLI
- `filedo_check.exe` — health scanner
- `filedo_fill.exe` — fill / wipe operations
- `filedo_test.exe` — speed/capacity test

That ruled out the simplest packaging type (`InstallerType: portable` with a single `.exe`). Three viable options remained:

1. **Single portable EXE** — easiest, but only ships one binary.
2. **Portable zip** (`InstallerType: zip` + `NestedInstallerType: portable`) — ships all binaries, each registered as a separate command alias.
3. **Real installer** (Inno Setup / WiX MSI) — cleaner uninstall and Start Menu integration, but heavyweight for a CLI tool.

I picked **option 2**. Each binary becomes a `PortableCommandAlias`, so users can call `filedo`, `filedo_check`, `filedo_fill`, and `filedo_test` from any shell after install.

---

## 3. The release pipeline (GitHub Actions)

winget needs a **stable HTTPS download URL with a known SHA256**. The standard place for that is a GitHub Release attached to a tag. Doing this by hand every time is error-prone, so I wrote a workflow that does the whole thing on `git tag push`.

**File: [`.github/workflows/release.yml`](../.github/workflows/release.yml)**

The shape:

```yaml
name: Release
on:
  push:
    tags: ['v*']
  workflow_dispatch:
    inputs:
      tag:
        description: 'Tag to release (e.g. v2604271200)'
        required: true

permissions:
  contents: write

jobs:
  release:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      # ... resolve $version from tag (strip leading "v")
      # ... go build all 4 exes with -trimpath -s -w
      # ... copy LICENSE, README.md, .bat helpers into stage dir
      # ... Compress-Archive to FileDO-<version>-windows-x64.zip
      # ... Get-FileHash -Algorithm SHA256 → write to .sha256 file
      - uses: softprops/action-gh-release@v2
        with:
          tag_name: ${{ steps.ver.outputs.tag }}
          files: |
            dist/${{ steps.pkg.outputs.name }}
            dist/${{ steps.pkg.outputs.name }}.sha256
```

A couple of details worth noting:

- The four `main` packages each have their own `go.mod`. They use slightly different Go versions (1.21 and 1.24.4). Setting `go-version-file: go.mod` reads the root module's `go 1.24.4`, which satisfies all four.
- I set `CGO_ENABLED=0` for the release builds. The local `build.bat` uses `-race` (needs CGO), but that's a development setting — never ship `-race` builds.
- `-trimpath -s -w` strips local paths and debug info from the binaries. This shrinks the artifacts and avoids leaking my username.
- `workflow_dispatch` is included so I can re-run a release for an existing tag manually if something goes wrong.

### Cutting the first release

```bash
git add .github/workflows/release.yml
git commit -m "Add GitHub Actions release workflow"
git push
git tag v2604271301
git push origin v2604271301
```

I use the `yyMMddHHmm` date-time format for tags — same convention used elsewhere in the project. Anything that's a valid SemVer-ish string works for winget; it just has to be unique per release.

### Blocker #1 — the empty `build.yml`

I pushed the tag, then waited. And waited. The Release workflow had `total_count: 0` runs.

The repo had a leftover **0-byte `.github/workflows/build.yml`** from earlier experimentation. Every push for weeks had triggered it and it had failed instantly because YAML parsing of an empty file fails. GitHub's "auto-disable workflows after repeated failures" policy had **paused all Actions on the repo**.

The fix was three steps:

1. Remove the dead workflow:
   ```bash
   git rm .github/workflows/build.yml
   git commit -m "Remove empty build.yml workflow"
   git push
   ```
2. **Re-enable Actions** in the repo settings (browser): https://github.com/SerZhyAle/FileDO/actions → "Enable Actions on this repository". This had to be done manually — there is no API for it on a free account.
3. Re-cut the tag. Tag pushes that fire **before** Actions is enabled don't get retried. I deleted and re-pushed:
   ```bash
   git push origin :refs/tags/v2604271256   # delete remote tag
   git tag -d v2604271256                    # delete local tag
   git tag -a v2604271301 -m "first winget-ready release"
   git push origin v2604271301
   ```

This time the workflow ran, built in about 2 minutes, and published the release with the zip + SHA256 file:

```
https://github.com/SerZhyAle/FileDO/releases/tag/v2604271301
```

The SHA256 ended up being `A6E7CF8D3427393DBA1B3C28FB205FA69A49292121E8F6095EFB3E1DCC69DE18`.

---

## 4. The three manifest files

A winget package is described by a **manifest set** of three YAML files in one folder:

```
manifests/s/SerZhyAle/FileDO/2604271301/
├── SerZhyAle.FileDO.yaml             # version manifest
├── SerZhyAle.FileDO.installer.yaml   # installer manifest
└── SerZhyAle.FileDO.locale.en-US.yaml  # default-locale manifest
```

The folder path uses the publisher's first-letter as a sharding directory (`s/` for `SerZhyAle`).

### Version manifest (`SerZhyAle.FileDO.yaml`)

The "index" file. Tiny — it just points at which locale is the default and declares the schema version.

```yaml
PackageIdentifier: SerZhyAle.FileDO
PackageVersion: "2604271301"
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.12.0
```

### Installer manifest (`SerZhyAle.FileDO.installer.yaml`)

The interesting one. Declares the zip, where to download it, the SHA256 to verify, and which exes inside the archive get registered as PATH commands.

```yaml
PackageIdentifier: SerZhyAle.FileDO
PackageVersion: "2604271301"
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
- RelativeFilePath: filedo.exe
  PortableCommandAlias: filedo
- RelativeFilePath: filedo_check.exe
  PortableCommandAlias: filedo_check
- RelativeFilePath: filedo_fill.exe
  PortableCommandAlias: filedo_fill
- RelativeFilePath: filedo_test.exe
  PortableCommandAlias: filedo_test
Installers:
- Architecture: x64
  InstallerUrl: https://github.com/SerZhyAle/FileDO/releases/download/v2604271301/FileDO-2604271301-windows-x64.zip
  InstallerSha256: A6E7CF8D3427393DBA1B3C28FB205FA69A49292121E8F6095EFB3E1DCC69DE18
ManifestType: installer
ManifestVersion: 1.12.0
ReleaseDate: 2026-04-27
```

The `NestedInstallerFiles` list with multiple portable entries is a feature of schema **1.12.0** (which is why the schema version matters — see Blocker #3 below).

### Locale manifest (`SerZhyAle.FileDO.locale.en-US.yaml`)

Everything human-readable: publisher, author, license, description, tags, URLs.

```yaml
PackageIdentifier: SerZhyAle.FileDO
PackageVersion: "2604271301"
PackageLocale: en-US
Publisher: SZA
PublisherUrl: https://github.com/SerZhyAle
PublisherSupportUrl: https://github.com/SerZhyAle/FileDO/issues
Author: Serhii Zhyhunenko
PackageName: FileDO
PackageUrl: https://github.com/SerZhyAle/FileDO
License: MIT
LicenseUrl: https://github.com/SerZhyAle/FileDO/blob/main/LICENSE
Copyright: Copyright (c) 2025 sza
ShortDescription: Windows CLI for storage testing, fake-capacity detection, secure wipe, and duplicate management.
Description: |-
  FileDO is a fast Windows command-line tool for working with storage devices, folders, and network shares.
  It detects fake USB/SD cards by writing and verifying real data, benchmarks read/write speeds, securely
  wipes free space at multi-GB/s, finds and manages duplicate files via MD5 with hash caching, and copies
  large trees with progress and timeout protection.
Moniker: filedo
Tags:
- benchmark
- cli
- disk
- duplicates
- fake-capacity
- file-manager
- hash
- md5
- portable
- sd-card
- secure-erase
- speed-test
- storage
- usb
- wipe
ReleaseNotesUrl: https://github.com/SerZhyAle/FileDO/releases/tag/v2604271301
ManifestType: defaultLocale
ManifestVersion: 1.12.0
```

`Moniker: filedo` is what makes `winget install filedo` work as a shorthand (assuming no other package has claimed that moniker).

---

## 5. Local validation

Before submitting anything, validate against the schema:

```powershell
winget validate --manifest p:\WINDOWS\FileDo\winget
```

Output:

```
Manifest validation succeeded.
```

That checks YAML syntax, required fields, schema version compatibility, and field formats. It does **not** download the URL or verify the hash — that happens later in CI.

The full local install test (which I'd recommend) needs an admin opt-in:

```powershell
# In an elevated PowerShell:
winget settings --enable LocalManifestFiles
winget install --manifest p:\WINDOWS\FileDo\winget
```

This actually downloads the release zip, verifies the SHA256, extracts the four exes to a winget-managed location, and adds them to `PATH`. It's the single best test before submitting the PR. If you skip it, the PR's CI will catch problems anyway, but the round-trip is slower.

---

## 6. Submitting the PR with `wingetcreate`

The official microsoft/winget-pkgs repo is one of the largest on GitHub by file count. Cloning a fork to manually open a PR is painful (multi-GB checkout). **`wingetcreate`** automates the whole submission flow:

```powershell
winget install Microsoft.WingetCreate
```

It also needs a GitHub Personal Access Token. Generate one at https://github.com/settings/tokens/new with **only** the `public_repo` scope (nothing else — the principle of least privilege matters here, and you'll see why below).

Then submit:

```powershell
wingetcreate submit --token ghp_yourTokenHere p:\WINDOWS\FileDo\winget
```

Output:

```
Manifest validation succeeded: True
Submitting pull request for manifest...
Pull request can be found here: https://github.com/microsoft/winget-pkgs/pull/365587
```

Behind the scenes, `wingetcreate`:

1. Validates the manifest set against the schema again.
2. Forks `microsoft/winget-pkgs` to your account if you don't already have a fork.
3. Creates a branch named `<PackageId>-<version>-<uuid>` on your fork.
4. Copies your manifests into `manifests/s/SerZhyAle/FileDO/<version>/`.
5. Commits with a standard message format.
6. Opens a PR titled `<PackageId> version <version>` against `microsoft/winget-pkgs:master`.

### Blocker #2 — the leaked token

`wingetcreate`'s output explicitly warned:

> Warning: Using the `--token` argument may result in the token being logged.

I'd already pasted the token to my AI assistant in the chat to run the submit, which means it's now in transcript history. The right response: **revoke the token immediately** at https://github.com/settings/tokens after the PR is open. With `public_repo` scope only, the blast radius if the token leaked publicly is push-access to my own public repos — annoying, not catastrophic. If I'd granted broader scope (e.g. `repo` for private), the impact would have been much worse. Always grant the minimum needed scope, and revoke any token that's been pasted into a place you don't fully control.

The cleaner alternative `wingetcreate` supports is interactive browser auth — it opens a device-code flow and you log in once. No token in your shell history.

---

## 7. The PR template checklist & schema-version mismatch

The PR template that microsoft/winget-pkgs auto-applies includes:

```
- [ ] Have you checked that there aren't other open pull requests for the same manifest update?
- [ ] This PR only modifies one (1) manifest
- [ ] Have you validated your manifest locally with winget validate --manifest <path>?
- [ ] Have you tested your manifest locally with winget install --manifest <path>?
- [ ] Does your manifest conform to the 1.12 schema?
```

That last line was a problem.

### Blocker #3 — wrong schema version

I'd written my manifests against schema **1.10.0** based on what I thought was the current version. The actual current schema is **1.12.0**, listed in `microsoft/winget-pkgs/doc/manifest/schema/1.12.0/`. The PR template explicitly asks for 1.12.

`wingetcreate` had silently written `ManifestVersion: 1.10.0` into the files even though it's a 1.12-aware tool — it appears to honor whatever version you put in the input. So I needed to bump.

### The surgical fix

A naive "edit the local files, rerun `wingetcreate submit`" would have opened a *second* PR. I needed to push directly to the existing PR's branch on my fork.

The challenge: cloning my fork of winget-pkgs is genuinely massive. I tried `git clone --depth 1 --branch <pr-branch>` and aborted after several minutes and 150MB+ of `.git` data with no end in sight.

The trick is **partial clone with sparse-checkout**, which only fetches the path you care about:

```bash
mkdir winget-pkgs-fork && cd winget-pkgs-fork
git init -q
git remote add origin https://github.com/SerZhyAle/winget-pkgs.git
git config core.sparseCheckout true
echo "manifests/s/SerZhyAle/FileDO/" > .git/info/sparse-checkout
git fetch --depth 1 origin <pr-branch>
git checkout -q FETCH_HEAD
```

Done in seconds. Only the three manifest files were materialized.

The next subtlety: the original files used **CRLF** line endings (Windows convention; what `wingetcreate` produces). My local edits had reproduced them with **LF** endings. If I'd just overwritten the files, every line would show as changed in the PR diff — a noise-fest that reviewers hate.

I patched the version strings in place while preserving the CRLFs:

```powershell
$dir = '...\winget-pkgs-fork\manifests\s\SerZhyAle\FileDO\2604271301'
Get-ChildItem "$dir\*.yaml" | ForEach-Object {
  $p = $_.FullName
  $t = [System.IO.File]::ReadAllText($p, [System.Text.Encoding]::UTF8)
  $t = $t -replace '1\.10\.0\.schema\.json','1.12.0.schema.json'
  $t = $t -replace '(?m)^ManifestVersion: 1\.10\.0','ManifestVersion: 1.12.0'
  [System.IO.File]::WriteAllText($p, $t, [System.Text.UTF8Encoding]::new($false))
}
```

Result: a clean 6-line diff (two lines per file × three files) that just bumps the version numbers.

```bash
git config core.autocrlf false   # don't let git rewrite CRLF on commit
git commit -am "Bump manifest schema 1.10.0 -> 1.12.0"
git push origin <pr-branch>
```

The PR's CI re-triggered automatically on the new commit.

---

## 8. What the PR's automated validation does

Within ~10–30 minutes of opening (or updating) the PR, several Azure Pipelines checks run:

- **ManifestValidation** — same `winget validate` you ran locally, but against the latest schema rules.
- **InstallerValidation** — actually downloads the URL, verifies the SHA256, runs SmartScreen on the file, and attempts a sandbox install.
- **URLValidation** — checks that all URLs return reasonable HTTP statuses and aren't on a blocklist.
- **DefenderScan** — antivirus scan of the binary.

If everything's green, a label like `Validation-Completed` is added and the PR enters the moderator queue. First-time publishers may get extra scrutiny — expect anywhere from a few hours to a couple of days.

---

## 9. After merge

Once a maintainer merges the PR:

1. The community source index rebuilds (~30–60 minutes).
2. `winget search FileDO`, `winget search SerZhyAle`, and tag-based searches like `winget search fake-capacity` all start finding the package.
3. `winget install SerZhyAle.FileDO` works from any Windows machine with no flags or admin opt-in.

---

## 10. The follow-up release flow

For every future release the cycle is:

```bash
git tag v<new-version>
git push origin v<new-version>
# wait for CI to publish the release
```

Then one command updates the manifests and opens a new PR:

```powershell
wingetcreate update SerZhyAle.FileDO `
  --version <new-version> `
  --urls https://github.com/SerZhyAle/FileDO/releases/download/v<new-version>/FileDO-<new-version>-windows-x64.zip `
  --submit `
  --token <pat>
```

`wingetcreate update` fetches the new zip, computes the SHA256 itself, copies the latest version's locale/installer fields forward, opens the PR. ~30 seconds of effort per release.

---

## 11. Lessons worth remembering

1. **Empty workflow files silently break a repo.** A 0-byte `build.yml` failed for weeks before getting Actions auto-paused. Either commit a real workflow, delete the file, or comment everything out — never leave an empty trigger.
2. **Tag pushes don't retry when Actions is disabled.** If you enable Actions after pushing a tag, you have to push a new tag (or use `workflow_dispatch`) to re-trigger.
3. **Match the PR template's schema version.** Don't trust your memory; check `microsoft/winget-pkgs/doc/manifest/schema/` for the highest directory.
4. **`wingetcreate submit` is dramatically faster than a manual PR**, but it does write your token to the command line. Use device-code auth, or revoke the PAT immediately after.
5. **Sparse-checkout, not full clone**, when you need to touch a single path in a giant repo. `git config core.sparseCheckout true` + a one-line `.git/info/sparse-checkout` is gold.
6. **Preserve line endings on Windows-authored manifests.** Tools like `wingetcreate` emit CRLF; `sed` with default settings will rewrite them and produce huge no-op diffs. Use PowerShell's `[System.IO.File]::ReadAllText` / `WriteAllText` for in-place edits.
7. **Source-of-truth your manifests.** I keep the latest version's manifests committed in [`winget/`](../winget/) of the FileDO repo. They mirror exactly what's in the live PR, so the next release just needs a version bump and a new SHA — no archaeology required.

---

## 12. Files in this repo that are part of the winget pipeline

- [`.github/workflows/release.yml`](../.github/workflows/release.yml) — builds + publishes the release zip on tag push.
- [`winget/SerZhyAle.FileDO.yaml`](../winget/SerZhyAle.FileDO.yaml) — version manifest.
- [`winget/SerZhyAle.FileDO.installer.yaml`](../winget/SerZhyAle.FileDO.installer.yaml) — installer manifest.
- [`winget/SerZhyAle.FileDO.locale.en-US.yaml`](../winget/SerZhyAle.FileDO.locale.en-US.yaml) — locale manifest.
- [`README.md`](../README.md) → "Installation" section — user-facing install instructions.

That's the whole story. Total elapsed time, including all blockers and false starts: about an hour. For your project, knowing the gotchas in advance, it should be 15 minutes.
