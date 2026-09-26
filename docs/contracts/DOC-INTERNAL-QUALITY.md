# DOC-INTERNAL-QUALITY

| | |
| --- | --- |
| **Id** | `DOC-INTERNAL-QUALITY` |
| **Version** | 0.9 draft (no wire carrier - a file tree layout and check invariants) |
| **Role** | consumer - the internal engineering corpus: `AGENTS.md`, `RELEASE.md`, `docs/README.md`, `docs/contracts/`, the per-folder READMEs |
| **Home** | shared contracts catalog, folder `documentation-quality/` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- Declare every maintained document in [`../DOCUMENT_REGISTRY.jsonl`](../DOCUMENT_REGISTRY.jsonl) - path,
  topic, product area, update triggers, role (`source`/`render`) and corpus (`internal`/`external`) - or
  cover it with an ignore record and its reason. A new `.md`/`.html` without one fails the gate.
- Keep every relative link and anchor resolving; a move or rename updates its referrers in the same change.
- Keep the house text style and English-only comments; no remote embeds or `http://` links in internal docs.
- Regenerate, never hand-edit, anything derived from code; keep the flag and exit-code tables in step with
  the parsers.

## Gates

| Rules | Gate | Command |
| --- | --- | --- |
| 1-2 registry both ways, 3 links and anchors, 5 style, 6 image paths, 7 offline safety | `packaging/check-internal-docs.ps1`, step 8 of `build.ps1 -Test` | `pwsh -NoProfile -File packaging/check-internal-docs.ps1` |
| 4 the check flag and `FILEDO_CHECK_*` lists in the READMEs, and `filedo_check`'s option table, against the parser and the launcher | `TestDocs_*` in `cmd/filedo/docs_flags_test.go`, step 4 of `build.ps1 -Test` | `go test ./cmd/filedo/ -count=1 -vet=off -run TestDocs_` |
| 4 exit vocabulary and shell strings in every README locale | `TestSurfaces_*` in `cmd/filedo/fdsec_surfaces_test.go` | `go test ./cmd/filedo/ -count=1 -vet=off -run TestSurfaces_` |

Exit codes follow `CHECK-VERDICT`: 0 pass, 1 a defect was found, 2 could not verify.

**Status: adopted 2026-09-26; all seven rules gated 2026-09-26 (spec SP-0061).** Not gated: English-only
code comments (rule 5), which no check reads.
