# UPDATE-MANIFEST

| | |
| --- | --- |
| **Id** | `UPDATE-MANIFEST` |
| **Version** | 0.9, draft |
| **Role** | consumer - release asset discovery and SHA-256 verification |
| **Home** | shared contracts catalog, folder `app-update-feed/`, document `README.md` |
| **Owner** | sza.od.ua hub |

## What this repository must do to stay conformant

- **Hash verification.** Standalone update packages verify SHA-256 integrity prior to launching installer.
- **Graceful network degradation.** Failed update checks do not interrupt app startup.
