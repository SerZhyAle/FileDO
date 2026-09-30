# FDD-FORMAT

| | |
| --- | --- |
| **Id** | `FDD-FORMAT` |
| **Version** | 0.1 draft (wire carrier: `version_major` / `version_minor` inside the sealed header, value 1.0) |
| **Home** | shared contracts catalog, folder `disk-container/`, document `FDD-FORMAT.md` |
| **Role** | producer and consumer - this repository will be the reference implementation |
| **Owner** | FileDO. Amendments are written in the catalog first |

## What this repository must do to stay conformant

- **`vdisk/` implements it**, unreleased: the reader, the writer, and the key slots of both kinds. A 1.0
  writer keeps one credential per encrypted container (its section 7.3).
- Every offset, width, derivation, refusal rule, commit order and the pepper are the contract's, not the
  code's: a change the catalog does not already say is a violation, even when it is an improvement.
  Source comments cite `FDD-FORMAT.md section N` and nothing more.
- Nothing is pending in the contract (its section 16): the sector and cluster sizing closed on
  2026-09-29 by gate G0, KDF parameter set 1 on 2026-09-27. The contract stays 0.1 draft until the first
  release that carries the feature, and no build that writes containers ships before the feature's gate
  G3 passes.
- `vdisk/testdata/vectors/` is this repository's fixture, byte-identical to the catalog's vectors and
  generated, never hand-edited. `.gitignore` ignores `*.fdd` and re-admits exactly that folder.
- An unknown major version, profile, refused flag bit, KDF parameter set or slot kind is refused **by name**
  as unsupported; a broken structural rule is damage; a wrong credential exists only for an encrypted
  container whose header opened. The three never map to each other.
