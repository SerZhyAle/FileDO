<#
.SYNOPSIS
  Build the FileDO Microsoft Store package (MSIX).

.DESCRIPTION
  version -> build filedo.exe + filedo_win.exe -> stage -> logos -> fill manifest ->
  makeappx pack -> read the packed manifest back and assert it.

  Three modes, and the mode decides what the output may be used for:

  STORE (default)  Unsigned package for Partner Center. Needs the reserved identity: pass
                   -IdentityName (Partner Center > Product > Product identity) or record it
                   once in msix\identity.json. Publisher and PublisherDisplayName default to
                   the SZA account values, which are the same for every SZA product.
                   Output: out\FileDO_<ver>.msix. Microsoft re-signs it at certification.

  -SelfSign        Local sideload test. Fixed TEST identity, signed with a self-signed cert.
                   Output: out\FileDO_<ver>_LOCALTEST.msix. NEVER upload this.

  -Register        Local test without any certificate: registers the staged loose layout for
                   the current user (needs Developer Mode). Same TEST identity as -SelfSign.
                   The only switch here that changes the machine; undo with
                   Get-AppxPackage SZA.FileDO.LocalTest | Remove-AppxPackage

.PARAMETER IdentityName
  Package/Identity/Name reserved in Partner Center (e.g. SZA.FileDO). Per product.

.PARAMETER Stamp
  The release stamp yyMMddHHmm (the git tag without the v). release.ps1 passes the tag's
  stamp so the exes inside the package carry the tag's version: filedo.exe prints it, and
  filedo_win.exe has it as its PE version (yy.M.d.HHmm) and BuildStamp. Default: now.

.PARAMETER Cli
  visible (default)  the console tool is a second, visible Start-menu app with the `filedo`
                     alias. hidden = AppListEntry="none": the Store rejects that shape at
                     upload unless the account holds the HeadlessAppBypass waiver.
                     none = ship the GUI only (the fallback if the Store rejects the CLI app).

.PARAMETER ExplorerCommand
  Opt-in (AUD-17-F1, owner decision 2026-09-26): build shellext\FileDOShell.dll, stage it, and keep
  the packaged Explorer command of SP-0020 in the manifest - the desktop4:FileExplorerContextMenus
  verb on every file and the com:SurrogateServer class that loads the DLL. Without it (the default,
  and what release.ps1 builds) the package carries neither extension nor the DLL, and the
  packed-manifest check fails a package that does. The Store listing and the README say the Store
  edition adds no Explorer entries: a Store package built with this switch contradicts them, and
  ships only once SP-0020's exit criterion and SP-0005 G3 are met and those surfaces change in one
  edit. Any mode takes it; -SelfSign / -Register with it is how SP-0020 is proven.

.EXAMPLE
  .\msix\build-msix.ps1 -IdentityName "SZA.FileDO"          # Store package
.EXAMPLE
  .\msix\build-msix.ps1 -Register                            # local run under MSIX, no cert
.EXAMPLE
  .\msix\build-msix.ps1 -SelfSign -ExplorerCommand           # local test WITH the Explorer command
.NOTES
  Needs Go, goversioninfo, VS Build Tools (MSBuild) and the Windows SDK (makeappx). With
  -ExplorerCommand also the VS C++ x64 tools (MSVC, for shellext\FileDOShell.dll - component
  Microsoft.VisualStudio.Component.VC.Tools.x86.x64).
    winget install Microsoft.WindowsSDK.10.0.26100
    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1
#>
[CmdletBinding()]
param(
    [string]$IdentityName,
    # Account-wide for the SZA publisher; identical for every SZA product.
    [string]$Publisher = "CN=F98ACEDB-1E22-4C39-AF63-F9FCFE807DCD",
    [string]$PublisherDisplayName = "SZA",
    [ValidatePattern('^\d{10}$')]
    [string]$Stamp,
    [ValidateSet('visible', 'hidden', 'none')]
    [string]$Cli = 'visible',
    [switch]$SelfSign,
    [switch]$Register,
    [switch]$ExplorerCommand
)

$ErrorActionPreference = "Stop"
$msix      = $PSScriptRoot
. (Join-Path $msix "msix-manifest.ps1")
$root      = Split-Path $msix -Parent
$stage     = Join-Path $msix "stage"
$outDir    = Join-Path $msix "out"
$assetsOut = Join-Path $stage "Assets"

# The identity a local test package carries. It must never equal the reserved Store identity:
# a different package family is what lets a test build sit beside (or before) the real one.
$TestIdentity  = "SZA.FileDO.LocalTest"
$TestPublisher = "CN=SZA-LocalTest"
$TestDisplay   = "SZA (local test)"

function Fail([string]$m) { throw "build-msix: $m" }

# --- mode + identity ---------------------------------------------------------
$testMode = $SelfSign -or $Register
if ($testMode) {
    foreach ($p in 'IdentityName', 'Publisher', 'PublisherDisplayName') {
        if ($PSBoundParameters.ContainsKey($p)) { Fail "-$p makes no sense with -SelfSign/-Register: a test build always carries the fixed test identity, so it can never pass for a Store package." }
    }
    $IdentityName = $TestIdentity; $Publisher = $TestPublisher; $PublisherDisplayName = $TestDisplay
} else {
    # The recorded reservation, if any. Two sources that disagree are an error, never a guess.
    $idFile = Join-Path $msix "identity.json"
    if (Test-Path $idFile) {
        $rec = Get-Content $idFile -Raw | ConvertFrom-Json
        $pairs = @(
            @{ Param = 'IdentityName';         Key = 'IdentityName' },
            @{ Param = 'Publisher';            Key = 'Publisher' },
            @{ Param = 'PublisherDisplayName'; Key = 'PublisherDisplayName' })
        foreach ($p in $pairs) {
            $recorded = $rec.($p.Key)
            if (-not $recorded) { continue }
            if ($PSBoundParameters.ContainsKey($p.Param)) {
                if ((Get-Variable $p.Param -ValueOnly) -cne $recorded) { Fail "-$($p.Param) '$((Get-Variable $p.Param -ValueOnly))' disagrees with msix\identity.json ('$recorded'). One of them is wrong; the Partner Center Product identity page decides." }
            } else {
                Set-Variable $p.Param $recorded
            }
        }
    }
    if (-not $IdentityName) {
        Fail "no Store identity. Reserve the app name in Partner Center (Create a new product > MSIX or PWA app), read Product > Product identity, then pass -IdentityName '<Package/Identity/Name>' or record it in msix\identity.json (see msix\README.md). For a local test use -Register."
    }
    if ($IdentityName -notmatch '^[A-Za-z0-9][A-Za-z0-9.\-]{2,49}$') { Fail "IdentityName '$IdentityName' is not a valid MSIX identity name (3-50 chars: letters, digits, '.', '-')." }
    if ($IdentityName -match 'LocalTest') { Fail "IdentityName '$IdentityName' is the local-test identity; a Store package cannot carry it." }
    # The placeholder Publisher ('CN=SerZhyAle') is exactly what once produced a Store-invalid
    # package from a bare build, so refuse anything that is not a Partner Center publisher id.
    if ($Publisher -notmatch '^CN=[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$') { Fail "Publisher '$Publisher' is not a Partner Center publisher id (CN=<GUID>); a Store package signed for anything else is rejected at upload." }
    if (-not $PublisherDisplayName) { Fail "PublisherDisplayName is empty." }
}

# --- version -----------------------------------------------------------------
# The stamp is the truth (yyMMddHHmm, the tag without the v). It is parsed as a REAL date, then
# remapped mechanically: YY.(M*100+D).HHmm.0. The Store reserves the revision (0), each part is
# <= 65535, and the MSIX schema forbids leading zeros, so HHmm goes through an int cast.
# T3-F2 / AUD-68-F1 (owner decision 2026-10-01): a Store package that carries a release stamp is
# built only from sources equal to that release's tag - HEAD's tree equals v<Stamp> outside winget\
# (the one folder release.ps1 commits after the tag) and nothing is changed or untracked outside
# winget\. Otherwise it would ship, under the tag's version, code no gate judged and that differs
# from GitHub's build of it. release.ps1 checks the same before it calls this script. A test
# package (-SelfSign/-Register) never ships and is not held to it. To rebuild an older release
# after main moved on: git worktree add ..\FileDO-v<Stamp> v<Stamp>, and run this script there.
if ($PSBoundParameters.ContainsKey('Stamp') -and -not $testMode) {
    $relTag = "v$Stamp"
    $null = git -C $root rev-parse -q --verify "refs/tags/$relTag^{commit}" 2>$null
    if ($LASTEXITCODE -ne 0) { Fail "-Stamp $Stamp names a release, but $relTag is not a local tag (git fetch origin refs/tags/${relTag}:refs/tags/${relTag}). A Store package with a release stamp is built only from that tag's sources." }
    git -C $root diff --quiet $relTag HEAD -- . ':!winget'
    $diffCode = $LASTEXITCODE
    if ($diffCode -eq 1) { Fail "HEAD's sources differ from $relTag outside winget\ (git diff --stat $relTag HEAD -- . ':!winget'). Build the Store package for $Stamp from the tagged sources: git worktree add ..\FileDO-$relTag $relTag, and run this script there." }
    if ($diffCode -ne 0) { Fail "could not compare HEAD with $relTag (git diff exit $diffCode)." }
    $tagDirty = @(git -C $root status --porcelain --untracked-files=all -- . ':!winget')
    if ($LASTEXITCODE -ne 0) { Fail "git status failed (exit $LASTEXITCODE)." }
    if ($tagDirty.Count) {
        $tagDirty | Select-Object -First 20 | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
        Fail "the working tree has $($tagDirty.Count) change(s) outside winget\ - a Store package for $Stamp is built only from the sources of $relTag. Commit or remove them, or use a worktree of $relTag."
    }
    Write-Host "  sources     : equal to $relTag (outside winget\)"
}
if (-not $Stamp) { $Stamp = Get-Date -Format "yyMMddHHmm" }
try { $when = [datetime]::ParseExact($Stamp, 'yyMMddHHmm', [Globalization.CultureInfo]::InvariantCulture) }
catch { Fail "stamp '$Stamp' is not a real yyMMddHHmm date." }
$vMaj = [int]$when.ToString('yy'); $vMin = [int]$when.ToString('MM'); $vPat = [int]$when.ToString('dd'); $vBld = [int]$when.ToString('HHmm')
$storeVer = "{0}.{1}.{2}.0" -f $vMaj, ($vMin * 100 + $vPat), $vBld
foreach ($part in $storeVer.Split('.')) {
    if ([int]$part -gt 65535) { Fail "version part '$part' of $storeVer exceeds 65535." }
    if ($part.Length -gt 1 -and $part.StartsWith('0')) { Fail "version part '$part' of $storeVer has a leading zero." }
}
if (-not $storeVer.EndsWith('.0')) { Fail "revision of $storeVer is not 0." }

$modeName = if ($testMode) { "LOCAL TEST (never upload)" } else { "STORE" }
Write-Host "FileDO MSIX build - $modeName" -ForegroundColor Cyan
Write-Host "  stamp       : $Stamp   (the exe inside prints this)"
Write-Host "  store ver   : $storeVer"
Write-Host "  identity    : $IdentityName"
Write-Host "  publisher   : $Publisher"
Write-Host "  display name: $PublisherDisplayName"
Write-Host "  CLI app     : $Cli"
Write-Host "  Explorer cmd: $(if ($ExplorerCommand) { 'ON (FileDOShell.dll + manifest verb, -ExplorerCommand)' } else { 'off (default; no Explorer entries, no FileDOShell.dll)' })"
if ($ExplorerCommand -and -not $testMode) {
    Write-Host "  WARNING: a Store package WITH the Explorer command. The listing and the README say the Store" -ForegroundColor Yellow
    Write-Host "           edition adds no Explorer entries; do not upload it until SP-0020's exit criterion and" -ForegroundColor Yellow
    Write-Host "           SP-0005 G3 are met and those surfaces change in one edit (AUD-17-F1)." -ForegroundColor Yellow
}
Write-Host ""

# --- tools -------------------------------------------------------------------
function Find-SdkTool([string]$name) {
    $kit = Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\bin"
    if (-not (Test-Path $kit)) { Fail "Windows SDK not found under $kit. Install: winget install Microsoft.WindowsSDK.10.0.26100" }
    $dirs = Get-ChildItem $kit -Directory | Where-Object { $_.Name -match '^10\.' } | Sort-Object Name -Descending
    foreach ($d in $dirs) {
        $cand = Join-Path $d.FullName "x64\$name"
        if (Test-Path $cand) { return $cand }
    }
    Fail "$name not found in any Windows SDK bin\x64 folder."
}
function Find-MSBuild {
    $vsw = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
    if (-not (Test-Path $vsw)) { Fail "vswhere not found. Install VS Build Tools with the MSBuild component to build the GUI." }
    $mb = & $vsw -latest -products * -requires Microsoft.Component.MSBuild -find "MSBuild\**\Bin\MSBuild.exe" | Select-Object -First 1
    if (-not $mb -or -not (Test-Path $mb)) { Fail "MSBuild.exe not found via vswhere." }
    return $mb
}
foreach ($t in 'go', 'goversioninfo') {
    if (-not (Get-Command $t -ErrorAction SilentlyContinue)) {
        Fail "'$t' is not on PATH.$(if ($t -eq 'goversioninfo') { ' Install: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1' })"
    }
}
# One toolchain for every channel (SP-0030 CI-03): the Store's filedo.exe is compiled by the Go
# that go.mod's `toolchain` line pins - the one release.yml installs and build.ps1 tests with.
$pinnedGo = if ((Get-Content (Join-Path $root "go.mod") -Raw) -match '(?m)^toolchain\s+(go\S+)\s*$') { $Matches[1] } else { $null }
if (-not $pinnedGo) { Fail "go.mod has no 'toolchain goX.Y.Z' line - the release toolchain is not pinned." }
$localGo = (& go env GOVERSION | Out-String).Trim()
if ($localGo -ne $pinnedGo) { Fail "the local Go is $localGo, but every channel builds with $pinnedGo (go.mod toolchain). Install it, or set GOTOOLCHAIN=$pinnedGo." }
$makeappx = Find-SdkTool "makeappx.exe"
$makepri  = Find-SdkTool "makepri.exe"
$msbuild  = Find-MSBuild
try { Add-Type -AssemblyName System.Drawing -ErrorAction Stop } catch { Add-Type -AssemblyName System.Drawing.Common }
Add-Type -AssemblyName System.IO.Compression.FileSystem

if ($Register) {
    $dev = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock' -ErrorAction SilentlyContinue
    if (-not $dev -or $dev.AllowDevelopmentWithoutDevLicense -ne 1) { Fail "-Register needs Developer Mode (Settings > System > For developers). Use -SelfSign instead, or turn it on." }
}

# A test package registered by an earlier -Register run lives IN stage\, which this run is about
# to rebuild: unregister that one (ours, test-only, and only when it points into stage\) instead
# of failing on locked files or leaving a registration whose files are gone.
$stale = @(Get-AppxPackage $TestIdentity -ErrorAction SilentlyContinue | Where-Object { $_.InstallLocation -like "$stage*" })
if ($stale.Count) {
    Write-Host "Unregistering the previous local-test package (its files are in stage\)..." -ForegroundColor DarkGray
    $stale | Remove-AppxPackage
}

# --- clean stage -------------------------------------------------------------
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Path $stage, $assetsOut, $outDir -Force | Out-Null

# --- filedo.exe: built the way the release workflow builds it ----------------
# goversioninfo embeds the PE version info AND cmd\filedo\app.manifest (UTF-8 code page,
# long paths). A plain `go build` embeds neither, which would make the Store copy behave
# differently from the zip and the MSI. -trimpath, CGO off, no -s -w (stripping raises the
# false-positive rate of AV heuristics for Go binaries) - all as in release.yml.
Write-Host "Building filedo.exe..." -NoNewline
$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
Push-Location (Join-Path $root "cmd\filedo")
try {
    $fv = "$vMaj.$vMin.$vPat.$vBld"
    goversioninfo -64 -ver-major $vMaj -ver-minor $vMin -ver-patch $vPat -ver-build $vBld `
        -product-ver-major $vMaj -product-ver-minor $vMin -product-ver-patch $vPat -product-ver-build $vBld `
        -file-version $fv -product-version $fv -o resource.syso versioninfo.json
    if ($LASTEXITCODE -ne 0) { Fail "goversioninfo failed ($LASTEXITCODE)" }
    $env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
    $goOut = & go build -trimpath -ldflags "-X main.version=$Stamp" -o (Join-Path $stage "filedo.exe") . 2>&1
    if ($LASTEXITCODE -ne 0) { Write-Host ""; Write-Host ($goOut | Out-String); Fail "go build failed ($LASTEXITCODE)" }
} finally {
    Remove-Item "resource.syso" -ErrorAction SilentlyContinue
    foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    Pop-Location
}
Write-Host " OK"

# The freshly built exe must print the stamp it was built with (the same smoke as build.ps1).
$smoke = & (Join-Path $stage "filedo.exe") "-?" 2>&1 | Out-String
if ($smoke -notmatch [regex]::Escape($Stamp)) { Fail "the built filedo.exe does not print the stamp $Stamp - the version wiring is broken." }

# --- filedo_win.exe (the VB.NET GUI) -----------------------------------------
# Stamped the way build.ps1 and release.yml stamp it (PKG-03): BuildStamp is the release stamp
# About and Send logs print, AssemblyVersion the PE version yy.M.d.HHmm. Without them the Store
# copy of the GUI was 0.0.0.0, and every Store install reported a file-time fallback.
Write-Host "Building filedo_win.exe (GUI)..." -NoNewline
$guiVersion = "$vMaj.$vMin.$vPat.$vBld"
$guiOut = & $msbuild (Join-Path $root "filedo_win_vb\FileDOGUI.vbproj") /t:Rebuild /p:Configuration=Release /p:Platform=AnyCPU /p:BuildStamp=$Stamp /p:AssemblyVersion=$guiVersion /v:quiet /nologo 2>&1
if ($LASTEXITCODE -ne 0) { Write-Host ""; Write-Host ($guiOut | Out-String); Fail "GUI build failed ($LASTEXITCODE)" }
$guiBin = Join-Path $root "filedo_win_vb\bin\Release"
Copy-Item (Join-Path $guiBin "filedo_win.exe") (Join-Path $stage "filedo_win.exe") -Force
# Read back, as build.ps1's smoke does: what was asked of MSBuild is not what was built until
# the staged exe says so.
$guiFileVersion = (Get-Item (Join-Path $stage "filedo_win.exe")).VersionInfo.FileVersion
if ($guiFileVersion -ne $guiVersion) { Fail "the staged filedo_win.exe has PE version '$guiFileVersion', want '$guiVersion' - the GUI build ignored the stamp." }
# The DPI declaration is two files and both must ship: the manifest inside the exe makes the
# process per-monitor aware, and this config is what makes WinForms rescale its controls.
$cfg = Join-Path $guiBin "filedo_win.exe.config"
if (-not (Test-Path $cfg)) { Fail "filedo_win.exe.config was not produced by the GUI build; shipping the manifest without it renders worse than shipping neither." }
Copy-Item $cfg (Join-Path $stage "filedo_win.exe.config") -Force
Write-Host " OK"

# --- FileDOShell.dll (the first-level Explorer command, SP-0020) -------------
# Opt-in (-ExplorerCommand, AUD-17-F1). With it the manifest declares the command, and a package
# that declares a COM class and lacks the DLL installs fine and then shows nothing, so the DLL is
# built and asserted in the package below. Without it neither the DLL nor the declaration ships.
if ($ExplorerCommand) {
    Write-Host "Building FileDOShell.dll (Explorer command)..." -NoNewline
    $shellOut = Join-Path $outDir "shellext"
    & (Join-Path $root "shellext\build-shellext.ps1") -OutDir $shellOut -FileVersion "$vMaj.$vMin.$vPat.$vBld" | Out-Null
    Copy-Item (Join-Path $shellOut "FileDOShell.dll") (Join-Path $stage "FileDOShell.dll") -Force
    Write-Host " OK"
}

Copy-Item (Join-Path $root "LICENSE") (Join-Path $stage "LICENSE.txt") -Force
Copy-Item (Join-Path $root "THIRD-PARTY-NOTICES.txt") (Join-Path $stage "THIRD-PARTY-NOTICES.txt") -Force

# --- logos -------------------------------------------------------------------
function New-Logo([string]$src, [string]$dst, [int]$size) {
    $img = [System.Drawing.Image]::FromFile($src)
    try {
        $bmp = New-Object System.Drawing.Bitmap($size, $size)
        $g = [System.Drawing.Graphics]::FromImage($bmp)
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.SmoothingMode     = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $g.PixelOffsetMode   = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $g.Clear([System.Drawing.Color]::Transparent)
        $g.DrawImage($img, 0, 0, $size, $size)
        $g.Dispose()
        $bmp.Save($dst, [System.Drawing.Imaging.ImageFormat]::Png)
        $bmp.Dispose()
    } finally { $img.Dispose() }
}
Write-Host "Generating logos..." -NoNewline
$iconSrc = Join-Path $root "assets\icon.png"
New-Logo $iconSrc (Join-Path $assetsOut "Square44x44Logo.png")   44
New-Logo $iconSrc (Join-Path $assetsOut "Square71x71Logo.png")   71
New-Logo $iconSrc (Join-Path $assetsOut "Square150x150Logo.png") 150
New-Logo $iconSrc (Join-Path $assetsOut "StoreLogo.png")         50

# Windows selects target-size and unplated logo variants only when resources.pri indexes them.
# Keep every derivative mechanically tied to the one FileDO mark rather than maintaining a
# second set of artwork that can drift from the MSI, EXE and site icon.
$logoVariants = New-Object System.Collections.Generic.List[string]
foreach ($size in 16, 24, 32, 48, 256) {
    foreach ($form in '', '_altform-unplated', '_altform-lightunplated') {
        $leaf = "Square44x44Logo.targetsize-$size$form.png"
        New-Logo $iconSrc (Join-Path $assetsOut $leaf) $size
        $logoVariants.Add("Assets\$leaf")
    }
}

# The .fd-sec and .fdd file types show their ICON-SET meanings (content.secret-file,
# content.disk-container), not the product mark (SP-0016 T8, ICON-SET rule 7; SP-0004 T6.27): their
# PNGs are cut from the same .ico files the MSI and the classic registration use, so the three
# channels show one picture. The Explorer command (FileDOShell.dll) reads the menu icons from icons\
# beside it, as the classic registration does.
$menuIconsSrc = Join-Path $root "assets\menu-icons"
$menuIconsOut = Join-Path $stage "icons"
New-Item -ItemType Directory -Force -Path $menuIconsOut | Out-Null
Copy-Item (Join-Path $menuIconsSrc "*.ico") $menuIconsOut -Force
$menuIconEntries = @(Get-ChildItem $menuIconsOut -Filter *.ico | ForEach-Object { "icons\$($_.Name)" })
if ($menuIconEntries.Count -ne 7) { Fail "assets\menu-icons holds $($menuIconEntries.Count) icons, expected 7 - run filedo_win.exe --write-menu-icons assets\menu-icons" }
function New-TypeLogo([string]$icoPath, [string]$dst, [int]$size) {
    $ico = New-Object System.Drawing.Icon($icoPath, $size, $size)
    try {
        $bmp = $ico.ToBitmap()
        try {
            if ($bmp.Width -ne $size) { Fail "$(Split-Path $icoPath -Leaf) has no $size px image" }
            $bmp.Save($dst, [System.Drawing.Imaging.ImageFormat]::Png)
        } finally { $bmp.Dispose() }
    } finally { $ico.Dispose() }
}
# One logo set per file type: <Base>.png (the name the manifest gives) and <Base>.targetsize-N.png.
# The 256 px image is a PNG inside the .ico, which System.Drawing.Icon skips; it is copied out
# byte for byte. The unqualified 44 px asset the manifest names is scaled from it (the .ico has
# no 44): Windows picks a targetsize form whenever one fits, so that one is the fallback only.
foreach ($type in @(@{ Ico = "content.secret-file.ico"; Base = "SecretFile" },
                    @{ Ico = "content.disk-container.ico"; Base = "DiskContainer" })) {
    $icoPath = Join-Path $menuIconsSrc $type.Ico
    $big = Join-Path $assetsOut "$($type.Base).targetsize-256.png"
    $icoBytes = [System.IO.File]::ReadAllBytes($icoPath)
    $png = $null
    for ($i = 0; $i -lt [BitConverter]::ToUInt16($icoBytes, 4); $i++) {
        $at = 6 + 16 * $i
        if ($icoBytes[$at] -eq 0) {
            $len = [BitConverter]::ToInt32($icoBytes, $at + 8); $off = [BitConverter]::ToInt32($icoBytes, $at + 12)
            $png = New-Object byte[] $len
            [Array]::Copy($icoBytes, $off, $png, 0, $len)
        }
    }
    if (-not $png -or $png[1] -ne 0x50) { Fail "$($type.Ico) carries no 256 px PNG image" }
    [System.IO.File]::WriteAllBytes($big, $png)
    New-Logo $big (Join-Path $assetsOut "$($type.Base).png") 44
    $logoVariants.Add("Assets\$($type.Base).png")
    foreach ($size in 16, 24, 32, 48, 256) {
        $leaf = "$($type.Base).targetsize-$size.png"
        if ($size -ne 256) { New-TypeLogo $icoPath (Join-Path $assetsOut $leaf) $size }
        $logoVariants.Add("Assets\$leaf")
    }
}
foreach ($entry in $menuIconEntries) { $logoVariants.Add($entry) }
Write-Host " OK"

# --- manifest ----------------------------------------------------------------
# msix-manifest.ps1 fills the template and shapes it: the CLI application (-Cli) and, only with
# -ExplorerCommand, the packaged Explorer command (the template carries it; a default build drops it).
try {
    $xml = New-FileDOManifest -TemplatePath (Join-Path $msix "AppxManifest.xml") -IdentityName $IdentityName `
        -Publisher $Publisher -PublisherDisplayName $PublisherDisplayName -Version $storeVer -Cli $Cli `
        -ExplorerCommand:$ExplorerCommand
} catch { Fail $_.Exception.Message }
$ns = New-FileDOManifestNs $xml
[System.IO.File]::WriteAllText((Join-Path $stage "AppxManifest.xml"), $xml.OuterXml, (New-Object System.Text.UTF8Encoding($false)))

# A PRI is not cosmetic: without it Windows ignores the targetsize-* and unplated forms above.
# Generate the config on every build (the SDK owns its schema), then index the final staged manifest
# and assets. The manifest remains the Store's language declaration, so assert all five shipped
# languages before and after packing rather than assuming a resource index cannot narrow them.
$expectedResourceLanguages = @('en-us', 'ru', 'uk', 'de', 'fr')
$manifestResourceLanguages = @($xml.SelectNodes('/m:Package/m:Resources/m:Resource', $ns) | ForEach-Object { $_.GetAttribute('Language') })
if (($manifestResourceLanguages -join ',') -cne ($expectedResourceLanguages -join ',')) {
    Fail "manifest Resource languages are '$($manifestResourceLanguages -join ',')', expected '$($expectedResourceLanguages -join ',')'. The PRI must not narrow Store listing reach."
}
$priConfig = Join-Path $outDir 'resources.priconfig.xml'
$priFile = Join-Path $stage 'resources.pri'
Write-Host "Indexing logo resources..." -NoNewline
$priOut = & $makepri createconfig /cf $priConfig /dq en-US /pv 10.0.0 /o 2>&1
if ($LASTEXITCODE -ne 0) { Write-Host ""; Write-Host ($priOut | Out-String); Fail "makepri createconfig failed ($LASTEXITCODE)" }
$priOut = & $makepri new /pr $stage /cf $priConfig /mn (Join-Path $stage 'AppxManifest.xml') /of $priFile /o 2>&1
if ($LASTEXITCODE -ne 0) { Write-Host ""; Write-Host ($priOut | Out-String); Fail "makepri new failed ($LASTEXITCODE)" }
if (-not (Test-Path $priFile) -or (Get-Item $priFile).Length -eq 0) { Fail "makepri did not produce a non-empty resources.pri." }
Write-Host " OK"

# --- pack --------------------------------------------------------------------
$suffix  = if ($testMode) { "_LOCALTEST" } else { "" }
$outMsix = Join-Path $outDir "FileDO_$storeVer$suffix.msix"
Write-Host "Packing $outMsix..." -NoNewline
$packOut = & $makeappx pack /o /d $stage /p $outMsix 2>&1
if ($LASTEXITCODE -ne 0) { Write-Host ""; Write-Host ($packOut | Out-String); Fail "makeappx failed ($LASTEXITCODE)" }
Write-Host " OK"

# --- read the packed package back and assert it ------------------------------
# What was asked for is not what was packed until the packed manifest says so.
Write-Host "Verifying the packed manifest..." -NoNewline
$zip = [System.IO.Compression.ZipFile]::OpenRead($outMsix)
try {
    $entries = @($zip.Entries | ForEach-Object { [uri]::UnescapeDataString($_.FullName).Replace('/', '\') })
    $entry = $zip.GetEntry('AppxManifest.xml')
    if (-not $entry) { Fail "the package has no AppxManifest.xml." }
    $reader = New-Object System.IO.StreamReader($entry.Open())
    try { $packed = [xml]$reader.ReadToEnd() } finally { $reader.Dispose() }
} finally { $zip.Dispose() }

$pns = New-Object System.Xml.XmlNamespaceManager($packed.NameTable)
$pns.AddNamespace('m',      'http://schemas.microsoft.com/appx/manifest/foundation/windows10')
$pns.AddNamespace('uap5', 'http://schemas.microsoft.com/appx/manifest/uap/windows10/5')
$pns.AddNamespace('desktop4', 'http://schemas.microsoft.com/appx/manifest/desktop/windows10/4')
$pns.AddNamespace('uap',    'http://schemas.microsoft.com/appx/manifest/uap/windows10')
$pns.AddNamespace('rescap', 'http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities')
$id = $packed.SelectSingleNode('/m:Package/m:Identity', $pns)
$problems = New-Object System.Collections.ArrayList
if ($id.GetAttribute('Name')      -cne $IdentityName) { [void]$problems.Add("Identity Name is '$($id.GetAttribute('Name'))', expected '$IdentityName'") }
if ($id.GetAttribute('Publisher') -cne $Publisher)    { [void]$problems.Add("Identity Publisher is '$($id.GetAttribute('Publisher'))', expected '$Publisher'") }
if ($id.GetAttribute('Version')   -cne $storeVer)     { [void]$problems.Add("Identity Version is '$($id.GetAttribute('Version'))', expected '$storeVer'") }
if ($id.GetAttribute('ProcessorArchitecture') -ne 'x64') { [void]$problems.Add("ProcessorArchitecture is not x64") }
$pdn = $packed.SelectSingleNode('/m:Package/m:Properties/m:PublisherDisplayName', $pns).InnerText
if ($pdn -cne $PublisherDisplayName) { [void]$problems.Add("PublisherDisplayName is '$pdn', expected '$PublisherDisplayName'") }
$caps = @($packed.SelectNodes('/m:Package/m:Capabilities/*', $pns) | ForEach-Object { $_.GetAttribute('Name') })
if (($caps -join ',') -cne 'runFullTrust') { [void]$problems.Add("capabilities are '$($caps -join ',')', expected exactly runFullTrust (one restricted capability to justify)") }
$apps = @($packed.SelectNodes('/m:Package/m:Applications/m:Application', $pns))
$expectApps = if ($Cli -eq 'none') { 1 } else { 2 }
if ($apps.Count -ne $expectApps) { [void]$problems.Add("$($apps.Count) applications packed, expected $expectApps") }
foreach ($a in $apps) {
    $exe = $a.GetAttribute('Executable')
    if ($entries -notcontains $exe) { [void]$problems.Add("Application $($a.GetAttribute('Id')) names $exe, which is not in the package") }
    $ve = $a.SelectSingleNode('uap:VisualElements', $pns)
    $hiddenApp = $ve.GetAttribute('AppListEntry') -eq 'none'
    if ($hiddenApp -and $Cli -ne 'hidden') { [void]$problems.Add("Application $($a.GetAttribute('Id')) is hidden, which the Store rejects as a headless app") }
    foreach ($attr in 'Square150x150Logo', 'Square44x44Logo') {
        if ($entries -notcontains $ve.GetAttribute($attr)) { [void]$problems.Add("Application $($a.GetAttribute('Id')) references a missing $attr") }
    }
}
foreach ($p in (Test-ApplicationStartup -Manifest $packed)) { [void]$problems.Add($p) }
$fdsecTypes = @($packed.SelectNodes('/m:Package/m:Applications/m:Application[@Id="FileDOGui"]/m:Extensions/uap:Extension[@Category="windows.fileTypeAssociation"]/uap:FileTypeAssociation[@Name="filedo.securecontainer"]/uap:SupportedFileTypes/uap:FileType', $pns) | ForEach-Object { $_.InnerText })
if (($fdsecTypes -join ',') -cne '.fd-sec') {
    [void]$problems.Add("FileDOGui .fd-sec association is '$($fdsecTypes -join ',')', expected .fd-sec")
}
# SP-0004 T6.27: .fdd once, for the GUI app, its logo packed, and no verb - this build cannot mount.
foreach ($p in (Test-DiskContainerAssociation -Manifest $packed -Entries $entries)) { [void]$problems.Add($p) }
# SP-0020 / AUD-17-F1: the Explorer command matches the switch. Default: neither the
# fileExplorerContextMenus nor the comServer extension, and no FileDOShell.dll. -ExplorerCommand:
# one CLSID in three places (verb, COM class, the DLL's source) and the DLL in the package.
$srcClsid = if ($ExplorerCommand) { Get-FileDOShellClsid (Join-Path $root "shellext\FileDOShell.cpp") } else { $null }
foreach ($p in (Test-ExplorerCommand -Manifest $packed -Entries $entries -ExplorerCommand ([bool]$ExplorerCommand) -SourceClsid $srcClsid)) { [void]$problems.Add($p) }
foreach ($need in 'filedo_win.exe.config', 'LICENSE.txt', 'THIRD-PARTY-NOTICES.txt', 'Assets\StoreLogo.png') {
    if ($entries -notcontains $need) { [void]$problems.Add("$need is not in the package") }
}
foreach ($need in @('resources.pri') + $logoVariants) {
    if ($entries -notcontains $need) { [void]$problems.Add("$need is not in the package") }
}
$packedResourceLanguages = @($packed.SelectNodes('/m:Package/m:Resources/m:Resource', $pns) | ForEach-Object { $_.GetAttribute('Language') })
if (($packedResourceLanguages -join ',') -cne ($expectedResourceLanguages -join ',')) {
    [void]$problems.Add("packed Resource languages are '$($packedResourceLanguages -join ',')', expected '$($expectedResourceLanguages -join ',')'; resources.pri must not narrow Store listing reach")
}
if (-not $testMode -and ($IdentityName -match 'LocalTest' -or $Publisher -match 'LocalTest')) { [void]$problems.Add("a Store package carries the local-test identity") }
if ($problems.Count) {
    Write-Host " FAILED" -ForegroundColor Red
    $problems | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    Fail "$($problems.Count) problem(s) in the packed manifest; $outMsix must not be uploaded."
}
Write-Host " OK" -ForegroundColor Green

# Signing changes the package bytes. Finish it before writing the integrity sidecar.
if ($SelfSign) {
    $signtool = Find-SdkTool "signtool.exe"
    $friendly = "FileDO MSIX local test (never upload)"
    $cert = Get-ChildItem "Cert:\CurrentUser\My" | Where-Object { $_.Subject -eq $Publisher -and $_.FriendlyName -eq $friendly } | Sort-Object NotAfter | Select-Object -Last 1
    if (-not $cert) {
        $cert = New-SelfSignedCertificate -Type Custom -Subject $Publisher -KeyUsage DigitalSignature -FriendlyName $friendly `
                    -CertStoreLocation "Cert:\CurrentUser\My" `
                    -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3", "2.5.29.19={text}")
    }
    & $signtool sign /fd SHA256 /sha1 $cert.Thumbprint $outMsix
    if ($LASTEXITCODE -ne 0) { Fail "signtool failed ($LASTEXITCODE)" }
    $cer = Join-Path $outDir "filedo-localtest.cer"
    Export-Certificate -Cert $cert -FilePath $cer | Out-Null
}

$sha = (Get-FileHash $outMsix -Algorithm SHA256).Hash
[System.IO.File]::WriteAllText("$outMsix.sha256", "$sha  $(Split-Path $outMsix -Leaf)`n", [System.Text.Encoding]::ASCII)
Write-Host ("  packed: {0}  ({1:N1} MB)" -f (Split-Path $outMsix -Leaf), ((Get-Item $outMsix).Length / 1MB))
Write-Host "  sha256: $sha"

# --- what to do with it ------------------------------------------------------
if ($SelfSign) {
    Write-Host ""
    Write-Host "Signed for LOCAL TEST only. To trust + install:" -ForegroundColor Yellow
    Write-Host "  # 1) in an ADMIN PowerShell:"
    Write-Host "  Import-Certificate -FilePath `"$cer`" -CertStoreLocation Cert:\LocalMachine\TrustedPeople"
    Write-Host "  # 2) then:"
    Write-Host "  Add-AppxPackage -Path `"$outMsix`""
    Write-Host "Remove:  Get-AppxPackage $TestIdentity | Remove-AppxPackage"
} elseif ($Register) {
    Get-AppxPackage $TestIdentity -ErrorAction SilentlyContinue | Remove-AppxPackage -ErrorAction SilentlyContinue
    Add-AppxPackage -Register (Join-Path $stage "AppxManifest.xml")
    $pkg = Get-AppxPackage $TestIdentity
    Write-Host ""
    Write-Host "Registered for this user (loose layout, no certificate): $($pkg.PackageFullName)" -ForegroundColor Yellow
    Write-Host "  GUI  : explorer.exe shell:AppsFolder\$($pkg.PackageFamilyName)!FileDOGui   (Add-AppxPackage does not launch)"
    Write-Host "  CLI  : open a NEW terminal and run: filedo -?"
    Write-Host "  Remove: Get-AppxPackage $TestIdentity | Remove-AppxPackage"
} elseif ($ExplorerCommand) {
    Write-Host ""
    Write-Host "Unsigned Store package WITH the Explorer command - do NOT upload it while the listing says the" -ForegroundColor Yellow
    Write-Host "Store edition adds no Explorer entries (AUD-17-F1; SP-0020 exit criterion, SP-0005 G3):" -ForegroundColor Yellow
    Write-Host "  $outMsix"
} else {
    Write-Host ""
    Write-Host "Unsigned Store package - upload to Partner Center (Microsoft re-signs at certification):" -ForegroundColor Green
    Write-Host "  $outMsix"
    Write-Host "Next: msix\README.md, sections 3-5 (listing import, screenshots, submission)." -ForegroundColor DarkGray
}
