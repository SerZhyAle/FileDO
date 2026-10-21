# WORKER-IPC

| | |
| --- | --- |
| **Id** | `WORKER-IPC` |
| **Version** | 0.17, draft; read 2026-10-21 (SP-0170), disk-control consumer |
| **Home** | shared contracts catalog, folder `sidecar-control/`, document `worker-ipc.md` |
| **Role** | consumer - `fmsworker/` is FileDO's control client for the FMS worker; the disk requests only |
| **Owner** | FMS Companion. FileDO proposes amendments; it does not edit the owner's protocol |

## What this repository must do to stay conformant

- Use the frozen pipe name with bounded connections and one flat JSON request and response per connection.
- Discover disk-sharing capabilities through schema-1 `GetStatus`; refuse unavailable capabilities before sending schema-2 disk requests. An absent or unknown capability is unavailable.
- Match fields by name, refuse unsupported response schemas and unsuccessful `ok`, and keep credentials out of errors and logs.
- Keep the disk extension aligned with `DISK-SHARE`: six schema-2 request types, explicit `owner`, consent for encrypted autostart, worker-authoritative state and the file event channel for worker invocation of the CLI.
- **A pipe that cannot be reached is an observation, not a finding.** Nothing FileDO says for it claims that FMS is absent, uninstalled or not running, and the holder it shows is `unknown`, never `none`.
- **What 0.15 to 0.17 add does not reach FileDO.** Schema 3 (the resource PIN carrier), the broadcast gateway, `lastStartErrorKind`, `broadcastSupported` and the exchange control are host and worker features; the amendment says the schema-2 disk requests are unaffected. Launch and state-directory migration moved to `WORKER-LAUNCH`, which does not bind FileDO.
- **Item D (0.14), read 2026-10-07 and again 2026-10-21:** FileDO is a named consumer of the disk requests. A disk state the client does not know is read as unknown per record (`DiskState.UnmarshalJSON`), never as an error and never as closed; an unknown or absent holder is unknown, never free (`vdHolderOf`). Open for the owner: item D says unknown never authorizes a mount, while `DISK-SHARE` rule 60 lets a mount go on the container lock alone when the worker cannot be asked - `vdMountHolderGuard` follows rule 60 (dated exception in the registry).
- Prove the client and worker exchange together. Unit fixtures alone do not establish live mount, restart, SFTP, or installed-worker compatibility. The exchange vectors of item A are owed by the catalog and none exist yet.
