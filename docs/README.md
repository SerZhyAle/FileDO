# docs/ - index of this tree

This tree is the published site, plus one folder of contract pointers that the site does not serve
(`contracts/`, below). The working documents that precede a feature - specs, plans, questionnaires,
research notes - are not here; see "What does not live here" below.

## The published site (GitHub Pages serves this tree)

Canonical pages, all hand-authored - there is no generator, so these are **not** render targets and are
edited in place:

| Page | What it is |
| --- | --- |
| [`index.html`](index.html) | Product landing page. Switches language at runtime from `sza-lang`. Its download buttons resolve from the `releases/latest` API by asset-name suffix (`-setup.exe`, `-windows-x64.zip`, `-windows-x64.msi`), so a release needs no edit here - see `RELEASE.md`. |
| [`guides/index.html`](guides/index.html) | Guide hub - the safe starting point chooser. |
| [`guides/check-storage.html`](guides/check-storage.html) | Verifying real capacity and speed of a drive. |
| [`guides/files-and-copies.html`](guides/files-and-copies.html) | Duplicates, copies and folder differences. |
| [`guides/gui-command-builder.html`](guides/gui-command-builder.html) | Building a command in the VB.NET GUI. |
| [`guides/install-and-explorer.html`](guides/install-and-explorer.html) | Choosing an install, what the MSI writes, and the Explorer entries. |
| [`guides/install-trust.html`](guides/install-trust.html) | The `INSTALL-TRUST` page: what the Windows warning is, why it appears, exactly what to click, and what the app never does - in that order. Its fourth section says the same thing as `privacy.html`, the MSIX capability list and the Store data-safety answers, so the four change together. When the build becomes signed the page is **updated, not deleted** - users of older builds still meet the warning. |
| [`guides/fd-sec-containers.html`](guides/fd-sec-containers.html) | `.fd-sec` containers - packing, restoring, revealing, and what each choice promises. |
| [`privacy.html`](privacy.html) | Privacy policy - no network, no telemetry, no accounts. |

Supporting files: [`robots.txt`](robots.txt), [`sitemap.xml`](sitemap.xml), [`assets/`](assets/),
[`kit/sza-kit.css`](kit/sza-kit.css), `_config.yml`, `.nojekyll`.

`ru/`, `ua/`, `de/` and `fr/` hold **redirect stubs only**. Each sets `sza-lang` and bounces to the site
root, which is where the translated text actually lives - so they carry a canonical link to the root and
are deliberately absent from `sitemap.xml`.

## Not part of the site

[`contracts/`](contracts/README.md) holds one pointer file per shared contract this product produces or
consumes - id, version, the catalog folder it lives in, this repo's role, and what we must keep doing to
stay conformant. Pointers, never copies: the contracts themselves live outside this repository
(`AGENTS.md`, "External contracts"). The files are developer documentation, so they are unlinked from
every page, absent from `sitemap.xml`, and left unrendered by `.nojekyll`.

## What does not live here

Specs, implementation plans, questionnaires and research write-ups are working documents, not product
documentation. They live in `PLAN/` at the repository root, which `.gitignore` keeps out of git. What
the repository carries is documentation of the finished product, written for an experienced user - the
pages above, the READMEs, and the listing sources.

## Editing rules

A user-visible change lands in **every** surface in one edit - the landing page, the guides, the READMEs
and the listing sources - per canon `DOCUMENTATION_CONCEPT.md` 5. Add a public page and `sitemap.xml`
changes in the same commit; the sitemap lists canonical URLs only.

The product pages consume `PAGE-CONTENT`, `PAGE-STYLE`, and `SITE-FAMILY-MAP`. `kit/sza-kit.css` is the
vendored `PAGE-STYLE` artifact: copy it byte-for-byte from the catalog and put FileDO-specific styling in
the page layer instead of editing the kit.
