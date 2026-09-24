# CHECK-PLACEMENT

| | |
| --- | --- |
| **Id** | `CHECK-PLACEMENT` |
| **Version** | 0.10, draft |
| **Role** | producer - script organization |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Check ownership.** Every automated check is mapped to a runner (`build.ps1 -Test`, `release.ps1` preflight, or unit test suite) and not left unreferenced.
