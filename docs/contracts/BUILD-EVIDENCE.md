# BUILD-EVIDENCE

| | |
| --- | --- |
| **Id** | `BUILD-EVIDENCE` |
| **Version** | 0.9, draft |
| **Role** | producer - build automation evidence |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Fresh artifact stamping.** `build.ps1` stamps binary version `yyMMddHHmm` into all five executables and MSI package.
- **Deterministic verification.** Test gates run without flaky retries.
