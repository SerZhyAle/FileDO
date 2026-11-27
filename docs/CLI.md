# FileDO commands

> This file is generated from [cli-commands.json](cli-commands.json) by `packaging/write-cli-docs.ps1`. Do not edit it by hand: change the manifest and run the script.

FileDO answers commands from scripts, scheduled tasks, shortcuts and development tools. Type `filedo <command>` in cmd or PowerShell. Command set version **1.0**: 11 user commands (10 on `filedo.exe`, 1 on `filedo_win.exe`), 6 developer tools that `filedo_win.exe` runs in its own process, and 3 non-forwarded console families.

To see what a FileDO on a machine can be asked, run `filedo commands --json`. It prints the manifest as shipped, starts no window, and is the same list as this file.

## How a command is answered

- `filedo.exe` carries the command to the FileDO window program (`filedo_win.exe`: the main window, the Disk Manager and the tray icon) when it is running. The carrying is a local pipe that only your own Windows account, in your own session, can reach - never the network. The running copy does the work and answers; `filedo` prints the answer and exits with its exit code.
- With no window program running, each command's own rule decides, as the table says: start it and ask it, do the work on the spot, or refuse with exit 11.
- Add `--json` to a command that offers it to get the answer as one JSON line with `status`, `exitCode`, `reason`, `message` and, when the command returns something, `data`.
- A command waits for the answer for at most its time bound. If the bound passes, `filedo` exits 2 and the running copy may still be working; it never reports a command done that it did not see end.
- One command is answered at a time. A second caller meets exit 11 (busy) when the first has not finished.
- FileDO's console verbs - `filedo C: info`, `filedo copy`, `filedo vd mount` and the rest of `filedo -?` - are listed under Command families below with their own exit and event contracts.

## Commands

| Command | What it does | Risk | With no window program running | Time bound |
| --- | --- | --- | --- | --- |
| `filedo commands` | Print the list of commands this product accepts. | safe | does the work on the spot | 5 s |
| `filedo version` | Print the product name, its version and the version of its command set. | safe | does the work on the spot | 5 s |
| `filedo status` | Say whether the FileDO window program is running and ready to take commands. | safe | refuses: exit 11, reason `not-running` | 2 s |
| `filedo show` | Bring the main window forward, opening it when the program runs without one. | safe | starts FileDO, then asks it | 10 s |
| `filedo quit` | Close the running window program cleanly; with none running there is nothing to do. A run in progress makes it refuse, unless --stop-jobs stops the runs first. | safe | does the work on the spot | 30 s |
| `filedo disk-manager show` | Bring the Disk Manager window forward, opening it (alone, without the main window) when the program has none. | safe | starts FileDO, then asks it | 10 s |
| `filedo settings show` | Open the Settings page of the main window, on the screen the person is using. | safe | starts FileDO, then asks it | 10 s |
| `filedo tray start` | Start the window program hidden, with its tray icon and no window; with a copy already running nothing changes. | safe | starts FileDO, then asks it | 10 s |
| `filedo job list` | List the runs in progress in the main window and the Disk Manager, and the operations waiting in the Disk Manager's queue. | safe | refuses: exit 11, reason `not-running` | 3 s |
| `filedo job stop` | Ask the runs in progress to stop gracefully, as the Stop button does, empty the Disk Manager's queue and wait until no run of that window is left. | safe | does the work on the spot | 60 s |
| `filedo_win.exe open` | Open a .fdd disk container or .fd-sec secure container in its own FileDO window, or open an already-mounted virtual disk drive in Explorer. | safe | does the work on the spot | no bound |

### `filedo commands`

Print the list of commands this product accepts.

- **Synopsis:** `filedo commands [--developer] [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 5 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--developer` | bool | no | false | Include developer tooling verbs in the readable list. |
| `--json` | bool | no | false | Print the manifest itself as JSON. |

| Exit code | Meaning |
| --- | --- |
| 0 | printed |
| 10 | an unknown option |

Example (powershell):

```powershell
filedo commands --json | ConvertFrom-Json
```

Expect: exit 0; an object with a commands array; no window opens.

Example (cmd):

```bat
filedo commands
```

Expect: exit 0; one line per user command with its summary and the console families.

Example (cmd):

```bat
filedo commands --developer
```

Expect: exit 0; user commands, developer tools and the console families.

### `filedo version`

Print the product name, its version and the version of its command set.

- **Synopsis:** `filedo version [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 5 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the answer as one JSON line. |

Answer (`data`):

| Key | Kind | Meaning |
| --- | --- | --- |
| `product` | string | FileDO |
| `version` | string | the version of this filedo.exe |
| `cliVersion` | string | the version of the command set |

| Exit code | Meaning |
| --- | --- |
| 0 | printed |
| 10 | an unknown option |

Example (cmd):

```bat
filedo version
```

Expect: exit 0; one line with FileDO, its version and its cliVersion.

### `filedo status`

Say whether the FileDO window program is running and ready to take commands.

- **Synopsis:** `filedo status [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** refuses: exit 11, reason `not-running`
- **Waits for:** done, at most 2 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the response line itself. |

Answer (`data`):

| Key | Kind | Meaning |
| --- | --- | --- |
| `version` | string | the running copy's version |
| `pid` | int | its process id |
| `shellOpen` | bool | whether the main window exists |
| `managerOpen` | bool | whether the Disk Manager window is open |
| `trayHeld` | bool | whether the program stays alive for its tray icon with no window shown |
| `runningJobs` | int | runs in progress now, in the main window and the Disk Manager together |
| `acceptsRestrictedCommands` | bool | whether the Settings switch that lets other programs ask FileDO to work with files, the screen, the clipboard or the network is on |

| Exit code | Meaning |
| --- | --- |
| 0 | running and ready |
| 2 | no answer within the bound |
| 10 | an unknown option |
| 11 | not running, or busy |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo status
```

Expect: exit 0 while the window program runs and is ready, 11 otherwise.

Example (powershell):

```powershell
filedo status --json | ConvertFrom-Json
```

Expect: exit 0 and an object whose data holds version, pid and runningJobs, or exit 11 and reason not-running.

### `filedo show`

Bring the main window forward, opening it when the program runs without one.

- **Synopsis:** `filedo show [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** starts FileDO, then asks it
- **Waits for:** done, at most 10 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Kept spellings:** `ui` (no answer, legacy), `gui` (no answer, legacy), `-ui` (no answer, legacy), (no arguments) (no answer, legacy), (no arguments) (filedo_win.exe, no answer, legacy), `--startup` (filedo_win.exe, no answer, legacy), `--legacy-builder` (filedo_win.exe, no answer, legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the response line itself. |

| Exit code | Meaning |
| --- | --- |
| 0 | the main window is forward |
| 2 | no answer within the bound |
| 10 | an unknown option |
| 11 | busy, or filedo_win.exe is not beside filedo.exe |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo show
```

Expect: exit 0; one main window, the same program that was running.

### `filedo quit`

Close the running window program cleanly; with none running there is nothing to do. A run in progress makes it refuse, unless --stop-jobs stops the runs first.

- **Synopsis:** `filedo quit [--stop-jobs] [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 30 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--stop-jobs` | bool | no | false | Stop the runs in progress gracefully, as the Stop button does, wait for them to end and then close. Without it a run in progress makes quit refuse, because the window's own Exit would ask the person. |
| `--json` | bool | no | false | Print the response line itself. |

| Exit code | Meaning |
| --- | --- |
| 0 | closed, or was not running |
| 1 | a window did not close |
| 2 | no answer within the bound, or with --stop-jobs a run did not end within it; the program is left open and the runs keep the stop that was asked |
| 10 | an unknown option |
| 11 | a run is in progress and --stop-jobs was not given, or the window program is busy |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo quit
```

Expect: exit 0 (errorlevel 0) when nothing runs.

Example (powershell):

```powershell
filedo quit; $LASTEXITCODE
```

Expect: 0 once the program closed, 11 while a run is in progress.

Example (powershell):

```powershell
filedo quit --stop-jobs; $LASTEXITCODE
```

Expect: 0 after the runs stopped and the program closed.

### `filedo disk-manager show`

Bring the Disk Manager window forward, opening it (alone, without the main window) when the program has none.

- **Synopsis:** `filedo disk-manager show [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** starts FileDO, then asks it
- **Waits for:** done, at most 10 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Kept spellings:** `dm` (no answer, legacy), `-dm` (no answer, legacy), `--disks` (filedo_win.exe, no answer, legacy), `--startup --disks` (filedo_win.exe, no answer, legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the response line itself. |

| Exit code | Meaning |
| --- | --- |
| 0 | the Disk Manager is forward |
| 2 | no answer within the bound |
| 10 | an unknown option |
| 11 | busy, or filedo_win.exe is not beside filedo.exe |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo disk-manager show
```

Expect: exit 0; the Disk Manager is in front; with no program running it is the only window that opens.

Example (powershell):

```powershell
filedo disk-manager show; $LASTEXITCODE
```

Expect: 0 once the Disk Manager is forward.

### `filedo settings show`

Open the Settings page of the main window, on the screen the person is using.

- **Synopsis:** `filedo settings show [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** starts FileDO, then asks it
- **Waits for:** done, at most 10 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the response line itself. |

| Exit code | Meaning |
| --- | --- |
| 0 | the Settings page is forward |
| 2 | no answer within the bound |
| 10 | an unknown option |
| 11 | busy, or filedo_win.exe is not beside filedo.exe |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo settings show
```

Expect: exit 0; the main window shows Settings, also when the program runs only as a tray icon.

Example (powershell):

```powershell
filedo settings show; $LASTEXITCODE
```

Expect: 0 once the Settings page is forward.

### `filedo tray start`

Start the window program hidden, with its tray icon and no window; with a copy already running nothing changes.

- **Synopsis:** `filedo tray start [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** starts FileDO, then asks it
- **Waits for:** done, at most 10 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Kept spellings:** `--startup --tray` (filedo_win.exe, no answer, legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the response line itself. |

| Exit code | Meaning |
| --- | --- |
| 0 | the program runs with its tray icon; a copy that was already running is left as it is |
| 2 | no answer within the bound |
| 10 | an unknown option |
| 11 | busy, or filedo_win.exe is not beside filedo.exe |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo tray start
```

Expect: exit 0; no window opens; the tray icon is the way back.

Example (powershell):

```powershell
filedo tray start; $LASTEXITCODE
```

Expect: 0 with the tray icon present and no window.

### `filedo job list`

List the runs in progress in the main window and the Disk Manager, and the operations waiting in the Disk Manager's queue.

- **Synopsis:** `filedo job list [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** refuses: exit 11, reason `not-running`
- **Waits for:** done, at most 3 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--json` | bool | no | false | Print the response line itself. |

Answer (`data`):

| Key | Kind | Meaning |
| --- | --- | --- |
| `jobs` | array | one object per run: where (shell or manager), verb (the console verb of the run), state (running or queued), step (the name of the run's current step, empty when it has not named one) and percent (a whole number, null when the run has not said how much there is to do); no path and no name of the person's files; an empty array when nothing runs |

| Exit code | Meaning |
| --- | --- |
| 0 | listed (the array may be empty) |
| 2 | no answer within the bound |
| 10 | an unknown option |
| 11 | not running, or busy |
| 12 | the running copy is elevated and this caller is not |

Example (powershell):

```powershell
(filedo job list --json | ConvertFrom-Json).data.jobs.Count
```

Expect: the number of runs and queued operations; exit 11 when the window program is not running.

Example (cmd):

```bat
filedo job list
```

Expect: exit 0; one line per run with where, verb, state, step and percent, or a sentence saying nothing runs.

### `filedo job stop`

Ask the runs in progress to stop gracefully, as the Stop button does, empty the Disk Manager's queue and wait until no run of that window is left.

- **Synopsis:** `filedo job stop [--where <all|shell|manager>] [--json]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 60 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--where` | enum: all, shell, manager | no | all | Which window's runs to stop and wait for: shell is the main window (the Job page and the Command page), manager is the Disk Manager. |
| `--json` | bool | no | false | Print the response line itself. |

Answer (`data`):

| Key | Kind | Meaning |
| --- | --- | --- |
| `stopped` | int | runs that were asked to stop; 0 when nothing ran |
| `cleared` | int | queued operations taken out of the Disk Manager's queue; 0 when the queue was empty |

| Exit code | Meaning |
| --- | --- |
| 0 | no run of that window is left, or none was running |
| 2 | a run did not end within the bound; the stop it was asked for stays requested and no run is ended by force |
| 10 | an unknown option or value |
| 11 | the window program is busy |
| 12 | the running copy is elevated and this caller is not |

Example (cmd):

```bat
filedo job stop
```

Expect: exit 0 once no run is left.

Example (powershell):

```powershell
(filedo job stop --where manager --json | ConvertFrom-Json).data.stopped
```

Expect: the number of runs of the Disk Manager that were asked to stop.

### `filedo_win.exe open`

Open a .fdd disk container or .fd-sec secure container in its own FileDO window, or open an already-mounted virtual disk drive in Explorer.

- **Synopsis:** `filedo_win.exe open --path <file> [--mode <mount|mount-ro|unmount>]`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** does the work on the spot
- **Waits for:** done, no bound of its own
- **Repeat after exit 2:** not safe, a second run may repeat the work
- **Default verb for:** `filedo_win.exe` (or just a path)
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Run by:** `filedo_win.exe` itself, in its own process (one window per target)
- **Kept spellings:** `{path}` (no answer, legacy), `--mount {path}` (implies `--mode=mount`, no answer, legacy), `--mount-ro {path}` (implies `--mode=mount-ro`, no answer, legacy), `--unmount {path}` (implies `--mode=unmount`, no answer, legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--path` | path | yes |  | The .fdd disk container, .fd-sec secure container, or file to open in its own window. |
| `--mode` | enum: mount, mount-ro, unmount | no | mount | For a .fdd virtual disk container: mount normally (or open its drive if already mounted), mount read-only, or unmount. |

| Exit code | Meaning |
| --- | --- |
| 0 | the container window opened and closed, or the already-mounted drive opened in Explorer |
| 10 | missing --path, an unknown option or mode, or an extra argument |
| 11 | the named file does not exist |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'open --path C:\v\work.fdd --mode mount-ro' -Wait -PassThru).ExitCode
```

Expect: 0 after the read-only mount window closes (or Explorer opens if already mounted); 11 when the file does not exist.

Example (cmd):

```bat
filedo_win.exe open --path C:\a\secret.fd-sec
```

Expect: opens its own window on the Reveal page without handing over to a running FileDO.

## Developer tools

Hidden from plain `filedo commands`; pass `filedo commands --developer` or `filedo commands --json` to list them.

| Command | What it does | Risk | With no window program running | Time bound |
| --- | --- | --- | --- | --- |
| `filedo_win.exe selftest run` | Developer tooling: run the window program's own gate and exit with its verdict, showing no window of its own. | safe | does the work on the spot | 600 s |
| `filedo_win.exe help-pages build` | Developer tooling: write the in-app help pages and their pictures, in every language and both themes, into a folder. | writes-user-data | does the work on the spot | 300 s |
| `filedo_win.exe screens capture` | Developer tooling: render the guide screenshots from the real windows into a folder. | writes-user-data | does the work on the spot | 300 s |
| `filedo_win.exe menu-icons write` | Developer tooling: write the Explorer menu icons into a folder and print each file written. | writes-user-data | does the work on the spot | 60 s |
| `filedo_win.exe ui-drive run` | Developer tooling: drive the real windows on the screen for the visual acceptance run and write its evidence into a folder; the person's own settings are restored afterwards. | writes-user-data | does the work on the spot | 600 s |
| `filedo_win.exe ui-showcase capture` | Developer tooling: render the showcase of the shared buttons, options, groups, navigation rows and a dialog at several sizes, palettes, languages and widths into a folder, with a geometry and contrast table. | writes-user-data | does the work on the spot | 300 s |

### `filedo_win.exe selftest run`

Developer tooling: run the window program's own gate and exit with its verdict, showing no window of its own.

- **Synopsis:** `filedo_win.exe selftest run`
- **Risk:** safe - changes only FileDO's own windows and runs and reads nothing of yours
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 600 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)
- **Run by:** `filedo_win.exe` itself, in its own process and never through the running window; developer tooling
- **Kept spellings:** `--selftest` (legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Exit code | Meaning |
| --- | --- |
| 0 | every row passed |
| 1 | a row failed, or the gate crashed |
| 10 | an unknown option |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'selftest run' -Wait -PassThru).ExitCode
```

Expect: 0 when the gate passes (about 61 to 74 s).

### `filedo_win.exe help-pages build`

Developer tooling: write the in-app help pages and their pictures, in every language and both themes, into a folder.

- **Synopsis:** `filedo_win.exe help-pages build --out <folder>`
- **Risk:** writes-user-data - creates, changes or deletes your files or settings outside FileDO's own data folder, or acts inside another program
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 300 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)
- **Run by:** `filedo_win.exe` itself, in its own process and never through the running window; developer tooling
- **Kept spellings:** `--build-help {out}` (legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--out` | path | yes |  | The existing folder to write. The frozen spelling --build-help takes it as the next argument. |

| Exit code | Meaning |
| --- | --- |
| 0 | every file was written |
| 1 | a file could not be written |
| 10 | no folder given, or an unknown option |
| 11 | the named folder does not exist |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'help-pages build --out docs\help' -Wait -PassThru).ExitCode
```

Expect: 0 and the pages under docs\help.

### `filedo_win.exe screens capture`

Developer tooling: render the guide screenshots from the real windows into a folder.

- **Synopsis:** `filedo_win.exe screens capture --out <folder>`
- **Risk:** writes-user-data - creates, changes or deletes your files or settings outside FileDO's own data folder, or acts inside another program
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 300 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)
- **Run by:** `filedo_win.exe` itself, in its own process and never through the running window; developer tooling
- **Kept spellings:** `--capture-screens {out}` (legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--out` | path | yes |  | The existing folder to write. The frozen spelling --capture-screens takes it as the next argument. |

| Exit code | Meaning |
| --- | --- |
| 0 | every file was written |
| 1 | a file could not be written |
| 10 | no folder given, or an unknown option |
| 11 | the named folder does not exist |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'screens capture --out temp\shots' -Wait -PassThru).ExitCode
```

Expect: 0 and the pictures under temp\shots.

### `filedo_win.exe menu-icons write`

Developer tooling: write the Explorer menu icons into a folder and print each file written.

- **Synopsis:** `filedo_win.exe menu-icons write --out <folder>`
- **Risk:** writes-user-data - creates, changes or deletes your files or settings outside FileDO's own data folder, or acts inside another program
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 60 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)
- **Run by:** `filedo_win.exe` itself, in its own process and never through the running window; developer tooling
- **Kept spellings:** `--write-menu-icons {out}` (legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--out` | path | yes |  | The existing folder to write. The frozen spelling --write-menu-icons takes it as the next argument. |

| Exit code | Meaning |
| --- | --- |
| 0 | every file was written |
| 1 | a file could not be written, or the glyph catalog has a problem |
| 10 | no folder given, or an unknown option |
| 11 | the named folder does not exist |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'menu-icons write --out assets\menu-icons' -Wait -PassThru).ExitCode
```

Expect: 0 and the .ico files under assets\menu-icons.

### `filedo_win.exe ui-drive run`

Developer tooling: drive the real windows on the screen for the visual acceptance run and write its evidence into a folder; the person's own settings are restored afterwards.

- **Synopsis:** `filedo_win.exe ui-drive run --out <folder>`
- **Risk:** writes-user-data - creates, changes or deletes your files or settings outside FileDO's own data folder, or acts inside another program
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 600 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)
- **Run by:** `filedo_win.exe` itself, in its own process and never through the running window; developer tooling
- **Kept spellings:** `--ui-drive {out}` (legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--out` | path | yes |  | The existing folder to write. The frozen spelling --ui-drive takes it as the next argument. |

| Exit code | Meaning |
| --- | --- |
| 0 | the run passed |
| 1 | the run failed |
| 10 | no folder given, or an unknown option |
| 11 | the named folder does not exist |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'ui-drive run --out temp\evidence\run1' -Wait -PassThru).ExitCode
```

Expect: 0 when the driven run passes; it uses the screen while it runs.

### `filedo_win.exe ui-showcase capture`

Developer tooling: render the showcase of the shared buttons, options, groups, navigation rows and a dialog at several sizes, palettes, languages and widths into a folder, with a geometry and contrast table.

- **Synopsis:** `filedo_win.exe ui-showcase capture --out <folder>`
- **Risk:** writes-user-data - creates, changes or deletes your files or settings outside FileDO's own data folder, or acts inside another program
- **With no window program running:** does the work on the spot
- **Waits for:** done, at most 300 s
- **Repeat after exit 2:** safe, a second run leaves the same state as one
- **Process:** runs in the receiving process (`forward: never`), never through the running window
- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)
- **Run by:** `filedo_win.exe` itself, in its own process and never through the running window; developer tooling
- **Kept spellings:** `--capture-ui-showcase {out}` (legacy) - older forms that still start the same thing
- **Since:** command set 1.0

| Option | Kind | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--out` | path | yes |  | The existing folder to write. The frozen spelling --capture-ui-showcase takes it as the next argument. |

| Exit code | Meaning |
| --- | --- |
| 0 | every file was written and every measurement passed |
| 1 | a file could not be written or a measurement failed |
| 10 | no folder given, or an unknown option |
| 11 | the named folder does not exist |

Example (powershell):

```powershell
(Start-Process .\filedo_win.exe -ArgumentList 'ui-showcase capture --out temp\evidence\showcase' -Wait -PassThru).ExitCode
```

Expect: 0, the pictures, geometry.csv and summary.txt under temp\evidence\showcase.

## Command families

These grammars are not forwarded through the running window program; each runs in `filedo.exe` and keeps its own usage and exit contract.

### `target-first`

Target-first device, folder, file, share and batch operations (info, speed, test, fill, clean, cd, copy, compare, check, wipe, backup, restore, rehash, from, hist).

- **Executable:** `filedo.exe`
- **Usage:** `filedo help`
- **Exit contract:** CLI-EVENT-STREAM
- **Event contract:** CLI-EVENT-STREAM

### `fdsec`

Password-protected .fd-sec secure containers (secure, unsecure, reveal).

- **Executable:** `filedo.exe`
- **Usage:** `filedo help`
- **Exit contract:** FDSEC-BEHAVIOUR
- **Event contract:** CLI-EVENT-STREAM

### `vd`

Virtual disks (.fdd containers and FileDO GPT partitions, 31 verbs).

- **Executable:** `filedo.exe`
- **Usage:** `filedo help`
- **Exit contract:** FDD-BEHAVIOUR
- **Event contract:** CLI-EVENT-STREAM

## Exit codes

The codes are the portfolio vocabulary; each command lists the subset it can return.

| Code | Meaning here |
| --- | --- |
| 0 | done |
| 1 | the command ran and the operation failed |
| 2 | not proven: the time bound passed with no answer, or the answer was not understood |
| 10 | usage: an unknown command, option or value, a missing required option, or a request that is too large |
| 11 | not ready: no window program is running, it is busy (a dialog is open, it is closing, or another command is being answered), or the named target or folder does not exist |
| 12 | permission: the command's risk class is switched off in Settings, or the running FileDO is elevated and this caller is not |

## Risk classes and the one setting

Every command declares one risk class, and the class decides who may ask.

| Risk | What a command of this class does | Commands in this set |
| --- | --- | --- |
| safe | changes only FileDO's own windows and runs and reads nothing of yours | `filedo commands`, `filedo version`, `filedo status`, `filedo show`, `filedo quit`, `filedo disk-manager show`, `filedo settings show`, `filedo tray start`, `filedo job list`, `filedo job stop`, `filedo_win.exe open`, `filedo_win.exe selftest run` |
| reads-user-data | puts something of yours - screen content, the clipboard, file contents or file names - into its answer or into a file the caller names | none |
| writes-user-data | creates, changes or deletes your files or settings outside FileDO's own data folder, or acts inside another program | `filedo_win.exe help-pages build`, `filedo_win.exe screens capture`, `filedo_win.exe menu-icons write`, `filedo_win.exe ui-drive run`, `filedo_win.exe ui-showcase capture` |
| leaves-machine | sends anything off this computer | none |

A `safe` command is always accepted. A command of any other class that goes through the running window program is refused with exit 12 until the person turns on one setting: **Let other programs ask FileDO to work with my files, screen, clipboard or the network**, in Settings under "Windows and window positions". The setting is off until the person turns it on, and no command can change it. `filedo status` reports whether it is on.

