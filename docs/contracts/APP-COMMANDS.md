# APP-COMMANDS

| | |
| --- | --- |
| **Id** | `APP-COMMANDS` |
| **Version** | 0.3 read and synced in working tree (`SP-0216`..`SP-0220`, `SP-0223`, `SP-0228`; not yet built or tested; C9 manual open) |
| **Role** | consumer: `filedo.exe` is the second process (rule 9 companion), the running `filedo_win.exe` is the first instance |
| **Home** | shared contracts catalog, folder `app-commands/`, document `README.md` |
| **Owner** | shared |

## What this repository must do to stay conformant

- **One manifest.** `docs/cli-commands.json` is the source of truth (rule 18). `filedo.exe` embeds a byte copy
  (`cmd/filedo/cli-commands.json`), `filedo_win.exe` embeds the file itself, and `commands --json` prints the
  shipped copy with no first instance and no window. `families[]` lists FileDO's non-forwarded console grammars
  (`target-first`, `fdsec`, `vd`; item B); `commands` hides `audience: developer` verbs unless `--developer` is passed
  (item D). A test holds the copies equal (case C1).
- **The transport.** A named pipe of the third shape of `APP-ACTIVATION` rule 3,
  `FileDO_Commands_<userSid>_<sessionId>`: one request line and one response line on the same connection
  (rule 7), the current user only and never the network (rule 15). Only the first copy of the session
  listens. The pipe name is a frozen anchor once shipped. The payload-less window messages
  `FileDO.Shell.ShowShell` and `FileDO.Shell.ShowDiskManager` stay as they are for the focus-only start.
- **The grammar is the first instance's.** `filedo.exe` decides its role before it loads anything, forwards
  the canonical verb and the raw arguments, and parses them itself only for `commands`, `version` and a cold
  start - with the same manifest-driven reading `AppCommands.vb` uses. The manifest's verbs are reserved first
  words of `filedo.exe` and win over a path probe; every other first word keeps the target-first grammar,
  and the console verbs keep their own exit classes (rule 11).
- **The frozen aliases keep their start.** `ui`, `gui`, `-ui`, the empty argument list, `--startup` and
  `--legacy-builder` are listed as alias objects (`exe`, `legacy: true`, `answers: false`; item E) of `show` and keep
  the start they always made (rule 2, case C13). `legacy: true` on a pipe request lifts rule 13 only for a verb whose
  manifest entry lists a `legacy` alias (item F, case C14).
- **Bounds and answers.** Delivery inside 1 s; completion inside the verb's `timeoutMs`, after which the second
  process exits 2 with reason `timeout`; a request above 64 KiB or an argument above 32 KiB is refused with 10;
  busy (a modal window, the program closing, a window thread that does not take the command within 800 ms) is
  11 inside the delivery bound; one command at a time. A JSON request missing `requestId` is refused `bad-argument`
  (10) with `requestId` `""`, and an empty or foreign `requestId` in a reply makes `filedo.exe` exit 2 (items I.3 and
  X, case C19). A closed connection with no line or a first line without `schemaVersion` makes `filedo.exe` exit 2 as
  an older copy (item K, case C15). A pipe response line over 64 KiB fails `response-too-large` (1) (item L, case C17).
- **The setting of rule 13** is on the Settings page (`ShellCommandsRestricted` in the user's own key), off
  until the person turns it on, changed by no command; `status` reports it. Every verb of `cliVersion` 1.0 is
  `safe`, so the gate refuses nothing yet.
- **Log line of rule 16** through the shell log, arguments redacted by `CLI-EVENT-STREAM` rule 13.
- **Product reason tokens** beyond the table of rule 12 (allowed by its last sentence): `not-closed` (a window
  did not close on `quit`), `error` (the command threw; the detail is in the shell log).
- **Tests that must stay.** `go test ./cmd/filedo/ -run TestAppCommands` (C1, C2, C3, C4, C5, C7, C8, C10,
  C13, C15, C16, C19 and the reserved words) and `filedo_win.exe --selftest`, group `appcmd` (C1, C4, C5, C6, C8, C13,
  C14, C17, missing `requestId`, missing `--out` folder, the log line, the pipe name). C9 - an elevated first instance
  answering a non-elevated caller `elevation`, and another user or session reaching nothing - is a recorded manual step.
- **The window verbs (SP-0217).** `disk-manager show`, `settings show` and `tray start` are start verbs of the window
  program: with no first instance, each launches the spelling of its own start (`--disks`, bare, or `--startup --tray`)
  and then asks the copy it started; with a copy running, the copy answers and nothing is started. Tests that must stay:
  `go test ./cmd/filedo/ -run TestWindowVerbs` - each verb is a subtest of `TestWindowVerbs_ColdStartLaunchesThenAsks`
  and `TestWindowVerbs_ForwardedWhenRunningLaunchesNothing`, and all three are in `TestWindowVerbs_BusyIsExit11`,
  with `TestWindowVerbsLaunchTheRightWindow` and `TestWindowVerbs_FrozenSpellingsStillStart` - and the self-test rows
  `appcmd:window-verb:*` and `appcmd:window-verbs-modal-is-busy`, group `appcmd`. The windows, tray icons and processes
  after each verb are a live check at the release.
- **The job verbs (SP-0218).** `job list` is `coldStart: refuse` and answers `data.jobs`, one object per run of the
  main window (the Job page and the Command page) and per running or queued operation of the Disk Manager: `where`
  (`shell` or `manager`), `verb` (a console verb from a closed list, `command` for the Command page), `state`
  (`running` or `queued`), `step` (a short word, empty until the run names one) and `percent` (a number, `null` when
  the run has not said how much there is to do) - no path and no name of the person's files; `status.runningJobs` is
  the number of `running` entries. `job stop [--where all|shell|manager]` is `coldStart: oneshot` (with no program
  running it is done, `stopped` 0): it makes the Stop button's own request on every run of the scope, empties the
  Disk Manager's queue, and waits - on the listener's own thread, never the window's - until the scope holds no run,
  at most the verb's `timeoutMs` less 3 s, then answers `failed`, 2, `timeout` and ends nothing by force.
  `quit --stop-jobs` does the same for every window and then closes as the window's own Exit closes; without the
  flag a run in progress is the refusal (11, `busy`) of SP-0216. While such a wait lasts the pipe serves no other
  command (rule 10, one at a time): a second caller is answered busy at its delivery bound. Tests that must stay:
  `go test ./cmd/filedo/ -run TestJobVerbs` (`TestJobVerbs_ListNotRunningIsExit11`, `_ListPrintsEachRun`,
  `_ListEmptyPrintsNoRunLine`, `_StopWithNoProgramSaysZero`, `_StopForwardsTheScope`,
  `_StopScopeOutsideTheManifestIsExit10`, `_StopPastTheBoundIsExit2`, `_QuitStopJobs`, `_ManifestRows`) and the
  self-test rows `appcmd:job-*` and `appcmd:quit-*`, group `appcmd`. The runs listed, stopped and waited for with a
  live first instance, and the process gone after `quit --stop-jobs`, are live checks at the release.
- **The release-tooling verbs (SP-0219, SP-0223).** `selftest run`, `help-pages build`, 

  `ui-drive run` carry `exe: filedo_win.exe`, `audience: developer`, `forward: never` (item D, item Z) and the old switch
  as their frozen `legacy` alias object (rule 2, item E). The five folder-only verbs refuse a missing `--out` folder
  with exit 11 without creating it (item V.2). The window program starts them from its own command line before it
  claims the first-instance mutex and never through the pipe; `filedo.exe` neither reserves nor forwards their words, so
  a folder named `screens` keeps the target-first reading. Tests that must stay:
  `go test ./cmd/filedo/ -run "TestAppCommands_C13|TestAppCommands_ReleaseTooling"`
  (`TestAppCommands_C13_ReleaseToolingSpellingsAreListed`, `TestAppCommands_ReleaseToolingIsNotFilediosVerbs`) and the
  self-test rows `appcmd:switch-off:shipped-verbs-pass`, `appcmd:tooling-missing-folder-refused`, group `appcmd`.
- **The container-file start verb (SP-0228).** `open --path <file> [--mode <mount|mount-ro|unmount>]` carries
  `exe: filedo_win.exe`, `forward: never`, `defaultFor: filedo_win.exe`, `coldStart: oneshot`, `wait: done`,
  `timeoutMs: 0`, `risk: safe`, and four `legacy: true, answers: false` aliases (`{path}`, `--mount {path}`,
  `--mount-ro {path}`, `--unmount {path}`). `filedo_win.exe open` opens a `.fdd` or `.fd-sec` container in its own
  window with no single-instance claim and no handover (or opens an already-mounted `.fdd` drive in Explorer);
  `filedo.exe` neither reserves nor forwards `open`. Tests that must stay:
  `TestAppCommands_C13_ContainerOpenSpellingsAreListed` and self-test rows `appcmd:open-*`, group `appcmd`.
- **The documents (rule 20, SP-0220).** `docs/CLI.md` is generated from the manifest by `packaging/write-cli-docs.ps1`
  and never edited by hand; the `internal-docs` step of the gate (`write-cli-docs.ps1 -Check`) fails when it is stale,
  when `docs/cli-commands.json` and the copy `filedo.exe` embeds differ, or when a page of the commands guide
  (`docs/guides/commands.html` and its de and fr pages) leaves a command of the manifest out. The README section
  *Commands for Scripts and Agents* (five languages), the privacy page and the Store listing's privacy summary name
  the four risk classes and the one setting of rule 13. `AGENTS.md` tells development agents to read `docs/CLI.md` and
  run `filedo commands --json`.
