# CLI-EVENT-STREAM

| | |
| --- | --- |
| **Id** | `CLI-EVENT-STREAM` |
| **Version** | 0.9, draft (wire carrier: `schemaVersion` 1 on every line) |
| **Home** | shared contracts catalog, folder `cli-event-stream/` |
| **Role** | producer (`cmd/filedo`) and consumer (`filedo_win_vb`) - both implementations live here |
| **Owner** | FileDO. Amendments are written in the catalog first |

## What this repository must do to stay conformant

- **The channel stays opt-in.** Without `--events <path>` the CLI behaves exactly as it did before it had
  one. `--events` and `--stop-file` are stripped in `extractGlobalFlags` before verb dispatch.
- **One event is one line**, appended and flushed, valid JSON at every instant. The shell tails by byte
  offset and must not advance past an incomplete final line.
- **The verdict is the `result` event, never the exit code alone.** A run that wrote no `result` is
  reported as *Not proven* with a reason. A code that disagrees with the event is *Not proven* too
  (`filedo_win_vb/Runner.vb`, `Judge`).
- **Supervisor exit codes are 0 / 1 / 2** - passed, failed, could not be verified. The container verbs use
  the separate classes of `FDSEC-BEHAVIOUR` section 7.1 and must carry a `result` event, because those
  digits mean something else.
- **Nothing secret reaches the channel**: `run.data.args` passes `redactCredentialArgs` first, and a
  sealed name never appears in any event.
- **A stop is a file**, not a signal: the shell creates the `--stop-file`, the CLI polls for its presence
  and takes the interrupt path it already has.
- Both sides cite `CLI-EVENT-STREAM rule N` in their comments.
- Open deviations are recorded as dated exceptions in the catalog's registry; ticket SP-0012.

## Where each half lives

- **Producer.** `cmd/filedo/outcome.go` is the single site: `beginRun` opens the stream, `runStep`,
  `runDefect`, `runNumber` and `runFailure` record, and `finishRun` - deferred once in `main()` - writes
  the one `result` event and sets the one exit code. `cmd/filedo/events.go` is the wire format,
  `progress.go` the `progress` tick every long loop shares. Proven by
  `go test ./cmd/filedo/ -count=1 -vet=off -run 'TestEventManager_Emission|TestInterruptHandler_StopFile|TestEventSample_V1|TestExitCodeVocabulary|TestEventStreamShape|TestContainerVerbKeepsItsOwnClasses|TestRunEventRedactsCredentials'`.
- **Consumer.** `filedo_win_vb/EventStream.vb` tails by byte offset and never advances past an incomplete
  line; `filedo_win_vb/Runner.vb` (`Judge`) decides the verdict. Proven by `filedo_win.exe --selftest`,
  whose `verdict:` and `tailer:` lines are rungs 2 and 3 of the contract's conformance ladder.
