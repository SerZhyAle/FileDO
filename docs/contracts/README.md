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
| [`FDD-FORMAT.md`](FDD-FORMAT.md) | the `.fdd` disk container, byte for byte (draft; `vdisk/`, unreleased) | producer and consumer (Owner) |
| [`FDD-BEHAVIOUR.md`](FDD-BEHAVIOUR.md) | what a program that reads and writes `.fdd` containers must do: read path, classes 2-8, clean marker (draft; `vdisk/` and `filedo vd`, unreleased) | producer and consumer (Owner) |
| [`DISK-SHARE.md`](DISK-SHARE.md) | FileDO virtual disk sharing via Fast Media Sorter | producer and consumer (Shared) |
| [`WORKER-IPC.md`](WORKER-IPC.md) | bounded named-pipe control of the FMS worker, including schema-2 disk requests | consumer (`fmsworker/`) |
| [`CLI-EVENT-STREAM.md`](CLI-EVENT-STREAM.md) | how the CLI reports a run to the shell that started it | producer (CLI) and consumer (shell) (Owner) |
| [`INSTALL-TRUST.md`](INSTALL-TRUST.md) | what a user reads after Windows warns about an unsigned build | producer |
| [`APP-BEHAVIOUR.md`](APP-BEHAVIOUR.md) | shared desktop application UX moments | consumer (GUI shell) |
| [`APP-STYLE.md`](APP-STYLE.md) | desktop app styling, themes and palette vocabulary | consumer (GUI shell) |
| [`APP-SETTINGS.md`](APP-SETTINGS.md) | the persistent settings surface | consumer (GUI shell, since 2026-10-05) |
| [`WINDOWS-UI.md`](WINDOWS-UI.md) | shared Windows settings, dialogs and viewer profile | consumer (GUI shell, since 2026-10-05) |
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
| [`SITE-STRUCTURE.md`](SITE-STRUCTURE.md) | which pages a product site has, how a visitor moves, what an address promises (0.1 draft, opted in 2026-10-06; guide tier) | consumer |
| [`SITE-EXPERIENCE.md`](SITE-EXPERIENCE.md) | the portal layer over the kit, shared language and theme state, third-party origins, keyboard and screen-reader reach (0.1 draft, opted in 2026-10-06) | consumer |
| [`SITE-REPRESENTATION.md`](SITE-REPRESENTATION.md) | one positioning source, facts typed once, the fixed anatomy of a function page, the chain of truth (0.1 draft, opted in 2026-10-06) | consumer |

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
| [`APP-ACTIVATION.md`](APP-ACTIVATION.md) | GUI second-start window focus and process invocation | scope disputed; the focus-only question awaits the owner |
| [`INPUT-PARITY.md`](INPUT-PARITY.md) | keyboard and pointer action parity | consumer; partial adoption |
| [`CLIPBOARD-GUARD.md`](CLIPBOARD-GUARD.md) | safe clipboard interactions and history protection | consumer |
| [`PACKAGE-VERSIONING.md`](PACKAGE-VERSIONING.md) | release stamp and package ordering across desktop channels | consumer; Windows desktop profile, `compact` rendering (0.2); dated draft exception is the owner's to close |
| [`WINDOWS-STORE.md`](WINDOWS-STORE.md) | the package and listing a product hands to Microsoft Store | consumer (Store channel) |

### Read against this repository and found not to bind it

A contract the catalog added that FileDO neither produces nor consumes, read once so the next run does not
read it again. One line each: the id and the version read, the date, and why. A contract that starts to
bind leaves this table for one of the tables above and gets a pointer.

| Contract | Read | Why it does not bind |
| --- | --- | --- |
| `CAPTURE-OUTPUT` 0.2 draft (`capture-output/`) | 2026-10-01 | FileDO creates none of its capture kinds - photo, video, screenshot, recording, frame, note, OCR text, translation. Its own files are another contract's (`.fd-sec`, the diagnostic zip), a test payload `clean` finds by name (`FILL_*.tmp`, `speedtest_*.txt`), the shell's state under `%LOCALAPPDATA%\FileDO`, a copy the user asked for, or a site render target (`--capture-screens`). Open: whether a tool's report of its own run (`compare_report_*.log`, `delete_report_*.log`, `check --report`) is in scope - asked of the owner in `PROPOSAL-2026-09-26-filedo-tool-reports.md`; FileDO reads it as outside. Registry row dated 2026-09-26. |
| `INPUT-CHORD` 0.3 draft (`input-controls/`) | 2026-10-01 | FileDO has fixed in-window shortcuts only, no stored interchange chord, user chord capture or global hook; domain section 1 explicitly excludes in-window shortcuts. `INPUT-PARITY` applies separately. |
| `HARNESS-PROFILE` 0.9 draft (`rule-adoption/`) | 2026-10-01 | FileDO does not run the shared harness or carry its profile; its own build gate uses `packaging/check-placement.jsonl`. |
| `WAVE-PARTICLES` 0.12 (animated-backdrop/) | 2026-10-01 | No waves/particles backdrop is implemented. |
| `FMSCFG` 2.1 (config-interchange/) | 2026-10-01 | No FMS configuration interchange producer or importer. |
| `LAN-DISCOVERY` 0.9 (lan-discovery/) | 2026-10-01 | No mDNS/DNS-SD announcement or discovery; the requested loopback disk server is not a LAN service. |
| `SCENARIO-FILE` 0.9 (launch-scenarios/) | 2026-10-01 | No AppItem XML scenario interchange; batch lists are FileDO commands. |
| `LAYOUT-SIGNAL` 1.2 (layout-indicator/) | 2026-10-01 | No keyboard-layout publication or caret claim. |
| `LAYOUT-PALETTE` 1.1 (layout-indicator/) | 2026-10-01 | No layout marker; GUI themes are APP-STYLE. |
| `LETTERBOX-BARS` 0.1 (letterbox-fill/) | 2026-10-01 | No media viewer or image-fit band renderer. |
| `LETTERBOX-HALO` 0.1 (letterbox-fill/) | 2026-10-01 | No media viewer halo. |
| `LIVE-BROADCAST` 0.13 (live-broadcast/) | 2026-10-01 | No media broadcast producer, descriptor reader or player. |
| `OCR-OVERLAY` 1.2 (ocr-overlay/) | 2026-10-01 | No OCR overlay or recognition rendering. |
| `PAGE-CONTENT-VISION` 1.1 (product-web-pages/) | 2026-10-01 | A non-binding historical vision record; PAGE-CONTENT is the binding contract. |
| `SHARE-SESSION` 1.0 (remote-folder-access/) | 2026-10-01 | No FMS SFTP session provider or client; generic Windows shares use the filesystem. |
| `STREAM-BANK` 2.1 (stream-catalog/) | 2026-10-01 | No stream bank ZIP/CSV importer or publisher. |
| `USER-PLAYLIST` 0.11 (user-playlist/) | 2026-10-01 | No user playlist/stream collection importer or exporter. |
| `MEDIA-CLASSIFICATION` 0.10 (`media-classification/`) | 2026-10-02 | FileDO is not a consumer: the catalog accepted FileDO's scope proposal into 0.10. It accepts arbitrary files as storage targets, reports Go's MIME guess in `file info`, and has no media kind enum, browser, routing or sidecar association. The pointer is retired. |
| `UPDATE-MANIFEST` 0.10 (`app-update-feed/`) | 2026-10-02 | FileDO is a planned consumer only, not bound: no update-check client exists. The site resolves downloads through GitHub Releases and winget uses its own YAML, neither of which is this feed. The pointer is retired; a client that parses the manifest brings it back. |
| `APP-SETTINGS` 0.1 (`desktop-app-ux/`) | 2026-10-02 -> bound 2026-10-05 | Superseded by the portfolio owner's 2026-10-05 opt-in (WINDOWS-UI 0.1, APP-SETTINGS 0.2 section 8): the settings page is a persistent settings surface and FileDO is a consumer, implemented in SP-0150. The row moved to the table above with its pointer. |
