# SETTINGS-NAVIGATION

| | |
| --- | --- |
| **Id** | `SETTINGS-NAVIGATION` |
| **Version** | 2.5, read 2026-11-26 (FILEDO-0245); implemented partial 2.5 (item J, the shell's whole rail, audited by source read 2026-11-26, FILEDO-0248; open clauses below - FILEDO-0249, and FILEDO-0250 waiting on a catalog proposal); migration SP-0169 in progress (no wire - settings navigation, page composition and presentation behavior) |
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
and the complete About set. The complete-frame timings are the `page-timing:*` checks of `ui-drive run`
(`PageTiming.vb`: activation to presented frame, 20 first visits and 20 warm transitions per page, raw
`page-timings.csv`, nearest-rank p95 and maximum in `page-timings.txt`); written 2026-11-19, not yet run.

**Status: partial, migration in progress (SP-0169; timings S5 2026-11-27).** Built 2026-10-29 (slice S3) and
shipped in `v2611110217` and `v2611171800`: the rail's 32/16 tokens and one identity-coloured ink policy, the
narrow-layout navigation view (breakpoint: a client width under rail 240 plus content 480 design pixels), the
Disk settings window's two-destination rail, and the complete About set with the related programs - measured by
the `rail:*`, `rail-label:*`, `rail-hue:*`, `settings-nav:*` and `about:*` rows that ran all-or-nothing green in
both release gates (no per-row log retained). Open: the complete-frame timings (S5) and the real-build captures
(S4).

**The shell rail under item J (audit of 2026-11-26, FILEDO-0248, source read only).** The window is a work window
with a rail, so sections 2, 4 and 5 bind every row. Met: the start-side rail and pane, names with one vocabulary
glyph each, one child level under parent headers with their own disclosure, the row tokens, the narrow view, the
selected and keyboard states; the product's own settings are the Settings destination (theme and language in its first
group), About is last, the Settings groups run from main to advanced, and no work page edits a Settings value. Adapted
by the same ticket, not yet compiled: the Disk manager row is marked as an action row (`settings-nav:action-row`).
Adapted by FILEDO-0250, not yet compiled: the window the shell asks for opens or comes forward on the requesting
window's monitor (item H condition 3, `disk-ui:manager-follows-requester`), and the action row draws the vocabulary's
`content.disk-container` in the Disks hue, the rail's one treatment (2.3, item I,
`disk-ui:shell-opens-manager-button`; the window, its taskbar button and the Start menu shortcut keep the plated
product icon - owner decision 2026-11-26). Open:

- the action row opens a work window with a toolbar and no rail of its own (items H and K). An amendment permitting
  that shape was proposed to the catalog on 2026-11-26 (FILEDO-0250, canon CANONR-0239) and is not yet decided;
- choosing a job row resets the job page, so input not yet run is lost, and only the Settings page keeps its scroll
  anchor (4.3) (FILEDO-0249);
- a drive job enumerates drives on the UI thread when its page is chosen (5.2) (FILEDO-0249).
