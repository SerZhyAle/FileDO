# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**Read [`AGENTS.md`](AGENTS.md) - it is the single source of truth for this repo.** This file is only a
pointer; nothing here overrides it and no guidance is duplicated into it. `AGENTS.md` covers the shared
SZA Unified Rules canon, the project shape and frozen anchors, the build/release wall, the testing gate
(root `go test ./...` is known-broken on purpose), destructive-tool safety invariants, and commit/PR rules.

Two things that are expensive to get wrong, both detailed there: `build.ps1` must never create or push a
`v*` tag (only `release.ps1` does), and `RELEASE.md` is the long-form manual for both flows. Prefer the
in-repo skills `/build` and `/release` over running the scripts by hand.
