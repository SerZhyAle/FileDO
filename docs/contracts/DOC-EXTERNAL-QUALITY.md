# DOC-EXTERNAL-QUALITY

| | |
| --- | --- |
| **Id** | `DOC-EXTERNAL-QUALITY` |
| **Version** | 0.9 draft (no wire carrier - a documentation corpus and its web rendering) |
| **Role** | consumer - the published site under `docs/` (landing, privacy, `guides/`) and the five root READMEs |
| **Home** | shared contracts catalog, folder `documentation-quality/` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- Keep the guides task-oriented, reachable from the hub, and every public page in `sitemap.xml`.
- Land a change in every authored locale in one edit; keep product terms consistent per locale.
- Give every public page the full SEO block and every multi-step guide current screenshots.
- Keep zero broken links and anchors across the site and the READMEs.
- Requires `DOC-INTERNAL-QUALITY` first (contract section 4).

**Status: adopted 2026-09-26, partially compliant.** Held: task guides with a hub, sitemap complete, 0
broken links, claim tokens pinned across locales by `TestSurfaces_*` in `cmd/filedo/fdsec_surfaces_test.go`.
Open: glossary and subject index, mechanical sitemap and freshness tracking, termbase, the SEO block,
screenshots, a link gate, and the dashes in `guides/install-trust.html` - spec SP-0062.
