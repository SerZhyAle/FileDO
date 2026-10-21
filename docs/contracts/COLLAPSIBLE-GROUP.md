# COLLAPSIBLE-GROUP

| | |
| --- | --- |
| **Id** | `COLLAPSIBLE-GROUP` |
| **Version** | 1.1 (no wire - observable group structure, disclosure state and flow) |
| **Role** | consumer - the Settings page groups (`SettingsLayout.vb`); migration ticket SP-0169 |
| **Home** | shared contracts catalog, folder `collapsible-group/` |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Header** (section 3): a filled state triangle at the extreme reading start (right collapsed, down expanded;
  collapsed points left in a right-to-left layout), a header face distinct from the body, a bold caption, hit
  height 32 growing with a wrapped caption; the whole header toggles once, the body never does
  (`GroupHeader`, `SettingsGroup`). The triangle is control-owned vector geometry (ICON-RENDER 0.24 section 20).
- **Flow** (section 4): the body opens in normal flow under its header across the content width, groups open
  independently, values and focus survive, no handler runs on expansion; group ids, the remembered expansion and
  the anchor-plus-offset viewport are the stored `SettingsGroup_<id>` / `SettingsViewport` values, unchanged.
- **Design record:** the Settings page has four topics (Appearance, History, Startup, Windows), so groups are
  justified; compact packing of collapsed headers is **not** chosen (a few groups; one header per row). The shell
  rail's job groups are navigation parents, outside this contract (SETTINGS-NAVIGATION 2.4).

**Gates.** `--selftest` group `ui-contracts` (`group:*` - tokens, marker direction in LTR and RTL, header face,
weight and contrast, activation, accessibility state, collapsed body) and the existing `windows-ui:*`,
`rail-groups:*` rows.

**Status: header implemented, flow evidence owed (SP-0169 slice S1, 2026-10-21).** Owed: the per-job flag groups of
the Damaged page, scroll-anchoring evidence and the showcase captures (S2, S4).
