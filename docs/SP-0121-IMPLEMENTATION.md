# SP-0121 selected tasks: implementation evidence

Date: 2026-10-02 (revised after an independent review the same day). Scope: TS-0121-01-10
through 16, TS-0121-02-01 through 03, and TS-0121-04-01 through 07. No release or
installation is part of this change.

| Tasks | Implementation | Verification |
| --- | --- | --- |
| 01-10, 01-11, 04-01 | `cmd/filedo/vdisk_share.go`, `vdisk_autostart.go` | CLI tests with a per-call failing stub: worker refusals, exit classes, path validation, collisions, read-only, consent, snapshot self-repair, key-deletion verification through `GetDiskStatus`, password asked once |
| 01-12, 01-13, 04-02 | `fmsworker/client.go` | Exchange tests on `net.Pipe` plus a test on a real `winio` named pipe; bounded dial stage, context errors preserved, `WorkerError` with `OutcomeClass`, request JSON for every disk operation |
| 01-14, 04-03 | `vdisk/shared_state.go` | Persistence, immutable snapshots, concurrent handles, holder conflicts, `ReconcileAll` (worker list is the truth), rollback on write failure, ephemeral fields never persisted |
| 01-15, 04-04 | `vdisk/mount_no_letter_windows.go`, native attach/detach | Windows API seam tests (arguments and order recorded), exact protected DACL on a real temp directory, trusted owners (user, SYSTEM, Administrators), idempotent cleanup, stale-directory sweep. A real volume mount point is NOT covered |
| 01-16, 04-05 | `fmsworker/drain.go` | Client-side model of the drain contract: bound 1..90 s (30 default, anything else rejected), forced close is reported as `Forced` and never as a clean drain, context expiry counts as a timeout. It is not a handle counter and is not called from production code: the authoritative drain runs in the worker |
| 02-01 | FMS_Lite `tools/Stage-FileDoPayload.ps1`, build scripts and both Inno scripts | Archive and executable SHA-256 verified; the executable is also cross-checked against `binarySha256` in `release.json`; both installer scripts compiled with a fixture payload |
| 02-02, 04-06 | fms_companion `internal/diskshare` | Hash/path refusals, real child process, stdin credentials, machine events, exit classes, real DPAPI (user scope), lifecycle and autostart tests; the capability probe is exercised against a fake child and, on demand, a real `filedo.exe` (`FMS_REAL_FILEDO`) |
| 02-03 | fms_companion `internal/ipc` | Real named-pipe schema-1 discovery and schema-2 disk tests; unsupported schemas and non-disk types never reach handlers |
| 04-07 | fms_companion `internal/sftpserver` | Volume GUID validation, ownership, unavailable-folder parity for every disk state, junction/symlink containment for every operation, trailing-separator roots, Windows name traps (streams, reserved names, trailing dots) |

The handler and SFTP implementations are necessary dependencies of 04-06/07; they live in
`P:\WINDOWS\fms_companion`. That directory has no Git repository. The desktop packaging
edits live in `P:\WINDOWS\FastMediaSorter_Lite`.

## Review corrections

An independent review of every task found defects that the first version of this note did
not mention. They are fixed in the working tree, except where a section below says otherwise.

- The capability probe in the worker accepted only exit class 4 and then only verdict
  `Failed`. A real filedo reports a missing file as class 5 with verdict `Not proven`, so
  disk sharing could never have turned on. Fixed and checked against a real build.
- The SFTP containment check let a junction inside a root be used to create files and
  directories outside it, and a root ending in a backslash (`C:\`, `\\?\Volume{GUID}\`)
  refused every deeper path, which broke the volume-path fallback entirely.
- The first CLI tests were partly vacuous (the failing stub broke an earlier call), so
  worker refusals and key deletion were not actually covered. Rewritten.
- The no-letter manager could silently fall back to the unprotected volume path on the second
  run for an elevated owner; the reason of any fallback is now recorded and logged.
- `DrainManager` is not wired into production; this note previously implied it was.

## Verification

Fresh runs on 2026-10-02 (Go 1.26, windows/386, no `-race`: the race detector needs cgo):

- FileDO: `go build ./...`; `go test ./cmd/filedo ./vdisk ./fmsworker -count=1`;
  `go vet ./vdisk ./fmsworker` clean. `go vet ./cmd/filedo` reports one older finding at
  `speedtest_unbuffered_windows.go:42` (unsafe.Pointer) that this change does not touch.
- Worker: `go build ./...`, `go vet ./...` and `go test ./... -count=1` pass.
- Capability probe against a real `filedo.exe` built from this tree: exit class 5, one
  schema-1 result event, verdict `Not proven`; the probe test passes with it.
- Payload staging: the pinned download path, tampered-executable and missing-metadata cases
  were exercised on temp copies; PowerShell parsing passes.
- Desktop: the .NET suite (303 tests) and the Inno compiles were run before the review and
  were not re-run after it; the review did not touch those paths.

## Deployment boundaries

The pinned published FileDO is v2609260512, which predates these commands. The worker
advertises disk-sharing only after both checksum verification and the parser capability probe.
Refresh the payload after a release containing this implementation; a development build
must never be labelled as that published release.

The existing Server installer runs LocalService. SP-0121's machine-key holder is LocalSystem;
this change does not switch the installed account, so on the Server edition disk sharing
stays unavailable until that owner decision is made (it also lifts the ticket 054 H-14
restriction). Machine credential storage refuses other accounts, and unavailable support is
reported in worker status. The startup-only `--filedo-root` option names the installer-owned
Program Files bundle for a staged worker; IPC cannot select an executable.

## Residual risks and open items

- No impersonation: the service opens a container with its own account. Only UNC, device,
  stream and non-fixed-drive paths are refused, and identity (path, container id,
  encryption flag) is re-verified at open. Guarantee 8 is therefore not fully enforced.
- `diskshares.json` is writable by the management user under the installer ACL. Records are
  validated at load and invalid ones are ignored and logged, but the file should be
  restricted to SYSTEM and Administrators in the installer.
- The check of a container and its use are separate steps (TOCTOU); closing that needs
  FileDO to take the file identity as an argument.
- Worker failures that are not FileDO outcomes carry no outcome class (0) instead of an
  invented one. A dedicated code would be a contract change.
- `worker-ipc.md` must record: schema 2 admits only disk types and `GetStatus`; the
  "disk sharing is starting" status text; `outcomeClass` absent on worker-side failures.
- The autostart password is not validated by a trial mount; a stored key that no longer
  matches (container password changed) is destroyed on the first failed open.
- The worker can start without disk sharing and enables it once the probe and autostart
  restore finish; transient class-7 failures at boot are retried for a bounded time.

## Release gates still open

Live disk attachment, a real volume mount point as an SFTP root (V3), Explorer invisibility,
Session 0 and reboot behaviour, SYSTEM-scope DPAPI, an elevated run of the no-letter owner
tests, the stale mount-point sweep on a detached test VHD, and the phone regression remain
manual gates. Tests use isolated state and pipe names; they do not install services, mount
user disks or change Explorer registration. A drain timeout refuses close unless force was
explicitly requested. Drain bounds are 30 seconds by default and accept 1 through 90 seconds
(enforced in `fmsworker/drain.go` and, for the real drain, in the worker).

The canonical contracts are `P:\Contracts\disk-sharing\DISK-SHARE.md` (1.1 draft)
and `P:\Contracts\sidecar-control\worker-ipc.md` (0.10 draft). Disk requests use
the existing flat envelope with schemaVersion 2; old host calls keep schemaVersion 1.
