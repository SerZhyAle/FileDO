# docs/ - index of this tree

This tree is the published site, plus one folder of contract pointers that the site does not serve
(`contracts/`, below). The working documents that precede a feature - specs, plans, questionnaires,
research notes - are not here; see "What does not live here" below.

## The published site (GitHub Pages serves this tree)

Canonical pages, all hand-authored and edited in place - none of them is a render target. The renders in this
tree are named under "Supporting files" below.

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
| [`guides/virtual-disks.html`](guides/virtual-disks.html) | `.fdd` virtual disks - create, mount, read and export without mounting, obfuscated against encrypted, what Windows and the Store build can and cannot do, and the Disk manager window (states, keyboard, help, first run). Its screenshots `gui-disk-*` are produced by `filedo_win.exe --capture-screens`. |
| [`guides/fms-sharing.html`](guides/fms-sharing.html) | Sharing `.fdd` disks through Fast Media Sorter & Sharing - prerequisites (the optional FileDO disk component), registration from the Disk Manager, the `vd share` verbs, what paired devices see over SFTP, the unavailable-sharing dialogs and recovery after a failed step. Reuses the `gui-disk-share-*` screenshots. |
| [`guides/glossary.html`](guides/glossary.html) | Glossary - one entry per term in [`termbase.json`](termbase.json), `id="term-<id>"`. |
| [`guides/topics.html`](guides/topics.html) | Subject index - every subject the guides cover and the page for it; it links every guide. |
| [`guides/release-notes.html`](guides/release-notes.html) | Release notes - what changed in each shipped release, newest first, with its date; one `id="v<stamp>"` section per release of the Version History in `README.md`, which `check-external-docs.ps1` (step `releasenotes`) holds equal. |
| [`guides/support.html`](guides/support.html) | Support - where to report a problem, what to include, and which pages answer the usual questions (the install warning, privacy, release notes). |
| [`privacy.html`](privacy.html) | Privacy policy - no network, no telemetry, no accounts. |

Supporting files: [`robots.txt`](robots.txt), [`sitemap.xml`](sitemap.xml) (a render: written by
`packaging/write-sitemap.ps1`, never by hand), [`assets/`](assets/) (the guide
screenshots in `assets/guides/` are produced by `packaging/capture-guide-screens.ps1`, never drawn by hand; the
share images `social-card.png` (1200x630) and `social-preview.png` (1280x640) are written by
`assets/make-product-icons.ps1 -SocialOnly`),
[`termbase.json`](termbase.json), [`translation-fingerprints.json`](translation-fingerprints.json) (a render - see below),
the IndexNow key file `ac596e568e538e6f5b1d5a60660ae226.txt` (its name is the key; keep it),
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

[`DOCUMENT_REGISTRY.jsonl`](DOCUMENT_REGISTRY.jsonl) declares every maintained document in the repository -
this tree, the READMEs, `AGENTS.md`, `RELEASE.md`, the per-folder READMEs and the winget manifests - with
its topic, product area, update triggers, role (`source` or `render`) and corpus (`internal` or
`external`), plus ignore records for what is deliberately not a document. `packaging/check-internal-docs.ps1`
holds it to the disk both ways, so a new page or `.md` needs a record in the same commit
(`DOC-INTERNAL-QUALITY`).

## What does not live here

Specs, implementation plans, questionnaires and research write-ups are working documents, not product
documentation. They live in `PLAN/` at the repository root, which `.gitignore` keeps out of git. What
the repository carries is documentation of the finished product, written for an experienced user - the
pages above, the READMEs, and the listing sources.

## Editing rules

A user-visible change lands in **every** surface in one edit - the landing page, the guides, the READMEs
and the listing sources - per canon `DOCUMENTATION_CONCEPT.md` 5. Add a public page and run
`packaging/write-sitemap.ps1`; the sitemap lists canonical URLs only. Each `lastmod` is the page file's last
commit date, so run the script again after the commit and commit that one-file change (a page not yet committed
carries its last-write date until then).

`packaging/check-external-docs.ps1` (step 9 of `build.ps1 -Test`, `DOC-EXTERNAL-QUALITY`) holds the published
corpus - these pages and the five READMEs:

- every public page is in `sitemap.xml` at its canonical URL and nothing else is; every page carries the full
  SEO block (title with FileDO under 60, description under 160, canonical, Open Graph with the size and alt
  text of its image, Twitter card, JSON-LD, `hreflang` x-default, en, de and fr, no `keywords` meta) and its
  runtime per-locale title and description;
- every group of `data-l` spans has ru, en and ua exactly once; the German and French pages are standalone
  (`docs/de/`, `docs/fr/`, one language each, the same path as the runtime page, each group read against the English
  of the page it mirrors; a secondary DE/FR row on every page, never written to `sza-lang`); and every README translation has README.md's
  `## ` sections. **Changing English text fails the gate** until the ru/ua spans (or the README translations)
  are brought up to date and the fingerprints re-recorded with `packaging/check-external-docs.ps1 -Record` -
  that command is the statement "the translations were read against this English", so run it only then;
- no forbidden synonym from `termbase.json` in any locale's prose; the glossary has one entry per term;
- a page whose `<main>` has `data-ui-steps` shows screenshots; every `<img>` has alt text and the true
  width and height of a file under `assets/`;
- no `http://` link, no en or em dash and no `...` in prose.

The ru and ua locales share the English URL (runtime switch), so `hreflang` names x-default, en, de and fr only,
and there is no search index; both are put to the contract owner as a proposal (`docs/contracts/DOC-EXTERNAL-QUALITY.md`).

The product pages consume `PAGE-CONTENT`, `PAGE-STYLE`, and `SITE-FAMILY-MAP`. `kit/sza-kit.css` is the
vendored `PAGE-STYLE` artifact: copy it byte-for-byte from the catalog and put FileDO-specific styling in
the page layer instead of editing the kit.
