# BUILD-EVIDENCE

| | |
| --- | --- |
| **Id** | `BUILD-EVIDENCE` |
| **Version** | 0.10, draft |
| **Role** | producer - build automation evidence |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Fresh artifact stamping.** `build.ps1` stamps binary version `yyMMddHHmm` into all five executables and MSI package.
- **Deterministic verification.** Test gates run without flaky retries.
- **0.10: an artifact rebuilt outside the tested build states its binding** (rule 8): the release workflow's rebuild is held to the tested tree by the tag pin, and the version is read back from the produced file, not from the script's variable.
