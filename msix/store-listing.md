# FileDO - Store listing (what is NOT in the CSV)

The listing copy - Title, short description, Description, Product features, search terms and
"What's new" - lives in `msix/listing/<code>.txt` (en, ru, uk, de, fr) and reaches Partner Center
through the export, merge, import round trip of `msix/build-store-listing-csv.ps1` (`msix/README.md`,
section 4). Edit it THERE and never in the console: the console is a render target, and two
sources of truth is how a corrected claim survives in a live listing.

This file keeps only what the CSV does not carry and a person pastes or ticks by hand: the
Properties page, the `runFullTrust` justification, the export-compliance answer and the privacy
declaration. The justification has a ~1000-char limit (both versions below are well under it).

Repo: https://github.com/SerZhyAle/FileDO - Contact: serzhyale@gmail.com - License: MIT

---
## Properties page (Partner Center > Product > Properties)

Every field of that page, in the order the console shows them. Nothing here is generated; a person
types it, which is why it is written down once instead of being decided again at each submission.

| Field | Value |
| --- | --- |
| Category | `Utilities + tools` |
| Subcategory | `Backup + manage` |
| Secondary category (optional) | `Security` |
| Does this product access, collect, or transmit personal information? | **Yes** |
| Privacy policy URL | `https://serzhyale.github.io/FileDO/privacy.html` |
| Support info > Website | `https://serzhyale.github.io/FileDO/` |
| Support info > Support contact info | `serzhyale@gmail.com` |
| Support info > Phone, Address, Postal code, City, State, Country | leave empty (optional, and a private address is not listing material) |

**Category.** `Utilities + tools` has exactly two subcategories, `Backup + manage` and
`File managers` ([the Store's own table][cat]). FileDO is not a file manager - it browses nothing -
so `Backup + manage` is the honest one. `Security` as the secondary category is earned by the secure
wipe and the password-protected `.fd-sec` container, not bought for reach.

**The personal-information answer is Yes, deliberately**, and it does not contradict the "no data
collected" declaration further down. The question asks what the product *accesses*, not what its
author receives: FileDO reads and writes raw device data on drives the user points it at, reads drive
serial numbers through WMI, and writes `history.json`, which records full command lines and target
paths. It transmits none of it. Answering No would also make the privacy policy URL optional, and
that URL is a gate this repository checks before every submission (`msix\test-store-package.ps1`,
SP-0007 C6).

[cat]: https://learn.microsoft.com/en-us/windows/apps/publish/publish-your-app/msix/categories-and-subcategories

---
## runFullTrust justification (keep under ~1000 chars)

```
FileDO is a full-trust Win32 desktop application, not a UWP app: a Go command-line engine (filedo.exe) and a .NET Framework window (filedo_win.exe) that starts it. runFullTrust is required to run as a normal desktop process and to call the Win32 storage APIs its core features depend on:
- Direct device/volume access (CreateFile on \\.\PhysicalDrive and volume handles, SetFilePointer, raw read/write): needed to measure true disk speed and to detect fake-capacity drives by writing and reading back data across the full device. It accesses storage the user explicitly targets; it does not scan the system or read personal files on its own.
- High-throughput file I/O for secure wipe/fill: overwrites free space or user-specified folders to prevent recovery. Destructive actions confirm first and never auto-force drive roots, reparse points, or system TEMP.
These APIs are available only to full-trust desktop apps. FileDO runs entirely locally, makes no network connections, and collects no user data. Open source: https://github.com/SerZhyAle/FileDO
```

### Short variant (if a brief reason is also requested)

```
Full-trust Win32 desktop app (Go command-line engine plus a .NET Framework window). Needs runFullTrust for raw disk/volume access (CreateFile on \\.\PhysicalDrive, raw read/write) used for speed testing, fake-capacity detection, and secure wiping - APIs only available to full-trust desktop apps. Runs locally, no network, no data collection. https://github.com/SerZhyAle/FileDO
```

## Export compliance - encryption (answer this BEFORE the first submission carrying secret files)

Partner Center asks, under Product declarations, whether the product calls, supports, contains
or uses cryptography. Until the `.fd-sec` feature shipped, the honest answer was no. It is now
**yes**, and the answer below is what goes in. This is an engineer's reading of the question,
not legal advice: the owner confirms it before pressing Submit, and this section is why the
submission cannot be the first place the question is read.

```
Yes. FileDO uses cryptography for one feature: packing a single user-chosen file into a
password-protected .fd-sec container and opening it again.

It uses standard published algorithms only, from a permissively licensed, widely distributed
library (the Go project's golang.org/x/crypto). It implements no cryptographic algorithm of
its own, provides no cryptographic service to other programs, and has no networking code of
any kind, so nothing is encrypted in transit. The container format is published in full, with
test vectors, so the data can be read without FileDO.

This is the ordinary ancillary use of published algorithms in a mass-market tool, not a
cryptographic product.
```

**Reach must not shrink.** The canon forbids a release that takes countries, an age rating or
a platform floor away from the previous one (INVARIANTS 2). The declaration above is made on
the way in, so it changes no country in the current listing; if Partner Center answers it by
restricting availability anywhere, the submission is stopped and the question comes back to
the owner rather than being resolved by accepting a smaller reach.

**What the Store build does NOT carry.** No Explorer entries and no `.fd-sec` association: a
packaged build can only declare a context-menu handler through a signed shell command handler,
which is not built (SP-0005 9.3). The verbs work from the terminal in the Store build exactly
as everywhere else. Say that plainly rather than implying parity.

## Privacy policy

**Privacy policy URL** (paste into Partner Center ▸ Properties ▸ Privacy policy URL):

```
https://serzhyale.github.io/FileDO/privacy.html
```

The hosted page (`docs/privacy.html`) is the single source of truth. The Partner Center
data-collection form and the summary below are *rendered from it* - if the page changes,
re-derive them; never edit the summary on its own.

Partner Center data collection: declare **no** data collection (the app has no networking
code at all and the package declares no `internetClient` capability).

```
FileDO does not collect, store, or transmit any personal data. It runs entirely on your
device, has no servers, makes no network requests, and contains no
telemetry/analytics/ads/accounts.

What it accesses and why:
- Raw disk/device data (read and write) on the drives, folders, or shares you target -
  used only for speed testing, fake-capacity detection, secure wiping, and copying.
- Drive model, serial number, and interface type, read via WMI for the `info` command and
  shown on screen only - never logged to file, never transmitted.
- The wipe and fill features intentionally overwrite/destroy data you point them at; they
  confirm before acting and never silently force drive roots, junctions, or system TEMP.

Local files it writes (they stay on your device unless you mail them yourself - see the last
item below):
- history.json - a log of operations (time, command, target path, full command line,
  parameters, results), written to the current working directory, last 1000 entries.
  Disable per run with the `nohist` (or `no_history`) flag.
- hash_cache.json - cached file paths, sizes, timestamps, and MD5 hashes that speed up
  duplicate scans, stored next to the exe (redirected into the package's per-user
  LocalCache under MSIX). No file contents are stored.
- %LOCALAPPDATA%\FileDO\runs and \reports - GUI event stream files for live UI tracking
  (kept 7 days) and structured JSON run reports (kept 30 days), swept on start.
- A revealed copy - when you open a .fd-sec container without unpacking it, its contents are
  written read-only into %LOCALAPPDATA%\FileDO\reveal, in a folder whose access list names
  your account and the system only. It is removed when you say so or when the program that
  opened it lets go; after a power loss the next FileDO start removes it. Passwords are never
  written to any of these files, and neither is the name sealed inside a container.

Sending logs to the author is entirely yours to start: the About window of the GUI has a
button that packs those local log files into a zip in your TEMP folder, shows it to you in
Explorer, and opens your own mail program addressed to the author. The app never uploads
anything, never sends mail itself, and never does this automatically - you attach the file and
press Send, or you delete the zip and nothing happens.

Data sharing: none - the app shares nothing on its own; the only outbound path is a mail you
compose and send yourself from your own account. Children: no data collected.
Open source: https://github.com/SerZhyAle/FileDO
Contact: serzhyale@gmail.com
```
