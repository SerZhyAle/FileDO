//go:build windows

package main

import (
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"filedo/vdisk"
)

// Automatic mounting (SP-0004 Q11, spec 11, P4 section 7): a scheduled task
// the owner creates on purpose, one per registered container, in the \FileDO\
// folder of the system's own Task Scheduler, removable in one command.
//
// The task runs `filedo --no-history vd mount <name>` at the owner's logon,
// with the highest privileges the account has - which is what lets the
// initiator step run without a consent prompt nobody would be there to see.
// It is written as task XML rather than with schtasks' own switches because
// /SC ONLOGON alone triggers at the logon of ANY user; the XML names the
// owner's account in the trigger and in the principal. Creating and removing
// it needs administrator rights, so both go through the consent prompt as the
// internal step _task.
//
// Only the logon trigger exists in this version. A start-up trigger runs
// before anybody logs on, as another account, whose state folder and drive
// letters are not the owner's; it is refused with that reason (P4 amendment 2).

const vdTaskFolder = `\FileDO\`

func vdTaskName(name string) string { return vdTaskFolder + "FileDO Mount " + name }

// vdAutoTasks lists the containers that have a task, by lower-cased name.
// Best effort: a listing that fails shows no tasks.
func vdAutoTasks() map[string]bool {
	out := map[string]bool{}
	b, err := exec.Command(vdSchtasks(), "/Query", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return out
	}
	rows, _ := csv.NewReader(bytes.NewReader(b)).ReadAll()
	prefix := strings.ToLower(vdTaskName(""))
	for _, r := range rows {
		if len(r) > 0 && strings.HasPrefix(strings.ToLower(r[0]), prefix) {
			out[strings.TrimPrefix(strings.ToLower(r[0]), prefix)] = true
		}
	}
	return out
}

func vdHasTask(name string) bool { return vdAutoTasks()[strings.ToLower(name)] }

func vdSchtasks() string {
	sys, _ := windows.GetSystemDirectory()
	return filepath.Join(sys, "schtasks.exe")
}

// vdAuto: filedo vd auto <name> logon | filedo vd auto off <name>.
func vdAuto(args []string, batch bool) error {
	usage := vdUsagef("auto needs a registered name and a trigger: filedo vd auto <name> logon, or filedo vd auto off <name>")
	if len(args) != 2 {
		return usage
	}
	if strings.EqualFold(args[0], "off") {
		name := args[1]
		if n := vdRegisteredName(name); n != "" {
			name = n
		}
		return vdAutoOff(name, batch)
	}
	name, trigger := args[0], strings.ToLower(args[1])
	if n := vdRegisteredName(name); n != "" {
		// A registered container's path means its name: the task is named after it.
		name = n
	}
	switch trigger {
	case "logon":
	case "start", "startup", "boot":
		return fmt.Errorf("%w: a start-up trigger runs before anybody logs on, as another account whose state folder and drive letters are not yours; this version mounts at logon only", vdisk.ErrUnsupported)
	default:
		return usage
	}
	r, err := vdLoadRegistry()
	if err != nil {
		return err
	}
	i := r.find(name)
	if i < 0 {
		return vdUsagef("%s is not a registered name; register the container first: filedo vd add <file.fdd> as %s", name, name)
	}
	e := r.Containers[i]
	if vdHasTask(e.Name) {
		return vdUsagef("%s mounts automatically already; remove it first with: filedo vd auto off %s", e.Name, e.Name)
	}
	if _, err := os.Stat(e.Path); err != nil {
		return vdUsagef("the file of %s is missing: %s", e.Name, e.Path)
	}
	// An encrypted container needs a credential the machine can reach
	// without a person, and this version keeps none (FDD-BEHAVIOUR 7 rule
	// 9). Refused here, loudly; a task created anyway would fail at every
	// logon with the usage class, since its mount has no terminal to ask on.
	if info, err := vdisk.Inspect(e.Path); err != nil {
		return err
	} else if !info.Obfuscated {
		return vdUsagef("%s is encrypted, and an automatic mount would need its credential stored where the machine can read it without you; this version stores none, so it does not mount encrypted containers automatically", e.Name)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(exe), `\windowsapps\`) {
		// A packaged app's own path is not something a task can start; the
		// execution alias is.
		exe = "filedo.exe"
	}
	sid, err := vdCurrentUserSID()
	if err != nil {
		return err
	}
	taskXML := vdTaskXML(e.Name, e.Path, exe, sid)

	fmt.Printf("This creates the scheduled task %s: at your logon it runs\n  %s --no-history vd mount %s\n", vdTaskName(e.Name), exe, e.Name)
	fmt.Println("with administrator rights and without asking, so the volume is there when you log on.")
	fmt.Println("The container is obfuscated, not encrypted: anyone who has the file reads it, and once mounted the volume is open to every program that runs as you.")
	fmt.Printf("It stays until you remove it with: filedo vd auto off %s  (an uninstall of FileDO does not remove it).\n", e.Name)
	res, err := vdRunElevated("_task", vdRequest{TaskName: vdTaskName(e.Name), TaskXML: taskXML}, batch, nil)
	if err != nil {
		return err
	}
	_ = res
	vdLogf("auto: created task %s for %s (%s), command %s --no-history vd mount %s", vdTaskName(e.Name), e.Name, e.Path, exe, e.Name)
	fmt.Printf("Created. %s will mount at your next logon.\n", e.Name)
	return nil
}

func vdAutoOff(name string, batch bool) error {
	if !vdHasTask(name) {
		return vdUsagef("%s has no automatic mount (see: filedo vd list)", name)
	}
	if _, err := vdRunElevated("_task", vdRequest{TaskName: vdTaskName(name), TaskDelete: true}, batch, nil); err != nil {
		return err
	}
	vdLogf("auto: removed task %s", vdTaskName(name))
	fmt.Printf("Removed the automatic mount of %s. The container and its registration are unchanged.\n", name)
	return nil
}

// vdTaskStep is the elevated half: it registers or deletes the task, and
// nothing else.
func vdTaskStep(req vdRequest) (vdResult, error) {
	var res vdResult
	if !strings.HasPrefix(req.TaskName, vdTaskName("")) {
		return res, vdUsagef("refusing a task outside %s: %s", vdTaskFolder, req.TaskName)
	}
	var cmd *exec.Cmd
	if req.TaskDelete {
		cmd = exec.Command(vdSchtasks(), "/Delete", "/TN", req.TaskName, "/F")
	} else {
		f, err := os.CreateTemp("", "filedo-task-*.xml")
		if err != nil {
			return res, err
		}
		defer os.Remove(f.Name())
		// schtasks reads task XML as UTF-16 with a byte-order mark.
		u := utf16.Encode([]rune(req.TaskXML))
		buf := []byte{0xFF, 0xFE}
		for _, c := range u {
			buf = append(buf, byte(c), byte(c>>8))
		}
		f.Write(buf)
		f.Close()
		cmd = exec.Command(vdSchtasks(), "/Create", "/TN", req.TaskName, "/XML", f.Name())
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	vdLogf("_task %s delete=%v: %s", req.TaskName, req.TaskDelete, text)
	if err != nil {
		return res, fmt.Errorf("schtasks failed (%v): %s", err, text)
	}
	return res, nil
}

func vdCurrentUserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

func vdTaskXML(name, path, exe, sid string) string {
	esc := func(s string) string {
		var b strings.Builder
		xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	dir := filepath.Dir(exe)
	if dir == "." {
		dir = os.Getenv("USERPROFILE")
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>FileDO</Author>
    <Description>Mounts the FileDO container ` + esc(name) + ` (` + esc(path) + `) at your logon. Created by: filedo vd auto ` + esc(name) + ` logon. Remove with: filedo vd auto off ` + esc(name) + `</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + esc(sid) + `</UserId>
      <Delay>PT20S</Delay>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + esc(sid) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT10M</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + esc(exe) + `</Command>
      <Arguments>--no-history vd mount ` + esc(name) + `</Arguments>
      <WorkingDirectory>` + esc(dir) + `</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`
}
