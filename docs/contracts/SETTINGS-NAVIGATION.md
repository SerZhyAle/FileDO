# SETTINGS-NAVIGATION

| | |
| --- | --- |
| **Id** | `SETTINGS-NAVIGATION` |
| **Version** | 2.0 (no wire - settings navigation, page composition and presentation behavior) |
| **Role** | consumer - the shell rail with its Settings and About destinations, and the standalone Disk settings dialog; migration ticket SP-0169 |
| **Home** | shared contracts catalog, folder `settings-navigation/` |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

A start-side rail and a content pane that stays visible; short names with one glyph each and one ink policy for
the whole surface; one child level; About last and complete (description, version, licence, privacy, help, site,
support, diagnostic export, update information, repository, resource index, related products); a documented
narrow-layout adaptation; page changes that never resize the window, discard edits or run operations; complete
frames - warm p95 under 150 ms and first visit under 500 ms, 20 samples each, raw samples and the maximum.

**Gates.** The existing `rail:*`, `rail-label:*`, `windows-ui:*` rows hold the rail's anatomy and the remembered
page; no measured timing or About inventory row exists yet.

**Status: partial, migration in progress (SP-0169; S3 target 2026-11-12, timings S5 2026-11-27).** Known gaps, from
the read of 2026-10-21: group headers and lone jobs are identity-coloured while the rows inside a group are mono;
rows are 44 px with 20 px glyphs against the 32/16 baseline; the standalone Disk settings dialog has no rail; About
lacks the description, licence page, update information, resource index and related products.
