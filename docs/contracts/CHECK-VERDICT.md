# CHECK-VERDICT

| | |
| --- | --- |
| **Id** | `CHECK-VERDICT` |
| **Version** | 0.10, draft |
| **Role** | producer and consumer - build, gate and test exit codes (`build.ps1`, `release.ps1`, `--selftest`) |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Exit code vocabulary.** Automated checks and build scripts adhere to standard verdict codes: 0 = PASS/Done, 1 = FAIL/defect found, 2 = COULD NOT VERIFY (missing tool, invalid target).
- **Clear verdict lines.** Scripts output concise status lines and actionable reasons on non-zero exit.
