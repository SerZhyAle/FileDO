# WORKER-IPC

| | |
| --- | --- |
| **Id** | `WORKER-IPC` |
| **Version** | 0.10, draft; disk-control consumer |
| **Home** | shared contracts catalog, folder `sidecar-control/`, document `worker-ipc.md` |
| **Role** | consumer - `fmsworker/` is FileDO's control client for the FMS worker |
| **Owner** | FMS Companion. FileDO proposes amendments; it does not edit the owner's protocol |

## What this repository must do to stay conformant

- Use the frozen pipe name with bounded connections and one flat JSON request and response per connection.
- Discover disk-sharing capabilities through schema-1 `GetStatus`; refuse unavailable capabilities before sending schema-2 disk requests.
- Match fields by name, refuse unsupported response schemas and unsuccessful `ok`, and keep credentials out of errors and logs.
- Keep the disk extension aligned with `DISK-SHARE`: six schema-2 request types, explicit `owner`, consent for encrypted autostart, worker-authoritative state and the file event channel for worker invocation of the CLI.
- Prove the client and worker exchange together. Unit fixtures alone do not establish live mount, restart, SFTP, or installed-worker compatibility.
