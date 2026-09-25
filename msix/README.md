# FileDO - Microsoft Store (MSIX) publishing

Everything the Store leg needs, from the reserved name to the submitted listing. Publishing is
**manual** in the last step (Partner Center has no submission CLI), and the free path needs no
code-signing certificate: the package is uploaded **unsigned** and Microsoft re-signs it at
certification. Nothing here tags, uploads or submits anything; that is [`RELEASE.md`](../RELEASE.md)
and the owner's decision.

| Path | Role |
| --- | --- |
| `AppxManifest.xml` | Manifest **template**: placeholders for the identity and version, `runFullTrust`, two applications (the window and the console tool), five languages. |
| `build-msix.ps1` | version, build both exes, stage, logos, fill manifest, `makeappx pack`, **read the packed manifest back and assert it**. Three modes: Store, `-SelfSign`, `-Register`. |
| `stage/resources.pri` | Generated resource index. It is what lets Windows select the target-size and unplated forms of the 44 px logo; never edit or package a PRI by hand. |
| `identity.json` | The reserved Store identity, recorded once (**absent until the name is reserved**, see section 2). |
| `listing/<code>.txt` | The listing copy per language (`en ru uk de fr`) + `shared.txt`. **The single source**; the console is a render target. |
| `build-store-listing-csv.ps1` | Patches a fresh Partner Center export with `listing/*.txt`, keeping the export's own style. |
| `make-screenshots.ps1` | Captures the window per page and language into `screenshots/`. |
| `screenshots/` | `<page>-<locale>.png`, 1920x1200, dark theme. Order = `DesktopScreenshot1..5`. |
| `store-listing.md` | What the CSV does **not** carry: the `runFullTrust` justification, the export-compliance answer, the privacy declaration. |
| `test-store-tools.ps1`, `testdata/` | Self-test of the listing sources and the CSV builder (50 checks, no Partner Center session). |
| `stage/`, `out/` | Generated, git-ignored. `out/FileDO_<ver>.msix` is what you upload. |

## 1. Prerequisites

```powershell
winget install Microsoft.WindowsSDK.10.0.26100                                 # makeappx.exe, signtool.exe
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1   # PE version + manifest, as in CI
# Go and VS Build Tools (MSBuild, for filedo_win.exe) must be present too.
```

## 2. Identity - reserve once, record once

Two values are **account-wide** and are the defaults of `build-msix.ps1`:
`Publisher = CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD`, `PublisherDisplayName = SZA`.
Only `Package/Identity/Name` is per product, and it exists only after the name is reserved:

1. Partner Center > **Apps and games > New product > MSIX or PWA app** > reserve the name **FileDO**.
   The reservation is permanent: choose the spelling once.
2. **Product > Product identity**: read `Package/Identity/Name`, `Publisher`, `PublisherDisplayName`.
   The other SZA products follow `SZA.<name>` (`SZA.CyrFlip`, `SZA.StreamsPlayer`,
   `SZA.Doc-HTML-Translate`), so `SZA.FileDO` is the *expected* value - confirm it on that page, do not assume it.
3. Record it here, once, exactly as shown (this is a frozen anchor: changing it orphans installed copies):

```json
{
  "IdentityName": "<Package/Identity/Name>",
  "Publisher": "CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD",
  "PublisherDisplayName": "SZA",
  "StoreId": "<9N.. product id>",
  "PackageFamilyName": "<Name>_fdk7e19xt9z9j",
  "IarcRatingId": "<from section 6>"
}
```

**Reserved identity** - done, recorded in `msix\identity.json` on **2026-09-22**:
Name `SZA.FileDO` | Store ID `9PH1LPCMRG83` | PFN `SZA.FileDO_fdk7e19xt9z9j` | IARC `-` (section 6, at
the first submission). The product sits **In draft**; the reservation expires if nothing is submitted
within three months of that date, i.e. by **2026-12-22**.

A **bare build can no longer produce a Store-invalid package**: without an identity (`-IdentityName`
or `identity.json`) the script refuses, and it also refuses any Publisher that is not a
`CN=<GUID>` (the old placeholder `CN=SerZhyAle` is what once made a bare build Store-invalid).

## 3. Build the package

```powershell
.\msix\build-msix.ps1                                  # Store package (identity from identity.json)
.\msix\build-msix.ps1 -IdentityName "SZA.FileDO"       # or pass it explicitly (must equal identity.json)
.\msix\build-msix.ps1 -Stamp 2609211530                # pin to a release tag's stamp (release.ps1 does this)
```

Output `out\FileDO_<ver>.msix` (unsigned) and a `.sha256`. What it does and asserts:

- **Version.** The stamp `yyMMddHHmm` (the tag without the `v`) is parsed as a real date and remapped
  mechanically to `YY.(M*100+D).HHmm.0`, e.g. `2609211530` gives `26.921.1530.0`. Revision 0, every part
  <= 65535, no leading zeros (`HHmm` goes through an int cast). `filedo.exe` inside prints the same stamp
  (checked by a smoke run). The remap is monotonic, but it must also exceed the version already published
  in the dashboard - look.
- **Binaries** are built as the release workflow builds them: `goversioninfo` embeds the PE version info
  and `cmd\filedo\app.manifest` (UTF-8 code page, long paths), `-trimpath`, `CGO_ENABLED=0`, no `-s -w`.
  `filedo_win.exe.config` ships beside the GUI or its DPI declaration does nothing.
- **Read-back.** The packed `AppxManifest.xml` is opened from the `.msix` and checked: Identity
  Name/Publisher/Version, PublisherDisplayName, exactly one capability (`runFullTrust`), every
  `Executable` and logo present, the 16/24/32/48/256 target-size/unplated logo variants and
  `resources.pri`, all five manifest languages, and no hidden application. A mismatch aborts before the
  file is offered.

### Two applications, and the "headless" rule

The package ships the window (`FileDOGui`, tile **FileDO**) and the console tool (`FileDO`, tile **FileDO CLI**,
alias `filedo` on PATH). The console tile is **visible** on purpose: the Store rejects a hidden application
(`AppListEntry="none"`) at upload with *"The package specifies a headless app. You don't have permission to
create a headless app"* - measured on this account with doc-html-translate. If the upload is refused anyway:

| Flag | Shape |
| --- | --- |
| `-Cli visible` (default) | two Start-menu tiles, `filedo` on PATH |
| `-Cli hidden` | the original shape; only for an account that holds the `HeadlessAppBypass` waiver |
| `-Cli none` | the window only; the CLI file stays in the package but there is no `filedo` alias |

WACK has its own opinion about this shape, and it is worth knowing before the upload rather than after:
its *Application count* test fails the two-application package with *"The Microsoft Store does not support
a package containing more than one applications defined in the manifest."* The test is marked optional and
the overall verdict stays a warning, so it is not automatically a refusal - but it is the same ground the
headless rule stands on, and `-Cli none` is the answer if Partner Center agrees with it.

### Test the package locally

```powershell
.\msix\build-msix.ps1 -Register     # no certificate, needs Developer Mode; registers the staged layout
.\msix\build-msix.ps1 -SelfSign     # signed with a self-signed cert; trusting it needs an ADMIN import (printed)
Get-AppxPackage SZA.FileDO.LocalTest | Remove-AppxPackage   # undo either
```

Both use the fixed test identity `SZA.FileDO.LocalTest` / `CN=SZA-LocalTest` and write
`FileDO_<ver>_LOCALTEST.msix`, which can never be mistaken for the upload. `Add-AppxPackage` installs but
does not launch: start from the Start menu or `explorer.exe shell:AppsFolder\<PFN>!FileDOGui`, and open a
**new** terminal for `filedo`.

Measured on Windows 11 build 26200 with a packaged process (`GetPackageFullName` confirmed): the
`fdsec reveal` sandbox is written to the real `%LOCALAPPDATA%\FileDO\reveal`, not into the package's
`LocalCache`, so a handler outside the package can read it. Older builds were not tested; if a reviewer
reports that *Open a secret file* cannot open the copy, look at file-system write virtualization first.

### Check it before submitting it

```powershell
# from an ADMIN PowerShell - appcert refuses to run, or even to print its help, without one
.\msix\build-msix.ps1 -SelfSign
.\msix\test-store-package.ps1 -TrustTestCert
```

`test-store-package.ps1` runs the Windows App Certification Kit
(`%ProgramFiles(x86)%\Windows Kits\10\App Certification Kit\appcert.exe`, its own kit directory - not the
SDK `bin\<version>\x64` where `makeappx` and `signtool` live) over the local-test package, and checks that
the privacy-policy URL the listing carries answers 200 (Store policy 10.5). `-TrustTestCert` imports the
self-signed certificate that `-SelfSign` exported, because appcert installs what it tests.

Read the verdict from the report, not from the exit code: appcert returns 0 for a session that ran and
found failures, and rolls an *optional* test's FAIL up into `OVERALL_RESULT=WARNING`. The script prints
every test that is not `PASS` with the Store's own wording, optional ones included - those are what a
submission has to be argued through. The report itself stays at `msix\out\wack-report.xml`.

What it cannot tell you: anything about the reserved Store identity or Microsoft's re-signing. The
local-test package has neither, and both are decided at upload.

## 4. The listing

1. **Add every language first**: the submission > Store listings > *Manage additional languages*
   (`en-us` is there already; add `ru`, `uk`, `de`, `fr`). An import cannot create a column, and a language
   that is not already there has its copy dropped **silently**.
2. **Export** the listing and save it as `msix\out\listingData.export.csv`. Take a fresh export every time:
   it carries the current asset URLs and defines which columns the next import accepts.
3. Self-test, then merge:

```powershell
.\msix\test-store-tools.ps1                            # sources + builder (once per change to either)
.\msix\build-store-listing-csv.ps1 -FillNothing        # byte-identical round trip on THIS export, or stop
.\msix\build-store-listing-csv.ps1 -Screenshots        # first submission: copy + screenshots
.\msix\build-store-listing-csv.ps1 -Refresh ReleaseNotes   # every later release: "What's new" only
```

4. **Import** `msix\out\store-import\` - the folder, in the console's own folder picker. The CSV and the
   images travel together. (The console offers no zip upload; the builder still writes `store-import.zip`
   only as a copy to keep.) Rules that were measured, all of them:
   - The import is **all-or-nothing per language**, and *only* per language: on 2026-09-22 one file put
     `en-us`, `ru` and `uk` in completely - text and all five screenshots each - while `de` and `fr` were
     refused. The console named the two languages and gave **no field-level detail**, so the failing field
     has to be found by bisection.
   - **Bisect with `-Only`.** Because the builder fills only *empty* cells, a re-run after a partial import
     targets exactly the languages that failed; `-Only Description`, `-Only 'Feature*'`, `-Only 'SearchTerm*'`,
     `-Only ReleaseNotes` are four small files, and the first one refused names the field. Start from a file
     that is a **byte-identical copy of the export**: if even that is refused, nothing in the copy is at fault.
   - A bare CSV upload cannot carry new screenshots, and **a bare file name in the cell is refused even
     through the folder/zip import**. The value has to be the path *including the import root folder's
     name* - `store-import/capacity-ru.png` - which is what the export's Type column means by "Relative
     path (or URL to file in Partner Center)". Measured 2026-09-22: `capacity-ru.png` on its own answers
     "The value you provided is not valid (capacity-ru.png)" and drops all five languages, text included.
     The builder writes the prefix and archives the folder itself, so the paths resolve inside the zip.
     Partner Center turns them into its own asset URLs, which the next export carries.
   - A listing is **Incomplete** until it has a description *and* a screenshot, and the console reports nothing.
   - Never copy `OverrideLogosForWin10 = True` into a language with no `StoreLogo` rows of its own.
   - The builder writes the export back in **the export's own style**. A real export is UTF-8 with BOM,
     CRLF between records, LF inside cells, minimal quoting and no final newline, and the builder reproduces
     exactly that; the canon's "no BOM, every field quoted" line describes a different target and is left to
     the first real import to settle. `-FillNothing` is the guard either way.
5. `ReleaseNotes` is refreshed per release with `-Refresh ReleaseNotes`. **On a first submission the Store's
   own guidance is to leave "What's new in this version" blank**; it was sent anyway on 2026-09-22 and
   `en-us`, `ru` and `uk` accepted it, so it is not what refuses a language - but it is the first thing to
   drop if certification objects.
6. Two content rules from the same page, neither enforced at import: the description must carry **no URLs**
   (ours ends with the GitHub link in all five languages - the designated place is the support website
   field on Properties), and product features are capped at 200 characters and 20 items.

**Where the listing stands (2026-09-22).** `en-us`, `ru`, `uk`: complete - copy, features, search terms,
release notes and five screenshots each, the screenshot cells now holding `developer.microsoft.com` asset
URLs. `de`, `fr`: empty, refused by the import with no reason given; the field that refuses them has not
been identified yet, and the four `-Only` files above are how to find it.

## 5. Screenshots

```powershell
.\msix\build-msix.ps1 -Register               # stages the exes (any mode does)
.\msix\make-screenshots.ps1                   # 5 pages x 5 languages, dark, about 3.5 minutes
.\msix\make-screenshots.ps1 -Theme light -OutDir <folder>  # the light set of the same 25
.\msix\make-screenshots.ps1 -Language en -Page capacity    # one shot
```

The script chooses each page's rail row through **UI Automation** - every row is named `rail:<key>` and
has a default action (APP-BEHAVIOUR rule 9) - so it clicks nothing, but it does bring the window to the
top for a few seconds per shot and parks the mouse pointer in a corner: run it when the machine is idle. It
snapshots and restores the developer's `HKCU\Software\FileDO` settings, kills the window instead of closing
it (a graceful close writes the window placement), sizes the client area in design pixels scaled by the
window's DPI, and never saves a shot in which a control painted as a WinForms red-cross placeholder. **Dark
is the default** and is the Store set; `-Theme light` makes the other half of the APP-STYLE light/dark pair.
(The light theme used to paint the selected rail row as exactly that placeholder - an `OverflowException` in
the palette's colour mix, fixed in `Theme.vb`.) `-Live` grabs the real screen instead of `PrintWindow`, to
tell a real defect from an artefact. Store rule: PNG, 1366x768 up to 3840x2160.

## 6. Submission click path (manual)

**First submission**: Apps and games > FileDO >
1. **Packages**: upload `out\FileDO_<ver>.msix`. A *headless app* refusal here means `-Cli none` (section 3).
2. **Store listings**: import (section 4). Check each language shows a description **and** screenshots.
3. **Properties**: every field of that page is written out in `store-listing.md` (category
   `Utilities + tools` > `Backup + manage`, secondary `Security`, personal information **yes**, privacy
   policy URL, support website and contact). Copy from there, decide nothing in the console.
   Product declarations: **uses cryptography = yes** and the answer in `store-listing.md` (export compliance).
4. **Age ratings**: file a **fresh IARC questionnaire** - the SZA portfolio rating id belongs to a different
   content profile and does not transfer. Answer honestly: a local storage utility, no accounts, purchases, ads
   or user-to-user content. A stricter rating than the other SZA apps is a *coverage regression* to decide before
   the release, not after. Record the resulting id in `identity.json`.
5. **Pricing and availability**: Free, all markets offered to the other SZA apps (never fewer than a previous release).
6. Data collection form: **no data collected** (no networking code, no `internetClient` capability), consistent
   with `docs/privacy.html`. Paste the `runFullTrust` justification from `store-listing.md`.
7. Submit for certification (a few business days).

**Update**: *Create new submission* > replace the package (same identity, higher version) > `-Refresh ReleaseNotes`
import > submit. Then **update over the real prior install** and check the version, the listing and the notes -
a fresh install cannot catch an identity mistake.

## 7. What the Store build does not do

- No classic registry context-menu entries. From SP-0020 (built 2026-09-25, not yet released) the package
  declares the `File DO..` group instead as a packaged Explorer command - `FileDOShell.dll`, built by
  `shellext\build-shellext.ps1` on every `build-msix.ps1` run and hosted in a COM surrogate - which is what
  reaches the Windows 11 first-level menu. `build-msix.ps1` fails the package if the DLL is missing or the
  CLSID of the verb, the COM class and the DLL source disagree. It also declares the `.fd-sec` association, so
  double-clicking a container starts the GUI with that file. Do not advertise the first-level menu in the
  listing until it has been seen in the Store build (SP-0020 section 5).
- `filedo fdsec register` must **not** be advertised for this build. Inside the package it refuses as a usage error
  (exit 2) and writes nothing: Windows keeps a packaged app's `Software\Classes` writes to the app, where Explorer
  never looks, and the command lines would point into `C:\Program Files\WindowsApps\..`, which Explorer cannot run.
- The window needs .NET Framework 4.8, which is in the box from Windows 10 1903; the manifest floor
  (`10.0.18362.0`) says so. The MSI and zip channels have no such floor.
