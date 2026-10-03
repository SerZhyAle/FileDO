# DIAGNOSTIC-REPORT

| | |
| --- | --- |
| **Id** | `DIAGNOSTIC-REPORT` |
| **Version** | 0.12, draft; re-read 2026-10-02 (SP-0122) |
| **Role** | producer - `filedo_win_vb/LogReport.vb` |
| **Home** | shared contracts catalog, folder `diagnostic-report/`, document `README.md` |
| **Owner** | StreamsPlayer |

## What this repository must do to stay conformant

- **User-initiated only.** Diagnostic export runs only upon explicit user action ("Send logs"), never on a timer or background telemetry.
- **Sanitization invariants.** Text passes through `DiagnosticText` before packaging. Credential, URL and absolute-path lines and private-key blocks are omitted; ZIP entry names are generated. History, JSON reports and file lists are excluded. The GUI selftest exercises synthetic credential and path fixtures through the actual archive writer.
- **Bounded archives.** At most 40 logs and 20 MB of log payload, plus count-only `environment.txt` and `filedo-report.txt`. Oversized source files are omitted with a marker, rather than exporting an opaque fragment.
- **Session logs.** Each GUI process keeps its own leased log. Startup keeps the ten newest sessions and preserves older live sessions; logs above 16 MB compact to a 1 MB head and 7 MB tail with a marker.
- **Carriers (0.12 section 8 A).** The archive `filedo-logs-<stamp>.zip` is the carrier. FileDO has no copy-to-clipboard diagnostic surface, so the single JSON envelope is not required of it: the former registry exception for that gap is closed by 0.12, not by code.
- **Per-product metrics (section 8 B).** FileDO names its own count-only facts in `environment.txt` and states the media-library family unavailable, never inferred from user files.
- **Oversized untrusted logs (section 8 C).** A source above the 16 MB cap is omitted whole with the marker `[Diag] LOG OMITTED | reason=oversized_untrusted | source_bytes=<n>`, because redaction cannot precede a cut of an unread source.
