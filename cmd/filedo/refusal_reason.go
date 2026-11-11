package main

import "errors"

// The closed `details.reason` tokens of CLI-EVENT-STREAM rule 17 that say a
// volume has too little room, beside the container destination-name one
// (fdsecReasonDestinationName). Each is published in the catalogue, with the
// keys its numbers travel under, before the code that emits it; a window with
// no console shows its own sentence for it (SP-0196).
const (
	reasonFillInsufficientSpace      = "fill-insufficient-space"
	reasonTestInsufficientSpace      = "test-insufficient-space"
	reasonContainerInsufficientSpace = "container-insufficient-space"
	// reasonVolumeFull is the volume that filled up while a verb was writing:
	// no numbers, because the verb that met it measured none.
	reasonVolumeFull = "volume-full"
	// reasonFileInUse is a file the run needed that another program holds open
	// (a sharing or lock violation): no numbers, the run measured no size.
	reasonFileInUse = "file-in-use"
)

// reasonError carries a closed token and its numbers on an ordinary error, so
// the refusal keeps its type, its exit class and its console sentence and
// runFailure can still put the token on the `error` finding that ends the run.
// The token is never the only carrier of the fact (rule 17): the message, the
// verdict and the exit code say it too.
type reasonError struct {
	err     error
	details map[string]interface{}
}

func (e *reasonError) Error() string { return e.err.Error() }
func (e *reasonError) Unwrap() error { return e.err }

// withReason marks err with a closed token and the numbers its catalogue row
// names. The numbers are integers in bytes and never a path, a name or a
// credential (rule 17).
func withReason(err error, reason string, numbers map[string]interface{}) error {
	if err == nil {
		return nil
	}
	details := make(map[string]interface{}, len(numbers)+1)
	for k, v := range numbers {
		details[k] = v
	}
	details["reason"] = reason
	return &reasonError{err: err, details: details}
}

// reasonDetails is the `details` of the first token in err's chain, or nil
// when the error carries none.
func reasonDetails(err error) map[string]interface{} {
	var re *reasonError
	if errors.As(err, &re) {
		return re.details
	}
	return nil
}

// failureDetails is what runFailure puts on the `error` finding: the token a
// refusal carries on purpose, else `volume-full` for any error whose chain
// holds the file system's "disk full" - the one user-fixable ending every
// writing verb can meet, and a copy, a container or a virtual disk reports it
// as a raw write error otherwise - else `file-in-use` for a sharing or lock
// violation in the chain.
func failureDetails(err error) map[string]interface{} {
	if details := reasonDetails(err); details != nil {
		return details
	}
	if isDiskFullError(err) {
		return map[string]interface{}{"reason": reasonVolumeFull}
	}
	if isFileInUseError(err) {
		return map[string]interface{}{"reason": reasonFileInUse}
	}
	return nil
}
