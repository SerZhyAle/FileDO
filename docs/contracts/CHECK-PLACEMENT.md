# CHECK-PLACEMENT

| | |
| --- | --- |
| **Id** | `CHECK-PLACEMENT` |
| **Version** | 0.11, draft |
| **Role** | producer - script organization |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Check ownership.** Every automated check is mapped to a runner (`build.ps1 -Test`, `release.ps1` preflight, or unit test suite) and not left unreferenced.
- **0.11: runner classes are named** (`per-change`, `agent-closure`, `build`, `release`, `hand-run`) and a runner's trigger path filter is compared with a check's inputs when the check is placed. FileDO declares "per-change runner: none" once, in its registry row.
