# DOC-INTERNAL-QUALITY

| | |
| --- | --- |
| **Id** | `DOC-INTERNAL-QUALITY` |
| **Version** | 0.9 draft (no wire carrier - a file tree layout and check invariants) |
| **Role** | consumer - the internal engineering corpus: `AGENTS.md`, `RELEASE.md`, `docs/README.md`, `docs/contracts/`, the per-folder READMEs |
| **Home** | shared contracts catalog, folder `documentation-quality/` |
| **Owner** | FastMediaSorter Android. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- Keep every relative link and anchor resolving; a move or rename updates its referrers in the same change.
- Keep the house text style and English-only comments; no remote embeds or `http://` links in internal docs.
- Regenerate, never hand-edit, anything derived from code; keep the flag and exit-code tables in step with
  the parsers.

**Status: adopted 2026-09-26, partially compliant.** Measured that day: 0 broken links in 173, internal
corpus clean of style and offline violations. Held today only by `TestSurfaces_*` in
`cmd/filedo/fdsec_surfaces_test.go` (exit vocabulary, shell strings). Open: the document registry and
reverse coverage (rules 1-2), and gates for rules 3-7 - spec SP-0061.
