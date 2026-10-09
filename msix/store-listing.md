# FileDO - Store listing (what is NOT in the CSV)

The listing copy - Title, short description, Description, Product features, search terms and
"What's new" - lives in `msix/listing/<code>.txt` (en, ru, uk, de, fr) and reaches Partner Center
through the export, merge, import round trip of `msix/build-store-listing-csv.ps1` (`msix/README.md`,
section 4). Edit it THERE and never in the console: the console is a render target, and two
sources of truth is how a corrected claim survives in a live listing.

What those fields say (SP-0166): the short description fits the 270 characters the Store shows in
every view and makes no network claim; the long description opens with the value for the reader, not
with a list of commands; there are at most seven search terms of at most 30 characters, each relevant
to the product, none another product's title and none a pricing term (policy 10.1.3), and neither
"encryption" nor "file manager" among them. The closing "No accounts, no ads, no telemetry." is in
the English short description only: the ru, uk, de and fr texts are at 259-268 of 270 characters and
cannot hold it; the long description (PRIVATE BY DESIGN) and Feature10 carry the claim in every
locale. The capacity test is described as testing the free space and taking time on large media; a
folder wipe is a plain delete; wiping free space or one file is described as making recovery harder,
never as secure or unrecoverable. The builder fills only empty cells and `-Refresh` takes exact
field names, so a change to these fields is imported with
`-Refresh ShortDescription,Description,Feature1,Feature3,Feature5,SearchTerm1,SearchTerm2,SearchTerm3,SearchTerm4,SearchTerm5,SearchTerm6,SearchTerm7`.
Run the builder from a PowerShell session (`& .\msix\build-store-listing-csv.ps1 -Refresh a,b,c`),
not through `pwsh -File`, which passes the comma list as one string and refreshes nothing; a good
run prints a non-zero "refreshed N".

This file keeps only what the CSV does not carry and a person pastes or ticks by hand: the
Properties page, the `runFullTrust` justification, the export-compliance answer and the privacy
declaration. The justification has a ~1000-char limit: the short variant below (about 380
characters) fits; the long variant is about 1100 characters, so paste it only where the console
accepts that length and use the short one otherwise.

Repo: https://github.com/SerZhyAle/FileDO - Contact: sza@ukr.net - License: MIT

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
| Support info > Support contact info | `sza@ukr.net` |
| Support info > Phone, Address, Postal code, City, State, Country | leave empty (optional, and a private address is not listing material) |

**Category.** `Utilities + tools` has exactly two subcategories, `Backup + manage` and
`File managers` ([the Store's own table][cat]). FileDO is not a file manager - it browses nothing -
so `Backup + manage` is the honest one. `Security` as the secondary category is earned by the wipe
of free space and files and the password-protected `.fd-sec` container, not bought for reach.

**The personal-information answer is Yes, deliberately**, and it does not contradict the "no data
collected" declaration further down. The question asks what the product *accesses*, not what its
author receives: FileDO reads and writes raw device data on drives the user points it at, reads drive
serial numbers through WMI, and writes `history.json`, which records full command lines and target
paths. It transmits none of it. Answering No would also make the privacy policy URL optional, and
that URL is a gate this repository checks before every submission (`msix\test-store-package.ps1`,
SP-0007 C6).

[cat]: https://learn.microsoft.com/en-us/windows/apps/publish/publish-your-app/msix/categories-and-subcategories

---
## runFullTrust justification (short variant fits ~1000 chars; long variant is about 1100)

```
FileDO is a full-trust Win32 desktop application, not a UWP app: a Go command-line engine (filedo.exe) and a .NET Framework window (filedo_win.exe) that starts it. runFullTrust is required to run as a normal desktop process and to call the Win32 storage APIs its core features depend on:
- Direct device/volume access (CreateFile on \\.\PhysicalDrive and volume handles, SetFilePointer, raw read/write): needed to measure true disk speed and to detect fake-capacity drives by writing data into the free space and reading it back. It accesses storage the user explicitly targets; it does not scan the system or read personal files on its own.
- High-throughput file I/O for wipe and fill: fills free space or overwrites one file to make recovery harder; a folder wipe is a plain delete. Destructive actions confirm first and never auto-force drive roots, reparse points, or system TEMP.
These APIs are available only to full-trust desktop apps. FileDO runs entirely locally, never connects to the internet or to any other computer, and collects no user data. Open source: https://github.com/SerZhyAle/FileDO
```

### Short variant (if a brief reason is also requested)

```
Full-trust Win32 desktop app (Go command-line engine plus a .NET Framework window). Needs runFullTrust for raw disk/volume access (CreateFile on \\.\PhysicalDrive, raw read/write) used for speed testing, fake-capacity detection, and wiping - APIs only available to full-trust desktop apps. Runs locally, no internet connection, no data collection. https://github.com/SerZhyAle/FileDO
```

## Export compliance - encryption (answer this BEFORE the first submission carrying secret files)

Partner Center asks, under Product declarations, whether the product calls, supports, contains
or uses cryptography. Until the `.fd-sec` feature shipped, the honest answer was no. It is now
**yes**, and the answer below is what goes in. This is an engineer's reading of the question,
not legal advice: the owner confirms it before pressing Submit, and this section is why the
submission cannot be the first place the question is read.

```
Yes. FileDO uses cryptography for two features: packing a single user-chosen file into a
password-protected .fd-sec container and opening it again, and keeping a .fdd virtual disk
container (one file holding a whole volume) encrypted under a password - or, without one,
obfuscated with a key kept in the file itself, which is not encryption - and reading it again.

It uses standard published algorithms only, from permissively licensed, widely distributed
libraries (the Go standard library and the Go project's golang.org/x/crypto). It implements no
cryptographic algorithm of its own, provides no cryptographic service to other programs, and
never connects to the internet or to any other computer, so nothing is encrypted in transit.
Both container formats are published in full, with test vectors, so the data can be read
without FileDO.

This is the ordinary ancillary use of published algorithms in a mass-market tool, not a
cryptographic product.
```

**Reach must not shrink.** The canon forbids a release that takes countries, an age rating or
a platform floor away from the previous one (INVARIANTS 2). The declaration above is made on
the way in, so it changes no country in the current listing; if Partner Center answers it by
restricting availability anywhere, the submission is stopped and the question comes back to
the owner rather than being resolved by accepting a smaller reach.

**What the Store build does NOT carry.** No Explorer context-menu entries: a packaged build can
only declare a context-menu handler through a signed shell command handler, which is not built
(SP-0005 9.3). It **does** declare the `.fd-sec` association, so double-clicking a container opens
FileDO. The verbs work from the terminal in the Store build exactly as everywhere else. It also
declares `.fdd` for the window only, with no verb, and it **cannot mount** a `.fdd` virtual disk: a
packaged app can neither configure the Windows iSCSI initiator nor ask for administrator rights, so
`mount`, `unmount`, `save`, `format`, `vd auto`, `vd guard` and `vd register` return class 6 there. The read path -
info, verify, export, and the file-level changes - works. The listing says exactly that and
promises no mounting.

## Privacy policy

**Privacy policy URL** (paste into Partner Center ▸ Properties ▸ Privacy policy URL):

```
https://serzhyale.github.io/FileDO/privacy.html
```

The hosted page (`docs/privacy.html`) is the single source of truth. The Partner Center
data-collection form and the summary below are *rendered from it* - if the page changes,
re-derive them; never edit the summary on its own.

Partner Center data collection: declare **no** data collection (the app never connects to the
internet or to any other computer - in this build it opens no socket at all, because the loopback
block server of a mounted virtual disk does not run in the package - and the package declares no
`internetClient` capability).

```
FileDO does not collect, store, or transmit any personal data. It runs entirely on your
device, has no servers, makes no network requests, and contains no
telemetry/analytics/ads/accounts.

What it accesses and why:
- Raw disk/device data (read and write) on the drives, folders, or shares you target -
  used only for speed testing, fake-capacity detection, wiping, and copying.
- Drive model, serial number, and interface type, read via WMI for the `info` command and
  shown on screen only - never logged to file, never transmitted.
- The wipe and fill features intentionally overwrite/destroy data you point them at; they
  confirm before acting and never silently force drive roots, junctions, or system TEMP.

Local files it writes (they stay on your device unless you mail them yourself - see the last
item below):
- history.json - a log of operations (time, command, target path, full command line,
  parameters, results), written to %LOCALAPPDATA%\FileDO\state\ (inside the package's own
  per-user storage in the Store build), last 1000 entries. Help and the history listing
  write nothing. Disable per run with the `nohist` (or `no_history`) flag.
- hash_cache.json - cached file paths, sizes, timestamps, file IDs and SHA-256 hashes that
  speed up duplicate scans, in the same %LOCALAPPDATA%\FileDO\state\ folder. No file
  contents are stored.
- The copy and check lists (skipped and damaged files, a check's resume state) - in the same
  folder, never in the folder a command was started from.
- %LOCALAPPDATA%\FileDO\runs and \reports - GUI event stream files for live UI tracking
  (kept 7 days) and structured JSON run reports (kept 30 days), swept on start.
- A revealed copy - when you open a .fd-sec container without unpacking it, its contents are
  written read-only into %LOCALAPPDATA%\FileDO\reveal, in a folder whose access list names
  your account and the system only. It is removed when you say so or when the program that
  opened it lets go; after a power loss the next FileDO start removes it. Passwords are never
  written to any of these files, and neither is the name sealed inside a container.
- vdisk-state.json and vdisk.log - the short names you gave .fdd virtual disks, their paths, what
  is mounted and a log of mounts, in the same %LOCALAPPDATA%\FileDO\state\ folder. No container
  password is written there. Outside the Store build, the block server of a mounted disk listens
  on 127.0.0.1 only: nothing leaves the device, and the container never goes to any network.
- vdisk-shared.json - the .fdd virtual disks you shared with Fast Media Sorter for Windows (the
  container path, the folder name it is shared under, the read-only and autostart marks), in the same
  state folder. No password is written there. FileDO opens no network connection for sharing: it asks
  that separate program over a local pipe, and that program - not FileDO - serves the files of an open
  disk to the devices you paired with it. An encrypted disk is unlocked on this device only; if you turn
  on autostart for one, its password is kept on the device, protected for that program, until you turn
  autostart off, stop sharing or change the password.

Sending logs to the author is entirely yours to start: the About window of the GUI has a
button that packs those local log files into a zip in your TEMP folder, shows it to you in
Explorer, and opens your own mail program addressed to the author. The app never uploads
anything, never sends mail itself, and never does this automatically - you attach the file and
press Send, or you delete the zip and nothing happens.

Data sharing: none - the app shares nothing on its own; the only outbound path is a mail you
compose and send yourself from your own account. Children: no data collected.
Open source: https://github.com/SerZhyAle/FileDO
Contact: sza@ukr.net
```
