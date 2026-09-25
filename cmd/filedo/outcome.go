package main

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// The run outcome: the one place where what a verb found becomes a verdict, a
// `result` event and a process exit code.
//
// Two contracts meet here and they must not be confused with each other.
// CLI-EVENT-STREAM rule 10 says the verdict is the `result` event and never
// the exit code alone, and rule 11 fixes the supervisor vocabulary at three
// digits - 0 the operation ran and passed, 1 it ran and found a defect, 2 it
// could not be verified. FDSEC-BEHAVIOUR section 7.1 gives the container verbs
// their own classes, where 2 means a usage error and not "unverified"; rule 12
// is what bridges the seam, and it bridges it through the `result` event,
// which is why those verbs carry one and keep their own digits.
//
// Everything a verb does is a recording - beginRun, runStep, runDefect,
// runFailure, runNumber. Nothing outside finishRun decides a verdict and
// nothing outside setExitCode writes the process code, so the vocabulary has
// exactly one site to read and exactly one site to change.

// Verdict is the closed word list of CLI-EVENT-STREAM rule 10.
type Verdict string

const (
	VerdictPassed    Verdict = "Passed"
	VerdictFailed    Verdict = "Failed"
	VerdictDone      Verdict = "Done"
	VerdictStopped   Verdict = "Stopped"
	VerdictNotProven Verdict = "Not proven"
)

// errDefect marks an error that is a judgement about the target rather than a
// failure to reach one. A fake-capacity test that concluded the device lies is
// a defect and exits 1; a test that could not open the device at all proved
// nothing and exits 2. Without the distinction both are "an error" and the
// caller cannot tell the two apart - which is the whole of rule 11.
var errDefect = errors.New("defect found")

// defectf wraps a message as a defect. It reads like fmt.Errorf on purpose:
// the call sites are the lines that already printed "TEST FAILED".
func defectf(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", errDefect, fmt.Sprintf(format, args...))
}

// recordedDefectError is a defect the verb has already recorded with its own
// finding type and details (recordedDefect). It still satisfies
// errors.Is(err, errDefect), so every caller reads it as a defect; runFailure
// alone knows not to record it again.
type recordedDefectError struct{ error }

func (e recordedDefectError) Unwrap() error { return e.error }

// recordedDefect records a finding of the given type and returns the defect
// error the verb hands back up.
func recordedDefect(findingType, message string, details map[string]interface{}) error {
	runDefect(findingType, message, details)
	return recordedDefectError{defectf("%s", message)}
}

// runKind separates a verb that answers a question about its target from one
// that acts on it. It is the difference between `Passed` and `Done`, and
// nothing else depends on it.
type runKind int

const (
	runActs runKind = iota
	runJudges
)

type runOutcome struct {
	mu        sync.Mutex
	begun     bool
	finished  bool
	kind      runKind
	defects   int
	notProven bool
	// failures counts every runFailure; a batch compares it before and after
	// a line to know whether that line failed (CLI-12).
	failures int
	numbers   map[string]interface{}
	filesLeft []string
	reports   []string
}

var currentRun = &runOutcome{numbers: make(map[string]interface{})}

// beginRun opens the run: the first call emits `run`, every later one emits a
// `step`. The second shape is the batch path - `filedo from list.lst` runs many
// commands inside one process, and one process is one stream whose `run` comes
// first and whose `result` comes last (rule 9).
func beginRun(kind runKind, command, target string, args []string) {
	currentRun.mu.Lock()
	defer currentRun.mu.Unlock()

	if currentRun.begun {
		if kind == runJudges {
			currentRun.kind = runJudges
		}
		EmitStepEvent(command, target)
		return
	}
	currentRun.begun = true
	currentRun.kind = kind
	EmitRunEvent(version, command, target, args)
}

// runStep names a phase of the work - what a window shows above the bar.
func runStep(name, description string) {
	EmitStepEvent(name, description)
}

// runDefect records a judgement against the target: the run ran and the answer
// is no. It is what turns a verdict into `Failed` and an exit code into 1.
func runDefect(findingType, message string, details map[string]interface{}) {
	ensureRunForFinding()
	currentRun.mu.Lock()
	currentRun.defects++
	currentRun.mu.Unlock()
	EmitFindingEvent(findingType, message, details)
}

// runFailure records an error. A defect-marked error is a judgement and goes to
// runDefect; anything else means the run could not judge, which is `Not proven`
// and exit 2 - never a silent zero.
func runFailure(err error) {
	if err == nil {
		return
	}
	// A defect whose finding the verb already recorded, with its own type
	// and details, is not recorded a second time.
	var rd recordedDefectError
	if errors.As(err, &rd) {
		return
	}
	if errors.Is(err, errDefect) {
		runDefect("defect", err.Error(), nil)
		return
	}
	ensureRunForFinding()
	currentRun.mu.Lock()
	currentRun.notProven = true
	currentRun.failures++
	currentRun.mu.Unlock()
	EmitFindingEvent("error", eventSafeErrorMessage(err), nil)
}

// runProblemCount is how many defects and failures the run has recorded so
// far - what a batch compares across one line.
func runProblemCount() int {
	currentRun.mu.Lock()
	defer currentRun.mu.Unlock()
	return currentRun.defects + currentRun.failures
}

// ensureRunForFinding closes the old gap where a dispatch error could emit a
// lone finding (or only print to the console).  Rule 9 requires run first and
// rule 10 requires result last even when there was no useful work to do.
func ensureRunForFinding() {
	currentRun.mu.Lock()
	begun := currentRun.begun
	currentRun.mu.Unlock()
	if !begun {
		beginRun(runActs, "unknown", "", nil)
	}
}

// eventSafeErrorMessage is deliberately narrower than console/history error
// reporting.  A container's sealed filename is useful on the terminal, but
// CLI-EVENT-STREAM rule 13 says it may never enter the durable event log.
func eventSafeErrorMessage(err error) string {
	var screened *fdsecEventSafeError
	if errors.As(err, &screened) {
		return screened.eventMessage
	}
	return err.Error()
}

// runNumber adds one measurement to `result.numbers`, whose keys belong to the
// verb that filled it (the contract's section 5).
func runNumber(key string, value interface{}) {
	currentRun.mu.Lock()
	defer currentRun.mu.Unlock()
	currentRun.numbers[key] = value
}

// runFilesLeft names files the run deliberately did not remove - the test files
// kept as the evidence for an estimated-real-capacity report, above all.
func runFilesLeft(paths ...string) {
	currentRun.mu.Lock()
	defer currentRun.mu.Unlock()
	currentRun.filesLeft = append(currentRun.filesLeft, paths...)
}

// runReportFile names a report the run wrote.
func runReportFile(path string) {
	currentRun.mu.Lock()
	defer currentRun.mu.Unlock()
	currentRun.reports = append(currentRun.reports, path)
}

// verdict is the decision, and it is made here and nowhere else.
func (ro *runOutcome) verdict() Verdict {
	// A container verb's digits are FDSEC-BEHAVIOUR's, so its verdict is read
	// off those classes rather than off this function's counters (rule 12).
	if fdsecExitCode != 0 {
		return verdictForFdsecExit(fdsecExitCode)
	}
	// A stop is not a failure and it is not a result either: the run ended
	// before it could judge, so it says so (rule 15).
	if globalInterruptHandler != nil && globalInterruptHandler.IsInterrupted() {
		return VerdictStopped
	}
	// A proven defect outranks "could not verify": a batch that found a fake
	// on one line and could not reach a share on the next still found the
	// fake, and losing that answer is the failure mode rule 11 exists to
	// prevent (CLI-06).
	switch {
	case ro.defects > 0:
		return VerdictFailed
	case ro.notProven:
		return VerdictNotProven
	case ro.kind == runJudges:
		return VerdictPassed
	default:
		return VerdictDone
	}
}

// verdictForFdsecExit reads a container verb's exit class as a verdict. Wrong
// credential, tampering and damage are answers about the container - the verb
// ran and the answer is no. A usage error, an I/O failure and an unsupported
// version are not answers at all.
func verdictForFdsecExit(code int) Verdict {
	switch code {
	case fdsecExitCredentialOrTamper, fdsecExitDamaged:
		return VerdictFailed
	default:
		return VerdictNotProven
	}
}

// exitCodeFor is the supervisor vocabulary of rule 11, written once.
//
// `Stopped` exits 0, not 2. Rule 15 is explicit that a stop is not a failure,
// and the digit would be telling the one caller who already knows: whoever
// created the stop file asked for this ending. It is also the difference
// between a stop and an abort in this program - the reveal's "remove the copy
// now" is a stop, and it is the reveal finishing, not failing. What a
// supervisor reads is the `Stopped` verdict in the result event, which rule 15
// makes authoritative over the code anyway.
func exitCodeFor(v Verdict) int {
	switch v {
	case VerdictFailed:
		return 1
	case VerdictNotProven:
		return 2
	default: // Passed, Done, Stopped
		return 0
	}
}

// globalExitCode is what main() hands to os.Exit. setExitCode is its only
// writer and finishRun is setExitCode's only caller.
var globalExitCode int

// setExitCode keeps the worse of two answers: a defect found by one command of
// a batch is not erased by the next one passing, and a defect (1) outranks an
// unproven run (2), because "we looked and it is broken" says more than "we
// could not look".
func setExitCode(code int) {
	if globalExitCode == 0 || (globalExitCode == 2 && code == 1) {
		globalExitCode = code
	}
}

// finishRun closes the run: one `result` event, one exit code, once. main()
// defers it so that every path through the program ends here - including the
// ones that print an error and return, which is how a failed verb used to exit
// 0. A run that never began emits nothing at all: a verb this contract has not
// reached yet stays honestly silent, and the supervisor reports *Not proven*
// rather than being told something that was never measured (rule 10).
func finishRun() {
	currentRun.mu.Lock()
	if !currentRun.begun || currentRun.finished {
		currentRun.mu.Unlock()
		return
	}
	currentRun.finished = true
	v := currentRun.verdict()
	numbers := currentRun.numbers
	filesLeft := currentRun.filesLeft
	reports := currentRun.reports
	currentRun.mu.Unlock()

	EmitResultEvent(string(v), numbers, filesLeft, reports)

	// The container verbs keep their own classes; everything else gets the
	// three digits of rule 11.
	if fdsecExitCode != 0 {
		return
	}
	setExitCode(exitCodeFor(v))
}

// finishForcedRun is the second-Ctrl+C path.  os.Exit skips normal defers, so
// it cannot rely on finishRun to speak the channel vocabulary.  The outcome is
// not proven: the operation was abandoned before its own finishing code ran.
func finishForcedRun() int {
	ensureRunForFinding()

	currentRun.mu.Lock()
	if currentRun.finished {
		currentRun.mu.Unlock()
		return 2
	}
	currentRun.finished = true
	numbers := currentRun.numbers
	filesLeft := currentRun.filesLeft
	reports := currentRun.reports
	currentRun.mu.Unlock()

	EmitResultEvent(string(VerdictNotProven), numbers, filesLeft, reports)
	setExitCode(2)
	return 2
}

// runStopRequested reports whether the stop channel or a Ctrl+C has asked this
// run to end. Long loops already poll the interrupt handler; this is the same
// question asked in the words of the contract.
func runStopRequested() bool {
	return globalInterruptHandler != nil && globalInterruptHandler.IsInterrupted()
}

// usageFailure ends a run that never started work. The words were wrong, so
// nothing was measured and nothing is claimed: *Not proven*, exit 2. It opens
// the run first, because a stream with no `result` on the end of it is a run
// that said nothing at all (rule 10), and "you typed it wrong" is something.
func usageFailure(command string, args []string, format string, a ...interface{}) {
	beginRun(runActs, command, "", args)
	err := fmt.Errorf(format, a...)
	runFailure(err)
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

// reportRunError prints an error, records it in the history entry and lets it
// decide the verdict - the three things that used to be done by three separate
// lines of which the last one was missing.
func reportRunError(err error, historyLogger *HistoryLogger) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	historyLogger.SetError(err)
	runFailure(err)
}
