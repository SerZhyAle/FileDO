# DIAGNOSTIC-REPORT

| | |
| --- | --- |
| **Id** | `DIAGNOSTIC-REPORT` |
| **Version** | 0.9, draft |
| **Role** | producer - `filedo_win_vb/LogReport.vb` |
| **Home** | shared contracts catalog, folder `diagnostic-report/`, document `README.md` |
| **Owner** | StreamsPlayer |

## What this repository must do to stay conformant

- **User-initiated only.** Diagnostic export runs only upon explicit user action ("Send logs"), never on a timer or background telemetry.
- **Sanitization invariants.** Archive contents must mask credentials and personal paths before packaging. FileDO's current raw-file archive has not established this guarantee; the dated registry exception records the gap.
- **Bounded archives.** The current diagnostic zip is bounded (max 40 files, max 20 MB payload total), with manifest `filedo-report.txt`. The 0.10 session-log and environment carrier is pending implementation.
