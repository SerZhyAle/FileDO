# ICON-RENDER

| | |
| --- | --- |
| **Id** | `ICON-RENDER` |
| **Version** | 0.15 draft (no wire carrier - a picture, not a payload) |
| **Role** | consumer - the shell draws glyphs, the site draws one, the MSIX and MSI carry the product mark |
| **Home** | shared contracts catalog, folder `iconography/` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Colour comes from the role.** A control glyph paints in the content colour; a state glyph
  (`status.*`) paints in the state's colour, as `Theme.VerdictColor` does on both verdict pages.
- **Every theme, 3:1.** Each glyph keeps a 3:1 contrast against its background in light, dark and
  follow-system.
- **A toggle shows its state** with the right pair of glyphs.
- **Size tiers and targets.** Glyphs are drawn at a declared tier (16 px and up), and a touch or click
  target is at least the platform's minimum.
- **Every glyph has an accessible name**, or is marked decorative when a word beside it carries the
  meaning.
- **The product mark on system surfaces** follows the platform's shape rules.
- **One look per surface**, and the shared `state.*` hues from the catalog's palette.

**Status: adopted, draft (0.13, re-checked at 1.0).** Held: two themes plus follow-system, no product
themes; every vocabulary glyph is the catalog's Material filled drawing on the 24 grid, filled as a vector
path (`filedo_win_vb/Glyphs.vb`, `SvgPath.vb`) in the colour of its role, at the 20 px tier on the rail and
its group chevrons and 24 px beside the job page's verdict badge; the verdict glyph, the Command page's
verdict line and the verdict badge take the shared `state.*` tones exactly (`Theme.vb` `StateOk`,
`StateWarning`, `StateError`), held to the vendored `palette.json` by the self-test's `state-tone:` rows;
every glyph's contrast against every surface it is drawn on is measured by the `contrast:` rows (3:1 for a
glyph, 4.5:1 for the verdict word and badge) in both palettes; the verdict glyph is decoration beside the
word that names the verdict; the rail's 44 px targets; the site's glyph as a single-colour inline SVG
painting with `currentColor`; the MSIX `targetsize-*` and unplated variants in `resources.pri`, with the
five manifest resource languages preserved; the Explorer surfaces of rule 9 as 0.12 names them - each
entry of the `File DO..` group and the `.fd-sec` document type in the mono look in the one menu tone
`#808080` (`Theme.MenuIconTone`) at 16 to 256 px, the MSIX file type's logo cut from the same icon, the
group with the product mark. Open: the MSIX WACK run and the on-screen screenshots are owed.

**0.14 and 0.15, applied 2026-10-02 (SP-0122).**
- **`state.warning` day tone is `#EF6C00`** (3.08:1 on white). The shell's light `StateWarning` is that tone, held to the vendored `palette.json` by the `state-tone:state.warning:day` row; the former day-exception row and its registry exception are retired. The tone is drawn as a glyph only where it reaches 3:1 (the verdict glyph on a card, a Disk Manager row on its plain background, the help legend); on the hover and selected row backgrounds, and for the verdict word and badge (4.5:1 needed), the shell uses its darker warning ink `#7E560F`. That fallback is a dated exception in the catalog registry, not a local choice.
- **Hit-target floor on Windows: 28 px under a mouse or a pen, 44 px under touch or when the input is unknown**, in logical pixels. The rail's rows are 44 px; `GlyphButton` now has a 28 px floor. Open: the shell cannot tell the input in use, so its glyph buttons (36 px toolbar buttons, 28 to 32 px inline ones) fall under the 44 px that rule requires; recorded as a dated exception. Owner decision 2026-10-02: Windows is pointer-first for this product and compactness outranks a touch floor, so the amendment is asked for in the catalog (a declared pointer-first profile keeps 28 px); the exception stands until the catalog decides.
