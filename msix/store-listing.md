# FileDO - Store listing copy

Paste these into Partner Center. Tweak freely; the runFullTrust justification has a
~1000-char limit (the version below is well under it).

Repo: https://github.com/SerZhyAle/FileDO · Contact: serzhyale@gmail.com · License: MIT

---

## Description

```
FileDO is a fast command-line tool for advanced file and storage operations on Windows 10/11. You drive it from a terminal: after install, just type `filedo`.

It tests real read/write speed of disks, folders, and network shares; detects fake-capacity (counterfeit) USB flash drives and SD cards by writing and verifying data across the whole device; securely wipes free space or whole folders at high speed to prevent recovery; copies large trees with live progress, ETA, and resilient handling of slow or damaged disks; and finds duplicate files using MD5 hashing with a reuse cache.

It runs entirely on your device. FileDO makes no network connections, contains no telemetry, ads, or accounts, and collects no personal data. It is open source: https://github.com/SerZhyAle/FileDO

Note: FileDO needs full-trust desktop access to read and write raw disk/device data - required for capacity testing, speed benchmarking, and secure wiping. The wipe and fill operations destroy data by design and always confirm before touching drive roots or system folders.
```

## Product features (one per line, ≤200 chars each)

```
Real read/write speed testing for disks, folders, and network shares, with detailed timing
Fake-capacity detection: writes and verifies data across the whole device to expose counterfeit USB/SD drives
High-speed secure wiping of free space or whole folders to prevent data recovery
Resilient copy of large trees with live progress, ETA, metadata preservation, and damaged-disk handling
Duplicate finder using MD5 hashing with a reuse cache; delete or move duplicates by oldest/newest/name
Confirmation guardrails: wipe always prompts, and drive roots, junctions, and system TEMP are never auto-forced
Runs from a terminal, no install footprint beyond the package, nothing extra required
Open source - no telemetry, no network, no data collection
```

## runFullTrust justification (keep under ~1000 chars)

```
FileDO is a full-trust Win32 console application (Go), not a UWP app, so runFullTrust is required to run as a normal desktop process and to call the Win32 storage APIs its core features depend on:
- Direct device/volume access (CreateFile on \\.\PhysicalDrive and volume handles, SetFilePointer, raw read/write): needed to measure true disk speed and to detect fake-capacity drives by writing and reading back data across the full device. It accesses storage the user explicitly targets; it does not scan the system or read personal files on its own.
- High-throughput file I/O for secure wipe/fill: overwrites free space or user-specified folders to prevent recovery. Destructive actions confirm first and never auto-force drive roots, reparse points, or system TEMP.
These APIs are available only to full-trust desktop apps. FileDO runs entirely locally, makes no network connections, and collects no user data. Open source: https://github.com/SerZhyAle/FileDO
```

### Short variant (if a brief reason is also requested)

```
Full-trust Win32 console app. Needs runFullTrust for raw disk/volume access (CreateFile on \\.\PhysicalDrive, raw read/write) used for speed testing, fake-capacity detection, and secure wiping - APIs only available to full-trust desktop apps. Runs locally, no network, no data collection. https://github.com/SerZhyAle/FileDO
```

## Privacy policy (host as a page, e.g. GitHub Pages; key points)

```
FileDO does not collect, store, log, or transmit any personal data. It runs entirely on
your device, has no servers, makes no network requests, and contains no
telemetry/analytics/ads/accounts.

What it accesses and why:
- Raw disk/device data (read and write) on the drives, folders, or shares you target -
  used only for speed testing, fake-capacity detection, secure wiping, and copying.
- The wipe and fill features intentionally overwrite/destroy data you point them at; they
  confirm before acting and never silently force drive roots, junctions, or system TEMP.

Local files it writes (these never leave your device):
- history.json - a log of operations, written to the current working directory.
- hash_cache.json - cached file hashes to speed up duplicate scans, stored next to the app.

Data sharing: none. Children: no data collected. Open source: https://github.com/SerZhyAle/FileDO
Contact: serzhyale@gmail.com
```
