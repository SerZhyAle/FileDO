package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The static half of the shared desktop contracts, APP-BEHAVIOUR and
// APP-STYLE, read over the window program's sources (filedo_win_vb). FileDO
// is a consumer of both; docs/contracts/ says what it owes them. The dynamic
// half - pages built, painted, measured and walked - is the shell's own
// `filedo_win.exe --selftest`.
//
// Each test is a rung of a contract's conformance ladder that can be held by
// reading the code, which is why it is here and not in the self-test: a
// source rule is cheapest to enforce where the source is.

// shellSources returns every .vb file of the window program, by base name.
func shellSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, "filedo_win_vb")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot list %s: %v", dir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".vb") {
			continue
		}
		out[e.Name()] = readSurface(t, root, filepath.Join("filedo_win_vb", e.Name()))
	}
	if len(out) == 0 {
		t.Fatal("filedo_win_vb holds no .vb files")
	}
	return out
}

// codeLines splits a VB file into lines with the comment part removed, so a
// rule about code is not tripped by a comment that describes the rule.
func codeLines(body string) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, ln := range lines {
		lines[i] = stripVBComment(ln)
	}
	return lines
}

// stripVBComment drops a ' comment that is not inside a string literal. VB
// escapes a quote inside a string by doubling it, which the toggle handles.
func stripVBComment(line string) string {
	inString := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inString = !inString
		case '\'':
			if !inString {
				return line[:i]
			}
		}
	}
	return line
}

var catchVar = regexp.MustCompile(`(?i)\bCatch\s+(\w+)\s+As\b`)

// APP-BEHAVIOUR rule 6: an exception's text never reaches the user. It goes
// to the shell's log (ShellLog), and the screen gets a named cause read from
// the exception's type. The test finds every variable a Catch binds and fails
// on its .Message outside a logging call. The allow-list is short and each
// entry says why: the self-test's own report, and the log archive's manifest,
// which the author reads and the screen never shows.
func TestShell_NoExceptionTextReachesTheScreen(t *testing.T) {
	allowFile := map[string]bool{"SelfTest.vb": true}
	allowLine := []struct{ file, contains string }{
		{"LogReport.vb", `c.Skip = "left out: could not be read (" & ex.Message`},
		{"LogReport.vb", `Return "filedo.exe found, could not be read (" & ex.Message`},
	}
	for name, body := range shellSources(t) {
		if allowFile[name] {
			continue
		}
		vars := map[string]bool{}
		for _, m := range catchVar.FindAllStringSubmatch(body, -1) {
			vars[m[1]] = true
		}
		for n, ln := range codeLines(body) {
			for v := range vars {
				if !regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\.Message\b`).MatchString(ln) {
					continue
				}
				if strings.Contains(ln, "ShellLog.") {
					continue
				}
				allowed := false
				for _, a := range allowLine {
					if a.file == name && strings.Contains(ln, a.contains) {
						allowed = true
					}
				}
				if !allowed {
					t.Errorf("%s:%d shows a caught exception's text (%s.Message): name the cause from its type and log the exception (APP-BEHAVIOUR rule 6)", name, n+1, v)
				}
			}
		}
	}
}

// APP-BEHAVIOUR rule 7 and rung B3: a translated template is filled by
// Localization.Format, which never throws. String.Format on a translation is
// the call that throws the day a translator drops a brace.
func TestShell_TranslatedTemplatesUseTheSafeFormatter(t *testing.T) {
	forbidden := []string{"String.Format(L(", "String.Format(LText(", "String.Format(Localization.T("}
	for name, body := range shellSources(t) {
		for n, ln := range codeLines(body) {
			for _, f := range forbidden {
				if strings.Contains(ln, f) {
					t.Errorf("%s:%d fills a translated template with %s - use Localization.Format, which cannot throw", name, n+1, strings.TrimSuffix(f, "("))
				}
			}
		}
	}
}

var (
	locLine     = regexp.MustCompile(`^\s*"([a-z0-9_]+)\|(.*)",?\s*$`)
	placeholder = regexp.MustCompile(`\{(\d+)(?:,[^}:]*)?(?::[^}]*)?\}`)
)

// APP-BEHAVIOUR rung B1: dictionary parity. Every key of the English table
// is in each of the other four, no table has a key English lacks, and every
// key carries the same placeholders in every language - a German template
// with {0} where English has {0} and {1} renders a sentence with a hole in
// it. The named-key test above guards the fdsec strings' meaning; this one
// guards the tables' shape, all keys at once.
func TestShell_EveryLocaleHasEveryKeyAndPlaceholder(t *testing.T) {
	root := repoRoot(t)
	body := strings.ReplaceAll(readSurface(t, root, filepath.Join("filedo_win_vb", "Localization.vb")), "\r\n", "\n")

	tables := map[string]map[string]string{}
	for _, fn := range []string{"EnLines", "RuLines", "UkLines", "DeLines", "FrLines"} {
		start := strings.Index(body, "Private Function "+fn+"() As String()")
		if start < 0 {
			t.Fatalf("Localization.vb has no %s table", fn)
		}
		end := strings.Index(body[start:], "\n    End Function")
		if end < 0 {
			t.Fatalf("the %s table is not terminated", fn)
		}
		table := map[string]string{}
		for _, ln := range strings.Split(body[start:start+end], "\n") {
			m := locLine.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			if _, dup := table[m[1]]; dup {
				t.Errorf("%s has %s twice - the second silently wins", fn, m[1])
			}
			table[m[1]] = m[2]
		}
		if len(table) == 0 {
			t.Fatalf("the %s table parsed to nothing", fn)
		}
		tables[fn] = table
	}

	placeholders := func(v string) string {
		set := map[string]bool{}
		for _, m := range placeholder.FindAllStringSubmatch(strings.ReplaceAll(strings.ReplaceAll(v, "{{", ""), "}}", ""), -1) {
			set[m[1]] = true
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return strings.Join(keys, ",")
	}

	en := tables["EnLines"]
	for fn, table := range tables {
		if fn == "EnLines" {
			continue
		}
		for k, ev := range en {
			v, ok := table[k]
			if !ok {
				t.Errorf("%s has no %s - that locale falls back to English in silence", fn, k)
				continue
			}
			if placeholders(v) != placeholders(ev) {
				t.Errorf("%s %s carries placeholders {%s}, English carries {%s}", fn, k, placeholders(v), placeholders(ev))
			}
		}
		for k := range table {
			if _, ok := en[k]; !ok {
				t.Errorf("%s has %s, which English does not - a key nothing falls back for", fn, k)
			}
		}
	}
}

// APP-BEHAVIOUR rule 1 and rung B4, the static sweep: every dialog is owned
// by the window that asked. The shell's questions are ShellDialog, which has
// an Escape; a Win32 message box with Yes and No has none. One message box is
// allowed, and only in Program.vb: the notice of an exception on another
// thread while the process is ending, when no window can be trusted as owner.
func TestShell_EveryDialogHasAnOwner(t *testing.T) {
	for name, body := range shellSources(t) {
		for n, ln := range codeLines(body) {
			if strings.Contains(ln, ".ShowDialog()") {
				t.Errorf("%s:%d opens a dialog with no owner - pass FindForm() (APP-BEHAVIOUR rule 1)", name, n+1)
			}
			if strings.Contains(ln, "MessageBox.Show(") && name != "Program.vb" {
				t.Errorf("%s:%d uses a system message box - use ShellDialog, which is owned, themed and has an Escape", name, n+1)
			}
		}
	}
	program := shellSources(t)["Program.vb"]
	if strings.Count(program, "MessageBox.Show(") > 1 {
		t.Error("Program.vb has more than the one standalone notice it is allowed")
	}
}

// APP-STYLE section 3 and the shell's own rule (AGENTS.md): Theme.vb is the
// only file that names a colour, and mixing two tokens counts as naming one,
// so Theme.Blend is private to it.
func TestShell_OnlyTheThemeNamesAColour(t *testing.T) {
	named := regexp.MustCompile(`\bColor\.|\bFromArgb\b|\bRGB\(|\bBlend\(`)
	for name, body := range shellSources(t) {
		if name == "Theme.vb" {
			continue
		}
		for n, ln := range codeLines(body) {
			if named.MatchString(ln) {
				t.Errorf("%s:%d names or mixes a colour outside Theme.vb: %s", name, n+1, strings.TrimSpace(ln))
			}
		}
	}
}

// APP-BEHAVIOUR rule 2: growing text grows the surface. The rail used to cut
// labels with an ellipsis, and the shipped German screenshot showed it; the
// rows wrap now, and the self-test measures every label in every language.
// What this holds is that the ellipsis does not come back.
func TestShell_TheRailDoesNotCutLabels(t *testing.T) {
	rail := shellSources(t)["Rail.vb"]
	for n, ln := range codeLines(rail) {
		if strings.Contains(ln, "EndEllipsis") {
			t.Errorf("Rail.vb:%d cuts a label with an ellipsis (APP-BEHAVIOUR rule 2)", n+1)
		}
	}
}
