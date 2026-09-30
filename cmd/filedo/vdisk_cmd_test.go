package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"filedo/vdisk"
)

func TestVD_Claims(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want bool
	}{
		{[]string{"work.fdd", "mount"}, true},
		{[]string{"WORK.FDD", "MNT", "ro"}, true},
		{[]string{"work.fdd", "unmount", "force"}, true},
		{[]string{"work.fdd", "info"}, true},
		{[]string{"X:", "unmount"}, true},
		{[]string{"x:", "detach"}, true},
		{[]string{"work.fdd", "save"}, true},
		{[]string{"X:", "save"}, true},
		{[]string{"work.txt", "save"}, false},
		{[]string{"work.fdd", "seal", "s.fdd"}, true},
		{[]string{"X:", "seal"}, false},
		{[]string{"disc.iso", "mount"}, true},
		{[]string{"D.VHDX", "unmount"}, true},
		{[]string{"disc.iso", "copy", "e:"}, false},
		{[]string{"X:", "mount"}, false}, // a drive is not a container
		{[]string{"X:", "info"}, false},  // the generic device info
		{[]string{"work.txt", "mount"}, false},
		{[]string{"work.fdd", "copy"}, false},
		{[]string{"work.fdd"}, false},
	} {
		if got := vdClaims(c.argv); got != c.want {
			t.Errorf("vdClaims(%v) = %v, want %v", c.argv, got, c.want)
		}
	}
}

// TestVD_WordsDoNotCollide: no vd word is an operation word of the generic
// chain or an option word of the sibling (spec 5.2, "a trap in that alias list").
func TestVD_WordsDoNotCollide(t *testing.T) {
	var words []string
	for _, l := range [][]string{list_of_flags_for_vd, vdMountWords, vdUnmountWords, vdNewWords, vdSaveWords, vdSealWords, {"ro", "readonly", "noscan", "nosave", "as", "force", "status", "stop"}} {
		words = append(words, l...)
	}
	for _, w := range words {
		if isOperationWord(w) {
			t.Errorf("%q is a generic operation word", w)
		}
		if verbOf(w) != "" && !contains(list_of_flags_for_vd, w) {
			t.Errorf("%q is already the verb %q", w, verbOf(w))
		}
	}
	if verbOf("vd") != "vd" || verbOf("VDISK") != "vd" {
		t.Fatal("vd is not a verb")
	}
}

func TestVD_MountOptions(t *testing.T) {
	o, err := vdParseMountOpts([]string{"a.fdd", "as", "x:", "ro"})
	if err != nil || o.Path != "a.fdd" || o.Letter != "X:" || !o.ReadOnly || o.NoScan {
		t.Fatalf("%+v %v", o, err)
	}
	if o, err := vdParseMountOpts([]string{"a.fdd", "as", "Q", "NoScan"}); err != nil || o.Letter != "Q:" || !o.NoScan {
		t.Fatalf("as Q noscan: %+v %v", o, err)
	}
	for _, bad := range [][]string{{"a.fdd", "as"}, {"a.fdd", "as", "C:"}, {"a.fdd", "as", "XY"}, {"a.fdd", "rw", "ro"}, {"a.fdd", "ro", "pw"}, {"a.fdd", "p:a", "k:b"}, {}} {
		if _, err := vdParseMountOpts(bad); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("%v: %v", bad, err)
		}
	}
	// The shared grammar's bare trailing token (SP-0004 P5): the one word
	// after the container is a password unless it is an option. On an
	// obfuscated container vdMount refuses it as a mistyped option, which is
	// what `a.fdd rw` always was.
	for _, c := range []struct {
		args []string
		want credArg
		ro   bool
	}{
		{[]string{"a.fdd", "rw"}, credArg{"bare", "rw"}, false},
		{[]string{"a.fdd", "hunter2"}, credArg{"bare", "hunter2"}, false},
		{[]string{"a.fdd", "ro"}, credArg{}, true},
		{[]string{"a.fdd", "p:ro"}, credArg{"p", "ro"}, false},
		{[]string{"a.fdd", "ro", "as", "x:", "k:C:\\key"}, credArg{"k", "C:\\key"}, true},
		{[]string{"a.fdd", "pf:C:\\my files\\pw.txt", "ro"}, credArg{"pf", "C:\\my files\\pw.txt"}, true},
	} {
		o, err := vdParseMountOpts(c.args)
		if err != nil || o.Cred != c.want || o.ReadOnly != c.ro {
			t.Errorf("%v: %+v %v", c.args, o, err)
		}
	}
}

func TestVD_ExitClasses(t *testing.T) {
	for _, c := range []struct {
		err  error
		want int
	}{
		{errTransport("x"), 7},
		{errBusy("x"), 8},
		{vdUsagef("x"), 2},
		{vdClassError(7, "remote"), 7},
		{vdClassError(8, "remote"), 8},
		{vdClassError(5, "remote"), 5},
		{fmt.Errorf("wrapped: %w", vdisk.ErrDamaged), 4},
	} {
		if got := vdExitClass(c.err); got != c.want {
			t.Errorf("%v: class %d, want %d", c.err, got, c.want)
		}
	}
	if !errors.Is(vdClassError(8, "m"), vdisk.ErrBusy) || vdClassError(8, "m").Error() != "m" {
		t.Fatal("a remote error loses its class or its text")
	}
}

func TestVD_IQN(t *testing.T) {
	iqn := vdIQN("D0B665D8-08F3-4BE1-A376-6977FC74A9E1")
	if !regexp.MustCompile(`^iqn\.2026-09\.ua\.od\.sza:filedo\.vd\.[0-9a-f]{32}$`).MatchString(iqn) || len(iqn) > 223 {
		t.Fatalf("iqn %q", iqn)
	}
}

// TestVD_Registry: add, the path-wins rule, a taken name, forget, and a file
// that is gone listed as missing rather than dropped.
func TestVD_Registry(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "work.fdd")
	c, err := vdisk.Create(context.Background(), vdisk.CreateOptions{Path: path, LogicalSize: 1 << 20, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := vdAdd([]string{path}); err != nil {
		t.Fatal(err)
	}
	if got := vdResolve("work"); got != path {
		t.Fatalf("work resolves to %q", got)
	}
	if got := vdResolve("WORK"); got != path {
		t.Fatal("names are not case-insensitive")
	}
	if err := vdAdd([]string{path, "as", "other"}); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("the same file twice: %v", err)
	}
	if err := vdAdd([]string{path, "as", "X:"}); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("a drive letter as a name: %v", err)
	}
	// A file named like the entry, in the current folder, wins.
	os.WriteFile("work", []byte("x"), 0o600)
	if got := vdResolve("work"); got != "work" {
		t.Fatalf("the path did not win: %q", got)
	}
	os.Remove("work")
	os.Remove(path)
	out := captureStdout(t, func() {
		if err := vdList(); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "MISSING") || !strings.Contains(out, path) {
		t.Fatalf("a gone file is not listed as missing:\n%s", out)
	}
	if err := vdForget([]string{"work"}); err != nil {
		t.Fatal(err)
	}
	if vdResolve("work") != "work" {
		t.Fatal("a forgotten name still resolves")
	}
	if err := vdForget([]string{"work"}); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("forgetting twice: %v", err)
	}
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

// TestVD_TaskXML: the task names the owner in its trigger and its principal,
// runs with the highest rights, and escapes what it carries.
func TestVD_TaskXML(t *testing.T) {
	x := vdTaskXML("work", `C:\a&b\work.fdd`, `C:\Program Files\FileDO\filedo.exe`, "S-1-5-21-1-2-3-1001")
	for _, want := range []string{
		"<UserId>S-1-5-21-1-2-3-1001</UserId>", "<RunLevel>HighestAvailable</RunLevel>", "<LogonTrigger>",
		"--no-history vd mount work", `C:\a&amp;b\work.fdd`, `<WorkingDirectory>C:\Program Files\FileDO</WorkingDirectory>`,
	} {
		if !strings.Contains(x, want) {
			t.Errorf("task XML lacks %q", want)
		}
	}
	if strings.Count(x, "<UserId>") != 2 {
		t.Error("the trigger or the principal does not name the user")
	}
	if !strings.HasPrefix(vdTaskName("work"), `\FileDO\`) {
		t.Error("the task is not in the FileDO folder")
	}
}

// TestVD_State: concurrent updates serialise under the lock, and a row whose
// server is gone - or whose pid was reused - is recognised as stale.
func TestVD_State(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := vdUpdateState(func(s *vdState) error {
				s.Mounts = append(s.Mounts, vdMountRow{ContainerID: fmt.Sprint(i)})
				return nil
			}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	s, err := vdLoadState()
	if err != nil || len(s.Mounts) != 16 {
		t.Fatalf("%d rows after 16 concurrent updates: %v", len(s.Mounts), err)
	}
	if _, ok := vdFindMount("7"); !ok {
		t.Fatal("row 7 missing")
	}

	self := os.Getpid()
	started, alive := vdProcessStart(self)
	if !alive || started == 0 {
		t.Fatal("the test process is not seen alive")
	}
	if !vdServerAlive(vdMountRow{ServerPID: self, ServerStarted: started}) {
		t.Fatal("a live server is taken for dead")
	}
	if vdServerAlive(vdMountRow{ServerPID: self, ServerStarted: started + 1}) {
		t.Fatal("a reused pid is taken for the server")
	}
	cmd := exec.Command("cmd", "/c", "exit", "0")
	if err := cmd.Run(); err != nil {
		t.Skip("cannot run cmd:", err)
	}
	if vdServerAlive(vdMountRow{ServerPID: cmd.Process.Pid}) {
		t.Fatal("an exited process is taken for a live server")
	}
}
