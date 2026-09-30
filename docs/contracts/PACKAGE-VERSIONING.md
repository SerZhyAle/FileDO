# PACKAGE-VERSIONING

| | |
| --- | --- |
| **Id** | `PACKAGE-VERSIONING` |
| **Version** | 0.1, draft |
| **Role** | consumer - FileDO's release scripts and package builders stamp one release across the desktop channels |
| **Home** | shared contracts catalog, folder `package-versioning/` |
| **Owner** | FastMediaSorter Android. FileDO's frozen version shape is the subject of a dated catalog proposal and exception |

## What this repository must do to stay conformant

- Derive the CLI, GUI, MSI, setup EXE, MSIX, winget manifest and release assets from one validated `yyMMddHHmm` stamp.
- Reject a stamp older than a published tag or one that would prevent an MSI upgrade.
- Keep the shipped tag and channel ordering compatible while the catalog owner decides how the draft covers Windows packages without a 9-digit versionCode.
