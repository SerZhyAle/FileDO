package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// The one verb dispatch (CLI-30).
//
// There used to be two copies of the verb switch - main() for the command line
// and executeInternalCommand for each line of a `.lst` - and they drifted:
// verb-first lines never opened a run (CLI-11), `fdsec info` from a batch lost
// its exit class (CLI-10), a failed target was counted as a success (CLI-12),
// a mask or an unknown word was run as an external program (CLI-13), help
// words worked in one and ran help.exe in the other. dispatchLine is now the
// only switch: main() calls it with the command line, the batch calls it with
// each line, and a new verb is one alias slice plus one case below.

// verbOf maps an alias to its canonical verb, or "" when the word is not a
// verb. The alias slices in main.go stay the vocabulary; this is the one
// place they are read to dispatch.
func verbOf(word string) string {
	w := strings.ToLower(word)
	for _, v := range []struct {
		name    string
		aliases []string
	}{
		{"device", list_of_flags_for_device},
		{"folder", list_of_flags_for_folder},
		{"file", list_of_flags_for_file},
		{"network", list_of_flags_for_network},
		{"from", list_of_flags_for_from},
		{"hist", list_of_flags_for_hist},
		{"check-duplicates", list_of_flags_for_duplicates},
		{"compare", list_of_flags_for_compare},
		{"copy", list_of_flags_for_copy},
		{"fastcopy", list_of_flags_for_fastcopy},
		{"synccopy", list_of_flags_for_synccopy},
		{"balanced", list_of_flags_for_balanced},
		{"maxcopy", list_of_flags_for_maxcopy},
		{"smartcopy", list_of_flags_for_smartcopy},
		{"safecopy", list_of_flags_for_safecopy},
		{"check", list_of_flags_for_check},
		{"fdsec", list_of_flags_for_fdsec},
		{"ui", list_of_flags_for_ui},
	} {
		if contains(v.aliases, w) {
			return v.name
		}
	}
	return ""
}

// isHelpWord reports whether a first word asks for help.
func isHelpWord(word string) bool {
	return contains(list_fo_flags_for_help, strings.ToLower(word))
}

// classifyTarget decides what kind of target a first word names when it is
// not a verb (CLI-17, CLI-19, CLI-22). The rules, in order:
//
//   - `\\?\UNC\srv\share` and `\\srv\share` (or `//srv/share`) are network.
//   - `\\?\X:\..` and `\\.\X:\..` name a local path: the drive root is the
//     device X:, anything below it is the folder or file it names. `\\.\`
//     followed by anything else (a raw disk, a device object) is refused.
//   - Only an ASCII letter, alone or followed by `:`, `:\` or `:/`, is a
//     device. `.` is a folder, and a one-character name in another script
//     is a name, not a drive (the old byte test panicked on `я`).
//   - Anything else is a folder or a file if it exists.
//
// A word that names nothing is an error: "does not exist" when it looks like
// a path, "unknown command" when it does not.
func classifyTarget(word string) (kind, target string, err error) {
	switch {
	case hasPrefixFold(word, `\\?\UNC\`):
		return "network", word, nil
	case strings.HasPrefix(word, `\\?\`) || strings.HasPrefix(word, `\\.\`):
		rest := word[4:]
		if driveRootSpelling.MatchString(rest) {
			return "device", deviceRootPath(rest), nil
		}
		if len(rest) >= 3 && isASCIILetter(rest[:1]) && rest[1] == ':' && (rest[2] == '\\' || rest[2] == '/') {
			return statKind(word)
		}
		if strings.HasPrefix(word, `\\?\`) && hasPrefixFold(rest, `Volume{`) {
			return statKind(word)
		}
		return "", word, fmt.Errorf("%s names a raw device or a device object; FileDO works on drive letters, folders, files and shares", word)
	case strings.HasPrefix(word, `\\`) || strings.HasPrefix(word, "//"):
		return "network", word, nil
	case isASCIILetter(word):
		return "device", word + ":", nil
	case driveRootSpelling.MatchString(word):
		return "device", word, nil
	}
	return statKind(word)
}

func statKind(word string) (string, string, error) {
	if info, err := os.Stat(word); err == nil {
		if info.IsDir() {
			return "folder", word, nil
		}
		return "file", word, nil
	}
	switch {
	case strings.HasSuffix(word, "/") || strings.HasSuffix(word, `\`):
		return "", word, fmt.Errorf("the folder %q does not exist", word)
	case strings.ContainsAny(word, `.:\/`):
		return "", word, fmt.Errorf("the path %q does not exist", word)
	}
	return "", word, errUnknownCommand
}

var errUnknownCommand = errors.New("unknown command")

// dispatchLine runs one command line - the arguments after the program name,
// typed at the console or read from a `.lst` - and records its outcome with
// the recorders of outcome.go. It returns a non-nil error when the line did
// not succeed; the error has already been reported, and a batch uses it only
// to count.
func dispatchLine(args []string, hl *HistoryLogger, batch bool) error {
	if len(args) == 0 {
		return nil
	}
	first := args[0]
	lower := strings.ToLower(first)

	if isHelpWord(first) {
		if contains(list_fo_flags_for_short_help, lower) {
			fmt.Println(shortUsage)
		} else {
			fmt.Println(usage)
		}
		return nil
	}

	// Target-first container verbs run ahead of any path probe: a mask target
	// (`*.txt secure`) never passes os.Stat.
	if handled, err := fdsecDispatchTarget(args, hl); handled {
		if err != nil {
			reportFdsecError(err, hl)
			return err
		}
		hl.SetSuccess()
		return nil
	}

	verb := verbOf(first)
	var kind, target string
	rest := args[1:]
	if verb == "" {
		// `wipe` and `w` take their target first. A folder of that name is
		// its own target; otherwise the word is a mistyped order.
		if contains(list_of_flags_for_wipe, lower) {
			if _, err := os.Stat(first); err != nil {
				usageFailure("wipe", args, "wipe takes its target first: filedo <target> wipe [--force]")
				return errUnknownCommand
			}
		}
		var err error
		kind, target, err = classifyTarget(first)
		if err != nil {
			if errors.Is(err, errUnknownCommand) {
				usageFailure(lower, args, "Unknown command %q", first)
				if !batch {
					fmt.Println(usage)
				}
				return err
			}
			beginRun(runActs, "info", first, args)
			reportRunError(err, hl)
			return err
		}
		verb = kind
		rest = append([]string{target}, args[1:]...)
	}

	switch verb {
	case "device", "folder", "file", "network":
		cmdType := map[string]CommandType{"device": CommandDevice, "folder": CommandFolder, "file": CommandFile, "network": CommandNetwork}[verb]
		// No flag of the set is defined, and ExitOnError used to exit past the
		// result, the history and --pause on a folder named `-old` (CLI-23).
		fs := flag.NewFlagSet(verb, flag.ContinueOnError)
		fs.SetOutput(os.Stdout)
		before := runProblemCount()
		runGenericCommand(fs, cmdType, rest, hl)
		if runProblemCount() > before {
			return errLineFailed
		}
		return nil

	case "fdsec":
		if err := handleFdsecCommand(rest, hl); err != nil {
			reportFdsecError(err, hl)
			return err
		}
		hl.SetSuccess()
		return nil

	case "check-duplicates":
		// The verb-first form is `cd from <list>`; everything else takes its
		// target first.
		if len(rest) < 1 || !strings.EqualFold(rest[0], "from") {
			usageFailure("check-duplicates", args, "the verb-first form is: filedo cd from list <file> [options]; to scan, name the target first: filedo <target> cd [options]")
			return errLineFailed
		}
		hl.SetCommand(lower, "from", "check-duplicates")
		beginRun(runActs, "check-duplicates", "from", args)
		if err := handleCheckDuplicatesCommand(args); err != nil {
			reportRunError(err, hl)
			return err
		}
		hl.SetSuccess()
		return nil

	case "hist":
		// Listing the history is not a run: no history line, no events.
		handleHistoryCommand(args)
		return nil

	case "from":
		if len(rest) < 1 {
			usageFailure(lower, args, "Missing file path for 'from' command")
			return errLineFailed
		}
		hl.SetCommand(lower, rest[0], "batch")
		beginRun(runActs, "batch", rest[0], args)
		if err := executeFromFile(rest[0], hl); err != nil {
			reportRunError(err, hl)
			return err
		}
		return nil

	case "ui":
		if batch {
			usageFailure("ui", args, "cannot launch the UI shell from a batch file")
			return errLineFailed
		}
		hl.SetCommand("ui", "", "ui")
		beginRun(runActs, "ui", "", args)
		if err := launchUI(rest...); err != nil {
			fmt.Fprintf(os.Stderr, "GUI is available as filedo_win.exe (download from https://github.com/SerZhyAle/FileDO/releases)\n")
			reportRunError(err, hl)
			return err
		}
		hl.SetSuccess()
		return nil

	case "check":
		if len(rest) < 1 {
			usageFailure(lower, args, "CHECK command requires a folder or a file path")
			return errLineFailed
		}
		hl.SetCommand(lower, rest[0], "check")
		beginRun(runJudges, "check", rest[0], args)
		return finishVerb(HandleCheckArgs(rest[0], rest[1:]), hl)
	}

	// The two-path verbs.
	twoPath := map[string]struct {
		op  string
		run func(src, dst string, extra []string) error
	}{
		"compare":   {"compare", func(s, d string, x []string) error { return handleCompareCommand(s, d, x...) }},
		"copy":      {"auto-copy", func(s, d string, x []string) error { copyPrecount = wantsCopyPrecount(x); return handleAutoCopyCommand(s, d) }},
		"fastcopy":  {"fastcopy", func(s, d string, _ []string) error { return handleFastCopyCommand(s, d) }},
		"synccopy":  {"synccopy", func(s, d string, _ []string) error { return handleSyncCopyCommand(s, d) }},
		"balanced":  {"balanced", func(s, d string, _ []string) error { return handleBalancedCopyCommand(s, d) }},
		"maxcopy":   {"maxcopy", func(s, d string, _ []string) error { return handleMaxCopyCommand(s, d) }},
		"smartcopy": {"smartcopy", func(s, d string, _ []string) error { return handleSmartCopyCommand(s, d) }},
		"safecopy":  {"safecopy", func(s, d string, _ []string) error { return SafeCopy(s, d) }},
	}
	if tp, ok := twoPath[verb]; ok {
		if len(rest) < 2 {
			usageFailure(lower, args, "%s requires source and target paths", verb)
			return errLineFailed
		}
		hl.SetCommand(lower, rest[0], tp.op)
		beginRun(runActs, tp.op, rest[0], args)
		return finishVerb(tp.run(rest[0], rest[1], rest[2:]), hl)
	}

	usageFailure(lower, args, "Unknown command %q", first)
	return errUnknownCommand
}

var errLineFailed = errors.New("the command failed")

// finishVerb ends a verb that returned an error or nil.
func finishVerb(err error, hl *HistoryLogger) error {
	if err != nil {
		reportRunError(err, hl)
		return err
	}
	hl.SetSuccess()
	return nil
}

// executeInternalCommand runs one batch line through the one dispatch, with a
// history entry of its own.
func executeInternalCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}
	internalLogger := NewHistoryLogger(append([]string{"filedo"}, args...))
	defer internalLogger.Finish()
	// Each batch line asks for --precount on its own; none inherits it.
	copyPrecount = false
	return dispatchLine(args, internalLogger, true)
}
