# filedo_fill - a shortcut for `filedo <target> fill`

`filedo_fill.exe` is a launcher, not a second copy of the fill engine. It turns its command line
into the equivalent `filedo.exe` command line, runs the `filedo.exe` that sits in its own folder
with the same console, and ends with that exit code. Everything the run does - the files it
writes, the prompts, the safety checks on the system drive and the verdict - is what
`filedo.exe` does for that command, so it behaves exactly like the main tool and gets every fix
the main tool gets.

## Usage

```cmd
filedo_fill.exe <target> [size] [del] [global options]
filedo_fill.exe <target> clean [global options]
```

| You type | filedo.exe runs |
| --- | --- |
| `filedo_fill.exe D:` | `filedo.exe D: fill 100` |
| `filedo_fill.exe D: 500` | `filedo.exe D: fill 500` |
| `filedo_fill.exe D: 1000 del` | `filedo.exe D: fill 1000 del` |
| `filedo_fill.exe D: del 1000` | `filedo.exe D: fill 1000 del` |
| `filedo_fill.exe D: del` | `filedo.exe D: fill 100 del` |
| `filedo_fill.exe D:\Temp 200 del` | `filedo.exe D:\Temp fill 200 del` |
| `filedo_fill.exe \\server\share` | `filedo.exe \\server\share fill 100` |
| `filedo_fill.exe D: clean` | `filedo.exe D: clean` |

- **target** - a drive (`D:`), a folder (`D:\Temp`) or a network share (`\\server\share`). A
  single letter (`D`) means that drive; a bare folder name (`Photos`) is passed as `.\Photos`,
  so it can never be mistaken for one of `filedo.exe`'s verbs.
- **size** - the size of each file in MB, 1-10240. Default 100.
- **`del`**, `delete`, `d` - delete the files again once the target is full.
- **`clean`**, `c` - delete the test files a fill or a test left on the target. It takes no size
  and no `del`.
- **Global options** are handed to `filedo.exe` unchanged: `--events <file>`,
  `--stop-file <file>`, `--pause`, `--no-history`, `--no-ui`, `nohist`, and `-y`, `--yes`,
  `--force`. What they do is what `filedo.exe` does with them for that verb.
- `filedo_fill.exe -?` shows the usage and the version.

Anything else - an unknown word, a size out of range, `clean` together with a size - is refused
before `filedo.exe` starts.

## Exit codes

`filedo.exe`'s own: **0** done, **1** a defect was found, **2** could not verify.
`filedo_fill.exe` adds nothing of its own except **2** for a command line it refuses and for a
`filedo.exe` that is not in its folder or cannot be started.

Ctrl+C reaches `filedo.exe` directly, because the two share the console; it stops the way it
always does, and `filedo_fill.exe` waits for it and returns its exit code.

## Where filedo.exe comes from

Only from the folder `filedo_fill.exe` is in - never from the current folder and never from
`PATH`. A winget install starts the tools through links in `WinGet\Links`; the launcher follows
its own link back to the package folder first. The zip, the installer and winget all ship the two
files together.

## Building

```cmd
cd cmd\filedo-fill
go build -o filedo_fill.exe .
go test ./...
```

It is its own Go module with no dependencies outside the standard library. `go test` builds the
launcher and a stub `filedo.exe` and runs them together; it never touches a real drive.
