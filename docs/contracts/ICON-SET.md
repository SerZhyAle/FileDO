# ICON-SET

| | |
| --- | --- |
| **Id** | `ICON-SET` |
| **Version** | 0.13 draft (no wire carrier - the vocabulary is read by people, never by a shipped product) |
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

**Status: partially adopted.** Held: the verdict glyph of the job and Command pages (`status.ok`,
`status.error`, one mapping in `Theme.vb`, asserted by `--selftest`), the rail group chevrons
(`nav.expand` / `nav.collapse`), the rail label of the clean job, the site's back-to-top control
(`nav.scroll-top`) and copy buttons, and emoji-free READMEs. Open: the rail glyphs that draw another
meaning's picture or have no id, the Stopped and Not proven verdicts, and the Explorer sub-verbs - each
waits for an id or a decision proposed in the catalog. The full inventory and the tickets are SP-0016.
