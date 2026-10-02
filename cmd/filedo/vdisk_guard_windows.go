//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"filedo/statedir"
	"filedo/vdisk"
)

// The shutdown guard (SP-0080): one scheduled task per account starts a
// resident watcher at logon, and at the session's end the watcher saves dirty
// ram disks - largest first - and unmounts every mounted container, which is
// what sets the clean marker, so the next logon says "Mounted" and not "Not
// closed cleanly". The task is written by the same elevated `_task` step as a
// mount task, and it is removed by `vd guard off` or by the user's own hand in
// Task Scheduler - never by an uninstall of FileDO.
//
// The guard never mounts anything, never deletes or wipes a container, and it
// never stores, asks for or accepts a credential: a save or an unmount talks
// to a block server that opened its container at mount time, by way of the
// same files and the same elevated steps the `save` and `unmount` verbs use.
// It runs only at the session's end - never at sleep, hibernate, lock or a
// disconnect - and it never blocks that end except while a ram save is
// actually running ("Shut down anyway" always remains the user's escape).

const (
	// vdGuardTask is the one task of the guard, fixed, in the \FileDO\ folder
	// beside the per-container mount tasks.
	vdGuardTask = vdTaskFolder + "FileDO Shutdown guard"

	// vdGuardReportFile is the report of the last session end (SP-0080 3.3),
	// schema filedo.vd-guard-run v1, shown by `vd guard status` and by the
	// Disk Manager's Autostart dialog.
	vdGuardReportFile   = "vd-guard-last-run.json"
	vdGuardReportSchema = "filedo.vd-guard-run"

	// vdGuardBeatFile is the heartbeat the watcher touches every 5 s; older
	// than 15 s means the watcher is not running (SP-0080 4, D8).
	vdGuardBeatFile   = "vd-guard-heartbeat.json"
	vdGuardBeatSchema = "filedo.vd-guard-heartbeat"
	vdGuardBeatEvery  = 5 * time.Second
	vdGuardStaleAfter = 15 * time.Second

	// vdGuardStopFile asks a running watcher to end without acting. `guard
	// off` writes it after removing the task, so a guard that is already
	// running does not outlive its task to the end of this session.
	vdGuardStopFile = "vd-guard.stop"

	// The per-container caps (D4): the time Windows grants at session end is
	// not contractual - the manual kit measures what it really is - so the
	// data most at risk goes first, and whatever does not finish is recorded,
	// not hidden.
	vdGuardSaveCap    = 60 * time.Second
	vdGuardUnmountCap = 10 * time.Second

	vdGuardBlockReason = "FileDO is saving your RAM disk.."
)

// The watcher's user32 calls. The window is the only way to hear that the
// session is ending: WM_QUERYENDSESSION and WM_ENDSESSION go to top-level
// windows, and they cover shutdown (Fast Startup's hybrid shutdown included),
// restart and sign-out, and never sleep, hibernate, lock or disconnect.
var (
	vdGuardUser32                 = windows.NewLazySystemDLL("user32.dll")
	vdGuardProcRegisterClassExW   = vdGuardUser32.NewProc("RegisterClassExW")
	vdGuardProcCreateWindowExW    = vdGuardUser32.NewProc("CreateWindowExW")
	vdGuardProcDefWindowProcW     = vdGuardUser32.NewProc("DefWindowProcW")
	vdGuardProcGetMessageW        = vdGuardUser32.NewProc("GetMessageW")
	vdGuardProcTranslateMessage   = vdGuardUser32.NewProc("TranslateMessage")
	vdGuardProcDispatchMessageW   = vdGuardUser32.NewProc("DispatchMessageW")
	vdGuardProcPostQuitMessage    = vdGuardUser32.NewProc("PostQuitMessage")
	vdGuardProcSetTimer           = vdGuardUser32.NewProc("SetTimer")
	vdGuardProcShowWindow         = vdGuardUser32.NewProc("ShowWindow")
	vdGuardProcBlockReasonCreate  = vdGuardUser32.NewProc("ShutdownBlockReasonCreate")
	vdGuardProcBlockReasonDestroy = vdGuardUser32.NewProc("ShutdownBlockReasonDestroy")
	vdGuardKernel32               = windows.NewLazySystemDLL("kernel32.dll")
	vdGuardProcGetConsoleWindow   = vdGuardKernel32.NewProc("GetConsoleWindow")
	vdGuardProcGetModuleHandleW   = vdGuardKernel32.NewProc("GetModuleHandleW")
)

// vdGuardMsg is windows.MSG, which this x/sys does not carry: the pump reads
// it through GetMessageW and hands it to Translate/Dispatch unchanged.
type vdGuardMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

const (
	vdGuardMsgQueryEndSession = 0x0011 // WM_QUERYENDSESSION
	vdGuardMsgEndSession      = 0x0016 // WM_ENDSESSION
	vdGuardMsgTimer           = 0x0113 // WM_TIMER
	vdGuardMsgDestroy         = 0x0002 // WM_DESTROY
)

// ---------------------------------------------------------------- the verbs

// vdGuard: filedo vd guard on | off | run | status.
func vdGuard(args []string, batch bool) error {
	usage := vdUsagef("guard needs one word: filedo vd guard on, off, run or status")
	if len(args) != 1 {
		return usage
	}
	switch strings.ToLower(args[0]) {
	case "on":
		return vdGuardOn(batch)
	case "off":
		return vdGuardOff(batch)
	case "run":
		return vdGuardWatch()
	case "status":
		return vdGuardStatusText()
	}
	// Never quoted back: the word may be a password typed in its slot.
	return usage
}

// vdGuardOn creates the task through the elevated `_task` step, saying the
// consequences first (SP-0004 7.4): what will run, the rights unmount needs,
// and that the task outlives an uninstall.
func vdGuardOn(batch bool) error {
	if vdGuardHasTask() {
		return vdUsagef("the shutdown guard is on already; take it off first with: filedo vd guard off")
	}
	exe, err := vdTaskExe()
	if err != nil {
		return err
	}
	sid, err := vdCurrentUserSID()
	if err != nil {
		return err
	}
	fmt.Printf("This creates the scheduled task %s: at your logon it runs\n  %s --no-history vd guard run\n", vdGuardTask, exe)
	fmt.Println("hidden, with administrator rights and without asking.")
	fmt.Println("When your session ends - a shutdown, a restart or a sign-out - it saves dirty ram disks, largest first, and unmounts every mounted container, so each one is closed cleanly at the next logon.")
	fmt.Println("The unmount needs the administrator rights the task grants. Sleep, hibernate and locking are not the session's end; sign-out is, and Fast Startup shuts the session down the same way a shutdown does.")
	fmt.Printf("It stays until you remove it with: filedo vd guard off  (an uninstall of FileDO does not remove it).\n")
	if _, err := vdRunElevated("_task", vdRequest{TaskName: vdGuardTask, TaskSID: sid}, batch, nil); err != nil {
		return err
	}
	// A fresh on must not be ended at once by a stop a previous off left over.
	os.Remove(vdGuardStatePath(vdGuardStopFile))
	vdLogf("guard: created task %s, command %s --no-history vd guard run", vdGuardTask, exe)
	fmt.Println("Created. The guard starts at your next logon; this session is not watched.")
	return nil
}

// vdGuardOff removes the task and touches no disk. A watcher that is running
// has already been asked, by the stop file, to end without acting.
func vdGuardOff(batch bool) error {
	if !vdGuardHasTask() {
		return vdUsagef("the shutdown guard is not on (see: filedo vd guard status)")
	}
	if _, err := vdRunElevated("_task", vdRequest{TaskName: vdGuardTask, TaskDelete: true}, batch, nil); err != nil {
		return err
	}
	vdLogf("guard: removed task %s", vdGuardTask)
	fmt.Printf("Removed the task %s. No disk was touched.\n", vdGuardTask)
	if vdGuardRunning() {
		if err := statedir.WriteFileAtomic(vdGuardStatePath(vdGuardStopFile), []byte("stop"), 0o600); err == nil {
			fmt.Println("The watcher that is running has been asked to stop; it unmounts nothing.")
		}
	}
	return nil
}

// vdGuardStatusText is the text for a person: installed, running, and the last
// run in three lines (SP-0080 4).
func vdGuardStatusText() error {
	if !vdGuardHasTask() {
		fmt.Println("The shutdown guard is not installed.")
		fmt.Println("At the end of your session it would save dirty ram disks, largest first, and unmount every mounted container, so each is closed cleanly at the next logon.")
		fmt.Println("Turn it on with: filedo vd guard on")
		return nil
	}
	state := "not running"
	if vdGuardRunning() {
		state = "running"
	}
	fmt.Printf("The shutdown guard is installed (task %s) and is %s.\n", vdGuardTask, state)
	r, ok := vdGuardReadReport()
	if !ok {
		fmt.Println("It has not run yet: it runs when a session ends while the guard is on.")
		return nil
	}
	fmt.Printf("The last run %s: %s\n", vdTime(time.Time(r.At)), r.Summary)
	left := 0
	for _, row := range r.Containers {
		if row.Outcome != "saved" && row.Outcome != "unmounted" {
			fmt.Printf("  %s: %s - %s\n", row.Name, row.Outcome, row.Reason)
			left++
		}
	}
	if left == 0 {
		fmt.Println("Nothing was left behind.")
	}
	return nil
}

// ---------------------------------------------------------------- the watcher

// vdGuardSessionEnd is the seam the tests drive: the window procedure calls it
// when the session ends, and a test calls the procedure with a fake message.
var vdGuardSessionEnd = vdGuardEndSession

// vdGuardNow is the clock of the heartbeat and of the report; a seam.
var vdGuardNow = time.Now

// vdGuardHasTask is the verbs' door to the task query: on, off and status ask
// it before anything else, and a test points it at "no task" so nothing here
// touches the machine's own scheduler. The snapshot has its own seam above
// this one (vdSnapshotGuard).
var vdGuardHasTask = vdGuardInstalled

// vdGuardServerAlive is the watcher's door to a block server's liveness; the
// snapshot's seam (vdSnapshotAlive) is its own.
var vdGuardServerAlive = vdServerAlive

// vdGuardWatch is `vd guard run`: what the task starts. It hides the console a
// task gives a console application, refuses a second instance by the
// heartbeat, and pumps messages until the session ends.
func vdGuardWatch() error {
	vdGuardHideConsole()
	if vdGuardRunning() {
		// A second watcher would race the first at the session's end; the
		// heartbeat is the one seat, and it is taken (SP-0080 3.2).
		vdLogf("guard: the heartbeat is already held; a second watcher exits")
		return nil
	}
	// A stop left over from a guard off before this run must not end it at once.
	os.Remove(vdGuardStatePath(vdGuardStopFile))
	return vdGuardPump()
}

func vdGuardHideConsole() {
	if h, _, _ := vdGuardProcGetConsoleWindow.Call(); h != 0 {
		vdGuardProcShowWindow.Call(h, 0 /* SW_HIDE */)
	}
}

var vdGuardWindow uintptr

// vdGuardPump creates the watcher's invisible top-level window - message-only
// windows never receive the session-end broadcast - and runs until
// PostQuitMessage. The heartbeat is a WM_TIMER on the pump's own thread, so
// the beat never races the pump.
func vdGuardPump() error {
	cls, err := vdGuardWindowClass()
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString("FileDO vd guard")
	if err != nil {
		return err
	}
	h, _, _ := vdGuardProcCreateWindowExW.Call(0, uintptr(cls), uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0, 0, 0, uintptr(vdGuardModule()), 0)
	if h == 0 {
		return fmt.Errorf("the watcher's window could not be created")
	}
	vdGuardWindow = h
	vdGuardProcSetTimer.Call(h, 1, uintptr(vdGuardBeatEvery.Milliseconds()), 0)
	vdGuardTouchBeat()
	vdLogf("guard: watching (pid %d)", os.Getpid())
	var m vdGuardMsg
	for {
		r, _, _ := vdGuardProcGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1, 0: // an error, or WM_QUIT
			return nil
		}
		vdGuardProcTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		vdGuardProcDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// vdGuardWndProc is the watcher's window procedure. Everything but the
// session-end work is a line; the work is behind vdGuardSessionEnd.
func vdGuardWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case vdGuardMsgQueryEndSession:
		return 1 // the session may end; the work happens at WM_ENDSESSION
	case vdGuardMsgEndSession:
		if wp != 0 {
			vdGuardSessionEnd()
			vdGuardProcPostQuitMessage.Call(0)
		}
		return 0
	case vdGuardMsgTimer:
		vdGuardBeat()
	case vdGuardMsgDestroy:
		vdGuardProcPostQuitMessage.Call(0)
	}
	r, _, _ := vdGuardProcDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// vdGuardBeat is the timer tick: touch the heartbeat, and honour a stop a
// `guard off` left, by ending without acting.
func vdGuardBeat() {
	vdGuardTouchBeat()
	if _, err := os.Stat(vdGuardStatePath(vdGuardStopFile)); err == nil {
		os.Remove(vdGuardStatePath(vdGuardStopFile))
		vdLogf("guard: stop requested; the watcher ends without touching a disk")
		vdGuardProcPostQuitMessage.Call(0)
	}
}

type vdGuardWndClass struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   uintptr
	ClassName  uintptr
	SmallIcon  uintptr
}

var vdGuardClass uintptr

func vdGuardWindowClass() (uintptr, error) {
	if vdGuardClass != 0 {
		return vdGuardClass, nil
	}
	name, err := windows.UTF16PtrFromString("FileDO vd guard")
	if err != nil {
		return 0, err
	}
	c := vdGuardWndClass{
		Size:      uint32(unsafe.Sizeof(vdGuardWndClass{})),
		WndProc:   windows.NewCallback(vdGuardWndProc),
		Instance:  vdGuardModule(),
		ClassName: uintptr(unsafe.Pointer(name)),
	}
	atom, _, _ := vdGuardProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&c)))
	if atom == 0 {
		return 0, fmt.Errorf("the watcher's window class could not be registered")
	}
	vdGuardClass = atom
	return atom, nil
}

func vdGuardModule() uintptr {
	h, _, _ := vdGuardProcGetModuleHandleW.Call(0)
	return h
}

// ---------------------------------------------------------------- the session end

// vdGuardEndSession is the whole work of a session's end: save the dirty ram
// disks (largest first, under the block reason), unmount every mounted
// container, write the report, leave. The order is the honesty: the data most
// at risk goes first, and whatever does not finish is recorded, not hidden.
func vdGuardEndSession() {
	vdLogf("guard: the session is ending")
	targets, err := vdGuardTargets()
	if err != nil {
		vdLogf("guard: the mount state could not be read, so nothing was touched: %v", err)
		return
	}
	saves, unmounts := vdGuardPlan(targets)
	steps := vdGuardSteps{Block: vdGuardSetBlock, Save: vdGuardSaveStep, Unmount: vdGuardUnmountStep}
	rep := vdGuardExecute(saves, unmounts, steps, vdGuardNow())
	if b, err := json.Marshal(rep); err == nil {
		if p, err := statedir.Path(vdGuardReportFile); err == nil {
			statedir.WriteFileAtomic(p, b, 0o600)
		}
	}
	vdLogf("guard: done - %s", rep.Summary)
}

// vdGuardTarget is one mounted container as the guard sees it at the session's
// end: the state row, the registry's name for it, and what its server says.
type vdGuardTarget struct {
	Row         vdMountRow
	Name        string
	DirtyBytes  int64
	ServerAlive bool
	IsRAM       bool
}

// vdGuardTargets reads every mounted container: the state file's rows, the
// registry's names and each ram server's own status. A registry that cannot be
// read costs the names, not the work.
func vdGuardTargets() ([]vdGuardTarget, error) {
	s, err := vdLoadState()
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if reg, err := vdLoadRegistry(); err == nil {
		for _, e := range reg.Containers {
			names[strings.ToLower(e.ContainerID)] = e.Name
		}
	}
	mounts := append([]vdMountRow(nil), s.Mounts...)
	sort.SliceStable(mounts, func(a, b int) bool { return mounts[a].Letter < mounts[b].Letter })
	var out []vdGuardTarget
	for _, m := range mounts {
		t := vdGuardTarget{
			Row:         m,
			ServerAlive: vdGuardServerAlive(m),
			IsRAM:       m.Profile == vdisk.ProfileRAM.String(),
		}
		t.Name = names[strings.ToLower(m.ContainerID)]
		if t.Name == "" {
			t.Name = strings.TrimSuffix(filepath.Base(m.Path), filepath.Ext(m.Path))
		}
		if t.IsRAM && t.ServerAlive && !m.ReadOnly {
			if r, ok := vdReadRAMStatus(m.ContainerID); ok {
				t.DirtyBytes = r.DirtyBytes
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// vdGuardPlan orders the session-end work: dirty ram saves first, largest
// dirty first, then every mounted container by letter (SP-0080 3.2).
func vdGuardPlan(targets []vdGuardTarget) (saves, unmounts []vdGuardTarget) {
	for _, t := range targets {
		if t.IsRAM && t.ServerAlive && !t.Row.ReadOnly && t.DirtyBytes > 0 {
			saves = append(saves, t)
		}
	}
	sort.SliceStable(saves, func(a, b int) bool {
		if saves[a].DirtyBytes != saves[b].DirtyBytes {
			return saves[a].DirtyBytes > saves[b].DirtyBytes
		}
		return saves[a].Row.Letter < saves[b].Row.Letter
	})
	unmounts = append(unmounts, targets...)
	sort.SliceStable(unmounts, func(a, b int) bool { return unmounts[a].Row.Letter < unmounts[b].Row.Letter })
	return saves, unmounts
}

// vdGuardReport is the report of one session's end (SP-0080 3.3): when, what
// ended the session, one row per container, and the leftovers in one sentence.
// It carries no credential, no port, no process id - the same "never carries"
// rule as the snapshot (SP-0063 8.1).
type vdGuardReport struct {
	Schema     string       `json:"schema"`
	Version    int          `json:"version"`
	At         vdStamp      `json:"at"`
	Ended      string       `json:"ended"`
	Containers []vdGuardRow `json:"containers"`
	Summary    string       `json:"summary"`
}

// vdGuardRow is one container's outcome. The trigger covers shutdown, restart
// and sign-out alike (D7): the report says the session ended, and pretends to
// know none of the three apart.
type vdGuardRow struct {
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Action     string  `json:"action"`  // save | unmount
	Outcome    string  `json:"outcome"` // saved | unmounted | skipped | unfinished
	Reason     string  `json:"reason,omitempty"`
	BytesSaved int64   `json:"bytes_saved,omitempty"`
	Seconds    float64 `json:"seconds"`
}

// vdGuardSteps are the runner's three moves: the block reason, a save, an
// unmount. The real ones touch the machine; a test scripts them.
type vdGuardSteps struct {
	Block   func(on bool)
	Save    func(t vdGuardTarget, cap time.Duration) vdGuardRow
	Unmount func(t vdGuardTarget, cap time.Duration) vdGuardRow
}

// vdGuardExecute runs the plan in its order and builds the report. The block
// reason exists only while a ram save is actually running (D3), and "Shut down
// anyway" always wins over it.
func vdGuardExecute(saves, unmounts []vdGuardTarget, steps vdGuardSteps, at time.Time) vdGuardReport {
	rep := vdGuardReport{Schema: vdGuardReportSchema, Version: 1, At: vdStamp(at), Ended: "session", Containers: []vdGuardRow{}}
	blocked := false
	defer func() {
		if blocked {
			steps.Block(false)
		}
	}()
	if len(saves) > 0 {
		steps.Block(true)
		blocked = true
	}
	for _, t := range saves {
		rep.Containers = append(rep.Containers, steps.Save(t, vdGuardSaveCap))
	}
	for _, t := range unmounts {
		rep.Containers = append(rep.Containers, steps.Unmount(t, vdGuardUnmountCap))
	}
	if blocked {
		steps.Block(false)
		blocked = false
	}
	rep.Summary = vdGuardSummary(rep.Containers)
	return rep
}

// vdGuardSummary is the report's one sentence: a run that did everything says
// so in one breath; a run that ran out of time says what was left. Containers
// are counted by their unmount rows - every mounted container has exactly one,
// and a ram disk has a save row besides - so a saved ram disk whose unmount
// did not finish is one container not closed, never "one of two" closed.
func vdGuardSummary(rows []vdGuardRow) string {
	if len(rows) == 0 {
		return "Nothing was mounted; there was nothing to do."
	}
	containers, closed, skipped, unfinished, saved := 0, 0, 0, 0, 0
	for _, r := range rows {
		if r.Action == "unmount" {
			containers++
		}
		switch r.Outcome {
		case "saved":
			saved++
		case "unmounted":
			closed++
		case "unfinished":
			unfinished++
		default:
			skipped++
		}
	}
	if skipped == 0 && unfinished == 0 {
		if saved > 0 {
			return fmt.Sprintf("Every mounted container was closed cleanly (%d ram disk(s) saved first).", saved)
		}
		return "Every mounted container was closed cleanly."
	}
	parts := []string{fmt.Sprintf("%d of %d containers were closed cleanly", closed, containers)}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	if unfinished > 0 {
		parts = append(parts, fmt.Sprintf("%d unfinished", unfinished))
	}
	return strings.Join(parts, ", ") + " - the rows above say which and why."
}

// vdGuardSetBlock sets and clears the shutdown block reason (D3): only while a
// ram save is actually running, and never for an unmount.
func vdGuardSetBlock(on bool) {
	if vdGuardWindow == 0 {
		return
	}
	if on {
		if s, err := windows.UTF16PtrFromString(vdGuardBlockReason); err == nil {
			vdGuardProcBlockReasonCreate.Call(vdGuardWindow, uintptr(unsafe.Pointer(s)))
		}
		return
	}
	vdGuardProcBlockReasonDestroy.Call(vdGuardWindow)
}

// vdGuardSaveStep is the save of one dirty ram disk, through the same code the
// `save` verb runs: the elevated flush, then the save request the block server
// answers. A container whose save needs a credential the guard does not have
// is impossible here - the server opened its container at mount time - and a
// save that fails for any reason is skipped and recorded, never asked for.
func vdGuardSaveStep(t vdGuardTarget, cap time.Duration) vdGuardRow {
	return vdGuardCapped(vdGuardRow{Name: t.Name, Path: t.Row.Path, Action: "save"}, cap, func() (string, string, int64) {
		return vdGuardSaveOne(t)
	})
}

func vdGuardSaveOne(t vdGuardTarget) (outcome, reason string, bytesSaved int64) {
	if !t.ServerAlive {
		return "skipped", "its block server is gone; what it still held in memory was lost", 0
	}
	if t.Row.ReadOnly {
		return "skipped", "mounted read-only; nothing to save", 0
	}
	if _, err := vdRunElevated("_flush", vdRequest{Letter: t.Row.Letter}, true, nil); err != nil {
		return "skipped", "the Windows cache could not be flushed: " + err.Error(), 0
	}
	savePath, err := vdSidePath(t.Row.ContainerID, ".save")
	if err != nil {
		return "skipped", err.Error(), 0
	}
	savedPath, _ := vdSidePath(t.Row.ContainerID, ".saved.json")
	token := fmt.Sprintf("guard-%d-%d", os.Getpid(), vdGuardNow().UnixNano())
	os.Remove(savedPath)
	if err := statedir.WriteFileAtomic(savePath, []byte(token), 0o600); err != nil {
		return "skipped", "the save request could not be written: " + err.Error(), 0
	}
	for {
		if b, err := os.ReadFile(savedPath); err == nil {
			var ans vdSaved
			if json.Unmarshal(b, &ans) == nil && ans.Request == token {
				if ans.Error != "" {
					return "skipped", "the save failed: " + ans.Error, 0
				}
				return "saved", "", t.DirtyBytes
			}
		}
		if !vdServerAlive(t.Row) {
			return "skipped", "the block server exited before it answered the save", 0
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// vdGuardUnmountStep unmounts one mounted container, through the same code the
// `unmount` verb runs, under its own cap.
func vdGuardUnmountStep(t vdGuardTarget, cap time.Duration) vdGuardRow {
	return vdGuardCapped(vdGuardRow{Name: t.Name, Path: t.Row.Path, Action: "unmount"}, cap, func() (string, string, int64) {
		return vdGuardUnmountOne(t)
	})
}

func vdGuardUnmountOne(t vdGuardTarget) (outcome, reason string, _ int64) {
	if _, _, _, err := vdUnmountOne(t.Row, false, false, true); err != nil {
		return "skipped", err.Error(), 0
	}
	return "unmounted", "", 0
}

// vdGuardCapped runs one container's work under its cap. The work cannot be
// taken back mid-flight, so an overrun abandons the goroutine - the process
// exits at the session's end anyway - and the row says unfinished.
func vdGuardCapped(row vdGuardRow, cap time.Duration, work func() (outcome, reason string, bytesSaved int64)) vdGuardRow {
	type answer struct {
		outcome, reason string
		bytes           int64
	}
	done := make(chan answer, 1)
	go func() {
		o, r, b := work()
		done <- answer{o, r, b}
	}()
	start := vdGuardNow()
	select {
	case a := <-done:
		row.Outcome, row.Reason, row.BytesSaved = a.outcome, a.reason, a.bytes
	case <-time.After(cap):
		row.Outcome = "unfinished"
		row.Reason = fmt.Sprintf("did not finish within %d s; the session's end does not wait", int(cap.Seconds()))
	}
	row.Seconds = vdGuardNow().Sub(start).Seconds()
	return row
}

// ---------------------------------------------------------------- heartbeat and report

func vdGuardStatePath(name string) string {
	p, err := statedir.Path(name)
	if err != nil {
		return filepath.Join(os.TempDir(), name)
	}
	return p
}

type vdGuardBeatDoc struct {
	Schema  string  `json:"schema"`
	Version int     `json:"version"`
	At      vdStamp `json:"at"`
}

func vdGuardTouchBeat() {
	b, _ := json.Marshal(vdGuardBeatDoc{Schema: vdGuardBeatSchema, Version: 1, At: vdStamp(vdGuardNow())})
	statedir.WriteFileAtomic(vdGuardStatePath(vdGuardBeatFile), b, 0o600)
}

// vdGuardBeatAge is how old the heartbeat is, and whether there was one to
// read. A document that does not parse is no heartbeat.
func vdGuardBeatAge() (time.Duration, bool) {
	b, err := os.ReadFile(vdGuardStatePath(vdGuardBeatFile))
	if err != nil {
		return 0, false
	}
	var d vdGuardBeatDoc
	if json.Unmarshal(b, &d) != nil || d.Schema != vdGuardBeatSchema {
		return 0, false
	}
	t := time.Time(d.At)
	if t.IsZero() {
		return 0, false
	}
	return vdGuardNow().Sub(t), true
}

// vdGuardRunning is the heartbeat's answer (D8): touched within the last
// 15 s means a watcher is running; anything older, or nothing, means not.
func vdGuardRunning() bool {
	age, ok := vdGuardBeatAge()
	return ok && age >= 0 && age <= vdGuardStaleAfter
}

// vdGuardInstalled is whether the task is there. schtasks' output is
// localized; only the exit code is read.
func vdGuardInstalled() bool {
	return exec.Command(vdSchtasks(), "/Query", "/TN", vdGuardTask).Run() == nil
}

func vdGuardReadReport() (vdGuardReport, bool) {
	b, err := os.ReadFile(vdGuardStatePath(vdGuardReportFile))
	if err != nil {
		return vdGuardReport{}, false
	}
	var r vdGuardReport
	if json.Unmarshal(b, &r) != nil || r.Schema != vdGuardReportSchema {
		return vdGuardReport{}, false
	}
	return r, true
}

// vdGuardState is the snapshot's `guard` field (SP-0080 4): additive inside
// schema v1, whose reader ignores unknown fields - an old GUI and a new CLI,
// and the reverse, both keep working.
func vdGuardState() vdSnapGuard {
	g := vdSnapGuard{Installed: vdGuardHasTask(), Running: vdGuardRunning()}
	if r, ok := vdGuardReadReport(); ok {
		g.LastRun = r.At
		g.Ended = r.Ended
		for _, row := range r.Containers {
			g.Containers = append(g.Containers, vdSnapGuardRow{
				Name: row.Name, Path: row.Path, Action: row.Action, Outcome: row.Outcome,
				Reason: row.Reason, BytesSaved: row.BytesSaved,
			})
		}
	}
	return g
}
