//go:build windows

package main

import (
	"errors"
	"filedo/fmsworker"
	"testing"
)

func TestVDShareAvailabilityNeverInfersAbsentInstallation(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "ready"},
		{fmsworker.ErrWorkerUnavailable, "unavailable"},
		{fmsworker.ErrUntrustedServer, "untrusted"},
		{fmsworker.ErrNotCapable, "not-capable"},
		{fmsworker.ErrCommunication, "communication"},
		{errors.New("timeout"), "communication"},
	} {
		if got := vdShareAvailability(tc.err); got != tc.want {
			t.Errorf("availability = %q, want %q", got, tc.want)
		}
	}
}
