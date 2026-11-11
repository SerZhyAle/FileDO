package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"filedo/fileduplicates"
)

func TestInformativeExitClasses(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{errors.New("unknown"), 2},
		{exitClassf(10, "usage"), 10},
		{os.ErrNotExist, 11},
		{fmt.Errorf("wrapped: %w", errInvalidDrive), 11},
		{fmt.Errorf("wrapped: %w", errNotReady), 11},
		{fmt.Errorf("wrapped: %w", errAccessDenied), 12},
		{fmt.Errorf("wrapped: %w", errDiskFull), 13},
		{fmt.Errorf("wrapped: %w", errIODevice), 14},
		{fmt.Errorf("wrapped: %w", errDeviceNotConnected), 14},
		{fmt.Errorf("wrapped: %w", errBadNetPath), 15},
		{fmt.Errorf("wrapped: %w", errNetworkAccessDenied), 15},
		{fmt.Errorf("wrapped: %w", errSharingViolation), 2},
		{&fileduplicates.UsageError{Msg: "words"}, 10},
		{fmt.Errorf("wrapped: %w", &fileduplicates.UsageError{Msg: "words"}), 10},
	}
	for _, c := range cases {
		if got := failureExitCode(c.err); got != c.code {
			t.Errorf("%v: got %d, want %d", c.err, got, c.code)
		}
	}
	if got := exitCodeFor(VerdictStopped); got != 16 {
		t.Errorf("stopped: got %d, want 16", got)
	}
	for v, want := range map[Verdict]int{VerdictPassed: 0, VerdictDone: 0, VerdictFailed: 1, VerdictNotProven: 2} {
		if got := exitCodeFor(v); got != want {
			t.Errorf("%s: got %d, want %d", v, got, want)
		}
	}
}

// TestPreRunErrorSaysTheTargetIsAbsent is the device-absent-before-the-run
// half of the 11/14 split: a device-level errno met while looking for the
// target is the target not being there, and the same errno in the middle of an
// I/O stays hardware. The other classes keep their number.
func TestPreRunErrorSaysTheTargetIsAbsent(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{fmt.Errorf("stat: %w", errDeviceNotConnected), 11},
		{fmt.Errorf("stat: %w", errNotReady), 11},
		{fmt.Errorf("stat: %w", errNoMediaInDrive), 11},
		{errors.New("no errno at all"), 11},
		{fmt.Errorf("stat: %w", errAccessDenied), 12},
		{fmt.Errorf("stat: %w", errBadNetPath), 15},
		{exitClassf(10, "usage"), 10},
	}
	for _, c := range cases {
		if got := failureExitCode(preRunError(c.err)); got != c.code {
			t.Errorf("%v: got %d, want %d", c.err, got, c.code)
		}
	}
	if got := failureExitCode(fmt.Errorf("read: %w", errDeviceNotConnected)); got != 14 {
		t.Errorf("the same errno during an I/O: got %d, want 14", got)
	}
	if preRunError(nil) != nil {
		t.Error("preRunError(nil) must stay nil")
	}
}

// TestCauseKeeperKeepsTheFirstSpecificClass is the rule of a sweep that ends in
// one summary sentence: the class is read from the types of the errors behind
// it, the first specific one wins over an unclassified one, and the sentence
// itself is never touched.
func TestCauseKeeperKeepsTheFirstSpecificClass(t *testing.T) {
	summary := errors.New("check could not verify everything: 2 file(s) were not judged")

	var none causeKeeper
	if got := none.classify(summary); got != summary || failureExitCode(got) != 2 {
		t.Errorf("no cause: got %v (class %d), want the summary itself at 2", got, failureExitCode(got))
	}

	var k causeKeeper
	k.keep(nil)
	k.keep(fmt.Errorf("open: %w", errSharingViolation)) // class 2: nothing specific yet
	if got := failureExitCode(k.classify(summary)); got != 2 {
		t.Errorf("only an unclassified cause: got %d, want 2", got)
	}
	k.keep(fmt.Errorf("list: %w", errAccessDenied)) // the first specific class replaces it
	k.keep(fmt.Errorf("write: %w", errDiskFull))    // and the first specific class stays
	got := k.classify(summary)
	if code := failureExitCode(got); code != 12 {
		t.Errorf("first specific cause: got %d, want 12", code)
	}
	if got.Error() != summary.Error() {
		t.Errorf("the class must not change the sentence: %q", got.Error())
	}
}
