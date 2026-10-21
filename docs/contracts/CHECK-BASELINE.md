# CHECK-BASELINE

| | |
| --- | --- |
| **Id** | `CHECK-BASELINE` |
| **Version** | 0.11, read 2026-10-21 (SP-0170); implemented partial 0.11; SP-0171 W8, draft |
| **Role** | producer - quality and baseline tracking |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Accepted debt ratchets.** Baseline files (if introduced) are shrink-only and track known legacy exceptions with open tickets.
- **0.10: absence is recorded.** FileDO's one baseline is `cmd/filedo/vet-baseline.txt`; a product with no accepted debt records `none` in its adoption row.
