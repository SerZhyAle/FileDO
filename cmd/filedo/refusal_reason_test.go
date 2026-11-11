package main

// SP-0196: a refusal the user can act on says why in a closed token on the
// `error` finding that ends the run (CLI-EVENT-STREAM rule 17), so a window
// with no console can show a sentence of its own. These tests run in-process:
// the planners are pure and the event stream is a scratch file.

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// reasonNumber reads one integer out of a finding's details, as the shell does.
func reasonNumber(t *testing.T, details map[string]interface{}, key string) int64 {
	t.Helper()
	v, ok := details[key].(float64)
	if !ok {
		t.Fatalf("details carries no number %q: %v", key, details)
	}
	return int64(v)
}

func TestFillRefusalCarriesItsReason(t *testing.T) {
	v := volumeFacts{Total: 1 << 40, FileSystem: "NTFS"}
	_, err := planFill(50*capMiB, 100, v)
	if err == nil {
		t.Fatal("planFill accepted 100 MB files on a volume with 50 MB free")
	}
	details := reasonDetails(err)
	if details == nil || details["reason"] != reasonFillInsufficientSpace {
		t.Fatalf("details = %v, want reason %q", details, reasonFillInsufficientSpace)
	}
	want := map[string]int64{"fileBytes": 100 * capMiB, "freeBytes": 50 * capMiB, "reserveBytes": fillReserve(v)}
	for key, n := range want {
		if got, ok := details[key].(int64); !ok || got != n {
			t.Errorf("details[%q] = %v, want %d", key, details[key], n)
		}
	}
	if len(details) != len(want)+1 {
		t.Errorf("details carries keys the catalogue row does not name: %v", details)
	}
	if got := failureExitCode(err); got != 13 {
		t.Errorf("exit class %d, want 13: the token never changes the class", got)
	}
	var marked *exitClassError
	if msg := err.Error(); msg == "" || !errors.As(err, &marked) {
		t.Errorf("the refusal lost its sentence or its exit class: %q", msg)
	}
}

func TestFillThatFitsCarriesNoReason(t *testing.T) {
	v := volumeFacts{Total: 1 << 40, FileSystem: "NTFS"}
	if _, err := planFill(10*capGiB, 100, v); err != nil {
		t.Fatalf("planFill refused a fill that fits: %v", err)
	}
}

func TestCapacityTestRefusalCarriesItsReason(t *testing.T) {
	cases := []struct {
		name string
		v    volumeFacts
		free int64
	}{
		{"data volume", volumeFacts{Total: 1 << 40, FileSystem: "NTFS"}, 60 * capMiB},
		{"system volume", volumeFacts{Total: 500 * capGiB, FileSystem: "NTFS", SystemVolume: true}, 10*capGiB + 50*capMiB},
		{"system volume under its reserve", volumeFacts{Total: 500 * capGiB, FileSystem: "NTFS", SystemVolume: true}, 5 * capGiB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := planCapacityTest(c.free, 100, c.v)
			if err == nil {
				t.Fatal("planCapacityTest accepted a volume with too little room")
			}
			details := reasonDetails(err)
			if details == nil || details["reason"] != reasonTestInsufficientSpace {
				t.Fatalf("details = %v, want reason %q", details, reasonTestInsufficientSpace)
			}
			if got, ok := details["requiredBytes"].(int64); !ok || got != capacityMinTestBytes {
				t.Errorf("requiredBytes = %v, want %d", details["requiredBytes"], int64(capacityMinTestBytes))
			}
			if got, ok := details["freeBytes"].(int64); !ok || got != c.free {
				t.Errorf("freeBytes = %v, want %d", details["freeBytes"], c.free)
			}
			if got, ok := details["reserveBytes"].(int64); !ok || got != p.Reserve {
				t.Errorf("reserveBytes = %v, want %d", details["reserveBytes"], p.Reserve)
			}
			avail, ok := details["availableBytes"].(int64)
			if !ok || avail < 0 || avail >= capacityMinTestBytes {
				t.Errorf("availableBytes = %v, want a non-negative amount below the %d minimum", details["availableBytes"], int64(capacityMinTestBytes))
			}
			if len(details) != 5 {
				t.Errorf("details carries keys the catalogue row does not name: %v", details)
			}
			if got := failureExitCode(err); got != 13 {
				t.Errorf("exit class %d, want 13", got)
			}
		})
	}
}

func TestContainerRestoreRefusalCarriesItsReason(t *testing.T) {
	t.Setenv(fdsecRevealRootEnv, filepath.Join(t.TempDir(), "root"))
	const huge = int64(1) << 62
	_, err := fdsecStartRoot(huge)
	if err == nil {
		t.Fatal("fdsecStartRoot accepted a container larger than any volume")
	}
	details := reasonDetails(err)
	if details == nil || details["reason"] != reasonContainerInsufficientSpace {
		t.Fatalf("details = %v, want reason %q", details, reasonContainerInsufficientSpace)
	}
	if got, ok := details["neededBytes"].(int64); !ok || got != huge {
		t.Errorf("neededBytes = %v, want %d", details["neededBytes"], huge)
	}
	if got, ok := details["freeBytes"].(int64); !ok || got < 0 || got >= huge {
		t.Errorf("freeBytes = %v, want a measured, non-negative amount", details["freeBytes"])
	}
	if len(details) != 3 {
		t.Errorf("details carries keys the catalogue row does not name: %v", details)
	}
}

// A disk that filled up under a write is the one user-fixable ending every writing
// verb can meet; the token needs the errno in the chain and nothing else.
func TestDiskFullErrnoCarriesVolumeFull(t *testing.T) {
	full := fmt.Errorf("fdsec: write chunk 3: %w", errDiskFull)
	details := failureDetails(full)
	if details == nil || details["reason"] != reasonVolumeFull || len(details) != 1 {
		t.Fatalf("details = %v, want exactly reason %q", details, reasonVolumeFull)
	}
	if got := failureDetails(fmt.Errorf("write: %w", errHandleDiskFull)); got == nil || got["reason"] != reasonVolumeFull {
		t.Errorf("ERROR_HANDLE_DISK_FULL lost its token: %v", got)
	}
	if got := failureDetails(errors.New("the disk is full")); got != nil {
		t.Errorf("the wording of a message earns a token: %v", got)
	}
	if got := failureDetails(fmt.Errorf("write: %w", errAccessDenied)); got != nil {
		t.Errorf("another errno earns a token: %v", got)
	}
	// A specific refusal keeps its own token even when a disk-full errno is below it.
	specific := withReason(full, reasonContainerInsufficientSpace, map[string]interface{}{"freeBytes": int64(1), "neededBytes": int64(2)})
	if got := failureDetails(specific); got == nil || got["reason"] != reasonContainerInsufficientSpace {
		t.Errorf("the specific token was replaced: %v", got)
	}
}

// A file another program holds open ends a run on a sharing or lock violation:
// the token is file-in-use with no numbers, and a disk-full errno keeps its own.
func TestFileInUseErrnoCarriesFileInUse(t *testing.T) {
	for _, cause := range []error{errSharingViolation, errLockViolation} {
		details := failureDetails(fmt.Errorf("wipe: %w", cause))
		if details == nil || details["reason"] != reasonFileInUse || len(details) != 1 {
			t.Errorf("%v: details = %v, want exactly reason %q", cause, details, reasonFileInUse)
		}
	}
	if got := failureDetails(fmt.Errorf("write: %w", errDiskFull)); got == nil || got["reason"] != reasonVolumeFull {
		t.Errorf("a disk-full errno lost volume-full: %v", got)
	}
}

func TestReasonDetailsSurviveWrapping(t *testing.T) {
	base := withReason(errors.New("no room"), reasonFillInsufficientSpace, map[string]interface{}{"freeBytes": int64(1)})
	wrapped := fmt.Errorf("fill: %w", base)
	if details := reasonDetails(wrapped); details == nil || details["reason"] != reasonFillInsufficientSpace {
		t.Errorf("a wrapped refusal lost its token: %v", details)
	}
	if reasonDetails(errors.New("plain")) != nil || reasonDetails(nil) != nil {
		t.Error("an error with no token carries details")
	}
	if withReason(nil, reasonFillInsufficientSpace, nil) != nil {
		t.Error("withReason invented an error out of nil")
	}
}

// The token travels on the `error` finding, and only a refusal that carries it
// puts `reason` there; the verdict and the exit code stay what rule 11 says.
func TestRunFailureEmitsTheReasonOnTheFinding(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantReason string
	}{
		{"fill refusal", mustPlanFillRefusal(t), reasonFillInsufficientSpace},
		{"plain failure", exitClassf(11, "target missing"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			events := filepath.Join(t.TempDir(), "events.jsonl")
			savedRun, savedExit, savedEvents, savedInterrupt := currentRun, globalExitCode, globalEventManager, globalInterruptHandler
			defer func() {
				currentRun, globalExitCode, globalEventManager, globalInterruptHandler = savedRun, savedExit, savedEvents, savedInterrupt
			}()
			currentRun = &runOutcome{numbers: make(map[string]interface{})}
			globalExitCode = 0
			globalInterruptHandler = nil
			em, err := InitEventManager(events)
			if err != nil {
				t.Fatal(err)
			}
			defer em.Close()

			func() {
				defer finishRun()
				beginRun(runActs, "fill", "", nil)
				runFailure(c.err)
			}()

			var finding map[string]interface{}
			for _, ev := range readEvents(t, events) {
				if ev["kind"] != "finding" {
					continue
				}
				data, _ := ev["data"].(map[string]interface{})
				if data["type"] == "error" {
					finding = data
				}
			}
			if finding == nil {
				t.Fatal("the run recorded no error finding")
			}
			details, _ := finding["details"].(map[string]interface{})
			if c.wantReason == "" {
				if _, has := details["reason"]; has {
					t.Fatalf("a failure with no token carries details.reason: %v", details)
				}
			} else {
				if details["reason"] != c.wantReason {
					t.Fatalf("details.reason = %v, want %q", details["reason"], c.wantReason)
				}
				if reasonNumber(t, details, "fileBytes") != 100*capMiB || reasonNumber(t, details, "freeBytes") != 50*capMiB {
					t.Errorf("the numbers did not reach the wire: %v", details)
				}
				if msg, _ := finding["message"].(string); msg == "" {
					t.Error("the finding carries no message: the token must never be the only carrier of the fact")
				}
			}
			if got := lastResult(t, events)["verdict"]; got != "Not proven" {
				t.Errorf("verdict %v, want Not proven", got)
			}
		})
	}
}

func mustPlanFillRefusal(t *testing.T) error {
	t.Helper()
	_, err := planFill(50*capMiB, 100, volumeFacts{Total: 1 << 40, FileSystem: "NTFS"})
	if err == nil {
		t.Fatal("planFill accepted 100 MB files on a volume with 50 MB free")
	}
	return err
}
