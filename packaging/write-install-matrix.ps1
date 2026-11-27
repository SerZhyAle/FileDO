#Requires -Version 7.0
<#
.SYNOPSIS
  Writes the component and channel table of the install guide from the packaging files, never by hand
  (DOCUMENTATION_CONCEPT section 4, the component/profile/channel matrix; SP-0166 F18).

.DESCRIPTION
  The table under "Choose how to install" in docs/guides/install-and-explorer.html (ru, en and ua spans) and in
  its de and fr pages: one row per thing a visitor may get, one column per way of installing. Every cell is read
  from the file that decides it:

    Setup EXE, MSI   packaging/wix/FileDO.wxs. A row names the component (group) that carries it; the feature
                     holding that reference decides the cell: AllowAbsent="no" is "Yes", an optional feature at
                     Level 1 is "Yes, can be unticked", at a higher level "Optional, unticked", Level 0 or no
                     feature at all is "No". The setup EXE is the bundle of packaging/wix/FileDO-bundle.wxs: one
                     MsiPackage shown through WixInternalUIBootstrapperApplication, so it offers the MSI's own
                     feature tree and its column equals the MSI's; any other bundle shape stops the run.
    winget, ZIP      winget/SerZhyAle.FileDO.installer.yaml: the release zip installed portable. The files it holds
                     and, for winget, the command aliases it puts on the PATH. A portable install registers
                     nothing, so the Explorer entries and the file types come from a command
                     (filedo fdsec register, filedo vd register).
    Microsoft Store  msix/AppxManifest.xml as the default Store build stages it: New-FileDOManifest of
                     msix/msix-manifest.ps1 without -ExplorerCommand. Its applications and command alias, its
                     file type associations and their verbs, the context-menu extension, the capabilities.

  Mounting a virtual disk, and sharing one, needs administrator consent for the iSCSI initiator step: the
  unpackaged program asks for it, a package that declares no allowElevation capability cannot.

  The rows' and the cells' text is written here in five languages; the pages carry the table between the
  markers <!-- install-matrix:begin and <!-- install-matrix:end -->, and a locale page mirrors the English page's
  span groups one for one. Run it after a change of any of the three inputs; packaging/check-internal-docs.ps1
  runs -Check.

.PARAMETER Check
  Render and compare; write nothing.

  Exit codes (CHECK-VERDICT): 0 written or matching, 1 a page differs (-Check) or lacks its markers,
  2 an input is missing or cannot be read, or the bundle is not the shape this script reads.
#>
[CmdletBinding(PositionalBinding = $false)]
param([switch]$Check)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent

function Stop-Unverified([string]$why) {
    Write-Host "install-matrix: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

function Read-Input([string]$rel) {
    $full = Join-Path $root ($rel -replace '/', '\')
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { Stop-Unverified "$rel is missing" }
    return [IO.File]::ReadAllText($full)
}

# ---- Inputs --------------------------------------------------------------------------------------
try { [xml]$wxs = Read-Input 'packaging/wix/FileDO.wxs' } catch { Stop-Unverified "packaging/wix/FileDO.wxs does not parse: $($_.Exception.Message)" }
try { [xml]$bundle = Read-Input 'packaging/wix/FileDO-bundle.wxs' } catch { Stop-Unverified "packaging/wix/FileDO-bundle.wxs does not parse: $($_.Exception.Message)" }
$winget = Read-Input 'winget/SerZhyAle.FileDO.installer.yaml'
$helpers = Join-Path $root 'msix\msix-manifest.ps1'
if (-not (Test-Path -LiteralPath $helpers -PathType Leaf)) { Stop-Unverified 'msix/msix-manifest.ps1 is missing' }
. $helpers
try {
    $store = New-FileDOManifest -TemplatePath (Join-Path $root 'msix\AppxManifest.xml') -IdentityName 'Matrix.Read' `
        -Publisher 'CN=matrix' -PublisherDisplayName 'matrix' -Version '1.0.0.0'
} catch { Stop-Unverified "the default Store manifest cannot be staged: $($_.Exception.Message)" }

# The setup EXE offers the MSI's own feature tree, or its column cannot be read from FileDO.wxs.
$packages = @($bundle.SelectNodes("//*[local-name()='MsiPackage']"))
$internalUi = @($bundle.SelectNodes("//*[local-name()='WixInternalUIBootstrapperApplication']"))
if ($packages.Count -ne 1 -or $internalUi.Count -ne 1) {
    Stop-Unverified "packaging/wix/FileDO-bundle.wxs carries $($packages.Count) MsiPackage and $($internalUi.Count) WixInternalUIBootstrapperApplication; the setup EXE column is read as the MSI's only for one of each"
}

# ---- Facts ---------------------------------------------------------------------------------------
function Get-FeatureState([System.Xml.XmlElement]$feature) {
    $level = $feature.GetAttribute('Level'); if (-not $level) { $level = '1' }
    $absent = $feature.GetAttribute('AllowAbsent'); if (-not $absent) { $absent = 'yes' }
    if ($level -eq '0') { return 'no' }
    $parent = $feature.ParentNode
    if ($parent -is [System.Xml.XmlElement] -and $parent.LocalName -eq 'Feature') {
        $up = Get-FeatureState $parent
        if ($up -eq 'no') { return 'no' }
        if ($absent -eq 'no') { return $up }
    } elseif ($absent -eq 'no') { return 'yes' }
    if ([int]$level -le 1) { return 'optional-on' }
    return 'optional-off'
}
function Get-RefState([string]$refId) {
    $refs = @($wxs.SelectNodes("//*[local-name()='Feature']/*[(local-name()='ComponentRef' or local-name()='ComponentGroupRef') and @Id='$refId']"))
    if ($refs.Count -gt 1) { Stop-Unverified "packaging/wix/FileDO.wxs references '$refId' from $($refs.Count) features" }
    if (-not $refs.Count) { return 'no' }
    return Get-FeatureState $refs[0].ParentNode
}

$zipFiles = @([regex]::Matches($winget, '(?m)^\s*-\s*RelativeFilePath:\s*(\S+)\s*$') | ForEach-Object { $_.Groups[1].Value })
$aliases = @([regex]::Matches($winget, '(?m)^\s*PortableCommandAlias:\s*(\S+)\s*$') | ForEach-Object { $_.Groups[1].Value })
if ($winget -notmatch '(?m)^InstallerType:\s*zip\s*$' -or $winget -notmatch '(?m)^NestedInstallerType:\s*portable\s*$') {
    Stop-Unverified 'winget/SerZhyAle.FileDO.installer.yaml is not a portable zip install; its columns are read only for that shape'
}
$hasProgram = ($zipFiles -contains 'filedo.exe') -and ($zipFiles -contains 'filedo_win.exe')

$ns = New-FileDOManifestNs $store
$apps = @($store.SelectNodes('/m:Package/m:Applications/m:Application', $ns))
$storeExe = @($apps | ForEach-Object { $_.GetAttribute('Executable') })
$storeAlias = @($store.SelectNodes("//*[local-name()='ExecutionAlias']") | ForEach-Object { $_.GetAttribute('Alias') })
$storeMenus = @($store.SelectNodes("//*[@Category='windows.fileExplorerContextMenus']")).Count -gt 0
function Get-StoreType([string]$ext) {
    $assoc = @($store.SelectNodes("//uap:FileTypeAssociation[uap:SupportedFileTypes/uap:FileType='$ext']", $ns))
    if (-not $assoc.Count) { return 'no' }
    $verbs = @($assoc | ForEach-Object { $_.SelectNodes(".//*[local-name()='Verb' or local-name()='SupportedVerbs']") })
    if ($ext -eq '.fdd' -and -not $verbs.Count) { return 'yes-no-mount' }
    return 'yes'
}
$storeElevates = @($store.SelectNodes("//rescap:Capability[@Name='allowElevation']", $ns)).Count -gt 0
$storeDesktop = @($store.SelectNodes("//*[local-name()='Shortcut']")).Count -gt 0

$program = Get-RefState 'FileDOFiles'
$cells = [ordered]@{
    app     = @{ setup = $program; msi = $program; winget = $(if ($hasProgram) { 'yes' } else { 'no' }); zip = $(if ($hasProgram) { 'yes' } else { 'no' })
                 store = $(if (($storeExe -contains 'filedo_win.exe') -and ($storeExe -contains 'filedo.exe')) { 'yes' } else { 'no' }) }
    path    = @{ setup = (Get-RefState 'EnvPath'); msi = (Get-RefState 'EnvPath'); winget = $(if ($aliases -contains 'filedo') { 'yes' } else { 'no' }); zip = 'from-folder'
                 store = $(if ($storeAlias -contains 'filedo.exe') { 'yes' } else { 'no' }) }
    menu    = @{ setup = (Get-RefState 'ShellIntegration'); msi = (Get-RefState 'ShellIntegration'); winget = 'cmd-fdsec'; zip = 'cmd-fdsec'
                 store = $(if ($storeMenus) { 'yes' } else { 'no' }) }
    fdsec   = @{ setup = (Get-RefState 'ShellIntegration'); msi = (Get-RefState 'ShellIntegration'); winget = 'cmd-fdsec'; zip = 'cmd-fdsec'; store = (Get-StoreType '.fd-sec') }
    fdd     = @{ setup = (Get-RefState 'DiskContainerShell'); msi = (Get-RefState 'DiskContainerShell'); winget = 'cmd-vd'; zip = 'cmd-vd'; store = (Get-StoreType '.fdd') }
    desktop = @{ setup = (Get-RefState 'DesktopShortcut'); msi = (Get-RefState 'DesktopShortcut'); winget = 'no'; zip = 'no'; store = $(if ($storeDesktop) { 'yes' } else { 'no' }) }
    mount   = @{ setup = $program; msi = $program; winget = $(if ($hasProgram) { 'yes' } else { 'no' }); zip = $(if ($hasProgram) { 'yes' } else { 'no' }); store = $(if ($storeElevates) { 'yes' } else { 'no' }) }
    share   = @{ setup = $program; msi = $program; winget = $(if ($hasProgram) { 'yes' } else { 'no' }); zip = $(if ($hasProgram) { 'yes' } else { 'no' }); store = $(if ($storeElevates) { 'yes' } else { 'no' }) }
}

# ---- Words ---------------------------------------------------------------------------------------
$langs = @('en', 'ru', 'ua', 'de', 'fr')
$words = @{
    intro = @{ en = 'What each way of installing gives you:'; ru = 'Что даёт каждый способ установки:'; ua = 'Що дає кожен спосіб установлення:'
               de = 'Was jede Installationsart mitbringt:'; fr = "Ce qu$([char]0x2019)apporte chaque mode d$([char]0x2019)installation$([char]0xA0):" }
    head  = @{ en = 'Component'; ru = 'Компонент'; ua = 'Компонент'; de = 'Komponente'; fr = 'Composant' }
}
$rowWords = [ordered]@{
    app     = @{ en = 'Window and command line'; ru = 'Окно и командная строка'; ua = 'Вікно і командний рядок'; de = 'Fenster und Befehlszeile'; fr = 'Fenêtre et ligne de commande' }
    path    = @{ en = '<code>filedo</code> in any terminal'; ru = '<code>filedo</code> в любом терминале'; ua = '<code>filedo</code> у будь-якому терміналі'; de = '<code>filedo</code> in jedem Terminal'; fr = "<code>filedo</code> dans n$([char]0x2019)importe quel terminal" }
    menu    = @{ en = 'Explorer right-click menu'; ru = 'Контекстное меню Проводника'; ua = 'Контекстне меню Провідника'; de = 'Explorer-Kontextmenü'; fr = "Menu contextuel de l$([char]0x2019)Explorateur" }
    fdsec   = @{ en = '<code>.fd-sec</code> file type'; ru = 'Тип файлов <code>.fd-sec</code>'; ua = 'Тип файлів <code>.fd-sec</code>'; de = 'Dateityp <code>.fd-sec</code>'; fr = 'Type de fichier <code>.fd-sec</code>' }
    fdd     = @{ en = '<code>.fdd</code> file type'; ru = 'Тип файлов <code>.fdd</code>'; ua = 'Тип файлів <code>.fdd</code>'; de = 'Dateityp <code>.fdd</code>'; fr = 'Type de fichier <code>.fdd</code>' }
    desktop = @{ en = 'Desktop icon'; ru = 'Значок на рабочем столе'; ua = 'Піктограма на робочому столі'; de = 'Desktopsymbol'; fr = 'Icône sur le bureau' }
    mount   = @{ en = 'Mounting virtual disks'; ru = 'Подключение виртуальных дисков'; ua = 'Підключення віртуальних дисків'; de = 'Virtuelle Datenträger einbinden'; fr = 'Montage des disques virtuels' }
    share   = @{ en = 'Disk sharing through Fast Media Sorter &amp; Sharing'; ru = 'Общий доступ к дискам через Fast Media Sorter &amp; Sharing'; ua = 'Спільний доступ до дисків через Fast Media Sorter &amp; Sharing'; de = 'Datenträger-Freigabe über Fast Media Sorter &amp; Sharing'; fr = 'Partage des disques via Fast Media Sorter &amp; Sharing' }
}
$cellWords = @{
    'yes'          = @{ en = 'Yes'; ru = 'Да'; ua = 'Так'; de = 'Ja'; fr = 'Oui' }
    'no'           = @{ en = 'No'; ru = 'Нет'; ua = 'Ні'; de = 'Nein'; fr = 'Non' }
    'optional-on'  = @{ en = 'Yes, can be unticked'; ru = 'Да, можно снять'; ua = 'Так, можна зняти'; de = 'Ja, abwählbar'; fr = 'Oui, décochable' }
    'optional-off' = @{ en = 'Optional, unticked'; ru = 'По выбору, не отмечено'; ua = 'За вибором, не позначено'; de = 'Optional, nicht ausgewählt'; fr = 'En option, non cochée' }
    'from-folder'  = @{ en = 'From the unpacked folder'; ru = 'Из распакованной папки'; ua = 'З розпакованої теки'; de = 'Aus dem entpackten Ordner'; fr = 'Depuis le dossier décompressé' }
    'cmd-fdsec'    = @{ en = 'With <code>filedo fdsec register</code>'; ru = 'Командой <code>filedo fdsec register</code>'; ua = 'Командою <code>filedo fdsec register</code>'; de = 'Mit <code>filedo fdsec register</code>'; fr = 'Avec <code>filedo fdsec register</code>' }
    'cmd-vd'       = @{ en = 'With <code>filedo vd register</code>'; ru = 'Командой <code>filedo vd register</code>'; ua = 'Командою <code>filedo vd register</code>'; de = 'Mit <code>filedo vd register</code>'; fr = 'Avec <code>filedo vd register</code>' }
    'yes-no-mount' = @{ en = 'Yes, without mounting'; ru = 'Да, без подключения'; ua = 'Так, без підключення'; de = 'Ja, ohne Einbinden'; fr = 'Oui, sans montage' }
}
$channels = [ordered]@{ setup = 'Setup EXE'; msi = 'MSI'; winget = 'winget'; zip = 'ZIP'; store = 'Microsoft Store' }

# ---- Render --------------------------------------------------------------------------------------
# The English page is a runtime page: each group carries en, ru and ua, the en span first. A locale page carries
# its own language only (the uk key of the spans is ua on the runtime page, as everywhere on the site).
function Format-Group([hashtable]$w, [string]$page, [string]$indent) {
    if ($page -eq 'en') {
        return "<span data-l=`"en`">$($w.en)</span`n$indent><span data-l=`"ru`">$($w.ru)</span`n$indent><span data-l=`"ua`">$($w.ua)</span>"
    }
    return "<span data-l=`"$page`">$($w[$page])</span>"
}
function Format-Table([string]$page) {
    $i8 = ' ' * 8; $i10 = ' ' * 10; $i12 = ' ' * 12; $i14 = ' ' * 14; $i16 = ' ' * 16; $i18 = ' ' * 18
    $sb = [System.Text.StringBuilder]::new()
    [void]$sb.Append("<!-- install-matrix:begin - written by packaging/write-install-matrix.ps1 from packaging/wix/FileDO.wxs, msix/AppxManifest.xml and the winget installer manifest; never edit by hand -->`n")
    [void]$sb.Append("$i8<p class=`"intro matrix-intro`">`n$i10$(Format-Group $words.intro $page $i10)`n$i8</p>`n")
    [void]$sb.Append("$i8<div class=`"tablewrap`">`n$i10<table class=`"keytable`">`n$i12<thead>`n$i14<tr>`n")
    [void]$sb.Append("$i16<th>$(Format-Group $words.head $page $i16)</th>`n")
    foreach ($c in $channels.Keys) { [void]$sb.Append("$i16<th>$($channels[$c])</th>`n") }
    [void]$sb.Append("$i14</tr>`n$i12</thead>`n$i12<tbody>`n")
    foreach ($r in $rowWords.Keys) {
        [void]$sb.Append("$i14<tr>`n$i16<td>$(Format-Group $rowWords[$r] $page $i16)</td>`n")
        foreach ($c in $channels.Keys) {
            $state = $cells[$r][$c]
            if (-not $cellWords.ContainsKey($state)) { throw "row $r, channel ${c}: no words for state '$state'" }
            [void]$sb.Append("$i16<td>$(Format-Group $cellWords[$state] $page $i16)</td>`n")
        }
        [void]$sb.Append("$i14</tr>`n")
    }
    [void]$sb.Append("$i12</tbody>`n$i10</table>`n$i8</div>`n$i8<!-- install-matrix:end -->")
    return $sb.ToString()
}

$pages = [ordered]@{
    'docs/guides/install-and-explorer.html'    = 'en'
    'docs/de/guides/install-and-explorer.html' = 'de'
    'docs/fr/guides/install-and-explorer.html' = 'fr'
}
$problems = [System.Collections.Generic.List[string]]::new()
$written = 0
foreach ($rel in $pages.Keys) {
    $full = Join-Path $root ($rel -replace '/', '\')
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { Stop-Unverified "$rel is missing" }
    $orig = [IO.File]::ReadAllText($full)
    $block = [regex]::Matches($orig, '(?s)<!-- install-matrix:begin.*?<!-- install-matrix:end -->')
    if ($block.Count -ne 1) { $problems.Add("${rel}: expected one install-matrix block between its markers, found $($block.Count)"); continue }
    $table = Format-Table $pages[$rel]
    $next = $orig.Substring(0, $block[0].Index) + $table + $orig.Substring($block[0].Index + $block[0].Length)
    if ($next -cne $orig) {
        if ($Check) { $problems.Add("${rel}: the install matrix differs from what the packaging files give - run packaging/write-install-matrix.ps1") }
        else { [IO.File]::WriteAllText($full, ($next -replace "`r`n", "`n"), [Text.UTF8Encoding]::new($false)); $written++ }
    }
}
if ($problems.Count) {
    foreach ($p in $problems) { Write-Host "  FAIL  $p" -ForegroundColor Red }
    Write-Host "install-matrix: FAIL ($($problems.Count) problem(s))" -ForegroundColor Red
    exit 1
}
$summary = ($rowWords.Keys | ForEach-Object { $r = $_; "$r=" + (($channels.Keys | ForEach-Object { $cells[$r][$_] }) -join '/') }) -join ' '
if ($Check) { Write-Host "install-matrix: PASS ($($pages.Count) pages match; $summary)" -ForegroundColor Green }
else { Write-Host "install-matrix: PASS ($written of $($pages.Count) pages written; $summary)" -ForegroundColor Green }
exit 0
