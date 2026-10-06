# SITE-REPRESENTATION

| | |
| --- | --- |
| **Id** | `SITE-REPRESENTATION` |
| **Version** | 0.1 draft (opted in 2026-10-06; no wire carrier - a content model: facts, their sources and the shape of a function page) |
| **Role** | consumer - the site, the READMEs and the listing sources, at the guide tier |
| **Home** | shared contracts catalog, folder `product-site/`, document `SITE-REPRESENTATION.md` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **One positioning source** that every external surface is written from, with the pillars in its order:
  `packaging/positioning-source.json` (five pillars: check, tidy and move, erase, protect, virtual disks).
  It also declares, per surface, the region that lists the pillars - the landing and its de/fr pages, the
  guides hub, the five READMEs, the Store listing source, the winget manifest. `packaging/check-external-docs.ps1`
  (step `positioning`) fails when a region does not name the pillars it owes or names them out of order. No
  surface is written from another; a change of order is made in the source first. The guides hub has no
  guide for the erase pillar yet, so it is the one declared omission (`allowMissing`).
- **A fact is typed once.** The release version and channels come from the release source the landing reads;
  the public editions are one product (one release, one tag, a companion binary - not an edition), declared
  as such; a count or a name typed by hand on a page is compared with its source by a gate or is a gap.
- **Guide pages name the interface in the interface's words** (`docs/termbase.json` is checked by
  `packaging/check-external-docs.ps1`), with a limitation stated next to the thing it limits.
- **The page set says only what shipped**; a planned capability is labelled planned.
- **The privacy page, the install-trust page, the Store data declaration and the README say one thing**;
  `DISK-SHARE` and `INSTALL-TRUST` cover the parts that are theirs.
- **Function-page rules (6 to 10) bind where the site publishes function pages**; FileDO publishes task guides,
  so they apply to each guide section as a function owner and the registry records the verdict per group.
