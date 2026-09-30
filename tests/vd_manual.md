# Virtual disks (`.fdd`) - the manual surface checklist

What `go test`, `filedo_win.exe --selftest` and `tests/vd_batch.lst` cannot reach: the installer, the
real registry, Explorer, the consent prompt, a real volume, the window on screen, and the Microsoft Store
build. The owner runs it at the screen before a release that carries the feature, on Windows 11 and
once on Windows 10, and attaches what each step names as its evidence.

Ground rules:

- Use a scratch folder, `C:\vdtest\` below, never a folder that holds real data. Every container is
  created there and destroyed at the end.
- Run the elevated steps from an **elevated** PowerShell and everything else from a normal one. The
  consent prompt appearing - or not - is part of what is checked.
- Where a step says *sentence*, copy the console output into the evidence as it is; a paraphrase proves
  nothing.
- A step that does not do what it says is a finding: note the step number, the output and a screenshot,
  and go on to the next step unless it says to stop.

The containers used below, made once at the start (normal console):

```powershell
mkdir C:\vdtest; cd C:\vdtest
filedo vd new plain.fdd 1G                  # obfuscated (no password)
filedo vd new vault.fdd 1G vault            # encrypted - type a test password twice
filedo vd new spare.fdd 1G                  # for format, destroy and the unclean case
```

## 1. The installer (setup EXE or bare MSI)

1. [ ] **Before.** On a machine with no FileDO installed:
   `reg query HKLM\Software\Classes\.fdd /s` and
   `reg query HKLM\Software\Classes\FileDO.DiskContainer /s` - both *ERROR: The system was unable to
   find the specified registry key or value*. Save both outputs.
2. [ ] **Install with every feature** (the setup EXE, clicking through). The *Choose what to install* page
   shows **Disk container files (.fdd)** as its own feature, ticked; its description says what a
   double-click does. Screenshot the page with that feature selected.
3. [ ] **After.** The same two `reg query` commands. Expected:
   - `.fdd`: `(Default) REG_SZ FileDO.DiskContainer`, and `OpenWithProgids` with
     `FileDO.DiskContainer REG_SZ` (empty data);
   - `FileDO.DiskContainer`: `(Default)` and `FriendlyTypeName` = `FileDO disk container`,
     `FileDO.RegisteredBy` = `C:\Program Files\FileDO\filedo.exe`, `DefaultIcon` =
     `C:\Program Files\FileDO\icons\content.disk-container.ico`, `shell` (Default) = `mount`,
     `shell\mount` MUIVerb `Mount with FileDO` and command `"C:\Program Files\FileDO\filedo_win.exe" "%1"`,
     `shell\mountro` MUIVerb `Mount read-only` and command `.. filedo_win.exe" --mount-ro "%1"`,
     `shell\unmount` MUIVerb `Unmount` and command `.. filedo_win.exe" --unmount "%1"`.
   Save both outputs. The icon file exists at that path.
4. [ ] **Uninstall** (Settings > Apps > FileDO > Uninstall), then both `reg query` commands again: both keys
   are gone. Save both outputs.
5. [ ] **Deselect on the Customize page.** Install again, untick *Disk container files (.fdd)*, finish.
   Both `reg query` commands find nothing, while the `.fd-sec` type and the `File DO..` group are there.
   Then Settings > Apps > FileDO > **Change**, tick the feature back: both keys appear. Uninstall.
6. [ ] **Deselect with ADDLOCAL.**
   `msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main,ExplorerIntegration,DesktopShortcut`
   - no `.fdd` keys. Uninstall, then
   `msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main,ExplorerIntegration,DiskContainerIntegration,DesktopShortcut`
   - the keys of step 3. Keep this install for sections 3 to 5.
7. [ ] **An upgrade keeps the choice.** With the feature deselected, install the next build over it: the
   `.fdd` keys stay absent (`MigrateFeatures`). Optional when no second build is at hand.
8. [ ] **Start menu.** After an install (step 2, and again in step 5 with *Disk container files (.fdd)*
   unticked) the Start menu shows **FileDO** and **FileDO Disk Manager**; the second opens the Disk Manager
   straight - no shell window in front of it. Screenshot the Start menu folder and the opened window. Then
   Uninstall (do this last if step 6's install is still needed): both entries are gone. Screenshot again.

## 2. `vd register` on the real HKCU (portable zip, no install)

On a machine (or account) with no installed FileDO, from the unzipped portable build:

1. [ ] `reg query HKCU\Software\Classes\.fdd /s` and `reg query HKCU\Software\Classes\FileDO.DiskContainer /s`
   - nothing. Save.
2. [ ] `filedo vd register` - it prints what it wrote. The same two queries now show the shape of
   section 1 step 3 under HKCU, with the command lines pointing at the unzipped folder. Save.
   A `.fdd` file in Explorer shows the disk container icon (refresh the folder if it does not).
3. [ ] `filedo vd unregister` - both keys gone. Save both queries.
4. [ ] **HKLM wins.** With the installer's registration present (section 1 step 6), a per-user
   `filedo vd register` stands down and says a machine-wide registration is in place; HKCU stays empty.
5. [ ] **Machine-wide.** From an elevated console with no installer registration: `filedo vd register
   -all-users` writes HKLM, `filedo vd unregister -all-users` removes it. Save the queries.

## 3. Explorer: the double-click and the two verbs

With the install of section 1 step 6. For each container, close every FileDO window first.

1. [ ] **Clean obfuscated** (`plain.fdd`): double-click. The FileDO window opens on the Mount page, the
   consent prompt appears once, and the volume mounts at the first free letter without another question
   (the first mount formats it NTFS). The page says the container is obfuscated, not encrypted.
   Screenshot the page after the run.
2. [ ] **Mounted already**: double-click `plain.fdd` again. No FileDO window: Explorer opens its drive.
3. [ ] **Unmount verb**: right-click `plain.fdd` > (Windows 11: *Show more options* >) **Unmount**. The
   window opens on Unmount and runs; the drive letter disappears.
4. [ ] **Encrypted** (`vault.fdd`): double-click. The window opens on Mount and waits with an empty,
   masked password box; nothing is mounted until the password is typed and Run is pressed. Type it: the
   volume mounts. Check the process list meanwhile (`Get-CimInstance Win32_Process -Filter "Name='filedo.exe'" |
   Select CommandLine`): the line carries `pe:FILEDO_SHELL_CRED`, never the password. Unmount it.
5. [ ] **Mount read-only verb**: right-click `plain.fdd` > **Mount read-only**. It mounts; creating a
   file on the drive fails with a write-protected error. Unmount.
6. [ ] **Unclean**: mount `spare.fdd` from the console (`filedo spare.fdd mount`), then end the block
   server without unmounting (Task Manager > end the `filedo.exe` that is serving it, or pull power on a
   test VM). `filedo spare.fdd info` says it was not closed cleanly. Double-click `spare.fdd`: the window
   reports the unclean state with the time of the last complete save **before** anything is attached,
   and waits. Screenshot. Mount it read-only first, then unmount; a normal mount after that marks it
   clean again.
7. [ ] **Unmount on a container that is not mounted**: right-click `plain.fdd` > **Unmount**. The page says
   it is not mounted and runs nothing.
8. [ ] **Windows 10**: repeat steps 1 and 3 on Windows 10, where the verbs are in the first-level menu.

## 4. The console on a real volume

Elevated where the step says so; otherwise a normal console (the consent prompt appears).

1. [ ] `filedo plain.fdd mount` - consent prompt once, `Mounted at X:.`; copy a file onto X:.
   `filedo vd status` lists it. `filedo X: unmount` - consent prompt, the letter goes.
2. [ ] **ram**: `filedo vd new r.fdd 512M ram`, mount it, copy a file, `filedo X: save` (consent prompt),
   `filedo X: unmount`. Mount again: the file is there. Unmount; `filedo r.fdd destroy force`.
3. [ ] **format, NTFS**: `filedo spare.fdd format` - asks `y/N`; answer `y`. Mount: an empty NTFS volume.
   While it is mounted, `filedo spare.fdd format force` is refused with exit 8 (`$LASTEXITCODE`). Unmount.
4. [ ] **format, exFAT**: `filedo spare.fdd format fs exfat label EXF force`. Mount: an empty exFAT volume
   named `EXF` (Explorer > Properties). Unmount.
5. [ ] **grow**: `filedo spare.fdd grow 2G`; the output says Windows sees the larger disk at the next mount.
   Mount, extend the volume in Disk Management (*Extend Volume*), check the new size. Unmount.
6. [ ] **A batch never raises the prompt**: from a normal console, a one-line list `plain.fdd mount` run
   through `filedo from` ends with the sentence that mounting needs administrator rights and a batch never
   raises a consent prompt, and nothing is mounted. From an elevated console the same list mounts; unmount.
7. [ ] **MSiSCSI stopped** (elevated: `Stop-Service MSiSCSI`): `filedo plain.fdd mount` starts the service
   in its elevated step and mounts. Unmount.
8. [ ] **MSiSCSI disabled** (elevated: `Set-Service MSiSCSI -StartupType Disabled; Stop-Service MSiSCSI`):
   `filedo plain.fdd mount` ends with exit 7 and the sentence *the Microsoft iSCSI Initiator service
   (MSiSCSI) is disabled*; nothing is mounted. Run the Mount page in the window the same way: it shows the
   exit-7 sentence. Save both. Restore: `Set-Service MSiSCSI -StartupType Manual`.
9. [ ] **Consent refused**: `filedo plain.fdd mount`, answer **No** at the prompt: exit 7, *administrator
   consent was not given; nothing was changed*.

## 5. The window

1. [ ] **A mount outlives the window.** Mount `plain.fdd` from the window's Mount page, close the window
   (it may ask about a running job - there is none). The drive and its `filedo.exe` block server are still
   there (`filedo vd status`). Open FileDO again: the *Disks and mounts* page lists it as mounted. Unmount
   from that row.
2. [ ] **Every Disks page in both themes.** Settings > theme Light, then Dark: a screenshot of each of the
   seventeen pages (Disks and mounts, Create a disk, Mount, Unmount, Disk info, Verify a disk, Export an
   image, Save now (ram), Compact, Grow, Format, Seal a copy, Clone, Change password, Destroy, Auto-mount,
   Remember a name) with a container chosen. `msix\make-screenshots.ps1 -Theme light|dark` takes the rail
   rows by their `rail:<key>` names. No page shows a red-cross placeholder, and no line is cut off.
3. [ ] **Five languages.** In Settings switch to Russian, Ukrainian, German and French and open Create a
   disk: the empty-password line says obfuscated, not encrypted, in that language - never the word for
   encrypted or protected on its own.
4. [ ] **Change password refuses an empty new one** and names Clone without a password instead.
5. [ ] **history.json after a vault mount.** Mount `vault.fdd` from the window with its password, unmount,
   then search the state folder for the password:
   `Select-String -Path "$env:LOCALAPPDATA\FileDO\state\*","$env:LOCALAPPDATA\FileDO\reports\*","$env:LOCALAPPDATA\FileDO\runs\*" -Pattern '<the test password>' -SimpleMatch`
   finds nothing, and the `history.json` entry of that run shows `pe:FILEDO_SHELL_CRED` in its command.
   Attach the entry.

## 5b. The Disk manager (SP-0063)

The second window of `filedo_win.exe`. Run it with the containers of the top of this file registered
(`filedo vd add C:\vdtest\plain.fdd as plaintest`, likewise `vaulttest`, `sparetest`).

1. [ ] **Every way in.** The Start menu entry *FileDO Disk Manager* opens the manager and **no** main window;
   the *Disk manager* button at the top right of the FileDO window and the first row of the Disks group in its
   rail open it; `filedo_win.exe --disks` again while it runs brings it forward and opens no second copy.
2. [ ] **The two windows open each other.** In a manager started from the Start menu, *Main window* (and
   Ctrl+Shift+O) opens the FileDO window; in the FileDO window, *Disk manager* (and Ctrl+Shift+D) brings the
   manager forward. Close them in turn: the program ends only when the last one has closed.
3. [ ] **First run.** With `HKCU\Software\FileDO\DiskManagerWelcomed` absent, the first opening shows *Welcome
   to the Disk manager* once. *Not now*, Escape and the close box all leave a working window, and the next
   start does not show it again; Help > *First steps..* opens it whenever asked. *Create my first disk..* opens
   the Create a disk page in the FileDO window, *Add a .fdd file I already have..* opens the file dialog, and
   the guide link opens the browser only when clicked - Resource Monitor > Network shows nothing before that
   click. With an empty list the same three ways to start are on the list area itself.
4. [ ] **State stays true (within 5 s each).** From a console mount `plain.fdd`: its row says *Mounted* at the
   letter. Eject the drive in Explorer: *Not mounted*. Mount again and end that container's `filedo.exe` block
   server in Task Manager: *Server gone - volume offline*. Sign out and in with an auto-mount task: the row is
   mounted at the logon. A screenshot of each.
5. [ ] **One gesture.** Double-click and Enter on a disk at rest mount it (consent prompt); on a mounted one
   they open the drive in Explorer and never unmount. Toolbar *Unmount*; a `ram` disk with data says the
   unsaved amount first, and *Save now* writes it. Select three mounted disks and *Unmount*: the window says
   Windows will ask three times, and it does.
6. [ ] **Keyboard and names.** F1, then every key of the table it shows does what the table says (Ctrl+N, O, M,
   Shift+M, U, E, S, I, Shift+V, Enter, Del, C, A, Menu, F5, F, Esc, F1). Tab visits the toolbar, the filter,
   the list, the detail pane and the strip in that order, and no toolbar button carries a frame when the window
   opens. With Narrator every button is read by its name - the glyph-only ones (Refresh, Help, the filter's
   clear cross, the detail pane's close cross) by their canonical names.
7. [ ] **Look.** Light and Dark, at 100 % and 150 % display scaling and once on a second monitor of another
   scale, in English, Russian and German: the selected row's text is readable (never dark on dark), the hover
   tint, the header, the state glyphs, the captions of disabled buttons in Dark, the cross at the right edge of
   the detail pane, and **no white horizontal scroll bar** at the opening width. Eight screenshots.
8. [ ] **A mount outlives the manager.** With two disks mounted close the manager: it says once *Your disks stay
   mounted*, both stay, and a fresh manager lists them. *Don't say this again* is remembered.
9. [ ] **Drag and drop.** A `.fdd` dropped on the list is added (a taken name asks for another); a `.vhdx`
   asks one confirmation before it is mounted; a `.txt` is refused with a sentence.
10. [ ] **Help.** Each entry of the Help menu (guide, all guides, website, documentation on GitHub, report a
    problem) opens the right page, and none opens before the click.
11. [ ] **Nothing destructive by accident.** *Format..* and *Destroy..* open their job page in the FileDO window
    with the container chosen and still ask for the typed `FORMAT` / `DESTROY`; Delete only removes the name
    from the list; double-click, Enter and Backspace never reach either.
12. [ ] **No credential anywhere.** Mount `vault.fdd` from the manager with its password, unmount, then repeat
    the search of step 5.5: nothing in the run report, `history.json` or the event stream.

## 6. Destroy and the auto-mount task

1. [ ] `filedo vd add C:\vdtest\plain.fdd as plaintest`, then `filedo vd auto plaintest logon` (consent
   prompt). Task Scheduler shows the task. Sign out and in: `plain.fdd` is mounted. Unmount it.
2. [ ] `filedo vd auto vaulttest logon` after `filedo vd add C:\vdtest\vault.fdd as vaulttest` is refused:
   an encrypted container is never mounted automatically. Note what the window's Auto-mount page says for
   the same container, and compare (a mismatch is a finding).
3. [ ] `filedo plain.fdd destroy force` is **refused** while the task exists, with *run: filedo vd auto off
   plaintest first*. The file is still there.
4. [ ] `filedo vd auto off plaintest` (consent prompt), then `filedo plain.fdd destroy` asks `y/N`; answer
   `y`. The file is gone, and the output says the name `plaintest` was forgotten in the same run
   (`filedo vd list` no longer shows it).
5. [ ] `filedo spare.fdd destroy wipe` asks for the typed `WIPE`, overwrites, removes.

## 7. The Microsoft Store build

On a machine with the Store build installed (or the `-SelfSign` package from `msix\build-msix.ps1`) and no
other FileDO:

1. [ ] Double-click a `.fdd`: the FileDO window opens on that container and says this build cannot attach
   disks; it offers no Mount button that would run. There are no Mount, Mount read-only or Unmount entries
   in the right-click menu.
2. [ ] `filedo C:\vdtest\vault.fdd mount` from a terminal ends with exit 6 and the sentence that the
   Microsoft Store build of FileDO cannot mount - a packaged app can neither configure the Windows iSCSI
   initiator nor ask for administrator rights. Save it. The same for `unmount`, `save`, `format`,
   `vd auto` and `vd register`.
3. [ ] `info`, `verify`, `export <dest> vhd`, `grow`, `compact`, `clone <new.fdd> nopass`, `pass`,
   `destroy force`, `vd new`, `vd list`, `vd status`, `vd add` and `vd forget` all work (exit 0).
4. [ ] The listing (Partner Center preview) promises no mounting in any language.
5. [ ] **The Disk manager in this build.** It opens and lists the disks; *Mount*, *Mount read-only*, *Mount
   as..*, *Mount image..*, *Unmount*, *Save now*, *Turn on/off auto-mount* and *Format..* are **absent** (hidden,
   not greyed) from the toolbar, the row menu, the More menu and the detail pane, while *New disk..*, *Add..*,
   *Info*, *Verify* and *Export..* remain. The first-steps window (Help > *First steps..*) says this build
   cannot mount and that the setup program from the website can.

## 8. Clean up

`filedo vd list` and `filedo vd status` show nothing of this run; `Remove-Item C:\vdtest -Recurse`;
Settings > Apps > FileDO > Uninstall, then the two `reg query` commands of section 1 find nothing.
