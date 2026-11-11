# TEST-LAB

| | |
| --- | --- |
| **Id** | `TEST-LAB` |
| **Version** | 0.1, draft (documents, not a payload: no wire, no vector) |
| **Home** | shared contracts catalog, folder `test-lab/`, document `README.md` |
| **Role** | reporter only - this repository files one inventory report and owns no profile; it is listed among the products that report to the Windows profile (`TEST-LAB-WINDOWS`) |
| **Owner** | shared (the catalog owner); the Windows profile belongs to its platform owner, FMS_Win |

## What this repository must do to stay conformant

- **One report, on itself only** (section 3, rule 4): the inventory of this repository's equipment, test practice
  and contract proof is filed in the catalog, status `proposed`, 2026-10-25, covering the program and the site.
  This repository edits no profile and no other product's rows; FMS_Win folds the report into the Windows
  profile.
- **The rules of the collection** (section 4): the report names a source for every item or says `unknown`,
  carries nothing secret and says what was not verified. The dated records in `tests/` quote a disposable disk's
  identifiers, build hashes and host details; none was carried into the report, and none belongs in a later one.
- **Evidence stays private** (`AGENTS.md`, "Private verification artifacts"): the report cites no local
  evidence folder, and a tracked verification summary may record a build, a command, an exit code and a run id.
