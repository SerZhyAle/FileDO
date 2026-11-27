# RELEASE-LEDGER

| | |
| --- | --- |
| **Id** | `RELEASE-LEDGER` |
| **Version** | 0.2 draft, read 2026-11-26 (FILEDO-0245; rule 12 is about Play channels, and FileDO has none), adopted at 0.1 on 2026-11-17 (SP-0229); the build and release hooks are written and proven on a scratch repository, the first real build and release that write through them are still to come |
| **Role** | producer of the ledger files in `docs/releases/`, written only through the canon's one writer |
| **Home** | shared contracts catalog, folder `release-ledger/`, document `README.md` |
| **Owner** | sza-unified-rules |

## What this repository must do to stay conformant

- **The ledger is `docs/releases/`**, the default of `HARNESS-PROFILE` `paths.releasesDir` (the profile names no
  other): `channels.json` (declared by hand), `builds.json` and `history.jsonl` (machine-written, the history
  append-only) and `RELEASES.md` (rendered after every write, never edited). The folder is tracked in git. It is
  not part of the site (see `docs/README.md`).
- **Two packages, four channels.** `filedo` (the zip, the MSI and the setup installer, one `yyMMddHHmm` stamp)
  ships to `github`, `winget` and `installer` (the setup EXE, read from the latest GitHub Release by a pattern);
  `filedo-msix` (the Store package, the four-part version `yy.MMdd.HHmm.0`) ships to `msstore`. The two version
  shapes are why they are two packages: a version is compared as a string for equality only (rule 7), so one
  package with two shapes would never match its own channels. `channels.json` declares every channel the canon
  stamp lists.
- **One door to the writer.** `packaging/release-ledger.ps1` finds the shipped writer (`SZA_HARNESS_ROOT`, then
  the installed sza plugin) and adds `-RepoRoot`; this repository has no `tools/harness.ps1` forwarder.
- **Every successful build records itself as its last step** (rule 4): `build.ps1` writes `filedo` and
  `msix/build-msix.ps1` writes `filedo-msix`. The kind is `release` only for the gate build of `release.ps1`
  (`FILEDO_BUILD_KIND=release`) and for a Store package built with `-Stamp` from its tag; every other build is a
  `test` build. A ledger failure is one yellow line and never fails a build.
- **Every channel step of a release records itself** (`release.ps1`): GitHub and the installer `live` at step 5,
  winget `submitted` at step 7 (`pending` when the submit failed), the MSIX `pending` at step 8. The Store
  submission and a merged winget pull request are the owner's moments: `packaging/release-ledger.ps1 probe` sees
  them, or `confirm` records them with the page that shows them.
- **The folder rides with the release.** `release.ps1` allows `docs/releases/` dirty in the preflight and after the
  gate, commits it with `exe_to_download/` in `Build v<stamp>`, commits what it writes after the tag as
  `Ledger: v<stamp>`, and `Get-TagSourceDrift` and `msix/build-msix.ps1` leave it out of the "sources equal the
  tag" check, as they leave out `winget/`.
- **No secret and no machine fact goes into the files** (rule 9): the Store id and the package identifier are
  public, artifacts are repository-relative paths.
- **Two edges of the writer, carried here.** The winget probe reads the version of an open pull request from its
  title only when the version has a dot, and a FileDO stamp has none, so the open pull request is recorded by
  `release.ps1` (a best-effort lookup) or by hand with `publish`, and only the merge shows in `probe`. `import`
  stamps the merged winget versions with the import time, because the listing carries no dates.
