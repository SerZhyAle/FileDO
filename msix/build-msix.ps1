<#
.SYNOPSIS
  Build a Microsoft Store-ready MSIX package for FileDO (CLI).

.DESCRIPTION
  build filedo.exe -> compute Store version -> stage payload -> generate logos
  -> fill AppxManifest.xml -> makeappx pack -> (optional) self-sign for local test.

  For a STORE upload, pass the three identity values from Partner Center
  (Product > Product identity) and DO NOT use -SelfSign. Upload the unsigned .msix;
  Microsoft re-signs it during certification.

      .\msix\build-msix.ps1 -IdentityName "<Package/Identity/Name>" `
                            -Publisher "<Package/Identity/Publisher e.g. CN=...>" `
                            -PublisherDisplayName "<Package/Properties/PublisherDisplayName>"

  For a LOCAL test install, add -SelfSign. The script prints the two commands to
  trust the cert (run as admin) and install the package.

      .\msix\build-msix.ps1 -SelfSign

.NOTES
  Needs the Windows SDK (makeappx.exe + signtool.exe) and Go on PATH.
    winget install Microsoft.WindowsSDK.10.0.26100
  Run with Windows PowerShell 5.1 or pwsh 7 (the logo step uses System.Drawing).
#>
[CmdletBinding()]
param(
  # Defaults are placeholders; the real values come from Partner Center for a Store build.
  [string]$IdentityName        = "SerZhyAle.FileDO",
  [string]$Publisher           = "CN=SerZhyAle",
  [string]$PublisherDisplayName = "SerZhyAle",
  # Optional explicit Major.Minor.Build.0 (else computed from the current date/time).
  [string]$Version,
  [switch]$SelfSign
)

$ErrorActionPreference = "Stop"
$msix = $PSScriptRoot
$root = Split-Path $msix -Parent
$stage = Join-Path $msix "stage"
$outDir = Join-Path $msix "out"
$assetsOut = Join-Path $stage "Assets"

function Find-SdkTool([string]$name) {
  $kit = Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\bin"
  if (-not (Test-Path $kit)) { throw "Windows SDK not found under $kit. Install: winget install Microsoft.WindowsSDK.10.0.26100" }
  $dirs = Get-ChildItem $kit -Directory | Where-Object { $_.Name -match '^10\.' } | Sort-Object Name -Descending
  foreach ($d in $dirs) {
    $cand = Join-Path $d.FullName "x64\$name"
    if (Test-Path $cand) { return $cand }
  }
  throw "$name not found in any Windows SDK bin\x64 folder."
}

function Find-MSBuild {
  $vsw = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
  if (-not (Test-Path $vsw)) { throw "vswhere not found. Install VS Build Tools with the MSBuild component to build the GUI." }
  $mb = & $vsw -latest -products * -requires Microsoft.Component.MSBuild -find "MSBuild\**\Bin\MSBuild.exe" | Select-Object -First 1
  if (-not $mb -or -not (Test-Path $mb)) { throw "MSBuild.exe not found via vswhere." }
  return $mb
}

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

# --- versions -----------------------------------------------------------------
$now = Get-Date
$stamp = $now.ToString("yyMMddHHmm")            # app's own version (-X main.version), e.g. 2606141430
if ($Version) {
  if ($Version -notmatch '^\d+\.\d+\.\d+\.0$') { throw "Version must be Major.Minor.Build.0 (revision 0)." }
  $storeVer = $Version
} else {
  # Remap yyMMddHHmm -> YY.(M*100+D).HHmm.0 : monotonic over time, unique per minute, each part <= 65535.
  $major = [int]$now.ToString("yy")
  $minor = [int]$now.Month * 100 + [int]$now.Day
  $build = [int]$now.ToString("HHmm")
  $storeVer = "$major.$minor.$build.0"
}
foreach ($p in $storeVer.Split('.')) { if ([int]$p -gt 65535) { throw "Version part '$p' exceeds 65535." } }

Write-Host "FileDO MSIX build" -ForegroundColor Cyan
Write-Host "  app version : $stamp"
Write-Host "  store ver   : $storeVer"
Write-Host "  identity    : $IdentityName"
Write-Host "  publisher   : $Publisher"
Write-Host ""

# --- tools --------------------------------------------------------------------
$makeappx = Find-SdkTool "makeappx.exe"
$msbuild = Find-MSBuild
try { Add-Type -AssemblyName System.Drawing -ErrorAction Stop } catch { Add-Type -AssemblyName System.Drawing.Common }

# --- clean stage --------------------------------------------------------------
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Path $stage, $assetsOut, $outDir -Force | Out-Null

# --- build filedo.exe straight into the stage ---------------------------------
Write-Host "Building filedo.exe..." -NoNewline
$env:GOOS = "windows"; $env:GOARCH = "amd64"
Push-Location $root
try {
  go build "-ldflags=-X main.version=$stamp" -o (Join-Path $stage "filedo.exe") .\cmd\filedo 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
} finally { Pop-Location }
Write-Host " OK"

# --- build the GUI (filedo_win.exe) straight into the stage -------------------
Write-Host "Building filedo_win.exe (GUI)..." -NoNewline
& $msbuild (Join-Path $root "filedo_win_vb\FileDOGUI.vbproj") /p:Configuration=Release /p:Platform=AnyCPU /v:quiet /nologo | Out-Null
if ($LASTEXITCODE -ne 0) { throw "GUI build failed ($LASTEXITCODE)" }
$guiBin = Join-Path $root "filedo_win_vb\bin\Release"
Copy-Item (Join-Path $guiBin "filedo_win.exe") (Join-Path $stage "filedo_win.exe") -Force
if (Test-Path (Join-Path $guiBin "filedo_win.exe.config")) {
  Copy-Item (Join-Path $guiBin "filedo_win.exe.config") (Join-Path $stage "filedo_win.exe.config") -Force
}
Write-Host " OK"

# --- payload extras -----------------------------------------------------------
Copy-Item (Join-Path $root "LICENSE") (Join-Path $stage "LICENSE.txt") -Force

# --- logos --------------------------------------------------------------------
Write-Host "Generating logos..." -NoNewline
$iconSrc = Join-Path $root "assets\icon.png"
New-Logo $iconSrc (Join-Path $assetsOut "Square44x44Logo.png")   44
New-Logo $iconSrc (Join-Path $assetsOut "Square71x71Logo.png")   71
New-Logo $iconSrc (Join-Path $assetsOut "Square150x150Logo.png") 150
New-Logo $iconSrc (Join-Path $assetsOut "StoreLogo.png")         50
Write-Host " OK"

# --- fill manifest ------------------------------------------------------------
$manifest = Get-Content (Join-Path $msix "AppxManifest.xml") -Raw
$manifest = $manifest.Replace("{{IDENTITY_NAME}}", $IdentityName).
                      Replace("{{PUBLISHER}}", $Publisher).
                      Replace("{{PUBLISHER_DISPLAY_NAME}}", $PublisherDisplayName).
                      Replace("{{VERSION}}", $storeVer)
Set-Content -Path (Join-Path $stage "AppxManifest.xml") -Value $manifest -Encoding UTF8

# --- pack ---------------------------------------------------------------------
$outMsix = Join-Path $outDir "FileDO_$storeVer.msix"
Write-Host "Packing $outMsix..." -NoNewline
& $makeappx pack /o /d $stage /p $outMsix | Out-Null
if ($LASTEXITCODE -ne 0) { throw "makeappx failed ($LASTEXITCODE)" }
Write-Host " OK"

# --- optional self-sign for local testing -------------------------------------
if ($SelfSign) {
  $signtool = Find-SdkTool "signtool.exe"
  $cer = Join-Path $msix "filedo-selfsign.cer"
  $pfx = Join-Path $msix "filedo-selfsign.pfx"
  $pwd = "filedo-msix"
  Write-Host "Creating self-signed cert (Subject must match Publisher exactly)..."
  $cert = New-SelfSignedCertificate -Type Custom -Subject $Publisher `
            -KeyUsage DigitalSignature -FriendlyName "FileDO MSIX self-sign (test only)" `
            -CertStoreLocation "Cert:\CurrentUser\My" `
            -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3", "2.5.29.19={text}")
  $sec = ConvertTo-SecureString -String $pwd -Force -AsPlainText
  Export-PfxCertificate -Cert $cert -FilePath $pfx -Password $sec | Out-Null
  Export-Certificate   -Cert $cert -FilePath $cer | Out-Null
  & $signtool sign /fd SHA256 /a /f $pfx /p $pwd $outMsix
  if ($LASTEXITCODE -ne 0) { throw "signtool failed ($LASTEXITCODE)" }
  Write-Host ""
  Write-Host "Signed for LOCAL TEST. To trust + install, run:" -ForegroundColor Yellow
  Write-Host "  # 1) in an ADMIN PowerShell:"
  Write-Host "  Import-Certificate -FilePath `"$cer`" -CertStoreLocation Cert:\LocalMachine\TrustedPeople"
  Write-Host "  # 2) then:"
  Write-Host "  Add-AppxPackage -Path `"$outMsix`""
  Write-Host ""
  Write-Host "Run it:  filedo device c:" -ForegroundColor Green
  Write-Host "Remove:  Get-AppxPackage *FileDO* | Remove-AppxPackage"
} else {
  Write-Host ""
  Write-Host "Unsigned package ready for Partner Center upload:" -ForegroundColor Green
  Write-Host "  $outMsix"
}
