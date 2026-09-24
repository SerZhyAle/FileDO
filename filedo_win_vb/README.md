# FileDO GUI (filedo_win.exe)

A VB.NET (Windows Forms, .NET Framework 4.8) window for `filedo.exe`: the **shell** - a rail of jobs on
the left and one numbered page per job, described under "The shell's pages" below. The command builder
that shipped before the shell has been retired; its successor is the shell's **Command** page, where any
FileDO command can be assembled or typed by hand. `--legacy-builder` is still accepted on the command line
and simply opens the shell.

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

1. The FileDO artifacts are looked for first - next to the exe, in `%LOCALAPPDATA%\FileDO`, in
   `%USERPROFILE%`, in `%TEMP%\FileDO_Operations` and in `%TEMP%`: `filedo_win.log`,
   `filedo_win_debug.log`, `history.json`, `check_report_*`, `check_state.json`, `compare_report_*.log`,
   `delete_report_*.log`, `skip_files.list`, `damaged_files.log`. When there are none, the window says so
   and asks nothing.
2. Otherwise a dialog states what will be collected, that paths inside the logs can contain your own
   folder and account names, and that nothing is sent automatically. **Build the zip** or **Cancel**.
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
- **Compare two folders** - all eleven delete rules, each explained in the chosen language
- **Fill the free space** - the file size, `del`, and `fill verify`
- **Copy files** - the strategy: `copy`, `fastcopy`, `synccopy`, `balanced`, `maxcopy`,
  `smartcopy` or `safecopy`
- **Wipe a folder** - the typed `WIPE` and `-y` both, because the CLI asks again on a console a window
  run does not have; an empty folder is said to be empty, and a drive root, a share root, a junction or
  TEMP is sent to the console, where FileDO asks twice
- **Protect** - see below
- Every job except the three secret-file ones also offers `nohist`

The **Command** page is the expert builder: all twenty-eight operations `filedo.exe` takes -
including `probe`, `recover`, the six named copy strategies, `secure`, `unsecure`, `reveal`, the
two `fdsec` inspections, `from`, `hist` and the two Explorer registrations - with the flags and
rule pickers that belong to whichever one is chosen, and the command line itself editable
underneath. A line that holds `wipe` - with `-y` or without - runs only once `WIPE` is typed beside it,
and progress is drawn by the same rule as on a job page.

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

## Usage

```powershell
filedo_win                      # if on PATH (winget / Store)
filedo_win.exe                  # next to filedo.exe in the portable zip
filedo_win.exe C:\a\secret.fd-sec   # opens on that container's page
filedo_win.exe -debug           # adds diagnostic lines to %LOCALAPPDATA%\FileDO\filedo_win.log
```
