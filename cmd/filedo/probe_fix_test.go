//go:build windows

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// probeWorld stands in for the volume, the probe and the format of one
// probeDriveFlow run. statAnswers are the answers to successive stats of the
// drive's root: the first is asked before the probe, the second after it.
type probeWorld struct {
	statAnswers []error
	stats       int
	probes      int
	formats     int
	probeErr    error
}

func (w *probeWorld) install(t *testing.T) {
	t.Helper()
	oldStat, oldRun, oldFormat := probeStatRoot, probeRunFn, probeFormatFn
	t.Cleanup(func() { probeStatRoot, probeRunFn, probeFormatFn = oldStat, oldRun, oldFormat })
	probeStatRoot = func(string) (os.FileInfo, error) {
		i := w.stats
		w.stats++
		if i < len(w.statAnswers) {
			return nil, w.statAnswers[i]
		}
		return nil, nil
	}
	probeRunFn = func(string) error { w.probes++; return w.probeErr }
	probeFormatFn = func(rune) error { w.formats++; return nil }
}

// AUD-02-F3: a drive that cannot be read before the probe - locked by
// BitLocker, not ready, listing denied - is "could not verify". It is never a
// reason to format: the probe never touched it, and `yes` skips the questions.
func TestProbeFix_NeverFormatsADriveItCouldNotReadBeforeTheProbe(t *testing.T) {
	for name, statErr := range map[string]error{
		"access denied":     os.ErrPermission,
		"not ready":         errors.New("The device is not ready."),
		"BitLocker locked":  errors.New("This drive is locked by BitLocker Drive Encryption."),
		"file system error": errors.New("The file or directory is corrupted and unreadable."),
	} {
		t.Run(name, func(t *testing.T) {
			w := &probeWorld{statAnswers: []error{statErr}}
			w.install(t)

			err := probeDriveFlow(`\\.\X:`, 'X', true, true)
			if err == nil {
				t.Fatal("probe fix yes on a drive that could not be read returned success")
			}
			if w.formats != 0 {
				t.Fatalf("the drive was formatted %d time(s) although the probe never read it", w.formats)
			}
			if w.probes != 0 {
				t.Errorf("the probe ran %d time(s) on a drive that could not be read", w.probes)
			}
			if !strings.Contains(err.Error(), "recover") {
				t.Errorf("the refusal does not name `recover` as the explicit repair: %v", err)
			}
		})
	}
}

// The repair `fix` was documented for still exists: a drive that read fine
// before the probe and does not after it is offered the quick format.
func TestProbeFix_StillRepairsADriveThatBrokeDuringTheProbe(t *testing.T) {
	w := &probeWorld{statAnswers: []error{nil, os.ErrPermission}}
	w.install(t)

	if err := probeDriveFlow(`\\.\X:`, 'X', true, true); err != nil {
		t.Fatalf("probe fix yes on a drive that broke during the probe: %v", err)
	}
	if w.probes != 1 || w.formats != 1 {
		t.Errorf("probes=%d formats=%d, want 1 and 1", w.probes, w.formats)
	}
}

// Without `fix` nothing is ever formatted, however the drive ends up.
func TestProbeFix_WithoutFixNeverFormats(t *testing.T) {
	for name, answers := range map[string][]error{
		"unreadable before": {os.ErrPermission},
		"broken during":     {nil, os.ErrPermission},
	} {
		t.Run(name, func(t *testing.T) {
			w := &probeWorld{statAnswers: answers}
			w.install(t)
			if err := probeDriveFlow(`\\.\X:`, 'X', true, false); err == nil {
				t.Error("an unreadable drive returned success")
			}
			if w.formats != 0 {
				t.Errorf("formatted %d time(s) without fix", w.formats)
			}
		})
	}
}
