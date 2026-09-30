# MEDIA-CLASSIFICATION

| | |
| --- | --- |
| **Id** | `MEDIA-CLASSIFICATION` |
| **Version** | 0.9, draft |
| **Role** | applicability disputed - generic storage operations do not classify media |
| **Home** | shared contracts catalog, folder `media-classification/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- FileDO accepts `.fd-sec` and archive files as opaque storage targets. Its `file info` report displays Go's MIME guess, but no operation routes by the contract's media categories or associates sidecars.
- FileDO does not omit temporary or OS metadata files from a user-selected storage operation merely because a media browser would hide them. The owner's scope decision is requested in the catalog proposal dated 2026-10-01; this pointer stays until that decision.
