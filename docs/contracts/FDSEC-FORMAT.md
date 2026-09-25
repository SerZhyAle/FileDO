# FDSEC-FORMAT

| | |
| --- | --- |
| **Id** | `FDSEC-FORMAT` |
| **Version** | 1.3 (wire carrier: format version byte 1, suite id 1 - one file - or 3 - one directory tree; suite 2 - one file, no head - is found by opening, section 13.4) |
| **Home** | shared contracts catalog, folder `secure-container/`, document `FDSEC-FORMAT.md` |
| **Role** | producer and consumer - this repository is the reference implementation |
| **Owner** | FileDO. Amendments are written in the catalog first |

## What this repository must do to stay conformant

- The format core is the root package `fdsec/`. Every offset, width, derivation and refusal rule is the
  contract's, not this code's: a change here that the catalog does not already say is a violation, even
  when it is an improvement.
- `fdsec/testdata/vectors/` (suite 1), `fdsec/testdata/vectors/suite3/` (suite 3) and `fdsec/testdata/vectors/suite2/` (suite 2) are this repository's fixture and must stay byte-identical to `vectors/` in the
  catalog folder. Regenerate with `FDSEC_WRITE_VECTORS=1 go test ./fdsec -run "TestVectors_Suite1|TestVectors_Suite2|TestVectors_Suite3"`, then
  copy across - never hand-edit either copy.
- `go test ./fdsec/ -run "TestVectors_Suite1|TestVectors_Suite2|TestVectors_Suite3|TestSuite1_StillOpens"` is the conformance gate. It must pass against the catalog's
  vectors, not only against the local fixture.
- Unknown version, suite, flag bit or key-slot type is refused **by name** as unsupported after slot 0
  authenticates; a broken structural rule is damage. The two never map to each other.
- Source comments cite `FDSEC-FORMAT.md section N` and nothing more.
- `fdsec.Open` is the one dispatch (section 13.4): the suite-1/3 head first, suite 2's try-list only when the head does
  not authenticate. A refusal after the head authenticated never falls through to suite 2, and neither the
  suite-2 pepper nor a pinned try-list entry ever changes - `TestVectors_Suite2` fails loudly if either drifts.
