# FileDO GUI (filedo_win.exe)

A VB.NET (Windows Forms, .NET Framework 4.8) window for `filedo.exe`: the **shell** - a rail of jobs on
the left and one numbered page per job, described under "The shell's pages" below. The command builder
that shipped before the shell has been retired; its successor is the shell's **Command** page, where any
FileDO command can be assembled or typed by hand. `--legacy-builder` is still accepted on the command line
and simply opens the shell. A second window, the **Disk manager**, lists the `.fdd` virtual disks and starts
with `--disks`; it is the same program, described under "The Disk manager (a second window)" below.

## When something fails

A failure is shown as what happened and what can be done about it - try again, open the folder, copy the
address or the path, **Send logs** - never as the exception's own text. That text goes to the window's own
log, `%LOCALAPPDATA%\FileDO\filedo_win.log`, which Send logs packs. `-debug` adds diagnostic lines to the
same file. Every question the window asks has a no-action answer that Escape and the close box give.

## A running job

While a job runs, clicking another job in the rail keeps the running page in front and says why; History,
Settings, About and Command stay reachable. A result that arrives while another view is in front waits on
the job's page. Closing the window while a job runs asks **Stop and close** (the job is asked to stop, its
report is written, then the window closes) or **Keep running** - which is also what Escape gives.

## About page and sending logs

**About** holds the GUI build stamp, the `filedo.exe` version beside it, the author, and links to the site,
the source, the issue tracker, the privacy page and the author's other tools.

Its one action is **Send logs to the author**:

1. The FileDO artifacts are looked for first - next to the exe and in FileDO's own state folder
   `%LOCALAPPDATA%\FileDO\state\` (the window's log, `filedo_win.log` and its previous generation, in
   `%LOCALAPPDATA%\FileDO`), never in the profile root or `%TEMP%`, where another program's file of the
   same name may sit: `filedo_win_debug.log`, `history.json`, `check_report_*`, `check_damaged.list`,
   `compare_report_*.log`, `delete_report_*.log`, `skip_files.list`, `damaged_files.log`. When there are
   none, the window says so and asks nothing.
2. Otherwise a dialog states what will be collected, that paths inside the logs can contain your own
   folder and account names and the names of the files you protected (history.json says which file
   became which container), and that nothing is sent automatically. **Build the zip** or **Cancel**.
   Archives older than seven days are removed the next time.
3. The files are packed newest-first into `%TEMP%\FileDO_Logs\filedo-logs-<stamp>.zip`, together with a
   generated `filedo-report.txt` (build stamps, OS, culture, and a manifest of what went in and what was
   left out under the 40-file / 8 MB-per-file / 20 MB-total caps).
4. Explorer opens with the zip selected, its path goes to the clipboard, and the default mail program
   opens addressed to the author with an English subject and body template. `mailto:` cannot carry an
   attachment, so attaching the zip is the one manual step - the closing dialog says so and shows the path.

## How it finds filedo.exe

`filedo_win.exe` runs `filedo.exe` from the same folder if present, otherwise from `PATH` (the winget
portable alias or the Microsoft Store appExecutionAlias). So it works the same whether launched from the
portable zip, the installed location, or the Store tile.

## Build

```powershell
msbuild FileDOGUI.vbproj /p:Configuration=Release /p:Platform=AnyCPU
filedo_win.exe --selftest    # the shell's own gate; writes filedo_win_selftest.log beside the exe
```

Output: `bin\Release\filedo_win.exe`. The repo's `build.ps1` and the release CI build this automatically
and ship it in the winget package, the portable zip, the MSI, and the Microsoft Store (MSIX) package, where
it is the clickable tile.

## The shell's pages

The window is a rail of jobs on the left
and one numbered page per job - what to work on, which parameters, then check and run. Every
option the CLI takes for a job is on its page:

- **Capacity test** - Quick (100 files), Thorough (1000) or a file count of your own; `del`
- **Speed test** - Quick (100 MB), Thorough (`max`) or a size of your own; `nodel`, `short`, `del`
- **Target info** - `short` for the brief report
- **Damaged files check** - all twenty-nine `check` flags, grouped by what they decide: which
  files are read, how they are read, how hard the drive is pushed, and the run's own report
- **Raw capacity probe** and **Recover a drive** - `yes`, and `fix` / `format`. Both are marked
  destructive, both ask for Administrator
- **Duplicate files** - the rule (`old`, `new`, `abc`, `xyz`), what happens to the rest (report,
  `del`, `move <folder>`), `list <file>` and `quiet`
- **Compare two folders** - all eleven delete rules, each explained in the chosen language; a rule
  other than "none" makes the page destructive and permanent and asks for the typed `DELETE`, then
  passes `--yes`, because the CLI asks before its delete phase on a console a window run does not have
- **Fill the free space** - the file size, `del`, and `fill verify`
- **Copy files** - the strategy: `copy`, `fastcopy`, `synccopy`, `balanced`, `maxcopy`,
  `smartcopy` or `safecopy`
- **Wipe a folder** - the typed `WIPE` and `-y` both, because the CLI asks again on a console a window
  run does not have; an empty folder is said to be empty, and a drive root, a share root, a junction or
  TEMP is sent to the console, where FileDO asks twice
- **Protect** - see below
- **Disks** - the `.fdd` virtual disks, seventeen pages, and the row that opens the Disk manager; see below
- Every job except the three secret-file ones also offers `nohist`

The **Command** page is the expert builder: all twenty-eight operations `filedo.exe` takes -
including `probe`, `recover`, the six named copy strategies, `secure`, `unsecure`, `reveal`, the
two `fdsec` inspections, `from`, `hist` and the two Explorer registrations - with the flags and
rule pickers that belong to whichever one is chosen, and the command line itself editable
underneath. A line that holds `wipe` - with `-y` or without - runs only once `WIPE` is typed beside it,
and progress is drawn by the same rule as on a job page.

**Create shortcut** beside Copy command saves the edited line as a `.lnk` on your desktop. Give it a
name and choose whether the console stays open after the run (on by default, with `--pause`). A
double-click then runs `filedo.exe` in a console, where you can answer prompts and read the verdict.
The shortcut stores its arguments, so the window refuses a line containing `p:<password>` or
`pe:FILEDO_SHELL_CRED`; for a credential-taking command, remove that argument and let the console
ask for the password. An existing desktop shortcut is never overwritten. Uninstalling FileDO does
not remove shortcuts you created.

The five verbs that need a password get a masked field of their own on that page, `secure` asking
twice. The password never enters the command line: what the line carries is `pe:FILEDO_SHELL_CRED`,
and the value goes to the child process in that variable, so the line can be copied, logged and
read over a shoulder without giving anything away. A `secure` that deletes or overwrites the
original also needs `-y` from here - that confirmation is asked on a console, and a run started in
a window has none; without it the original is kept and the run says so.

## Secret files (the Protect pages)

The window carries the three `.fd-sec` operations as pages of their own - make a file secret, get the
original back, and open a secret file without unpacking it. Four things about them are worth knowing
before reading the code:

- The password is **masked**, typed **twice** when packing, and never put on a command line: it travels in
  an environment variable set on the child process alone (`pe:FILEDO_SHELL_CRED`), so the process list,
  the run report and `history.json` see the variable's name and nothing else.
- The line under the password box changes while it is typed and says what the credential is worth. An
  **empty password is accepted** and means obfuscation only, with no secrecy.
- What happens to the original is chosen **before** anything runs - keep (the default), delete, or
  overwrite-then-delete. Only the last one asks for `WIPE` to be typed out, and the page's badges and
  accent follow the answer rather than the page.
- Opening a container starts a run that lasts exactly as long as the plaintext copy exists, and the run
  strip's button reads **Remove the copy now**. That button is the whole of the reveal's second lifetime
  mechanism here: there is no console to press Enter in, so the shell writes the stop file `filedo.exe`
  is already watching.

## Virtual disks (the Disks pages)

The **Disks** group carries the `.fdd` containers of `filedo vd` as seventeen pages: Disks and mounts (a
table of what `vd list` and `vd status` report, with Open the drive, Unmount and Turn off auto-mount on each
row), Create a disk, Mount, Unmount, Disk info, Verify a disk, Export an image, Save now (ram), Compact,
Grow, Format, Seal a copy, Clone, Change password, Destroy, Auto-mount and Remember a name. Each page builds
the console line of the verb it is named after (`DiskJobs.vb`); the window never reads a container itself -
what a page says about one is what `filedo.exe <x.fdd> info` printed. Things worth knowing before reading
the code:

- **Obfuscated is never called encrypted.** An empty password on Create, and Clone or Seal without a
  password, say in the page's own words that the result is obfuscated, not encrypted - anyone with the file
  and FileDO reads it. Change password refuses an empty new one and points at Clone without a password,
  because a password is never taken off in place.
- **The password never reaches a command line.** It travels as `pe:FILEDO_SHELL_CRED`, a new one for
  Change password as `new pe:FILEDO_SHELL_CRED_NEW`, set on the child process alone.
- **Elevation is `filedo.exe`'s, not the window's.** Mount, Unmount, Save now, Format and Auto-mount carry
  the Administrator badge: `filedo.exe` asks Windows for consent for the iSCSI initiator step only, and that
  step never carries the password. The window itself never starts elevated.
- **A mount outlives the window.** The block server is started outside the window's Job Object, so closing
  the window leaves the drive attached, and a new window's Disks and mounts page lists it again.
- **What cannot run is said, not offered.** A mounted container's Compact, Grow, Change password, Format and
  Destroy pages say it is mounted (the console would refuse it with exit 8); in the Microsoft Store build the verbs that
  need the transport say that build cannot attach disks, while Info, Verify and Export work.
- **Double-click.** `filedo_win.exe "<x.fdd>"` mounts, `--mount-ro "<x.fdd>"` mounts read-only and
  `--unmount "<x.fdd>"` unmounts - the three Explorer entries of the `.fdd` type. A clean obfuscated
  container mounts at once, an encrypted one waits for its password, one not closed cleanly is reported
  first and waits, and one already mounted opens its drive with no window at all.

## The Disk manager (a second window)

`filedo_win.exe` has a second window, the **Disk manager** (SP-0063): one row per virtual disk - the containers in
the user's list, the ones mounted now, and the VHD, VHDX or ISO images FileDO mounted - each with its state in
words and a glyph, kept true without a manual refresh. It is the same program and the same process as the shell,
not a new executable (`filedo_win.exe` is a frozen anchor): `AppHost.vb` owns the windows of the process and
ends it when the last one has closed. Ways in: `filedo_win.exe --disks` starts straight into the manager with
no shell window (the Start menu's **FileDO Disk Manager** entry, which the MSI installs, runs exactly that); the
**Disk manager** button in the shell's header, the first row of the rail's Disks group and **Ctrl+Shift+D** open
it from the shell; and the manager's **Main window** button brings the shell forward, opening it if the manager
was started alone.

Things worth knowing before reading the code:

- **State comes from one place.** The window learns state from `filedo vd status json` - schema
  `filedo.vd-status`, version 1, the one machine-readable snapshot - and from nothing else: it never opens a
  container, `vd-registry.json` or `vdisk-state.json` itself. A read is one child process, coalesced and killed
  after eight seconds; a read that fails keeps the last good state on screen and says so.
- **Everyday operations run from the row, the rest are delegated.** Mount, unmount, open, save, info, verify,
  auto-mount and the list's names build their `filedo.exe` line through `DiskCommands.Build` and run like a job.
  New, export, compact, grow, seal, clone, change password, format and destroy open their existing job page in the
  shell with the container chosen, so the page that builds a destructive line stays the only one - and Format and
  Destroy keep their typed confirmation there.
- **A disabled action says why** - in its tooltip, in the menu and in the detail pane - and a build that can never
  mount (the Microsoft Store one) hides the mount controls instead of greying them.
- **Closing never unmounts.** A mount outlives the manager, as it outlives the shell.
- **The keyboard is data**: `DiskShortcuts.All` in `DiskHelp.vb`. The key handler, every tooltip and menu item
  that shows a shortcut and the help window's table all read it, and `--selftest` walks it.
- **The first run** shows the welcome window once (`DiskWelcomeDialog`); Help, First steps.. opens it again. Neither
  the help nor the welcome makes a network call: a link is followed only when it is clicked.
- **Autostart, in one dialog** (`DiskAutostartDialog.vb`, SP-0080): the per-container logon mounts and the shutdown
  guard together, opened by *Autostart..* under More actions - with or without a selection, since the guard
  belongs to the account and not to a disk - or from a registered disk's menu or detail pane. The dialog learns state
  only through delegates over the snapshot the manager has already read, and acts only through the manager's own
  flows - a logon switch is exactly the row's `vd auto` action, and the guard's switch is one
  `DiskCommands.Build("guard", ..)` line through the same queue. The packaged build never opens it: mount and
  unmount are refused there, so the entry is hidden, not greyed.

Where the code lives: `DiskManager.vb` (the window), `DiskHelp.vb` (the keyboard table, the help and the welcome
windows), `GlyphButton.vb` (the glyph buttons), `DiskGlyphs.vb` (one glyph per meaning), `DiskStates.vb` (the state
of a row and which action applies, as pure functions), `DiskSnapshot.vb` (the `vd status json` reader),
`DiskAutostartDialog.vb` (the logon mounts and the shutdown guard in one place, with the guard's words as pure
functions), `DiskDialogs.vb` (the password, Mount as.. and the name a container is added under) and `AppHost.vb`
(the windows of the process). `filedo_win.exe --capture-screens <folder>` renders the site's three pictures of it
(`gui-disk-manager-*`, `gui-disk-help-*`, `gui-disk-welcome-*`).

## Application settings and the tray

The shell's Settings page and the Disk Manager's Settings toolbar button edit the same values through
`SettingsPanel.vb`. Theme changes apply to both windows immediately; language changes apply on reopening.
The manager opens an owned settings dialog with a Close button. Values apply immediately.

Run at Windows startup offers Off, Open FileDO, Open Disk Manager and Tray icon only. It writes only this
user's `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` entry named `FileDO`, without elevation.
Turning it off removes that entry. Uninstalling leaves it behind; turn it off before uninstalling.
The Store build instead declares a disabled `FileDOShellStartup` task: enable or disable it in Windows
Settings > Apps > Startup or Task Manager. The in-app choice is hidden there; enabling the OS task opens
the shell. Starting by hand always opens a window.

Minimize to the tray applies to either window's minimize button. Close still closes and asks while a job
runs. The shared icon appears for a hidden startup or a window minimized to the tray; its tooltip reports
running jobs and mounted disks. Left-click opens FileDO. The menu opens either window or settings, offers
Unmount all when disks are mounted (outside the Store build), and Stop while a job runs, through the same
paths as the windows. Exit uses their existing running-job questions and waits for cleanup. A tray-held
process stays available after its windows close until Exit is chosen. Finish balloons can be disabled.
Reset window positions restores default geometry on each window's next opening.

## Usage

```powershell
filedo_win                      # if on PATH (winget / Store)
filedo_win.exe                  # next to filedo.exe in the portable zip
filedo_win.exe C:\a\secret.fd-sec   # opens on that container's page
filedo_win.exe C:\v\work.fdd    # mounts that virtual disk (--mount-ro, --unmount)
filedo_win.exe --disks          # opens the Disk manager alone, with no shell window
filedo_win.exe -debug           # adds diagnostic lines to %LOCALAPPDATA%\FileDO\filedo_win.log
```
