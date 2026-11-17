# CLIPBOARD-GUARD

| | |
| --- | --- |
| **Id** | `CLIPBOARD-GUARD` |
| **Version** | 0.12, read 2026-11-16 (SP-0221); 0.12 item S N/A; scoped to persistent plain-text copies, draft |
| **Role** | consumer - persistent plain-text copying on the GUI thread |
| **Home** | shared contracts catalog, folder `clipboard-guard/`, document `README.md` |
| **Owner** | CyrFlip |

## What this repository must do to stay conformant

- **UI-thread clipboard use.** `Ui.CopyText` and `LogReport.CopyPathToClipboard` call WinForms `Clipboard.SetText` from GUI actions on the STA message-loop thread, as amendment A permits. A failed copy is reported without an unhandled exception.
- **Scope of the copy.** These actions copy ordinary command text or the diagnostic archive path for the user to keep. FileDO has no in-place selection transform, clipboard backup/restore, clipboard monitor, history vault, or secret-copy action; the conditional pipeline and privacy-marker rules do not run on these paths.
