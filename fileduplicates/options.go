package fileduplicates

import (
	"fmt"
	"strings"
)

// ruleModes maps the rule words to the copy that is kept. Each word names
// what is removed: `old` removes the older copies (the newest is kept), `new`
// the newer ones, `abc` every name but the alphabetically last, `xyz` every
// name but the alphabetically first.
var ruleModes = map[string]DuplicateSelectionMode{
	"old": NewestAsOriginal,
	"new": OldestAsOriginal,
	"abc": LastAlphaAsOriginal,
	"xyz": FirstAlphaAsOriginal,
}

// isReservedWord reports whether a word is one of the option words, so that
// `move -y` or `list del` is read as a missing operand rather than as a
// folder named "-y".
func isReservedWord(w string) bool {
	switch strings.ToLower(w) {
	case "list", "quiet", "q", "short", "s", "move", "del", "delete", "-y", "--yes",
		"old", "new", "abc", "xyz", "nohist", "no_history":
		return true
	}
	return false
}

// ParseArguments parses command line arguments for duplicate processing.
//
// Word order does not matter: every decision is taken after the whole list
// is read. Whether each file is asked about is decided by one explicit word,
// -y (or --yes), and never by the rule word or its position (DUP-03). Words
// that cannot be accepted are kept for Validate, which refuses the run.
func ParseArguments(args []string) DuplicateOptions {
	options := DefaultOptions()
	rule := ""
	action := ""

	for i := 0; i < len(args); i++ {
		raw := strings.TrimSpace(args[i])
		arg := strings.ToLower(raw)
		if arg == "" {
			continue
		}

		switch arg {
		case "list":
			if i+1 < len(args) && !isReservedWord(args[i+1]) {
				options.OutputPath = args[i+1]
				options.OutputFileSpecified = true
				i++ // Skip the next argument as it's the filename
			} else {
				options.problems = append(options.problems, "'list' needs a file name after it")
			}

		case "quiet", "q", "short", "s":
			options.Verbose = false

		case "old", "new", "abc", "xyz":
			if rule != "" && rule != arg {
				options.problems = append(options.problems,
					fmt.Sprintf("two rules given (%s and %s); give one", rule, arg))
			}
			rule = arg
			options.SelectionMode = ruleModes[arg]
			options.SelectionModeSet = true

		case "move":
			if action == "del" {
				options.problems = append(options.problems, "both 'del' and 'move' given; give one")
			}
			action = "move"
			if i+1 < len(args) && !isReservedWord(args[i+1]) {
				options.Action = MoveAction
				options.TargetDir = args[i+1]
				i++ // Skip the next argument as it's the target directory
			} else {
				options.problems = append(options.problems, "'move' needs a target folder after it")
			}

		case "delete", "del":
			if action == "move" {
				options.problems = append(options.problems, "both 'del' and 'move' given; give one")
			}
			action = "del"
			options.Action = DeleteAction

		case "-y", "--yes":
			options.BatchMode = true

		case "nohist", "no_history":
			// Read by the history logger; nothing to do here.

		default:
			options.problems = append(options.problems, fmt.Sprintf("unknown word %q", raw))
		}
	}

	return options
}

// Validate refuses options ParseArguments could not make sense of. A typo in
// a rule word must not quietly fall back to the default rule and delete the
// other copy.
func (o DuplicateOptions) Validate() error {
	if len(o.problems) > 0 {
		return &UsageError{Msg: "check-duplicates: " + strings.Join(o.problems, "; ") +
			". Words: old|new|abc|xyz, del | move <folder>, list <file>, quiet, -y"}
	}
	return nil
}

// CheckConsent refuses a deleting or moving run that would have to ask about
// every file while nobody can answer: no console, or a caller with a machine
// stop channel. -y is the only way to say yes in advance.
func (o DuplicateOptions) CheckConsent() error {
	if o.Action == NoAction || o.BatchMode || o.Interactive {
		return nil
	}
	return &UsageError{Msg: fmt.Sprintf(
		"check-duplicates: '%s' asks before each file and nobody can answer here (no console); "+
			"add -y to confirm every %s in advance", o.actionWord(), o.actionNoun())}
}

func (o DuplicateOptions) actionWord() string {
	if o.Action == MoveAction {
		return "move"
	}
	return "del"
}

func (o DuplicateOptions) actionNoun() string {
	if o.Action == MoveAction {
		return "move"
	}
	return "deletion"
}

// RuleDescription says which copy the rule keeps, in the words printed before
// any deletion (DUP-09).
func (o DuplicateOptions) RuleDescription() string {
	var s string
	switch o.SelectionMode {
	case OldestAsOriginal:
		s = "keep the oldest copy (by creation time), remove the newer ones"
	case FirstAlphaAsOriginal:
		s = "keep the alphabetically first name, remove the others"
	case LastAlphaAsOriginal:
		s = "keep the alphabetically last name, remove the others"
	default:
		s = "keep the newest copy (by creation time), remove the older ones"
	}
	if !o.SelectionModeSet {
		s += " (the default rule)"
	}
	return s
}
