# FileDO - Microsoft Store (MSIX) packaging

Packages the `filedo` CLI as an MSIX for the Microsoft Store. A single `filedo`
execution alias is exposed on PATH; `filedo.exe` already contains every
subcommand (`device` / `folder` / `file` / `network` / `copy` / `check` /
`wipe` / `fill` / `hist`), so the three standalone helper exes are not shipped here.

See the reusable playbook this is derived from: `P:\WINDOWS\CyrFlip\STORE_PUBLISHING.md`.
FileDO is a Go **CLI**, so the playbook's tray/autostart/`%LOCALAPPDATA%` code phase
does not apply; the CLI-specific piece it adds is the **AppExecutionAlias**.

## Files

| File | Role |
| --- | --- |
| `AppxManifest.xml` | Manifest template: Identity placeholders, `runFullTrust`, `appExecutionAlias` (`filedo`), visual assets. |
| `build-msix.ps1` | build → version remap → stage → generate logos → fill manifest → `makeappx pack` → optional self-sign. |
| `store-listing.md` | Description / features / runFullTrust justification / privacy text, pre-filled for FileDO. |
| `stage/`, `out/`, `*.pfx`, `*.cer` | Generated; git-ignored. |

## Prerequisites

```powershell
winget install Microsoft.WindowsSDK.10.0.26100   # makeappx.exe + signtool.exe
# Go must be on PATH (the script runs `go build`)
```

## 1. Verify locally (self-signed)

```powershell
.\msix\build-msix.ps1 -SelfSign
```
Prints two commands: `Import-Certificate` (run as **admin**) to trust the test cert,
then `Add-AppxPackage` to install. After install, open a **new** terminal and run:

```powershell
filedo device c:
```

Remove with `Get-AppxPackage *FileDO* | Remove-AppxPackage`.

Notes / pitfalls (FileDO-specific):
- It is a **console tool**. Launching the Start-menu tile just flashes a console that
  prints usage - that is expected; use it from a terminal via `filedo`.
- `hash_cache.json` is written next to the exe; under MSIX the install dir is
  read-only, so Windows redirects the write into the package's per-user VFS
  (`%LOCALAPPDATA%\Packages\<PFN>\LocalCache\...`). It works, but the packaged
  cache is separate from the unpackaged one. `history.json` is written to the
  current working directory. Neither blocks packaging.

## 2. Build for the Store (unsigned)

After reserving the app and reading **Product ▸ Product identity** in Partner Center:

```powershell
.\msix\build-msix.ps1 `
  -IdentityName        "<Package/Identity/Name>" `
  -Publisher           "<Package/Identity/Publisher, e.g. CN=...>" `
  -PublisherDisplayName "<Package/Properties/PublisherDisplayName>"
```

Upload the **unsigned** `out\FileDO_<ver>.msix`. Microsoft re-signs it during
certification - no paid code-signing certificate is required.

## Version scheme

The app version stamp is `yyMMddHHmm` (matches `build.ps1`). The Store requires a
4-part `Major.Minor.Build.0` with each part ≤ 65535, so the script remaps to
`YY.(M*100+D).HHmm.0` (e.g. 2026-06-14 14:30 → `26.614.1430.0`) - monotonic and
unique per minute. Override with `-Version 26.614.1430.0` to re-pack a fixed version.
