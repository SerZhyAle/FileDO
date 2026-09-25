# filedo_check - a shortcut for `filedo check`

`filedo_check.exe` is a launcher, not a second copy of the check engine. It turns its command line
into the equivalent `filedo.exe check` command line, runs the `filedo.exe` that sits in its own
folder with the same console, and ends with that exit code. Everything the run does - which files
it reads and how, what counts as damage, the `skip_files.list` and `check_files.list` it keeps,
and the verdict - is what `filedo.exe check` does, so the two can never disagree.

## Usage

```cmd
filedo_check.exe <target> [mode] [options] [global options]
```

| You type | filedo.exe runs |
| --- | --- |
| `filedo_check.exe D:` | `filedo.exe check D:\ --mode balanced` |
| `filedo_check.exe D: quick` | `filedo.exe check D:\ --mode quick` |
| `filedo_check.exe D:\Photos deep` | `filedo.exe check D:\Photos --mode deep` |
| `filedo_check.exe \\server\share` | `filedo.exe check \\server\share --mode balanced` |
| `filedo_check.exe D:\Video\one.mkv` | `filedo.exe check D:\Video\one.mkv --mode balanced` |
| `filedo_check.exe D: --threshold 5` | `filedo.exe check D:\ --mode balanced --threshold 5` |

- **target** - a drive (`D:`, checked from its root), a folder, a network share, or one file. A
  single letter (`D`) means that drive; a bare folder name (`Photos`) is passed as `.\Photos`.
- **mode** - one of:
  - `quick`, `q` - read the start of each file;
  - `balanced`, `b` - also read the middle of large files. **The default**, passed explicitly
    because `filedo.exe check` on its own defaults to `quick`;
  - `deep`, `d` - also read three points inside large files.

### Options

Each is the `filedo.exe check` option of the same name, passed on unchanged (`--name value` or
`--name=value`):

| Option | What it does |
| --- | --- |
| `--threshold <sec>` | a read slower than this marks the file damaged (default 2) |
| `--workers <n>` | number of parallel readers (default: chosen from the drive type) |
| `--max-files <n>` | stop after this many files |
| `--min-mb <n>` / `--max-mb <n>` | only files of at least / at most n MB |
| `--include-ext <list>` / `--exclude-ext <list>` | only / skip these extensions, comma-separated |
| `--report csv\|json` | also write `check_report_<time>.csv` or `.json` |
| `--verbose` / `--quiet` | more or less output |
| `--precount` | count the files first, for exact totals (already the default) |

`filedo.exe check` has further tuning options; run it directly for those. The `FILEDO_CHECK_*`
environment variables it reads apply here too, because `filedo.exe` inherits the environment -
except `FILEDO_CHECK_MODE`, which the mode word (or the `balanced` default) always overrides.

`--resume` and `--dry-run`, which earlier versions of this tool listed, are refused: the check
engine does not keep a position to resume from, and it has no dry run. Anything else unknown is
refused too, before `filedo.exe` starts.

### Global options

Handed to `filedo.exe` unchanged: `--events <file>`, `--stop-file <file>`, `--pause`,
`--no-history`, `--no-ui`, `nohist`.

`filedo_check.exe -?` shows the usage and the version.

## Exit codes

`filedo.exe`'s own: **0** passed, **1** damaged files were found, **2** could not verify.
`filedo_check.exe` adds nothing of its own except **2** for a command line it refuses and for a
`filedo.exe` that is not in its folder or cannot be started.

Ctrl+C reaches `filedo.exe` directly, because the two share the console; it stops the way it
always does, and `filedo_check.exe` waits for it and returns its exit code.

## Where filedo.exe comes from

Only from the folder `filedo_check.exe` is in - never from the current folder and never from
`PATH`. A winget install starts the tools through links in `WinGet\Links`; the launcher follows
its own link back to the package folder first. The zip, the installer and winget all ship the two
files together.

## Building

```cmd
cd cmd\filedo-check
go build -o filedo_check.exe .
go test ./...
```

It is its own Go module with no dependencies outside the standard library. `go test` builds the
launcher and a stub `filedo.exe` and runs them together; it never reads a real drive.
`test_filedo_check.cmd` is a manual walk-through against a real `filedo.exe`.
