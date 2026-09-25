package main

import (
	"fmt"
	"strconv"
	"strings"
)

// version is stamped by build.ps1 and release.yml with -ldflags "-X main.version=<stamp>".
var version = "dev"

const selfName = "filedo_fill.exe"

// writerOptions are handed to filedo.exe on top of the global options. Whether
// the fill or clean verb uses them is filedo.exe's business, not this one's.
var writerOptions = map[string]bool{"-y": true, "--yes": true, "--force": true}

const defaultSizeMB = "100"

// mapArgs turns a filedo_fill command line (without the program name) into
// filedo.exe's:
//
//	filedo_fill <target> [size] [del]  ->  filedo <target> fill <size|100> [del]
//	filedo_fill <target> clean         ->  filedo <target> clean
//
// with the global options appended unchanged. The size is always spelled out,
// because filedo.exe reads the word after "fill" as either the size or "del".
func mapArgs(args []string) ([]string, error) {
	words, forwarded, err := splitGlobalOptions(args, writerOptions)
	if err != nil {
		return nil, err
	}
	target, rest, err := takeTarget(words)
	if err != nil {
		return nil, err
	}
	size := ""
	del, clean := false, false
	for _, w := range rest {
		lw := strings.ToLower(w)
		switch {
		case lw == "del" || lw == "delete" || lw == "d":
			del = true
		case lw == "clean" || lw == "c":
			clean = true
		case isDigits(lw):
			if size != "" {
				return nil, usagef("two sizes given (%s and %s)", size, w)
			}
			n, err := strconv.Atoi(lw)
			if err != nil || n < 1 || n > 10240 {
				return nil, usagef("size %s is out of range: give the size of each file in MB, 1-10240", w)
			}
			size = strconv.Itoa(n)
		default:
			return nil, usagef("unexpected %q: filedo_fill takes a target, then a size, del, or clean", w)
		}
	}
	if clean {
		if del || size != "" {
			return nil, usagef("clean takes no size and no del: it only deletes the test files already there")
		}
		return append([]string{target, "clean"}, forwarded...), nil
	}
	if size == "" {
		size = defaultSizeMB
	}
	out := []string{target, "fill", size}
	if del {
		out = append(out, "del")
	}
	return append(out, forwarded...), nil
}

func usageText() string {
	return fmt.Sprintf(`
FileDO FILL v%s - a shortcut for "filedo.exe <target> fill" and "filedo.exe <target> clean"

USAGE:
  filedo_fill.exe <target> [size] [del] [global options]
  filedo_fill.exe <target> clean [global options]

It runs the filedo.exe in its own folder with the equivalent command and ends
with that exit code. What the run does - safety checks and prompts included -
is what filedo.exe does for that command:
  filedo_fill.exe D:                -> filedo.exe D: fill 100
  filedo_fill.exe D: 500            -> filedo.exe D: fill 500
  filedo_fill.exe D: 1000 del       -> filedo.exe D: fill 1000 del
  filedo_fill.exe D: del            -> filedo.exe D: fill 100 del
  filedo_fill.exe D:\Temp 200 del   -> filedo.exe D:\Temp fill 200 del
  filedo_fill.exe \\server\share    -> filedo.exe \\server\share fill 100
  filedo_fill.exe D: clean          -> filedo.exe D: clean

TARGET:  a drive (D:), a folder (D:\Temp) or a network share (\\server\share)
SIZE:    size of each file in MB, 1-10240 (default 100)
del, delete, d    -> delete the files again once the target is full
clean, c          -> delete the test files a fill or a test left behind

GLOBAL OPTIONS, handed to filedo.exe unchanged:
  --events <file>  --stop-file <file>  --pause  --no-history  --no-ui  nohist
  -y  --yes  --force

EXIT CODES (filedo.exe's): 0 done, 1 a defect was found, 2 could not verify.
filedo_fill.exe itself exits 2 on a usage error and when filedo.exe is not in
its folder.

`, version)
}
