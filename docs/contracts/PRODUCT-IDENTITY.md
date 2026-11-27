# PRODUCT-IDENTITY

| | |
| --- | --- |
| **Id** | `PRODUCT-IDENTITY` |
| **Version** | 0.8, read 2026-11-26 (FILEDO-0245; 0.6 read 2026-11-23 in FILEDO-0243); implemented partial 0.8, draft (carrier: `identity-manifest.json` `schemaVersion` 1) |
| **Role** | consumer - every picture of FileDO the user meets: the exes, the installer, the window and tray, the Start menu, the MSIX package and the Store listing, the site, the help pages, the READMEs and the share images |
| **Home** | shared contracts catalog, folder `product-identity/`, document `README.md`, with its reference checker |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **One master, defined as code** (rules 1 and 33). The mark is drawn once, as data, in
  `assets/make-product-icons.ps1`; the same run writes it as SVG in three declared forms under
  `assets/identity/master/` - full (`master.svg`), medium 33-63 px (`master.reduced[]`) and small 32 px and below
  (`master.small`). The SVGs are outputs and never edited by hand.
- **Every rendition generated and on record** (rules 2 and 10, section 5). The one generator command writes the ICO,
  the PNGs, the site and help copies, the share images, the 19 MSIX logos, the installer wizard art and
  `assets/identity/identity-manifest.json`, which lists each with its SHA-256. A byte copy kept for a surface that
  cannot name the first file is its own row (rule 36).
- **The store icon is the installed icon** (rule 4). `msix/build-msix.ps1` copies the package logos from
  `assets/identity/renditions/msix/` and draws none; the Store logo set (`msix/store-logos/`) is rendered from the
  master SVG by `msix/make-store-logos.ps1` at a release and listed with the hash of the drawing it was rendered
  from.
- **Composed share and Store graphics** (rules 6 and 10). The share card (`docs/assets/social-card.png`, 1200x630),
  the GitHub social preview (`assets/social-preview.png` and `docs/assets/social-preview.png`, 1280x640) and the Store
  poster and box art (`msix/make-store-logos.ps1`) compose the master and the canonical product name `FileDO` on
  `master.background` (`#0A100C`), with no gradient or extra tagline; live GitHub and Store uploads happen only at a
  release.
- **Help pages show the picture** (rule 8; `CONTEXT-HELP` 0.6 items Q and R): `HelpPages.vb` writes
  `docs/help/filedo-mark.svg` before every title and `docs/help/favicon.png` as the page icon, and an element's
  `ICON-SET` glyph before its name.
- **Diagrams and other images** (rules 12-16): no diagram is drawn as a picture; the READMEs' layout tree is text.
  The screenshots and the demo are captured from the real build by the product's own scripts.

**Gates.** `assets/make-product-icons.ps1 -Check` (rung 3: a rerun on the unchanged drawing writes the same bytes;
a retired second drawing fails) runs inside `packaging/check-internal-docs.ps1`, so `build.ps1 -Fast` and `-Test`
fail on a hand-edited or stale picture. The catalog's `reference/Test-ProductIdentity.ps1 -RepoRoot .` holds the
manifest (rungs 1 and 2), and `release.ps1` runs it with `-Release` in the pre-flight so a rendition whose
`renderedFrom` differs from `master.sha256` (a `NOTE` between releases) blocks a release before the tag (rule 10);
`packaging/check-help.ps1` holds the help generator's Q and R and the two files beside the pages.

**Status: partial 0.6, 2026-11-23 (FILEDO-0236, FILEDO-0240, FILEDO-0241, FILEDO-0243).** Rungs 1, 2 and 3 hold; rung 4
holds for every surface the build and the repository carry; the four 0.4 surface ids (`app.installer-banner`,
`app.installer-dialog`, `app.shell-entry`, `app.about`) are adopted (`PROPOSAL-2026-11-20-filedo-installer-art-and-captures.md`
accepted in 0.4; 0.5 `app.notification` is Android/Wear OS only and not applicable; 0.6 exempts `hub.card` from the
release gate); the share card and GitHub social preview are recomposed under rule 6 in the tree (the live GitHub
repository social preview updates at the release, rule 10), `msix/make-store-logos.ps1` composes the Store poster and
box art the same way, and the five landing pages carry `alt=""` on the identity mark beside the name (rule 11,
FILEDO-0241). Open: the Store logo set still shows the retired hand-made drawing (`renderedFrom`
`bd8b9889ac213901289c5fe00b9d5999fe4387457cc6b0294653455313326711`) until the next release re-renders it
(`assets/make-product-icons.ps1 -StoreLogos`, a `NOTE` between releases and a finding under `-Release` in the
pre-flight); the help pages carry the picture and the glyphs only after the release regenerates them
(`filedo_win.exe --build-help docs\help`); rung 5 (the live Store and site read by eye) has not been made.
