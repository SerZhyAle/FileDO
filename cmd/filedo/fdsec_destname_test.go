package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filedo/fdsec"
)

// The destination-name predicate (FDSEC-FORMAT 1.4 section 20, FDSEC-BEHAVIOUR
// 1.6 section 18). Reading a container and restoring it to this Windows
// destination are two questions: fdsec.ValidateName answers the first and
// accepts U+0022 U+003C U+003E U+007C; these tests pin the second - the four
// characters and a name over 255 UTF-16 units stop a restore as a destination
// failure (exit class 5, one sentence, never the name, nothing written) and
// never as a damaged container.

var destFour = []string{`"`, `<`, `>`, `|`}

func TestDestinationName_PredicateIsTheFourCharactersAnd255Units(t *testing.T) {
	for _, c := range destFour {
		if fdsecNameFitsDestination("a" + c + "b.txt") {
			t.Errorf("a name with U+%04X fits a Windows destination", []rune(c)[0])
		}
		// The format accepts every one of them: the refusal is the destination's.
		if err := fdsec.ValidateName("a" + c + "b.txt"); err != nil {
			t.Errorf("the format refuses U+%04X, which section 20 keeps valid: %v", []rune(c)[0], err)
		}
	}
	for _, ok := range []string{"holiday.mp4", "Отчёт за квартал.docx", "!#;={}.txt", "a b.c.txt", "CONTRACT.pdf", strings.Repeat("n", 251) + ".txt"} {
		if !fdsecNameFitsDestination(ok) {
			t.Errorf("%q does not fit, but a Windows destination can create it", ok)
		}
	}
	// 255 UTF-16 units fit, 256 do not - counted in units, not characters: an
	// astral character is two.
	if !fdsecNameFitsDestination(strings.Repeat("n", 255)) || fdsecNameFitsDestination(strings.Repeat("n", 256)) {
		t.Error("the limit is not 255 units")
	}
	if !fdsecNameFitsDestination(strings.Repeat("\U0001F600", 127)+"a") || fdsecNameFitsDestination(strings.Repeat("\U0001F600", 128)) {
		t.Error("astral characters are not counted as two units each")
	}
}

func TestDestinationName_ErrorIsOneSentenceNamingTheRuleNeverTheName(t *testing.T) {
	const distinctive = "zebra"
	for _, use := range []fdsecNameUse{fdsecUseFile, fdsecUseReveal, fdsecUseTreeEntry} {
		for _, name := range []string{distinctive + "|crossing.txt", distinctive + `"q`, distinctive + "<>", strings.Repeat(distinctive, 60)} {
			err := fdsecDestinationNameCheck(name, use)
			if err == nil {
				t.Fatalf("use %d: %q passed", use, name[:8])
			}
			msg := err.Error()
			if strings.Contains(msg, distinctive) {
				t.Errorf("use %d: the refusal echoes the sealed name: %s", use, msg)
			}
			if !strings.Contains(msg, fdsecDestinationNameMarker) {
				t.Errorf("use %d: the refusal lacks the marker the window looks for: %s", use, msg)
			}
			if strings.Contains(msg, "\n") {
				t.Errorf("use %d: the refusal is not one sentence: %q", use, msg)
			}
			if strings.Contains(strings.ToLower(msg), "damaged") {
				t.Errorf("use %d: a destination failure is worded as damage: %s", use, msg)
			}
			// The outcome class is the I/O one: none of the container classes
			// and not a usage error, so fdsecSetExit's default branch (5) takes it.
			for _, other := range []error{fdsec.ErrDamaged, fdsec.ErrCredentialOrTamper, fdsec.ErrUnsupported, errFdsecUsage, fdsec.ErrTreeContainer, fdsec.ErrFileContainer, errRunStopped} {
				if errors.Is(err, other) {
					t.Errorf("use %d: the refusal wraps %v, so it would not exit 5", use, other)
				}
			}
			// And its event/history text is itself: it holds no name, and a
			// window with no console needs the sentence, not "see the console".
			if got := eventSafeErrorMessage(fdsecScreenEventError(err)); got != msg {
				t.Errorf("use %d: the event text is %q, want the sentence itself", use, got)
			}
		}
	}
	// A name that fits is no error.
	if err := fdsecDestinationNameCheck("fine.txt", fdsecUseFile); err != nil {
		t.Errorf("fine.txt: %v", err)
	}
	// The way out is named where there is one.
	if m := fdsecDestinationNameCheck("a|b", fdsecUseFile).Error(); !strings.Contains(m, "to <path>") || !strings.Contains(m, "rename") {
		t.Errorf("the file refusal does not name its way out: %s", m)
	}
	if m := fdsecDestinationNameCheck("a|b", fdsecUseReveal).Error(); !strings.Contains(m, "unsecure to <path>") {
		t.Errorf("the reveal refusal does not point at unsecure: %s", m)
	}
}

// rename and an exact to <path> do not use the sealed name, so they are the
// ways out; a default destination, here, and a to <existing folder> do.
func TestDestinationName_RenameAndExactToBypassThePredicate(t *testing.T) {
	dir := t.TempDir()
	container := filepath.Join(dir, "c.fd-sec")
	bad := `a|b.txt`
	long := strings.Repeat("n", 256) + ".txt"

	for _, name := range []string{bad, long} {
		for label, o := range map[string]*fdsecOpts{
			"default":            {},
			"here":               {here: true},
			"to an existing dir": {haveTo: true, to: dir},
		} {
			got, err := fdsecRestorePath(container, name, o)
			var dest *fdsecDestinationNameError
			if !errors.As(err, &dest) {
				t.Errorf("%s, %.8q: got %q, %v; want the destination-name error", label, name, got, err)
			}
			if errors.Is(err, errFdsecUsage) {
				t.Errorf("%s: the refusal is a usage error (exit 2), want the destination class (5)", label)
			}
		}
		// rename: a random name, whatever the sealed one is.
		got, err := fdsecRestorePath(container, name, &fdsecOpts{rename: true})
		if err != nil || strings.ContainsAny(filepath.Base(got), `"<>|`) || utf16Len(filepath.Base(got)) > fdsecMaxNameUnits {
			t.Errorf("rename: %q, %v", got, err)
		}
		// to <exact path>: the user's own name, even one the sealed name could not be.
		exact := filepath.Join(dir, "chosen.txt")
		got, err = fdsecRestorePath(container, name, &fdsecOpts{haveTo: true, to: exact})
		if err != nil || got != exact {
			t.Errorf("exact to: %q, %v", got, err)
		}
	}
	// A name the destination can create is untouched by all of this.
	if got, err := fdsecRestorePath(container, "ok.txt", &fdsecOpts{}); err != nil || got != filepath.Join(dir, "ok.txt") {
		t.Errorf("an ordinary name: %q, %v", got, err)
	}
	// The same question, as the early gate asks it.
	if fdsecSealedNameIsUsed(&fdsecOpts{rename: true}, false) || fdsecSealedNameIsUsed(&fdsecOpts{rename: true}, true) {
		t.Error("rename uses the sealed name")
	}
	if fdsecSealedNameIsUsed(&fdsecOpts{haveTo: true, to: filepath.Join(dir, "x.txt")}, false) {
		t.Error("an exact to <path> uses the sealed name")
	}
	if !fdsecSealedNameIsUsed(&fdsecOpts{haveTo: true, to: dir}, false) || !fdsecSealedNameIsUsed(&fdsecOpts{}, false) || !fdsecSealedNameIsUsed(&fdsecOpts{}, true) {
		t.Error("a default destination, a folder and a start sandbox use the sealed name")
	}
}

// The sandbox of reveal and of unsecure start keeps both questions apart: a
// name the FORMAT refuses is damage and exactly as strict as before; a name the
// format accepts and Windows cannot create is the destination failure.
func TestSandboxName_FormatRefusalsStayDamageAndDestinationRefusalsAreNot(t *testing.T) {
	for _, bad := range []string{`..\x`, `a/b`, `a:b`, `a*b`, `a?b`, "a\x01b", "CON", "nul.txt", "trailing.", "trailing ", ""} {
		for _, f := range []func(string) (string, error){
			fdsecSandboxName,
			func(n string) (string, error) { return fdsecSandboxNameFor(n, fdsecUseFile) },
		} {
			_, err := f(bad)
			if !errors.Is(err, fdsec.ErrDamaged) {
				t.Errorf("sandbox name %q: got %v, want damage (the format's own refusal)", bad, err)
			}
			var dest *fdsecDestinationNameError
			if errors.As(err, &dest) {
				t.Errorf("sandbox name %q: a format refusal is reported as a destination failure", bad)
			}
		}
	}
	// FDSEC-FORMAT 1.4 section 20: valid for the format, impossible here.
	for _, name := range append(destFourNames(), strings.Repeat("n", 256)+".txt", strings.Repeat("s", 300)+".txt") {
		_, err := fdsecSandboxName(name)
		var dest *fdsecDestinationNameError
		if !errors.As(err, &dest) || errors.Is(err, fdsec.ErrDamaged) {
			t.Errorf("sandbox name %.10q: got %v, want the destination-name error and not damage", name, err)
		}
	}
	// A name the destination can create passes, neighbours of the four included.
	for _, ok := range []string{"holiday.mp4", "!#;={}.txt", "CONTRACT.pdf", strings.Repeat("n", 251) + ".txt"} {
		if got, err := fdsecSandboxName(ok); err != nil || got != ok {
			t.Errorf("sandbox name %q: %q, %v", ok, got, err)
		}
	}
}

func destFourNames() []string {
	var out []string
	for _, c := range destFour {
		out = append(out, "a"+c+"b.txt")
	}
	return append(out, `"<>|`)
}

// The window's side: the result card turns a run into its own-language sentence
// by the words the CLI's refusal carries (FdsecResults.DestinationNameRefusal
// in filedo_win_vb\JobCatalogue.vb), and every language table holds the
// sentence. The shell's self-test (group fdsec-name) holds the rest.
func TestDestinationName_TheWindowLooksForTheWordsTheCLISays(t *testing.T) {
	vb, err := os.ReadFile(filepath.Join("..", "..", "filedo_win_vb", "JobCatalogue.vb"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vb), `"`+fdsecDestinationNameMarker+`"`) {
		t.Errorf("filedo_win_vb/JobCatalogue.vb does not look for %q: the card would show no sentence for a name this PC cannot create", fdsecDestinationNameMarker)
	}
	for _, use := range []fdsecNameUse{fdsecUseFile, fdsecUseReveal, fdsecUseTreeEntry} {
		if err := fdsecDestinationNameCheck(`a|b`, use); err == nil || !strings.Contains(err.Error(), fdsecDestinationNameMarker) {
			t.Errorf("use %d: the CLI sentence does not carry the marker the window looks for: %v", use, err)
		}
	}
	loc, err := os.ReadFile(filepath.Join("..", "..", "filedo_win_vb", "Localization.vb"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(loc), `"shell_fdsec_dest_name|`); n != 5 {
		t.Errorf("shell_fdsec_dest_name is in %d language tables, want 5", n)
	}
}
