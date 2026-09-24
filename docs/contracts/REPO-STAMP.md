# REPO-STAMP

| | |
| --- | --- |
| **Id** | `REPO-STAMP` |
| **Version** | 0.9, draft (no wire carrier: the stamp has no version field of its own, rule 9) |
| **Home** | shared contracts catalog, folder `rule-adoption/`, document `README.md` section 2 |
| **Role** | producer - this repository writes its own stamp, `.sza-canon.json`; nothing here reads it |
| **Owner** | sza-unified-rules. Amendments are written in the catalog first, by proposal |

## What this repository must do to stay conformant

- **One file at the root, valid JSON.** `.sza-canon.json` and no other name (rules 1-2).
- **The six required keys stay present**: `canon`, `overlay`, `ledgerShape`, and inside `canon`:
  `version`, `coreDigest`, `model` (rule 3).
- **Every optional key is written explicitly**, never left to a default - `role`, `shapes`, `channels`,
  `site`, `privacy`, `exemptions` and the rest (rule 4). `role` stays `product` (rule 5).
- **An exemption carries a check id and a reason**, and an `until` date when it is temporary (rule 6).
- **The stamp is written by the canon's adoption run, never by hand.** `canon.coreDigest` is recomputed
  from the installed rule set on every re-sync, never copied from another repository (rule 8).
- **Additive only.** The stamp has no carrier, so a rewrite never removes or renames a key it does not
  own - `$comment` included (rule 9, compatibility law rule 5).
