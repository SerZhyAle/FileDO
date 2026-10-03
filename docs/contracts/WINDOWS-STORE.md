# WINDOWS-STORE

| | |
| --- | --- |
| **Id** | `WINDOWS-STORE` |
| **Version** | 0.1, draft |
| **Home** | shared contracts catalog, folder `windows-store/` |
| **Role** | consumer - FileDO ships to Microsoft Store via this contract |
| **Owner** | StreamsPlayer (as of 2026-10-03 per catalog registry) |

## What this repository must do to stay conformant

- **Package identity is verified before upload.** The packed MSIX archive is inspected to confirm
  `Package/Identity/Name` = `SZA.FileDO`, `Package/Identity/Publisher` = `CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD`,
  `Package/Properties/PublisherDisplayName` = `SZA`, no `AppxSignature.p7x` entry, and version derived
  mechanically from the release stamp per `PACKAGE-VERSIONING`.
- **THIRD-PARTY-NOTICES.txt is included** when third-party binaries are bundled.
- **Store-specific artifacts are not duplicated.** Package registry entries, file associations, and
  container declarations replace shell writes that fail silently inside the MSIX container.
- **Listing import file constraints.** UTF-8 without BOM, CRLF line endings, no trailing newline,
  embedded quotes doubled, cells checked against Partner Center caps before file write.

## Where the artifact lives

- **Producer artifacts.** `msix/` directory contains Store packaging scripts and identity; the
  actual Store listing copy lives in the StreamsPlayer product repository and is not duplicated here.
- **Consumer behavior.** FileDO's Store channel build (via `build.ps1` and `msix/build-msix.ps1`)
  must pass the package identity verification and follow the catalog rules.