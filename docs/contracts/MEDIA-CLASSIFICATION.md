# MEDIA-CLASSIFICATION

| | |
| --- | --- |
| **Id** | `MEDIA-CLASSIFICATION` |
| **Version** | 0.9, draft |
| **Role** | consumer - file inspection, duplicate checks, container handling |
| **Home** | shared contracts catalog, folder `media-classification/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Container recognition.** `.fd-sec` is classified as `CONTAINER`.
- **Case-insensitive matching.** File extensions matched in lowercase ASCII.
- **System junk exclusion.** Skip temporary files (`*.tmp`, `*.~*`) and OS metadata (`Thumbs.db`, `.DS_Store`) during recursive scanning when configured.
