# docs/ - index of this tree

This tree is the published site and nothing else. The working documents that precede a feature -
specs, plans, questionnaires, research notes - are not here; see "What does not live here" below.

## The published site (GitHub Pages serves this tree)

Canonical pages, all hand-authored - there is no generator, so these are **not** render targets and are
edited in place:

| Page | What it is |
| --- | --- |
| [`index.html`](index.html) | Product landing page. Switches language at runtime from `sza-lang`. |
| [`guides/index.html`](guides/index.html) | Guide hub - the safe starting point chooser. |
| [`guides/check-storage.html`](guides/check-storage.html) | Verifying real capacity and speed of a drive. |
| [`guides/files-and-copies.html`](guides/files-and-copies.html) | Duplicates, copies and folder differences. |
| [`guides/gui-command-builder.html`](guides/gui-command-builder.html) | Building a command in the VB.NET GUI. |
| [`privacy.html`](privacy.html) | Privacy policy - no network, no telemetry, no accounts. |

Supporting files: [`robots.txt`](robots.txt), [`sitemap.xml`](sitemap.xml), [`assets/`](assets/),
[`kit/sza-kit.css`](kit/sza-kit.css), `_config.yml`, `.nojekyll`.

`ru/`, `ua/`, `de/` and `fr/` hold **redirect stubs only**. Each sets `sza-lang` and bounces to the site
root, which is where the translated text actually lives - so they carry a canonical link to the root and
are deliberately absent from `sitemap.xml`.

## What does not live here

Specs, implementation plans, questionnaires and research write-ups are working documents, not product
documentation. They live in `.specs/` at the repository root, which `.gitignore` keeps out of git. What
the repository carries is documentation of the finished product, written for an experienced user - the
pages above, the READMEs, and the listing sources.

## Editing rules

A user-visible change lands in **every** surface in one edit - the landing page, the guides, the READMEs
and the listing sources - per canon `DOCUMENTATION_CONCEPT.md` 5. Add a public page and `sitemap.xml`
changes in the same commit; the sitemap lists canonical URLs only.
