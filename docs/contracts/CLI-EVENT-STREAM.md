# CLI-EVENT-STREAM

| | |
| --- | --- |
| **Id** | `CLI-EVENT-STREAM` |
| **Version** | 0.16, draft; SP-0195 (0.14), SP-0196 (0.15) and SP-0201 (0.16) are built in the working tree, not yet built or tested - the shell reads `details.reason` for the six tokens below (wire carrier: `schemaVersion` 1 on every line) |
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
- **Supervisor exit codes** are 0 passed or done, 1 defect, 2 unclassified failure, 10 usage, 11 target unavailable, 12 permission, 13 resources, 14 hardware I/O, 15 network and 16 stopped. The container verbs use
  the separate classes of `FDSEC-BEHAVIOUR` section 7.1 and must carry a `result` event, because those
  digits mean something else.
- **Nothing secret reaches the channel**: `run.data.args` passes `redactCredentialArgs` first, and a
  sealed name never appears in any event.
- **A stop is a file**, not a signal: the shell creates the `--stop-file`, the CLI polls for its presence
  and takes the interrupt path it already has. A non-container stop with no recorded defect ends *Stopped*, exit 16; the reveal removal stop and one stopped container command exit 0 (a batch of several lines exits 16); a
  defect recorded before the stop outranks it - *Failed*, exit 1, on the graceful and the forced path
  (`outcome.go` `verdict`, `finishForcedRun`), and the shell's `Judge` keeps a *Failed* result even with
  its stop file present (rule 15).
- **A refusal the user can act on says why in a closed token** (rule 17): `runFailure` gives the `error` finding
  `details.reason` - `destination-name` for `fdsecDestinationNameError` (the message names the rule and never
  the name), `fill-insufficient-space`, `test-insufficient-space` and `container-insufficient-space` for the
  three planners that measured the refusal (`reasonError`, `refusal_reason.go`), each with the integer byte
  counts its row names, `volume-full` for any error whose chain holds the file system's disk-full errno, and
  `file-in-use` (no numbers) for one whose chain holds a sharing or lock violation errno.
  A token never changes the exit class or the verdict, and a new value needs the catalog first.
- **The shell turns a token into a sentence of its own language** (`filedo_win_vb/RunExplanation.vb`): a Not
  proven run shows the sentence of its token on the Job page's card and the Command page's verdict line, one
  plain sentence that points at the output when there is no token or it is unknown, and never the CLI's English
  message. A number a row names that cannot be read as a whole non-negative integer makes the sentence the plain
  one, never a zero. `Runner.ReasonOf` keeps the first token of an `error` finding.
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
