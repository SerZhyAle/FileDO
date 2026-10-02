# SP-0063 live verification - 2026-10-01

Verdict: FAIL for the visual gate; manual kit incomplete. Build: 2610011104 (GUI FileVersion 26.10.1.1104). No commit or release performed.

## Fresh evidence

- GUI `--selftest`: exit 0, `selftest: PASS (3066)`; see `selftest.log`.
- `window-probe.ps1`: exit 0; see `window-probe.log`. Manager-only launch has one window; second `--disks` exits 0 and leaves one manager; plain second launch opens the shell in the same process. Closing either window leaves the other running; closing the last exits 0.
- Live screen captures, EN/RU/DE, Light/Dark, DISPLAY5 at 96 DPI and 144 DPI. Six pictures per scale under the language folders. These contain two real, registered, unmounted 64 MiB test containers in a scratch state directory, not a synthetic snapshot.
- The original capture script saves requested outer dimensions without scaling them. Its 150 % pictures have a 1078x584 client, so they are narrower in design units than the 100 % pictures. `capture-design-size.ps1` explicitly sets scaled outer dimensions: 1628x904 client at 144 DPI. Corrected EN/RU/DE 150 % pictures are under `design-size/`.
- Mixed-monitor path: launch on the primary display, move to DISPLAY5 at 150 %, resize to the same design dimensions. Light/Dark evidence under `mixed/`.
- Installed Start-menu shortcut targets `C:\Program Files\FileDO\filedo_win.exe --disks`, version 26.9.30.2145. It does not prove shortcut launch against the tested build.

## Findings

1. At 100 % the list header font is visibly larger than the row font and is clipped vertically and horizontally. English Drive, Protection and Auto and German column captions truncate. See `en/disks-launch-100pct-light.png` and `de/disks-launch-100pct-dark.png`. At 150 % corrected width the headers fit. This fails the visual gate (kit 5b.7). Root cause not established.
2. Dark narrow-width 150 % window shows a white native horizontal scrollbar: `en/disks-launch-150pct-dark.png`. At corrected 1100 design-pixel outer width it disappears (`design-size/disks-launch-150pct-dark.png`). This is a narrow-window theme finding, not proof of a scrollbar at the intended default width.
3. SendKeys attempts for F1 and Ctrl+Shift+O did not open another window. Treat these as unsuccessful automation attempts, not product defects: foreground keyboard delivery was not independently proven. `capture-150.log` records the F1 capture failure. Keyboard shortcuts remain unverified.

## Manual kit coverage

| 5b item | Status | Evidence / remaining work |
| --- | --- | --- |
| 1 Every way in | PARTIAL | Command-line launch and single manager proven; shortcut target read. Header and rail buttons, actual new-build Start-menu launch not proven. |
| 2 Window lifetime | PARTIAL | Both windows and closing order proven through command-line handover and WM_CLOSE; mutual buttons and keyboard not proven. |
| 3 First run | NOT VERIFIED | Welcome dismissal, create/add actions, browser/network observation remain. |
| 4 State changes | NOT VERIFIED | Real initiator mount/eject/server termination/logon remain. |
| 5 One gesture | NOT VERIFIED | Consent, Explorer, RAM save and multiselect unmount remain. |
| 6 Keyboard / Narrator | NOT VERIFIED | Selftest pass is supporting evidence only. |
| 7 Look | FAIL / PARTIAL | Both themes and three languages at 100/150 %, plus mixed-monitor move captured. Header clipping found at 100 %. Selected/hover row and populated detail pane remain. |
| 8 Mount outlives manager | NOT VERIFIED | No disks mounted during this run. |
| 9 Drag/drop | NOT VERIFIED | Real drag/drop remains. |
| 10 Help links | NOT VERIFIED | No browser links invoked. |
| 11 Destructive safeguards | NOT VERIFIED | Live delegated Format/Destroy pages and gestures remain. |
| 12 Credential absence | NOT VERIFIED | No encrypted mount performed. |

## Restoration and limits

DISPLAY5 restored from 150 % to its original 100 % in finally blocks. Capture scripts and window probe restore HKCU Software\FileDO values, including registry types. Only processes created by the probes were terminated; the previously installed app exited independently before testing began. No installation, mount, UAC, sign-out, scheduled-task or destructive operation was performed.

Scratch containers: `%TEMP%\filedo-sp0063-20261001\`; scratch registry/history: `%TEMP%\filedo-capture-dpi-state\`. Kept for reproduction. No machine-wide disk registration was changed. Existing modified executables and earlier `tests/vd_manual.md` edits were preserved.
