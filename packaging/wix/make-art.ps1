# ============================================================================
#  Regenerates the two bitmaps the installer's dialogs use, from assets\icon.png.
#
#    banner.bmp  493x58   - the strip across the top of every wizard page
#    dialog.bmp  493x312  - the background of the Welcome and Finish pages
#
#  They are committed next to this script, because a build must not depend on
#  drawing code running correctly on someone else's machine. Run this only when
#  the icon or the wording changes, and commit the result:
#
#      pwsh -NoProfile -File packaging\wix\make-art.ps1
#
#  The layout is dictated by WixUI, not by taste: MSI draws the page title over
#  the LEFT of the banner and the welcome text over the RIGHT of the dialog
#  bitmap, so those areas stay empty here. Anything drawn there would end up
#  underneath a sentence.
# ============================================================================
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$icon = Join-Path $root "assets\icon.png"
if (-not (Test-Path $icon)) { throw "assets\icon.png not found at $icon" }

# The product's own colours: the dark green of the site's default theme.
$ink       = [System.Drawing.Color]::FromArgb(10, 15, 10)
$panel     = [System.Drawing.Color]::FromArgb(14, 26, 18)
$accent    = [System.Drawing.Color]::FromArgb(94, 214, 132)
$paper     = [System.Drawing.Color]::White
$subtle    = [System.Drawing.Color]::FromArgb(150, 173, 156)

$logo = [System.Drawing.Image]::FromFile($icon)

function New-Canvas([int]$w, [int]$h) {
    $bmp = New-Object System.Drawing.Bitmap $w, $h
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode     = "AntiAlias"
    $g.InterpolationMode = "HighQualityBicubic"
    $g.TextRenderingHint = "ClearTypeGridFit"
    return @($bmp, $g)
}

# ---- banner.bmp -------------------------------------------------------------
# MSI writes the page title and subtitle over the left ~350px, so the icon sits
# on the right and the rest is a plain light field the black title reads on.
$b, $bg = New-Canvas 493 58
$bg.Clear($paper)
$bg.FillRectangle((New-Object System.Drawing.SolidBrush $accent), 0, 55, 493, 3)
$bg.DrawImage($logo, 435, 9, 40, 40)
$bg.Dispose()
$b.Save((Join-Path $PSScriptRoot "banner.bmp"), [System.Drawing.Imaging.ImageFormat]::Bmp)
$b.Dispose()

# ---- dialog.bmp -------------------------------------------------------------
# The Welcome and Finish pages draw their text over the right two thirds, so the
# artwork is a band down the left: icon, name, and the one line that says what
# this program is for.
$d, $dg = New-Canvas 493 312
$dg.Clear($paper)
$dg.FillRectangle((New-Object System.Drawing.SolidBrush $ink), 0, 0, 164, 312)
$dg.FillRectangle((New-Object System.Drawing.SolidBrush $accent), 161, 0, 3, 312)
$dg.DrawImage($logo, 46, 40, 72, 72)

$nameFont = New-Object System.Drawing.Font("Segoe UI Semibold", 20, [System.Drawing.FontStyle]::Bold)
$lineFont = New-Object System.Drawing.Font("Segoe UI", 8.5)
$fmt = New-Object System.Drawing.StringFormat
$fmt.Alignment = "Center"
$dg.DrawString("FileDO", $nameFont, (New-Object System.Drawing.SolidBrush $paper),
               (New-Object System.Drawing.RectangleF 0, 160, 164, 34), $fmt)
$dg.DrawString("Know your storage`nbefore you trust it", $lineFont,
               (New-Object System.Drawing.SolidBrush $subtle),
               (New-Object System.Drawing.RectangleF 12, 200, 140, 60), $fmt)
$dg.Dispose()
$d.Save((Join-Path $PSScriptRoot "dialog.bmp"), [System.Drawing.Imaging.ImageFormat]::Bmp)
$d.Dispose()
$logo.Dispose()

Write-Host "Wrote banner.bmp (493x58) and dialog.bmp (493x312) into $PSScriptRoot"
