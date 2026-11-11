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

**Gates.** The existing `rail:*`, `rail-label:*`, `rail-hue:*`, `windows-ui:*` rows hold the rail's anatomy, its
one ink policy and the remembered page; `settings-nav:tokens`, `settings-nav:ink`, `settings-nav:dialog`,
`settings-nav:breakpoint`, `settings-nav:narrow`, `about:resources` and `about:related` (the `ui-contracts` group
of `--selftest`) hold the 32/16/8/12 row tokens, the Disk settings window's navigation, the narrow navigation view
and the complete About set. No measured timing row exists yet.

**Status: partial, migration in progress (SP-0169; timings S5 2026-11-27).** Built in the working tree 2026-10-29
(slice S3), not yet measured by a release build: the rail's 32/16 tokens and one identity-coloured ink policy, the
narrow-layout navigation view (breakpoint: a client width under rail 240 plus content 480 design pixels), the
Disk settings window's two-destination rail, and the complete About set with the related programs. Open: the
complete-frame timings (S5) and the real-build captures (S4).
