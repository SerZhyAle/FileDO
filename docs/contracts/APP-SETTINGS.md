# APP-SETTINGS

| | |
| --- | --- |
| **Id** | `APP-SETTINGS` |
| **Version** | 0.2, draft (no wire - a shipped user-facing surface) |
| **Role** | consumer - the settings page of the shell (`SettingsView.vb`, `SettingsPanel.vb`, `SettingsLayout.vb`) and the Disk Manager's settings dialog that hosts the same panel |
| **Home** | shared contracts catalog, folder `desktop-app-ux/`, document `APP-SETTINGS.md` |
| **Owner** | CyrFlip. An amendment is proposed in the catalog, not decided here |

FileDO's 2026-10-02 read of 0.1 recorded the draft as not binding because the shell had no persistent
settings surface of the kind the contract describes. The portfolio owner's 2026-10-05 decision adds
the WINDOWS-UI profile with APP-SETTINGS 0.2 section 8, and SP-0150 rebuilt the settings page to it:
the row below replaces that non-adoption record.

## What this repository must do to stay conformant

- **One instance, found where the user is** (rule 1). Settings is a page of the shell's rail; the Disk
  Manager's settings button and the tray's Settings entry route to the same existing window
  (`AppHost.ShowSettings`), never to a second instance; the isolated dialog remains for a manager
  started without the shell.
- **Vertical page list, About last** (rule 2). The rail; every destination whole in the longest shipped
  language (the rail's own measurement rows).
- **Page anatomy** (rule 3). A description first, then settings in independent groups; every setting has
  a caption, an editor and a muted hint (`SettingRow`), and the hint is `text.muted`.
- **Commit model** (rule 4). The touch-commit surface: reversible values apply when touched
  (theme, language, history, minimize, notification); the one action with external effects - the
  Windows startup choice - has its own explicit Apply button; resetting window positions is its own
  button too. Nothing irreversible rides on a control that fires on touch.
- **Language and theme selectors** (rules 5-6). Both on the first page's Appearance group; languages by
  endonym, applied live to every open window (`LiveLanguage`); System/Light/Dark applied without a
  restart (`ShellSettings.SetThemeChoice`); while Windows is in a high-contrast theme the surface follows
  the system colours whatever the chosen mode (`Theme.SystemContrastPalette`).
- **Scaling and compactness** (rules 7-8). PerMonitorV2, measurements from the font (`Ui.Px`), the
  surface sized to its content and scrolled when a page outgrows it.
- **Cooperate with the OS** (rule 9). Escape leaves the settings page with nothing changed; the
  packaged build's startup and AutoPlay rows deep-link into Windows' own settings (`ms-settings:`).
- **Accessibility and inventory** (rules 10-11). A sensible tab order; the shared row sets the editor's
  accessible name and description; every settings caption is inventoried in the five localization
  tables the `settings:locale:` rows hold.
- **About** (rule 12). The rail's last page: version, licence, the support bundle the user exports on
  a click, and the privacy link.

**Gates.** `filedo_win.exe --selftest`: `settings:locale:` rows (all five languages carry every
settings key), `windows-ui:` rows (anatomy, context, live language, watch, high contrast),
`rail-groups:` rows. `filedo_win.exe --ui-drive`: the driven acceptance of the settings page
(SP-0150).

**Status: implemented 2026-10-05 (SP-0150); read against 0.2, sections 2 and 8.** The registry carries
the row. Physically unverified: the high-contrast and multi-DPI runs named beside SP-0150.
