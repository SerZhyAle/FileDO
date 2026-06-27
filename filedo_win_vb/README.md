# FileDO GUI (filedo_win.exe)

A VB.NET (Windows Forms, .NET Framework 4.8) command builder for `filedo.exe`. It
assembles **any** FileDO command from a few controls, explains what each one does,
shows a live editable preview, and runs it in a console window.

## What it builds

- **Language**: English / Русский / Українська / Deutsch / Français, remembered
  between runs (HKCU\Software\FileDO\GuiLang)
- **Target**: device / folder / network / file - for **device** the path becomes a
  **drive-letter picker** populated from the actual drives
- **Operation** (one dropdown): info, speed, test, fill, clean, cd (duplicates),
  copy, fastcopy, synccopy, balanced, maxcopy, smartcopy, safecopy, wipe, compare,
  check, probe, recover, from, hist, help
- **Source + Destination** fields for copy and compare
- **Size** for speed / fill
- **Flags**: max, del, nodel, short, hist, `--force` (wipe)
- **Duplicate options** (old / new / abc / xyz, move to folder) for `cd`
- **Delete rule** dropdown for `compare`

## Inline help

- A **help panel** under the controls explains the selected operation and what it
  produces, shows an **example** for the chosen target type, and lists the meaning
  of every active flag - all in the chosen language.
- **Tooltips** on the operation and flag controls repeat the short description on hover.
- Choosing the **system drive** (e.g. `C:`) for a write test (test/fill/speed) shows
  a note: FileDO redirects those writes to `%TEMP%\FileDO_Operations` (fallback
  `C:\TEMP`) and asks to confirm, to protect Windows.

The **Command** box is editable - tweak by hand for anything the builder does not
cover. **Copy command** puts it on the clipboard; **RUN** executes it.

## How it finds filedo.exe

`filedo_win.exe` runs `filedo.exe` from the same folder if present, otherwise from
`PATH` (the winget portable alias or the Microsoft Store appExecutionAlias). So it
works the same whether launched from the portable zip, the installed location, or
the Store tile.

## Build

```powershell
msbuild FileDOGUI.vbproj /p:Configuration=Release /p:Platform=AnyCPU
```

Output: `bin\Release\filedo_win.exe`. The repo's `build.ps1` and the release CI
build this automatically and ship it in the winget package, the portable zip, the
MSI, and the Microsoft Store (MSIX) package, where it is the clickable tile.

## Usage

```powershell
filedo_win              # if on PATH (winget / Store)
filedo_win.exe          # next to filedo.exe in the portable zip
filedo_win.exe -debug   # writes filedo_win_debug.log next to the exe
```
