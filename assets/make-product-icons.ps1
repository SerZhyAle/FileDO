# ============================================================================
#  FileDO - the product's pictures, from one drawing (PRODUCT-IDENTITY)
#
#  The mark is drawn here. Get-MarkShapes holds it as data - the plate, the track accents, the F, the
#  speed bar, the D and the spark, in the pixel space of the canvas they are drawn on - and that data is
#  the master (PRODUCT-IDENTITY rule 33): every raster below is those shapes replayed through GDI+, and
#  the same run writes the same shapes, path for path, as SVG. Change the shapes, never an output.
#
#  The drawing has three forms, declared in the manifest (rules 2, 3 and 34):
#    full    64 px and up     assets\identity\master\filedo-mark.svg          the master, 256 units
#    medium  33 to 63 px      assets\identity\master\filedo-mark-medium.svg   no track accents, 48 units
#    small   32 px and below  assets\identity\master\filedo-mark-small.svg    the plate fills the square, 32 units
#  A stroke is never thinner than 1 px at any size; that is hinting, not a fourth form.
#
#  Renditions, each listed with its SHA-256 in assets\identity\identity-manifest.json (section 5):
#    assets\icon.ico                  16 24 32 48 64 128 256 (16-32 small, 48 medium, 64 and up full): the four
#                                     exes, the installer and its setup EXE, the window, taskbar and tray, the
#                                     Start menu and desktop shortcuts, the Explorer entry (staged as
#                                     FileDO.ico), the Disk Manager's main-window button
#    assets\icon.png                  256: the README head; the source of the installer art and the MSIX logos
#    docs\assets\filedo.png           256, a byte copy: the site's favicon, the guides' header mark, the hub card
#    docs\assets\filedo.svg           the medium form, a byte copy: the site's header identity
#    docs\help\filedo-mark.svg        the medium form, a byte copy: the help pages' title picture
#    docs\help\favicon.png            a byte copy of the site's favicon: the help pages' icon
#    assets\social-preview.png        1280x640, and its copy docs\assets\social-preview.png: the GitHub social preview
#    docs\assets\social-card.png      1200x630: the Open Graph image (one artwork with the preview, scaled)
#    assets\identity\renditions\msix  the 19 logos msix\build-msix.ps1 packs, cut from the 256 px PNG
#    packaging\wix\banner.bmp         493x58, and dialog.bmp 493x312: the installer wizard's art
#  The Microsoft Store logo set in msix\store-logos\ is rendered from the master SVG by
#  msix\make-store-logos.ps1 (headless Edge, about a minute; -StoreLogos runs it) and listed with the hash
#  of the drawing it was rendered from (renderedFrom); a set rendered from another drawing is a NOTE.
#
#  A rerun on the unchanged drawing writes the same bytes (section 6 rung 3), and -Check proves it:
#    pwsh -NoProfile -File assets\make-product-icons.ps1               write every file and the manifest
#    pwsh -NoProfile -File assets\make-product-icons.ps1 -Check        write nothing: render into a scratch
#                                                                      folder and compare every file
#    pwsh -NoProfile -File assets\make-product-icons.ps1 -StoreLogos   also re-render the Store logos (release)
#  -SocialOnly is accepted for old callers and changes nothing: every run writes the whole set, because the
#  manifest hashes every file and a rerun leaves an unchanged file as it was.
#
#  Exit codes (CHECK-VERDICT): 0 = written, or -Check found every file as the drawing gives it; 1 = -Check
#  found a file that differs or a retired second drawing; 2 = could not verify. The last line is the
#  verdict: identity: PASS | FAIL | COULD NOT VERIFY (..).
# ============================================================================
[CmdletBinding()]
param(
    [switch]$Check,
    [switch]$StoreLogos,
    [switch]$SocialOnly
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$root = Split-Path $PSScriptRoot -Parent

function Exit-Verdict([int]$code, [string]$text) {
    Write-Output "identity: $text"
    exit $code
}

if ($Check -and $StoreLogos) { Exit-Verdict 2 'COULD NOT VERIFY (-Check writes nothing, so it cannot re-render the Store logos)' }

# Where this run writes: the repository itself, or a scratch folder with the same layout for -Check.
$out = $root
$scratch = $null
if ($Check) {
    $scratch = Join-Path ([IO.Path]::GetTempPath()) ('filedo-identity-' + [guid]::NewGuid().ToString('N'))
    $out = $scratch
}

# Every path the manifest names, relative to the repository root with forward slashes (section 5).
$rel = [ordered]@{
    MasterFull   = 'assets/identity/master/filedo-mark.svg'
    MasterMedium = 'assets/identity/master/filedo-mark-medium.svg'
    MasterSmall  = 'assets/identity/master/filedo-mark-small.svg'
    Ico          = 'assets/icon.ico'
    Png256       = 'assets/icon.png'
    SitePng      = 'docs/assets/filedo.png'
    SiteSvg      = 'docs/assets/filedo.svg'
    HelpSvg      = 'docs/help/filedo-mark.svg'
    HelpFavicon  = 'docs/help/favicon.png'
    Preview      = 'assets/social-preview.png'
    SitePreview  = 'docs/assets/social-preview.png'
    Card         = 'docs/assets/social-card.png'
    MsixDir      = 'assets/identity/renditions/msix'
    Banner       = 'packaging/wix/banner.bmp'
    Dialog       = 'packaging/wix/dialog.bmp'
    Manifest     = 'assets/identity/identity-manifest.json'
    StoreLogos   = 'msix/store-logos'
}
# The second drawing and the copies this generator replaced; -Check fails while one is back (rule 3).
$retired = @('assets/icon.svg', 'assets/icon-branded-512.png', 'packaging/wix/make-art.ps1')

function Get-OutPath([string]$r) { Join-Path $out ($r -replace '/', '\') }
function Get-RepoPath([string]$r) { Join-Path $root ($r -replace '/', '\') }
function New-ParentFolder([string]$file) {
    $dir = Split-Path -Parent $file
    if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
}

# The files this run wrote, relative; -Check compares exactly these with the repository.
$written = [System.Collections.Generic.List[string]]::new()
function Register-Written([string]$r) { if (-not $written.Contains($r)) { $written.Add($r) } }

# Color Palette
$cBgDark      = [System.Drawing.Color]::FromArgb(255, 10, 16, 12)       # Deep obsidian forest #0A100C - master.background
$cBgSurface   = [System.Drawing.Color]::FromArgb(255, 15, 26, 18)       # Dark emerald panel #0F1A12
$cBgPlateEnd  = [System.Drawing.Color]::FromArgb(255, 8, 14, 10)        # Gradient dark end
$cBorderOuter = [System.Drawing.Color]::FromArgb(180, 94, 214, 132)     # Emerald outer border
$cAccentMint  = [System.Drawing.Color]::FromArgb(255, 94, 214, 132)     # Signature mint #5ED684
$cAccentNeon  = [System.Drawing.Color]::FromArgb(255, 0, 230, 118)      # Vivid neon #00E676
$cAccentLight = [System.Drawing.Color]::FromArgb(255, 167, 243, 208)    # Highlight #A7F3D0
$cWhite       = [System.Drawing.Color]::FromArgb(255, 255, 255, 255)
$cCyan        = [System.Drawing.Color]::FromArgb(255, 56, 189, 248)     # Cyan data pulse #38BDF8

# ---- the drawing ---------------------------------------------------------------------------------
# A shape is one GDI+ call: 'fill' (a path filled with a solid or linear-gradient brush), 'stroke' (a path
# drawn with a pen) or 'line' (one DrawLine). A path is a list of figures; a figure is a list of ops -
# 'arc' (GraphicsPath.AddArc: bounding box, start angle, sweep), 'line' (AddLine) or 'poly' (AddPolygon) -
# closed when the drawing closes it. Every number is computed as the drawing always computed it, so the
# rasters stay the bytes they were.

function Get-RoundedRectOps([float]$x, [float]$y, [float]$w, [float]$h, [float]$r) {
    $d = [float]($r * 2.0)
    return @(
        @{ T = 'arc'; X = $x; Y = $y; W = $d; H = $d; S = 180.0; D = 90.0 },
        @{ T = 'arc'; X = [float]($x + $w - $d); Y = $y; W = $d; H = $d; S = 270.0; D = 90.0 },
        @{ T = 'arc'; X = [float]($x + $w - $d); Y = [float]($y + $h - $d); W = $d; H = $d; S = 0.0; D = 90.0 },
        @{ T = 'arc'; X = $x; Y = [float]($y + $h - $d); W = $d; H = $d; S = 90.0; D = 90.0 }
    )
}

function Get-MarkShapes([int]$size) {
    $scale = [float]$size / 256.0
    $shapes = [System.Collections.Generic.List[object]]::new()

    # 1. Outer plate: a rounded square with a gradient and a border. The small form (32 px and below)
    #    lets the plate fill the square with a tighter corner.
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
    $plate = @(@{ Ops = (Get-RoundedRectOps $pad $pad $plateW $plateH $cornerR); Closed = $true })
    $shapes.Add(@{ Kind = 'fill'; Figures = $plate
        Fill = @{ T = 'linear'; X1 = $pad; Y1 = $pad; X2 = [float]($pad + $plateW); Y2 = [float]($pad + $plateH); C1 = $cBgSurface; C2 = $cBgPlateEnd } })

    # Track accents on the full form only (64 px and up): the medium and small forms leave them out.
    if ($size -ge 64) {
        $trackColour = [System.Drawing.Color]::FromArgb(25, 94, 214, 132)
        $trackWidth = [float](1.5 * $scale)
        $shapes.Add(@{ Kind = 'line'; Color = $trackColour; Width = $trackWidth
            X1 = [float]($pad + 20 * $scale); Y1 = [float]($pad + 40 * $scale); X2 = [float]($pad + 60 * $scale); Y2 = [float]($pad + 40 * $scale) })
        $shapes.Add(@{ Kind = 'line'; Color = $trackColour; Width = $trackWidth
            X1 = [float]($pad + 60 * $scale); Y1 = [float]($pad + 40 * $scale); X2 = [float]($pad + 80 * $scale); Y2 = [float]($pad + 60 * $scale) })
        $shapes.Add(@{ Kind = 'line'; Color = $trackColour; Width = $trackWidth
            X1 = [float]($pad + $plateW - 60 * $scale); Y1 = [float]($pad + $plateH - 40 * $scale); X2 = [float]($pad + $plateW - 20 * $scale); Y2 = [float]($pad + $plateH - 40 * $scale) })
    }

    # Plate border: never thinner than 1 px.
    $shapes.Add(@{ Kind = 'stroke'; Figures = $plate; Color = $cBorderOuter; Width = [float][Math]::Max(1.0, 2.5 * $scale) })

    # 2. The emblem: the F (stem, top bar, speed bar), the D and the spark inside it.
    $cx = [float]($size / 2.0)
    $cy = [float]($size / 2.0)
    $emblemScale = [float](0.92 * $scale)
    $fFill = @{ T = 'linear'
        X1 = [float]($cx - 70 * $emblemScale); Y1 = [float]($cy - 70 * $emblemScale)
        X2 = [float]($cx - 20 * $emblemScale); Y2 = [float]($cy + 70 * $emblemScale)
        C1 = $cAccentLight; C2 = $cAccentMint }

    # Main left vertical stem (with chamfer)
    $stemX = [float]($cx - 62 * $emblemScale)
    $stemY = [float]($cy - 60 * $emblemScale)
    $stemW = [float](24 * $emblemScale)
    $stemH = [float](120 * $emblemScale)
    $stemR = [float](6 * $emblemScale)
    $shapes.Add(@{ Kind = 'fill'; Fill = $fFill; Figures = @(@{ Closed = $true; Ops = @(
        @{ T = 'arc'; X = $stemX; Y = $stemY; W = [float]($stemR*2); H = [float]($stemR*2); S = 180.0; D = 90.0 },
        @{ T = 'line'; X1 = [float]($stemX + $stemR*2); Y1 = $stemY; X2 = [float]($stemX + $stemW); Y2 = $stemY },
        @{ T = 'line'; X1 = [float]($stemX + $stemW); Y1 = $stemY; X2 = [float]($stemX + $stemW); Y2 = [float]($stemY + $stemH) },
        @{ T = 'arc'; X = $stemX; Y = [float]($stemY + $stemH - $stemR*2); W = [float]($stemR*2); H = [float]($stemR*2); S = 90.0; D = 90.0 }
    ) }) })

    # Top horizontal F-bar (top arm of F)
    $topBarX = [float]($cx - 62 * $emblemScale)
    $topBarY = [float]($cy - 60 * $emblemScale)
    $topBarW = [float](75 * $emblemScale)
    $topBarH = [float](22 * $emblemScale)
    $topBarR = [float](6 * $emblemScale)
    $shapes.Add(@{ Kind = 'fill'; Fill = $fFill; Figures = @(@{ Closed = $true; Ops = @(
        @{ T = 'arc'; X = $topBarX; Y = $topBarY; W = [float]($topBarR*2); H = [float]($topBarR*2); S = 180.0; D = 90.0 },
        @{ T = 'arc'; X = [float]($topBarX + $topBarW - $topBarR*2); Y = $topBarY; W = [float]($topBarR*2); H = [float]($topBarR*2); S = 270.0; D = 90.0 },
        @{ T = 'line'; X1 = [float]($topBarX + $topBarW); Y1 = [float]($topBarY + $topBarH); X2 = [float]($topBarX + $topBarW - 12 * $emblemScale); Y2 = [float]($topBarY + $topBarH) },
        @{ T = 'line'; X1 = [float]($topBarX + $topBarW - 12 * $emblemScale); Y1 = [float]($topBarY + $topBarH); X2 = $topBarX; Y2 = [float]($topBarY + $topBarH) }
    ) }) })

    # Middle F-bar (speed flash bar)
    $midBarX = [float]($cx - 42 * $emblemScale)
    $midBarY = [float]($cy - 8 * $emblemScale)
    $midBarW = [float](46 * $emblemScale)
    $midBarH = [float](18 * $emblemScale)
    $shapes.Add(@{ Kind = 'fill'; Fill = @{ T = 'solid'; C = $cAccentNeon }; Figures = @(@{ Closed = $true; Ops = @(
        @{ T = 'line'; X1 = $midBarX; Y1 = $midBarY; X2 = [float]($midBarX + $midBarW - 8 * $emblemScale); Y2 = $midBarY },
        @{ T = 'line'; X1 = [float]($midBarX + $midBarW - 8 * $emblemScale); Y1 = $midBarY; X2 = [float]($midBarX + $midBarW); Y2 = [float]($midBarY + $midBarH/2.0) },
        @{ T = 'line'; X1 = [float]($midBarX + $midBarW); Y1 = [float]($midBarY + $midBarH/2.0); X2 = [float]($midBarX + $midBarW - 8 * $emblemScale); Y2 = [float]($midBarY + $midBarH) },
        @{ T = 'line'; X1 = [float]($midBarX + $midBarW - 8 * $emblemScale); Y1 = [float]($midBarY + $midBarH); X2 = $midBarX; Y2 = [float]($midBarY + $midBarH) }
    ) }) })

    # Right D shield and vault arc
    $dArcX = [float]($cx - 10 * $emblemScale)
    $dArcY = [float]($cy - 60 * $emblemScale)
    $dArcH = [float](120 * $emblemScale)
    $dThickness = [float](22 * $emblemScale)
    $dCornerR = [float](36 * $emblemScale)
    $inR = [float]($dCornerR - $dThickness)
    $shapes.Add(@{ Kind = 'fill'
        Fill = @{ T = 'linear'
            X1 = [float]($cx - 10 * $emblemScale); Y1 = [float]($cy - 60 * $emblemScale)
            X2 = [float]($cx + 65 * $emblemScale); Y2 = [float]($cy + 60 * $emblemScale)
            C1 = $cAccentMint; C2 = $cCyan }
        Figures = @(@{ Closed = $true; Ops = @(
        # Outer perimeter of D
        @{ T = 'arc'; X = $dArcX; Y = $dArcY; W = [float]($dCornerR*2); H = [float]($dCornerR*2); S = 270.0; D = 90.0 },
        @{ T = 'line'; X1 = [float]($dArcX + $dCornerR*2); Y1 = [float]($dArcY + $dArcH - $dCornerR*2); X2 = [float]($dArcX + $dCornerR*2); Y2 = [float]($dArcY + $dArcH) },
        @{ T = 'arc'; X = $dArcX; Y = [float]($dArcY + $dArcH - $dCornerR*2); W = [float]($dCornerR*2); H = [float]($dCornerR*2); S = 0.0; D = 90.0 },
        @{ T = 'line'; X1 = [float]($dArcX + $dCornerR); Y1 = [float]($dArcY + $dArcH); X2 = $dArcX; Y2 = [float]($dArcY + $dArcH) },
        @{ T = 'line'; X1 = $dArcX; Y1 = [float]($dArcY + $dArcH); X2 = $dArcX; Y2 = [float]($dArcY + $dArcH - $dThickness) },
        # Inner cutout of D
        @{ T = 'arc'; X = [float]($dArcX + $dThickness); Y = [float]($dArcY + $dArcH - $dThickness - $inR*2); W = [float]($inR*2); H = [float]($inR*2); S = 90.0; D = -90.0 },
        @{ T = 'arc'; X = [float]($dArcX + $dThickness); Y = $dArcY; W = [float]($inR*2); H = [float]($inR*2); S = 0.0; D = -90.0 },
        @{ T = 'line'; X1 = $dArcX; Y1 = [float]($dArcY + $dThickness); X2 = $dArcX; Y2 = $dArcY }
    ) }) })

    # Core data verification pulse (diamond spark inside D)
    $spX = [float]($cx + 20 * $emblemScale)
    $spY = [float]$cy
    $spR = [float](12 * $emblemScale)
    $shapes.Add(@{ Kind = 'fill'
        Fill = @{ T = 'linear'
            X1 = [float]($cx + 18 * $emblemScale); Y1 = [float]($cy - 12 * $emblemScale)
            X2 = [float]($cx + 34 * $emblemScale); Y2 = [float]($cy + 12 * $emblemScale)
            C1 = $cWhite; C2 = $cAccentLight }
        Figures = @(@{ Closed = $false; Ops = @(
        @{ T = 'poly'; P = @(
            @($spX, [float]($spY - $spR)),
            @([float]($spX + $spR * 0.8), $spY),
            @($spX, [float]($spY + $spR)),
            @([float]($spX - $spR * 0.8), $spY)) }
    ) }) })

    return $shapes
}

# ---- the drawing replayed through GDI+ -----------------------------------------------------------

function New-ShapePath($figures) {
    $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
    foreach ($f in $figures) {
        foreach ($op in $f.Ops) {
            switch ($op.T) {
                'arc'  { $path.AddArc([float]$op.X, [float]$op.Y, [float]$op.W, [float]$op.H, [float]$op.S, [float]$op.D) }
                'line' { $path.AddLine([float]$op.X1, [float]$op.Y1, [float]$op.X2, [float]$op.Y2) }
                'poly' {
                    [System.Drawing.PointF[]]$pts = @(foreach ($p in $op.P) { [System.Drawing.PointF]::new([float]$p[0], [float]$p[1]) })
                    $path.AddPolygon($pts)
                }
            }
        }
        if ($f.Closed) { $path.CloseFigure() }
    }
    return $path
}

function New-ShapeBrush($fill) {
    if ($fill.T -eq 'linear') {
        $p1 = [System.Drawing.PointF]::new([float]$fill.X1, [float]$fill.Y1)
        $p2 = [System.Drawing.PointF]::new([float]$fill.X2, [float]$fill.Y2)
        return [System.Drawing.Drawing2D.LinearGradientBrush]::new($p1, $p2, $fill.C1, $fill.C2)
    }
    return [System.Drawing.SolidBrush]::new($fill.C)
}

function Render-FileDOIcon([int]$size) {
    $bmp = [System.Drawing.Bitmap]::new($size, $size)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode     = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode   = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit
    $g.Clear([System.Drawing.Color]::Transparent)
    foreach ($shape in (Get-MarkShapes $size)) {
        switch ($shape.Kind) {
            'fill' {
                $path = New-ShapePath $shape.Figures
                $brush = New-ShapeBrush $shape.Fill
                $g.FillPath($brush, $path)
                $brush.Dispose()
                $path.Dispose()
            }
            'stroke' {
                $path = New-ShapePath $shape.Figures
                $pen = [System.Drawing.Pen]::new($shape.Color, [float]$shape.Width)
                $g.DrawPath($pen, $path)
                $pen.Dispose()
                $path.Dispose()
            }
            'line' {
                $pen = [System.Drawing.Pen]::new($shape.Color, [float]$shape.Width)
                $g.DrawLine($pen, [float]$shape.X1, [float]$shape.Y1, [float]$shape.X2, [float]$shape.Y2)
                $pen.Dispose()
            }
        }
    }
    $g.Dispose()
    return $bmp
}

# ---- the drawing written as SVG (rule 33: the same shapes, path for path) ------------------------

$inv = [System.Globalization.CultureInfo]::InvariantCulture

function Format-Num([double]$v) {
    $r = [Math]::Round($v, 4)
    if ($r -eq 0) { return '0' }
    return $r.ToString('0.####', $inv)
}

function Format-Colour([System.Drawing.Color]$c) { '#{0:X2}{1:X2}{2:X2}' -f $c.R, $c.G, $c.B }

# A point of an AddArc ellipse at an angle in degrees, clockwise from +x in a y-down space - GDI+'s reading.
function Get-ArcPoint($op, [double]$deg) {
    $rx = [double]$op.W / 2.0
    $ry = [double]$op.H / 2.0
    $a = $deg * [Math]::PI / 180.0
    return @(([double]$op.X + $rx + $rx * [Math]::Cos($a)), ([double]$op.Y + $ry + $ry * [Math]::Sin($a)))
}

# Path data as GDI+ builds the path: inside a figure every op starts with a line from the point the
# previous op ended at, unless it starts there; a figure the drawing closes ends in Z.
function Get-PathData($figures) {
    $parts = [System.Collections.Generic.List[string]]::new()
    foreach ($f in $figures) {
        $cur = $null
        foreach ($op in $f.Ops) {
            switch ($op.T) {
                'arc' {
                    $s = Get-ArcPoint $op ([double]$op.S)
                    $e = Get-ArcPoint $op ([double]$op.S + [double]$op.D)
                    $sp = "$(Format-Num $s[0]) $(Format-Num $s[1])"
                    if ($null -eq $cur) { $parts.Add("M$sp") } elseif ($cur -ne $sp) { $parts.Add("L$sp") }
                    $large = if ([Math]::Abs([double]$op.D) -gt 180) { 1 } else { 0 }
                    $sweep = if ([double]$op.D -gt 0) { 1 } else { 0 }
                    $ep = "$(Format-Num $e[0]) $(Format-Num $e[1])"
                    $parts.Add("A$(Format-Num ([double]$op.W / 2.0)) $(Format-Num ([double]$op.H / 2.0)) 0 $large $sweep $ep")
                    $cur = $ep
                }
                'line' {
                    $sp = "$(Format-Num ([double]$op.X1)) $(Format-Num ([double]$op.Y1))"
                    $ep = "$(Format-Num ([double]$op.X2)) $(Format-Num ([double]$op.Y2))"
                    if ($null -eq $cur) { $parts.Add("M$sp") } elseif ($cur -ne $sp) { $parts.Add("L$sp") }
                    $parts.Add("L$ep")
                    $cur = $ep
                }
                'poly' {
                    $first = $true
                    foreach ($p in $op.P) {
                        $pp = "$(Format-Num ([double]$p[0])) $(Format-Num ([double]$p[1]))"
                        $parts.Add($(if ($first) { "M$pp" } else { "L$pp" }))
                        $first = $false
                    }
                    $parts.Add('Z')
                    $cur = $null
                }
            }
        }
        if ($f.Closed) { $parts.Add('Z') }
    }
    return ($parts -join ' ')
}

function Get-StrokeAttributes([System.Drawing.Color]$c, [double]$width) {
    $a = "fill=""none"" stroke=""$(Format-Colour $c)"""
    if ($c.A -lt 255) { $a += " stroke-opacity=""$(Format-Num ($c.A / 255.0))""" }
    return $a + " stroke-width=""$(Format-Num $width)"""
}

function Get-MarkSvg([int]$size, [string]$form) {
    $defs = [System.Collections.Generic.List[string]]::new()
    $body = [System.Collections.Generic.List[string]]::new()
    $n = 0
    foreach ($shape in (Get-MarkShapes $size)) {
        switch ($shape.Kind) {
            'fill' {
                $f = $shape.Fill
                if ($f.T -eq 'linear') {
                    $n++
                    $stops = foreach ($pair in @(@('0', $f.C1), @('1', $f.C2))) {
                        $stop = "<stop offset=""$($pair[0])"" stop-color=""$(Format-Colour $pair[1])"""
                        if ($pair[1].A -lt 255) { $stop += " stop-opacity=""$(Format-Num ($pair[1].A / 255.0))""" }
                        $stop + '/>'
                    }
                    $defs.Add("<linearGradient id=""g$n"" gradientUnits=""userSpaceOnUse"" x1=""$(Format-Num $f.X1)"" y1=""$(Format-Num $f.Y1)"" x2=""$(Format-Num $f.X2)"" y2=""$(Format-Num $f.Y2)"">$($stops -join '')</linearGradient>")
                    $paint = "fill=""url(#g$n)"""
                } else {
                    $paint = "fill=""$(Format-Colour $f.C)"""
                    if ($f.C.A -lt 255) { $paint += " fill-opacity=""$(Format-Num ($f.C.A / 255.0))""" }
                }
                # GraphicsPath fills in alternate mode: evenodd.
                $body.Add("<path d=""$(Get-PathData $shape.Figures)"" $paint fill-rule=""evenodd""/>")
            }
            'stroke' { $body.Add("<path d=""$(Get-PathData $shape.Figures)"" $(Get-StrokeAttributes $shape.Color $shape.Width)/>") }
            'line' {
                $d = "M$(Format-Num $shape.X1) $(Format-Num $shape.Y1) L$(Format-Num $shape.X2) $(Format-Num $shape.Y2)"
                $body.Add("<path d=""$d"" $(Get-StrokeAttributes $shape.Color $shape.Width)/>")
            }
        }
    }
    $lines = [System.Collections.Generic.List[string]]::new()
    $lines.Add("<svg xmlns=""http://www.w3.org/2000/svg"" viewBox=""0 0 $size $size"" width=""$size"" height=""$size"">")
    $lines.Add("<!-- FileDO mark, $form. Written by assets/make-product-icons.ps1 from the drawing in that script (PRODUCT-IDENTITY rule 33): change the script, never this file. -->")
    $lines.Add('<defs>')
    $lines.AddRange($defs)
    $lines.Add('</defs>')
    $lines.AddRange($body)
    $lines.Add('</svg>')
    return ($lines -join "`n") + "`n"
}

# ---- writing ---------------------------------------------------------------------------------------

function Write-TextFile([string]$r, [string]$text) {
    $file = Get-OutPath $r
    New-ParentFolder $file
    [System.IO.File]::WriteAllText($file, $text.Replace("`r`n", "`n"), [System.Text.UTF8Encoding]::new($false))
    Register-Written $r
}

function Save-Png([System.Drawing.Bitmap]$bmp, [string]$r) {
    $file = Get-OutPath $r
    New-ParentFolder $file
    $bmp.Save($file, [System.Drawing.Imaging.ImageFormat]::Png)
    Register-Written $r
}

# A rendition that stands in a second folder because its surface cannot name the first (rule 36).
function Copy-Rendition([string]$fromRel, [string]$toRel) {
    $to = Get-OutPath $toRel
    New-ParentFolder $to
    Copy-Item -LiteralPath (Get-OutPath $fromRel) -Destination $to -Force
    Register-Written $toRel
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

# Compose the social artwork on the master background, using its mark and canonical name.
# Both target sizes use the same 1280 x 640 layout, scaled and centred.
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

    $g.Clear($cBgDark)
    $g.TranslateTransform($ox, $oy)
    $g.ScaleTransform($s, $s)

    # Draw Master Emblem on left (360x360 on the design canvas). It is rendered at its
    # final pixel size and blitted 1:1 outside the world transform, so it stays sharp.
    $emblemPx = [int][Math]::Round(360.0 * $s)
    $emblem = Render-FileDOIcon $emblemPx
    $gState = $g.Save()
    $g.ResetTransform()
    $g.DrawImage($emblem, [int][Math]::Round($ox + 100.0 * $s), [int][Math]::Round($oy + 140.0 * $s), $emblemPx, $emblemPx)
    $g.Restore($gState)
    $emblem.Dispose()

    $font = [System.Drawing.Font]::new("Segoe UI", 72.0, [System.Drawing.FontStyle]::Bold)
    $brush = [System.Drawing.SolidBrush]::new($cWhite)
    $g.DrawString("FileDO", $font, $brush, 520.0, 250.0)
    $brush.Dispose()
    $font.Dispose()
    $g.Dispose()

    New-ParentFolder $outPath
    $bmp.Save($outPath, [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()
}

# One MSIX logo: the 256 px PNG scaled to the logo's size, exactly as the package build used to cut it.
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

# The installer wizard's two bitmaps, composed from the 256 px PNG. The layout is dictated by WixUI, not
# by taste: MSI draws the page title over the LEFT of the banner and the welcome text over the RIGHT of
# the dialog bitmap, so those areas stay empty. Anything drawn there would end up underneath a sentence.
function Write-InstallerArt([string]$iconPng, [string]$bannerPath, [string]$dialogPath) {
    # The product's own colours: the dark green of the site's default theme.
    $ink    = [System.Drawing.Color]::FromArgb(10, 15, 10)
    $accent = [System.Drawing.Color]::FromArgb(94, 214, 132)
    $paper  = [System.Drawing.Color]::White
    $subtle = [System.Drawing.Color]::FromArgb(150, 173, 156)

    $logo = [System.Drawing.Image]::FromFile($iconPng)
    try {
        # banner.bmp 493x58 - the strip across the top of every wizard page: the icon on the right, a plain
        # light field the black title reads on.
        $b = New-Object System.Drawing.Bitmap 493, 58
        $bg = [System.Drawing.Graphics]::FromImage($b)
        $bg.SmoothingMode     = "AntiAlias"
        $bg.InterpolationMode = "HighQualityBicubic"
        $bg.TextRenderingHint = "ClearTypeGridFit"
        $bg.Clear($paper)
        $bg.FillRectangle((New-Object System.Drawing.SolidBrush $accent), 0, 55, 493, 3)
        $bg.DrawImage($logo, 435, 9, 40, 40)
        $bg.Dispose()
        New-ParentFolder $bannerPath
        $b.Save($bannerPath, [System.Drawing.Imaging.ImageFormat]::Bmp)
        $b.Dispose()

        # dialog.bmp 493x312 - the background of the Welcome and Finish pages: a band down the left with the
        # icon, the name and the one line that says what this program is for.
        $d = New-Object System.Drawing.Bitmap 493, 312
        $dg = [System.Drawing.Graphics]::FromImage($d)
        $dg.SmoothingMode     = "AntiAlias"
        $dg.InterpolationMode = "HighQualityBicubic"
        $dg.TextRenderingHint = "ClearTypeGridFit"
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
        New-ParentFolder $dialogPath
        $d.Save($dialogPath, [System.Drawing.Imaging.ImageFormat]::Bmp)
        $d.Dispose()
    } finally { $logo.Dispose() }
}

# ---- the run -------------------------------------------------------------------------------------

try {
    # 1. The master and its two reduced forms, as SVG.
    Write-TextFile $rel.MasterFull   (Get-MarkSvg 256 'full form, 64 px and up (the master)')
    Write-TextFile $rel.MasterMedium (Get-MarkSvg 48 'medium form, 33 to 63 px')
    Write-TextFile $rel.MasterSmall  (Get-MarkSvg 32 'small form, 32 px and below')

    # 2. The 256 px picture and the multi-size ICO.
    $icon256 = Render-FileDOIcon 256
    Save-Png $icon256 $rel.Png256
    $icon256.Dispose()
    $icoFile = Get-OutPath $rel.Ico
    New-ParentFolder $icoFile
    $bitmaps = @(foreach ($s in 16, 24, 32, 48, 64, 128, 256) { Render-FileDOIcon $s })
    Save-MultiFrameIcon -icoPath $icoFile -bitmaps $bitmaps
    foreach ($b in $bitmaps) { $b.Dispose() }
    Register-Written $rel.Ico

    # 3. The site's and the help pages' copies (rule 36).
    Copy-Rendition $rel.Png256 $rel.SitePng
    Copy-Rendition $rel.SitePng $rel.HelpFavicon
    Copy-Rendition $rel.MasterMedium $rel.SiteSvg
    Copy-Rendition $rel.MasterMedium $rel.HelpSvg

    # 4. The share artwork: one drawing, two sizes, each under 1 MB.
    Render-SocialCard -w 1280 -h 640 -outPath (Get-OutPath $rel.Preview)
    Register-Written $rel.Preview
    Copy-Rendition $rel.Preview $rel.SitePreview
    Render-SocialCard -w 1200 -h 630 -outPath (Get-OutPath $rel.Card)
    Register-Written $rel.Card
    foreach ($r in $rel.Preview, $rel.Card) {
        $len = (Get-Item -LiteralPath (Get-OutPath $r)).Length
        if ($len -ge 1000000) { throw "$r is $len bytes - a social image must stay under 1 MB" }
    }

    # 5. The MSIX logos msix\build-msix.ps1 packs. Windows picks the target-size and unplated forms only when
    #    resources.pri indexes them, so every one is a file of its own.
    $msixLogos = [System.Collections.Generic.List[object]]::new()
    $msixLogos.Add(@{ Leaf = 'Square44x44Logo.png'; Size = 44; Surface = 'msix.square44' })
    foreach ($size in 16, 24, 32, 48, 256) {
        foreach ($form in '', '_altform-unplated', '_altform-lightunplated') {
            $msixLogos.Add(@{ Leaf = "Square44x44Logo.targetsize-$size$form.png"; Size = $size; Surface = 'msix.square44' })
        }
    }
    $msixLogos.Add(@{ Leaf = 'Square71x71Logo.png'; Size = 71; Surface = 'msix.square71' })
    $msixLogos.Add(@{ Leaf = 'Square150x150Logo.png'; Size = 150; Surface = 'msix.square150' })
    $msixLogos.Add(@{ Leaf = 'StoreLogo.png'; Size = 50; Surface = 'msix.storelogo' })
    foreach ($logo in $msixLogos) {
        $r = "$($rel.MsixDir)/$($logo.Leaf)"
        $dst = Get-OutPath $r
        New-ParentFolder $dst
        New-MsixLogo (Get-OutPath $rel.Png256) $dst $logo.Size
        Register-Written $r
    }

    # 6. The installer wizard's art.
    Write-InstallerArt (Get-OutPath $rel.Png256) (Get-OutPath $rel.Banner) (Get-OutPath $rel.Dialog)
    Register-Written $rel.Banner
    Register-Written $rel.Dialog

    # 7. The Store logo set, from the master SVG (a release step: headless Edge).
    if ($StoreLogos) {
        & (Join-Path $root 'msix\make-store-logos.ps1') -Svg (Get-OutPath $rel.MasterFull) -Background ('#{0:X2}{1:X2}{2:X2}' -f $cBgDark.R, $cBgDark.G, $cBgDark.B)
        if (-not $?) { throw 'msix\make-store-logos.ps1 failed' }
    }
} catch {
    if ($scratch) { Remove-Item -LiteralPath $scratch -Recurse -Force -ErrorAction SilentlyContinue }
    Exit-Verdict 2 "COULD NOT VERIFY (a rendition could not be drawn: $($_.Exception.Message))"
}

# ---- the manifest (section 5) ----------------------------------------------------------------------

function Get-Sha([string]$file) { (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant() }

$masterSha = Get-Sha (Get-OutPath $rel.MasterFull)
$rows = [System.Collections.Generic.List[object]]::new()
function Add-Row([string]$surface, [string]$path, [string]$format, [int]$w, [int]$h, [System.Collections.IDictionary]$extra) {
    $file = if ($path.StartsWith("$($rel.StoreLogos)/")) { Get-RepoPath $path } else { Get-OutPath $path }
    $row = [ordered]@{ surface = $surface; path = $path; format = $format }
    if ($w -gt 0) { $row.width = $w; $row.height = $h }
    $row.sha256 = Get-Sha $file
    if ($extra) { foreach ($k in $extra.Keys) { $row[$k] = $extra[$k] } }
    $rows.Add($row)
}

# The installed product (PRODUCT-IDENTITY 0.4 rule 2: app.shell-entry is the Explorer entry / open verb;
# app.about covers the in-window display of the mark on the Disk Manager's main-window button)
foreach ($surface in 'app.exe-ico', 'app.installer-ico', 'app.launcher', 'app.titlebar', 'app.tray', 'app.shell-entry', 'app.about') {
    Add-Row $surface $rel.Ico 'ico' 256 256 $null
}
# The MSIX package
foreach ($logo in $msixLogos) { Add-Row $logo.Surface "$($rel.MsixDir)/$($logo.Leaf)" 'png' $logo.Size $logo.Size $null }
# The Store listing: rendered from the master SVG by msix\make-store-logos.ps1, which records the hash of
# the drawing it read in master-sha256.txt. Until the release render runs, the 21 files in the repository
# were rendered from the retired hand-made SVG (SHA-256 bd8b9889ac21..).
$storeStamp = Join-Path (Get-RepoPath $rel.StoreLogos) 'master-sha256.txt'
$renderedFrom = 'bd8b9889ac213901289c5fe00b9d5999fe4387457cc6b0294653455313326711'
if (Test-Path -LiteralPath $storeStamp) {
    $first = (Get-Content -LiteralPath $storeStamp -TotalCount 1)
    if ($first) { $renderedFrom = ($first -split '\s+')[0].ToLowerInvariant() }
}
$storeFiles = @()
foreach ($locale in 'en-us', 'ru', 'uk') {
    foreach ($name in 'poster-720x1080', 'poster-1440x2160', 'box-1080x1080', 'box-2160x2160', 'tile-300x300', 'tile-150x150', 'tile-71x71') {
        $r = "$($rel.StoreLogos)/$locale/$name.png"
        if (-not (Test-Path -LiteralPath (Get-RepoPath $r))) { continue }
        $wh = ($name -split '-')[1] -split 'x'
        Add-Row 'store.ms.logo' $r 'png' ([int]$wh[0]) ([int]$wh[1]) ([ordered]@{ renderedFrom = $renderedFrom })
        $storeFiles += $r
    }
}
# The site, the README, the help pages and the family map
Add-Row 'readme.head' $rel.Png256 'png' 256 256 $null
Add-Row 'web.favicon-png' $rel.SitePng 'png' 256 256 $null
Add-Row 'site.identity' $rel.SitePng 'png' 256 256 $null
Add-Row 'site.identity' $rel.SiteSvg 'svg' 0 0 $null
Add-Row 'hub.card' $rel.SitePng 'png' 256 256 $null
Add-Row 'help.head' $rel.HelpSvg 'svg' 0 0 $null
Add-Row 'web.favicon-png' $rel.HelpFavicon 'png' 256 256 $null
Add-Row 'web.og-image' $rel.Card 'png' 1200 630 $null
Add-Row 'github.social-preview' $rel.Preview 'png' 1280 640 $null
Add-Row 'github.social-preview' $rel.SitePreview 'png' 1280 640 $null
# The installer wizard (PRODUCT-IDENTITY 0.4 rule 2 and section 5: bmp width and height read from BITMAPINFOHEADER)
Add-Row 'app.installer-banner' $rel.Banner 'bmp' 493 58 $null
Add-Row 'app.installer-dialog' $rel.Dialog 'bmp' 493 312 $null

$manifest = [ordered]@{
    schemaVersion = 1
    product       = 'FileDO'
    family        = $null
    master        = [ordered]@{
        svg        = $rel.MasterFull
        small      = $rel.MasterSmall
        reduced    = @([ordered]@{ name = 'medium'; path = $rel.MasterMedium; minPx = 33; maxPx = 63 })
        background = '#{0:X2}{1:X2}{2:X2}' -f $cBgDark.R, $cBgDark.G, $cBgDark.B
        sha256     = $masterSha
    }
    generator     = 'pwsh -NoProfile -File assets/make-product-icons.ps1'
    renditions    = $rows
}
Write-TextFile $rel.Manifest (($manifest | ConvertTo-Json -Depth 8) + "`n")

$notes = [System.Collections.Generic.List[string]]::new()
if ($storeFiles.Count -and $renderedFrom -ne $masterSha) {
    $notes.Add("store.ms.logo: $($storeFiles.Count) file(s) in $($rel.StoreLogos) were rendered from another drawing (renderedFrom $renderedFrom) - run this script with -StoreLogos before the next Store submission")
}

# ---- -Check: the scratch run against the repository ------------------------------------------------

if ($Check) {
    $findings = [System.Collections.Generic.List[string]]::new()
    foreach ($r in $written) {
        $mine = Get-OutPath $r
        $theirs = Get-RepoPath $r
        if (-not (Test-Path -LiteralPath $theirs)) { $findings.Add("$r is missing - run assets\make-product-icons.ps1"); continue }
        if ((Get-Sha $mine) -ne (Get-Sha $theirs)) {
            $findings.Add("$r differs from what the drawing gives (edited by hand, or not regenerated) - run assets\make-product-icons.ps1")
        }
    }
    foreach ($r in $retired) {
        if (Test-Path -LiteralPath (Get-RepoPath $r)) { $findings.Add("$r is back: a second drawing or copy of the mark beside the generated ones (rule 3) - delete it") }
    }
    Remove-Item -LiteralPath $scratch -Recurse -Force -ErrorAction SilentlyContinue
    foreach ($n in $notes) { Write-Output "  NOTE $n" }
    foreach ($f in $findings) { Write-Output "  FAIL $f" }
    $summary = "$($written.Count) generated file(s) compared, $($rows.Count) manifest row(s)"
    if ($findings.Count) { Exit-Verdict 1 "FAIL ($($findings.Count) finding(s), $summary)" }
    Exit-Verdict 0 "PASS ($summary)"
}

foreach ($r in $written) { Write-Output "  wrote $r" }
foreach ($n in $notes) { Write-Output "  NOTE $n" }
Exit-Verdict 0 "PASS (written $($written.Count) file(s), $($rows.Count) manifest row(s), master $($masterSha.Substring(0, 12)))"
