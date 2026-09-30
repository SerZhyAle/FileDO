package main

// AUD-07-F1 (with SP-0038 F5 and AUD-01-F3): links and copy. A source folder
// that is itself a junction is refused as a usage error before anything is
// created at the target; a junction inside the tree is not followed, and the
// run that left it behind ends exit 2 with the entry named - on both engines.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeJunction links link to target with `mklink /J` (no elevation needed),
// or skips the test where junctions cannot be made.
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J unavailable (%v: %s)", err, out)
	}
}

// findingMessages returns the message of every `finding` event in the file.
func findingMessages(t *testing.T, events string) []string {
	t.Helper()
	var msgs []string
	for _, ev := range readEvents(t, events) {
		if ev["kind"] != "finding" {
			continue
		}
		if data, ok := ev["data"].(map[string]interface{}); ok {
			if m, ok := data["message"].(string); ok {
				msgs = append(msgs, m)
			}
		}
	}
	return msgs
}

func TestCopyJunctionSourceRootRefused(t *testing.T) {
	wd := t.TempDir()
	real := filepath.Join(wd, "RealData")
	writeFile(t, filepath.Join(real, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(real, "sub", "b.txt"), []byte("b"))
	link := filepath.Join(wd, "ext")
	makeJunction(t, link, real)

	rows := []struct {
		name string
		args func(src, dst string) []string
	}{
		{"folder copy", func(src, dst string) []string { return []string{"folder", src, "copy", dst} }},
		{"folder copy trailing separator", func(src, dst string) []string { return []string{"folder", src + `\`, "copy", dst} }},
		{"copy", func(src, dst string) []string { return []string{"copy", src, dst} }},
		{"fastcopy", func(src, dst string) []string { return []string{"fastcopy", src, dst} }},
		{"safecopy", func(src, dst string) []string { return []string{"safecopy", src, dst} }},
		{"smartcopy", func(src, dst string) []string { return []string{"smartcopy", src, dst} }},
	}
	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			dst := filepath.Join(wd, "dst"+string(rune('a'+i)))
			events := filepath.Join(t.TempDir(), "events.jsonl")
			out, code := run(t, wd, append([]string{"--events", events}, row.args(link, dst)...)...)
			if code != 2 {
				t.Errorf("exit %d, want 2\n%s", code, out)
			}
			if exists(dst) {
				t.Errorf("the target %s was created", dst)
			}
			if !strings.Contains(out, link) || !strings.Contains(out, "links are not followed") {
				t.Errorf("the refusal does not name the link %s:\n%s", link, out)
			}
			if !strings.Contains(strings.ToLower(out), strings.ToLower(real)) {
				t.Errorf("the refusal does not name where the link leads (%s):\n%s", real, out)
			}
			if got := lastResult(t, events)["verdict"]; got != "Not proven" {
				t.Errorf("verdict %q, want Not proven", got)
			}
		})
	}
	if !exists(filepath.Join(real, "a.txt")) || !exists(filepath.Join(real, "sub", "b.txt")) {
		t.Fatalf("the folder behind the junction was changed")
	}

	// The folder itself, given directly, still copies.
	dst := filepath.Join(wd, "direct")
	if out, code := run(t, wd, "folder", real, "copy", dst); code != 0 || !exists(filepath.Join(dst, "sub", "b.txt")) {
		t.Errorf("copying the real folder exited %d, sub\\b.txt at the target=%v\n%s", code, exists(filepath.Join(dst, "sub", "b.txt")), out)
	}
}

func TestCopyTreeWithJunctionIsIncomplete(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	writeFile(t, filepath.Join(src, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(src, "sub", "b.txt"), []byte("b"))
	outside := filepath.Join(wd, "outside")
	writeFile(t, filepath.Join(outside, "c.txt"), []byte("c"))
	link := filepath.Join(src, "ext")
	makeJunction(t, link, outside)

	rows := []struct {
		name string
		args func(dst string) []string
	}{
		{"folder copy", func(dst string) []string { return []string{"folder", src, "copy", dst} }},
		{"fastcopy", func(dst string) []string { return []string{"fastcopy", src, dst} }},
		{"synccopy", func(dst string) []string { return []string{"synccopy", src, dst} }},
		{"safecopy", func(dst string) []string { return []string{"safecopy", src, dst} }},
	}
	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			dst := filepath.Join(wd, "dst"+string(rune('a'+i)))
			events := filepath.Join(t.TempDir(), "events.jsonl")
			out, code := run(t, wd, append([]string{"--events", events}, row.args(dst)...)...)
			if code != 2 {
				t.Errorf("exit %d, want 2\n%s", code, out)
			}
			for _, rel := range []string{"a.txt", filepath.Join("sub", "b.txt")} {
				if !exists(filepath.Join(dst, rel)) {
					t.Errorf("%s was not copied\n%s", rel, out)
				}
			}
			if exists(filepath.Join(dst, "ext", "c.txt")) {
				t.Errorf("the junction was followed: ext\\c.txt is at the target")
			}
			if !strings.Contains(out, "SKIPPED: "+link+" is not a regular file") {
				t.Errorf("the skipped junction %s is not named:\n%s", link, out)
			}
			if !strings.Contains(out, "1 not regular files (links are not followed)") {
				t.Errorf("the summary does not count the skipped entry:\n%s", out)
			}
			if got := lastResult(t, events)["verdict"]; got != "Not proven" {
				t.Errorf("verdict %q, want Not proven", got)
			}
			found := false
			for _, m := range findingMessages(t, events) {
				if strings.Contains(m, "incomplete") && strings.Contains(m, "not regular files") {
					found = true
				}
			}
			if !found {
				t.Errorf("no finding says the copy is incomplete: %q", findingMessages(t, events))
			}
		})
	}
	if _, err := os.Lstat(link); err != nil || !hasReparsePoint(link) {
		t.Fatalf("the junction is gone or no longer a link: %v", err)
	}
}
