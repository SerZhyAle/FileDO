# State lists - line format

The per-file lists FileDO keeps in its state root (`%LOCALAPPDATA%\FileDO\state\`, or `FILEDO_STATE_DIR`):

| File | Written by | Read by | Meaning of an entry |
| --- | --- | --- | --- |
| `check_damaged.list` | `check` | `check` | the file read slowly or failed with a device error; the next sweep re-reports it without reading it |
| `check_files.list` | `check` | `check --resume` | the file read cleanly; `--resume` does not read it again |
| `skip_files.list` | `safecopy` and the damaged-disk copy | every copy verb | the source proved unreadable; a copy skips it |

The reader and the writer are `cmd/filedo/state_lists.go`. These files are FileDO's own state, not a
contract with another product: nothing outside this repository reads them.

## Format 2 (current, since AUD-14-F1, 2026-09-26)

UTF-8 text, one entry per line, `LF` line ends (a trailing `CR` is tolerated on read):

```
# filedo-state-list 2
<path> TAB <size> TAB <mtime> TAB <volume serial> TAB <file ID>
```

- **Header** - the first line of every file this format writes: `# filedo-state-list ` and the format
  number.
- **path** - the absolute path as the run spelled it. It only finds the candidate entry; matching is
  case-insensitive (the path is case-folded on load).
- **size** - bytes, decimal.
- **mtime** - the modification time in UTC, RFC 3339 with nanoseconds (Go `time.RFC3339Nano`).
- **volume serial** - the serial number of the volume holding the file, exactly 8 upper-case hex digits.
- **file ID** - the file's ID on that volume (`FileIndexHigh:FileIndexLow`), exactly 16 upper-case hex digits.

Volume serial and file ID are what `fsx.IdentityOf` reports: `GetFileInformationByHandle` on a handle
opened for attributes only, so resolving them reads no file data.

**An entry is trusted only when all five fields match the file as it is now.** A file that changed (size or
mtime), a copy at the same path (another file ID) and a file at the same path on another volume - the next
SD card or USB stick at the same drive letter - are different files: the entry is ignored and the file is
read again. A file whose identity cannot be resolved is read again too. The identity is resolved only for a
path the list names, and the writer resolves it when it records a file; a file whose identity cannot be
resolved is not recorded.

Lines starting with `#` are comments. The same path recorded again replaces the earlier entry (last line
wins). A writer appends; losing an entry only means a file is read again, which is the safe direction.

## Reading older and newer files

- **Format 1** (path, size, mtime - three fields, no header) and **path-only** lines from earlier versions
  are read safely and never trusted: they cannot say which volume the file was on. Any other line that
  does not parse as five valid fields is treated the same way. The run prints how many lines it did not trust.
- **The first write** to a list that held untrusted lines or no header replaces the file - through a
  temporary file in the same folder, then a rename - with the header and the trusted entries only.
- **A newer format** (a header with a number above 2) is not trusted at all and is never written to, so an
  older build cannot damage a list a newer build made.
- A missing file is an empty list. Reading never fails on content; only an I/O error on the file itself is
  reported, and the run goes on without the list.

## Changing the format

Raise the number in the header and `stateListFormat` in `cmd/filedo/state_lists.go` together, keep a
reader for every older format that treats its lines as untrusted, and update this page in the same change.
