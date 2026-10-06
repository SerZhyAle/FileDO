//go:build windows

package main

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"filedo/vdisk"
)

// The verdict table: a code this table does not know is never read as success, and a scan never
// calls problems "repaired".
func TestVD_ChkdskVerdict(t *testing.T) {
	cases := []struct {
		code   int
		fix    bool
		wantOK bool
		word   string
	}{
		{0, false, true, "no problems"},
		{0, true, true, "no problems"},
		{2, false, true, "no problems"}, // a scan that did not clean up: nothing wrong
		{2, true, true, "no problems"},  // cleanup performed
		{1, true, true, "repaired"},
		{3, true, false, "could not repair"},
		{3, false, false, "problems"},
		{1, false, false, "problems"}, // a scan cannot have repaired anything
		{4, true, false, "unexpected exit code 4"},
		{-1, false, false, "unexpected exit code -1"},
	}
	for _, c := range cases {
		ok, what := vdChkdskVerdict(c.code, c.fix)
		if ok != c.wantOK || !strings.Contains(what, c.word) {
			t.Errorf("code %d fix=%v: ok=%v %q, want ok=%v containing %q", c.code, c.fix, ok, what, c.wantOK, c.word)
		}
	}
}

func TestVD_ChkdskParse(t *testing.T) {
	o, err := vdParseChkdsk([]string{"a.fdd"})
	if err != nil || o.fix || o.force || o.path != "a.fdd" {
		t.Fatalf("a bare chkdsk is a scan: %+v, %v", o, err)
	}
	for _, args := range [][]string{
		{"a.fdd", "fix", "force", "p:pw"},
		{"a.fdd", "p:pw", "-y", "FIX"},
	} {
		o, err := vdParseChkdsk(args)
		if err != nil || !o.fix || !o.force || !o.cred.given() {
			t.Errorf("%v parsed as %+v, %v", args, o, err)
		}
	}
	for _, args := range [][]string{
		nil,
		{"a.fdd", "hunter2"},
		{"a.fdd", "p:a", "p:b"},
	} {
		_, err := vdParseChkdsk(args)
		if vdExitClass(err) != vdisk.ExitUsage || (len(args) > 1 && strings.Contains(err.Error(), "hunter2")) {
			t.Errorf("%v: class %d, %v", args, vdExitClass(err), err)
		}
	}
}

// chkdsk redraws its progress with a carriage return, and its report can be long: the text kept
// is the last drawing of each line and the tail of the whole, cut at a character.
func TestVD_ChkdskTidy(t *testing.T) {
	got := vdChkdskTidy("Stage 1\r\n  10 percent\r 50 percent\r100 percent\r\nDone\r\n")
	if got != "Stage 1\n100 percent\nDone" {
		t.Errorf("tidy = %q", got)
	}
	long := strings.Repeat("ёж\n", vdChkdskMaxText)
	got = vdChkdskTidy(long + "summary")
	if len(got) > vdChkdskMaxText || !utf8.ValidString(got) || !strings.HasSuffix(got, "summary") {
		t.Errorf("a long report: %d bytes, valid=%v, tail %q", len(got), utf8.ValidString(got), got[len(got)-10:])
	}
}

// A repair is refused before anything is attached when it cannot be answered (a batch never
// answers, so it says force), and a partition disk has no chkdsk.
func TestVD_ChkdskRefusals(t *testing.T) {
	dir := vdTestEnv(t)
	p := dir + `\a.fdd`
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	before := vdTestHash(t, p)
	_, err := vdTestRun(t, true, "chkdsk", p, "fix")
	if vdExitClass(err) != vdisk.ExitUsage || !strings.Contains(err.Error(), "force") {
		t.Errorf("a batch repair without force: class %d, %v", vdExitClass(err), err)
	}
	if _, err := vdTestRun(t, true, "chkdsk", p, "fix", "extra"); vdExitClass(err) != vdisk.ExitUsage {
		t.Errorf("an unknown word: %v", err)
	}
	if vdTestHash(t, p) != before {
		t.Fatal("a refused chkdsk changed the container")
	}
	if !errors.Is(errVdPartChkdsk, vdisk.ErrUnsupported) {
		t.Error("chkdsk on a partition disk is not class unsupported")
	}
}
