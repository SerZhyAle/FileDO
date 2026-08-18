# docs/ - index of this tree

Two different things live here, and mixing them up is the only trap in this folder.

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

## Repo documents that are not part of the site

Reachable by URL, but linked from nothing and excluded from the sitemap on purpose:

- [`github-store-submission.md`](github-store-submission.md) - Microsoft Store submission write-up.
- [`how-i-posted-this-project-to-winget.md`](how-i-posted-this-project-to-winget.md) - the winget walkthrough.
- [`spec-safety-speed-improvements.md`](spec-safety-speed-improvements.md) - safety/speed work spec.
- [`spec-send-logs-to-author.md`](spec-send-logs-to-author.md) - log-sending feature spec.

## Editing rules

A user-visible change lands in **every** surface in one edit - the landing page, the guides, the READMEs
and the listing sources - per canon `DOCUMENTATION_CONCEPT.md` 5. Add a public page and `sitemap.xml`
changes in the same commit; the sitemap lists canonical URLs only.
