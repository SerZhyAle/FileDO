# DOC-EXTERNAL-QUALITY

| | |
| --- | --- |
| **Id** | `DOC-EXTERNAL-QUALITY` |
| **Version** | 0.9 draft (no wire carrier - a documentation corpus and its web rendering) |
| **Role** | consumer - the published site under `docs/` (landing, privacy, `guides/`) and the five root READMEs |
| **Home** | shared contracts catalog, folder `documentation-quality/` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- Keep the guides task-oriented, reachable from the hub, with the glossary and the subject index, and every
  public page in `sitemap.xml`.
- Land a change in every authored locale in one edit; keep product terms consistent per locale.
- Give every public page the full SEO block and every multi-step guide current screenshots.
- Keep zero broken links and anchors across the site and the READMEs.
- Requires `DOC-INTERNAL-QUALITY` first (contract section 4).

## Gates

| Rule | Held by | Command |
| --- | --- | --- |
| 1 portal, subject index, glossary | `check-external-docs.ps1` structure; `TestSurfaces_TheGuideHubLeadsToTheGlossaryAndSubjectIndex` | `pwsh -NoProfile -File packaging/check-external-docs.ps1` |
| 2 sitemap against the page set | `check-external-docs.ps1` sitemap | same |
| 3 every locale present, translation freshness | `check-external-docs.ps1` locales + `docs/translation-fingerprints.json` | same; `-Record` after the translations were read |
| 4 termbase | `check-external-docs.ps1` terms + `docs/termbase.json` | same |
| 5 SEO block | `check-external-docs.ps1` seo | same |
| 6 screenshots | `check-external-docs.ps1` screens; pictures from `packaging/capture-guide-screens.ps1` | same |
| 7 links, anchors, https, prose hygiene | `check-internal-docs.ps1` links; `check-external-docs.ps1` hygiene | `pwsh -NoProfile -File packaging/check-internal-docs.ps1` |

Both scripts are steps 8 and 9 of `build.ps1 -Test`, exit 0/1/2 (`CHECK-VERDICT`).

**Status: adopted 2026-09-26; all seven rules gated 2026-09-26 by SP-0062.** Two points are put to the owner
rather than decided here - `hreflang` on a single-URL runtime-localized site (FileDO emits `x-default` only)
and the search index on a small corpus (FileDO has a gated subject index instead): catalog
`documentation-quality/PROPOSAL-2026-09-26-single-url-locales-and-search-index.md`.
