# SITE-EXPERIENCE

| | |
| --- | --- |
| **Id** | `SITE-EXPERIENCE` |
| **Version** | 0.5, read 2026-10-29 (SP-0174); implemented partial 0.5; release-note dates (SP-0187) are open; rule 6 control glyphs held since 2026-10-29 (SP-0186), draft (opted in 2026-10-06; no wire carrier - a stylesheet layer, a set of components and a behaviour contract) |
| **Role** | consumer - the guide pages and the landing under `docs/` |
| **Home** | shared contracts catalog, folder `product-site/`, document `SITE-EXPERIENCE.md` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Build on the kit.** `docs/kit/sza-kit.css` is served byte-identical to the catalog copy and linked before
  any other stylesheet (`PAGE-STYLE` holds the vendoring). The guide layer `docs/guides/guide.css` takes kit
  tokens by reference.
- **Language and theme are one shared state.** `sza-lang` and `sza-theme` are read and written as `PAGE-STYLE`
  section 7 states, on the landing, the privacy page and every guide page. The language control is the one
  `PAGE-STYLE` section 4.2 specifies; a picker is a conforming form too (0.3, rule 10).
- **Every third-party origin the pages contact is declared and named on the privacy page** (today Google Fonts
  on every page, `api.github.com` on the landing); no advertising or analytics origin.
- **A download or version is taken from the landing's release source at run time**, never retyped.
- **Reach**: keyboard order and a visible focus ring, a skip link, one `main` and one `h1` per page, a `lang`
  attribute equal to the page language, 4.5 : 1 text contrast in both themes, 44 px targets, an `alt` on every
  image, motion stopped under `prefers-reduced-motion`.
- **0.2 and 0.3, read 2026-10-07 (SP-0165).** Rule 10 holds with the segmented control (RU EN UA) plus a de and fr text row; 0.3 changes nothing here. Rule 20 (every page fills the screen) is pinned by `TestSurfaces_ThePagesUseTheFullWidth` and has not been measured at 1920 and 2560 px. Rule 1 is the kit exception recorded under `PAGE-STYLE`.
- **A shell control named on a page shows the shell's glyph (rule 6, SP-0186).** The mention is
  `<svg class="cg" aria-hidden="true" focusable="false"><use href="#g-<id>"/></svg>` before the name; each page's sprite is
  written from `assets/glyphs/` by `packaging/sync-guide-glyphs.ps1` (`-Check` fails on drift), never by hand. No glyph for a
  control the shell draws without a vocabulary glyph, nor for a Windows or third-party control.
- **The portal-only rules (search, function-page components, drawer) bind only once the product publishes a
  portal**; until then they are not gaps, and the registry says so.
