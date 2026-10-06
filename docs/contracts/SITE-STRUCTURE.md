# SITE-STRUCTURE

| | |
| --- | --- |
| **Id** | `SITE-STRUCTURE` |
| **Version** | 0.1 draft (opted in 2026-10-06; no wire carrier - a page set, a navigation shape and an address scheme) |
| **Role** | consumer - the published site under `docs/`, declared at the **guide** tier |
| **Home** | shared contracts catalog, folder `product-site/`, document `SITE-STRUCTURE.md` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Stay at the guide tier or publish what a higher one owes.** A landing, the privacy page, the install-trust
  page, guide pages by task with a hub, the subject index, the glossary, the sitemap. Never claim a tier lower
  than the pages published; a portal-only type (search, function pages, features showcase, a not-found page) is
  a dated exception in the registry until it exists.
- **Keep every public page reachable from the landing by links alone, in three steps or fewer**, and keep
  `sitemap.xml` and the page set equal both ways.
- **An address is permanent.** A page that moves leaves a forwarder; the redirect stubs `docs/{ru,ua,de,fr}/index.html`
  are such forwarders. The list of addresses held outside the site (README links, Store listing, in-app help,
  winget, the sibling sites) is owned by this repository: `packaging/site-held-addresses.json`, one file a
  program reads, resolved against the page set by `packaging/check-external-docs.ps1` (step `addresses`).
- **A language control offers only languages the page exists in**, and no link or control leads to an address
  that does not answer. The declared locale sets per page group are in the registry adoption row: ru, en, ua
  for every group, de and fr declared for every group and published as standalone pages (SP-0154, 2026-10-06).
- **A function ships with its page**: a user-visible capability is covered by a guide section before the
  release that carries it (`docs/guides/`; `TestSurfaces_*` hold the guide completeness).
- **Gates that already hold part of this**: `packaging/check-external-docs.ps1` (sitemap, locales, SEO, held addresses),
  `packaging/check-internal-docs.ps1` (links), `TestSurfaces_*` in `cmd/filedo/fdsec_surfaces_test.go`. The rest
  is read off the rendered site by hand.
