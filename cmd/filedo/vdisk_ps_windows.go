package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"filedo/vdisk"

	"golang.org/x/sys/windows"
)

// vdPowerShellTimeout bounds one script run: a format or a mount of a large
// image takes seconds to a few minutes, and a script that never returns (an
// image on a dead share) must not hold a stop for ever. A variable so a test
// can shorten it.
var vdPowerShellTimeout = 10 * time.Minute

// vdPowerShellMaxScript is what a script may take of the 32767-character
// command line, leaving room for the executable path and the switches.
const vdPowerShellMaxScript = 30000

// psQuote is s as a PowerShell single-quoted string literal. PowerShell treats
// U+2018..U+201B as quote characters too, so each of them is doubled like the
// ASCII apostrophe: a path holding one is data, never the end of the literal
// (AUD-34-F4, AUD-31-F3).
func psQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '‘', '’', '‚', '‛':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// psDataExpr transfers a value through an ASCII-only encoded literal. Paths
// passed to an elevated script never become PowerShell source text.
func psDataExpr(s string) string {
	u := utf16.Encode([]rune(s))
	raw := make([]byte, 0, len(u)*2)
	for _, c := range u {
		raw = append(raw, byte(c), byte(c>>8))
	}
	return "[Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('" + base64.StdEncoding.EncodeToString(raw) + "'))"
}

// vdPowerShell runs script in Windows PowerShell and returns its output.
//
// The script goes in as the one argument of -Command, one unit: a `throw` (or
// any error under $ErrorActionPreference = 'Stop') ends the whole script and the
// process exits non-zero. Piping it to `-Command -` ran every line as a command
// of its own, so a guard that threw ended only its own line and the destructive
// line after it still ran, and a failing step was hidden by a last line that
// could not fail (AUD-34-F1, AUD-34-F2). The command line is UTF-16, so a
// non-ASCII path arrives as it is whatever the console code page, and the
// output is read back as UTF-8. The run ends with the run's stop or the
// timeout, whichever comes first.
//
// There is no -ExecutionPolicy and no -EncodedCommand: the policy governs script
// files, which this never runs, and the in-box Storage cmdlets load under
// Restricted and AllSigned alike. Both switches are what malware passes, and
// antivirus engines weigh them so (the winget validation of the builds that
// first carried them was blocked by Defender).
func vdPowerShell(script string) (string, error) {
	ctx := context.Background()
	if globalInterruptHandler != nil {
		ctx = globalInterruptHandler.Context()
	}
	return vdPowerShellCtx(ctx, script, vdPowerShellTimeout)
}

// errVdScriptStopped is a script ended by the run's stop: vdisk.ErrStopped for
// the vd exit class and errRunStopped for the run (AUD-34-F2).
var errVdScriptStopped error = vdScriptStopped{}

type vdScriptStopped struct{}

func (vdScriptStopped) Error() string { return "stopped by request" }
func (vdScriptStopped) Is(target error) bool {
	return target == vdisk.ErrStopped || target == errRunStopped
}

func vdPowerShellCtx(ctx context.Context, script string, timeout time.Duration) (string, error) {
	wrapped := "$ErrorActionPreference = 'Stop'\n$ProgressPreference = 'SilentlyContinue'\n" +
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" +
		"try {\n" + script + "\n} catch {\n" +
		"[Console]::Error.WriteLine($_.Exception.Message)\nexit 1\n}\n"
	if len(utf16.Encode([]rune(wrapped))) > vdPowerShellMaxScript {
		return "", fmt.Errorf("the PowerShell script is too long for a command line (%d characters, limit %d)", len(wrapped), vdPowerShellMaxScript)
	}

	sys, _ := windows.GetSystemDirectory()
	ps := filepath.Join(sys, `WindowsPowerShell\v1.0\powershell.exe`)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, ps, "-NoProfile", "-NonInteractive", "-OutputFormat", "Text", "-Command", wrapped)
	out, err := cmd.CombinedOutput()
	text := strings.ReplaceAll(string(out), "\r", "")
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			// The stop class, not an I/O failure (AUD-34-F2); errRunStopped for the run.
			return text, errVdScriptStopped
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return text, fmt.Errorf("Windows PowerShell did not finish within %v", timeout)
		}
	}
	return text, err
}
