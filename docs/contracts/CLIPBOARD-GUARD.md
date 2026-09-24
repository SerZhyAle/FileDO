# CLIPBOARD-GUARD

| | |
| --- | --- |
| **Id** | `CLIPBOARD-GUARD` |
| **Version** | 0.9, draft |
| **Role** | consumer - safe clipboard copying in GUI |
| **Home** | shared contracts catalog, folder `clipboard-guard/`, document `README.md` |
| **Owner** | CyrFlip |

## What this repository must do to stay conformant

- **No credential leaks.** Passwords and sensitive keys are not dumped into clipboard history unshielded.
- **Format preservation.** File paths and report paths written using standard UTF-16 text formatting with error handling around clipboard locks.
