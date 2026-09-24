<#
.SYNOPSIS
  Pre-submission checks for the Microsoft Store package: WACK on the local-test MSIX, and the
  privacy-policy URL the listing carries.

.DESCRIPTION
  Run this when a Store submission is being prepared. It is not part of the release gate: the
  Store channel is submitted by hand through Partner Center, so the checks live beside that
  step and never block a tag.

  WACK       The Windows App Certification Kit (appcert.exe, from the Windows SDK) runs the
             same test families the Store runs at certification. It is pointed at the package
             built by build-msix.ps1 -SelfSign, which is the only installable shape there is:
             a signed local-test package carrying the fixed TEST identity SZA.FileDO.LocalTest.

             Two limits, both known and accepted. It proves the crash/hang, resource-use and
             manifest classes of test. It proves nothing about the reserved Store identity or
             Microsoft's re-signing, because the package has neither - those are decided at
             upload, not here.

             appcert needs an elevated session and installs the package it tests, so this
             script refuses to guess: run it from an ADMIN PowerShell.

  PRIVACY    Store Policies 10.5 makes a working privacy-policy URL mandatory. The listing
             carries the canonical link of docs\privacy.html, so the check is a GET against
             the PUBLISHED page, not the working tree - which is exactly why it is here and
             not in the release gate, where GitHub Pages lag would make it noise.

  Exit code: 0 = every check passed, 1 = a defect was found, 2 = could not verify.

.PARAMETER Msix
  The package to test. Default: the newest msix\out\FileDO_*_LOCALTEST.msix.

.PARAMETER Build
  Build it first (build-msix.ps1 -SelfSign) instead of using an existing package.

.PARAMETER TrustTestCert
  Import msix\out\filedo-localtest.cer into LocalMachine\TrustedPeople if it is not trusted
  yet. Without it an untrusted test certificate is reported with the command to run, and
  nothing is installed. It is a self-signed test certificate and it trusts only itself.

.PARAMETER SkipWack
  Run the privacy check only - no elevation, no install, no SDK.

.EXAMPLE
  # from an ADMIN PowerShell, in the repo root:
  pwsh -NoProfile -File .\msix\test-store-package.ps1 -Build -TrustTestCert
.EXAMPLE
  pwsh -NoProfile -File .\msix\test-store-package.ps1 -SkipWack
#>
[CmdletBinding()]
param(
    [string]$Msix,
    [switch]$Build,
    [switch]$TrustTestCert,
    [switch]$SkipWack,
    # The canonical link of docs\privacy.html - the URL the Store listing carries.
    [string]$PrivacyUrl = "https://serzhyale.github.io/FileDO/privacy.html",
    [string]$ReportPath
)

$ErrorActionPreference = "Stop"
$msixDir = $PSScriptRoot
$root    = Split-Path $msixDir -Parent
$outDir  = Join-Path $msixDir "out"
if (-not $ReportPath) { $ReportPath = Join-Path $outDir "wack-report.xml" }

$script:fail = 0; $script:pass = 0; $script:unverified = @()
function Check([string]$name, [bool]$ok, [string]$detail = "") {
    if ($ok) { $script:pass++; Write-Host "  PASS  $name" -ForegroundColor Green }
    else     { $script:fail++; Write-Host "  FAIL  $name  $detail" -ForegroundColor Red }
}
function CannotVerify([string]$reason) {
    $script:unverified += $reason
    Write-Host "  NOT VERIFIED  $reason" -ForegroundColor Yellow
}

# ---------------------------------------------------------------------------------------------
# Privacy policy (Store Policies 10.5)
# ---------------------------------------------------------------------------------------------
Write-Host "PRIVACY" -ForegroundColor Cyan
try {
    $resp = Invoke-WebRequest -Uri $PrivacyUrl -UseBasicParsing -MaximumRedirection 5
    Check "$PrivacyUrl answers 200 (policy 10.5)" ($resp.StatusCode -eq 200) "status $($resp.StatusCode)"
} catch {
    $code = $null
    if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
    if ($code -eq 404) { Check "$PrivacyUrl answers 200 (policy 10.5)" $false ("status 404 - " + $_.Exception.Message) }
    else { CannotVerify "$PrivacyUrl could not be reached: $($_.Exception.Message)" }
}

# ---------------------------------------------------------------------------------------------
# WACK
# ---------------------------------------------------------------------------------------------
if ($SkipWack) {
    Write-Host "WACK" -ForegroundColor Cyan
    Write-Host "  SKIP  appcert (-SkipWack)" -ForegroundColor Yellow
} else {
    Write-Host "WACK" -ForegroundColor Cyan

    # appcert installs and launches the package it tests: elevation is a prerequisite, not a
    # detail. It refuses even to print its own help without it.
    $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $admin) {
        CannotVerify "appcert.exe requires an administrator session - re-run from an ADMIN PowerShell, or pass -SkipWack"
    } else {
        # appcert does NOT live in the SDK's bin\<version>\x64 (where makeappx and signtool are);
        # it has its own kit directory, so build-msix.ps1's Find-SdkTool does not find it.
        $ack = Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\App Certification Kit\appcert.exe"
        if (-not (Test-Path $ack)) {
            CannotVerify "$ack not found. Install: winget install Microsoft.WindowsSDK.10.0.26100"
        } else {
            if ($Build) {
                Write-Host "  building the local-test package..." -ForegroundColor DarkGray
                & (Join-Path $msixDir "build-msix.ps1") -SelfSign
                if ($LASTEXITCODE -ne 0) {
                    if ($LASTEXITCODE -eq 2) { CannotVerify "build-msix.ps1 -SelfSign could not verify" }
                    else { Check "build-msix.ps1 -SelfSign succeeds" $false "exit $LASTEXITCODE" }
                }
            }
            if (-not $Msix) {
                $Msix = (Get-ChildItem (Join-Path $outDir "FileDO_*_LOCALTEST.msix") -ErrorAction SilentlyContinue |
                         Sort-Object LastWriteTime | Select-Object -Last 1).FullName
            }
            if (-not $Msix -or -not (Test-Path $Msix)) {
                CannotVerify "no local-test package - run with -Build, or .\msix\build-msix.ps1 -SelfSign"
            } else {
                $Msix = (Resolve-Path $Msix).Path
                # A Store package must never be handed to this path: it is unsigned, appcert
                # cannot install it, and a WACK verdict on it would mean nothing anyway.
                if ($Msix -notmatch '_LOCALTEST\.msix$') {
                    Check "the package under test is the local-test one" $false "$Msix is not a _LOCALTEST package; WACK can only install a signed package, and the Store package is signed by Microsoft at certification"
                } else {
                    Write-Host "  package: $Msix" -ForegroundColor DarkGray

                    # appcert installs the package, so the self-signed test certificate has to be
                    # trusted by the machine first. build-msix.ps1 -SelfSign exports it beside the
                    # package and prints the same import command.
                    $cer = Join-Path $outDir "filedo-localtest.cer"
                    $trusted = $false
                    if (Test-Path $cer) {
                        $thumb = (Get-PfxCertificate -FilePath $cer).Thumbprint
                        $trusted = [bool](Get-ChildItem Cert:\LocalMachine\TrustedPeople -ErrorAction SilentlyContinue | Where-Object { $_.Thumbprint -eq $thumb })
                        if (-not $trusted -and $TrustTestCert) {
                            Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\TrustedPeople | Out-Null
                            $trusted = $true
                            Write-Host "  imported the local-test certificate into LocalMachine\TrustedPeople" -ForegroundColor DarkGray
                        }
                    }
                    if (-not $trusted) {
                        CannotVerify "the local-test certificate is not trusted. Re-run with -TrustTestCert, or import $cer"
                    } else {
                        New-Item -ItemType Directory -Force -Path (Split-Path $ReportPath -Parent) | Out-Null
                        Remove-Item $ReportPath -ErrorAction SilentlyContinue

                        # appcert's own instruction: reset before a new validation session.
                        & $ack reset | Out-Null
                        Write-Host "  running appcert (this takes minutes and launches the app)..." -ForegroundColor DarkGray
                        $wOut  = & $ack test -appxpackagepath $Msix -reportoutputpath $ReportPath 2>&1 | Out-String
                        $wCode = $LASTEXITCODE
                        # appcert's documented codes: 0 = ran, 1 = ran but the report needs
                        # finalizing (waivers), -1 command line, -2 infrastructure, -3 user,
                        # -4 install, -5 unpackaging. Only 0 and 1 mean the tests actually ran.
                        Check "appcert test ran (exit $wCode)" ($wCode -eq 0 -or $wCode -eq 1) ($wOut.Trim())
                        if ($wCode -eq 1) { Write-Host "  the report needs finalizing: $ack finalizereport -reportfilepath `"$ReportPath`"" -ForegroundColor Yellow }

                        if (Test-Path $ReportPath) {
                            $rep     = [xml](Get-Content $ReportPath -Raw)
                            $overall = $rep.DocumentElement.GetAttribute('OVERALL_RESULT')
                            # Each test's verdict is a RESULT *child element* holding CDATA, not an
                            # attribute - and it is the only place the verdict is: appcert returns 0
                            # for a session that ran and found failures, and rolls an optional test's
                            # FAIL up into an OVERALL_RESULT of WARNING.
                            $tests = @($rep.SelectNodes('//TEST') | ForEach-Object {
                                $res = $_.SelectSingleNode('RESULT')
                                [pscustomobject]@{
                                    Name     = $_.GetAttribute('NAME')
                                    Optional = ($_.GetAttribute('OPTIONAL') -eq 'TRUE')
                                    Result   = if ($res) { $res.InnerText.Trim() } else { '(none)' }
                                    Messages = @($_.SelectNodes('MESSAGES/MESSAGE') | ForEach-Object { $_.GetAttribute('TEXT') })
                                }
                            })
                            Check "the report lists the tests that ran" ($tests.Count -gt 0) "no TEST element in $ReportPath"

                            # Anything that is not PASS is a reason the Store can reject the
                            # submission, whether appcert marks the test optional or not. The check
                            # reports all of them and lets the verdict be the owner's to argue with,
                            # rather than deciding here which rejection is acceptable.
                            $notPass = @($tests | Where-Object { $_.Result -cne 'PASS' })
                            Check "every WACK test passed (report says OVERALL_RESULT=$overall)" ($notPass.Count -eq 0 -and $overall -eq 'PASS') "$($notPass.Count) of $($tests.Count) tests did not pass"
                            foreach ($t in $notPass) {
                                $opt = if ($t.Optional) { "optional" } else { "required" }
                                Write-Host "        $($t.Result)  [$opt]  $($t.Name)" -ForegroundColor Yellow
                                $t.Messages | ForEach-Object { Write-Host "            $_" -ForegroundColor DarkYellow }
                            }
                            Write-Host "  report: $ReportPath" -ForegroundColor DarkGray
                        } else {
                            Check "appcert wrote a report" $false "$ReportPath was not produced"
                        }

                        # Leave nothing installed: appcert installs the package to test it.
                        Get-AppxPackage "SZA.FileDO.LocalTest" -ErrorAction SilentlyContinue | Remove-AppxPackage -ErrorAction SilentlyContinue
                    }
                }
            }
        }
    }
}

Write-Host ""
if ($script:fail -gt 0) { Write-Host "store-package: FAIL ($script:fail checks)" -ForegroundColor Red; exit 1 }
if ($script:unverified.Count -gt 0) { Write-Host "store-package: NOT VERIFIED ($($script:unverified -join '; '))" -ForegroundColor Yellow; exit 2 }
Write-Host "store-package: PASS ($script:pass checks)" -ForegroundColor Green
exit 0
