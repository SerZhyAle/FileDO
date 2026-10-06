# APP-STYLE

| | |
| --- | --- |
| **Id** | `APP-STYLE` |
| **Version** | 0.12, draft (no wire - a palette role vocabulary and a theme mechanism) |
| **Role** | consumer - the GUI shell of `filedo_win.exe` (`filedo_win_vb/Theme.vb`, `Chrome.vb`) |
| **Home** | shared contracts catalog, folder `desktop-app-ux/`, document `APP-STYLE.md` |
| **Owner** | StreamsPlayer. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Three themes, no restart** (section 2). Follow Windows (the default), light, dark - applied live, and
  following `WM_SETTINGCHANGE` while Follow Windows is chosen.
- **One palette table, only dynamic references** (section 3). `Theme.vb` is the only file that names or
  mixes a colour; every view re-reads the palette in its `ApplyTheme`, verdict colours included, so a
  theme switch with a result on screen repaints it.
- **The role vocabulary** (section 4). FileDO's spelling: Background = `surface.window`, Surface =
  `surface.raised`, SurfaceAlt = `surface.sunken`, Border = `border`, Text = `text.primary`, MutedText =
  `text.muted`, TextDisabled = `text.disabled`, Link = `link`, ControlHover = `control.hover`,
  SurfaceSelected = `surface.selected`, Accent = `accent`, AccentText = `accent.ink`; Success, Warning and
  Danger are the proposed `success`, `warning`, `danger`. The map is in `Theme.vb`'s header.
- **Out-of-theme surfaces declare themselves** (section 5). The native surfaces WinForms cannot recolour -
  scroll bars, `ProgressBar`, a combo box's drop-down, the common file and folder dialogs, tooltips - are
  listed with the reason in `Theme.vb`'s header. FileDO's own questions are themed `ShellDialog`s, not
  system message boxes.

**Gates.** `filedo_win.exe --selftest` - both palettes set every role (rung 2), a Failed result follows a
palette switch on both run pages (the round trip that stands in for rung 1, since WinForms has no markup),
every rail row painted in every state under both palettes without an exception. `cmd/filedo/
shell_contract_test.go` - no colour named or mixed outside `Theme.vb`. Rung 3, the light and dark
screenshot pair, is `msix\make-screenshots.ps1 -Theme light` and `-Theme dark`.

**Status: implemented; 0.11 read and applied 2026-10-02 (SP-0122); 0.12 read and applied 2026-10-05 (SP-0150).**

**0.12 (section 10, complete live theme coverage):** every open window and nested child follows a switch through one
notification (`Theme.Watch`) - the shell, the Disk Manager, every `DiskSmallDialog` / `DiskPageDialog` / `ShellDialog` /
shortcut dialog, the tray menu, and the native combo editors (`NativeEditors`); a hidden or reused window repaints before it
is shown again, and the `theme:` and `windows-ui:watch` self-test rows and the driven `--ui-drive` run hold it. High contrast
takes precedence over the chosen mode with the system's own colours (`Theme.SystemContrastPalette`, WINDOWS-UI section 6.5).
The rail's per-destination identity tones are the navigation identity inks of ICON-RENDER section 11: declared in `Theme.vb`'s
one table with light and dark values, gated by the `contrast:` rows, and folded into text ink under high contrast. The `danger`, `warning` and `success` roles are [CONTRACT]: the acting button of a destructive confirmation takes `danger` while the safe answer holds the default focus (`ShellDialog.vb`, `dialog:` self-test rows). The live OS signal binds, which `WM_SETTINGCHANGE` under Follow Windows already is.

**Palette values** default to the shared ones, and `Theme.vb` deviates only where measured contrast forces it: the light theme's muted, danger, success and warning-ink values are darker than the kit's so that text reaches 4.5:1 and glyphs 3:1 on FileDO's tinted surfaces. Each deviation is a dated exception in the catalog registry; none is a local taste choice. The contrast of every role against every surface it is drawn on is measured by the self-test's `contrast:` rows, not claimed.
