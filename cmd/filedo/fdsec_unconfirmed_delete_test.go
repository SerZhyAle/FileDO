package main

// SP-0126 acceptance: a del or wipe whose question cannot be answered is not
// a success. The five disposition prompts are covered two ways: black-box
// through the built exe (closed stdin, a typed answer, an empty line, -y),
// and on the readConsoleLine seam for what no console can stage - a read that
// fails without reaching end of input, and proof that -y never reads.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filedo/fdsec"
)

// packForDel stages one packed original in dir: plain.txt packed and deleted,
// so the unsecure staging below restores onto its true name.
func packForDel(t *testing.T, dir string) []byte {
	t.Helper()
	payload := []byte("the original bytes behind the container")
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "plain.txt", "secure", "del", "-y", "p:pw"); code != 0 {
		t.Fatalf("staging secure del -y exited %d\n%s", code, out)
	}
	return payload
}

func TestUnconfirmedDeleteIsNotASuccess(t *testing.T) {
	t.Run("file del, nobody to ask", func(t *testing.T) {
		dir, payload := workdir(t)
		events := filepath.Join(dir, "events.jsonl")
		out, code := run(t, dir, "--events", events, "plain.txt", "secure", "p:pw", "del")
		if code != 2 {
			t.Fatalf("secure del with a closed stdin exited %d, want 2 (usage)\n%s", code, out)
		}
		if got := lastResult(t, events)["verdict"]; got != "Not proven" {
			t.Errorf("verdict %q, want Not proven\n%s", got, out)
		}
		if !exists(filepath.Join(dir, "plain.txt")) {
			t.Fatal("the original was deleted although nobody could confirm")
		}
		if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
			t.Error("the original was modified although nobody could confirm")
		}
		if !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Error("the verified container did not survive the unanswered del")
		}
		if !strings.Contains(out, "Original kept") {
			t.Errorf("the run does not say the original was kept\n%s", out)
		}
		if !strings.Contains(out, "-y") {
			t.Errorf("the refusal does not name the flag that settles it\n%s", out)
		}
	})

	// R3: a typed refusal stays what it is - Done, exit 0.
	t.Run("file del, typed n", func(t *testing.T) {
		dir, payload := workdir(t)
		out, code := runWithStdin(t, dir, "n\n", "plain.txt", "secure", "p:pw", "del")
		if code != 0 {
			t.Fatalf("secure del with a typed n exited %d, want 0\n%s", code, out)
		}
		if !exists(filepath.Join(dir, "plain.txt")) {
			t.Fatal("the original was deleted although n was typed")
		}
		if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
			t.Error("the original was modified although n was typed")
		}
		if !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Error("the container did not survive the refusal")
		}
	})

	// An empty line at a console is a refusal, not a missing answer.
	t.Run("file del, an empty line is a refusal", func(t *testing.T) {
		dir, _ := workdir(t)
		out, code := runWithStdin(t, dir, "\n", "plain.txt", "secure", "p:pw", "del")
		if code != 0 {
			t.Fatalf("secure del with an empty line exited %d, want 0\n%s", code, out)
		}
		if !exists(filepath.Join(dir, "plain.txt")) || !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Fatal("an empty Enter removed the original or lost the container")
		}
	})

	// A redirected-but-answered stdin is still an answer (spec section 3).
	t.Run("file del, typed y", func(t *testing.T) {
		dir, payload := workdir(t)
		out, code := runWithStdin(t, dir, "y\n", "plain.txt", "secure", "p:pw", "del")
		if code != 0 {
			t.Fatalf("secure del with a typed y exited %d, want 0\n%s", code, out)
		}
		if exists(filepath.Join(dir, "plain.txt")) {
			t.Fatal("the original survived a confirmed del")
		}
		if got := mustRead(t, filepath.Join(dir, "plain.fd-sec")); len(got) == 0 {
			t.Fatal("no container written")
		}
		if out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:pw"); code != 0 {
			t.Fatalf("unsecure after the del exited %d\n%s", code, out)
		}
		if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
			t.Error("the file restored after the confirmed del is not the original")
		}
	})

	t.Run("unsecure del, nobody to ask", func(t *testing.T) {
		dir := t.TempDir()
		payload := packForDel(t, dir)
		events := filepath.Join(dir, "events.jsonl")
		out, code := run(t, dir, "--events", events, "plain.fd-sec", "unsecure", "del", "p:pw")
		if code != 2 {
			t.Fatalf("unsecure del with a closed stdin exited %d, want 2 (usage)\n%s", code, out)
		}
		if got := lastResult(t, events)["verdict"]; got != "Not proven" {
			t.Errorf("verdict %q, want Not proven\n%s", got, out)
		}
		if !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Error("the container was deleted although nobody could confirm")
		}
		if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
			t.Error("the restored file is not the original")
		}
		if !strings.Contains(out, "-y") {
			t.Errorf("the refusal does not name the flag that settles it\n%s", out)
		}
	})

	t.Run("unsecure del, typed y", func(t *testing.T) {
		dir := t.TempDir()
		payload := packForDel(t, dir)
		out, code := runWithStdin(t, dir, "y\n", "plain.fd-sec", "unsecure", "del", "p:pw")
		if code != 0 {
			t.Fatalf("unsecure del with a typed y exited %d, want 0\n%s", code, out)
		}
		if exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Error("the container survived a confirmed del")
		}
		if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
			t.Error("the restored file is not the original")
		}
	})

	t.Run("unsecure del, typed n", func(t *testing.T) {
		dir := t.TempDir()
		payload := packForDel(t, dir)
		out, code := runWithStdin(t, dir, "n\n", "plain.fd-sec", "unsecure", "del", "p:pw")
		if code != 0 {
			t.Fatalf("unsecure del with a typed n exited %d, want 0\n%s", code, out)
		}
		if !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Fatal("the container was deleted although n was typed")
		}
		if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
			t.Error("the restored file is not the original")
		}
	})
}

// The batch path reports the same refusal the typed line would: the line
// fails, the batch carries on, and the run is Not proven rather than Done.
func TestUnconfirmedDeleteInABatch(t *testing.T) {
	dir, _ := workdir(t)
	lst := strings.Join([]string{
		"plain.txt secure p:pw del",
		"fdsec info plain.fd-sec p:pw",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "work.lst"), []byte(lst), 0o644); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(dir, "events.jsonl")
	out, code := run(t, dir, "--events", events, "from", "work.lst")
	if code != 2 {
		t.Fatalf("the batch exited %d, want 2 (Not proven)\n%s", code, out)
	}
	if got := lastResult(t, events)["verdict"]; got != "Not proven" {
		t.Errorf("verdict %q, want Not proven\n%s", got, out)
	}
	if !strings.Contains(out, "1/2") {
		t.Errorf("the batch did not carry on past the refused line\n%s", out)
	}
	if !exists(filepath.Join(dir, "plain.txt")) || !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Fatal("the refused del removed the original or lost the container")
	}
}

// seamSecureOne packs dir/plain.txt through fdsecSecureOne with the prompt
// answered by whatever readConsoleLine is set to. It returns the error and
// the history logger for the result keys.
func seamSecureOne(t *testing.T, dir string, o *fdsecOpts) (*HistoryLogger, error) {
	t.Helper()
	src := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(src, []byte("seam payload for the disposition prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	hl := NewHistoryLogger([]string{"seam-test"})
	err := fdsecSecureOne(src, o, fdsec.NewCredential("pw"), hl)
	return hl, err
}

func TestDispositionPromptsOnTheSeam(t *testing.T) {
	savedReader := readConsoleLine
	savedHandler := globalInterruptHandler
	defer func() {
		readConsoleLine = savedReader
		globalInterruptHandler = savedHandler
	}()
	globalInterruptHandler = newInterruptHandlerNoSignals()

	// A read that fails without reaching end of input - the case no console
	// can stage - must land in the same class as a closed stdin.
	t.Run("file wipe, a failing read is usage", func(t *testing.T) {
		dir := t.TempDir()
		readConsoleLine = func() (string, error) { return "", errors.New("injected read failure") }
		hl, err := seamSecureOne(t, dir, &fdsecOpts{wipe: true})
		if !errors.Is(err, errFdsecUsage) {
			t.Fatalf("err = %v, want the usage class", err)
		}
		if got := hl.entry.Results["original"]; got != "not-asked" {
			t.Errorf("history original = %v, want not-asked", got)
		}
		if !exists(filepath.Join(dir, "plain.txt")) || !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Error("the failing read removed the original or lost the container")
		}
	})

	t.Run("file del, a failing read is usage", func(t *testing.T) {
		dir := t.TempDir()
		readConsoleLine = func() (string, error) { return "", errors.New("injected read failure") }
		hl, err := seamSecureOne(t, dir, &fdsecOpts{del: true})
		if !errors.Is(err, errFdsecUsage) {
			t.Fatalf("err = %v, want the usage class", err)
		}
		if got := hl.entry.Results["original"]; got != "not-asked" {
			t.Errorf("history original = %v, want not-asked", got)
		}
		if !exists(filepath.Join(dir, "plain.txt")) || !exists(filepath.Join(dir, "plain.fd-sec")) {
			t.Error("the failing read removed the original or lost the container")
		}
	})

	t.Run("file wipe, WIPE typed", func(t *testing.T) {
		dir := t.TempDir()
		readConsoleLine = func() (string, error) { return "WIPE", nil }
		hl, err := seamSecureOne(t, dir, &fdsecOpts{wipe: true})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := hl.entry.Results["original"]; got != "wiped" {
			t.Errorf("history original = %v, want wiped", got)
		}
		if exists(filepath.Join(dir, "plain.txt")) {
			t.Error("the typed WIPE did not remove the original")
		}
	})

	t.Run("file del, n typed", func(t *testing.T) {
		dir := t.TempDir()
		readConsoleLine = func() (string, error) { return "n", nil }
		hl, err := seamSecureOne(t, dir, &fdsecOpts{del: true})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := hl.entry.Results["original"]; got != "kept" {
			t.Errorf("history original = %v, want kept", got)
		}
		if !exists(filepath.Join(dir, "plain.txt")) {
			t.Error("the typed n removed the original")
		}
	})

	// R5: -y reaches no reader at all.
	t.Run("file del, -y never asks", func(t *testing.T) {
		dir := t.TempDir()
		readConsoleLine = func() (string, error) { t.Error("the prompt read stdin under -y"); return "", nil }
		hl, err := seamSecureOne(t, dir, &fdsecOpts{del: true, assumeYes: true})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := hl.entry.Results["original"]; got != "deleted" {
			t.Errorf("history original = %v, want deleted", got)
		}
		if exists(filepath.Join(dir, "plain.txt")) {
			t.Error("-y did not delete the original")
		}
	})

	t.Run("folder del, a failing read is usage", func(t *testing.T) {
		dir := t.TempDir()
		folder := filepath.Join(dir, "Album")
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("one"), 0o644); err != nil {
			t.Fatal(err)
		}
		readConsoleLine = func() (string, error) { return "", errors.New("injected read failure") }
		hl := NewHistoryLogger([]string{"seam-test"})
		err := fdsecSecureTree(folder, &fdsecOpts{del: true}, fdsec.NewCredential("pw"), hl)
		if !errors.Is(err, errFdsecUsage) {
			t.Fatalf("err = %v, want the usage class", err)
		}
		if got := hl.entry.Results["original"]; got != "not-asked" {
			t.Errorf("history original = %v, want not-asked", got)
		}
		if !exists(filepath.Join(folder, "a.txt")) || !exists(filepath.Join(dir, "Album.fd-sec")) {
			t.Error("the failing read removed the folder or lost the container")
		}
	})

	t.Run("folder wipe, WIPE typed", func(t *testing.T) {
		dir := t.TempDir()
		folder := filepath.Join(dir, "Album")
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("one"), 0o644); err != nil {
			t.Fatal(err)
		}
		readConsoleLine = func() (string, error) { return "WIPE", nil }
		hl := NewHistoryLogger([]string{"seam-test"})
		err := fdsecSecureTree(folder, &fdsecOpts{wipe: true}, fdsec.NewCredential("pw"), hl)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := hl.entry.Results["original"]; got != "wiped" {
			t.Errorf("history original = %v, want wiped", got)
		}
		if exists(folder) {
			t.Error("the typed WIPE did not remove the folder")
		}
	})

	t.Run("unsecure del, a failing read is usage", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := seamSecureOne(t, dir, &fdsecOpts{}); err != nil {
			t.Fatalf("staging pack failed: %v", err)
		}
		container := filepath.Join(dir, "plain.fd-sec")
		src, err := os.Open(container)
		if err != nil {
			t.Fatal(err)
		}
		readConsoleLine = func() (string, error) { return "", errors.New("injected read failure") }
		hl := NewHistoryLogger([]string{"seam-test"})
		err = fdsecDeleteContainer(container, src, &fdsecOpts{del: true}, hl, "verified for the seam test.")
		if !errors.Is(err, errFdsecUsage) {
			t.Fatalf("err = %v, want the usage class", err)
		}
		if got := hl.entry.Results["container"]; got != "not-asked" {
			t.Errorf("history container = %v, want not-asked", got)
		}
		if !exists(container) {
			t.Error("the failing read deleted the container")
		}
	})
}
