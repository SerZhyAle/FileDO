# UPDATE-MANIFEST

| | |
| --- | --- |
| **Id** | `UPDATE-MANIFEST` |
| **Version** | 0.9, draft |
| **Role** | applicability disputed - no update-feed client exists in FileDO |
| **Home** | shared contracts catalog, folder `app-update-feed/`, document `README.md` |
| **Owner** | sza.od.ua hub |

## What this repository must do to stay conformant

- FileDO's site resolves download buttons through GitHub Releases, and winget uses its own YAML publication inputs. No FileDO process reads the catalog's update-feed JSON, downloads an update through it, or launches an installer from it.
- The owner's scope decision is requested in the catalog proposal dated 2026-10-01; this pointer stays until that decision.
