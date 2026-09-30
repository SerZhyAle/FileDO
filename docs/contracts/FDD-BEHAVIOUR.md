# FDD-BEHAVIOUR

| | |
| --- | --- |
| **Id** | `FDD-BEHAVIOUR` |
| **Version** | 0.1 draft (wire carrier: the exit-code classes 0 and 2-8 of its section 4) |
| **Home** | shared contracts catalog, folder `disk-container/`, document `FDD-BEHAVIOUR.md` |
| **Role** | producer and consumer - this repository will be the reference implementation |
| **Owner** | FileDO. Amendments are written in the catalog first |

## What this repository must do to stay conformant

- **`vdisk/` and `filedo vd` implement it**, unreleased: the read path, the writer, the mount path and
  exit classes 7 and 8.
- **The read path needs nothing but the file** - no mount, driver, socket or elevation - and never writes
  to the container. It runs in every build that ships `filedo.exe`, the packaged one included.
- **Obfuscated is never called encrypted.** Which one a container is comes from the header's flag, never
  from the profile, and every surface says which before it attaches or extracts anything.
  `cmd/filedo/vdisk_surfaces_test.go` holds the written surfaces to it - the five READMEs, the site guide
  and landing page, the Store listing sources and the shell's five locale tables - along with the platform
  limits of its section 9 (Windows only, mounting elevated, no mount in the Store build).
- **Exit classes 2-6 keep the digits of `FDSEC-BEHAVIOUR`**, and 7 (transport unavailable) and 8 (busy) are
  added through the one decision point in `cmd/filedo/outcome.go`, never beside it. A batch keeps 0/1/2.
- **A container not closed cleanly is reported, with its times, before it is used**, and never repaired.
- Credentials use the one shared parser and the one redaction list; no second grammar and no second copy.
