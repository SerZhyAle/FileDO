package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// The SP-0024 proofs. Each test names its ticket.

// memDevice is a fake block device: a map of physical sectors behind an
// address mapping, with optional faults.
type memDevice struct {
	sector      int
	mem         map[int64][]byte
	mapAddr     func(int64) int64 // logical -> physical
	dropAbove   int64             // writes at or above this are lost (0 = none)
	readErrAt   map[int64]bool    // reads fail at these logical offsets
	origErrOnce map[int64]bool    // the first read at these offsets fails
	failRestore bool              // writes after the verification reads fail
	reads       int
	writes      []int64
	verifying   bool
}

func newMemDevice(sector int) *memDevice {
	return &memDevice{sector: sector, mem: map[int64][]byte{}, mapAddr: func(o int64) int64 { return o }}
}

func (d *memDevice) content(off int64) []byte {
	if b, ok := d.mem[off]; ok {
		return b
	}
	// A sector nobody wrote holds its own "original" pattern.
	b := make([]byte, d.sector)
	copy(b, []byte("ORIGINAL@"))
	b[9] = byte(off >> 9)
	b[10] = byte(off >> 17)
	return b
}

func (d *memDevice) ReadAt(p []byte, off int64) error {
	d.reads++
	if d.origErrOnce[off] {
		delete(d.origErrOnce, off)
		return errors.New("unreadable sector")
	}
	if d.readErrAt[off] && d.verifying {
		return errors.New("read error")
	}
	copy(p, d.content(d.mapAddr(off)))
	return nil
}

func (d *memDevice) WriteAt(p []byte, off int64) error {
	d.writes = append(d.writes, off)
	if d.failRestore && d.verifying {
		return errors.New("restore write failed")
	}
	if d.dropAbove > 0 && off >= d.dropAbove {
		return nil
	}
	d.mem[d.mapAddr(off)] = append([]byte(nil), p...)
	return nil
}

const gib = int64(1) << 30

// verifyingDevice flips the device into its verification phase after the
// markers are written, so faults can target the read-back or the restore.
type phasedDevice struct {
	*memDevice
	writesBeforeVerify int
}

func (d *phasedDevice) ReadAt(p []byte, off int64) error {
	if len(d.writes) >= d.writesBeforeVerify {
		d.verifying = true
	}
	return d.memDevice.ReadAt(p, off)
}

func runProbe(t *testing.T, dev sectorDevice, total int64) (probeResult, error) {
	t.Helper()
	res, err := probeCore(dev, total, 512, nil, nil, nil)
	if err != nil {
		return res, err
	}
	return res, probeVerdict(res)
}

// TestProbeFakeIsDefect is CLI-01: a device that loses writes above its real
// capacity is a defect, never a pass.
func TestProbeFakeIsDefect(t *testing.T) {
	d := newMemDevice(512)
	d.dropAbove = 4 * gib
	res, err := runProbe(t, d, 64*gib)
	if !errors.Is(err, errDefect) {
		t.Fatalf("a write-dropping fake: verdict %v (res %+v), want a defect", err, res)
	}
}

// TestProbeWrapAroundIsDefect is CLI-08: a device mapping a -> a mod R, with
// R an eighth of the claimed size, is caught.
func TestProbeWrapAroundIsDefect(t *testing.T) {
	d := newMemDevice(512)
	r := 8 * gib
	d.mapAddr = func(o int64) int64 { return o % r }
	res, err := runProbe(t, d, 64*gib)
	if !errors.Is(err, errDefect) || res.aliases == 0 {
		t.Fatalf("a wrap-around fake: verdict %v, aliases %d, want a defect", err, res.aliases)
	}
}

// TestProbeGenuinePassesAndRestores: an honest device passes and every sector
// is back to its original.
func TestProbeGenuinePassesAndRestores(t *testing.T) {
	d := newMemDevice(512)
	res, err := runProbe(t, d, 64*gib)
	if err != nil {
		t.Fatalf("a genuine device: %v", err)
	}
	for _, off := range res.offsets {
		if !bytes.HasPrefix(d.content(off), []byte("ORIGINAL@")) {
			t.Fatalf("sector at %d was not restored", off)
		}
	}
	if res.written < 40 {
		t.Fatalf("only %d markers written; the plan must hold the anchor family too", res.written)
	}
}

// TestProbeReadErrorIsNotProven is CLI-01's other half: a read that fails
// during verification is "could not verify", never a defect and never a pass.
func TestProbeReadErrorIsNotProven(t *testing.T) {
	d := newMemDevice(512)
	plan := probePlan(64*gib, 512)
	d.readErrAt = map[int64]bool{plan[5]: true}
	pd := &phasedDevice{memDevice: d, writesBeforeVerify: len(plan)}
	res, err := runProbe(t, pd, 64*gib)
	if err == nil || errors.Is(err, errDefect) {
		t.Fatalf("a verification read error: verdict %v (res %+v), want not proven", err, res)
	}
}

// TestProbeUnreadableOriginalNeverWritten is CLI-07a.
func TestProbeUnreadableOriginalNeverWritten(t *testing.T) {
	d := newMemDevice(512)
	plan := probePlan(64*gib, 512)
	bad := plan[3]
	d.origErrOnce = map[int64]bool{bad: true}
	res, _ := runProbe(t, d, 64*gib)
	for _, w := range d.writes {
		if w == bad {
			t.Fatal("a sector whose original could not be read was written")
		}
	}
	if res.unreadable != 1 {
		t.Fatalf("unreadable = %d, want 1", res.unreadable)
	}
}

// TestProbeRestoreErrorIsReported is CLI-07b.
func TestProbeRestoreErrorIsReported(t *testing.T) {
	d := newMemDevice(512)
	plan := probePlan(64*gib, 512)
	d.failRestore = true
	pd := &phasedDevice{memDevice: d, writesBeforeVerify: len(plan)}
	res, err := runProbe(t, pd, 64*gib)
	if len(res.restoreErrs) == 0 || err == nil {
		t.Fatalf("a failed restore must be returned: restoreErrs %d, verdict %v", len(res.restoreErrs), err)
	}
}

// TestProbeStopAndForcedExitStillRestore is CLI-07c: a stop mid-probe ends it
// with everything restored, and the restore handed to the force-exit cleanup
// is the same idempotent one.
func TestProbeStopAndForcedExitStillRestore(t *testing.T) {
	d := newMemDevice(512)
	var restore func()
	writes := 0
	stop := func() bool { writes++; return writes > 5 }
	res, err := probeCore(d, 64*gib, 512, nil, func(r func()) func() { restore = r; return func() {} }, stop)
	if !errors.Is(err, errRunStopped) {
		t.Fatalf("a stopped probe: %v", err)
	}
	if restore == nil {
		t.Fatal("the restore was never offered to the force-exit cleanup")
	}
	restore() // a second call is a no-op
	for _, off := range res.offsets {
		if !bytes.HasPrefix(d.content(off), []byte("ORIGINAL@")) {
			t.Fatalf("sector at %d was left with a marker after a stop", off)
		}
	}
}

// TestProbeDriveLetterParse is CLI-27.
func TestProbeDriveLetterParse(t *testing.T) {
	for _, ok := range []string{"D", "d:", `D:\`, "D:/"} {
		if l, err := probeExtractDriveLetter(ok); err != nil || l != 'D' {
			t.Errorf("%q: %c %v, want D", ok, l, err)
		}
	}
	for _, bad := range []string{`\\.\PhysicalDrive1`, `\\?\Volume{1234}\`, "Data", `D:\x`, "", "я"} {
		if _, err := probeExtractDriveLetter(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// TestEveryAliasDispatches is CLI-30: every alias of the vocabulary reaches a
// verb through the one dispatch - interactive and batch alike, since both call
// dispatchLine - except the wipe words, which take their target first.
func TestEveryAliasDispatches(t *testing.T) {
	for _, a := range list_of_flags_for_all {
		v := verbOf(a)
		if contains(list_of_flags_for_wipe, a) {
			if v != "" {
				t.Errorf("%q must not be a verb-first word (wipe takes its target first), got %q", a, v)
			}
			continue
		}
		if v == "" {
			t.Errorf("alias %q reaches no verb", a)
		}
		if up := verbOf(strings.ToUpper(a)); up != v {
			t.Errorf("alias %q is case-sensitive: %q vs %q", a, v, up)
		}
	}
}

// TestTargetClassification is CLI-17, CLI-19 and CLI-22.
func TestTargetClassification(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	cases := []struct{ in, kind string }{
		{`\\server\share`, "network"},
		{`//server/share`, "network"},
		{`\\?\UNC\server\share`, "network"},
		{`\\?\C:\`, "device"},
		{`\\.\C:`, "device"},
		{"C:", "device"},
		{`C:\`, "device"},
		{"d", "device"},
		{".", "folder"},
		{"sub", "folder"},
		{"x.txt", "file"},
		{filepath.Join(dir, "sub"), "folder"},
	}
	for _, c := range cases {
		kind, _, err := classifyTarget(c.in)
		if err != nil || kind != c.kind {
			t.Errorf("classifyTarget(%q) = %q, %v; want %q", c.in, kind, err, c.kind)
		}
	}
	for _, bad := range []string{"я", "é", `\\.\PhysicalDrive0`, "missing.txt", "nosuchword"} {
		if _, _, err := classifyTarget(bad); err == nil {
			t.Errorf("classifyTarget(%q) must fail", bad)
		}
	}
}

func writeLst(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBatchHonoursStopFile is CLI-04.
func TestBatchHonoursStopFile(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for _, n := range []string{"a", "b", "c", "d"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte("content "+n), 0o644)
		body.WriteString(n + ".txt secure p:pw\n")
	}
	writeLst(t, dir, "work.lst", body.String())
	stop := filepath.Join(dir, "run.stop")
	os.WriteFile(stop, nil, 0o644)
	out, _ := run(t, dir, "--stop-file", stop, "from", "work.lst")
	if m, _ := filepath.Glob(filepath.Join(dir, "*.fd-sec")); len(m) > 0 {
		t.Fatalf("batch lines ran after a stop: %v\n%s", m, out)
	}
	if !strings.Contains(out, "were not run") {
		t.Errorf("the skipped lines are not reported\n%s", out)
	}
}

// TestFdsecVerifyWrongPasswordFromBatch is CLI-10: a batch line ends with the
// same class as the command typed.
func TestFdsecVerifyWrongPasswordFromBatch(t *testing.T) {
	dir, _ := workdir(t)
	if out, code := run(t, dir, "plain.txt", "secure", "p:right"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	writeLst(t, dir, "v.lst", "fdsec verify plain.fd-sec p:wrong\n")
	out, code := run(t, dir, "from", "v.lst")
	if code != fdsecExitCredentialOrTamper {
		t.Fatalf("batch verify with a wrong password exited %d, want %d\n%s", code, fdsecExitCredentialOrTamper, out)
	}
}

// TestBatchLinesBeginARun is CLI-11: verb-first lines and `from` itself open
// the run, so a batch produces a stream with a result.
func TestBatchLinesBeginARun(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "A")
	b := filepath.Join(dir, "B")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	os.WriteFile(filepath.Join(a, "f.txt"), []byte("same"), 0o644)
	os.WriteFile(filepath.Join(b, "f.txt"), []byte("same"), 0o644)
	writeLst(t, dir, "c.lst", "compare A B\n")
	events := filepath.Join(dir, "ev.jsonl")
	out, code := run(t, dir, "--events", events, "from", "c.lst")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	evs := readEvents(t, events)
	if len(evs) < 2 || evs[0]["kind"] != "run" {
		t.Fatalf("a batch must open with a run event: %v", evs)
	}
	if v := lastResult(t, events)["verdict"]; v != "Done" {
		t.Fatalf("verdict %v, want Done", v)
	}
}

// TestBatchCountsTargetFailures is CLI-12.
func TestBatchCountsTargetFailures(t *testing.T) {
	dir := t.TempDir()
	writeLst(t, dir, "m.lst", "folder "+filepath.Join(dir, "missing")+" info\n")
	out, code := run(t, dir, "from", "m.lst")
	if !strings.Contains(out, "0/1 commands succeeded") {
		t.Fatalf("a failed target counted as a success\n%s", out)
	}
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// TestFdsecMaskFromBatchFile and TestBatchUnknownWordIsUsageError are CLI-13.
func TestFdsecMaskFromBatchFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	writeLst(t, dir, "m.lst", "*.txt secure p:k\n")
	out, code := run(t, dir, "from", "m.lst")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "a.fd-sec")) || !exists(filepath.Join(dir, "b.fd-sec")) {
		t.Fatalf("the mask line did not pack both files\n%s", out)
	}
}

func TestBatchUnknownWordIsUsageError(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.txt")
	writeLst(t, dir, "x.lst", "cmd /c echo ran > "+marker+"\n")
	out, code := run(t, dir, "from", "x.lst")
	if exists(marker) {
		t.Fatalf("a batch line started an external program\n%s", out)
	}
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
}

// TestBatchBOMAndQuotes is CLI-14.
func TestBatchBOMAndQuotes(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "with space")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "doc.txt"), []byte("quoted"), 0o644)
	// UTF-8 BOM, a comment first, a program prefix, a quoted path and a
	// quoted password.
	body := "\ufeff# comment\n.\\FileDO.EXE \"" + filepath.Join(sub, "doc.txt") + "\" secure p:\"hunter2\"\n"
	writeLst(t, dir, "q.lst", body)
	out, code := run(t, dir, "from", "q.lst")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	sub2 := t.TempDir()
	if out, code := run(t, dir, filepath.Join(sub, "doc.fd-sec"), "unsecure", "to", sub2, "p:hunter2"); code != 0 {
		t.Fatalf("the quotes were sealed into the password: unsecure with hunter2 exited %d\n%s", code, out)
	}

	// UTF-16 LE with a BOM, as PowerShell 5.1's Out-File writes it.
	os.WriteFile(filepath.Join(dir, "u.txt"), []byte("u"), 0o644)
	u := utf16.Encode([]rune("u.txt secure p:pw\r\n"))
	raw := []byte{0xFF, 0xFE}
	for _, c := range u {
		raw = append(raw, byte(c), byte(c>>8))
	}
	os.WriteFile(filepath.Join(dir, "u16.lst"), raw, 0o644)
	if out, code := run(t, dir, "from", "u16.lst"); code != 0 || !exists(filepath.Join(dir, "u.fd-sec")) {
		t.Fatalf("a UTF-16 batch file did not run: exit %d\n%s", code, out)
	}
}

func TestSplitBatchLine(t *testing.T) {
	cases := map[string][]string{
		`a b  c`:                  {"a", "b", "c"},
		`"a b" c`:                 {"a b", "c"},
		`p:"hunter2"`:             {"p:hunter2"},
		`"C:\dir with\\" x`:       {`C:\dir with\`, "x"},
		`a\\\"b`:                  {`a\"b`},
		`"say ""hi"""`:            {`say "hi"`},
		`C:\path\to\file.txt`:     {`C:\path\to\file.txt`},
		"\t tab\tsep ":            {"tab", "sep"},
		`"" empty`:                {"", "empty"},
		`D:\Photos (2019) secure`: {`D:\Photos`, "(2019)", "secure"},
	}
	for in, want := range cases {
		got := splitBatchLine(in)
		if strings.Join(got, "|") != strings.Join(want, "|") || len(got) != len(want) {
			t.Errorf("splitBatchLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBatchSelfIncludeRefused is CLI-15.
func TestBatchSelfIncludeRefused(t *testing.T) {
	dir := t.TempDir()
	writeLst(t, dir, "self.lst", "from self.lst\n")
	out, code := run(t, dir, "from", "self.lst")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
	if strings.Count(out, "Executing") > 2 {
		t.Fatalf("the self-include recursed\n%s", out)
	}
}

// TestFolderNamedDash is CLI-23: a folder named -old is a target, and the run
// still ends with a result.
func TestFolderNamedDash(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "-old"), 0o755)
	events := filepath.Join(dir, "ev.jsonl")
	out, code := run(t, dir, "--events", events, "-old", "info")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if v := lastResult(t, events)["verdict"]; v != "Done" {
		t.Fatalf("verdict %v\n%s", v, out)
	}
}

// TestEmitResultWithInfStillWritesResult is CLI-24.
func TestEmitResultWithInfStillWritesResult(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ev.jsonl")
	em, err := InitEventManager(p)
	if err != nil {
		t.Fatal(err)
	}
	saved := globalEventManager
	globalEventManager = em
	defer func() { globalEventManager = saved }()
	EmitResultEvent("Failed", map[string]interface{}{"speedMBps": math.Inf(1), "ratio": math.NaN(), "ok": 3.5}, nil, nil)
	em.Close()
	raw, _ := os.ReadFile(p)
	var ev map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &ev); err != nil {
		t.Fatalf("no parseable result line: %v\n%s", err, raw)
	}
	data := ev["data"].(map[string]interface{})
	if data["verdict"] != "Failed" {
		t.Fatalf("result lost its verdict: %s", raw)
	}
	nums := data["numbers"].(map[string]interface{})
	if nums["speedMBps"] != nil || nums["ratio"] != nil || nums["ok"] != 3.5 {
		t.Fatalf("non-finite numbers must become null and finite ones stay: %v", nums)
	}
}

// TestFindUIExecutableIgnoresCwd is CLI-26.
func TestFindUIExecutableIgnoresCwd(t *testing.T) {
	dir := t.TempDir()
	planted := filepath.Join(dir, "filedo_win.exe")
	os.WriteFile(planted, []byte("MZ planted"), 0o644)
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	got := findUIExecutable()
	if got == "filedo_win.exe" || strings.EqualFold(got, planted) {
		t.Fatalf("findUIExecutable returned the file planted in the current directory: %q", got)
	}
}

// TestSecureRenameHistoryHidesNames is CLI-21.
func TestSecureRenameHistoryHidesNames(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "salary-2026.xlsx"), []byte("numbers"), 0o644)
	if out, code := run(t, dir, "salary-2026.xlsx", "secure", "rename", "p:pw"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	hist := string(mustRead(t, filepath.Join(dir, "history.json")))
	if strings.Contains(hist, "salary-2026") {
		t.Fatalf("history links the blob to the original's name:\n%s", hist)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if len(n) == 24 && !strings.Contains(n, ".") && strings.Contains(hist, n) {
			t.Fatalf("history names the anonymous blob %s", n)
		}
	}
}
