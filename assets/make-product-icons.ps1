# ============================================================================
#  FileDO - Product Icon & Asset Generator
#  Generates the official multi-size icon suite:
#    - assets\icon.png (256x256 master)
#    - assets\icon-branded-512.png (512x512 high-res)
#    - assets\icon.ico (16, 24, 32, 48, 64, 128, 256 px frames)
#    - docs\assets\filedo.png (256x256 web icon)
#    - docs\assets\social-preview.png & assets\social-preview.png (1280x640, the
#      GitHub repository social preview)
#    - docs\assets\social-card.png (1200x630, the Open Graph image)
#  The two social files are one artwork from one drawing function (Render-SocialCard);
#  only the canvas size differs.
#
#  -SocialOnly regenerates ONLY the three social files above. The icons, the .ico and
#  the MSIX logos are not drawn, written or touched.
# ============================================================================
[CmdletBinding()]
param(
    [switch]$SocialOnly
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$root = $PSScriptRoot
if (-not (Test-Path (Join-Path $root "assets"))) {
    $root = Split-Path $PSScriptRoot -Parent
}
$assetsDir = Join-Path $root "assets"
$docsAssets = Join-Path $root "docs\assets"

# Color Palette
$cBgDark      = [System.Drawing.Color]::FromArgb(255, 10, 16, 12)       # Deep obsidian forest #0A100C
$cBgSurface   = [System.Drawing.Color]::FromArgb(255, 15, 26, 18)       # Dark emerald panel #0F1A12
$cBgPlateEnd  = [System.Drawing.Color]::FromArgb(255, 8, 14, 10)        # Gradient dark end
$cBorderOuter = [System.Drawing.Color]::FromArgb(180, 94, 214, 132)     # Emerald outer border
$cAccentMint  = [System.Drawing.Color]::FromArgb(255, 94, 214, 132)     # Signature mint #5ED684
$cAccentNeon  = [System.Drawing.Color]::FromArgb(255, 0, 230, 118)      # Vivid neon #00E676
$cAccentLight = [System.Drawing.Color]::FromArgb(255, 167, 243, 208)    # Highlight #A7F3D0
$cWhite       = [System.Drawing.Color]::FromArgb(255, 255, 255, 255)
$cCyan        = [System.Drawing.Color]::FromArgb(255, 56, 189, 248)     # Cyan data pulse #38BDF8
$cSubtleText  = [System.Drawing.Color]::FromArgb(220, 150, 173, 156)

function Create-RoundedRectPath([float]$x, [float]$y, [float]$w, [float]$h, [float]$r) {
    $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $d = [float]($r * 2.0)
    $path.AddArc($x, $y, $d, $d, 180.0, 90.0)
    $path.AddArc([float]($x + $w - $d), $y, $d, $d, 270.0, 90.0)
    $path.AddArc([float]($x + $w - $d), [float]($y + $h - $d), $d, $d, 0.0, 90.0)
    $path.AddArc($x, [float]($y + $h - $d), $d, $d, 90.0, 90.0)
    $path.CloseFigure()
    return $path
}

function Render-FileDOIcon([int]$size, [bool]$showText) {
    $bmp = [System.Drawing.Bitmap]::new($size, $size)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode     = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode   = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit
    $g.Clear([System.Drawing.Color]::Transparent)

    $scale = [float]$size / 256.0

    # 1. Outer Plate (Squircle with gradient & glow)
    $pad = [float](14.0 * $scale)
    $plateW = [float]($size - (2.0 * $pad))
    $plateH = [float]($size - (2.0 * $pad))
    $cornerR = [float](48.0 * $scale)

    if ($size -le 32) {
        $pad = [float](1.0 * $scale)
        $plateW = [float]($size - (2.0 * $pad))
        $plateH = [float]($size - (2.0 * $pad))
        $cornerR = [float](6.0 * $scale)
    }

    $platePath = Create-RoundedRectPath $pad $pad $plateW $plateH $cornerR

    # Gradient fill for plate
    $pt1 = [System.Drawing.PointF]::new($pad, $pad)
    $pt2 = [System.Drawing.PointF]::new([float]($pad + $plateW), [float]($pad + $plateH))
    $bgBrush = [System.Drawing.Drawing2D.LinearGradientBrush]::new($pt1, $pt2, $cBgSurface, $cBgPlateEnd)
    $g.FillPath($bgBrush, $platePath)
    $bgBrush.Dispose()

    # Subtle Circuit / Storage track lines on medium/large sizes
    if ($size -ge 64) {
        $trackPen = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(25, 94, 214, 132), [float](1.5 * $scale))
        $g.DrawLine($trackPen, [float]($pad + 20 * $scale), [float]($pad + 40 * $scale), [float]($pad + 60 * $scale), [float]($pad + 40 * $scale))
        $g.DrawLine($trackPen, [float]($pad + 60 * $scale), [float]($pad + 40 * $scale), [float]($pad + 80 * $scale), [float]($pad + 60 * $scale))
        $g.DrawLine($trackPen, [float]($pad + $plateW - 60 * $scale), [float]($pad + $plateH - 40 * $scale), [float]($pad + $plateW - 20 * $scale), [float]($pad + $plateH - 40 * $scale))
        $trackPen.Dispose()
    }

    # Plate Border Glow
    $borderPen = [System.Drawing.Pen]::new($cBorderOuter, [float][Math]::Max(1.0, 2.5 * $scale))
    $g.DrawPath($borderPen, $platePath)
    $borderPen.Dispose()

    # 2. Central Emblem Geometry (The FileDO 'F-D' Storage Shield Monogram)
    $cx = [float]($size / 2.0)
    $cy = [float]($size / 2.0)
    $emblemScale = [float](0.92 * $scale)
    if ($showText -and $size -ge 256) {
        $cy = [float]($size * 0.42)
        $emblemScale = [float](0.76 * $scale)
    }

    # Left 'F' pillar + speed data bars
    $fPt1 = [System.Drawing.PointF]::new([float]($cx - 70 * $emblemScale), [float]($cy - 70 * $emblemScale))
    $fPt2 = [System.Drawing.PointF]::new([float]($cx - 20 * $emblemScale), [float]($cy + 70 * $emblemScale))
    $fBrush = [System.Drawing.Drawing2D.LinearGradientBrush]::new($fPt1, $fPt2, $cAccentLight, $cAccentMint)

    # Main Left Vertical Stem (with chamfer)
    $stemPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $stemX = [float]($cx - 62 * $emblemScale)
    $stemY = [float]($cy - 60 * $emblemScale)
    $stemW = [float](24 * $emblemScale)
    $stemH = [float](120 * $emblemScale)
    $stemR = [float](6 * $emblemScale)
    $stemPath.AddArc($stemX, $stemY, [float]($stemR*2), [float]($stemR*2), 180.0, 90.0)
    $stemPath.AddLine([float]($stemX + $stemR*2), $stemY, [float]($stemX + $stemW), $stemY)
    $stemPath.AddLine([float]($stemX + $stemW), $stemY, [float]($stemX + $stemW), [float]($stemY + $stemH))
    $stemPath.AddArc($stemX, [float]($stemY + $stemH - $stemR*2), [float]($stemR*2), [float]($stemR*2), 90.0, 90.0)
    $stemPath.CloseFigure()
    $g.FillPath($fBrush, $stemPath)
    $stemPath.Dispose()

    # Top horizontal F-bar (Top arm of F)
    $topBarPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $topBarX = [float]($cx - 62 * $emblemScale)
    $topBarY = [float]($cy - 60 * $emblemScale)
    $topBarW = [float](75 * $emblemScale)
    $topBarH = [float](22 * $emblemScale)
    $topBarR = [float](6 * $emblemScale)
    $topBarPath.AddArc($topBarX, $topBarY, [float]($topBarR*2), [float]($topBarR*2), 180.0, 90.0)
    $topBarPath.AddArc([float]($topBarX + $topBarW - $topBarR*2), $topBarY, [float]($topBarR*2), [float]($topBarR*2), 270.0, 90.0)
    $topBarPath.AddLine([float]($topBarX + $topBarW), [float]($topBarY + $topBarH), [float]($topBarX + $topBarW - 12 * $emblemScale), [float]($topBarY + $topBarH))
    $topBarPath.AddLine([float]($topBarX + $topBarW - 12 * $emblemScale), [float]($topBarY + $topBarH), $topBarX, [float]($topBarY + $topBarH))
    $topBarPath.CloseFigure()
    $g.FillPath($fBrush, $topBarPath)
    $topBarPath.Dispose()

    # Middle F-bar (Speed flash bar)
    $midBarPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $midBarX = [float]($cx - 42 * $emblemScale)
    $midBarY = [float]($cy - 8 * $emblemScale)
    $midBarW = [float](46 * $emblemScale)
    $midBarH = [float](18 * $emblemScale)
    $midBarPath.AddLine($midBarX, $midBarY, [float]($midBarX + $midBarW - 8 * $emblemScale), $midBarY)
    $midBarPath.AddLine([float]($midBarX + $midBarW - 8 * $emblemScale), $midBarY, [float]($midBarX + $midBarW), [float]($midBarY + $midBarH/2.0))
    $midBarPath.AddLine([float]($midBarX + $midBarW), [float]($midBarY + $midBarH/2.0), [float]($midBarX + $midBarW - 8 * $emblemScale), [float]($midBarY + $midBarH))
    $midBarPath.AddLine([float]($midBarX + $midBarW - 8 * $emblemScale), [float]($midBarY + $midBarH), $midBarX, [float]($midBarY + $midBarH))
    $midBarPath.CloseFigure()
    $midBrush = [System.Drawing.SolidBrush]::new($cAccentNeon)
    $g.FillPath($midBrush, $midBarPath)
    $midBrush.Dispose()
    $midBarPath.Dispose()
    $fBrush.Dispose()

    # Right 'D' Shield & Vault Arc
    $dPt1 = [System.Drawing.PointF]::new([float]($cx - 10 * $emblemScale), [float]($cy - 60 * $emblemScale))
    $dPt2 = [System.Drawing.PointF]::new([float]($cx + 65 * $emblemScale), [float]($cy + 60 * $emblemScale))
    $dBrush = [System.Drawing.Drawing2D.LinearGradientBrush]::new($dPt1, $dPt2, $cAccentMint, $cCyan)

    $dArcPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $dArcX = [float]($cx - 10 * $emblemScale)
    $dArcY = [float]($cy - 60 * $emblemScale)
    $dArcW = [float](75 * $emblemScale)
    $dArcH = [float](120 * $emblemScale)
    $dThickness = [float](22 * $emblemScale)
    $dCornerR = [float](36 * $emblemScale)

    # Outer perimeter of D
    $dArcPath.AddArc($dArcX, $dArcY, [float]($dCornerR*2), [float]($dCornerR*2), 270.0, 90.0)
    $dArcPath.AddLine([float]($dArcX + $dCornerR*2), [float]($dArcY + $dArcH - $dCornerR*2), [float]($dArcX + $dCornerR*2), [float]($dArcY + $dArcH))
    $dArcPath.AddArc($dArcX, [float]($dArcY + $dArcH - $dCornerR*2), [float]($dCornerR*2), [float]($dCornerR*2), 0.0, 90.0)
    $dArcPath.AddLine([float]($dArcX + $dCornerR), [float]($dArcY + $dArcH), $dArcX, [float]($dArcY + $dArcH))
    $dArcPath.AddLine($dArcX, [float]($dArcY + $dArcH), $dArcX, [float]($dArcY + $dArcH - $dThickness))
    
    # Inner cutout of D
    $inR = [float]($dCornerR - $dThickness)
    $dArcPath.AddArc([float]($dArcX + $dThickness), [float]($dArcY + $dArcH - $dThickness - $inR*2), [float]($inR*2), [float]($inR*2), 90.0, -90.0)
    $dArcPath.AddArc([float]($dArcX + $dThickness), $dArcY, [float]($inR*2), [float]($inR*2), 0.0, -90.0)
    $dArcPath.AddLine($dArcX, [float]($dArcY + $dThickness), $dArcX, $dArcY)
    $dArcPath.CloseFigure()

    $g.FillPath($dBrush, $dArcPath)
    $dArcPath.Dispose()
    $dBrush.Dispose()

    # Core Data Verification Pulse (Diamond / Spark inside D)
    $spPt1 = [System.Drawing.PointF]::new([float]($cx + 18 * $emblemScale), [float]($cy - 12 * $emblemScale))
    $spPt2 = [System.Drawing.PointF]::new([float]($cx + 34 * $emblemScale), [float]($cy + 12 * $emblemScale))
    $sparkBrush = [System.Drawing.Drawing2D.LinearGradientBrush]::new($spPt1, $spPt2, $cWhite, $cAccentLight)

    $sparkPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $spX = [float]($cx + 20 * $emblemScale)
    $spY = [float]$cy
    $spR = [float](12 * $emblemScale)
    [System.Drawing.PointF[]]$pts = @(
        [System.Drawing.PointF]::new($spX, [float]($spY - $spR)),
        [System.Drawing.PointF]::new([float]($spX + $spR * 0.8), $spY),
        [System.Drawing.PointF]::new($spX, [float]($spY + $spR)),
        [System.Drawing.PointF]::new([float]($spX - $spR * 0.8), $spY)
    )
    $sparkPath.AddPolygon($pts)
    $g.FillPath($sparkBrush, $sparkPath)
    $sparkPath.Dispose()
    $sparkBrush.Dispose()

    # 3. Large Size Typography (if requested & size >= 256)
    if ($showText -and $size -ge 256) {
        $fontTitle = [System.Drawing.Font]::new("Segoe UI", [float](28.0 * $scale), [System.Drawing.FontStyle]::Bold)
        $fontSub   = [System.Drawing.Font]::new("Segoe UI Semibold", [float](9.5 * $scale), [System.Drawing.FontStyle]::Bold)
        $fmt = [System.Drawing.StringFormat]::new()
        $fmt.Alignment = [System.Drawing.StringAlignment]::Center

        $textY = [float]($size * 0.72)
        $titleRect = [System.Drawing.RectangleF]::new(0.0, $textY, [float]$size, [float](36.0 * $scale))
        $titleBrush = [System.Drawing.SolidBrush]::new($cWhite)
        $g.DrawString("FileDO", $fontTitle, $titleBrush, $titleRect, $fmt)
        $titleBrush.Dispose()

        # Tagline "STORAGE INTEGRITY"
        $subY = [float]($textY + 34.0 * $scale)
        $subRect = [System.Drawing.RectangleF]::new(0.0, $subY, [float]$size, [float](18.0 * $scale))
        $subBrush = [System.Drawing.SolidBrush]::new($cAccentMint)
        $g.DrawString("STORAGE INTEGRITY", $fontSub, $subBrush, $subRect, $fmt)
        $subBrush.Dispose()

        $fontTitle.Dispose()
        $fontSub.Dispose()
        $fmt.Dispose()
    }

    $platePath.Dispose()
    $g.Dispose()
    return $bmp
}

# Function to save multi-frame ICO file
function Save-MultiFrameIcon([string]$icoPath, [System.Drawing.Bitmap[]]$bitmaps) {
    $fs = [System.IO.FileStream]::new($icoPath, [System.IO.FileMode]::Create)
    $bw = [System.IO.BinaryWriter]::new($fs)

    $count = $bitmaps.Length
    # ICONHEADER
    $bw.Write([uint16]0)       # Reserved
    $bw.Write([uint16]1)       # Type (1 = ICO)
    $bw.Write([uint16]$count)   # Number of images

    $pngList = [System.Collections.Generic.List[byte[]]]::new()
    foreach ($b in $bitmaps) {
        $ms = [System.IO.MemoryStream]::new()
        $b.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
        $pngList.Add($ms.ToArray())
        $ms.Dispose()
    }

    # Header size: 6 bytes + (16 bytes * count)
    $offset = 6 + (16 * $count)

    for ($i = 0; $i -lt $count; $i++) {
        $b = $bitmaps[$i]
        $bytes = $pngList[$i]
        $w = if ($b.Width -ge 256) { [byte]0 } else { [byte]$b.Width }
        $h = if ($b.Height -ge 256) { [byte]0 } else { [byte]$b.Height }

        $bw.Write([byte]$w)            # Width
        $bw.Write([byte]$h)            # Height
        $bw.Write([byte]0)             # Color palette count
        $bw.Write([byte]0)             # Reserved
        $bw.Write([uint16]1)           # Color planes
        $bw.Write([uint16]32)          # Bits per pixel
        $bw.Write([uint32]$bytes.Length) # Image size in bytes
        $bw.Write([uint32]$offset)       # Image offset in file

        $offset += $bytes.Length
    }

    # Write actual image data
    for ($i = 0; $i -lt $count; $i++) {
        $bw.Write($pngList[$i])
    }

    $bw.Flush()
    $fs.Dispose()
}

# Function to render the social artwork at any canvas size ($w x $h).
# The artwork is laid out on a fixed 1280x640 design canvas, scaled uniformly by
# min($w/1280, $h/640) and centred; the background gradient fills the whole canvas.
# At 1280x640 the scale is 1 and the offset 0 (the repository social preview);
# at 1200x630 everything is scaled to 93.75 % and centred (the Open Graph image) -
# nothing is cropped, nothing is re-laid out.
function Render-SocialCard([int]$w, [int]$h, [string]$outPath) {
    $designW = 1280.0
    $designH = 640.0
    $s  = [float][Math]::Min($w / $designW, $h / $designH)
    $ox = [float](($w - $designW * $s) / 2.0)
    $oy = [float](($h - $designH * $s) / 2.0)
    $bmp = [System.Drawing.Bitmap]::new($w, $h)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode     = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode   = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit

    # Background gradient
    $pt1 = [System.Drawing.PointF]::new(0.0, 0.0)
    $pt2 = [System.Drawing.PointF]::new([float]$w, [float]$h)
    $bgBrush = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        $pt1, $pt2,
        [System.Drawing.Color]::FromArgb(255, 6, 12, 8),
        [System.Drawing.Color]::FromArgb(255, 12, 22, 16)
    )
    $g.FillRectangle($bgBrush, 0, 0, $w, $h)
    $bgBrush.Dispose()

    # Everything below is positioned on the 1280x640 design canvas
    $g.TranslateTransform($ox, $oy)
    $g.ScaleTransform($s, $s)

    # Decorative background glow
    $glowPen = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(20, 94, 214, 132), 2.0)
    $g.DrawEllipse($glowPen, 80, 100, 440, 440)
    $glowPen.Dispose()

    # Draw Master Emblem on left (360x360 on the design canvas). It is rendered at its
    # final pixel size and blitted 1:1 outside the world transform, so it stays sharp.
    $emblemPx = [int][Math]::Round(360.0 * $s)
    $emblem = Render-FileDOIcon -size $emblemPx -showText $false
    $gState = $g.Save()
    $g.ResetTransform()
    $g.DrawImage($emblem, [int][Math]::Round($ox + 100.0 * $s), [int][Math]::Round($oy + 140.0 * $s), $emblemPx, $emblemPx)
    $g.Restore($gState)
    $emblem.Dispose()

    # Draw Right Side Branding & Copy
    $fKicker = [System.Drawing.Font]::new("Segoe UI", 15.0, [System.Drawing.FontStyle]::Bold)
    $fTitle  = [System.Drawing.Font]::new("Segoe UI", 48.0, [System.Drawing.FontStyle]::Bold)
    $fSlogan = [System.Drawing.Font]::new("Segoe UI Semibold", 22.0, [System.Drawing.FontStyle]::Bold)
    $fBody   = [System.Drawing.Font]::new("Segoe UI", 16.0)
    $fBadge  = [System.Drawing.Font]::new("Segoe UI Semibold", 13.0, [System.Drawing.FontStyle]::Bold)

    # Kicker
    $kBrush = [System.Drawing.SolidBrush]::new($cAccentMint)
    $g.DrawString("WINDOWS CLI & GUI STORAGE TOOLKIT", $fKicker, $kBrush, 520.0, 130.0)
    $kBrush.Dispose()

    # Title "FileDO"
    $tBrush = [System.Drawing.SolidBrush]::new($cWhite)
    $g.DrawString("FileDO", $fTitle, $tBrush, 516.0, 160.0)
    $tBrush.Dispose()

    # Slogan
    $sBrush = [System.Drawing.SolidBrush]::new($cAccentLight)
    $g.DrawString("Know your storage before you trust it", $fSlogan, $sBrush, 520.0, 245.0)
    $sBrush.Dispose()

    # Description - pillar order of packaging\positioning-source.json (check, tidy, erase, protect);
    # wording kept inside docs\termbase.json (no "vault", no "encrypted", "fake" not "counterfeit")
    # and the over-claim lines (a wipe "makes recovery harder", a container is password-protected).
    $desc = "Storage speed tests, fake capacity detection, duplicate finder,`nwipe that makes recovery harder, password-protected .fd-sec containers."
    $dBrush = [System.Drawing.SolidBrush]::new($cSubtleText)
    $g.DrawString($desc, $fBody, $dBrush, 520.0, 295.0)
    $dBrush.Dispose()

    # Capability Pills / Badges
    $badges = @("Speed Benchmark", "Fake Capacity Test", "Secret Files .fd-sec", "Wipe & Fill", "Duplicates")
    $bx = 520.0
    $by = 400.0
    foreach ($b in $badges) {
        $badgeSize = $g.MeasureString($b, $fBadge)
        $bw = [float]($badgeSize.Width + 24)
        $bh = [float]34
        $bRect = Create-RoundedRectPath $bx $by $bw $bh 8.0
        $fillB = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 18, 32, 22))
        $g.FillPath($fillB, $bRect)
        $fillB.Dispose()
        $penB = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(160, 94, 214, 132), 1.5)
        $g.DrawPath($penB, $bRect)
        $penB.Dispose()
        $txtB = [System.Drawing.SolidBrush]::new($cWhite)
        $g.DrawString($b, $fBadge, $txtB, [float]($bx + 12), [float]($by + 7))
        $txtB.Dispose()
        $bRect.Dispose()
        $bx += $bw + 12.0
        if ($bx -gt 1180.0) {
            $bx = 520.0
            $by += 44.0
        }
    }

    # Platform tag
    $tagBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(180, 150, 173, 156))
    $dot = " " + [char]0x00B7 + " "   # middle dot, spelled as a code point so the file stays pure ASCII
    $platformTag = ("Windows x64", "Portable", "Setup EXE", "WiX MSI", "MSIX Store", "Go + VB.NET") -join $dot
    $g.DrawString($platformTag, $fBadge, $tagBrush, 520.0, 520.0)
    $tagBrush.Dispose()

    $fKicker.Dispose(); $fTitle.Dispose(); $fSlogan.Dispose(); $fBody.Dispose(); $fBadge.Dispose()
    $g.Dispose()

    $bmp.Save($outPath, [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()
}

# --- Generation Execution ---
if ($SocialOnly) {
    Write-Host "Generating FileDO social artwork only (-SocialOnly)..."
} else {
    Write-Host "Generating FileDO product icons..."
}

if (-not $SocialOnly) {
    # 1. Generate PNGs
    $icon256 = Render-FileDOIcon -size 256 -showText $false
    $icon256Path = Join-Path $assetsDir "icon.png"
    $icon256.Save($icon256Path, [System.Drawing.Imaging.ImageFormat]::Png)

    $icon512 = Render-FileDOIcon -size 512 -showText $true
    $icon512Path = Join-Path $assetsDir "icon-branded-512.png"
    $icon512.Save($icon512Path, [System.Drawing.Imaging.ImageFormat]::Png)
    $icon512.Dispose()

    # Web icon
    if (Test-Path $docsAssets) {
        Copy-Item $icon256Path (Join-Path $docsAssets "filedo.png") -Force
    }

    # 2. Generate multi-resolution ICO
    $icoSizes = @(16, 24, 32, 48, 64, 128, 256)
    $bitmaps = @()
    foreach ($s in $icoSizes) {
        $bitmaps += (Render-FileDOIcon -size $s -showText $false)
    }
    $icoPath = Join-Path $assetsDir "icon.ico"
    Save-MultiFrameIcon -icoPath $icoPath -bitmaps $bitmaps
    foreach ($b in $bitmaps) { $b.Dispose() }
    $icon256.Dispose()
}

# 3. Generate the social artwork: one drawing, two sizes
#    1280x640 - the GitHub repository social preview (assets\ and docs\assets\)
#    1200x630 - the Open Graph image (docs\assets\social-card.png)
#    Both PNGs must stay under 1 MB.
$socialAssets = Join-Path $assetsDir "social-preview.png"
Render-SocialCard -w 1280 -h 640 -outPath $socialAssets
$socialFiles = @($socialAssets)
if (Test-Path $docsAssets) {
    Copy-Item $socialAssets (Join-Path $docsAssets "social-preview.png") -Force
    $socialCard = Join-Path $docsAssets "social-card.png"
    Render-SocialCard -w 1200 -h 630 -outPath $socialCard
    $socialFiles += $socialCard
}
foreach ($f in $socialFiles) {
    $len = (Get-Item -LiteralPath $f).Length
    if ($len -ge 1000000) { throw "$f is $len bytes - a social image must stay under 1 MB" }
}

# 4. Generate MSIX Package Logos
$msixAssets = Join-Path $root "msix\stage\Assets"
if ((-not $SocialOnly) -and (Test-Path $msixAssets)) {
    function New-MsixLogo([string]$src, [string]$dst, [int]$size) {
        $img = [System.Drawing.Image]::FromFile($src)
        try {
            $bmp = [System.Drawing.Bitmap]::new($size, $size)
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

    New-MsixLogo $icon256Path (Join-Path $msixAssets "Square44x44Logo.png") 44
    New-MsixLogo $icon256Path (Join-Path $msixAssets "Square71x71Logo.png") 71
    New-MsixLogo $icon256Path (Join-Path $msixAssets "Square150x150Logo.png") 150
    New-MsixLogo $icon256Path (Join-Path $msixAssets "StoreLogo.png") 50

    foreach ($size in 16, 24, 32, 48, 256) {
        foreach ($form in '', '_altform-unplated', '_altform-lightunplated') {
            $leaf = "Square44x44Logo.targetsize-$size$form.png"
            New-MsixLogo $icon256Path (Join-Path $msixAssets $leaf) $size
        }
    }
}

if ($SocialOnly) {
    Write-Host "FileDO social artwork successfully generated (social-preview 1280x640, social-card 1200x630)."
} else {
    Write-Host "FileDO product icons successfully generated across product, site, packaging, and MSIX."
}
