# CHECK-BASELINE

| | |
| --- | --- |
| **Id** | `CHECK-BASELINE` |
| **Version** | 0.12, read 2026-10-29 (SP-0174); implemented partial 0.12 - the comparison with the committed copy is open (SP-0179), draft |
| **Role** | producer - quality and baseline tracking |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Accepted debt ratchets.** Baseline files (if introduced) are shrink-only and track known legacy exceptions with open tickets.
- **0.10: absence is recorded.** FileDO's two baselines are `cmd/filedo/vet-baseline.txt` and `docs/help/help-baseline.txt`; a product with no accepted debt records `none` in its adoption row.
