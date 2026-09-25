package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// The `from` batch: a `.lst` file of FileDO command lines, run in order in one
// process (one event stream, one result).
//
// Every line goes through dispatchLine - the same dispatch as a typed command
// - so a verb cannot work interactively and fail from a file. There is no
// "run any program" path any more: a line that is not a FileDO command is a
// usage error, never an exec of whatever the first word names (CLI-13).

// maxBatchDepth bounds `from` inside `from` (CLI-15).
const maxBatchDepth = 16

// batchOpen is the set of batch files being run right now, so a file that
// includes itself, directly or through another, is refused instead of
// recursing until the handles run out (CLI-15).
var (
	batchOpen  = map[string]bool{}
	batchDepth int
)

func executeFromFile(filePath string, historyLogger *HistoryLogger) error {
	historyLogger.SetCommand("from", filePath, "batch")

	abs, err := filepath.Abs(filePath)
	if err != nil {
		abs = filePath
	}
	key := strings.ToLower(filepath.Clean(abs))
	if batchOpen[key] {
		return usagef("%s is already running as a batch (it includes itself); refusing to run it again", filePath)
	}
	if batchDepth >= maxBatchDepth {
		return usagef("batch files nest more than %d deep; refusing %s", maxBatchDepth, filePath)
	}
	batchOpen[key] = true
	batchDepth++
	defer func() {
		delete(batchOpen, key)
		batchDepth--
	}()

	raw, err := os.ReadFile(filePath)
	if err != nil {
		historyLogger.SetError(fmt.Errorf("failed to open file: %w", err))
		return fmt.Errorf("cannot open file '%s': %w", filePath, err)
	}
	lines, err := batchLines(raw)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filePath, err)
	}

	commandCount := 0
	successCount := 0
	failed := 0
	skipped := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// No line starts after a stop - the stop file, Ctrl+C, the GUI's Stop.
		// A verb that never polls the handler (wipe, secure, probe) would
		// otherwise run whole after the user asked to stop (CLI-04).
		if runStopRequested() {
			for _, rest := range lines[i:] {
				if r := strings.TrimSpace(rest); r != "" && !strings.HasPrefix(r, "#") {
					skipped++
				}
			}
			break
		}
		args := splitBatchLine(line)
		if len(args) > 0 && isFiledoProgramWord(args[0]) {
			args = args[1:]
		}
		if len(args) == 0 {
			continue
		}
		commandCount++
		// The echo repeats the command line, so the same redaction as history
		// applies (invariant 8: no credential on the console either).
		fmt.Printf("\n[%d] Executing: %s\n", commandCount, strings.Join(redactCredentialArgs(args), " "))

		if err := executeInternalCommand(args); err != nil {
			failed++
			fmt.Printf("Command %d failed\n", commandCount)
		} else {
			successCount++
		}
	}

	if skipped > 0 {
		fmt.Printf("\nStopped: %d of the remaining command lines were not run.\n", skipped)
		historyLogger.SetResult("skippedCommands", skipped)
	}
	fmt.Printf("\nBatch execution complete: %d/%d commands succeeded\n", successCount, commandCount)

	historyLogger.SetResult("totalCommands", commandCount)
	historyLogger.SetResult("successfulCommands", successCount)

	if failed > 0 {
		// The lines recorded their own failures; this sentence is for the
		// history entry of the batch as a whole.
		historyLogger.SetError(fmt.Errorf("batch execution failed: %d out of %d commands failed", failed, commandCount))
		return nil
	}
	historyLogger.SetSuccess()
	return nil
}

// batchLines decodes a batch file: UTF-8 with or without a byte-order mark,
// or UTF-16 (little- or big-endian) with one - the default of PowerShell 5.1's
// Out-File (CLI-14).
func batchLines(raw []byte) ([]string, error) {
	var text string
	switch {
	case bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}):
		text = string(raw[3:])
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}), bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		be := raw[0] == 0xFE
		body := raw[2:]
		if len(body)%2 != 0 {
			return nil, fmt.Errorf("a UTF-16 file with an odd number of bytes")
		}
		u := make([]uint16, len(body)/2)
		for i := range u {
			if be {
				u[i] = uint16(body[2*i])<<8 | uint16(body[2*i+1])
			} else {
				u[i] = uint16(body[2*i+1])<<8 | uint16(body[2*i])
			}
		}
		text = string(utf16.Decode(u))
	default:
		text = string(raw)
	}
	text = strings.TrimPrefix(text, "\ufeff")
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), nil
}

// splitBatchLine splits one line by the rules cmd.exe hands a program its
// arguments with (CommandLineToArgvW / the MSVC runtime): whitespace separates;
// double quotes group and are removed; 2n backslashes before a quote become n
// and the quote is special, 2n+1 become n and a literal quote; a doubled quote
// inside quotes is a literal quote; any other backslash is literal. So a path
// with spaces can be written, and `p:"hunter2"` seals `hunter2`, exactly as
// the same line typed at a prompt (CLI-14).
func splitBatchLine(line string) []string {
	var args []string
	var cur strings.Builder
	inArg, inQuote := false, false
	r := []rune(line)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(r) && r[i] == '\\' {
				n++
				i++
			}
			if i < len(r) && r[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteRune('"')
				} else {
					inQuote = !inQuote
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			inArg = true
		case c == '"':
			if inQuote && i+1 < len(r) && r[i+1] == '"' {
				cur.WriteRune('"')
				i++
			} else {
				inQuote = !inQuote
			}
			inArg = true
		case (c == ' ' || c == '\t') && !inQuote:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

// isFiledoProgramWord recognises a leading program name on a batch line -
// `filedo`, `filedo.exe`, `.\filedo.exe`, `C:\Tools\FileDO.EXE` - compared by
// base name and without regard to case (CLI-14).
func isFiledoProgramWord(w string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(w, "/", `\`)))
	return base == "filedo" || base == "filedo.exe"
}
