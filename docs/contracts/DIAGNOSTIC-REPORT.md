# DIAGNOSTIC-REPORT

| | |
| --- | --- |
| **Id** | `DIAGNOSTIC-REPORT` |
| **Version** | 0.9, draft |
| **Role** | producer - `filedo_win_vb/LogReport.vb` |
| **Home** | shared contracts catalog, folder `diagnostic-report/`, document `README.md` |
| **Owner** | shared (candidate StreamsPlayer) |

## What this repository must do to stay conformant

- **User-initiated only.** Diagnostic export runs only upon explicit user action ("Send logs"), never on a timer or background telemetry.
- **Sanitization invariants.** Log bundles mask secret credentials, true sealed names, and redact user profile paths (`<USER>`, `<APP_DATA>`).
- **Bounded archives.** Diagnostic zip size is bounded (max 40 files, max 20 MB payload total), with manifest `filedo-report.txt`.
