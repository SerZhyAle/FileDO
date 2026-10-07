<#
.SYNOPSIS
  Render the Store logo set for the Partner Center "Store logos" section from assets\icon.svg.

.DESCRIPTION
  The listing page takes optional logo images next to the ones in the package:
    9:16 Poster art       720x1080 or 1440x2160   (highly recommended; the main logo on Windows 10/11)
    1:1  Box art          1080x1080 or 2160x2160  (recommended; other Store layouts)
    1:1  App tile icon    300x300, 150x150, 71x71 (Store display images, Windows 10/11)
  The package's own logos are cut from the 270 px assets\icon.png; that is too small to blow up to
  the poster, so this script renders the vector mark (assets\icon.svg) at the exact pixel size.

  Rendering is headless Microsoft Edge (installed with Windows): the SVG and the wordmark are laid
  out as HTML and screenshotted at the target size. Nothing is drawn by hand twice - the mark is
  the same file the MSI, the EXE and the site use, so it cannot drift from them.

  The poster and the box art carry words, so they exist once per listing language. The tagline is
  the site's headline (docs\index.html) and the strip words are the listing's own section names
  (msix\listing\<code>.txt), so the picture says what the page says. The tiles carry no words and
  are identical in every language. To add a language, add a row to $locales below.

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
    [string]$Edge
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$root = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $PSScriptRoot 'store-logos' }
$svgPath = Join-Path $root 'assets\icon.svg'
if (-not (Test-Path $svgPath)) { throw "the mark is missing: $svgPath" }

# Words per language. Tag: the site's headline, written as HTML (&nbsp; keeps a fixed phrase such
# as "прежде чем" on one line). Strip: section names from the listing source, four on the poster,
# five on the box art (which is wider).
$locales = [ordered]@{
    'en-us' = @{
        Lang  = 'en'
        Tag   = 'Know your storage before you trust it'
        Strip = @('Speed', 'Capacity', 'Vault', 'Wipe', 'Duplicates')
    }
    'ru'    = @{
        Lang  = 'ru'
        Tag   = 'Узнайте носитель, прежде&nbsp;чем доверить ему данные'
        Strip = @('Скорость', 'Ёмкость', 'Защита', 'Стирание', 'Дубликаты')
    }
    'uk'    = @{
        Lang  = 'uk'
        Tag   = 'Дізнайтеся про носій, перш&nbsp;ніж довірити йому дані'
        Strip = @('Швидкість', 'Ємність', 'Захист', 'Стирання', 'Дублікати')
    }
}
$wanted = @($Locale -split '[,\s]+' | Where-Object { $_ })
foreach ($c in $wanted) { if (-not $locales.Contains($c)) { throw "unknown locale '$c'; known: $($locales.Keys -join ', ')." } }

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
    if ($m -eq $svgText) { throw "icon.svg has no root <svg> element." }
    return $m
}

$fonts = "'Segoe UI Variable Display','Segoe UI',system-ui,sans-serif"

function Get-ArtHtml([string]$lang, [string]$tag, [string]$strip, [int]$w, [int]$h, [string]$markPx, [string]$wordPx, [string]$tagPx, [string]$padTop, [string]$gap) {
    $mark = Get-Mark '0 0 256 256'
    @"
<!doctype html><html lang="$lang"><head><meta charset="utf-8"><style>
html,body{margin:0;width:${w}px;height:${h}px;overflow:hidden;background:#070C09}
body{position:relative;font-family:$fonts;color:#fff;
  background:radial-gradient(circle at 50% 36%,#143324 0,#0C1710 38%,#060A07 78%)}
.col{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;padding-top:$padTop;text-align:center}
.mark{width:$markPx;height:$markPx;filter:drop-shadow(0 12px 40px rgba(0,0,0,.55))}
.name{margin-top:$gap;font-size:$wordPx;font-weight:700;letter-spacing:-0.02em;line-height:1}
.tag{margin-top:calc($gap * .55);font-size:$tagPx;font-weight:600;color:#A7F3D0;line-height:1.25;max-width:84%;text-wrap:balance}
.strip{position:absolute;left:0;right:0;bottom:calc(${h}px * .055);text-align:center;font-size:calc($tagPx * .62);
  letter-spacing:.14em;text-transform:uppercase;color:#7FA38C;font-weight:600}
</style></head><body>
<div class="col"><div class="mark">$mark</div>
<div class="name">FileDO</div>
<div class="tag">$tag</div></div>
<div class="strip">$strip</div>
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
        $l = $locales[$code]
        $dir = Join-Path $OutDir $code
        New-Item -ItemType Directory -Force $dir | Out-Null
        Write-Host "[$code]"

        $strip4 = ($l.Strip[0..3]) -join ' &middot; '
        $strip5 = ($l.Strip[0..4]) -join ' &middot; '
        $poster = Get-ArtHtml $l.Lang $l.Tag $strip4 720 1080 '460px' '118px' '38px' '150px' '56px'
        $box    = Get-ArtHtml $l.Lang $l.Tag $strip5 1080 1080 '470px' '128px' '40px' '130px' '58px'
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
Write-Host "OK - $count files in $OutDir"
