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
| [`FDSEC-FORMAT.md`](FDSEC-FORMAT.md) | the `.fd-sec` container, byte for byte | producer and consumer |
| [`FDSEC-BEHAVIOUR.md`](FDSEC-BEHAVIOUR.md) | what `secure` / `unsecure` must do to be safe | producer and consumer |
| [`CLI-EVENT-STREAM.md`](CLI-EVENT-STREAM.md) | how the CLI reports a run to the shell that started it | producer (CLI) and consumer (shell) |
| [`INSTALL-TRUST.md`](INSTALL-TRUST.md) | what a user reads after Windows warns about an unsigned build | producer |
