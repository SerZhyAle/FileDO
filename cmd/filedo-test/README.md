# filedo_test - a shortcut for `filedo <target> test`

`filedo_test.exe` is a launcher, not a second copy of the fake-capacity engine. It turns its
command line into the equivalent `filedo.exe` command line, runs the `filedo.exe` that sits in its
own folder with the same console, and ends with that exit code. Everything the run does - the
test files, the checks that catch a counterfeit drive, the prompts, the safety checks on the
system drive and the verdict - is what `filedo.exe` does for that command, so a detection fix in
the main tool is a fix here too.

## Usage

```cmd
filedo_test.exe <target> [del] [global options]
filedo_test.exe <target> clean [global options]
```

| You type | filedo.exe runs |
| --- | --- |
| `filedo_test.exe E:` | `filedo.exe E: test` |
| `filedo_test.exe E: del` | `filedo.exe E: test del` |
| `filedo_test.exe D:\Temp` | `filedo.exe D:\Temp test` |
| `filedo_test.exe D:\Temp del` | `filedo.exe D:\Temp test del` |
| `filedo_test.exe \\server\share` | `filedo.exe \\server\share test` |
| `filedo_test.exe E: clean` | `filedo.exe E: clean` |

- **target** - a drive (`E:`), a folder (`D:\Temp`) or a network share (`\\server\share`). A
  single letter (`E`) means that drive; a bare folder name (`Photos`) is passed as `.\Photos`,
  so it can never be mistaken for one of `filedo.exe`'s verbs.
- **`del`**, `delete`, `d` - delete the test files when the test passes. When it fails they are
  kept: they are the evidence.
- **`clean`**, `c` - delete the test files a test or a fill left on the target. It takes no
  `del`.
- **Global options** are handed to `filedo.exe` unchanged: `--events <file>`,
  `--stop-file <file>`, `--pause`, `--no-history`, `--no-ui`, `nohist`, and `-y`, `--yes`,
  `--force`. What they do is what `filedo.exe` does with them for that verb.
- `filedo_test.exe -?` shows the usage and the version.

Anything else is refused before `filedo.exe` starts. For a different number of test files, run
`filedo.exe <target> test <count>` directly.

## Exit codes

`filedo.exe`'s own: **0** the test passed, **1** a defect was found (a fake capacity, a write that
does not read back), **2** could not verify. `filedo_test.exe` adds nothing of its own except
**2** for a command line it refuses and for a `filedo.exe` that is not in its folder or cannot be
started.

Ctrl+C reaches `filedo.exe` directly, because the two share the console; it stops the way it
always does, and `filedo_test.exe` waits for it and returns its exit code.

## Where filedo.exe comes from

Only from the folder `filedo_test.exe` is in - never from the current folder and never from
`PATH`. A winget install starts the tools through links in `WinGet\Links`; the launcher follows
its own link back to the package folder first. The zip, the installer and winget all ship the two
files together.

## Building

```cmd
cd cmd\filedo-test
go build -o filedo_test.exe .
go test ./...
```

It is its own Go module with no dependencies outside the standard library; `go test` has to run
from inside this folder. It builds the launcher and a stub `filedo.exe` and runs them together; it
never touches a real drive.
