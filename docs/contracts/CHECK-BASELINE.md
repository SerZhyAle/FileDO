# CHECK-BASELINE

| | |
| --- | --- |
| **Id** | `CHECK-BASELINE` |
| **Version** | 0.12, read 2026-11-16 (SP-0221); implemented 0.12 (SP-0179 committed-copy comparison shipped in v2611110217), draft |
| **Role** | producer - quality and baseline tracking |
| **Home** | shared contracts catalog, folder `automated-checks/`, document `README.md` |
| **Owner** | FastMediaSorter Android |

## What this repository must do to stay conformant

- **Accepted debt ratchets.** Baseline files (if introduced) are shrink-only and track known legacy exceptions with open tickets.
- **0.10: absence is recorded.** FileDO's two baselines are `cmd/filedo/vet-baseline.txt` and `docs/help/help-baseline.txt`; a product with no accepted debt records `none` in its adoption row.
