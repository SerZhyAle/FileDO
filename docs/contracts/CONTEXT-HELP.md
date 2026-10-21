# CONTEXT-HELP

| | |
| --- | --- |
| **Id** | `CONTEXT-HELP` |
| **Version** | 0.1, read 2026-10-21 (SP-0170); implemented partial 0.1; SP-0168 owns the rest, draft (no wire - a key, a button, a help map file and a page anatomy) |
| **Role** | consumer - the GUI shell of `filedo_win.exe` (`filedo_win_vb/`), opted in by the portfolio owner's request of 2026-10-16 (SP-0168) |
| **Home** | shared contracts catalog, folder `in-app-help/`, document `README.md`, with its three reference artifacts |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **F1 and the `?` button give one answer** (rules 1-3). `Help.vb` is the one resolver: a surface id and an
  optional element in, a help page and an anchor out. The shell's F1 is handled at window level in
  `ShellForm.ProcessCmdKey` and never registered system-wide; the button is the `app.help` glyph, the
  trailing control of the first content row of the main window (`headerRow`), named "Help (F1)" in the UI
  language. The Disk Manager's F1 and its help button both call `OpenHelpPage`.
- **A map with a row for every surface** (rules 4 and 17). `docs/help/help-map.json` ships beside the exe
  (`help\help-map.json`); one row per rail destination and per window class, each with its form and, for
  `inherit` and `none`, its reason. The `help:` rows of `filedo_win.exe --selftest` enumerate the surfaces from
  `RailRow.All` and the `Form` classes of the assembly and diff them against the map.
- **A page per surface, from one source** (rules 5-9 and 13). `HelpContent.vb` lists the elements of each page,
  `HelpStrings*.vb` carries the words in the five shipped languages next to the UI's own strings, and
  `filedo_win.exe --build-help <folder>` writes `docs/help/<language>/<page>.html`. A page is a render target
  and is never edited by hand.
- **Screenshots from the build** (rule 14). The same command takes each picture off screen from the real
  window, in both themes, and reads the position of every numbered marker from the control tree
  (`HelpCapture.vb`); demo content only.
- **Offline, private, permanent** (rules 12 and 15). The pages are shipped beside the exe in every package,
  make no request, and open in the system browser only on a click or a key. A page that is not written yet
  answers with the page its map row names as fallback, and the debt is listed in
  `docs/help/help-baseline.txt`, which may only fall.

**Gates.** `filedo_win.exe --selftest` - the `help:` rows (map against surfaces, baseline, content, resolver,
key and button agreement, anatomy of every page in every language, text style);
`packaging/check-help.ps1` - the generated set on disk (pages in every language, links, no request,
baseline); both are steps of `build.ps1 -Test`.

**Status: partial 0.1, 2026-10-21 (SP-0168 slice S1).** Rung 1 holds for the main window's Command, History,
Settings and About pages, the shared job page and the Disk Manager window; the other 32 job pages and five
Disk Manager dialogs have map rows and fall back, and their key and button are not wired (baseline). Rung 2
(freshness) and rung 3 (the walk of every surface in every language and theme, by a person or UI
automation) are open. The Disk Manager's `?` is the trailing end of its toolbar and wraps with the toolbar in
a narrow window; its older menu (first steps, the help dialog, the links) is on the button's right click -
the owner's decision O3 is open.
