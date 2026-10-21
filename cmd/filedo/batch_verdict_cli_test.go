package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Black-box tests for AUD-29-F1: a run that recorded a defect ends Failed
// whatever a container line set, and the exit digit of a batch of several
// lines is CLI-EVENT-STREAM rule 11's (0.10 rule 12); a container command run
// alone keeps its FDSEC-BEHAVIOUR class (TestFdsecVerifyWrongPasswordFromBatch).

// TestBatchDefectSurvivesContainerFailure is the ticket's repro: `check` finds
// a file already on check_damaged.list, then `fdsec info` on a missing
// container fails. The defect is the run's answer: Failed, exit 1.
func TestBatchDefectSurvivesContainerFailure(t *testing.T) {
	root := checkTree(t, 3)
	wd := t.TempDir()
	var bad string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && bad == "" {
			bad = p
		}
		return nil
	})
	l, err := loadStateList(filepath.Join(wd, checkDamagedName))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(bad)
	l.AddInfo(bad, info)
	l.Close()

	writeLst(t, wd, "mix.lst", "check \""+root+"\"\nfdsec info \""+filepath.Join(wd, "nosuch.fd-sec")+"\"\n")
	events := filepath.Join(wd, "ev.jsonl")
	out, code := run(t, wd, "from", "mix.lst", "--events", events)
	if v := lastResult(t, events)["verdict"]; v != "Failed" {
		t.Fatalf("verdict %v, want Failed\n%s", v, out)
	}
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	found := false
	for _, ev := range readEvents(t, events) {
		if ev["kind"] != "finding" {
			continue
		}
		if d, ok := ev["data"].(map[string]interface{}); ok && d["type"] == "damaged-files" {
			found = true
		}
	}
	if !found {
		t.Errorf("the damaged-files finding is missing from the stream")
	}
}

// TestBatchContainerAnswerUsesRule11Digit: in a batch of several lines a wrong
// credential on one container line is still the run's "no" after a later
// container line failed with another class - Failed, exit 1, not the last
// line's class.
func TestBatchContainerAnswerUsesRule11Digit(t *testing.T) {
	dir, _ := workdir(t)
	if out, code := run(t, dir, "plain.txt", "secure", "p:right"); code != 0 {
		t.Fatalf("setup secure exited %d\n%s", code, out)
	}
	writeLst(t, dir, "two.lst", "fdsec verify plain.fd-sec p:wrong\nfdsec info \""+filepath.Join(dir, "nosuch.fd-sec")+"\"\n")
	events := filepath.Join(dir, "ev.jsonl")
	out, code := run(t, dir, "--events", events, "from", "two.lst")
	if v := lastResult(t, events)["verdict"]; v != "Failed" {
		t.Fatalf("verdict %v, want Failed\n%s", v, out)
	}
	if code != 1 {
		t.Fatalf("exit %d, want 1 (rule 11)\n%s", code, out)
	}

	// Two container lines that could not judge: Not proven, and the digit is
	// rule 11's 2, not a container class.
	writeLst(t, dir, "io.lst", "fdsec info \""+filepath.Join(dir, "nosuch1.fd-sec")+"\"\nfdsec info \""+filepath.Join(dir, "nosuch2.fd-sec")+"\"\n")
	os.Remove(events)
	out, code = run(t, dir, "--events", events, "from", "io.lst")
	if v := lastResult(t, events)["verdict"]; v != "Not proven" {
		t.Fatalf("verdict %v, want Not proven\n%s", v, out)
	}
	if code != 2 {
		t.Fatalf("exit %d, want 2 (rule 11)\n%s", code, out)
	}
}
