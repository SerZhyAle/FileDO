# HARNESS-PROFILE

| | |
| --- | --- |
| **Id** | `HARNESS-PROFILE` |
| **Version** | 0.17, draft (wire 1.0 - `.sza-profile.json` schema for the shared canon harness) |
| **Role** | consumer - root `.sza-profile.json` read by the `sza` plugin tools (`tools/harness/`) |
| **Home** | shared contracts catalog, folder `rule-adoption/`, document `README.md` section 3 |
| **Owner** | sza-unified-rules. Amendments are written in the catalog first, by proposal |

## What this repository must do to stay conformant

- Keep root `.sza-profile.json` valid JSON declaring `"version": "1"` (wire 1.0), `projectName` (`FileDo`),
  `envPrefix` (`SZA`), the spec paths (`PLAN`, `PLAN/done`, `PLAN/PQ_QUEUE.md`, `PLAN/PQ_READY.md`,
  `PLAN/PQ_DONE.md`), the `SP-XXXX` grammar and `probes.enabled: false`.
- Keep `paths.changelog` unset/empty (`ledgerShape: 4` in `.sza-canon.json` derives empty `paths.changelog`
  under rules 2 / 0.11-0.12 because this repo keeps no `dev/CHANGELOG.md` dev-log).
- Keep `commands.compileCheck` absent/empty while FileDO declares no standalone compile-only command for
  unattended passes (0.15 rule 11), inherit shipped `runner.effortByModel` defaults unless overridden (0.16),
  and omit `extraction` as a non-derived source product (0.17).
