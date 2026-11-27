# Releases

<!-- Rendered by the sza release-ledger from channels.json, builds.json and history.jsonl (RELEASE-LEDGER rule 10). Do not edit: the next write replaces this file. -->

What each package was built as last, what each channel carries, and every publication before it.

## Channels

| Package | Channel | Kind | Live | Since | In flight | Ref |
| --- | --- | --- | --- | --- | --- | --- |
| filedo | github | github | 2611270100 | 2026-11-27 | - | [v2611270100](https://github.com/SerZhyAle/FileDO/releases/tag/v2611270100) |
| filedo | winget | winget | 2611110217 | 2026-11-17 | 2611270100 pending (2026-11-27); 2611171800 submitted (2026-11-17) | - |
| filedo | installer (Setup installer (an asset of the GitHub Release)) | url | 2611270100 | 2026-11-27 | - | [v2611270100](https://github.com/SerZhyAle/FileDO/releases/tag/v2611270100) |
| filedo-msix | msstore (Microsoft Store) | msstore | - | - | 26.1127.100.0 pending (2026-11-27); 26.1117.1800.0 blocked (2026-11-17); 26.1111.217.0 submitted (2026-11-17) | - |

## Last builds

| Package | Flavor | Kind | Version | Built | Artifact |
| --- | --- | --- | --- | --- | --- |
| filedo | default | release | 2611270100 | 2026-11-27 02:03:28+01:00 | dist/FileDO-2611270100-setup.exe |
| filedo | default | test | 2611270100 | 2026-11-27 01:02:08+01:00 | dist/FileDO-2611270100-setup.exe |
| filedo-msix | default | release | 26.1127.100.0 | 2026-11-27 02:14:32+01:00 | msix/out/FileDO_26.1127.100.0.msix |
| filedo-msix | default | test | 26.1117.1958.0 | 2026-11-17 19:58:16+01:00 | msix/out/FileDO_26.1117.1958.0.msix |

## History

| When | Package | Channel | Version | State | Source | Ref | Note |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-11-27 02:14:33+01:00 | filedo-msix | msstore | 26.1127.100.0 | pending | release | - | MSIX built from the tag, unsigned; upload and submission are manual |
| 2026-11-27 02:14:23+01:00 | filedo | winget | 2611270100 | pending | release | - | not submitted: wingetcreate submit failed (exit 1) |
| 2026-11-27 02:14:03+01:00 | filedo | installer | 2611270100 | live | release | [v2611270100](https://github.com/SerZhyAle/FileDO/releases/tag/v2611270100) | the setup EXE and the MSI are assets of the Release |
| 2026-11-27 02:14:02+01:00 | filedo | github | 2611270100 | live | release | [v2611270100](https://github.com/SerZhyAle/FileDO/releases/tag/v2611270100) | - |
| 2026-11-17 18:58:31+01:00 | filedo | winget | 2611171800 | submitted | manual | [PR#482547](https://github.com/microsoft/winget-pkgs/pull/482547) | recorded again: the import stamped the merged versions with the import time, so the first record (the PR time) read as older |
| 2026-11-17 18:58:20+01:00 | filedo-msix | msstore | 26.1117.1800.0 | blocked | manual | - | MSIX built from the tag, not uploaded: Submission 8 (26.1111.217.0) is still PendingCommit in Partner Center |
| 2026-11-17 18:57:27+01:00 | filedo-msix | msstore | 26.1111.217.0 | submitted | import | [Submission 8](https://apps.microsoft.com/detail/9PH1LPCMRG83) | Store status PendingCommit |
| 2026-11-17 18:57:27+01:00 | filedo | installer | 2611171800 | live | import | [link](https://api.github.com/repos/SerZhyAle/FileDO/releases/latest) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2611110217 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2611110217) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2610211540 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2610211540) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2610102316 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2610102316) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2609260512 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2609260512) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2609241700 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2609241700) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2607301014 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2607301014) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2606120121 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2606120121) | - |
| 2026-11-17 18:57:27+01:00 | filedo | winget | 2604272228 | live | import | [link](https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SerZhyAle/FileDO/2604272228) | - |
| 2026-11-17 18:42:30+01:00 | filedo | winget | 2611171800 | submitted | manual | [PR#482547](https://github.com/microsoft/winget-pkgs/pull/482547) | pull request opened by hand: wingetcreate refuses schema 1.28; checklist 7/7, install test passed |
| 2026-11-17 17:40:14Z | filedo | github | 2611171800 | live | import | [v2611171800](https://github.com/SerZhyAle/FileDO/releases/tag/v2611171800) | - |
| 2026-11-11 02:23:10Z | filedo | github | 2611110217 | live | import | [v2611110217](https://github.com/SerZhyAle/FileDO/releases/tag/v2611110217) | - |
| 2026-10-21 13:37:19Z | filedo | github | 2610211540 | live | import | [v2610211540](https://github.com/SerZhyAle/FileDO/releases/tag/v2610211540) | - |
| 2026-10-10 21:46:43Z | filedo | github | 2610102316 | live | import | [v2610102316](https://github.com/SerZhyAle/FileDO/releases/tag/v2610102316) | - |
| 2026-10-09 18:03:51Z | filedo | github | 2610091940 | live | import | [v2610091940](https://github.com/SerZhyAle/FileDO/releases/tag/v2610091940) | - |
| 2026-10-07 02:24:39Z | filedo | github | 2610070412 | live | import | [v2610070412](https://github.com/SerZhyAle/FileDO/releases/tag/v2610070412) | - |
| 2026-09-26 03:21:34Z | filedo | github | 2609260512 | live | import | [v2609260512](https://github.com/SerZhyAle/FileDO/releases/tag/v2609260512) | - |
| 2026-09-24 15:28:17Z | filedo | github | 2609241700 | live | import | [v2609241700](https://github.com/SerZhyAle/FileDO/releases/tag/v2609241700) | - |
| 2026-07-30 08:18:56Z | filedo | github | 2607301014 | live | import | [v2607301014](https://github.com/SerZhyAle/FileDO/releases/tag/v2607301014) | - |
| 2026-06-11 23:27:10Z | filedo | github | 2606120121 | live | import | [v2606120121](https://github.com/SerZhyAle/FileDO/releases/tag/v2606120121) | - |
| 2026-05-15 18:57:41Z | filedo | github | 2605152056 | live | import | [v2605152056](https://github.com/SerZhyAle/FileDO/releases/tag/v2605152056) | - |
| 2026-05-15 18:54:23Z | filedo | github | 2605152052 | live | import | [v2605152052](https://github.com/SerZhyAle/FileDO/releases/tag/v2605152052) | - |
| 2026-05-15 18:43:53Z | filedo | github | 2605152042 | live | import | [v2605152042](https://github.com/SerZhyAle/FileDO/releases/tag/v2605152042) | - |
| 2026-04-27 20:30:43Z | filedo | github | 2604272228 | live | import | [v2604272228](https://github.com/SerZhyAle/FileDO/releases/tag/v2604272228) | - |
| 2026-04-27 11:03:21Z | filedo | github | 2604271301 | live | import | [v2604271301](https://github.com/SerZhyAle/FileDO/releases/tag/v2604271301) | - |
