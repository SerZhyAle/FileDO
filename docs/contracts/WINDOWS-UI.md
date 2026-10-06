# WINDOWS-UI

| | |
| --- | --- |
| **Id** | `WINDOWS-UI` |
| **Version** | 0.1, draft (no wire - observable Windows UI and UX) |
| **Role** | consumer - the GUI shell of `filedo_win.exe` (`filedo_win_vb/`), opted in by the portfolio decision of 2026-10-05 |
| **Home** | shared contracts catalog, folder `desktop-app-ux/`, document `WINDOWS-UI.md` |
| **Owner** | Fast Media Sorter & Sharing for Windows (portfolio owner decision). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Window anatomy and navigation** (section 2). Native window management only; the settings page has no
  second close cross beside the native one. The rail is the vertical navigation list: one glyph and a
  localized caption per destination, About last, hover/focus/selection/unavailable distinguishable,
  never by colour alone.
- **Independent collapsible groups and remembered context** (section 3). Groups never close one another;
  collapse changes visibility only, hands focus to the header when it held a child, and hidden children
  leave keyboard traversal. Last page, each group's expansion and each page's viewport are remembered
  under stable internal IDs with an anchor plus logical offset; restoring them runs no change handler.
  Explicit navigation reveals the setting's own group.
- **Setting rows and editors** (section 4). Caption and muted hint in the leading column, the editor in
  the trailing column of the same row; booleans keep the check box at the reading start with the
  description under the caption. `SettingRow` / `SettingsGroup` (`SettingsLayout.vb`) are the shared row
  and group components; `Ui.HitTargetFloor` and `NativeEditors` carry the target floors and the native
  editor theming. FileDO instantiates no native numeric spinner: its numbers are validated textual CLI
  arguments, so the numeric-editor rule binds the day one is added, not as a feature change.
- **Typography, geometry and DPI** (section 5). PerMonitorV2; dimensions defined once at 96 DPI
  (`Ui.Px`); long pages scroll; switching a page or folding a group never resizes the window. **Show
  cost** (proposed 2026-10-06 as `PROPOSAL-2026-10-06-show-layout-once`, not yet folded into the contract):
  a page or window is shown with its subtree's layout held and laid out once (`Ui.SuspendTree` /
  `ResumeTree`: `ShellForm.RailEntry_Click`, `SettingsPanel`, `DiskSettingsDialog`), and a remembered
  scroll position is restored after that layout, not from a hidden page's geometry.
- **Palette and contrast** (section 6). System/light/dark through APP-STYLE's table; every open window
  and nested child follows a switch (`Theme.Watch`), including hidden and reused dialogs before they are
  shown again; high contrast overrides the chosen mode with the system's own colours
  (`Theme.SystemContrastPalette`).
- **Secondary windows and feedback** (section 7). The same conventions in every dialog; a settings
  action with external effects has its own explicit button (the startup choice's Apply, AutoPlay's
  Disable); errors are actions, never raw exceptions.
- **Working content viewer** (section 8). Not applicable: FileDO has no media canvas, floating chrome or
  transport; the Disk Manager is a fixed-layout working window, not a content viewer.

**Gates.** `filedo_win.exe --selftest` - the `windows-ui:` rows (anatomy of every settings row, stable
IDs and viewport fallbacks, live-language rebind that touches no user value, watched hidden window
following a palette switch, high-contrast palette pinned to the system colours), the `rail-groups:`
rows (independent folding, focus handoff), the `hit-target:` rows and the `contrast:` rows.
`filedo_win.exe --ui-drive <folder>` - the driven visual acceptance (SP-0150): real windows on the
screen, light -> dark -> light on the live window and on an open dialog, keyboard activation of a
group header, Escape leaving the settings page, the real close path and a fresh reopen restoring the
page, the groups and the viewport, every step screened by its dominant colour.

**Status: implemented 2026-10-05 (SP-0150); read against 0.1.** Physically unverified and open:
150%/200% and mixed-monitor movement on real displays (P01), a real high-contrast session (the palette
and its precedence are pinned mechanically), and a right-to-left locale (the five shipped languages are
left-to-right; direction is declared with the inventory and no RTL language ships). The driven
acceptance and the evidence folder are recorded beside SP-0150.
