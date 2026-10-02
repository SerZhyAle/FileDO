# DIAGNOSTIC-REPORT

| | |
| --- | --- |
| **Id** | `DIAGNOSTIC-REPORT` |
| **Version** | 0.11, draft; partial adoption |
| **Role** | producer - `filedo_win_vb/LogReport.vb` |
| **Home** | shared contracts catalog, folder `diagnostic-report/`, document `README.md` |
| **Owner** | StreamsPlayer |

## What this repository must do to stay conformant

- **User-initiated only.** Diagnostic export runs only upon explicit user action ("Send logs"), never on a timer or background telemetry.
- **Sanitization invariants.** Text passes through `DiagnosticText` before packaging. Credential, URL and absolute-path lines and private-key blocks are omitted; ZIP entry names are generated. History, JSON reports and file lists are excluded. The GUI selftest exercises synthetic credential and path fixtures through the actual archive writer.
- **Bounded archives.** At most 40 logs and 20 MB of log payload, plus count-only `environment.txt` and `filedo-report.txt`. Oversized source files are omitted with a marker, rather than exporting an opaque fragment.
- **Session logs.** Each GUI process keeps its own leased log. Startup keeps the ten newest sessions and preserves older live sessions; logs above 16 MB compact to a 1 MB head and 7 MB tail with a marker.
- **Remaining scope.** FileDO has no JSON diagnostic clipboard envelope. The dated registry exception records this carrier gap and the oversized-file omission policy. Media-library metrics are explicitly unavailable, never inferred from user files.
