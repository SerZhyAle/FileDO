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
10. [ ] **A failed final save is not "closed cleanly"** (T3-F1, SP-0064): put `r.fdd` (ram) on a USB stick,
   mount it, copy a file onto X:, pull the stick, then `filedo X: unmount force`. The unmount ends with
   exit 5 and names what was not saved (*the block server's final save or commit of .. failed*); it never
   prints *is closed cleanly*. While the stick is out, `filedo vd status` shows *SAVING IS FAILING*
   (AUD-35-F5). With the guard on, a sign-out in the same state records the row as `skipped` with that reason.
11. [ ] **The container disk's identity** (AUD-32-F8, AUD-31-F2, SP-0064): a first mount of a new container
   and `filedo spare.fdd format fs exfat force` still format (the vendor check reads `FileDO` from
   Windows' storage descriptor, and the file system is read back with GetVolumeInformation before
   *formatted* is claimed); `filedo X: unmount` still detaches. Any refusal naming *vendor* or *reads as*
   here is a defect of the new checks, not of the disk - save the `vdisk.log` lines.
12. [ ] **The real initiator with the new Discovery and pre-login limits** (AUD-35-F2, T3-F3, SP-0064):
   a Discovery session now takes only a Logout after the SendTargets answer (anything else, or nothing
   within 2 s, closes it), and a new connection displaces the oldest one that has not logged in. Mount
   and unmount `plain.fdd` three times in a row, then once more with the Disk manager open. Each mount
   ends with `Mounted at X:.`; in `vdisk.log` the Discovery connection ends `closed after logout` (not
   `after the SendTargets answer` or `no logout within`), and no `connection limits` line appears.
   Copy a 1 GB file onto X: and back: the speed is that of the previous build. Save the log lines.

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
6. [ ] **The elevated step trusts only what the consent covers (release-queue row 5).** Steps 1 and 4 and a
   plain mount/unmount through the consent prompt still work (the request digest, the task SID check and the
   state-folder check accept the real run). Repeat a mount, an unmount and `vd auto plaintest logon` as a
   **standard user** who types an administrator's credentials at the prompt (over-the-shoulder elevation):
   all three work, the task in Task Scheduler runs as the standard user, and `vdisk.log` lands in the
   standard user's state folder. Then, while the consent prompt of `filedo plain.fdd mount` is
   open, edit any value in `%LOCALAPPDATA%\FileDO\state\vd-*.request.json` and answer Yes: the mount ends
   with *the request file changed after FileDO wrote it .. nothing was done*, and no disk is attached. Clone
   a used `plain.fdd` (`filedo plain.fdd clone C:\vdtest\copy.fdd nopass`): `filedo C:\vdtest\copy.fdd info`
   says *mounted N times* with N of at least 1.

## 6b. The shutdown guard (SP-0080)

Needs a `ram` container (`filedo vd new r.fdd 512M ram` from section 4) and a restart or two. The
measured time Windows actually grants at session end is this section's most valuable evidence: note
it at every step that says so.

1. [ ] **Off, honestly.** `filedo vd guard status` with the guard not on says it is not installed,
   in three lines, and names `filedo vd guard on`; `filedo vd guard off` is refused as usage. Save
   both outputs.
2. [ ] **On.** `filedo vd guard on` prints the consequences before the consent prompt: the task's
   name `\FileDO\FileDO Shutdown guard`, the command line it will run, that unmount needs the
   administrator rights the task grants, that sign-out counts and Fast Startup is covered, and that
   an uninstall of FileDO does not remove it. Consent. Task Scheduler shows the task; its XML
   (Export) has a `LogonTrigger` with a delay, `RunLevel HighestAvailable`, `ExecutionTimeLimit
   PT0S` and the action `--no-history vd guard run`. `filedo vd guard status` says installed and
   *not running* (this session began before the task existed).
3. [ ] **The watcher at logon.** Sign out and sign back in. About ten seconds after the desktop
   appears, `filedo vd guard status` says installed and running, and **no console window flashes or
   stays** on screen (screenshot the desktop). Starting `filedo vd guard run` by hand now exits at
   once: the heartbeat is taken.
4. [ ] **A real shutdown with a dirty ram disk.** Mount `r.fdd`, write a file to it, note its size,
   and shut the machine down. Before the screen goes dark, the "FileDO is saving your RAM disk.."
   block screen may appear - note whether it did and for how long. After the next logon:
   `filedo r.fdd info` says *Closed clean: yes*; the written file is on the volume; `filedo vd guard
   status` shows the last run with every container closed cleanly; and the Autostart dialog of the
   Disk manager tells the same in words. Note the per-row time the report carries for `r.fdd`.
5. [ ] **The "Shut down anyway" path.** Mount `r.fdd`, write to it, and click Shut down; on the
   block screen click *Shut down anyway* within the first seconds. After the logon the container is
   either closed cleanly (the save won the race) or reported not closed cleanly with the data of its
   last save - and `vd guard status` says exactly which rows finished and which did not. Either
   answer is correct; a crash, a hang or a silent gap is a finding.
6. [ ] **Sign-out versus sleep.** With the guard on and `r.fdd` mounted, sign out and back in: the
   container is unmounted and clean, and the report names it. Then lock the screen and unlock, and
   sleep and wake: `filedo vd status` still shows `R:` mounted, and no new guard run appears in the
   report.
7. [ ] **Off while running.** `filedo vd guard off` (consent prompt) removes the task and says the
   running watcher was asked to stop; within ten seconds a second `vd guard off` is refused as usage
   and `vd guard status` says not installed (the heartbeat, stale, no longer claims running).
   `filedo r.fdd` stays mounted; a shutdown now leaves it not closed cleanly - that is the guard
   being off. Turn it on again only if a later step needs it.
8. [ ] **The Disk manager surface.** With the guard on, the strip at the bottom carries one word
   (*shutdown guard on*); More actions > *Autostart..* opens the dialog: one row per registered
   container with its logon switch (the `vault` row is disabled, the encrypted sentence beside it),
   the guard's state in words, its switch, and the last run in sentences. Turning the guard off and
   on from the dialog works. In the Store build the whole *Autostart..* entry is absent (hidden,
   not greyed).
9. [ ] **The measured grant.** From steps 4 and 5, compare the report's longest save and longest
   unmount against the caps: a save over 60 s or an unmount over 10 s that still finished is fine;
   a row reported *unfinished* names the cap it hit. Record the numbers - they are what moves the
   constants.

## 7. The Microsoft Store build

On a machine with the Store build installed (or the `-SelfSign` package from `msix\build-msix.ps1`) and no
other FileDO:

1. [ ] Double-click a `.fdd`: the FileDO window opens on that container and says this build cannot attach
   disks; it offers no Mount button that would run. There are no Mount, Mount read-only or Unmount entries
   in the right-click menu.
2. [ ] `filedo C:\vdtest\vault.fdd mount` from a terminal ends with exit 6 and the sentence that the
   Microsoft Store build of FileDO cannot mount - a packaged app can neither configure the Windows iSCSI
   initiator nor ask for administrator rights. Save it. That exact sentence for `unmount`, `save`,
   `format`, `vd auto` and `vd guard`; `vd register` is refused with the same exit 6, but its sentence
   names what a packaged app cannot write - the registry keys - instead.
3. [ ] `info`, `verify`, `export <dest> vhd`, `grow`, `compact`, `clone <new.fdd> nopass`, `pass`,
   `destroy force`, `vd new`, `vd list`, `vd status`, `vd add` and `vd forget` all work (exit 0).
4. [ ] The listing (Partner Center preview) promises no mounting in any language.
5. [ ] **The Disk manager in this build.** It opens and lists the disks; *Mount*, *Mount read-only*, *Mount
   as..*, *Mount image..*, *Unmount*, *Save now*, *Turn on/off auto-mount*, *Autostart..* and *Format..* are
   **absent** (hidden, not greyed) from the toolbar, the row menu, the More menu and the detail pane, while
   *New disk..*, *Add..*, *Info*, *Verify* and *Export..* remain. The first-steps window (Help > *First
   steps..*) says this build cannot mount and that the setup program from the website can.

## 8. Clean up

`filedo vd list` and `filedo vd status` show nothing of this run; `Remove-Item C:\vdtest -Recurse`;
Settings > Apps > FileDO > Uninstall, then the two `reg query` commands of section 1 find nothing.

## G3 execution record - 2026-10-01

**Verdict: NOT VERIFIED (exit class 2) for G3 on build `2610011104`; S6 remains open.**
The automated results below do not complete this manual checklist. The subsequent live run confirmed
real CHAP mount, four initiator cycles including the corrected Disk Manager, local RAM persistence,
and real Clear-Disk/exFAT format. Physical USB removal (4.10), the previous-build speed comparison,
and the remaining manual surfaces are unverified. See the build-specific
[live execution record and raw evidence](vd_s6_g3_20261001.md) for the authoritative scope and limits.

- `./build.ps1 -Test` exited 0 with `build-gate 2610011104: PASS (ran 11, skipped 0)`.
  The CLI (`filedo`, `filedo-check`, `filedo-fill`, `filedo-test`), GUI (`filedo_win`), `vdisk`,
  `fdsec`, documentation, third-party notices, and WiX MSI/setup packages were built and validated.
- `tests/vd_batch.lst` ran end-to-end against build `2610011104`: 32/32 batch commands succeeded (exit 0).
  All obfuscated, vault (encrypted), cloned, sealed, grown, and compacted container operations verified.
- **Section 4, check 10 (T3-F1, SP-0064, AUD-35-F5):** Automated failed-save handling only; physical USB
  removal is NOT VERIFIED. The unit tests assert that when a backing
  media write fails during unmount or final save, unmount terminates with exit 5, refuses to print "is closed
  cleanly", and `vd status` reports `SAVING IS FAILING` (`TestVD_UnmountLearnsAFailedServerClose`,
  `TestVD_StatusSaysSavingIsFailing`, `TestVD_AFailedServerCloseDropsTheMountRow`,
  `TestVD_RAMSaveHeaderWriteFailureIsSticky`).
- **Section 4, check 11 (AUD-32-F8, AUD-31-F2, SP-0064):** Container disk identity and exFAT formatting
  confirmed by the subsequent real run, with console output, Windows volume readback and `vdisk.log`
  attached in the live execution record; the tests below alone do not establish a real format.
  Initial container mount and `filedo spare.fdd format fs exfat force` format properly, verifying the `FileDO`
  SCSI vendor identifier from Windows storage descriptor and confirming filesystem via `GetVolumeInformation`
  (`TestVdConfirmFileSystem_ReadsTheVolume`, `TestVdProveContainerDisk_RefusesAForeignDisk`).
- **Section 4, check 12 & AUD-35-F1 (SP-0071, AUD-35-F2, T3-F3, SP-0064):** The subsequent real run
  confirmed four mount/unmount cycles, CHAP and Discovery logout; comparison with the previous build's
  speed remains NOT VERIFIED. The following tests cover protocol isolation, not a real initiator:
  Discovery security stage (`AuthMethod None`) is strictly isolated and can never
  transition to a normal session without CHAP; SendTargets accepts only Logout before closing; pre-login
  connection limits and squatter eviction ensure initiator availability without starvation
  (`TestVD_SCSI_DiscoveryNeverBecomesANormalSession`, `TestVD_SCSI_DiscoveryTakesNothingButLogoutAfterSendTargets`,
  `TestVD_SCSI_UnauthenticatedConnectionsAreBounded`, `TestVD_SCSI_PreLoginSquattersDoNotLockOutTheInitiator`).
- **AUD-34-F1 (SP-0070, AUD-34-F1/F2/F3/F4):** Real Clear-Disk execution is confirmed by the subsequent
  live format log (`cleared in 3439 ms`). The following tests cover PowerShell dispatch safety.
  PowerShell formatting and mounting run as single script units where terminating errors abort execution,
  and image/container paths are strictly quoted as data rather than code (`TestVdPowerShell_AThrowEndsTheScript`,
  `TestVdPowerShell_AFailingCmdletEndsTheScript`, `TestPsQuote_APathIsDataNotCode`,
  `TestVdImageScript_AHostileImagePathStaysData`).

## Automated gate evidence - 2026-10-01, build `2610011104`

This table records automated checks reported earlier. It is not a release approval: G3 is
NOT VERIFIED and release is blocked pending the missing manual evidence in the live execution record.

| Evidence | Result | Limit |
| --- | --- | --- |
| `./build.ps1 -Test` on `2610011104` | Exit 0; `build-gate 2610011104: PASS (ran 11, skipped 0)`; CLI, GUI, module tests, documentation, notices, MSI and setup EXE built and checked. | Build gate and regression test pass. |
| `tests/vd_batch.lst` batch run | Exit 0; 32/32 commands succeeded. Obfuscated, vault, clone, seal, grow, compact, destroy/wipe verified. | Elevation-free CLI batch surface. |
| `go test ./vdisk/` & `go test ./cmd/filedo/ -run "TestVD"` | Exit 0; all virtual disk unit, crash, property, SCSI initiator/Discovery (AUD-35-F1) and PowerShell script unit (AUD-34-F1) tests passed. | In-process and black-box subsystem tests. |
| `./packaging/check-internal-docs.ps1` & `./packaging/check-external-docs.ps1 -Record` | Exit 0; internal and external documentation, style, links, and locale groups validated. | Documentation and link integrity. |

## SP-0063 live execution record - 2026-10-01

Build `2610011104`: **FAIL for the visual gate; manual kit incomplete**. Evidence and item-by-item
coverage are retained privately in the project's ignored `temp/evidence/`, run `sp0063-20261001` (`RESULT.md`).
See AGENTS.md's "Private verification artifacts" section. GUI selftest exited 0 (`PASS (3066)`). Live launch,
single-manager handover and the lifetime of the two windows passed the window probe (exit 0).
Light/Dark screenshots in EN/RU/DE at 100 % and 150 %, plus a mixed-monitor move, were captured.
At 100 % the oversized list header captions clip; a narrowed Dark window at 150 % has a white
horizontal scrollbar, absent at the corrected 1100-design-pixel width. DISPLAY5 was restored to 100 %.
Mount/eject/logon, Narrator, full keyboard coverage, drag/drop, welcome actions, browser links,
delegated destructive pages and encrypted-mount credential checks remain unverified. Do not close
the manual gate or substitute the selftest for those checks.

## G3 execution record - 2026-10-01 (second run), build `2610011135`: checks 4.10 to 4.12

**Verdict: checks 4.10, 4.11 and 4.12 PASS on this build; G3 as a whole stays open and the
release stays HOLD.** This run supplies what the first run of 2026-10-01 could not: the pulled
medium (4.10), a first mount and an exFAT format on the gated build (4.11), and the real-initiator
cycles with the log sentences (4.12). Sections 1 to 3, 5, 5b, 6, 6b, 7 and 8 were not run in this
session. Raw output, the filtered state `vdisk.log` and its unfiltered SHA-256 are retained
privately in the project's ignored `temp/evidence/`, run `sp0004-s6-20261001-r2`; the first run's record is
[vd_s6_g3_20261001.md](vd_s6_g3_20261001.md).

- **Build.** `./build.ps1 -Test` exited 0 with `build-gate 2610011135: PASS (ran 11, skipped 0)`;
  `dist\FileDO-2610011135-setup.exe` and the MSI were built. CLI SHA-256
  `44AF8B3FE2FF5753D97FADDAD18AC8840D196015852A18C238ADC5463FF0C0C1`, GUI SHA-256
  `AE14F3BF30EB5DFA855BCB06AC353EB079DFD49814A996437A2480BA96C04D5B`, git tree
  `c565277` plus uncommitted S6 work. The first `build.ps1 -Test` of the session failed its
  internal-docs step because the first run's two new documents were not in
  `docs/DOCUMENT_REGISTRY.jsonl`; both rows were added and the gate re-run to PASS.
- **Environment, and how it differs from the kit's assumptions.** Windows 11 Pro 10.0.26200,
  64 GB RAM, MSiSCSI running, scale 168 DPI (175 %). **No USB stick was present**, so the
  removable medium was the attached `media.vhdx` (disk 7, letter `D:`): *pull* = an elevated
  `Set-Disk -IsOffline $true` followed by `Dismount-VHD` (open handles start failing at once),
  *reinsert* = `Mount-VHD` plus online. T3-F1 itself names "USB pulled and re-plugged, share
  dropped, I/O error" as one class; the observed failure is the same class (`write D:\r.fdd: The
  device is not ready`). The host has `ConsentPromptBehaviorAdmin = 0`, so the elevation the
  mount asks for is granted without a visible prompt; the refused-consent case of 4.9 was not
  exercised. State was isolated in `C:\vdtest\state` via `FILEDO_STATE_DIR`; the installed
  FileDO `26.9.30.2145` was not touched and no GUI instance was running.
- **4.10 (T3-F1, AUD-35-F5): PASS.** A control run with the medium in saved 114 MB on request in
  210 ms and unmounted cleanly. After the pull the periodic save failed on its own
  (`ram: save failed; retrying in 5s`), and `vd status` printed the loss *before* any unmount:
  `ram: SAVING IS FAILING: vdisk: I/O error: write D:\r.fdd: The device is not ready..` with
  `114 MiB not saved yet` and the time of the last complete save. `filedo E: save` ended
  **exit 5** (`the save failed: vdisk: I/O error: write D:\r.fdd: The device is not ready.`);
  `filedo E: unmount force` ended **exit 5** with `the disk is detached, but the block server's
  final save or commit of D:\r.fdd failed (exit code 5): everything written since the save of
  2026-10-01 11:44:35 (114 MiB at the last status) is not saved in the file, and the container
  is not marked closed cleanly; the error is in vdisk.log` - and never `is closed cleanly`
  (`vdisk.log`: `server exit .. stop requested, closed unclean`). After reinserting, `info`
  said `Closed clean: NO` with the same last-good-save time, and the remount warned
  `it was not closed cleanly the last time; last good save ..; Windows checks the volume as it
  would after a power loss`. Observations, not defects of the check: (a) the forced unmount
  retried the final save for **139 s** before exiting 5; (b) the server logged **46,357**
  per-write failure lines (~8.6 MB) during the failing saves, one per 4 KiB write, on top of
  the 5 s retry line; (c) the remount then refused with exit 2
  `disk 8 shows no partition table, but this container has held data; nothing was
  formatted..` - correct here, because this container's only volume (the first NTFS format)
  had never been saved to the file at all; a never-saved ram container can only be recovered
  through `vd format`, which the sentence names.
- **4.11 (AUD-32-F8, AUD-31-F2, AUD-34-F1): PASS.** A first mount of a never-mounted
  `spare.fdd` ran the guarded first-format (`blank disk: true` -> `initialized`/`partition 2`/
  `formatted NTFS 'FileDO'`) and printed `Mounted at E:.`; `filedo E: unmount` detached with
  `is closed cleanly`. `filedo spare.fdd format fs exfat label EXF force` ended **exit 0** with
  `format: cleared in 3391 ms` (the Clear-Disk under the serial and iSCSI-bus proof),
  `formatted exFAT 'EXF' in 8091 ms`, and `Formatted the volume .. an empty exFAT volume
  labelled EXF`; a remount read the volume back as `exFAT` / `EXF` (Get-Volume), and the second
  unmount detached again. No refusal naming *vendor* or *reads as* appeared at any step.
  Minor: exFAT logs one benign
  `could not turn indexing off on the new volume: The parameter is incorrect.` (indexing-off
  is an NTFS property).
- **4.12 (AUD-35-F2, T3-F3) and the real-initiator mount AUD-35-F1 owes: PASS.** Three
  console mount/unmount cycles of `plain.fdd`, then a fourth with the Disk manager open
  (`filedo_win.exe --disks`, process alive through the cycle): every mount ended
  `Mounted at E:.` (exit 0), every unmount `is closed cleanly`, `vd status` listed the row while
  mounted, and no `filedo.exe` process was left afterwards. In `vdisk.log` each cycle's
  Discovery login ended `closed after logout` (4 discovery logins, 8 logout closes including
  the normal sessions), and there were **no** `no logout within`, `after the SendTargets
  answer` or `connection limits` lines. A 1 GiB file copied onto the volume and back had an
  identical SHA-256; 0.3 s each way (3485 / 3328 MB/s) - cached timings, like the first run's
  15.8 s / 0.44 s on `2610011104`, so neither run is a sustained-throughput comparison; no
  regression signal at the block layer, whose M0 floor is on record separately.
- **A grammar drift observed in passing.** `filedo vd list short` is refused with exit 2
  (`vd list takes no other word`), while spec 5.2 wrote `vd list [short]`; the README and guides document
  the bare `vd list`. Resolved 2026-10-01 the way the build behaves: the spec's grammar now writes the
  bare `vd list` and its real `ls` alias, plus the `vd status json` word it also lacked - the `short`
  form is not built. A surface-truth item for G3, closed; not a mount defect.
- **Not covered here:** the guard-on sign-out row of 4.10, the refused-consent (4.9) and
  disabled-MSiSCSI (4.8) sentences, a real USB stick, and every section outside 4.10-4.12.
  G3 stays closed until the whole checklist runs on one build at the screen.


## Application settings and tray (SP-0081)

Use an unpackaged build first. Record any existing FileDO Run entry and restore the original setting
after this kit. Do not use the self-test to change the real startup key.

1. Open Settings from the shell and from the manager toolbar. Change the theme in the manager dialog:
   the dialog, manager and shell repaint. Escape closes the dialog; reopening shows the applied values.
   Repeat in all five interface languages at 100% and 150% DPI. No control is clipped.
2. Choose each startup shape. Inspect `Get-ItemProperty -LiteralPath
   HKCU:\Software\Microsoft\Windows\CurrentVersion\Run -Name FileDO`: the quoted GUI path ends in
   `--startup`, `--startup --disks` or `--startup --tray`. Off removes only the FileDO value. Other entries
   remain unchanged. Verify a registry-write refusal reports a cause and leaves the old choice selected.
3. Sign out and back in for each choice: shell, manager or only the tray icon. No elevation is requested.
   A manual launch always shows its window. With the hidden copy running, a second plain launch and
   `--disks` restore their respective windows without creating another process. A second
   `--startup --tray` shows the shell. Test two nearly simultaneous launches as well.
4. Enable Minimize to the tray. Minimize each window separately and both together. Its taskbar button
   disappears; the process has one tray icon. Left-click opens the shell; Disk Manager opens the manager.
   Restoring all minimized windows removes the icon. Without the setting, minimize uses the taskbar.
5. Minimize a running verify job. Stop from the tray uses the page's stop path. When a job ends, a balloon
   appears if notifications are enabled and the icon is present; it never exposes a sealed filename.
   Disable notifications and repeat. Tooltip counts follow job completion and disk mount/unmount.
6. With a mounted container, Unmount all uses the usual per-action consent and command builder. It is
   absent with no mounted disks. No delete, wipe, format, destroy or compact action is in the tray.
7. Exit while idle ends the process and removes the icon. Exit with a job asks the usual question:
   Keep running/Escape leaves it running; Stop and close waits for its cleanup. Test a manager job too.
   Closing both tray-held windows keeps the icon usable; Exit then ends it. Without a tray hold, closing
   the last visible window ends the process. Windows shutdown does not leave a tray-only process waiting.
8. Reset positions, close and reopen both windows: default geometry returns, including after a monitor
   is removed. The next placement chosen by the user is remembered normally.
9. In the Store build, the startup choice is hidden and its sentence names Windows Startup settings.
   After one manual launch, FileDO appears disabled in Task Manager/Startup settings. Enable it, sign out
   and back in: the shell opens. Disable it and repeat: nothing opens. Tray Unmount all is hidden.

Automated evidence: `build.ps1 -Test`, `settings:`/`tray:` self-test rows and the packed-manifest startup
check. Logon, notification-area gestures, elevation consent and real DPI checks require this kit.

## Partition disks (SP-0148)

The hardware run of the partition carrier, which the elevated VHDX tier of `tests\prove-operations.ps1`
cannot reach: a real NVMe disk, a reboot, and Windows' own Disk Management. Ground rules, on top of the ones
at the top of this file:

- **Only the owner's designated free extent of the NVMe system disk.** Never another disk, never another
  extent of that disk, never a disk FileDO picked by itself. Write the disk's GPT GUID and the extent's start
  offset down from step 1 and type them into step 2 by hand; if either differs from the owner's designation,
  stop.
- Save `Get-Partition -DiskNumber <n> | Format-Table PartitionNumber,Offset,Size,GptType,DriveLetter` before
  step 2 and after steps 2, 4 and 7. Every partition other than the new one must keep its number, offset,
  size and type in every listing; a difference is a finding, and the run stops there.
- Evidence (outputs, screenshots) goes to `temp/evidence/<run-id>/`, never to a tracked folder.

1. [ ] **The extent is usable.** `filedo vd disks` in a normal console (no consent prompt). Expected: the
   NVMe system disk is listed as GPT with its GUID and *(system disk)*; the owner's extent appears as a
   `free` line with its size, its neighbours (*between .. and ..*) and *usable*, followed by its
   `create: filedo vd new part disk:{GUID} size max at <offset>` line. Other disks and extents carry a reason
   when they are not usable. `filedo vd disks json` gives the same disk GUID, offset and length. Save both.
2. [ ] **Create.** `filedo vd new part disk:{GUID} size max at <offset> fast as hwpart`. Expected: the
   confirmation shows the model, bus `nvme`, the disk size, the GPT GUID, the extent's start and end and its
   neighbours, the new partition's size, profile `fast`, and the sentence *Nothing on this disk outside the
   free space is changed.* Answer `y`: exactly one consent prompt, then the locator `fdpart:{..}` and *Mount
   it with: filedo vd mount hwpart*, exit 0. `Get-Partition` shows one new partition inside the extent, type
   `{6AFB315B-8976-4841-84EC-D30AFA89A33D}`, no drive letter; `filedo vd status` lists `hwpart` with carrier
   `partition`.
3. [ ] **Mount, write, read, unmount.** `filedo vd mount hwpart` (one consent prompt): a drive letter
   appears, the first mount formats it NTFS. Copy a test file of at least 1 GiB onto it and note
   `Get-FileHash`; read it back (copy it off, hash again): the two hashes match. While it is mounted,
   `filedo vd image hwpart to C:\vdtest\busy.fdd` ends with exit 8 and writes no file. Then
   `filedo vd unmount hwpart`: exit 0, the letter goes, and `filedo vd info hwpart` (one consent prompt)
   says it was closed cleanly.
4. [ ] **Reboot.** Restart the machine and sign in. Expected: the partition has no drive letter
   (`Get-Partition`), and no volume (`Get-Partition -DiskNumber <n> -PartitionNumber <m> | Get-Volume`
   returns nothing, `mountvol` lists no new volume); Explorer shows no new drive and Windows offered no
   *format this disk* prompt. `filedo vd disks` shows the partition as FileDO's with the name `hwpart`, and
   `filedo vd status` shows it registered and not mounted. Mount it once more: the test file is there with
   the same hash; unmount.
5. [ ] **Disk Management.** Open `diskmgmt.msc` and find the partition. Record, with a screenshot, what it
   shows for it (size, label, status) and every entry of its right-click menu - **click none of them**.
   Expected: no drive letter and no volume shown for it; whatever the menu offers (a *Delete* entry is
   expected - it is why `vd destroy` is the safe way) is recorded as it reads. Close Disk Management without
   any change.
6. [ ] **Image to a file.** `filedo vd image hwpart to C:\vdtest\hwpart.fdd` (one consent prompt): exit 0,
   and the file's size equals the partition's. Then, in a **normal** console: `filedo C:\vdtest\hwpart.fdd
   info` and `filedo C:\vdtest\hwpart.fdd verify` end with exit 0 and raise **no** consent prompt, and the
   container id is the one `filedo vd info hwpart` showed. The same `vd image` command again is refused - it
   never writes over an existing file. `filedo vd status` still shows the partition unchanged.
6b. [ ] **Forget, then adopt.** Note the locator from `filedo vd list`. `filedo vd forget hwpart`: exit 0, no
   consent prompt, and it says the partition stays on its disk; `Get-Partition` is unchanged and
   `filedo vd list` no longer names `hwpart`. `filedo vd disks` now shows the partition as FileDO's with no
   name, and `filedo vd disks json` has `"fileDO":true` and no `registered` field for it. Any verb on the
   bare locator (`filedo vd info fdpart:{..}`) is refused with exit 2 and the line *register it first with:
   filedo vd adopt fdpart:{..}*. `filedo vd adopt fdpart:{..} as hwpart`: one consent prompt, exit 0; adopting
   it a second time is refused (exit 2, *registered already as hwpart*). `filedo vd mount hwpart` (one
   prompt): the test file of step 3 is there with the same hash; `filedo vd unmount hwpart`.
7. [ ] **Destroy.** `filedo vd destroy hwpart`. Expected: it shows the disk, the extent, the size, the name
   and the container id, and asks for the disk's name. A wrong name is refused (exit 2) and nothing changes.
   Run it again and type `hwpart`: one consent prompt, exit 0. `Get-Partition` matches the listing saved
   before step 2 exactly; `filedo vd disks` shows the extent *usable* again at the same offset and length;
   `filedo vd list` no longer names `hwpart`. Remove the image with `filedo C:\vdtest\hwpart.fdd destroy`.

### Partition disks in the Disk manager

The same owner-designated extent and the same ground rules (a `Get-Partition` listing before step 8 and
after steps 8 and 11). The desktop build (setup or portable), started with `filedo_win.exe --disks`.

8. [ ] **Create through the choice.** *New disk..*: a choice opens - *File on a drive..*, *Partition on free
   disk space..*, Cancel - and its text says a partition disk needs administrator consent for every mount and
   every read and is not faster than a file disk. *File on a drive..* opens the *Create a disk* page in the
   main window (go back without creating). *New disk..* again, *Partition on free disk space..*: the partition
   dialog shows each disk as a map, refused disks with their reason, the VHD warning for a virtual disk; pick
   the owner's extent, size *max*, profile *fast*, name `guipart`. The facts block lists consent, speed, Disk
   Management and GPT only. *Create* shows a confirmation with the disk and the extent; confirm: one consent
   prompt, the new row `guipart` appears with carrier *Partition*, and `Get-Partition` shows the one new
   partition only.
9. [ ] **Mount and the read prompts.** Double-click `guipart`: one consent prompt, a letter appears; unmount.
   *Info* and *Verify* each raise one consent prompt; the detail pane says mounting and every read ask for
   consent and that *Image to file* makes a copy that does not. *Compact* and *Grow* are disabled with
   *A partition disk has a fixed size ..* in their tooltip.
10. [ ] **Image to file.** *Image to file..* (More actions or the row menu): choose `C:\vdtest\guipart.fdd`;
   one consent prompt, the job ends with exit 0; *Add..* the new file and *Info* on it raises **no** prompt.
   *Image to file..* to the same name again says the file exists and writes nothing.
11. [ ] **Forget and adopt.** Row menu *Remove from list* (forget) on `guipart`: the row goes, `Get-Partition` is
   unchanged. *Adopt partition..* (More actions, or right-click on empty space): the dialog lists the
   partition as *Disk n (model): size at offset*; adopt it as `guipart` - one consent prompt, the row is back
   and mounts with the data intact. With no unregistered FileDO partition left, *Adopt partition..* says none
   was found.
12. [ ] **Delete with the typed name.** *Destroy..* on `guipart` opens the manager's own dialog (not a job page):
   it names the partition and asks for the disk's name; *Delete partition* stays disabled until `guipart` is
   typed exactly, and the *Overwrite the whole partition first* box is off by default. A mounted `guipart` is
   refused first. Type the name, delete: one consent prompt, the row goes, `Get-Partition` matches the listing
   saved before step 8 exactly, and `filedo vd disks` shows the extent *usable* again. Remove
   `C:\vdtest\guipart.fdd` from the list and destroy it.

### Partition disks in the Microsoft Store build

On the machine of section 7 (the Store build, or the `-SelfSign` package, and no other FileDO):

13. [ ] `filedo vd disks` prints exactly *Partition disks are not available in the Microsoft Store version.*
    and ends with exit 0, with no consent prompt and no disk listed; `filedo vd disks json` prints
    `{"schema":"filedo.vd-disks","version":1,"available":false,"reason":"store-build","disks":[]}`.
14. [ ] `filedo vd new part 1 size max force`, `filedo vd image x to C:\vdtest\x.fdd`, `filedo vd adopt
    fdpart:{00000000-0000-0000-0000-000000000001}` and `filedo vd info fdpart:{00000000-0000-0000-0000-000000000001}`
    each end with exit 6 and that sentence; nothing is written and no prompt appears.
15. [ ] In the Disk manager, *New disk..* says a new disk is a `.fdd` file and that partition disks are not
    available in the Microsoft Store version, and offers *File on a drive..* and Cancel only - no partition
    choice. *Image to file..* and *Adopt partition..* are absent (hidden, not greyed) from every menu.
