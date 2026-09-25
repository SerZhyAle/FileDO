# ICON-SET

| | |
| --- | --- |
| **Id** | `ICON-SET` |
| **Version** | 0.15 draft (no wire carrier - the vocabulary is read by people, never by a shipped product). Adopted as a draft on 2026-09-25; the adoption is re-checked when the contract reaches 1.0 |
| **Role** | consumer - the shell, the site, the READMEs and the Explorer integration show glyphs |
| **Home** | shared contracts catalog, folder `iconography/` |
| **Owner** | FastMediaSorter Android. A new meaning or a new language is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **One meaning, one glyph.** A control that shows a picture shows the glyph of the meaning it performs,
  never the glyph of another meaning and never a picture with no id in the vocabulary.
- **Keep the `distinct` sets apart.** In particular: a plain check mark is `action.confirm`, not
  `status.ok`; a chevron pointing right is `nav.go-to`, not `nav.expand`.
- **One name per meaning** in every surface and every locale: the label, the tooltip, the accessible name,
  the manual and the web page use the meaning's `name`, qualified if needed, never another meaning's word.
- **A knowingly shared shape is declared** on both sides, with the reason.
- **Product artwork is not a glyph**: the product mark stands for the product, never for an action or a
  file type.
- **Documentation shows the glyph or nothing** - no emoji standing in for a meaning.
- A meaning FileDO needs and the vocabulary lacks is proposed in the catalog first; the code waits for the
  id.

## How the drawings get here

The shell draws the catalog's own glyph files, not a copy made by hand. `assets/sync-icon-glyphs.ps1`
imports the declared subset into `assets/glyphs/` beside a `PROVENANCE.txt` holding the catalog versions,
the vocabulary's SHA-256 and the SHA-256 of every file; `filedo_win.exe` embeds that folder and refuses to
draw a file whose hash is not its line, or a set mapped against another `ICON-SET` MAJOR. Never edit the
folder by hand, and keep `.gitattributes`' `-text` on it - a line-ending conversion changes every hash.

`pwsh -NoProfile -File assets/sync-icon-glyphs.ps1 -Check` compares the vendored files with the catalog
itself (exit 0 in sync, 1 drift, 2 catalog unreachable) and reports a meaning a control waits for
(`GlyphRef.Waiting`) once the catalog has its glyph. After an import that changes a drawing the Explorer
icons use, run `filedo_win.exe --write-menu-icons assets\menu-icons` and rebuild; the self-test fails
until then. The catalog root comes from `-CatalogRoot` or `SZA_CONTRACTS_ROOT`. Run it
after any change in the catalog's `iconography/` folder and before mapping a new meaning; the import is the
same command without `-Check`.

**Status: adopted, draft.** Held: every glyph FileDO draws is a vocabulary drawing - the rail's glyph map
in `Rail.vb`, the chevrons and the five verdicts in `Theme.vb`, and the Explorer surfaces - and the
self-test's `icons:`, `rail-glyph:` (stand-ins held at zero), `glyph:` and `menu-icon:` rows hold it. The
fifteen meanings FileDO proposed entered the vocabulary in 0.14 (`feature.capacity-test`,
`feature.speed-test`, `action.verify`, `feature.raw-probe`, `action.recover-drive`,
`action.find-duplicates`, `action.compare`, `action.fill-space`, `action.wipe`, `action.secure`,
`action.unsecure`, `content.secret-file`, `app.command-line`, `status.stopped`, `status.not-proven`), with
`de` and `fr` names for every meaning FileDO ships. Each entry of the `File DO..` Explorer group shows its
meaning's icon and the `.fd-sec` document type `content.secret-file`: `icons\<id>.ico` beside the exe,
drawn by `filedo_win.exe --write-menu-icons assets\menu-icons` from the same vendored files, named alike
by all three writers (`packaging/wix/FileDO.wxs`, `fdsec register`, `shellext/FileDOShell.cpp`); the group
keeps the product mark. Labels use the meanings' names (German clean job "löschen", English "Secure a
file" / "Unsecure a file"); "About" and the verdict words are the qualified forms the 0.14 notes declare.
The site's back-to-top control and copy buttons; emoji-free READMEs. The full inventory is SP-0016.
