# ICON-EXTERNAL

| | |
| --- | --- |
| **Id** | `ICON-EXTERNAL` |
| **Version** | 0.9 draft (no wire carrier) |
| **Role** | consumer - the site and the READMEs name third-party services |
| **Home** | shared contracts catalog, folder `iconography/` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Third-party marks.** GitHub, the Microsoft Store and winget stay text links; README badges carry no
  third-party logo. A mark, if one is ever shown, follows its owner's usage rules.
- **Other apps' icons and downloaded pictures** - FileDO shows neither today. A feature that starts to
  show one reads rules 2-4 first.
- **Source on record.** Every glyph the product draws names its source: the font or the vendored file,
  and the licence notice in `THIRD-PARTY-NOTICES.txt` when the source requires one.

**Status: adopted for rule 1, partial for rule 5.** The shell names its glyph font in `Theme.vb`; the
verdict codepoints name their font glyph in a comment. The rail codepoints do not yet name theirs - that
lands with the rail's mapping table (SP-0016 T1).
