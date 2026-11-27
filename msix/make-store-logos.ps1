<#
.SYNOPSIS
  Render the Store logo set for the Partner Center "Store logos" section from the product's master SVG.

.DESCRIPTION
  The listing page takes optional logo images next to the ones in the package:
    9:16 Poster art       720x1080 or 1440x2160   (highly recommended; the main logo on Windows 10/11)
    1:1  Box art          1080x1080 or 2160x2160  (recommended; other Store layouts)
    1:1  App tile icon    300x300, 150x150, 71x71 (Store display images, Windows 10/11)
  The package's own logos are cut from the 256 px assets\icon.png; that is too small to blow up to
  the poster, so this script renders the master SVG (assets\identity\master\filedo-mark.svg, which
  assets\make-product-icons.ps1 writes from the drawing every other picture of FileDO is cut from)
  at the exact pixel size (PRODUCT-IDENTITY rules 4 and 6).

  Rendering is headless Microsoft Edge (installed with Windows): the SVG and the wordmark are laid
  out as HTML and screenshotted at the target size. Nothing is drawn by hand twice - the mark is
  the same drawing the MSI, the EXE and the site show, so it cannot drift from them. The run records
  the SHA-256 of the SVG it read in store-logos\master-sha256.txt; the identity manifest lists the set
  with that hash and names a set rendered from another drawing.

  The poster and box art compose the master and canonical product name on the master's
  declared background. The tiles carry no words and are identical in every language.

  Output, one folder per Partner Center locale code (the same codes msix\screenshots uses), each
  holding the complete set so it can be uploaded from one place:
    msix\store-logos\<locale>\poster-720x1080.png, poster-1440x2160.png, box-1080x1080.png,
    box-2160x2160.png, tile-300x300.png, tile-150x150.png, tile-71x71.png
  Every file's pixel size and corner alpha are read back and must match its name. Partner Center
  takes one file per slot; upload the larger of each pair. The tiles keep transparent corners (the
  plate is rounded); the poster and the box art are opaque.

  This writes files only. It uploads nothing and touches no manifest.

.PARAMETER Locale
  Partner Center locale codes to render (comma separated). Default: en-us, ru, uk.

.PARAMETER Svg
  The mark to render. Default: assets\identity\master\filedo-mark.svg (assets\make-product-icons.ps1
  -StoreLogos passes the one it has just written).

.EXAMPLE
  .\msix\make-store-logos.ps1
.EXAMPLE
  .\msix\make-store-logos.ps1 -Locale ru
#>
[CmdletBinding()]
param(
    [string]$Locale = 'en-us,ru,uk',
    [string]$OutDir,
    # Path to msedge.exe. Default: the standard Windows install locations.
    [string]$Edge,
    [string]$Svg,
    [string]$Background
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$root = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $PSScriptRoot 'store-logos' }
if (-not $Svg) { $Svg = Join-Path $root 'assets\identity\master\filedo-mark.svg' }
$svgPath = $Svg
if (-not (Test-Path $svgPath)) { throw "the mark is missing: $svgPath (run assets\make-product-icons.ps1)" }

# Use the manifest background for a direct call; the master generator passes its current colour.
if (-not $Background) {
    $identity = Get-Content -Raw (Join-Path $root 'assets\identity\identity-manifest.json') | ConvertFrom-Json
    $Background = $identity.master.background
}
if ($Background -notmatch '^#[0-9A-Fa-f]{6}$') { throw 'The master background must be #RRGGBB.' }
$locales = @('en-us', 'ru', 'uk')
$wanted = @($Locale -split '[,\s]+' | Where-Object { $_ })
foreach ($c in $wanted) { if ($c -notin $locales) { throw "unknown locale '$c'; known: $($locales -join ', ')." } }

if (-not $Edge) {
    $Edge = @(
        "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe",
        "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe"
    ) | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $Edge -or -not (Test-Path $Edge)) { throw "msedge.exe not found; pass -Edge <path>." }

$work = Join-Path ([IO.Path]::GetTempPath()) ("filedo-store-logos-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $work | Out-Null

# The mark, as inline SVG that fills its box. $viewBox crops the plate: the full icon has a 14 px
# margin that is right for the package logos and wasted at 71 px.
$svgText = [IO.File]::ReadAllText($svgPath)
function Get-Mark([string]$viewBox) {
    $m = [regex]::Replace($svgText, '<svg\b[^>]*>', "<svg xmlns=`"http://www.w3.org/2000/svg`" viewBox=`"$viewBox`" style=`"display:block;width:100%;height:100%`">", 1)
    if ($m -eq $svgText) { throw "$svgPath has no root <svg> element." }
    return $m
}

$fonts = "'Segoe UI Variable Display','Segoe UI',system-ui,sans-serif"

function Get-ArtHtml([int]$w, [int]$h, [string]$markPx, [string]$wordPx, [string]$padTop, [string]$gap) {
    $mark = Get-Mark '0 0 256 256'
    @"
<!doctype html><html><head><meta charset="utf-8"><style>
html,body{margin:0;width:${w}px;height:${h}px;overflow:hidden;background:$Background}
body{position:relative;font-family:$fonts;color:#fff}
.col{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;padding-top:$padTop;text-align:center}
.mark{width:$markPx;height:$markPx}
.name{margin-top:$gap;font-size:$wordPx;font-weight:700;letter-spacing:-0.02em;line-height:1}
</style></head><body>
<div class="col"><div class="mark">$mark</div><div class="name">FileDO</div></div>
</body></html>
"@
}

function Get-TileHtml([int]$px) {
    $mark = Get-Mark '12 12 232 232'
    @"
<!doctype html><html><head><meta charset="utf-8"><style>
html,body{margin:0;width:${px}px;height:${px}px;overflow:hidden;background:transparent}
</style></head><body>$mark</body></html>
"@
}

# Render one page to a PNG and read it back: what was asked of Edge is not what was made until the
# file says so. The corner pixel tells the background - the rounded tiles are transparent there,
# the poster and the box art are opaque edge to edge.
function Save-Page([string]$html, [string]$name, [int]$w, [int]$h, [int]$scale, [string]$dst) {
    $page = Join-Path $work ([guid]::NewGuid().ToString('N') + '.html')
    [IO.File]::WriteAllText($page, $html, (New-Object Text.UTF8Encoding($false)))
    Remove-Item $dst -ErrorAction SilentlyContinue
    $edgeArgs = @(
        '--headless=new', '--disable-gpu', '--hide-scrollbars', '--no-first-run',
        "--user-data-dir=`"$(Join-Path $work 'profile')`"",
        '--default-background-color=00000000',
        "--force-device-scale-factor=$scale",
        "--window-size=$w,$h",
        '--virtual-time-budget=2000',
        "--screenshot=`"$dst`"",
        ([Uri]$page).AbsoluteUri
    )
    $p = Start-Process -FilePath $Edge -ArgumentList $edgeArgs -Wait -PassThru -NoNewWindow -RedirectStandardError (Join-Path $work 'edge.err') -RedirectStandardOutput (Join-Path $work 'edge.out')
    if (-not (Test-Path $dst)) { throw "Edge produced no file for $name (exit $($p.ExitCode))." }

    $want = "$($w * $scale)x$($h * $scale)"
    $img = [System.Drawing.Bitmap]::FromFile($dst)
    try { $got = "$($img.Width)x$($img.Height)"; $cornerAlpha = $img.GetPixel(0, 0).A } finally { $img.Dispose() }
    if ($got -ne $want) { throw "$name is $got, want $want." }
    $wantAlpha = if ($name -like 'tile-*') { 0 } else { 255 }
    if ($cornerAlpha -ne $wantAlpha) { throw "$name corner alpha is $cornerAlpha, want $wantAlpha." }
    return $got
}

try {
    # The tiles carry no words: render them once, copy into every language folder.
    $tileDir = Join-Path $work 'tiles'
    New-Item -ItemType Directory -Force $tileDir | Out-Null
    foreach ($px in 300, 150, 71) {
        $n = "tile-${px}x${px}.png"
        [void](Save-Page (Get-TileHtml $px) $n $px $px 1 (Join-Path $tileDir $n))
    }

    $count = 0
    foreach ($code in $wanted) {
        $dir = Join-Path $OutDir $code
        New-Item -ItemType Directory -Force $dir | Out-Null
        Write-Host "[$code]"

        $poster = Get-ArtHtml 720 1080 '460px' '118px' '150px' '56px'
        $box    = Get-ArtHtml 1080 1080 '470px' '128px' '130px' '58px'
        $art = @(
            @{ File = 'poster-720x1080.png';  W = 720;  H = 1080; Scale = 1; Html = $poster }
            @{ File = 'poster-1440x2160.png'; W = 720;  H = 1080; Scale = 2; Html = $poster }
            @{ File = 'box-1080x1080.png';    W = 1080; H = 1080; Scale = 1; Html = $box }
            @{ File = 'box-2160x2160.png';    W = 1080; H = 1080; Scale = 2; Html = $box }
        )
        foreach ($a in $art) {
            $size = Save-Page $a.Html $a.File $a.W $a.H $a.Scale (Join-Path $dir $a.File)
            Write-Host ("  {0,-22} {1}" -f $a.File, $size)
            $count++
        }
        foreach ($t in Get-ChildItem $tileDir -Filter 'tile-*.png') {
            Copy-Item $t.FullName (Join-Path $dir $t.Name) -Force
            Write-Host ("  {0,-22} (shared)" -f $t.Name)
            $count++
        }
    }
} finally {
    Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}
# The drawing this set was rendered from, for the identity manifest (renderedFrom).
$svgSha = (Get-FileHash -LiteralPath $svgPath -Algorithm SHA256).Hash.ToLowerInvariant()
[System.IO.File]::WriteAllText((Join-Path $OutDir 'master-sha256.txt'), "$svgSha  assets/identity/master/filedo-mark.svg`n", (New-Object Text.UTF8Encoding($false)))
Write-Host "OK - $count files in $OutDir (rendered from $($svgSha.Substring(0, 12)))"
