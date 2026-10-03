# FDD-BEHAVIOUR

| | |
| --- | --- |
| **Id** | `FDD-BEHAVIOUR` |
| **Version** | 0.2 draft (wire carrier: the exit-code classes 0 and 2-8 of its section 4) |
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
- **No-letter mount modes (V3 / V2)**: When mounting without a drive letter, the mount point is created in a directory whose ACL admits SYSTEM or the session worker only (V3), with volume GUID path fallback (V2). Surfaces describe this as "hidden from this PC's desktop", never "inaccessible on this PC".
- **Bounded drain on close (U2)**: A shared disk transitions to closing, rejects new open requests with standard unavailable status, waits for in-flight handles up to the bounded timeout, flushes dirty RAM buffers, and marks the container cleanly closed.
- **Holder rules (Guarantee 10)**: Exactly one holder per disk at any given time (`none`, `FileDO`, `FMS Service`, `FMS Session`). FileDO CLI and GUI refuse concurrent mounts when held by another holder.
- **Older copy compatibility (A12)**: Newer container versions produce warnings rather than immediate hard failures when feasible, falling back to exit class 6 (unsupported) only when an operation cannot proceed.
- Credentials use the one shared parser and the one redaction list; no second grammar and no second copy.
- **Partition carriers** (its sections 3 rule 7, 6 item 10, 8 rule 5, 8.1, 8.2): the device open of the
  read path is the only elevated step and `vd image` (image to file) is its escape route; a partition is
  created only in unallocated space through the Storage Management API with whole-list re-validation, and
  deleted only when its type and container id prove it FileDO's; the busy source is the server's share-0
  partition handle; identity is the locator `fdpart:{GUID}` resolved every time, never a disk number. The
  Microsoft Store build refuses every partition verb with the unsupported class.
