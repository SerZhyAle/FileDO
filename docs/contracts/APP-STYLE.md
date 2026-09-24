# APP-STYLE

| | |
| --- | --- |
| **Id** | `APP-STYLE` |
| **Version** | 0.10, draft (no wire - a palette role vocabulary and a theme mechanism) |
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

**Status: implemented.** FileDO has no adoption row in the catalog registry yet. The contrast claim of `Theme.vb` (4.5:1 for text) is written down, not measured -
section 8 leaves contrast undecided.
