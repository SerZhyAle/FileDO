# WORKER-IPC

| | |
| --- | --- |
| **Id** | `WORKER-IPC` |
| **Version** | 0.14, draft; disk-control consumer |
| **Home** | shared contracts catalog, folder `sidecar-control/`, document `worker-ipc.md` |
| **Role** | consumer - `fmsworker/` is FileDO's control client for the FMS worker |
| **Owner** | FMS Companion. FileDO proposes amendments; it does not edit the owner's protocol |

## What this repository must do to stay conformant

- Use the frozen pipe name with bounded connections and one flat JSON request and response per connection.
- Discover disk-sharing capabilities through schema-1 `GetStatus`; refuse unavailable capabilities before sending schema-2 disk requests.
- Match fields by name, refuse unsupported response schemas and unsuccessful `ok`, and keep credentials out of errors and logs.
- Keep the disk extension aligned with `DISK-SHARE`: six schema-2 request types, explicit `owner`, consent for encrypted autostart, worker-authoritative state and the file event channel for worker invocation of the CLI.
- Prove the client and worker exchange together. Unit fixtures alone do not establish live mount, restart, SFTP, or installed-worker compatibility.
- **0.14 item D, read 2026-10-07 (SP-0165):** FileDO is a named consumer of the disk requests. A disk state the client does not know is read as unknown per record (`DiskState.UnmarshalJSON`), never as an error and never as closed; an unknown or absent holder is unknown, never free (`vdHolderOf`). Open for the owner: item D says unknown never authorizes a mount, while `DISK-SHARE` rule 60 lets a mount go on the container lock alone when the worker cannot be asked - `vdMountHolderGuard` follows rule 60. The exchange vectors of item A are owed by the catalog and none exist yet.
