param()
$ErrorActionPreference = 'Stop'
$root = 'P:\WINDOWS\FileDo'
$evidence = Join-Path $root 'tests/evidence/sp0063-20261001'
try {
    & "$root/tests/set-display-scale.ps1" -Display '\\.\DISPLAY5' -Percent 150
    foreach ($lang in 'en','ru','de') {
        foreach ($themeName in 'light','dark') {
            & "$root/tests/capture-dpi.ps1" -Exe "$root/exe_to_download/filedo_win.exe" -Monitor '\\.\DISPLAY5' -Window disks -Language $lang -Theme $themeName -ClientWidth 1100 -ClientHeight 640 -OutDir "$evidence/$lang"
        }
    }
    foreach ($themeName in 'light','dark') {
        & "$root/tests/capture-dpi.ps1" -Exe "$root/exe_to_download/filedo_win.exe" -Monitor '\\.\DISPLAY5' -Window disks -Theme $themeName -ClientWidth 1100 -ClientHeight 640 -Key F1 -OutDir "$evidence/help"
        & "$root/tests/capture-dpi.ps1" -Exe "$root/exe_to_download/filedo_win.exe" -Monitor '\\.\DISPLAY5' -Window disks -Theme $themeName -ClientWidth 1100 -ClientHeight 640 -Path drag -OutDir "$evidence/mixed"
    }
} finally {
    & "$root/tests/set-display-scale.ps1" -Display '\\.\DISPLAY5' -Percent 100
}
