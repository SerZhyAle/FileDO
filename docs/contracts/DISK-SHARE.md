# DISK-SHARE

| | |
| --- | --- |
| **Id** | `DISK-SHARE` |
| **Version** | 1.4 draft |
| **Home** | shared contracts catalog, folder `disk-sharing/`, document `DISK-SHARE.md` |
| **Role** | producer and consumer - FileDO and Fast Media Sorter integration |
| **Owner** | FileDO. Amendments are written in the catalog first and agreed with affected consumers |

## What this repository must do to stay conformant

- **Commands and IPC:** `filedo.exe` provides `filedo vd share`, `vd autostart`, `vd open` and `vd close` commands, and communicates with the FMS worker over named pipe `\\.\pipe\fms-companion` using Schema 2 disk requests.
- **Pipe-server identity:** before it writes, the client establishes that the pipe is served by the FMS service process or by a process of the current account, and a request carrying a password also needs the server image to be the FMS worker; response size and record count are bounded (section 18).
- **Holder at the mount:** a FileDO mount asks the worker who holds the container and refuses a held one; the status shown to the user is the worker's live answer, or `unknown` - never `none` (section 18).
- **Worker invocation:** use the `vd` verb-first grammar and the private `--events` JSON Lines channel specified by CLI-EVENT-STREAM; stdout prose is not the event channel.
- **Holder isolation:** A disk is held by at most one holder at a time (FileDO or FMS Service/Session). Concurrent mounts are refused.
- **No-letter mounting:** Supports V3 folder mount points under protected ACL directories with V2 volume GUID path fallback.
- **Bounded drain:** Supports bounded drain logic before clean container unmount and close.
- **Security:** Credentials are provided via stdin / environment / DPAPI stored keys (per-disk autostart opt-in only) and are never logged or passed as command-line arguments.
- **Release proof:** disclose paired-device disk serving and autostart key storage in the privacy and permission surfaces, and prove real mount, SFTP parity, reboot and update-over-prior behavior jointly with FMS. Adoption is partial until those checks and disclosures are complete.
