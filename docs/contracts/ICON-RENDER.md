# ICON-RENDER

| | |
| --- | --- |
| **Id** | `ICON-RENDER` |
| **Version** | 0.11 draft (no wire carrier - a picture, not a payload) |
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

**Status: partially adopted.** Held: two themes plus follow-system, no product themes; the verdict glyph
in the state colour with the verdict word as its accessible name; the group chevron at the 16 px tier;
the site's glyph as a single-colour inline SVG painting with `currentColor`. Open, each a ticket of
SP-0016 or a question to the owner: the glyph family (the shell draws Segoe Fluent Icons, an outline
family, where rule 1 names Material filled), and whether the `state.*` hues bind by exact tone. The rail's
group headers and rows now use the 44 px target; the MSIX build generates the `targetsize-*` and unplated
variants and indexes them in `resources.pri`, while preserving the five manifest resource languages.
