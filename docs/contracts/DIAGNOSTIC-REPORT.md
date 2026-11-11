# DIAGNOSTIC-REPORT

| | |
| --- | --- |
| **Id** | `DIAGNOSTIC-REPORT` |
| **Version** | 0.15, draft; read 2026-10-29 (SP-0174); implemented partial 0.15 - item 1 `archive_cap` marker built (SP-0182, release selftest pending); item 3 (absent families written `=0`) open |
| **Role** | producer - `filedo_win_vb/LogReport.vb` |
| **Home** | shared contracts catalog, folder `diagnostic-report/`, document `README.md` |
| **Owner** | StreamsPlayer |

## What this repository must do to stay conformant

- **User-initiated only.** Diagnostic export runs only upon explicit user action ("Send logs"), never on a timer or background telemetry.
- **Sanitization invariants.** Text passes through `DiagnosticText` before packaging. Credential, URL and absolute-path lines and private-key blocks are omitted; ZIP entry names are generated. History, JSON reports and file lists are excluded. The GUI selftest exercises synthetic credential and path fixtures through the actual archive writer.
- **Bounded archives.** At most 40 logs and 20 MB of log payload, plus count-only `environment.txt` and `filedo-report.txt`. Oversized source files are omitted with a marker, rather than exporting an opaque fragment.
- **Session logs.** Each GUI process keeps its own leased log. Startup keeps the ten newest sessions and preserves older live sessions; logs above 16 MB compact to a 1 MB head and 7 MB tail with a marker.
- **Carriers (section 8 A).** The archive `filedo-logs-<stamp>.zip` is the carrier. FileDO has no copy-to-clipboard diagnostic surface, so the single JSON envelope is not required of it.
- **Per-product metrics (section 8 B).** FileDO names its own count-only facts in `environment.txt` and states the media-library family unavailable, never inferred from user files.
- **The omission marker (0.14, section 8 C).** A source above the 16 MB cap is omitted whole, with exactly `[Diag] LOG OMITTED | reason=oversized_untrusted | source_bytes=<decimal>` - those fields, in that order, and nothing of the source in it. The selftest rows `diagnostic:omitted-marker` and `diagnostic:omitted-no-source` hold it on a synthetic file. A log that the archive budget cannot hold, including one whose redacted text outgrew what the budget had left, is omitted whole with `[Diag] LOG OMITTED | reason=archive_cap | source_bytes=<decimal>` in its place (0.15 item 1); the selftest rows `diagnostic:archive-cap-marker` and `diagnostic:archive-cap-no-source` hold it on a synthetic file with a lowered budget.
- **Exchange account values (section 10).** FileDO holds no exchange login, server address, device token, pairing code or certificate fingerprint, and the bundle is an allow-list of FileDO's own log names, so none can enter it.
