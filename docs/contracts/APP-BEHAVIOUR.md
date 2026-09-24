# APP-BEHAVIOUR

| | |
| --- | --- |
| **Id** | `APP-BEHAVIOUR` |
| **Version** | 0.10, draft (no wire - the twelve moments a user of any SZA desktop app meets) |
| **Role** | consumer - the GUI shell of `filedo_win.exe` (`filedo_win_vb/`) |
| **Home** | shared contracts catalog, folder `desktop-app-ux/`, document `APP-BEHAVIOUR.md` |
| **Owner** | StreamsPlayer. An amendment is proposed in the catalog, not decided here |

The console program `filedo.exe` is outside the contract (its section 1 scopes "a desktop application with
a window"); its typed-`WIPE` prompts stay the canon's destructive-tool rules.

## What this repository must do to stay conformant

1. **One no-action exit.** Every question is a `ShellDialog`: owned, modal, centred on its window; Escape
   and the close box give the answer that changes nothing. No `MessageBox.Show` in the shell except the
   one notice of a dying process in `Program.vb`.
2. **Growing text grows the surface.** Cards wrap (`Ui.Wrap`); rail labels wrap and the row grows - no
   ellipsis anywhere.
3. **Progress, cancel, no second start.** One progress rule for both run pages (`RunProgress`); Stop
   writes the `--stop-file`; a running job's page is not lost to navigation; closing while a run is active
   asks Stop and close / Keep running.
4. **Network and disk only on request.** No network code; the log zip is built and mailed by the user.
5. **Confirm the irreversible, not the empty.** Typed `WIPE` (and `-y`, since the CLI's console question
   cannot be answered from a window); an empty wipe target and an empty log folder are said to be empty,
   never confirmed.
6. **A failure is actions.** A named cause read from the exception's type, plus retry / open the folder /
   copy / send logs; the exception itself goes to `%LOCALAPPDATA%\FileDO\filedo_win.log`, which Send logs
   packs.
7. **Localized rendering cannot throw.** Every template is filled by `Localization.Format`.
8. **Direction beside the language** - narrowed: all five languages are left-to-right and none declares a
   direction; the rule binds the day a right-to-left language ships.
9. **Named controls.** Every operable control has text or an accessible name; rail rows carry a default
   action UI Automation can invoke and the name `rail:<key>`.
10. **Geometry on close, defensive restore.** `WindowPlacement.Place`: onto a screen that exists, title
    strip reachable, scaled by the saved DPI.
11. **First run asks; nothing is an answer.** Nothing reaches outside the machine on any run.
12. **Settings commit on their own button** - held by the letter: the page offers neither Save nor Cancel
    and holds three reversible values.

**Gates.** `cmd/filedo/shell_contract_test.go` - no caught exception's text on screen (rule 6), no
`String.Format` on a translation (rule 7), full key and placeholder parity of the five tables (rung 1), no
owner-less dialog (rule 1, the static sweep), no ellipsis in the rail (rule 2). `filedo_win.exe
--selftest` - every rail label measured in five languages (rule 2), the progress rule on both pages (rule
3), the Wipe page's confirmations (rule 5), the formatter's failure cases (rule 7), every control of every
page walked for a name and every rail row for a default action (rule 9, rung 2), placement cases (rule 10).

**Status: implemented, rule 8 narrowed.** FileDO has no adoption row in the catalog registry yet; writing it is the catalog owner's step.
