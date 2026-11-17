package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// SP-0230: dated retention (`keep-days`) and monthly-first preservation
// (`keep-monthly`) of `backup` (CLI-EVENT-STREAM 0.18 items I to K).

func TestBackupKeepTimestamp(t *testing.T) {
	loc := time.Local
	cases := []struct {
		name string
		stem string
		ext  string
		want time.Time
		ok   bool
	}{
		{"Photos-2611171300.zip", "Photos", ".zip", time.Date(2026, 11, 17, 13, 0, 0, 0, loc), true},
		{"photos-2611171300.ZIP", "Photos", ".zip", time.Date(2026, 11, 17, 13, 0, 0, 0, loc), true},
		{"my-docs-2402290815.fd-sec", "my-docs", ".fd-sec", time.Date(2024, 2, 29, 8, 15, 0, 0, loc), true}, // leap day
		{"Photos-2602291300.zip", "Photos", ".zip", time.Time{}, false},                                     // non-leap 29 Feb
		{"Photos-2613011300.zip", "Photos", ".zip", time.Time{}, false},                                     // month 13
		{"Photos-2611311300.zip", "Photos", ".zip", time.Time{}, false},                                     // Nov 31
		{"Photos-2611172500.zip", "Photos", ".zip", time.Time{}, false},                                     // hour 25
		{"Photos-2611171300.fd-sec", "Photos", ".zip", time.Time{}, false},                                  // wrong ext
		{"Photos-2611171300.zip.filedo-partial", "Photos", ".zip", time.Time{}, false},
		{"Photos-2611171300.zip.bak", "Photos", ".zip", time.Time{}, false},
		{"Other-2611171300.zip", "Photos", ".zip", time.Time{}, false},
		{"Photos_2611171300.zip", "Photos", ".zip", time.Time{}, false}, // underscore, not hyphen
		{"Photos-261117130.zip", "Photos", ".zip", time.Time{}, false},  // 9 digits
		{"Photos-26111713001.zip", "Photos", ".zip", time.Time{}, false},
		{"-2611171300.zip", "Photos", ".zip", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := backupKeepTimestamp(c.name, c.stem, c.ext)
		if ok != c.ok || (ok && !got.Equal(c.want)) {
			t.Errorf("backupKeepTimestamp(%q) = (%v, %v), want (%v, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestBackupKeepSelect_Synthetic(t *testing.T) {
	loc := time.Local

	t.Run("month and year boundaries", func(t *testing.T) {
		now := time.Date(2026, 11, 17, 13, 0, 0, 0, loc)
		names := []string{
			"X-2511011300.zip", // prior year Nov (monthly kept)
			"X-2511151300.zip", // prior year Nov (retire)
			"X-2610151300.zip", // Oct earliest (monthly kept)
			"X-2610312359.zip", // Oct end (retire)
			"X-2611010000.zip", // Nov earliest (monthly kept)
			"X-2611011200.zip", // Nov 1 second run (retire)
			"X-2611101300.zip", // exactly 7 days old -> not older than 7 days (kept)
			"X-2611171300.zip", // today (kept)
			"X-2602291300.zip", // invalid date -> ignored
			"foreign.txt",
		}
		sel := backupKeepSelect(names, "X", ".zip", 7, true, now, "X-2611171300.zip")
		wantRetire := []string{"X-2511151300.zip", "X-2610312359.zip", "X-2611011200.zip"}
		if !reflect.DeepEqual(sel.retire, wantRetire) {
			t.Errorf("retire = %q, want %q", sel.retire, wantRetire)
		}
		if sel.keptCount != 5 || sel.monthlyKept != 3 {
			t.Errorf("keptCount=%d monthlyKept=%d, want 5 and 3", sel.keptCount, sel.monthlyKept)
		}
	})

	t.Run("29 February leap year", func(t *testing.T) {
		now := time.Date(2024, 3, 15, 13, 0, 0, 0, loc)
		alone := backupKeepSelect([]string{"X-2402291300.zip", "X-2403151300.zip"}, "X", ".zip", 7, true, now, "")
		if len(alone.retire) != 0 || alone.keptCount != 2 || alone.monthlyKept != 1 {
			t.Errorf("29 Feb alone = %+v, want 0 retired, 2 kept, 1 monthly", alone)
		}
		withFirst := backupKeepSelect([]string{"X-2402011300.zip", "X-2402291300.zip", "X-2403151300.zip"}, "X", ".zip", 7, true, now, "")
		if !reflect.DeepEqual(withFirst.retire, []string{"X-2402291300.zip"}) || withFirst.keptCount != 2 || withFirst.monthlyKept != 1 {
			t.Errorf("29 Feb with Feb 1 = %+v", withFirst)
		}
	})

	t.Run("month with no backup", func(t *testing.T) {
		now := time.Date(2026, 11, 17, 13, 0, 0, 0, loc)
		names := []string{"X-2608051300.zip", "X-2610031300.zip", "X-2610201300.zip", "X-2611171300.zip"}
		sel := backupKeepSelect(names, "X", ".zip", 7, true, now, "")
		if !reflect.DeepEqual(sel.retire, []string{"X-2610201300.zip"}) || sel.keptCount != 3 || sel.monthlyKept != 2 {
			t.Errorf("gap month = %+v", sel)
		}
	})

	t.Run("current month first backup younger than a week", func(t *testing.T) {
		now := time.Date(2026, 11, 5, 13, 0, 0, 0, loc)
		names := []string{"X-2610201300.zip", "X-2611011300.zip", "X-2611031300.zip", "X-2611051300.zip"}
		sel := backupKeepSelect(names, "X", ".zip", 7, true, now, "X-2611051300.zip")
		if len(sel.retire) != 0 || sel.keptCount != 4 || sel.monthlyKept != 1 {
			t.Errorf("young current month = %+v, want 0 retired, 4 kept, 1 monthly", sel)
		}
	})

	t.Run("clock set back", func(t *testing.T) {
		now := time.Date(2026, 11, 12, 13, 0, 0, 0, loc)
		names := []string{
			"X-2611011300.zip", // earliest of Nov (monthly kept)
			"X-2611031300.zip", // older than Nov 5 -> retire
			"X-2611151300.zip", // future stamp when clock moved back -> kept
			"X-2611121300.zip", // current run -> kept
		}
		sel := backupKeepSelect(names, "X", ".zip", 7, true, now, "X-2611121300.zip")
		if !reflect.DeepEqual(sel.retire, []string{"X-2611031300.zip"}) || sel.keptCount != 3 || sel.monthlyKept != 1 {
			t.Errorf("clock set back = %+v", sel)
		}
	})

	t.Run("two runs in one minute protected", func(t *testing.T) {
		now := time.Date(2026, 11, 17, 13, 0, 0, 0, loc)
		sel := backupKeepSelect([]string{"X-2611011300.zip"}, "X", ".zip", 7, false, now, "X-2611011300.zip")
		if len(sel.retire) != 0 || sel.keptCount != 1 {
			t.Errorf("protected file retired: %+v", sel)
		}
	})

	t.Run("keep-days without keep-monthly retires monthly-first too", func(t *testing.T) {
		now := time.Date(2026, 11, 17, 13, 0, 0, 0, loc)
		names := []string{"X-2609011300.zip", "X-2610011300.zip", "X-2611011300.zip", "X-2611171300.zip"}
		sel := backupKeepSelect(names, "X", ".zip", 7, false, now, "X-2611171300.zip")
		want := []string{"X-2609011300.zip", "X-2610011300.zip", "X-2611011300.zip"}
		if !reflect.DeepEqual(sel.retire, want) || sel.keptCount != 1 || sel.monthlyKept != 0 {
			t.Errorf("without keep-monthly = %+v", sel)
		}
	})
}

func TestBackupKeepArgs(t *testing.T) {
	o, err := parseBackupArgs("backup", []string{`D:\X`, "to", `E:\Bak`, "stem", "X", "keep-days", "7", "keep-monthly", "sec", "pe:BAK_PW", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if o.keepDays != 7 || !o.keepMonthly || o.stem != "X" || !o.sec || !o.yes || o.cred.src != "pe" {
		t.Fatalf("parsed %+v", o)
	}
	if o, err := parseBackupArgs("backup", []string{`D:\X`, "keep-days", "999"}); err != nil || o.keepDays != 999 || o.keepMonthly {
		t.Fatalf("keep-days 999 alone: %+v %v", o, err)
	}
	refused := [][]string{
		{`D:\x`, "keep-days"},
		{`D:\x`, "keep-days", "0"},
		{`D:\x`, "keep-days", "1000"},
		{`D:\x`, "keep-days", "week"},
		{`D:\x`, "keep-monthly"}, // needs keep-days
		{`D:\x`, "gen", "7", "keep-days", "7"},
		{`D:\x`, "gen", "7", "keep-monthly"},
		{`D:\x`, "keep-days", "7", "sec", "keep-zip"},
		{`D:\x`, "keep-days", "7", "stem"},
		{`D:\x`, "keep-days", "7", "stem", "a/b"},
	}
	for _, args := range refused {
		_, err := parseBackupArgs("backup", args)
		if err == nil {
			t.Errorf("backup %q was accepted", args)
		} else if failureExitCode(err) != 10 {
			t.Errorf("backup %q ended with class %d, want 10", args, failureExitCode(err))
		}
	}
	for _, args := range [][]string{{"a.zip", "keep-days", "7"}, {"a.zip", "keep-monthly"}} {
		if _, err := parseBackupArgs("restore", args); err == nil {
			t.Errorf("restore %q was accepted", args)
		}
	}
}

func TestBackupKeepRedaction(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"backup", `D:\X`, "to", `E:\Bak`, "stem", "X", "keep-days", "7", "keep-monthly", "sec", "pe:PW", "y"},
			[]string{"backup", `D:\X`, "to", `E:\Bak`, "stem", "X", "keep-days", "7", "keep-monthly", "sec", "pe:PW", "y"}},
		{[]string{"backup", `D:\X`, "keep-days", "hunter2"}, []string{"backup", `D:\X`, "keep-days", "***"}},
		{[]string{"restore", `C:\b.fd-sec`, "keep-days", "hunter2"}, []string{"restore", `C:\b.fd-sec`, "***", "***"}},
	}
	for _, c := range cases {
		if got := redactCredentialArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("redact %q = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBackupKeepCommit_RetiresNothingWhenStopped(t *testing.T) {
	e := newGenEnv(t)
	old := filepath.Join(e.dest, "Photos-2501011300.zip")
	final := filepath.Join(e.dest, "Photos-2611171300.zip")
	partial := final + ".filedo-partial"
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := &backupKeep{days: 7, monthly: true, stem: "Photos", dir: e.dest, ext: ".zip", now: time.Now(), finalPath: final}
	globalInterruptHandler.Interrupt()
	err := backupKeepCommit(partial, k, func() {})
	if !errors.Is(err, errRunStopped) {
		t.Fatalf("stopped commit = %v, want errRunStopped", err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("partial left behind on stopped commit")
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Error("final left behind on stopped commit")
	}
	if b, err := os.ReadFile(old); err != nil || string(b) != "old" {
		t.Errorf("old file changed on stopped commit: %q %v", b, err)
	}
}

// TestBackupKeep_TempFolderRun exercises end-to-end backup with keep-days and
// keep-monthly in a temp folder, proving mtime is ignored, foreign files stay
// untouched, catch-up removes multiple expired non-monthly files, same-minute
// re-runs refuse cleanly, and sealed runs keep an independent set.
func TestBackupKeep_TempFolderRun(t *testing.T) {
	e := newGenEnv(t)
	now := time.Now()
	recent := now.AddDate(0, 0, -2)
	// Ensure m1/m2 are in prior calendar months strictly older than 7 days.
	m1First := time.Date(now.Year(), now.Month(), 1, 13, 0, 0, 0, time.Local).AddDate(0, -2, 0)
	m1Second := m1First.AddDate(0, 0, 14)
	m2First := time.Date(now.Year(), now.Month(), 1, 13, 0, 0, 0, time.Local).AddDate(0, -1, 0)
	m2Second := m2First.AddDate(0, 0, 10)
	// Make sure m2Second is also older than 7 days when now is early in the month.
	if !m2Second.Before(now.AddDate(0, 0, -8)) {
		m1First = m1First.AddDate(0, -1, 0)
		m1Second = m1Second.AddDate(0, -1, 0)
		m2First = m2First.AddDate(0, -1, 0)
		m2Second = m2Second.AddDate(0, -1, 0)
	}

	seed := map[string]struct {
		body  string
		mtime time.Time
	}{
		backupKeepName("X", m1First, ".zip"):     {"m1-first", now},              // mtime is NOW, name is old -> MUST STAY (monthly)
		backupKeepName("X", m1Second, ".zip"):    {"m1-second", now},             // mtime is NOW, name is old -> MUST RETIRE
		backupKeepName("X", m2First, ".zip"):     {"m2-first", now},              // mtime is NOW, name is old -> MUST STAY (monthly)
		backupKeepName("X", m2Second, ".zip"):    {"m2-second", now},             // mtime is NOW, name is old -> MUST RETIRE
		backupKeepName("X", recent, ".zip"):      {"recent", m1First},            // mtime is OLD, name is 2d ago -> MUST STAY (within 7d)
		"Other-2001011300.zip":                   {"other stem", m1First},        // foreign stem -> MUST STAY
		backupKeepName("X", m1Second, ".fd-sec"): {"sealed set", m1First},        // other extension -> MUST STAY
		"X-2602291300.zip":                       {"invalid leap date", m1First}, // invalid date -> MUST STAY
		"notes.txt":                              {"foreign", m1First},           // non-set -> MUST STAY
		"X-2501011300.zip.filedo-partial":        {"foreign partial", m1First},   // partial -> MUST STAY
	}
	for n, info := range seed {
		p := filepath.Join(e.dest, n)
		if err := os.WriteFile(p, []byte(info.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, info.mtime, info.mtime); err != nil {
			t.Fatal(err)
		}
	}

	run := func(more ...string) (map[string]interface{}, error) {
		args := append([]string{e.src, "to", e.dest, "stem", "X", "keep-days", "7", "keep-monthly", "level", "1"}, more...)
		o, err := parseBackupArgs("backup", args)
		if err != nil {
			t.Fatalf("parse %q: %v", args, err)
		}
		currentRun = &runOutcome{numbers: make(map[string]interface{})}
		err = runBackup(o, &HistoryLogger{})
		return currentRun.numbers, err
	}

	nums, err := run()
	if err != nil {
		t.Fatalf("plain run: %v", err)
	}
	for k, want := range map[string]int{"retiredCount": 2, "keptCount": 4, "keepDays": 7, "monthlyKept": 2} {
		if v, ok := nums[k]; !ok || v != want {
			t.Errorf("numbers[%q] = %v (ok=%v), want %d", k, v, ok, want)
		}
	}
	if _, ok := nums["archiveBytes"]; !ok {
		t.Error("missing archiveBytes")
	}

	// Same-minute re-run must refuse class 10 backup-destination-refused without retiring anything.
	if _, err := run(); err == nil {
		t.Error("same-minute second run succeeded; want destination refused")
	} else if failureExitCode(err) != 10 || reasonOf(err) != reasonBackupDestinationRefused {
		t.Errorf("same-minute run = (%d, %q), want (10, %q)", failureExitCode(err), reasonOf(err), reasonBackupDestinationRefused)
	}

	// Sealed run operates only on .fd-sec set and leaves .zip set intact.
	sNums, err := run("sec", "p:pw")
	if err != nil {
		t.Fatalf("sealed run: %v", err)
	}
	for k, want := range map[string]int{"retiredCount": 1, "keptCount": 1, "keepDays": 7, "monthlyKept": 0} {
		if v, ok := sNums[k]; !ok || v != want {
			t.Errorf("sealed numbers[%q] = %v (ok=%v), want %d", k, v, ok, want)
		}
	}

	var gotNames []string
	ents, err := os.ReadDir(e.dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range ents {
		gotNames = append(gotNames, ent.Name())
	}
	sort.Strings(gotNames)
	// Expect: 2 monthly zips + 1 recent zip + 1 new zip + 1 new .fd-sec + 4 foreign files.
	if len(gotNames) != 9 {
		t.Errorf("destination has %d files %q, want 9", len(gotNames), gotNames)
	}
	if _, err := os.Stat(filepath.Join(e.dest, backupKeepName("X", m1Second, ".zip"))); !os.IsNotExist(err) {
		t.Error("m1Second zip should have been retired")
	}
	if _, err := os.Stat(filepath.Join(e.dest, backupKeepName("X", m2Second, ".zip"))); !os.IsNotExist(err) {
		t.Error("m2Second zip should have been retired")
	}
	if _, err := os.Stat(filepath.Join(e.dest, backupKeepName("X", m1Second, ".fd-sec"))); !os.IsNotExist(err) {
		t.Error("m1Second .fd-sec should have been retired by sealed run")
	}
}
