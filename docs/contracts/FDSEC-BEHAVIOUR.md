# FDSEC-BEHAVIOUR

| | |
| --- | --- |
| **Id** | `FDSEC-BEHAVIOUR` |
| **Version** | 1.3 (wire carrier: the exit-code classes of section 7.1) |
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
