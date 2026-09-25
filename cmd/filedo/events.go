package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

// The machine channel: one JSON object per line, appended to the file the
// caller named with --events, read by a supervisor that tails it. The shape is
// the CLI-EVENT-STREAM contract, which lives outside this repository - see
// AGENTS.md "External contracts" and docs/contracts/CLI-EVENT-STREAM.md.
//
// SchemaVersion is the wire carrier of that contract (rule 6). It is bumped
// only in the catalog first: a reader dispatches on it, and a higher MAJOR must
// be refused rather than half-read.
const SchemaVersion = 1

type EventKind string

const (
	EventKindRun      EventKind = "run"
	EventKindStep     EventKind = "step"
	EventKindProgress EventKind = "progress"
	EventKindFinding  EventKind = "finding"
	EventKindNote     EventKind = "note"
	EventKindResult   EventKind = "result"
)

type Event struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Kind          EventKind              `json:"kind"`
	Timestamp     time.Time              `json:"timestamp"`
	Data          map[string]interface{} `json:"data,omitempty"`
}

type EventManager struct {
	filePath string
	file     *os.File
	mu       sync.Mutex
	closed   bool
}

var globalEventManager *EventManager

// eventNow is a narrow clock seam for the generated conformance vector.  The
// production path keeps time.Now; the producer test fixes it so the checked-in
// stream is reproducible byte for byte (CLI-EVENT-STREAM §4 rung 1).
var eventNow = time.Now

// InitEventManager initializes the global event channel if eventsPath is not empty.
func InitEventManager(eventsPath string) (*EventManager, error) {
	if eventsPath == "" {
		return nil, nil
	}

	f, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open events file: %w", err)
	}

	em := &EventManager{
		filePath: eventsPath,
		file:     f,
	}
	globalEventManager = em
	return em, nil
}

func (em *EventManager) Emit(kind EventKind, data map[string]interface{}) {
	if em == nil {
		return
	}
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.closed || em.file == nil {
		return
	}

	ev := Event{
		SchemaVersion: SchemaVersion,
		Kind:          kind,
		Timestamp:     eventNow(),
		Data:          jsonSafe(data).(map[string]interface{}),
	}

	bytes, err := json.Marshal(ev)
	if err != nil && kind == EventKindResult {
		// A result that cannot be encoded still ends the stream with its
		// verdict: a stream without a result "said nothing" (rule 10), which
		// is the one thing a supervisor must never be told (CLI-24).
		ev.Data = map[string]interface{}{"verdict": data["verdict"]}
		bytes, err = json.Marshal(ev)
	}
	if err != nil {
		return
	}

	_, _ = em.file.Write(append(bytes, '\n'))
	_ = em.file.Sync()
}

// jsonSafe copies a value tree with every non-finite float replaced by nil.
// encoding/json refuses NaN and ±Inf outright, and a speed divided by a zero
// duration is exactly that - it used to drop the whole event, result included
// (CLI-24).
func jsonSafe(v interface{}) interface{} {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	case float32:
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil
		}
		return x
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, e := range x {
			out[k] = jsonSafe(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = jsonSafe(e)
		}
		return out
	case nil:
		return map[string]interface{}(nil)
	}
	return v
}

func (em *EventManager) Close() {
	if em == nil {
		return
	}
	em.mu.Lock()
	defer em.mu.Unlock()

	if !em.closed && em.file != nil {
		_ = em.file.Sync()
		_ = em.file.Close()
		em.closed = true
	}
}

// Global helper functions

func EmitRunEvent(version, command, target string, args []string) {
	if globalEventManager == nil {
		return
	}
	globalEventManager.Emit(EventKindRun, map[string]interface{}{
		"version": version,
		"command": command,
		"target":  target,
		"args":    redactCredentialArgs(args),
	})
}

func EmitStepEvent(name, description string) {
	if globalEventManager == nil {
		return
	}
	globalEventManager.Emit(EventKindStep, map[string]interface{}{
		"name":        name,
		"description": description,
	})
}

func EmitProgressEvent(doneItems, totalItems, doneBytes, totalBytes int64, speedBps float64, message string) {
	if globalEventManager == nil {
		return
	}
	globalEventManager.Emit(EventKindProgress, map[string]interface{}{
		"doneItems":  doneItems,
		"totalItems": totalItems,
		"doneBytes":  doneBytes,
		"totalBytes": totalBytes,
		"speedBps":   speedBps,
		"message":    message,
	})
}

func EmitFindingEvent(findingType, message string, details map[string]interface{}) {
	if globalEventManager == nil {
		return
	}
	data := map[string]interface{}{
		"type":    findingType,
		"message": message,
	}
	if details != nil {
		data["details"] = details
	}
	globalEventManager.Emit(EventKindFinding, data)
}

func EmitNoteEvent(message string) {
	if globalEventManager == nil {
		return
	}
	globalEventManager.Emit(EventKindNote, map[string]interface{}{
		"message": message,
	})
}

func EmitResultEvent(verdict string, numbers map[string]interface{}, filesLeft []string, reports []string) {
	if globalEventManager == nil {
		return
	}
	data := map[string]interface{}{
		"verdict": verdict, // Passed, Failed, Stopped, Done, Not proven
	}
	if numbers != nil {
		data["numbers"] = numbers
	}
	if len(filesLeft) > 0 {
		data["filesLeft"] = filesLeft
	}
	if len(reports) > 0 {
		data["reports"] = reports
	}
	globalEventManager.Emit(EventKindResult, data)
}
