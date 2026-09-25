package main

// launcher_test.go is identical in cmd/filedo-check, cmd/filedo-fill and
// cmd/filedo-test, like launcher.go. The module-specific inputs - sampleArgs,
// sampleWant and badArgs - live in each module's main_test.go.
//
// The tests build the real launcher and a stub filedo.exe (a tiny Go program
// that records its argv and stdin and exits with a code taken from the
// environment), lay them out the way every channel ships them - side by side -
// and run the launcher as a user would.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// testVersion is stamped into the launcher the way build.ps1 and release.yml
// stamp a release, so the -? smoke check can be tested for real.
const testVersion = "2609259999"

const stubGoMod = "module filedostub\n\ngo 1.21\n"

const stubSource = `package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/signal"
	"strconv"
	"time"
)

type record struct {
	Args        []string ` + "`json:\"args\"`" + `
	Stdin       string   ` + "`json:\"stdin\"`" + `
	Interrupted bool     ` + "`json:\"interrupted\"`" + `
}

func main() {
	rec := record{Args: os.Args[1:]}
	if os.Getenv("FILEDO_STUB_READ_STDIN") == "1" {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		rec.Stdin = line
	}
	if ready := os.Getenv("FILEDO_STUB_WAIT_INTERRUPT"); ready != "" {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		_ = os.WriteFile(ready, []byte("ready"), 0o644)
		select {
		case <-ch:
			rec.Interrupted = true
			// Still working after the interrupt: a launcher that died on
			// the same Ctrl+C would be gone before this process ends.
			time.Sleep(300 * time.Millisecond)
		case <-time.After(30 * time.Second):
		}
	}
	if path := os.Getenv("FILEDO_STUB_RECORD"); path != "" {
		data, _ := json.Marshal(rec)
		_ = os.WriteFile(path, data, 0o644)
	}
	code, _ := strconv.Atoi(os.Getenv("FILEDO_STUB_EXIT"))
	os.Exit(code)
}
`

type stubRecord struct {
	Args        []string `json:"args"`
	Stdin       string   `json:"stdin"`
	Interrupted bool     `json:"interrupted"`
}

var (
	buildOnce   sync.Once
	buildRoot   string
	buildErr    error
	launcherBin string
	stubBin     string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildRoot != "" {
		os.RemoveAll(buildRoot)
	}
	os.Exit(code)
}

// binaries builds the launcher and the stub once per test run.
func binaries(t *testing.T) (launcher, stub string) {
	t.Helper()
	buildOnce.Do(func() {
		buildRoot, buildErr = os.MkdirTemp("", "filedo-companion-test-")
		if buildErr != nil {
			return
		}
		stubDir := filepath.Join(buildRoot, "stubsrc")
		if buildErr = os.MkdirAll(stubDir, 0o755); buildErr != nil {
			return
		}
		if buildErr = os.WriteFile(filepath.Join(stubDir, "go.mod"), []byte(stubGoMod), 0o644); buildErr != nil {
			return
		}
		if buildErr = os.WriteFile(filepath.Join(stubDir, "main.go"), []byte(stubSource), 0o644); buildErr != nil {
			return
		}
		stubBin = filepath.Join(buildRoot, "stub.exe")
		if buildErr = goBuild(stubDir, stubBin); buildErr != nil {
			return
		}
		launcherBin = filepath.Join(buildRoot, selfName)
		buildErr = goBuild(".", launcherBin, "-ldflags=-X main.version="+testVersion)
	})
	if buildErr != nil {
		t.Fatalf("building the test binaries: %v", buildErr)
	}
	return launcherBin, stubBin
}

func goBuild(dir, out string, flags ...string) error {
	args := append([]string{"build"}, flags...)
	args = append(args, "-o", out, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, output)
	}
	return nil
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// install lays the launcher out in a fresh folder, with the stub beside it as
// filedo.exe when withMain is set - the shape the zip, the MSI and winget ship.
func install(t *testing.T, withMain bool) (dir, launcher string) {
	t.Helper()
	l, s := binaries(t)
	dir = t.TempDir()
	launcher = filepath.Join(dir, selfName)
	copyFile(t, l, launcher)
	if withMain {
		copyFile(t, s, filepath.Join(dir, mainExeName))
	}
	return dir, launcher
}

type runResult struct {
	code   int
	stdout string
	stderr string
	record *stubRecord // nil when the stub did not run
}

type runOptions struct {
	env   []string
	stdin string
	dir   string
}

func runLauncher(t *testing.T, launcher string, args []string, opt runOptions) runResult {
	t.Helper()
	recordPath := filepath.Join(t.TempDir(), "record.json")
	cmd := exec.Command(launcher, args...)
	cmd.Env = append(append(os.Environ(), "FILEDO_STUB_RECORD="+recordPath), opt.env...)
	cmd.Dir = opt.dir
	if opt.stdin != "" {
		cmd.Stdin = strings.NewReader(opt.stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	res := runResult{}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %s: %v", launcher, err)
		}
		res.code = exitErr.ExitCode()
	}
	res.stdout, res.stderr = stdout.String(), stderr.String()
	if data, err := os.ReadFile(recordPath); err == nil {
		var rec stubRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatalf("stub record %s: %v", data, err)
		}
		res.record = &rec
	}
	return res
}

func TestHelpPrintsTheStampAndExitsZero(t *testing.T) {
	_, launcher := install(t, true)
	for _, args := range [][]string{{"-?"}, {"/?"}, {"--help"}, {"help"}, {"?"}, {"D:", "--help"}} {
		res := runLauncher(t, launcher, args, runOptions{})
		if res.code != 0 {
			t.Errorf("%v: exit %d, want 0\n%s%s", args, res.code, res.stdout, res.stderr)
		}
		if !strings.Contains(res.stdout, testVersion) {
			t.Errorf("%v: the usage text does not carry the stamp %s:\n%s", args, testVersion, res.stdout)
		}
		if res.record != nil {
			t.Errorf("%v: filedo.exe ran for a help request: %v", args, res.record.Args)
		}
	}
}

func TestNoArgumentsShowsUsageAndExitsTwo(t *testing.T) {
	_, launcher := install(t, true)
	res := runLauncher(t, launcher, nil, runOptions{})
	if res.code != exitCouldNotVerify {
		t.Errorf("exit %d, want %d", res.code, exitCouldNotVerify)
	}
	if !strings.Contains(res.stdout, "USAGE") {
		t.Errorf("no usage text:\n%s", res.stdout)
	}
	if res.record != nil {
		t.Errorf("filedo.exe ran with no arguments given: %v", res.record.Args)
	}
}

func TestForwardsArgvAndExitCode(t *testing.T) {
	_, launcher := install(t, true)
	for _, code := range []int{0, 1, 2, 5} {
		res := runLauncher(t, launcher, sampleArgs, runOptions{env: []string{fmt.Sprintf("FILEDO_STUB_EXIT=%d", code)}})
		if res.code != code {
			t.Errorf("filedo.exe exited %d, the launcher %d\n%s", code, res.code, res.stderr)
		}
		if res.record == nil {
			t.Fatalf("filedo.exe did not run\n%s", res.stderr)
		}
		if !reflect.DeepEqual(res.record.Args, sampleWant) {
			t.Errorf("filedo.exe got %q, want %q", res.record.Args, sampleWant)
		}
	}
}

func TestInheritsStdin(t *testing.T) {
	// filedo.exe's prompts (the system-drive redirect, a confirmation) read
	// the console; the answer has to reach it through the launcher.
	_, launcher := install(t, true)
	res := runLauncher(t, launcher, sampleArgs, runOptions{env: []string{"FILEDO_STUB_READ_STDIN=1"}, stdin: "y\r\n"})
	if res.code != 0 || res.record == nil {
		t.Fatalf("exit %d, record %v\n%s", res.code, res.record, res.stderr)
	}
	if res.record.Stdin != "y\r\n" {
		t.Errorf("filedo.exe read %q from stdin, want %q", res.record.Stdin, "y\r\n")
	}
}

func TestUsageErrorExitsTwoWithoutRunningMain(t *testing.T) {
	_, launcher := install(t, true)
	for _, args := range badArgs {
		res := runLauncher(t, launcher, args, runOptions{})
		if res.code != exitCouldNotVerify {
			t.Errorf("%q: exit %d, want %d", args, res.code, exitCouldNotVerify)
		}
		if !strings.Contains(res.stderr, "Error:") {
			t.Errorf("%q: no error message on stderr:\n%s", args, res.stderr)
		}
		if res.record != nil {
			t.Errorf("%q: filedo.exe ran for a command line the companion refused: %q", args, res.record.Args)
		}
	}
}

func TestMissingMainExitsTwo(t *testing.T) {
	_, launcher := install(t, false)
	res := runLauncher(t, launcher, sampleArgs, runOptions{})
	if res.code != exitCouldNotVerify {
		t.Errorf("exit %d, want %d", res.code, exitCouldNotVerify)
	}
	if !strings.Contains(res.stderr, mainExeName) {
		t.Errorf("the error does not name %s:\n%s", mainExeName, res.stderr)
	}
}

func TestRefusesToRunItselfAsFiledo(t *testing.T) {
	// A launcher saved as filedo.exe must not start itself: a form that maps
	// onto itself would recurse without end.
	l, _ := binaries(t)
	dir := t.TempDir()
	impostor := filepath.Join(dir, mainExeName)
	copyFile(t, l, impostor)
	res := runLauncher(t, impostor, sampleArgs, runOptions{})
	if res.code != exitCouldNotVerify {
		t.Errorf("exit %d, want %d\n%s", res.code, exitCouldNotVerify, res.stderr)
	}
	if !strings.Contains(res.stderr, "this launcher itself") {
		t.Errorf("the error does not say why:\n%s", res.stderr)
	}
}

func TestNeverRunsAFiledoFromTheCurrentFolderOrPath(t *testing.T) {
	_, launcher := install(t, false)
	_, stub := binaries(t)
	planted := t.TempDir()
	copyFile(t, stub, filepath.Join(planted, mainExeName))
	res := runLauncher(t, launcher, sampleArgs, runOptions{
		dir: planted,
		env: []string{"PATH=" + planted + string(os.PathListSeparator) + os.Getenv("PATH")},
	})
	if res.record != nil {
		t.Fatalf("the launcher ran the filedo.exe planted in the current folder / PATH: %q", res.record.Args)
	}
	if res.code != exitCouldNotVerify {
		t.Errorf("exit %d, want %d", res.code, exitCouldNotVerify)
	}
}

func TestFindsMainThroughASymlink(t *testing.T) {
	// winget's portable install starts the companion through a link in
	// ...\WinGet\Links; filedo.exe is beside the link's target, not the link.
	_, launcher := install(t, true)
	link := filepath.Join(t.TempDir(), selfName)
	if err := os.Symlink(launcher, link); err != nil {
		t.Skipf("cannot create a symlink here (needs Developer Mode or elevation): %v", err)
	}
	res := runLauncher(t, link, sampleArgs, runOptions{})
	if res.code != 0 || res.record == nil {
		t.Fatalf("through a symlink: exit %d, filedo.exe ran: %v\n%s", res.code, res.record != nil, res.stderr)
	}
	if !reflect.DeepEqual(res.record.Args, sampleWant) {
		t.Errorf("filedo.exe got %q, want %q", res.record.Args, sampleWant)
	}
}

func TestNormalizeTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{"D:", "D:"},
		{"d:", "d:"},
		{`D:\`, `D:\`},
		{"D:/", "D:/"},
		{`D:\Temp`, `D:\Temp`},
		{`\\server\share`, `\\server\share`},
		{"//server/share", "//server/share"},
		{`\\?\D:\`, `\\?\D:\`},
		{`sub\dir`, `sub\dir`},
		{"E", "E:"},
		{"e", "e:"},
		{"d", "d:"},
		{"wipe", `.\wipe`},
		{"secure", `.\secure`},
		{"Photos 2025", `.\Photos 2025`},
		{".", `.\.`},
		{"..", `.\..`},
		{"7", `.\7`},
	}
	for _, c := range cases {
		if got := normalizeTarget(c.in); got != c.want {
			t.Errorf("normalizeTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitGlobalOptions(t *testing.T) {
	words, fwd, err := splitGlobalOptions([]string{
		"--events", "e.jsonl", "D:", "--stop-file", "s.stop", "x", "--events=f.jsonl", "--stop-file=t.stop",
		"--pause", "-pause", "/pause", "--no-history", "--no-ui", "nohist", "no_history", "-y", "--PAUSE",
	}, map[string]bool{"-y": true})
	if err != nil {
		t.Fatal(err)
	}
	wantWords := []string{"D:", "x", "--PAUSE"}
	wantFwd := []string{"--events", "e.jsonl", "--stop-file", "s.stop", "--events=f.jsonl", "--stop-file=t.stop",
		"--pause", "-pause", "/pause", "--no-history", "--no-ui", "nohist", "no_history", "-y"}
	if !reflect.DeepEqual(words, wantWords) {
		t.Errorf("words %q, want %q", words, wantWords)
	}
	if !reflect.DeepEqual(fwd, wantFwd) {
		t.Errorf("forwarded %q, want %q", fwd, wantFwd)
	}
	for _, args := range [][]string{{"D:", "--events"}, {"D:", "--stop-file"}} {
		if _, _, err := splitGlobalOptions(args, nil); !isUsageError(err) {
			t.Errorf("%q: err %v, want a usage error", args, err)
		}
	}
}

func TestIsHelpRequest(t *testing.T) {
	for _, args := range [][]string{{"?"}, {"/?"}, {"-?"}, {"--help"}, {"HELP"}, {"h"}, {"/help"}, {"D:", "-?"}, {"D:", "/?"}, {"D:", "--help"}} {
		if !isHelpRequest(args) {
			t.Errorf("%q is a help request", args)
		}
	}
	for _, args := range [][]string{{"D:"}, {"D:", "help"}, {"D:", "h"}, {"hd"}} {
		if isHelpRequest(args) {
			t.Errorf("%q is not a help request", args)
		}
	}
}

func isUsageError(err error) bool {
	var ue *usageError
	return errors.As(err, &ue)
}

// TestSharedFilesMatchTheOtherCompanions holds the three copies of the shared
// launcher files to one text, so a fix made in one companion cannot quietly
// miss the other two. A sibling folder that is not there (a module copied out
// on its own) is skipped, not failed.
func TestSharedFilesMatchTheOtherCompanions(t *testing.T) {
	normalize := func(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }
	for _, name := range []string{"launcher.go", "launcher_test.go", "launcher_windows_test.go"} {
		mine, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, sibling := range []string{"filedo-check", "filedo-fill", "filedo-test"} {
			path := filepath.Join("..", sibling, name)
			theirs, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				t.Logf("%s is not there; not compared", path)
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if normalize(mine) != normalize(theirs) {
				t.Errorf("%s differs from this module's %s: the shared launcher files change in all three companions together", path, name)
			}
		}
	}
}
