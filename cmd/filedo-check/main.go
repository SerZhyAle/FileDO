package main

import (
	"fmt"
	"strings"
)

// version is stamped by build.ps1 and release.yml with -ldflags "-X main.version=<stamp>".
var version = "dev"

const selfName = "filedo_check.exe"

// checkOptions are the options of "filedo check" this companion documents,
// each handed over under the same name; the value says whether it takes an
// argument. The rest of filedo check's options are reached through filedo.exe
// itself.
var checkOptions = map[string]bool{
	"--threshold":   true,
	"--workers":     true,
	"--max-files":   true,
	"--min-mb":      true,
	"--max-mb":      true,
	"--include-ext": true,
	"--exclude-ext": true,
	"--report":      true,
	"--verbose":     false,
	"--quiet":       false,
	"--precount":    false,
}

// withdrawnOptions were listed by the filedo_check that shipped before this
// launcher, and the check engine does not do them. Accepting them would be a
// promise nobody keeps, so they are refused with the reason.
var withdrawnOptions = map[string]string{
	"--resume":  "the check does not save its position, so there is nothing to resume from",
	"--dry-run": "the check has no dry run",
}

var modeWords = map[string]string{
	"quick": "quick", "q": "quick",
	"balanced": "balanced", "b": "balanced",
	"deep": "deep", "d": "deep",
}

// defaultMode is the mode filedo_check always documented as its default. It
// is passed explicitly, because filedo check on its own defaults to quick.
const defaultMode = "balanced"

// mapArgs turns a filedo_check command line (without the program name) into
// filedo.exe's:
//
//	filedo_check <target> [mode] [options]  ->  filedo check <target> --mode <mode|balanced> [options]
//
// with the global options appended unchanged. A drive is checked from its
// root, as filedo_check always did: "D:" alone would be D:'s current folder.
func mapArgs(args []string) ([]string, error) {
	words, forwarded, err := splitGlobalOptions(args, nil)
	if err != nil {
		return nil, err
	}
	var flags, rest []string
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "--") {
			rest = append(rest, w)
			continue
		}
		name, value, inline := strings.Cut(w, "=")
		if why, withdrawn := withdrawnOptions[name]; withdrawn {
			return nil, usagef("%s is not supported: %s", name, why)
		}
		takesValue, known := checkOptions[name]
		if !known {
			return nil, usagef("unknown option %s (filedo.exe check <path> has more options; see filedo.exe help)", w)
		}
		if !takesValue {
			if inline {
				return nil, usagef("%s takes no value", name)
			}
			flags = append(flags, w)
			continue
		}
		if inline {
			flags = append(flags, w)
		} else {
			if i+1 >= len(words) {
				return nil, usagef("%s needs a value after it", name)
			}
			i++
			value = words[i]
			flags = append(flags, name, value)
		}
		if name == "--report" {
			if v := strings.ToLower(value); v != "csv" && v != "json" {
				return nil, usagef("--report takes csv or json, not %q", value)
			}
		}
	}
	target, rest, err := takeTarget(rest)
	if err != nil {
		return nil, err
	}
	mode := ""
	for _, w := range rest {
		m, ok := modeWords[strings.ToLower(w)]
		if !ok {
			return nil, usagef("unexpected %q: filedo_check takes a target, then a mode (quick, balanced, deep) and options", w)
		}
		if mode != "" && mode != m {
			return nil, usagef("two modes given (%s and %s)", mode, m)
		}
		mode = m
	}
	if mode == "" {
		mode = defaultMode
	}
	out := []string{"check", driveRoot(target), "--mode", mode}
	out = append(out, flags...)
	return append(out, forwarded...), nil
}

// driveRoot turns a bare drive ("D:") into its root ("D:\"). filedo check
// walks the path it is given, and "D:" on its own names D:'s current folder,
// which a console can have set to anywhere.
func driveRoot(target string) string {
	if len(target) == 2 && target[1] == ':' && isASCIILetter(target[0]) {
		return target + `\`
	}
	return target
}

func usageText() string {
	return fmt.Sprintf(`
FileDO CHECK v%s - a shortcut for "filedo.exe check" (read-check files for damage)

USAGE:
  filedo_check.exe <target> [mode] [options] [global options]

It runs the filedo.exe in its own folder with the equivalent command and ends
with that exit code. What the run does - what counts as damage, the lists it
keeps and the verdict - is what filedo.exe check does:
  filedo_check.exe D:                  -> filedo.exe check D:\ --mode balanced
  filedo_check.exe D: quick            -> filedo.exe check D:\ --mode quick
  filedo_check.exe D:\Photos deep      -> filedo.exe check D:\Photos --mode deep
  filedo_check.exe \\server\share      -> filedo.exe check \\server\share --mode balanced
  filedo_check.exe D: --threshold 5    -> filedo.exe check D:\ --mode balanced --threshold 5

TARGET:  a drive (D:, checked from its root), a folder, a network share, or
         one file
MODES:
  quick, q        -> read the start of each file
  balanced, b     -> also read the middle of large files [DEFAULT]
  deep, d         -> also read three points inside large files
OPTIONS (the filedo check options of the same name):
  --threshold <sec>     a read slower than this marks the file damaged (2)
  --workers <n>         number of parallel readers (chosen from the drive type)
  --max-files <n>       stop after this many files
  --min-mb <n>          only files of at least n MB
  --max-mb <n>          only files of at most n MB
  --include-ext <list>  only these extensions, comma-separated
  --exclude-ext <list>  skip these extensions, comma-separated
  --report csv|json     also write check_report_<time>.csv or .json
  --verbose, --quiet    more or less output
  --precount            count the files first, for exact totals (the default)

GLOBAL OPTIONS, handed to filedo.exe unchanged:
  --events <file>  --stop-file <file>  --pause  --no-history  --no-ui  nohist

EXIT CODES (filedo.exe's): 0 passed, 1 damaged files were found, 2 could not
verify. filedo_check.exe itself exits 2 on a usage error and when filedo.exe
is not in its folder.

`, version)
}
