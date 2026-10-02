# SP-0121 selected tasks: implementation evidence

Date: 2026-10-02. Scope: TS-0121-01-10 through 16, TS-0121-02-01 through 03,
and TS-0121-04-01 through 07. No release or installation is part of this change.

| Tasks | Implementation | Verification |
| --- | --- | --- |
| 01-10, 01-11, 04-01 | `cmd/filedo/vdisk_share.go`, `vdisk_autostart.go` | CLI tests cover path validation, collisions, worker refusals, read-only, consent, snapshots and key deletion acknowledgment |
| 01-12, 01-13, 04-02 | `fmsworker/client.go` | Exchange tests cover schema, deadlines, retries, access denied, discovery and capability refusal |
| 01-14, 04-03 | `vdisk/shared_state.go` | Persistence, immutable snapshots, concurrent handles, holder conflicts and key-backend failures |
| 01-15, 04-04 | `vdisk/mount_no_letter_windows.go`, native attach/detach | Windows API seam tests cover V3, verified V2 fallback, cleanup and a real protected DACL |
| 01-16, 04-05 | `fmsworker/drain.go` | Tests cover live counts, cancellation, query failure, bounded drain and explicit force |
| 02-01 | FMS_Lite `tools/Stage-FileDoPayload.ps1`, build scripts and both Inno scripts | Verified published archive and executable SHA-256; both installer scripts compiled with a fixture payload |
| 02-02, 04-06 | fms_companion `internal/diskshare` | Hash/path refusals, real child process, stdin credentials, machine events, exit classes, real DPAPI, lifecycle and autostart tests |
| 02-03 | fms_companion `internal/ipc` | Real named-pipe schema-1 discovery/schema-2 disk tests; unsupported schemas never reach handlers |
| 04-07 | fms_companion `internal/sftpserver` | Volume GUID validation, directory paths, ownership, unavailable-folder parity, read/write and actual handle draining |

The handler and SFTP implementations are necessary dependencies of 04-06/07;
they live in `P:\WINDOWS\fms_companion`. This directory currently has no Git repository.
The desktop packaging edits live in `P:\WINDOWS\FastMediaSorter_Lite`.

## Verification

- FileDO: `go test ./fmsworker ./vdisk ./cmd/filedo` passed; `go test ./... -run '^$'` compiles every main-module package, including the existing joint regression harness.
- Worker: `go test ./...` and `go vet ./...` passed. Additional folder/disk ownership tests are part of this suite. Disk handler/native bundle package coverage: 81.9%.
- Desktop: `dotnet test tests/Companion.Tests/Companion.Tests.vbproj --no-restore`
  passed: 303 tests; one pre-existing response-vector test skipped.
- PowerShell parsing and payload staging passed.
- Inno Setup: both scripts compiled using version 0.0.0.0 and disposable fixture
  executables. These artifacts are syntax/packaging checks and are not installable product builds.

## Deployment boundaries

The pinned published FileDO is v2609260512, which predates these commands. The worker
advertises disk-sharing only after both checksum verification and a parser capability probe.
Refresh the payload after a release containing this implementation; a development build
must never be labelled as that published release.

The existing Server installer runs LocalService. SP-0121's machine-key holder is LocalSystem;
this change does not switch the installed account. Machine credential storage refuses other
accounts, and unavailable support is reported in worker status. The startup-only
`--filedo-root` option names the installer-owned Program Files bundle for a staged worker;
IPC cannot select an executable. Service-account migration is a separate unresolved decision.

Live disk attachment, Session 0, reboot and phone regression remain release gates.
Tests use isolated state and pipe names; they do not install services, mount user disks or
change Explorer registration. A drain timeout refuses close unless force was explicitly requested.
Drain bounds are 30 seconds by default and accept 1 through 90 seconds.

The canonical contracts are `P:\Contracts\disk-sharing\DISK-SHARE.md` (1.1 draft)
and `P:\Contracts\sidecar-control\worker-ipc.md` (0.10 draft). Disk requests use
the existing flat envelope with schemaVersion 2; old host calls keep schemaVersion 1.
