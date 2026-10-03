# CHECK-VERDICT

| | |
| --- | --- |
| **Id** | `CHECK-VERDICT` |
| **Version** | 0.11, draft |
| **Role** | producer and consumer - build, gate and test exit codes (`build.ps1`, `release.ps1`, `--selftest`) |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Exit code vocabulary.** Automated checks and build scripts adhere to standard verdict codes: 0 = PASS/Done, 1 = FAIL/defect found, 2 = COULD NOT VERIFY (missing tool, invalid target).
- **Clear verdict lines.** Scripts output concise status lines and actionable reasons on non-zero exit.
- **0.11: the closed set and the last line.** `COULD NOT VERIFY` travels with exit 2, the verdict line is the last line on stdout, and it is owed on every path that sets a code, the failure path included; an empty determined change set is 0 and an undetermined one is 2. Open: FileDO's gates print the word `NOT VERIFIED` for exit 2, not the closed-set word; recorded as a dated exception in the catalog registry (SP-0122), the rename is a follow-up across `build.ps1`, `release.ps1`, `packaging/` and `msix/`.
