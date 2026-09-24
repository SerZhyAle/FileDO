# FDSEC-FORMAT

| | |
| --- | --- |
| **Id** | `FDSEC-FORMAT` |
| **Version** | 1.1 (wire carrier: format version byte 1, suite id 1) |
| **Home** | shared contracts catalog, folder `secure-container/`, document `FDSEC-FORMAT.md` |
| **Role** | producer and consumer - this repository is the reference implementation |
| **Owner** | FileDO. Amendments are written in the catalog first |

## What this repository must do to stay conformant

- The format core is the root package `fdsec/`. Every offset, width, derivation and refusal rule is the
  contract's, not this code's: a change here that the catalog does not already say is a violation, even
  when it is an improvement.
- `fdsec/testdata/vectors/` is this repository's fixture and must stay byte-identical to `vectors/` in the
  catalog folder. Regenerate with `FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite1`, then
  copy across - never hand-edit either copy.
- `go test ./fdsec/ -run TestVectors_Suite1` is the conformance gate. It must pass against the catalog's
  vectors, not only against the local fixture.
- Unknown version, suite, flag bit or key-slot type is refused **by name** as unsupported after slot 0
  authenticates; a broken structural rule is damage. The two never map to each other.
- Source comments cite `FDSEC-FORMAT.md section N` and nothing more.
