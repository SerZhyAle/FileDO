package main

// The front of the app commands (APP-COMMANDS 0.1). filedo.exe is the second
// process of rule 9: for a verb of the manifest it decides its role before it
// loads anything, carries the command to the running filedo_win.exe over the
// command pipe, waits for the answer inside the bounds of rule 8 and exits with
// the answer's exit code. The grammar of a forwarded command is the first
// instance's: this side forwards `command` and the raw `args` (rule 7) and
// parses them itself only where it answers on its own - `commands`, `version`,
// and a cold start - with the same manifest-driven reading the first instance
// uses.
//
// The manifest (cli-commands.json, the byte copy of docs/cli-commands.json that
// a test holds equal, case C1) decides which first words are verbs. They win
// over a path probe: a folder named `status` no longer shadows the verb. Every
// other first word keeps the target-first grammar of dispatchLine unchanged,
// and the frozen aliases of `show` (ui, gui, -ui, the empty argument list) keep
// the start they always made.
//
// An app command is not a run: no banner, no history line, no events file, no
// result of outcome.go. Like the internal steps of a virtual-disk mount it
// leaves main() through its own exit code.

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed cli-commands.json
var appCmdManifestBytes []byte

// appCmdSchemaVersion is the request and response version of APP-COMMANDS 0.1.
const appCmdSchemaVersion = 1

// The bounds of rule 3 and rule 8.
const (
	appCmdMaxRequestBytes = 64 * 1024
	appCmdMaxArgBytes     = 32 * 1024
	appCmdDeliveryBound   = 1000 * time.Millisecond
	appCmdDefaultTimeout  = 10000
	appCmdMaxTimeout      = 600000
)

type appCmdAlias struct {
	Spelling string            `json:"spelling"`
	Exe      string            `json:"exe,omitempty"`
	Maps     map[string]string `json:"maps,omitempty"`
	Implies  []string          `json:"implies,omitempty"`
	Legacy   bool              `json:"legacy,omitempty"`
	Answers  *bool             `json:"answers,omitempty"`
}

func (a *appCmdAlias) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		*a = appCmdAlias{Spelling: s}
		return nil
	}
	type raw appCmdAlias
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*a = appCmdAlias(r)
	return nil
}

func (a appCmdAlias) AnswersCaller() bool {
	return a.Answers == nil || *a.Answers
}

type appCmdFamily struct {
	Name          string `json:"name"`
	Summary       string `json:"summary"`
	Exe           string `json:"exe"`
	Help          string `json:"help"`
	ExitContract  string `json:"exitContract"`
	EventContract string `json:"eventContract"`
}

type appCmdArg struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required"`
	Values   []string `json:"values"`
}

type appCmdEntry struct {
	Name       string        `json:"name"`
	Aliases    []appCmdAlias `json:"aliases"`
	Summary    string        `json:"summary"`
	Synopsis   string        `json:"synopsis"`
	Args       []appCmdArg   `json:"args"`
	Risk       string        `json:"risk"`
	Audience   string        `json:"audience"`
	Forward    string        `json:"forward"`
	DefaultFor string        `json:"defaultFor,omitempty"`
	ColdStart  string        `json:"coldStart"`
	Wait       string        `json:"wait"`
	TimeoutMs  int           `json:"timeoutMs"`
	Exe        string        `json:"exe"`
}

func (e *appCmdEntry) isForwardNever() bool {
	return strings.EqualFold(e.Forward, "never")
}

func (e *appCmdEntry) isDeveloper() bool {
	return strings.EqualFold(e.Audience, "developer")
}

func (e *appCmdEntry) hasLegacyStart() bool {
	return e.Name == "show" || e.Name == "disk-manager show"
}

type appCmdManifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	Product       string         `json:"product"`
	Exe           string         `json:"exe"`
	CliVersion    string         `json:"cliVersion"`
	Families      []appCmdFamily `json:"families"`
	Commands      []appCmdEntry  `json:"commands"`
}

// appCmdRequest and appCmdResponse are the two lines of rule 7.
type appCmdRequest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Command       string   `json:"command"`
	Args          []string `json:"args"`
	WorkingDir    string   `json:"workingDir,omitempty"`
	Origin        string   `json:"origin"`
	RequestID     string   `json:"requestId"`
	Wait          string   `json:"wait"`
	Legacy        bool     `json:"legacy,omitempty"`
}

type appCmdResponse struct {
	SchemaVersion int                    `json:"schemaVersion"`
	RequestID     string                 `json:"requestId"`
	Status        string                 `json:"status"`
	ExitCode      int                    `json:"exitCode"`
	Reason        string                 `json:"reason,omitempty"`
	Message       string                 `json:"message,omitempty"`
	Data          map[string]interface{} `json:"data,omitempty"`
}

// The seams the tests swap: where the pipe is, how the window program is
// started, where the answer is printed.
var (
	appCmdPipePath           = appCmdDefaultPipePath
	appCmdLaunch             = func(args ...string) error { return uiLauncher(args...) }
	appCmdOut      io.Writer = os.Stdout
	appCmdErr      io.Writer = os.Stderr
)

var appCmdManifestCache *appCmdManifest

func loadAppCmdManifest() (*appCmdManifest, error) {
	if appCmdManifestCache != nil {
		return appCmdManifestCache, nil
	}
	var m appCmdManifest
	if err := json.Unmarshal(appCmdManifestBytes, &m); err != nil {
		return nil, err
	}
	appCmdManifestCache = &m
	return appCmdManifestCache, nil
}

// appCommandOf reports which manifest verb the command line starts with and
// the arguments after its words. A verb is one or two words (rule 1), compared
// without regard to case; the longest match wins. Aliases are not matched here:
// an alias keeps the start it always made.
func appCommandOf(args []string) (*appCmdEntry, []string) {
	if len(args) == 0 {
		return nil, nil
	}
	m, err := loadAppCmdManifest()
	if err != nil {
		return nil, nil
	}
	var best *appCmdEntry
	bestWords := 0
	for i := range m.Commands {
		if !appCmdServedHere(&m.Commands[i], m) {
			continue
		}
		words := strings.Fields(m.Commands[i].Name)
		if len(words) == 0 || len(words) > len(args) || len(words) <= bestWords {
			continue
		}
		match := true
		for j, w := range words {
			if !strings.EqualFold(args[j], w) {
				match = false
				break
			}
		}
		if match {
			best = &m.Commands[i]
			bestWords = len(words)
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, args[bestWords:]
}

// appCmdServedHere reports whether filedo.exe is the front of a verb: the entry's
// `exe` (APP-COMMANDS rule 18; the product's when the entry names none) is
// filedo.exe. A verb of another executable - the developer tooling of the window
// program (SP-0219) - is neither reserved here nor forwarded, so `filedo screens
// capture` and a folder named `screens` keep the target-first reading they had.
func appCmdServedHere(e *appCmdEntry, m *appCmdManifest) bool {
	exe := e.Exe
	if exe == "" {
		exe = m.Exe
	}
	return strings.EqualFold(exe, "filedo.exe")
}

// isAppCommandWord reports whether a first word is a verb of the manifest. The
// batch path refuses such a line rather than reading it as a target.
func isAppCommandWord(word string) bool {
	e, _ := appCommandOf([]string{word})
	return e != nil && len(strings.Fields(e.Name)) == 1
}

// appCmdOptions is what the manifest-driven reading of the arguments found.
type appCmdOptions map[string]string

// parseAppCmdArgs reads the arguments of one verb against its manifest entry,
// the way the first instance does (rule 1, rule 3): `--name`, `--name=value`
// or `--name value`, names without regard to case, values exactly as typed,
// `--` ends the options. No verb of this manifest takes a bare value.
func parseAppCmdArgs(e *appCmdEntry, args []string) (appCmdOptions, string) {
	out := appCmdOptions{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return nil, fmt.Sprintf("%s takes no value; remove %q", e.Name, args[i+1])
			}
			break
		}
		if !strings.HasPrefix(a, "--") || len(a) == 2 {
			return nil, fmt.Sprintf("%s takes no value; remove %q", e.Name, a)
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		var spec *appCmdArg
		for j := range e.Args {
			if strings.EqualFold(e.Args[j].Name, name) {
				spec = &e.Args[j]
				break
			}
		}
		if spec == nil {
			return nil, fmt.Sprintf("--%s is not an option of %s; see filedo commands", name, e.Name)
		}
		key := strings.ToLower(spec.Name)
		if _, dup := out[key]; dup {
			return nil, fmt.Sprintf("--%s is given twice", spec.Name)
		}
		if spec.Kind == "bool" {
			if !hasValue {
				value = "true"
			} else if value != "true" && value != "false" {
				return nil, fmt.Sprintf("--%s takes true or false, not %q", spec.Name, value)
			}
			out[key] = value
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, fmt.Sprintf("--%s needs a value", spec.Name)
			}
			i++
			value = args[i]
		}
		switch spec.Kind {
		case "int":
			if _, err := strconv.Atoi(value); err != nil {
				return nil, fmt.Sprintf("--%s takes a whole number, not %q", spec.Name, value)
			}
		case "enum":
			ok := false
			for _, v := range spec.Values {
				if v == value {
					ok = true
				}
			}
			if !ok {
				return nil, fmt.Sprintf("--%s takes one of %s, not %q", spec.Name, strings.Join(spec.Values, ", "), value)
			}
		}
		out[key] = value
	}
	for _, spec := range e.Args {
		if _, given := out[strings.ToLower(spec.Name)]; spec.Required && !given {
			return nil, fmt.Sprintf("%s needs --%s", e.Name, spec.Name)
		}
	}
	return out, ""
}

// wantsJSON is the output switch of rule 9: the response line instead of its
// message. It is read from the raw arguments, so a forwarded command still
// prints its line when the first instance is the one that parsed it.
func wantsJSON(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if strings.EqualFold(a, "--json") || strings.EqualFold(a, "--json=true") {
			return true
		}
	}
	return false
}

// appCommandMain answers one app command and returns its exit code.
func appCommandMain(e *appCmdEntry, args []string) int {
	jsonOut := wantsJSON(args)
	reqID := newRequestID()
	refuse := func(status string, code int, reason, message string) int {
		return printAppCmdResponse(appCmdResponse{SchemaVersion: appCmdSchemaVersion, RequestID: reqID, Status: status, ExitCode: code, Reason: reason, Message: message}, jsonOut)
	}

	// Rule 3: bounded input, refused and never truncated.
	total := 0
	for _, a := range args {
		if len(a) > appCmdMaxArgBytes {
			return refuse("refused", 10, "too-large", fmt.Sprintf("One argument is longer than %d KiB; FileDO takes at most that much.", appCmdMaxArgBytes/1024))
		}
		total += len(a) + 4
	}
	if total+len(e.Name)+256 > appCmdMaxRequestBytes {
		return refuse("refused", 10, "too-large", fmt.Sprintf("The command is longer than %d KiB; FileDO takes at most that much.", appCmdMaxRequestBytes/1024))
	}

	switch e.Name {
	case "commands":
		opts, problem := parseAppCmdArgs(e, args)
		if problem != "" {
			return refuse("refused", 10, "bad-argument", problem)
		}
		return printAppCmdList(jsonOut, opts["developer"] == "true")
	case "version":
		if _, problem := parseAppCmdArgs(e, args); problem != "" {
			return refuse("refused", 10, "bad-argument", problem)
		}
		m, _ := loadAppCmdManifest()
		data := map[string]interface{}{"product": m.Product, "version": version, "cliVersion": m.CliVersion}
		return printAppCmdResponse(appCmdResponse{SchemaVersion: appCmdSchemaVersion, RequestID: reqID, Status: "done", ExitCode: 0,
			Message: fmt.Sprintf("%s %s, commands %s", m.Product, version, m.CliVersion), Data: data}, jsonOut)
	}
	if e.isForwardNever() {
		return refuse("refused", 10, "unknown-verb", fmt.Sprintf("%s runs in its own process and is not forwarded.", e.Name))
	}

	req := appCmdRequest{SchemaVersion: appCmdSchemaVersion, Command: e.Name, Args: append([]string{}, args...), Origin: "cli", RequestID: reqID, Wait: "done"}
	if wd, err := os.Getwd(); err == nil {
		req.WorkingDir = wd
	}
	if req.Args == nil {
		req.Args = []string{}
	}
	line, err := json.Marshal(req)
	if err != nil || len(line)+1 > appCmdMaxRequestBytes {
		return refuse("refused", 10, "too-large", fmt.Sprintf("The command is longer than %d KiB; FileDO takes at most that much.", appCmdMaxRequestBytes/1024))
	}

	timeout := appCmdTimeout(e)
	resp, state := appCmdExchange(line, reqID, timeout)
	switch state {
	case appCmdAnswered:
		return printAppCmdResponse(resp, jsonOut)
	case appCmdBusy:
		return refuse("busy", 11, "busy", "FileDO is busy with another command; try again in a moment.")
	case appCmdElevated:
		return refuse("refused", 12, "elevation", "FileDO runs as administrator and this command does not; run it from an elevated console, or restart FileDO normally.")
	case appCmdOlderCopy:
		if e.hasLegacyStart() {
			_ = appCmdLaunch(appCmdStartArgs(e.Name)...)
		}
		return refuse("failed", 2, "timeout", "The running FileDO is an older copy that does not take commands; restart FileDO and try again.")
	case appCmdNoAnswer:
		return refuse("failed", 2, "timeout", fmt.Sprintf("FileDO did not answer within %d seconds; it may still be working.", (timeout+999)/1000))
	case appCmdUnreadable:
		return refuse("failed", 2, "timeout", "FileDO answered with something this filedo.exe cannot read; restart FileDO and try again.")
	}

	// No first instance: the verb's coldStart decides (rule 10).
	if _, problem := parseAppCmdArgs(e, args); problem != "" {
		return refuse("refused", 10, "bad-argument", problem)
	}
	switch e.ColdStart {
	case "refuse":
		return refuse("refused", 11, "not-running", "FileDO is not running.")
	case "oneshot":
		// `quit` and `job stop` are the oneshot verbs that go to the first
		// instance: with none running each is done with nothing to do (rule 17).
		return printAppCmdResponse(coldOneshotAnswer(e.Name, reqID), jsonOut)
	case "start":
		if err := appCmdLaunch(appCmdStartArgs(e.Name)...); err != nil {
			return refuse("failed", 11, "target-missing", "filedo_win.exe was not found beside filedo.exe; install the FileDO window program.")
		}
		deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
		for time.Now().Before(deadline) {
			resp, state = appCmdExchange(line, reqID, int(time.Until(deadline)/time.Millisecond))
			switch state {
			case appCmdAnswered:
				return printAppCmdResponse(resp, jsonOut)
			case appCmdElevated:
				return refuse("refused", 12, "elevation", "FileDO runs as administrator and this command does not; run it from an elevated console, or restart FileDO normally.")
			case appCmdOlderCopy:
				return refuse("failed", 2, "timeout", "The running FileDO is an older copy that does not take commands; restart FileDO and try again.")
			case appCmdNoAnswer, appCmdUnreadable:
				return refuse("failed", 2, "timeout", fmt.Sprintf("FileDO did not answer within %d seconds; it may still be starting.", (timeout+999)/1000))
			}
			time.Sleep(100 * time.Millisecond)
		}
		return refuse("failed", 2, "timeout", fmt.Sprintf("FileDO did not start within %d seconds; it may still be starting.", (timeout+999)/1000))
	}
	return refuse("refused", 10, "unknown-verb", fmt.Sprintf("%s has no cold-start mode in the manifest of this filedo.exe.", e.Name))
}

// coldOneshotAnswer is the answer of a oneshot verb that asks the first instance
// when none is running: done, with nothing to do. `job stop` says so in its own
// words and carries the counts of its manifest (`stopped`, `cleared`), both 0,
// so a script that reads them needs no special case.
func coldOneshotAnswer(verb, reqID string) appCmdResponse {
	resp := appCmdResponse{SchemaVersion: appCmdSchemaVersion, RequestID: reqID, Status: "done", ExitCode: 0,
		Message: "FileDO is not running; there is nothing to close."}
	if verb == "job stop" {
		resp.Message = "FileDO is not running; there is nothing to stop."
		resp.Data = map[string]interface{}{"stopped": 0, "cleared": 0}
	}
	return resp
}

// appCmdTimeout is rule 8's completion bound for a verb.
func appCmdTimeout(e *appCmdEntry) int {
	t := e.TimeoutMs
	if t <= 0 {
		t = appCmdDefaultTimeout
	}
	if t > appCmdMaxTimeout {
		t = appCmdMaxTimeout
	}
	return t
}

// appCmdStartArgs is what a cold start launches filedo_win.exe with: the spelling
// of the verb's own start, so no first instance means the window program the verb
// asks for. disk-manager show starts the Disk Manager alone (--disks, as the Start
// menu entry does); tray start starts hidden with its tray icon (--startup --tray,
// as the Run key does); show and settings show start bare, and the request then
// brings the main window forward on the page it asks for.
func appCmdStartArgs(verb string) []string {
	switch verb {
	case "disk-manager show":
		return []string{"--disks"}
	case "tray start":
		return []string{"--startup", "--tray"}
	}
	return nil
}

type appCmdState int

const (
	appCmdAnswered   appCmdState = iota
	appCmdNotRunning             // no pipe: nobody to ask
	appCmdBusy                   // the pipe is there and taken past the delivery bound
	appCmdElevated               // the pipe refused this caller (an elevated first instance)
	appCmdOlderCopy              // connection closed with no line, or first line is not JSON with schemaVersion (item K, C15)
	appCmdNoAnswer               // delivered, no answer within the completion bound
	appCmdUnreadable             // a response line whose requestId is empty or foreign (item I.3, C19)
)

// appCmdExchange carries one request line and reads one response line on the
// same connection (rule 7).
func appCmdExchange(line []byte, reqID string, timeoutMs int) (appCmdResponse, appCmdState) {
	var resp appCmdResponse
	conn, state := appCmdDial(appCmdPipePath())
	if conn == nil {
		return resp, state
	}
	defer conn.Close()
	allowForeground()
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return resp, appCmdNoAnswer
	}
	raw, err := readAppCmdLine(conn, appCmdMaxRequestBytes)
	if err != nil {
		if errors.Is(err, errAppCmdTooLong) {
			return resp, appCmdUnreadable
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return resp, appCmdNoAnswer
		}
		return resp, appCmdOlderCopy
	}
	if json.Unmarshal(raw, &resp) != nil || resp.SchemaVersion <= 0 {
		return resp, appCmdOlderCopy
	}
	if resp.RequestID != reqID || resp.Status == "" {
		return resp, appCmdUnreadable
	}
	return resp, appCmdAnswered
}

var errAppCmdTooLong = errors.New("line too long")

// readAppCmdLine reads up to the first newline, at most limit bytes.
func readAppCmdLine(r io.Reader, limit int) ([]byte, error) {
	var buf bytes.Buffer
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			if i := bytes.IndexByte(chunk[:n], '\n'); i >= 0 {
				buf.Write(chunk[:i])
				return bytes.TrimRight(buf.Bytes(), "\r"), nil
			}
			buf.Write(chunk[:n])
			if buf.Len() > limit {
				return nil, errAppCmdTooLong
			}
		}
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return bytes.TrimRight(buf.Bytes(), "\r"), nil
			}
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
}

// printAppCmdResponse prints rule 9's output - the message on standard output,
// or with --json the response line - and returns the exit code. A refusal's
// message goes to standard error as well as being the answer.
func printAppCmdResponse(resp appCmdResponse, jsonOut bool) int {
	if resp.SchemaVersion == 0 {
		resp.SchemaVersion = appCmdSchemaVersion
	}
	if jsonOut {
		line, _ := json.Marshal(resp)
		fmt.Fprintln(appCmdOut, string(line))
		return resp.ExitCode
	}
	if resp.Message != "" {
		if resp.Status == "done" || resp.Status == "accepted" {
			fmt.Fprintln(appCmdOut, resp.Message)
		} else {
			fmt.Fprintln(appCmdErr, resp.Message)
		}
	}
	if len(resp.Data) > 0 && (resp.Status == "done" || resp.Status == "accepted") {
		keys := make([]string, 0, len(resp.Data))
		for k := range resp.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "jobs" {
				printAppCmdJobs(resp.Data[k])
				continue
			}
			fmt.Fprintf(appCmdOut, "  %s: %v\n", k, resp.Data[k])
		}
	}
	return resp.ExitCode
}

// printAppCmdJobs prints the `jobs` array of `job list` one run to a line: where,
// verb, state, then the step and the percent when the run has said them.
func printAppCmdJobs(v interface{}) {
	list, _ := v.([]interface{})
	for _, item := range list {
		j, _ := item.(map[string]interface{})
		where, _ := j["where"].(string)
		verb, _ := j["verb"].(string)
		state, _ := j["state"].(string)
		line := fmt.Sprintf("  %-8s %-12s %-8s", where, verb, state)
		if step, _ := j["step"].(string); step != "" {
			line += " " + step
		}
		if pct, ok := j["percent"].(float64); ok {
			line += fmt.Sprintf(" %d%%", int(pct))
		}
		fmt.Fprintln(appCmdOut, strings.TrimRight(line, " "))
	}
}

// printAppCmdList is `commands`: the embedded manifest as it ships, or one
// readable line per verb (rule 17), hiding developer verbs unless --developer
// is given (item D) and listing families underneath (item B).
func printAppCmdList(jsonOut, showDeveloper bool) int {
	if jsonOut {
		_, _ = appCmdOut.Write(appCmdManifestBytes)
		if !bytes.HasSuffix(appCmdManifestBytes, []byte("\n")) {
			fmt.Fprintln(appCmdOut)
		}
		return 0
	}
	m, _ := loadAppCmdManifest()
	fmt.Fprintf(appCmdOut, "%s commands %s - every command answers with an exit code; add --json for the answer as JSON.\n\n", m.Product, m.CliVersion)
	for _, c := range m.Commands {
		if c.isDeveloper() {
			continue
		}
		summary := c.Summary
		if c.DefaultFor != "" {
			summary += fmt.Sprintf(" (default for %s, or just a path)", c.DefaultFor)
		}
		fmt.Fprintf(appCmdOut, "  %-38s %s\n", c.Synopsis, summary)
	}
	if showDeveloper {
		hasDev := false
		for _, c := range m.Commands {
			if !c.isDeveloper() {
				continue
			}
			if !hasDev {
				fmt.Fprintln(appCmdOut, "\nDeveloper tools (run by filedo_win.exe in its own process):")
				hasDev = true
			}
			fmt.Fprintf(appCmdOut, "  %-48s %s\n", c.Synopsis, c.Summary)
		}
	}
	if len(m.Families) > 0 {
		fmt.Fprintln(appCmdOut, "\nCommand families (not forwarded; target-first and container grammars):")
		for _, f := range m.Families {
			fmt.Fprintf(appCmdOut, "  %-14s %s (help: %s; exit: %s)\n", f.Name, f.Summary, f.Help, f.ExitContract)
		}
	}
	return 0
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano()&0xffffffffffff)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
