$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

# Version = current build date/time in yyMMddHHmm format
$version = Get-Date -Format "yyMMddHHmm"

Write-Host "Version: $version"
Write-Host ""

$out = "$root\exe_to_download"

$builds = @(
    @{ Dir = $root;          Out = "$out\filedo.exe";       Flags = "-ldflags=-X 'main.version=$version'" },
    @{ Dir = "$root\FILL";   Out = "$out\filedo_fill.exe";  Flags = "" },
    @{ Dir = "$root\CHECK";  Out = "$out\filedo_check.exe"; Flags = "" },
    @{ Dir = "$root\TEST";   Out = "$out\filedo_test.exe";  Flags = "" }
)

$failed = @()

foreach ($b in $builds) {
    $name = Split-Path $b.Dir -Leaf
    if ($name -eq (Split-Path $root -Leaf)) { $name = "filedo" }
    Write-Host "Building $name..." -NoNewline

    Push-Location $b.Dir
    try {
        if ($b.Flags) {
            go build $b.Flags -o $b.Out . 2>&1 | Out-Null
        } else {
            go build -o $b.Out . 2>&1 | Out-Null
        }
        if ($LASTEXITCODE -ne 0) { throw "exit code $LASTEXITCODE" }
        Write-Host " OK"
    } catch {
        Write-Host " FAILED: $_"
        $failed += $name
    } finally {
        Pop-Location
    }
}

Write-Host ""
if ($failed.Count -eq 0) {
    Write-Host "All builds successful."
} else {
    Write-Host "Failed: $($failed -join ', ')"
    exit 1
}

$deploy = "C:\GD\tc\SZA\_APP"
Write-Host ""
Write-Host "Copying to $deploy ..." -NoNewline
Copy-Item "$out\*" $deploy -Force
Write-Host " OK"
