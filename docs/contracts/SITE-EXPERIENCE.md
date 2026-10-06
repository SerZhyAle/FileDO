# SITE-EXPERIENCE

| | |
| --- | --- |
| **Id** | `SITE-EXPERIENCE` |
| **Version** | 0.1 draft (opted in 2026-10-06; no wire carrier - a stylesheet layer, a set of components and a behaviour contract) |
| **Role** | consumer - the guide pages and the landing under `docs/` |
| **Home** | shared contracts catalog, folder `product-site/`, document `SITE-EXPERIENCE.md` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Build on the kit.** `docs/kit/sza-kit.css` is served byte-identical to the catalog copy and linked before
  any other stylesheet (`PAGE-STYLE` holds the vendoring). The guide layer `docs/guides/guide.css` takes kit
  tokens by reference.
- **Language and theme are one shared state.** `sza-lang` and `sza-theme` are read and written as `PAGE-STYLE`
  section 7 states, on the landing, the privacy page and every guide page. The language control is the one
  `PAGE-STYLE` section 4.2 specifies; the open hub proposal on a non-landing control form is cited by any
  exception.
- **Every third-party origin the pages contact is declared and named on the privacy page** (today Google Fonts
  on every page, `api.github.com` on the landing); no advertising or analytics origin.
- **A download or version is taken from the landing's release source at run time**, never retyped.
- **Reach**: keyboard order and a visible focus ring, a skip link, one `main` and one `h1` per page, a `lang`
  attribute equal to the page language, 4.5 : 1 text contrast in both themes, 44 px targets, an `alt` on every
  image, motion stopped under `prefers-reduced-motion`.
- **The portal-only rules (search, function-page components, drawer) bind only once the product publishes a
  portal**; until then they are not gaps, and the registry says so.
