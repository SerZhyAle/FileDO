# docs/ - index of this tree

This tree contains the public site, developer documentation and the release ledger. The Pages workflow
stages the public site from this tree and applies the exclusions listed below.

## The published site

Canonical pages, all hand-authored and edited in place - none of them is a render target. The renders in this
tree are named under "Supporting files" below.

| Page | What it is |
| --- | --- |
| [`index.html`](index.html) | Product landing page (English, primary locale). Release version, date, setup filename/size, SHA-256 link, and versioned asset links (`-setup.exe`, `-windows-x64.zip`, `-windows-x64.msi`) are rendered at build/release time from `packaging/release-manifest.json` (`packaging/render-site-release.ps1`), keeping durable `/releases/latest` beside them and `api.github.com` only as runtime refinement (`PAGE-STYLE` 3.10) - see `RELEASE.md`. |
| [`guides/index.html`](guides/index.html) | Guide hub - the safe starting point chooser. |
| [`guides/quick-start.html`](guides/quick-start.html) | Quick-start matrix - maps common tasks to the exact GUI page and CLI command (`filedo -?`), grouped into read-only checks, temporary test files/copies/containers, and destructive operations. |
| [`guides/check-storage.html`](guides/check-storage.html) | Verifying real capacity and speed of a drive. |
| [`guides/files-and-copies.html`](guides/files-and-copies.html) | Duplicates, copies and folder differences. |
| [`guides/erase.html`](guides/erase.html) | Erasing: what a file wipe, a folder wipe, a free-space wipe and a plain delete each do, the typed `WIPE` confirmation and what `--yes` skips, what is refused, and what a wipe cannot promise on an SSD. It is the guide behind the `erase` pillar of `packaging/positioning-source.json`. |
| [`guides/gui-command-builder.html`](guides/gui-command-builder.html) | Building a command in the VB.NET GUI. |
| [`guides/commands.html`](guides/commands.html) | Commands for scripts and agents - what a script, a scheduled task or a development tool can ask the running FileDO, with exit codes, risk classes and the one setting that gates them. Hand-written; `packaging/write-cli-docs.ps1 -Check` holds it to naming every command of the manifest. |
| [`guides/cli-reference.html`](guides/cli-reference.html) | Command reference - target types, operations and their short forms, modifiers, the read check (its flags in `README.md` and `README.ru.md`), batch files and exit codes, history and network paths. Between its `cli-reference` markers it shows the README's Command Reference, the read check subsections of its Quick Start and its Advanced Features, one block per language (`README.md`, `.ru`, `.ua`; the de and fr pages `README.de.md`, `README.fr.md`), written by `packaging/write-cli-reference.ps1`, never by hand; head, hero and footer are hand-written. The landings' "Documentation" link opens it. |
| [`guides/install-and-explorer.html`](guides/install-and-explorer.html) | Choosing an install, what the MSI writes, and the Explorer entries. Its component and channel table is written by `packaging/write-install-matrix.ps1` from the packaging files, never by hand. The asset names in its copy commands are rendered from `packaging/release-manifest.json` by `packaging/render-site-release.ps1`, as the landing's download facts are. |
| [`guides/install-trust.html`](guides/install-trust.html) | The `INSTALL-TRUST` page: what the Windows warning is, why it appears, exactly what to click, and what the app never does - in that order. Its fourth section says the same thing as `privacy.html`, the MSIX capability list and the Store data-safety answers, so the four change together. When the build becomes signed the page is **updated, not deleted** - users of older builds still meet the warning. |
| [`guides/fd-sec-containers.html`](guides/fd-sec-containers.html) | `.fd-sec` containers - packing, restoring, revealing, and what each choice promises. |
| [`guides/virtual-disks.html`](guides/virtual-disks.html) | `.fdd` virtual disks - create, mount, read and export without mounting, obfuscated against encrypted, what Windows and the Store build can and cannot do, and the Disk manager window (states, keyboard, help, first run). Its screenshots `gui-disk-*` are produced by `filedo_win.exe --capture-screens`. |
| [`guides/fms-sharing.html`](guides/fms-sharing.html) | Sharing `.fdd` disks through Fast Media Sorter & Sharing - prerequisites (the optional FileDO disk component), registration from the Disk Manager, the `vd share` verbs, what paired devices see over SFTP, the unavailable-sharing dialogs and recovery after a failed step. Reuses the `gui-disk-share-*` screenshots. |
| [`guides/glossary.html`](guides/glossary.html) | Glossary - one entry per term in [`termbase.json`](termbase.json), `id="term-<id>"`. |
| [`guides/topics.html`](guides/topics.html) | Subject index - every subject the guides cover and the page for it; it links every guide. |
| [`guides/release-notes.html`](guides/release-notes.html) | Release notes - what changed in each shipped release, newest first, with its date; one `id="v<stamp>"` section per release of the Version History in `README.md`, which `check-external-docs.ps1` (step `releasenotes`) holds equal. |
| [`guides/support.html`](guides/support.html) | Support - where to report a problem, what to include, and which pages answer the usual questions (the install warning, privacy, release notes). |
| [`guides/microsoft-store.html`](guides/microsoft-store.html) | The channel page of the Microsoft Store build (`SITE-STRUCTURE` edition-or-channel page) - what that build does, what it leaves out (mounting, partition disks, sharing, Explorer entries, startup in Windows Settings), how it is signed, and where to get it. The limits are stated here once and stay next to their function in the other guides; when the Store build gains or loses a capability this page changes in the same edit. |
| [`privacy.html`](privacy.html) | Privacy policy - local processing and data handling without analytics. |

Supporting files: [`robots.txt`](robots.txt), [`sitemap.xml`](sitemap.xml) (a render: written by
`packaging/write-sitemap.ps1`, never by hand), [`assets/`](assets/) (the guide
screenshots in `assets/guides/` are produced by `packaging/capture-guide-screens.ps1`, never drawn by hand; the
share images `social-card.png` (1200x630) and `social-preview.png` (1280x640), the mark `filedo.png` and
`filedo.svg` and the help pages' `help/filedo-mark.svg` and `help/favicon.png` are renditions of the product's one
drawing, written by `assets/make-product-icons.ps1` and listed with their SHA-256 in
`../assets/identity/identity-manifest.json` (PRODUCT-IDENTITY), never drawn or edited by hand; the demo `demo.mp4`, `demo.gif` and its poster `demo-poster.jpg`, shown on the
five landing pages and at the top of the READMEs, are recorded by `msix/make-demo.ps1`),
[`termbase.json`](termbase.json),
[`cli-commands.json`](cli-commands.json) (the command manifest, the source) and [`CLI.md`](CLI.md) (a render of it, written by
`packaging/write-cli-docs.ps1`, never by hand),
the IndexNow key file `ac596e568e538e6f5b1d5a60660ae226.txt` (its name is the key; keep it),
[`kit/sza-kit.css`](kit/sza-kit.css), `_config.yml`, `.nojekyll`.

`ru/` ([`ru/index.html`](ru/index.html)) and `uk/` ([`uk/index.html`](uk/index.html)) hold **standalone landing pages** in Russian and Ukrainian (`PAGE-STYLE` 3.9, SP-0199), each with its own canonical link, localized Open Graph/JSON-LD metadata, and `hreflang` cluster, and both are listed in `sitemap.xml`. `ua/` ([`ua/index.html`](ua/index.html)) is a **redirect stub** that sets `sza-lang` to `ua` and forwards to `../uk/`. `de/` and `fr/` are **standalone pages** (SP-0154): the landing, the privacy page and the guides in German and French, each with its own canonical link and listed in `sitemap.xml`.

## Repository documentation and release ledger

The Pages workflow (`.github/workflows/pages.yml`) excludes `contracts/`, this `README.md`,
`DOCUMENT_REGISTRY.jsonl`, [`translation-fingerprints.json`](translation-fingerprints.json) and
`help/help-baseline.txt` when it stages the site artifact.

[`contracts/`](contracts/README.md) holds one pointer file per shared contract this product produces or
consumes - id, version, the catalog folder it lives in, this repo's role, and what we must keep doing to
stay conformant. The contract catalog and its rules are described in `AGENTS.md`, "External contracts".

[`releases/`](releases/RELEASES.md) is the release ledger (`RELEASE-LEDGER`): `channels.json` declares the
packages and the channels each ships to, `builds.json` keeps the last build of each package, `history.jsonl`
every publication, and `RELEASES.md` renders the three. The scripts write them (`packaging/release-ledger.ps1`,
see `RELEASE.md`, "The release ledger"); only `channels.json` is edited by hand. The ledger is accessible by
its file paths and sits outside the site's sitemap and navigation.

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
  text of its image, Twitter card, JSON-LD, `hreflang` for x-default and the page's standalone locales,
  no `keywords` meta) and its runtime per-locale title and description where applicable;
- on pages that switch languages in place, every group of `data-l` spans has ru, en and ua exactly once;
  the German and French pages are standalone
  (`docs/de/`, `docs/fr/`, one language each, the same path as the runtime page, each group read against the English
  of the page it mirrors; a secondary DE/FR row on every page, never written to `sza-lang`); and every README translation has README.md's
  `## ` sections. **Changing English text fails the gate** until the ru/ua spans (or the README translations)
  are brought up to date and the fingerprints re-recorded with `packaging/check-external-docs.ps1 -Record` -
  that command is the statement "the translations were read against this English", so run it only then;
- no forbidden synonym from `termbase.json` in any locale's prose; the glossary has one entry per term;
- a page whose `<main>` has `data-ui-steps` shows screenshots; every `<img>` has alt text (empty only for
  the product's picture with the name as text beside it, `PRODUCT-IDENTITY` rule 11) and the true width and
  height of a file under `assets/`;
- no `http://` link, no en or em dash and no `...` in prose.

The five standalone landings include `ru` and `uk` in their reciprocal `hreflang` cluster. The Russian and
Ukrainian guide translations share the English guide URL and switch languages within that page.

The product pages consume `PAGE-CONTENT`, `PAGE-STYLE`, and `SITE-FAMILY-MAP`. `kit/sza-kit.css` is the
vendored `PAGE-STYLE` artifact: copy it byte-for-byte from the catalog and put FileDO-specific styling in
the page layer instead of editing the kit.
