# RULE-DELIVERY

| | |
| --- | --- |
| **Id** | `RULE-DELIVERY` |
| **Version** | 0.9, draft (wire carrier: the `sza` plugin version, derived from `CANON_VERSION`) |
| **Home** | shared contracts catalog, folder `rule-adoption/`, document `README.md` section 5 |
| **Role** | consumer - this repository receives the canon through the `sza` plugin |
| **Owner** | sza-unified-rules. Amendments are written in the catalog first, by proposal |

## What this repository must do to stay conformant

- **Declare the rule set by version and digest together**: `canon.version` and `canon.coreDigest` in
  `.sza-canon.json`, both from the installed plugin (rule 4).
- **Stay on the warning rung at worst.** A digest that differs from the installed canon owes a
  reconciliation of the changed rule documents and a re-stamp; one older than 180 days owes a full
  re-adoption (rule 5). Being behind is legal; being behind silently is not.
- **Never declare a version ahead of the published canon** - that is corruption, not staleness (rule 6).
- **Never hand-edit the version pair.** The canon's deploy path writes it; this repository only records
  what it holds (rule 7).
