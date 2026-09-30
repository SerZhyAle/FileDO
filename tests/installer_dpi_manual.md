# Manual proofs: rail at 100 % / 150 % (SP-0016 T4) and the MSI / setup EXE (SP-0030 PKG-01, PKG-02)

Release-queue items 31-33. None of these can be proven by the build gate. Record each result (PASS / FAIL, date,
build stamp, evidence path) under `tests/evidence/`, then tick the item in `PLAN/release-queue.md`.

Package under test: `dist\FileDO-<stamp>-windows-x64.msi` and `dist\FileDO-<stamp>-setup.exe` (unsigned; expect a
SmartScreen / "unknown publisher" prompt). Stamp `2609302145` carries MSI ProductVersion `26.9.43065`
(`yy.M.((dd-1)*1440+HH*60+mm)`), features `Main`, `ExplorerIntegration`, `DiskContainerIntegration`, `DesktopShortcut`.

## Item 31 - SP-0016 T4: the rail at 100 % and 150 %

BLOCKED until the mixed-DPI defect is fixed (SP-0016 section 8, `rail-100-moved-defect/`): after a move to a display at
another scale the fonts keep the old size, and a window created on such a display re-scales nothing, because
`Ui.Px` is computed in `BuildLayout` before the handle exists. `ShellForm.OnDpiChanged` only re-states `MinimumSize`
and `LayoutRail`. No ticket for the fix exists yet. Do not spend a run on the proof before it lands.

After the fix, per scale (100 %, 150 %, and 175 % as the control):

1. Set the display to the scale in Settings > Display, sign out and in (or use a display already at that scale).
2. `.\msix\make-screenshots.ps1 -Language en -Page capacity -Theme light -ClientWidth 1400 -ClientHeight 900 -Live
   -CollapsedGroups rail_group_tidy,rail_group_erase -Monitor <\\.\DISPLAYn> -OutDir <evidence dir>\<scale>\light`
   and the same with `-Theme dark`. The script kills the exe itself, so no placement is saved.
3. Also launch straight onto the display (no drag) and once drag from a display at another scale.
4. PASS when, in every shot: rail rows and group headers are 44 design px (77 device px at 175 %, 44 at 100 %, 66 at
   150 %); labels, headings and body text are the size of the scale, not of the previous display; nothing wraps or
   clips in the rail; the chevron is down on folded groups and up on open ones; no red-cross paint error.

Displays on this machine: DISPLAY2 175 %, DISPLAY1 225 %, DISPLAY5 100 %. No 150 % display: set one temporarily
(owner's call - it changes the desktop layout) or use a second machine.

## Item 32 - SP-0030 PKG-01: MSI on a clean VM

Use a clean VM. Windows Sandbox is present on this host and is a clean machine per start; no Hyper-V VM exists.
A Sandbox forgets everything on close, so one session = one scenario. Map `dist\` (and, for step 4, a folder with
the previous release's MSI) read-only:

```xml
<Configuration>
  <MappedFolders>
    <MappedFolder><HostFolder>P:\WINDOWS\FileDo\dist</HostFolder><ReadOnly>true</ReadOnly></MappedFolder>
  </MappedFolders>
</Configuration>
```

Save as `filedo.wsb`, open it; the folder appears as `C:\Users\WDAGUtilityAccount\Desktop\dist`.

1. Fresh install by double click, Next through the feature tree with defaults. Expect: Start menu `FileDO` and
   `FileDO Disk Manager`, desktop icon, `filedo --version` works in a new console (PATH), right-click a `.fd-sec`
   file shows the two FileDO verbs, Apps and features lists one `FileDO`, version `26.9.43065`.
2. Silent: `msiexec /i <msi> /qn ADDLOCAL=Main` - no Explorer verbs, no desktop icon, exit 0.
3. Uninstall from Apps and features: program folder, PATH entry, `HKLM\Software\Classes\FileDO.*` keys, shortcuts
   all gone.
4. Same-day upgrade (the PKG-01 defect): install MSI A, then MSI B built later the same day (run `build.ps1`
   twice a few minutes apart and keep the first pair). Expect exactly ONE `FileDO` entry in Apps and features,
   carrying B's version; MSI A's files replaced. Then run A over B: Windows Installer must refuse with
   "A newer version of FileDO is already installed."
5. Upgrade over the last published release (`gh release download v2609260512 --pattern "*.msi"`, read-only): the
   old four-field version must upgrade cleanly to `26.9.43065`, one entry, and a feature you deselected in the old
   install stays deselected (`MigrateFeatures`).

## Item 33 - SP-0030 PKG-02: setup EXE, Modify, upgrade of a previous install

Fresh Sandbox, `dist\` mapped.

1. Run `FileDO-<stamp>-setup.exe`: the feature tree appears (the bootstrapper shows the MSI's own UI).
2. Apps and features > FileDO shows ONE entry with both Modify and Uninstall (PKG-02 defect: Uninstall only).
3. Modify: clear `ExplorerIntegration`, finish. Expect the two Explorer verbs and the `.fd-sec` type gone while
   `FileDO` and PATH stay. Modify again: tick it back; verbs return. Check with a right-click and
   `reg query HKLM\Software\Classes\FileDO.SecureContainer`.
4. Repair puts the keys back after deleting one by hand.
5. Upgrade of a previous install (canon invariant 6): in a fresh Sandbox install the previous release's setup EXE
   (`gh release download v2609260512 --pattern "*setup.exe"`), untick `ExplorerIntegration` in it, then run the new
   setup EXE. Expect one entry, new version, `ExplorerIntegration` still off, no leftover old entry.
6. Uninstall from Apps and features removes both the bundle and the MSI, leaving no entry and no files.

Do not run any of this on the owner's host: it has FileDO `26.9.42641` installed, and an upgrade there would be the
only real prior-install proof but changes the owner's own setup. Ask before doing it.
