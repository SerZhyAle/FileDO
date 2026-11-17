# APP-ACTIVATION

| | |
| --- | --- |
| **Id** | `APP-ACTIVATION` |
| **Version** | 0.16, draft; read 2026-11-16 (SP-0221); implemented partial 0.16 - SP-0192 focus check shipped in v2611110217, SP-0216..0220 pipe commands in uncommitted working tree, manual checks (`tests/vd_manual.md`) remain open |
| **Role** | consumer, focus-only profile (the 2026-10-17 amendment) - GUI shell only |
| **Home** | shared contracts catalog, folder `app-activation/`, document `README.md` |
| **Owner** | CyrFlip |

## What this repository must do to stay conformant

- **Profile.** FileDO is bound as the focus-only profile for a second start: no `filedo://` scheme and no forwarded payload on a start. Rules 1 and 4 do not apply; rule 2's session-local mutex does (`Local\FileDO.Shell`, a frozen anchor).
- **The command pipe (since 2026-11-14, SP-0216).** The app commands of `APP-COMMANDS` travel on a pipe of rule 3's third shape, `FileDO_Commands_<userSid>_<sessionId>`, listened to by the first copy only, current user only, a 1 s connect bound, a per-connection failure boundary and a nudge connection on shutdown. The focus-only start below does not use it.
- **The signal.** A plain second start asks the running copy for its window with a registered local window message (`FileDO.Shell.ShowShell`; `--disks` uses `FileDO.Shell.ShowDiskManager` - both frozen), sent with `SendMessageTimeout` and `SMTO_ABORTIFHUNG` inside a fixed bound, with no payload. A window that does not answer is not a holder and not an absence: the start waits out its bound and opens its own window rather than vanish.
- **Per-target window policy.** A start that carries a file is never handed over; each file opens its own window. FileDO is therefore single-instance per session for plain starts and deliberately multi-window per file target - it is never described as a globally single-window application.
- **Tests that must stay.** `filedo_win.exe --selftest`, group `activation`: a hung receiver (bounded, not claimed, recovers), a payload-less signal, a file target that is not lost. A receiver in another user's session or at a higher integrity level is not testable in the selftest and stays a manual check.
