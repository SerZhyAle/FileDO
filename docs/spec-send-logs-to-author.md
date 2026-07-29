# Send Logs to Author - Specification

Status: Implemented
Rung: complex (new source file, new user-visible action, five-locale UI strings, seven doc surfaces)
Owner decisions taken: 2026-07-30
Authoritative record: this file. The repo has no ticket catalog, so the `Status:` line above is the
single record of state and is edited only by advancing it one level.

## 1. Problem

When something misbehaves, the author has nothing to look at. FileDO already leaves a useful trail on
the machine - an operation history, comparison and deletion reports, a check report and its resume
state, a skip list, and the GUI debug log - but that trail is scattered across whatever working
directory each run happened to use. A user who is not a developer cannot reasonably be asked to find
those files, understand which of them matter, pack them, and mail them.

There is no in-app path from "it broke" to "the author has the evidence".

## 2. Goals

1. One button, pressed by the user and by nobody else, that packages every local FileDO diagnostic
   artifact it can find into a single archive.
2. The user's own default mail program opens, pre-addressed to the author, with a subject that
   identifies the build and a body template that asks the three questions worth asking.
3. Nothing leaves the machine until the user presses Send in that mail program.
4. The archive is an ordinary file: the user can open it, inspect it, strip anything they do not want
   to share, or delete it and walk away.
5. The feature is described on every user-facing surface, in every locale that surface carries, in the
   same change.

## 3. Non-goals

- No automatic crash reporting, no telemetry, no background upload, no HTTP client, no listening port.
- No new logging subsystem and no new log files. Only artifacts that already exist are collected.
- No CLI counterpart. The CLI leaves its artifacts in its own working directory and a console user can
  already reach them.
- No Simple MAPI, no Outlook COM automation, no SMTP client of our own.
- No change to any frozen anchor, MSIX capability, winget manifest field or installer identity.

## 4. Constraints

Discovered from the code and the packaging, not assumed:

- **The GUI is one flat form.** `filedo_win.exe` is .NET Framework 4.8 WinForms with a single
  `MainForm`; there is no settings dialog, no `TabControl` and no About window anywhere in the
  project. The requested "About tab inside Settings" surface does not exist and would have to be
  built. See decision D1.
- **`mailto:` cannot carry an attachment.** RFC 6068 excludes it, and Outlook, Windows Mail and
  Thunderbird all drop an `attachment=` header field. A default-mail-client hand-off that pre-attaches
  a file needs Simple MAPI or a client-specific automation API. See decision D2.
- **The GUI writes a log only under `-debug`**, as `filedo_win_debug.log`, relative to the process
  working directory.
- **CLI artifacts follow the working directory.** The GUI launches `filedo.exe` with
  `WorkingDirectory` set to `%USERPROFILE%`, so GUI-driven runs drop their artifacts there. A user who
  runs the CLI by hand leaves them wherever that shell was.
- **System-drive writes are redirected** to `%TEMP%\FileDO_Operations`, so that directory can hold
  artifacts too.
- **The MSIX build is sandboxed.** `%TEMP%` is writable (redirected per-package); the install folder
  under `WindowsApps` is not. The collector must never require a writable install directory, and the
  archive must never be written next to the executable.
- **The privacy page currently claims no network use at all** - its title is literally "no network, no
  telemetry, no accounts". A feature that hands data to a mail client has to be named there in the
  same change, or the page, the store data-safety answers and the product stop agreeing.

## 5. Decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | A dedicated **About window** carries the button, together with the build identity, the author, and the links to the site, the source, the issue tracker, the privacy page and the author's other tools. `MainForm` gains only a compact **About** button in its top-right corner. No settings shell is built. | Owner decision, 2026-07-30, revised during implementation. First tried as a wide button in `MainForm`'s bottom action row (D1a below); the owner rejected it on sight as oversized and out of place there. The About window is also where a user looking for "who wrote this and how do I reach them" already goes, so the log button sits next to the mail address it uses. |
| D1a | *Superseded:* a `Send logs` button in the bottom row of `MainForm`, between **Copy command** and **RUN**. | Built, reviewed, rejected: it competed visually with **RUN** and put a rare diagnostic action in the primary action row. Kept here so the reasoning is not rediscovered. |
| D2 | Transport is **`mailto:` plus Explorer**, never Simple MAPI. The archive is built, its folder is opened with the archive selected, its full path is placed on the clipboard, and the default mail program is then opened pre-filled. The user attaches the file and presses Send. | Owner decision, 2026-07-30. Predictable on every Windows configuration including MSIX, with no `mapi32.dll` dependency and no silent failure mode on machines with no MAPI client. |
| D3 | **Subject and body are English** whatever the GUI language is; the **UI strings around them are localized** into all five GUI languages. | The mail is an author-facing artifact and falls under the English-artifact rule; the dialogs are user-facing and fall under the localization rule. |
| D4 | The archive is written to `%TEMP%\FileDO_Logs\` and never cleaned up automatically. | The user must be able to find it again after closing every dialog. `%TEMP%` is writable in every packaging flavour, and leaving the file is what makes the manual attach step recoverable. |

## 6. What gets collected

### 6.1 Directories searched

In this order, each one skipped without complaint if it does not exist or cannot be read:

1. The folder holding `filedo_win.exe` - portable and installed layouts keep the GUI debug log here.
2. `%USERPROFILE%` - the working directory the GUI gives every CLI run it launches.
3. `%TEMP%\FileDO_Operations` - the system-drive redirect target.
4. `%TEMP%` itself.

Search is one level deep. No recursion: a recursive sweep of a user profile is both slow and a way to
pick up files that are none of our business.

### 6.2 File patterns

Only names FileDO itself produces:

`filedo_win_debug.log`, `history.json`, `check_report_*.log`, `check_report_*.json`,
`check_report_*.csv`, `check_state.json`, `compare_report_*.log`, `delete_report_*.log`,
`skip_files.list`, `damaged_files.log`.

### 6.3 Caps

An archive that a mail provider rejects is worse than no archive.

- Newest first, by last-write time.
- At most 40 files.
- At most 8 MB per file; a larger file is named in the report and left out.
- At most 20 MB of payload in total; once the budget is spent the remaining files are named in the
  report and left out.

### 6.4 Generated report

One extra entry, `filedo-report.txt`, written into the archive and nowhere else. It carries:

- The local and UTC timestamp of collection.
- `filedo_win.exe` file version and full path.
- `filedo.exe` file version, size and last-write time if it sits next to the GUI, otherwise a line
  saying it was not found there.
- OS version, 64-bit OS and 64-bit process flags, CLR version.
- The current UI culture and the GUI language currently selected.
- A manifest: every included file with its original full path, size and last-write time.
- Every skipped file with the reason - too large, or over the total budget.

It deliberately does **not** add the Windows user name or the machine name as fields. Collected paths
may still contain the profile folder name, which is exactly why the confirmation dialog says so.

## 7. Behaviour

0. The user presses **About** in the top-right corner of the main window. The About window opens
   modally, showing the product line, the GUI build stamp, the `filedo.exe` version beside it, the
   author and their mail address, six links (website, source, issues, privacy, the author's other
   tools, and the mail address itself), the licence line, one sentence explaining what sending logs
   is for, and the **Send logs to the author** button. Every string except the URLs, the author
   name and the version stamps is localized.
1. The user presses **Send logs to the author**.
2. A confirmation dialog names, in the GUI language: what will be collected, that the paths inside may
   contain personal folder names, that the archive is left on disk for inspection, and that nothing is
   transmitted until the user sends the mail themselves. Buttons are Yes/No; No does nothing at all.
3. The archive is built at `%TEMP%\FileDO_Logs\filedo-logs-<yyyyMMdd-HHmmss>.zip`.
4. If not a single artifact was found, no archive is written and no mail program is opened; a dialog
   explains that there is nothing to send yet and how to produce a log - run the GUI with `-debug`, or
   run a command that writes a report.
5. Otherwise, in this order: Explorer opens with the archive selected, the full path goes to the
   clipboard, the default mail program opens with the author's address, the subject
   `FileDO logs <gui version> <yyyyMMdd-HHmmss>` and an English body template.
6. A final dialog restates the one manual step - attach the file whose path is already on the
   clipboard - and shows the path in full.
7. Any failure - archive creation, Explorer, clipboard, mail program - is reported in one dialog that
   names what failed and the archive path if there is one. No step is allowed to take the others down
   with it: a missing default mail client still leaves the user with a finished archive and an open
   folder.

## 8. Privacy contract

- The action is **user-initiated only**. No timer, no startup hook, no failure path invokes it.
- FileDO itself still opens no socket. The only outbound step is the mail the user composes and sends
  in their own client, under their own account.
- The archive contains only files FileDO wrote plus the generated report. It is left in `%TEMP%` where
  the user can inspect or delete it.
- The privacy page gains a section stating exactly this, and its effective date moves.

## 9. Acceptance criteria

| # | Criterion | Evidence rung |
| --- | --- | --- |
| A1 | `MainForm` shows a compact **About** button in its top-right corner that never overlaps the language row, and it opens the About window; the About window shows the build stamp, the author, the six links, the licence line and the **Send logs to the author** button, all in the selected language. | run-and-observe |
| A2 | Pressing No in the confirmation dialog writes no file and opens nothing. | run-and-observe |
| A3 | With at least one artifact present, one archive appears under `%TEMP%\FileDO_Logs\`, opens as a valid zip, and contains `filedo-report.txt` plus the collected files. | run-and-observe |
| A4 | With no artifact anywhere, the "nothing to send" dialog appears and no archive is written. | run-and-observe |
| A5 | Explorer opens with the archive selected, the clipboard holds the archive path, and the default mail program opens addressed to the author with the versioned subject. | run-and-observe |
| A6 | Every new UI string resolves in all five languages, with no key leaking through as a raw identifier. | targeted run per language |
| A7 | The GUI compiles clean in Release. | MSBuild exit 0 |
| A8 | No MSIX capability, winget manifest field, installer identity or other frozen anchor changed. | grep the diff |
| A9 | The feature is described in the GUI README, all five top-level READMEs, the site in every locale it carries, the GUI guide page, and the privacy page - in one change. | grep each surface |
| A10 | Every link in the About window points at a URL that exists in the project's own published set, and each one opens in the default handler. | grep the sources, then run-and-observe one link |

## 10. Surfaces to land together

`filedo_win_vb/README.md`, `README.md`, `README.ru.md`, `README.ua.md`, `README.de.md`,
`README.fr.md`, `docs/index.html` (ru/en/ua inline), `docs/guides/gui-command-builder.html`,
`docs/privacy.html` (ru/en/ua inline, new section, renumbered sections, new effective date), and
`msix/store-listing.md` - whose "these never leave your device" claim about the local log files
stops being true the moment a button can mail them, so it is corrected in the same change.

`docs/de/index.html` and `docs/fr/index.html` are **not** content surfaces: each is a 373-byte
stub that sets the language and redirects to `docs/index.html`. Nothing to land there.

## 11. Parked, not fixed here

- `filedo_win.exe -debug` opens its log file relative to the process working directory, so an installed
  or MSIX-packaged build launched from its own folder fails to create it and shows "Failed to create
  log file". Pre-existing, orthogonal to this change, and the collector already searches the folders
  where a successfully created log would land.

## Last Audit

2026-07-30, self-audit against sections 6-10, then re-verified at build time on build
`2607300116`/`2607300117`. Verdict: **Implemented** - every acceptance criterion has a fresh
command behind it, and no manual item is left open.

The build-time pass found and fixed one real defect the first audit had missed, because it had
only ever looked at the English window: **four About-window labels were clipped in ru, de and
fr.** `TextRenderer.MeasureText` against each label's own box, run for all five languages,
reported `LABELS CUT: 4` - the Russian licence line needed 342px in a 336px single-line box and
lost its last word (`открыт.`) with no ellipsis to show for it, and the ru/de/fr taglines each
needed four 13px lines in a 51px box. `lblTagline` went to five lines and `lblLicense` to two
(design heights 78 -> 102 and 24 -> 48), everything below shifted down and the form grew
568 -> 616. The same measurement now reports `LABELS CUT: 0` in all five languages, and the
on-screen captures confirm it. A first attempt to judge this from `DrawToBitmap` output was
misleading - it repaints overlapping labels differently and appeared to shave the tagline's first
line, which `CopyFromScreen` shows is not clipped at all. Measure the labels, and capture from the
screen.

Files touched: `filedo_win_vb/LogReport.vb` (new), `filedo_win_vb/AboutForm.vb` +
`AboutForm.Designer.vb` (new), `filedo_win_vb/MainForm.vb`, `MainForm.Designer.vb`,
`Localization.vb`, `FileDOGUI.vbproj`, plus the documentation surfaces of section 10.

| # | Result | Evidence |
| --- | --- | --- |
| A1 | PASS | UI Automation against build `2607300116`: `About` at 1024,367 83x19, `overlaps the language row: False`, `button inside the window: True`; clicking it opened `About FileDO` at 728,320 376x439 and enumerated all 16 of its controls - build stamp, `CLI filedo.exe`, author, six links, licence line and `Send logs to the author`. `gui-mainform.png`, `gui-about.png` and the per-language `about-{ru,de,fr,uk}.png` show nothing clipped. |
| A2 | PASS | Same run: `confirm dialog: 'FileDO - send logs to the author'`, `CLICKED: No`, then `A2 archives before: 2` / `A2 archives after No: 2` - `A2 PASS (nothing written): True`, and no Explorer or mail client started. |
| A3 | PASS | Headless run against the built assembly `2607300116`: `filedo-logs-20260730-011853.zip` under `%TEMP%\FileDO_Logs`, `zip opens as a valid zip: True`, 3 entries (`profile/history.json`, `app/history.json`, `filedo-report.txt`), report printed in full, `report adds a machine/user-name field: False`, `stray archives next to the exe: 0`. |
| A4 | PASS (carried) | Proven at the first audit by renaming the two real artifacts aside: `A4 NO ARCHIVE RETURNED: True`, `count=0`; both restored with `hash match: True`. Not re-run at build time - the collector is byte-for-byte unchanged since, the build-time fix touched only `AboutForm.Designer.vb`. |
| A5 | PASS (carried) | Proven at the first audit: `archives after Yes: 1`, clipboard equal to the archive path, `processes started during the hand-off: .., explorer, .., olk`. Deliberately not re-run at build time, because it opens the owner's real mail client; the hand-off code is unchanged since. |
| A6 | PASS | Re-run against build `2607300116`: all 24 new keys resolved in en/ru/uk/de/fr with no missing, empty, pipe-bearing or accidentally-English value, and no `...` or em/en-dash - `A6 FAILURES: 0` over 120 key/language pairs. Two same-as-English values are deliberate and whitelisted (`about_cli` is a product token; German for `Links:` is `Links:`). Rendering checked separately: `LABELS CUT: 0` in all five languages. |
| A7 | PASS | `.\build.ps1 -Test` -> all five binaries `OK`, `Smoke: filedo.exe -? ... OK (version 2607300116)`, `go test ./cmd/filedo-test ... OK`, `Test gate passed.`, `BUILD_EXITCODE=0`. |
| A8 | PASS | `git diff --name-only` contains no `winget/`, no MSIX manifest, no `packaging/`, no `go.mod`. |
| A9 | PASS | Every surface in section 10 greps positive for the feature; the HTML edits keep `section`, `article`, `li` and `ul` tags balanced. |
| A10 | PASS | All five URLs plus the mail address grep out of the project's own published set (`README.md`, `docs/index.html`, `docs/privacy.html`); the About window's links were observed as live link controls with their URLs on the tooltips. |

Not claimed: behaviour under an MSIX-packaged install. The flow avoids every known sandbox
trap by construction - it writes only to `%TEMP%`, never to the install folder, and adds no
capability - but it was proven on the plain Release build, not on the packaged one. Worth one
pass during the next release's pre-flight sweep.
