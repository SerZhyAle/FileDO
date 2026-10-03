#Requires -Version 7.0
# Build the local executables and open the GUI for manual testing.
[CmdletBinding()]
param(
    # Also run the existing build gate before opening the GUI.
    [switch]$Test
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try {
    $outputDir = Join-Path $PSScriptRoot 'exe_to_download'
    $guiPath = Join-Path $outputDir 'filedo_win.exe'
    foreach ($existing in @(Get-Process -Name 'filedo_win' -ErrorAction SilentlyContinue)) {
        if ($existing.Path -ne $guiPath) {
            Write-Host 'Close the other FileDO instance before starting this test build.' -ForegroundColor Yellow
            exit 2
        }
        Write-Host "Closing the previous test window (PID $($existing.Id))."
        Write-Host 'If an operation is running, answer the close question in FileDO.'
        if (-not $existing.CloseMainWindow() -or -not $existing.WaitForExit(15000)) {
            Write-Host 'FileDO is still open. Finish the operation, close its windows, then run again.' -ForegroundColor Yellow
            exit 2
        }
    }

    # A child process preserves build.ps1's exit code without exiting this launcher.
    # Disable deployment even when FILEDO_DEPLOY_DIR is set on the machine.
    $buildArgs = @('-NoProfile', '-File', (Join-Path $PSScriptRoot 'build.ps1'),
        '-SkipInstaller', '-DeployTo', '')
    if ($Test) { $buildArgs += '-Test' }
    & (Join-Path $PSHOME 'pwsh.exe') @buildArgs
    $buildExit = $LASTEXITCODE
    if ($buildExit -ne 0) {
        Write-Host "Build stopped (exit $buildExit). The GUI was not started." -ForegroundColor Yellow
        exit $buildExit
    }

    Write-Host "Starting $guiPath for manual testing."
    $gui = Start-Process -FilePath $guiPath -WorkingDirectory $outputDir -PassThru
    Start-Sleep -Seconds 2
    $gui.Refresh()
    if ($gui.HasExited) {
        Write-Host "The GUI closed during startup (exit $($gui.ExitCode))." -ForegroundColor Yellow
        exit 1
    }
    Write-Host "GUI running (PID $($gui.Id)). Run run.bat again to rebuild and reopen it."
    exit 0
} catch {
    Write-Host "Cannot build and run: $($_.Exception.Message)" -ForegroundColor Yellow
    exit 2
}
