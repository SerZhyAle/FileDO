package fileduplicates

import (
	"strings"
	"testing"
)

// permutations returns every ordering of units (each unit is kept whole, so a
// word and its operand - `list r.lst` - stay together).
func permutations(units [][]string) [][]string {
	if len(units) <= 1 {
		var flat []string
		for _, u := range units {
			flat = append(flat, u...)
		}
		return [][]string{flat}
	}
	var out [][]string
	for i := range units {
		rest := make([][]string, 0, len(units)-1)
		rest = append(rest, units[:i]...)
		rest = append(rest, units[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append(append([]string{}, units[i]...), p...))
		}
	}
	return out
}

// DUP-03: whether each file is asked about is decided by -y alone - never by
// the rule word, never by where it stands.
func TestParseArguments_BatchModeOnlyFromYes(t *testing.T) {
	cases := []struct {
		units [][]string
		batch bool
	}{
		{[][]string{{"del"}, {"new"}}, false},
		{[][]string{{"del"}, {"old"}}, false},
		{[][]string{{"del"}, {"xyz"}}, false},
		{[][]string{{"del"}, {"abc"}}, false},
		{[][]string{{"del"}}, false},
		{[][]string{{"move", `D:\Dups`}, {"new"}}, false},
		{[][]string{{"del"}, {"new"}, {"-y"}}, true},
		{[][]string{{"del"}, {"xyz"}, {"--yes"}, {"list", "r.lst"}, {"quiet"}}, true},
		{[][]string{{"move", `D:\Dups`}, {"old"}, {"-Y"}}, true},
	}
	for _, c := range cases {
		var first *DuplicateOptions
		for _, args := range permutations(c.units) {
			o := ParseArguments(args)
			if err := o.Validate(); err != nil {
				t.Fatalf("%v: unexpected usage error: %v", args, err)
			}
			if o.BatchMode != c.batch {
				t.Errorf("%v: BatchMode = %v, want %v", args, o.BatchMode, c.batch)
			}
			if first == nil {
				first = &o
				continue
			}
			if o.SelectionMode != first.SelectionMode || o.Action != first.Action ||
				o.TargetDir != first.TargetDir || o.OutputPath != first.OutputPath ||
				o.Verbose != first.Verbose || o.SelectionModeSet != first.SelectionModeSet {
				t.Errorf("%v: options differ from another order of the same words", args)
			}
		}
	}
}

// DUP-10 and the README: each rule word names what is removed.
func TestParseArguments_RuleWords(t *testing.T) {
	want := map[string]DuplicateSelectionMode{
		"old": NewestAsOriginal,
		"new": OldestAsOriginal,
		"abc": LastAlphaAsOriginal,
		"xyz": FirstAlphaAsOriginal,
		"OLD": NewestAsOriginal,
	}
	for word, mode := range want {
		o := ParseArguments([]string{word})
		if o.SelectionMode != mode || !o.SelectionModeSet {
			t.Errorf("%s: mode %v set=%v, want %v set=true", word, o.SelectionMode, o.SelectionModeSet, mode)
		}
	}
	// DUP-02: no rule word is recorded as such, and the default is one.
	o := ParseArguments([]string{"del"})
	if o.SelectionModeSet || o.SelectionMode != NewestAsOriginal {
		t.Errorf("default: mode %v set=%v, want NewestAsOriginal set=false", o.SelectionMode, o.SelectionModeSet)
	}
}

func TestParseArguments_RefusesBadWords(t *testing.T) {
	bad := [][]string{
		{"del", "nwe"},            // a typo in a rule word must not become the default rule
		{"old", "new"},            // two rules
		{"del", "move", `D:\x`},   // two actions
		{"move"},                  // no target
		{"move", "-y"},            // an option word is not a folder name
		{"list"},                  // no file
		{"del", "list", "quiet"},  // an option word is not a file name
		{"del", "--force-please"}, // unknown
	}
	for _, args := range bad {
		err := ParseArguments(args).Validate()
		if err == nil || !IsUsageError(err) {
			t.Errorf("%v: Validate() = %v, want a usage error", args, err)
		}
	}
	good := [][]string{{}, {"quiet"}, {"s"}, {"del", "nohist"}, {"list", "dups.lst", "xyz"}}
	for _, args := range good {
		if err := ParseArguments(args).Validate(); err != nil {
			t.Errorf("%v: unexpected error %v", args, err)
		}
	}
}

// DUP-03: a deleting or moving run with nobody to ask and no -y is refused.
func TestCheckConsent(t *testing.T) {
	cases := []struct {
		args        []string
		interactive bool
		refused     bool
	}{
		{[]string{"del"}, false, true},
		{[]string{"move", `D:\x`}, false, true},
		{[]string{"del", "-y"}, false, false},
		{[]string{"del"}, true, false},
		{[]string{}, false, false},
		{[]string{"list", "x.lst"}, false, false},
	}
	for _, c := range cases {
		o := ParseArguments(c.args)
		o.Interactive = c.interactive
		err := o.CheckConsent()
		if (err != nil) != c.refused {
			t.Errorf("%v interactive=%v: CheckConsent() = %v, refused want %v", c.args, c.interactive, err, c.refused)
		}
		if err != nil && (!IsUsageError(err) || !strings.Contains(err.Error(), "-y")) {
			t.Errorf("%v: refusal %q should be a usage error naming -y", c.args, err)
		}
	}
}
