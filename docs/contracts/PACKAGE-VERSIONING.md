# PACKAGE-VERSIONING

| | |
| --- | --- |
| **Id** | `PACKAGE-VERSIONING` |
| **Version** | 0.4, draft |
| **Role** | consumer - FileDO's release scripts and package builders stamp one release across the desktop channels |
| **Home** | shared contracts catalog, folder `package-versioning/` |
| **Owner** | FastMediaSorter Android. 0.2 registers FileDO's `compact` tag rendering in the Windows desktop profile; FileDO's former rendering exception is closed against that profile, while installed upgrade proof remains open |

## What this repository must do to stay conformant

- Derive the CLI, GUI, MSI, setup EXE, MSIX, winget manifest and release assets from one validated `yyMMddHHmm` stamp.
- Reject a stamp older than a published tag or one that would prevent an MSI upgrade.
- Stay inside the Windows desktop profile (0.2): the tag is the registered `compact` rendering `v<yyMMddHHmm>`, every channel version (Windows Installer, MSIX, PE file version) is a mechanical, order-preserving remap of the pinned stamp recorded in the registry row, there is no pair and no `versionCode`, and each artifact carries the stamp or its remap in a field read back from the produced file.
- Keep the shipped tag and channel ordering unchanged: the version shape is frozen for the product's life.
- **0.3 and 0.4, read 2026-10-07 (SP-0165):** both are registrations for other families - the StreamsPlayer Android bands and the `dotted-loose` Windows rendering - and leave FileDO's `compact` row untouched; nothing changes here.
