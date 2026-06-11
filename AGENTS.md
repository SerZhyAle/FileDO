# Repository Guidelines

## Project Structure & Module Organization
`FileDO` is a Windows-first Go CLI. The root package builds `filedo.exe` from top-level `main*.go` and feature files such as `copy_*.go`, `probe_*.go`, and `*_windows.go`. Shared logic lives in `fileduplicates/` and `helpers/`. Standalone companion tools are split into their own modules: `CHECK/`, `FILL/`, and `TEST/`. Packaging and distribution assets live in `packaging/wix/`, `winget/`, `assets/`, and `exe_to_download/`. Documentation is in `docs/`, and the optional VB.NET GUI is in `filedo_win_vb/`.

## Build, Test, and Development Commands
Use PowerShell from the repo root unless a submodule is noted.

- `.\build.ps1` builds all four executables and stages them in `exe_to_download/`.
- `go build -o filedo.exe .` builds only the main CLI.
- `cd CHECK; go build -o filedo_check.exe .` builds the check utility.
- `cd FILL; go build -o filedo_fill.exe .` builds the fill utility.
- `cd TEST; go test` verifies the test module compiles.
- `go test ./...` currently fails in the root package because of existing `fmt`/vet issues, so do not rely on it as the primary validation step.

## Coding Style & Naming Conventions
Format Go code with `gofmt` before committing. Follow Go defaults: tabs for indentation, mixedCaps for exported names, lowerCamelCase for locals, and lowercase file names with underscores for feature grouping, for example `device_windows.go` and `device_unsupported.go`. Keep Windows-specific behavior in `*_windows.go` and cross-platform fallbacks in `*_unsupported.go`. Prefer small, focused files over adding more logic to `main.go`.

## Testing Guidelines
Add or update tests in `TEST/` when changing user-visible behavior. Keep test names descriptive, for example `TestBuildsCleanly` or `TestProbeSkipsInvalidTarget`. Use `tests\prepare_test_env.cmd` for environment setup when exercising list-driven scenarios, and mention any disk, drive-letter, or admin requirements in your PR.

## Commit & Pull Request Guidelines
Recent history uses short, imperative subjects such as `Fix WiX icon path`, `Add MSI installer`, and `Sync winget/ ...`. Follow that pattern: start with a verb, keep the subject specific, and mention the affected area. PRs should include a brief summary, manual verification commands, linked issues when relevant, and screenshots for GUI, installer, or docs changes.
