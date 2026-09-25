# docs/contracts/ - pointers, never copies

One file per contract this product produces or consumes. Each names the contract **id**, the version this
repository is built against, the folder it lives in inside the shared contracts catalog, this product's
**role**, and what this repository has to keep doing to stay conformant.

That is the whole permitted content. A pointer that grows a second page has become a copy, and two copies
of a contract are two contracts. The text itself - every byte table, every rule, every vector - lives in
the catalog and nowhere else.

**Where the catalog is** is stated in exactly one file in this repository: [`AGENTS.md`](../../AGENTS.md),
section "External contracts". Nothing here repeats the path, because whoever clones this repository does
not have that drive, and because moving the catalog must cost one edit rather than forty.

**Source comments cite by id**: `FDSEC-FORMAT.md section 8`, `CLI-EVENT-STREAM rule 10`. Never a path,
never a link.

**This folder is not part of the published site.** GitHub Pages serves this tree, but these files are
developer documentation: they are absent from `sitemap.xml`, unlinked from every page, and `.nojekyll`
keeps them from being rendered. See [`../README.md`](../README.md) for what the site itself is.

| Pointer | Contract | Role |
| --- | --- | --- |
| [`FDSEC-FORMAT.md`](FDSEC-FORMAT.md) | the `.fd-sec` container, byte for byte | producer and consumer (Owner) |
| [`FDSEC-BEHAVIOUR.md`](FDSEC-BEHAVIOUR.md) | what `secure` / `unsecure` must do to be safe | producer and consumer (Owner) |
| [`CLI-EVENT-STREAM.md`](CLI-EVENT-STREAM.md) | how the CLI reports a run to the shell that started it | producer (CLI) and consumer (shell) (Owner) |
| [`INSTALL-TRUST.md`](INSTALL-TRUST.md) | what a user reads after Windows warns about an unsigned build | producer |
| [`APP-BEHAVIOUR.md`](APP-BEHAVIOUR.md) | shared desktop application UX moments | consumer (GUI shell) |
| [`APP-STYLE.md`](APP-STYLE.md) | desktop app styling, themes and palette vocabulary | consumer (GUI shell) |
| [`ICON-SET.md`](ICON-SET.md) | the glyph vocabulary: one meaning, one glyph, one name | consumer |
| [`ICON-RENDER.md`](ICON-RENDER.md) | how a glyph is drawn: colour role, themes, sizes, system surfaces | consumer |
| [`ICON-EXTERNAL.md`](ICON-EXTERNAL.md) | third-party marks, other apps' icons, downloaded pictures | consumer |

### Public-page contracts this repository consumes

| Pointer | Contract | Role |
| --- | --- | --- |
| [`PAGE-CONTENT.md`](PAGE-CONTENT.md) | product public web page content hierarchy and rules | consumer |
| [`PAGE-STYLE.md`](PAGE-STYLE.md) | product public web page styling and kit tokens | consumer |
| [`SITE-FAMILY-MAP.md`](SITE-FAMILY-MAP.md) | product public web page family grid and canonical URLs | consumer |
| [`DOC-EXTERNAL-QUALITY.md`](DOC-EXTERNAL-QUALITY.md) | what the published site and READMEs owe their public | consumer |
| [`DOC-INTERNAL-QUALITY.md`](DOC-INTERNAL-QUALITY.md) | what the internal engineering docs owe their readers | consumer |

### Other contracts this repository produces or consumes

| Pointer | Contract | Role |
| --- | --- | --- |
| [`REPO-STAMP.md`](REPO-STAMP.md) | canon adoption stamp `.sza-canon.json` | producer |
| [`REPO-LAYOUT.md`](REPO-LAYOUT.md) | the root and `docs/` names shared tools address | consumer |
| [`RULE-DELIVERY.md`](RULE-DELIVERY.md) | how the canon arrives, and how staleness is judged | consumer |
| [`CHECK-VERDICT.md`](CHECK-VERDICT.md) | standardized check verdict lines and exit codes | producer and consumer |
| [`CHECK-BASELINE.md`](CHECK-BASELINE.md) | accepted debt ratchets and baselines | producer |
| [`CHECK-PLACEMENT.md`](CHECK-PLACEMENT.md) | check runner placement and execution mapping | producer |
| [`BUILD-EVIDENCE.md`](BUILD-EVIDENCE.md) | build artifact stamping and test evidence | producer |
| [`DIAGNOSTIC-REPORT.md`](DIAGNOSTIC-REPORT.md) | sanitized diagnostic archive export | producer (`LogReport.vb`) |
| [`MEDIA-CLASSIFICATION.md`](MEDIA-CLASSIFICATION.md) | normalized media/container kinds and file rules | consumer |
| [`UPDATE-MANIFEST.md`](UPDATE-MANIFEST.md) | standalone release discovery and integrity checks | consumer |
| [`APP-ACTIVATION.md`](APP-ACTIVATION.md) | Explorer verb integration and process invocation | consumer |
| [`CLIPBOARD-GUARD.md`](CLIPBOARD-GUARD.md) | safe clipboard interactions and history protection | consumer |
