package main

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The SP-0039 (AUD-08) fixes at the check command surface. Each test names
// its finding.

// TestCheckJunctionRootIsRead is AUD-08-F3: a junction (or a volume mounted
// in a folder) named as the root is walked as the folder it names. The walk
// used to Lstat the root, see a reparse point that is not a directory and end
// "there is no non-empty file to check here", exit 2.
func TestCheckJunctionRootIsRead(t *testing.T) {
	root := checkTree(t, 3)
	j := filepath.Join(t.TempDir(), "jdata")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, root).CombinedOutput(); err != nil {
		t.Skipf("mklink /J is unavailable (%v: %s)", err, out)
	}
	wd := t.TempDir()
	for _, extra := range [][]string{{"--no-precount"}, {"--precount"}} {
		args := append([]string{"check", j}, extra...)
		out, code := run(t, wd, args...)
		if code != 0 || !strings.Contains(out, "found=3, checked=3") {
			t.Errorf("check <junction> %v: want found=3, checked=3, exit 0; got exit %d\n%s", extra, code, out)
		}
	}
	// The lists keep the spelling the user gave: the good list names the
	// files under the junction, not under its target.
	b, err := os.ReadFile(filepath.Join(wd, checkGoodListName))
	if err != nil {
		t.Fatalf("no good list after a clean sweep: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(b)), strings.ToLower(j)+`\`) {
		t.Errorf("the good list does not name the files through the junction %s\n%s", j, b)
	}
}

// TestCheckReportPathsRoundTrip is AUD-08-F8: the CSV and JSON reports name
// the files they list. Go's %q doubled every backslash in the CSV, and wrote a
// DEL (0x7F) in a name as \x7f, which is not JSON.
func TestCheckReportPathsRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	names := []string{
		filepath.Join(root, "sub", "plain.bin"),
		filepath.Join(root, "sub", "comma, name.bin"),
		filepath.Join(root, "del\x7fname.bin"),
	}
	for i, p := range names {
		writeFile(t, p, patternBytes(512, byte(i+1)))
	}
	sort.Strings(names)
	wd := t.TempDir()

	csvFile := filepath.Join(wd, "report.csv")
	out, code := run(t, wd, "check", root, "--no-precount", "--report", "csv", "--report-file", csvFile)
	if code != 0 {
		t.Fatalf("check --report csv exited %d\n%s", code, out)
	}
	f, err := os.Open(csvFile)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		t.Fatalf("the CSV report does not parse: %v", err)
	}
	if len(rows) == 0 || strings.Join(rows[0], ",") != "path,size,first_read_ms,status" {
		t.Fatalf("the CSV header changed: %v", rows)
	}
	var got []string
	for _, r := range rows[1:] {
		if len(r) != 4 || r[1] != "512" || r[3] != "ok" {
			t.Errorf("unexpected CSV row %q", r)
		}
		got = append(got, r[0])
	}
	sort.Strings(got)
	if strings.Join(got, "|") != strings.Join(names, "|") {
		t.Errorf("the CSV report does not name the files it lists\n got: %q\nwant: %q", got, names)
	}

	jsonFile := filepath.Join(wd, "report.json")
	out, code = run(t, wd, "check", root, "--no-precount", "--report", "json", "--report-file", jsonFile)
	if code != 0 {
		t.Fatalf("check --report json exited %d\n%s", code, out)
	}
	b, err := os.ReadFile(jsonFile)
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		Path        string   `json:"path"`
		Size        *int64   `json:"size"`
		FirstReadMs *float64 `json:"first_read_ms"`
		Status      string   `json:"status"`
	}
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatalf("the JSON report does not parse: %v\n%s", err, b)
	}
	got = got[:0]
	for _, it := range items {
		if it.Size == nil || *it.Size != 512 || it.FirstReadMs == nil || it.Status != "ok" {
			t.Errorf("unexpected JSON row %+v", it)
		}
		got = append(got, it.Path)
	}
	sort.Strings(got)
	if strings.Join(got, "|") != strings.Join(names, "|") {
		t.Errorf("the JSON report does not name the files it lists\n got: %q\nwant: %q", got, names)
	}
}
