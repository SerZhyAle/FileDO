# FDSEC-BEHAVIOUR

| | |
| --- | --- |
| **Id** | `FDSEC-BEHAVIOUR` |
| **Version** | 1.7, read 2026-10-21 (SP-0171); implemented partial 1.7 (section 18 except an interactive replacement-name offer; section 19, the closed token; section 4.3, the reserved stem and valid UTF-8; 2026-10-21) (wire carrier: the exit-code classes of section 7.1) |
| **Home** | shared contracts catalog, folder `secure-container/`, document `FD-SEC-CONTRACT.md` |
| **Role** | producer and consumer - this repository is the reference implementation |
| **Owner** | FileDO. Amendments are written in the catalog first |

## What this repository must do to stay conformant

- **The original survives until the copy is proven.** Nothing is deleted, moved or truncated before the
  container has been written, flushed, closed, reopened and read back (`fdsec/file.go`).
- **Write to a temporary name, rename into place**, so an interrupt leaves an obviously partial file and
  never a container that looks complete.
- **Three outcomes never merge**: wrong credential / not a container / tampering, damaged, unsupported.
  Exit codes 3 and 4 stay distinct in every surface and every message.
- **The exit codes are 0, 2, 3, 4, 5, 6** as section 7.1 lists them (`cmd/filedo/fdsec_cmd.go`). They are
  a different vocabulary from the supervisor codes of `CLI-EVENT-STREAM` rule 11; see that pointer.
- **A force flag skips the prompt, never the checks.**
- **The credential and the sealed true name never reach a log**, a history file, a window title, a
  temporary file or the console (`cmd/filedo/fdsec_redact.go`).
- `go test ./fdsec/` covers the section 12.2 checklist. The interoperability claim of 12.3 may be stated
  only after a fresh run with its output cited.
- **Suite 2 is chosen, never defaulted** (SP-0019 D2): `secure suite2` writes it, and every surface that offers it says
  that only FileDO reads it and that it keeps no original timestamps (section 11.4).
- **1.5 (additive):** the contract lists an optional sixth credential source, "Remembered on this device". FileDO does not offer it; no byte, exit class or reader changes, so nothing here moves.
- **A name the destination cannot create (1.6 section 18):** the restore stops before anything is written, with exit class 5 and one sentence that does not print the sealed name, on unsecure, `unsecure start`, reveal (also `-rw`), a folder container's root and its entries. The original container is untouched; `rename` and an exact `to <path>` are the ways out and re-run the collision rule without overwriting. There is no interactive offer of a replacement name (dated exception in the registry).
- **That refusal carries a closed token (1.7 section 19):** after the error line, one stderr line, exactly `reason=destination-name`, and `details.reason` = `destination-name` on the error finding (`CLI-EVENT-STREAM` rule 17). Exit code 5 is unchanged and the token never accompanies a damage (4) or any other class. It holds no name, path or credential.
- **A sealed name must be valid UTF-8 and its stem may not be a device name (1.7 section 4.3):** `fdsec.ValidateName` refuses both as damage in every suite, so a restore never creates a file under a name with U+FFFD in it.
