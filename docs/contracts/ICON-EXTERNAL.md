# ICON-EXTERNAL

| | |
| --- | --- |
| **Id** | `ICON-EXTERNAL` |
| **Version** | 0.10 draft (no wire carrier) |
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

**Status: adopted (rules 1 and 5).** Every vocabulary glyph the shell draws is a catalog file vendored
under `assets/glyphs/` with its SHA-256 in `PROVENANCE.txt`, and its Material Icons (Apache-2.0) notice is
in `THIRD-PARTY-NOTICES.txt`, which ships in every package; the Explorer icons in `assets/menu-icons/`
are drawn from those same files. No Segoe stand-in is left; one a future control needs would name its
codepoint and the font's own name for it (`GlyphRef.Waiting`).
