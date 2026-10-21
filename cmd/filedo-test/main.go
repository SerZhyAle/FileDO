package main

import (
	"fmt"
	"strings"
)

// version is stamped by build.ps1 and release.yml with -ldflags "-X main.version=<stamp>".
var version = "dev"

const selfName = "filedo_test.exe"

// writerOptions are handed to filedo.exe on top of the global options. Whether
// the test or clean verb uses them is filedo.exe's business, not this one's.
var writerOptions = map[string]bool{"-y": true, "--yes": true, "--force": true}

// mapArgs turns a filedo_test command line (without the program name) into
// filedo.exe's:
//
//	filedo_test <target> [del]  ->  filedo <target> test [del]
//	filedo_test <target> clean  ->  filedo <target> clean
//
// with the global options appended unchanged. clean is here because the
// filedo_test that shipped before this launcher told its users to run
// "filedo_test.exe <target> clean" to remove kept test files - and then
// started a new test when they did (SP-0028 COMP-07).
func mapArgs(args []string) ([]string, error) {
	words, forwarded, err := splitGlobalOptions(args, writerOptions)
	if err != nil {
		return nil, err
	}
	target, rest, err := takeTarget(words)
	if err != nil {
		return nil, err
	}
	del, clean := false, false
	for _, w := range rest {
		switch strings.ToLower(w) {
		case "del", "delete", "d":
			del = true
		case "clean", "c":
			clean = true
		default:
			return nil, usagef("unexpected %q: filedo_test takes a target, then del or clean (for another file count run filedo.exe <target> test <count>)", w)
		}
	}
	if clean {
		if del {
			return nil, usagef("clean and del together: clean only deletes the test files already there")
		}
		return append([]string{target, "clean"}, forwarded...), nil
	}
	out := []string{target, "test"}
	if del {
		out = append(out, "del")
	}
	return append(out, forwarded...), nil
}

func usageText() string {
	return fmt.Sprintf(`
FileDO TEST v%s - a shortcut for "filedo.exe <target> test" (fake capacity test)

USAGE:
  filedo_test.exe <target> [del] [global options]
  filedo_test.exe <target> clean [global options]

It runs the filedo.exe in its own folder with the equivalent command and ends
with that exit code. What the run does - safety checks, prompts and the
verdict included - is what filedo.exe does for that command:
  filedo_test.exe E:                -> filedo.exe E: test
  filedo_test.exe E: del            -> filedo.exe E: test del
  filedo_test.exe D:\Temp           -> filedo.exe D:\Temp test
  filedo_test.exe D:\Temp del       -> filedo.exe D:\Temp test del
  filedo_test.exe \\server\share    -> filedo.exe \\server\share test
  filedo_test.exe E: clean          -> filedo.exe E: clean

TARGET:  a drive (E:), a folder (D:\Temp) or a network share (\\server\share)
del, delete, d    -> delete the test files when the test passes
clean, c          -> delete the test files a test or a fill left behind

GLOBAL OPTIONS, handed to filedo.exe unchanged:
  --events <file>  --stop-file <file>  --pause  --no-history  --no-ui  nohist
  -y  --yes  --force

EXIT CODES (filedo.exe's): 0 the test passed, 1 a defect was found (for
example a fake capacity), 2 could not verify. filedo_test.exe itself exits 2
on a usage error and when filedo.exe is not in its folder.

`, version)
}
