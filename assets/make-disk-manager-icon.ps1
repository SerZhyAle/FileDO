# Simple Disk Manager Icon Generator
Add-Type -AssemblyName System.Drawing

$assetsDir = "/P/WINDOWS/FileDo/assets"
$icoPath = "$assetsDir/app.disk-manager.ico"

# Create a simple icon based on the SVG path
$sizes = @(16, 24, 32, 48, 64, 128, 256)
$bitmaps = @()

foreach ($size in $sizes) {
    $bmp = New-Object System.Drawing.Bitmap($size, $size)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $g.Clear([System.Drawing.Color]::Transparent)
    
    # Draw a simple disk icon
    $center = $size / 2
    $radius = $size * 0.4
    
    # Draw disk platter
    $brush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(255, 94, 214, 132))
    $g.FillEllipse($brush, $center - $radius, $center - $radius, $radius * 2, $radius * 2)
    $brush.Dispose()
    
    # Draw disk hole
    $holeRadius = $size * 0.2
    $brush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(255, 15, 26, 18))
    $g.FillEllipse($brush, $center - $holeRadius, $center - $holeRadius, $holeRadius * 2, $holeRadius * 2)
    $brush.Dispose()
    
    # Draw center indicator
    $centerRadius = $size * 0.1
    $brush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(255, 56, 189, 248))
    $g.FillEllipse($brush, $center - $centerRadius, $center - $centerRadius, $centerRadius * 2, $centerRadius * 2)
    $brush.Dispose()
    
    $bitmaps += $bmp
}

# Save as ICO (simple approach - save as PNG for now)
$bitmaps[0].Save("$assetsDir/app.disk-manager.png", [System.Drawing.Imaging.ImageFormat]::Png)
Write-Host "Created simple Disk Manager icon at $assetsDir/app.disk-manager.png"

# Clean up
foreach ($bmp in $bitmaps) { $bmp.Dispose() }
