package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"filedo/vdisk"
)

// Several containers in one command (SP-0004 spec 5.7, P6 T6.12): info and
// verify take more than one container, or a mask (`*.fdd`), and report each
// one separately. A failure on one does not abandon the rest; the command
// ends with the class of the first failure, and the rest are named in the
// console and in the event stream. The mask is expanded the way secure
// expands one (fdsecExpandMask): the folder part is literal, and only * and ?
// in the name are wildcards.

// vdManyError is the end of a run over several containers where some failed:
// its text counts them, and it carries the first failure's class.
type vdManyError struct {
	msg   string
	first error
}

func (e *vdManyError) Error() string { return e.msg }
func (e *vdManyError) Unwrap() error { return e.first }

func vdMasked(p string) bool { return strings.ContainsAny(p, "*?") }

// vdTargets turns the container words of a line into container paths. The
// first word was resolved through the registry already (runVd); every other
// one must be a .fdd path or a mask of them - a word that is neither is never
// quoted back, since it may be a password typed without its prefix.
func vdTargets(words []string) ([]string, error) {
	var out []string
	for i, w := range words {
		if i > 0 && !isFddPath(w) {
			return nil, vdUsagef("word %d is not a container: several containers are named as .fdd paths or a mask like *.fdd", i+1)
		}
		if !vdMasked(w) {
			out = append(out, w)
			continue
		}
		matches, err := fdsecExpandMask(w)
		if err != nil {
			return nil, fmt.Errorf("%w: expand mask %s: %v", vdisk.ErrIO, w, err)
		}
		n := 0
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.Mode().IsRegular() && isFddPath(m) {
				out = append(out, m)
				n++
			}
		}
		if n == 0 {
			return nil, vdUsagef("no container matches %s", w)
		}
	}
	return out, nil
}

// vdEach runs one verb over each target. One target is the verb itself. With
// several, each is headed by its path, a failure is printed and recorded as a
// finding of its own and the next one runs; a stop ends the loop.
func vdEach(targets []string, verb string, one func(string) error) error {
	if len(targets) == 1 {
		return one(targets[0])
	}
	var first error
	failed := 0
	for i, t := range targets {
		if runStopRequested() {
			return fmt.Errorf("%w: %d of %d containers done; the rest were not touched", vdisk.ErrStopped, i, len(targets))
		}
		if i > 0 {
			fmt.Println()
		}
		err := one(t)
		if err == nil {
			continue
		}
		if errors.Is(err, vdisk.ErrStopped) {
			return err
		}
		failed++
		if first == nil {
			first = err
		}
		fmt.Fprintf(os.Stderr, "Error: %s: %s\n", t, vdExplain(err))
		EmitFindingEvent("error", fmt.Sprintf("%s: %s", t, eventSafeErrorMessage(err)), map[string]interface{}{"container": t, "class": vdExitClass(err)})
	}
	if first == nil {
		fmt.Printf("\n%s: %d containers, none failed.\n", verb, len(targets))
		return nil
	}
	return &vdManyError{msg: fmt.Sprintf("%s: %d of %d containers failed; the first failure: %s", verb, failed, len(targets), vdExplain(first)), first: first}
}
