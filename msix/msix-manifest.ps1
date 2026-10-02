<#
.SYNOPSIS
  Manifest helpers shared by build-msix.ps1 and test-store-tools.ps1. Dot-source it; it runs nothing.

.DESCRIPTION
  New-FileDOManifest     fills the AppxManifest.xml template and shapes it for one build: the CLI
                         application (-Cli) and the packaged Explorer command (-ExplorerCommand).
  Test-ExplorerCommand   asserts a packed (or generated) manifest and the package's entry list
                         against the Explorer-command choice the build was made with.
  Test-DiskContainerAssociation  asserts the .fdd declaration (SP-0004 T6.27): once, for the GUI
                         app, with no verb - the Store build cannot mount.

  The packaged Explorer command (SP-0020: the desktop4:FileExplorerContextMenus verb, the
  com:SurrogateServer class, and FileDOShell.dll) is OPT-IN at build time (AUD-17-F1, owner decision
  (a), 2026-09-26). The template keeps both extensions so the CLSID stays in one reviewed place
  (TestShellExtClsidAgrees reads it); a default build removes them, and the package check then
  refuses a package that still carries either extension or the DLL. The listing and the README say
  the Store edition adds no Explorer entries - the default build is what keeps that true.

  Both functions are pure XML work: no SDK, no build, no file written, so test-store-tools.ps1 can
  prove them against the template without packing anything.
#>

$script:FileDOManifestNs = [ordered]@{
    uap5     = 'http://schemas.microsoft.com/appx/manifest/uap/windows10/5'
    m        = 'http://schemas.microsoft.com/appx/manifest/foundation/windows10'
    uap      = 'http://schemas.microsoft.com/appx/manifest/uap/windows10'
    rescap   = 'http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities'
    desktop4 = 'http://schemas.microsoft.com/appx/manifest/desktop/windows10/4'
    desktop5 = 'http://schemas.microsoft.com/appx/manifest/desktop/windows10/5'
    com      = 'http://schemas.microsoft.com/appx/manifest/com/windows10'
}

# SP-0081: the OS owns this per-user switch; no packaged Run-key writer exists.
function Test-ApplicationStartup {
    param([System.Xml.XmlDocument]$Manifest)
    $ns = New-FileDOManifestNs $Manifest
    $problems = [System.Collections.Generic.List[string]]::new()
    $extensions = @($Manifest.SelectNodes('//*[@Category="windows.startupTask"]'))
    $startup = @($Manifest.SelectNodes('/m:Package/m:Applications/m:Application[@Id="FileDOGui"]/m:Extensions/uap5:Extension[@Category="windows.startupTask"]', $ns))
    if ($startup.Count -ne 1 -or $extensions.Count -ne 1) {
        $problems.Add('FileDOGui must declare exactly one startupTask, with none on the CLI')
    } else {
        $task = $startup[0].SelectSingleNode('uap5:StartupTask', $ns)
        if (-not $task -or $task.GetAttribute('TaskId') -cne 'FileDOShellStartup' -or $task.GetAttribute('Enabled') -cne 'false' -or
            $startup[0].GetAttribute('Executable') -cne 'filedo_win.exe' -or $startup[0].GetAttribute('EntryPoint') -cne 'Windows.FullTrustApplication') {
            $problems.Add('startupTask must start filedo_win.exe, TaskId FileDOShellStartup, initially disabled')
        }
    }
    $gui = $Manifest.SelectSingleNode('/m:Package/m:Applications/m:Application[@Id="FileDOGui"]', $ns)
    if (-not $gui -or $gui.GetAttribute('SupportsMultipleInstances', 'http://schemas.microsoft.com/appx/manifest/desktop/windows10/4') -cne 'false') {
        $problems.Add('FileDOGui must not support multiple startup instances')
    }
    return $problems.ToArray()
}

# The two extension categories that make up the packaged Explorer command, and the DLL they load.
$script:ExplorerCommandCategories = @('windows.fileExplorerContextMenus', 'windows.comServer')
$script:ExplorerCommandDll        = 'FileDOShell.dll'

function New-FileDOManifestNs([System.Xml.XmlDocument]$doc) {
    $ns = New-Object System.Xml.XmlNamespaceManager($doc.NameTable)
    foreach ($k in $script:FileDOManifestNs.Keys) { $ns.AddNamespace($k, $script:FileDOManifestNs[$k]) }
    return , $ns
}

function New-FileDOManifest {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$TemplatePath,
        [Parameter(Mandatory)][string]$IdentityName,
        [Parameter(Mandatory)][string]$Publisher,
        [Parameter(Mandatory)][string]$PublisherDisplayName,
        [Parameter(Mandatory)][string]$Version,
        [ValidateSet('visible', 'hidden', 'none')][string]$Cli = 'visible',
        [switch]$ExplorerCommand
    )
    $text = Get-Content $TemplatePath -Raw
    $text = $text.Replace("{{IDENTITY_NAME}}", $IdentityName).
                  Replace("{{PUBLISHER}}", $Publisher).
                  Replace("{{PUBLISHER_DISPLAY_NAME}}", $PublisherDisplayName).
                  Replace("{{VERSION}}", $Version)
    if ($text -match '\{\{[A-Z_]+\}\}') { throw "an unfilled placeholder is left in the manifest: $($Matches[0])" }

    $xml = New-Object System.Xml.XmlDocument
    $xml.PreserveWhitespace = $true
    $xml.LoadXml($text)
    $ns = New-FileDOManifestNs $xml

    $cliApp = $xml.SelectSingleNode("//m:Application[@Id='FileDO']", $ns)
    if (-not $cliApp) { throw "the manifest template has no Application Id='FileDO' (the CLI application)." }
    switch ($Cli) {
        'none'   { [void]$cliApp.ParentNode.RemoveChild($cliApp) }
        'hidden' { $cliApp.SelectSingleNode('uap:VisualElements', $ns).SetAttribute('AppListEntry', 'none') }
    }

    $guiExt = $xml.SelectSingleNode("/m:Package/m:Applications/m:Application[@Id='FileDOGui']/m:Extensions", $ns)
    if (-not $guiExt) { throw "the manifest template has no Extensions on Application Id='FileDOGui'." }
    $parts = @($guiExt.ChildNodes | Where-Object {
        $_.NodeType -eq [System.Xml.XmlNodeType]::Element -and $script:ExplorerCommandCategories -contains $_.GetAttribute('Category') })
    if ($ExplorerCommand) {
        foreach ($cat in $script:ExplorerCommandCategories) {
            if (-not ($parts | Where-Object { $_.GetAttribute('Category') -eq $cat })) {
                throw "-ExplorerCommand: the manifest template has no '$cat' extension on FileDOGui to keep."
            }
        }
    } else {
        foreach ($p in $parts) {
            # Drop the extension, the comment that introduces it, and the whitespace between them,
            # so the staged manifest reads as if the command had never been declared.
            $prev = $p.PreviousSibling
            while ($prev -and $prev.NodeType -in [System.Xml.XmlNodeType]::Whitespace, [System.Xml.XmlNodeType]::SignificantWhitespace) {
                $before = $prev.PreviousSibling
                [void]$guiExt.RemoveChild($prev)
                if ($before -and $before.NodeType -eq [System.Xml.XmlNodeType]::Comment -and $before.Value -match 'SP-0020') {
                    $prev = $before.PreviousSibling
                    [void]$guiExt.RemoveChild($before)
                } else { break }
            }
            [void]$guiExt.RemoveChild($p)
        }
    }
    return , $xml
}

function Test-ExplorerCommand {
    <# Returns the problems (strings) of a manifest + entry list against the Explorer-command choice.
       -SourceClsid is the CLSID from shellext\FileDOShell.cpp (checked only when the command is on). #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][System.Xml.XmlDocument]$Manifest,
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$Entries,
        [Parameter(Mandatory)][bool]$ExplorerCommand,
        [string]$SourceClsid
    )
    $problems = New-Object System.Collections.Generic.List[string]
    $ns = New-FileDOManifestNs $Manifest
    # Anywhere in the package, not only on the GUI app: a default build must carry neither.
    $anyMenus = @($Manifest.SelectNodes("//*[@Category='windows.fileExplorerContextMenus']"))
    $anyCom   = @($Manifest.SelectNodes("//*[@Category='windows.comServer']"))
    $hasDll   = $Entries -contains $script:ExplorerCommandDll

    if (-not $ExplorerCommand) {
        if ($anyMenus.Count) { $problems.Add("a default build declares $($anyMenus.Count) windows.fileExplorerContextMenus extension(s); the Explorer command is opt-in (-ExplorerCommand, AUD-17-F1)") }
        if ($anyCom.Count)   { $problems.Add("a default build declares $($anyCom.Count) windows.comServer extension(s); the surrogate class ships only with -ExplorerCommand") }
        if ($hasDll)         { $problems.Add("a default build packs $($script:ExplorerCommandDll); it ships only with -ExplorerCommand") }
        return , $problems
    }

    # -ExplorerCommand: one CLSID in three places - the verb, the COM class, and the DLL's source -
    # and the DLL the class names must be in the package; any disagreement is a package that
    # installs and then shows no menu.
    $guiExt  = '/m:Package/m:Applications/m:Application[@Id="FileDOGui"]/m:Extensions'
    $verbs   = @($Manifest.SelectNodes("$guiExt/desktop4:Extension[@Category='windows.fileExplorerContextMenus']/desktop4:FileExplorerContextMenus/desktop5:ItemType[@Type='*']/desktop5:Verb", $ns))
    $classes = @($Manifest.SelectNodes("$guiExt/com:Extension[@Category='windows.comServer']/com:ComServer/com:SurrogateServer/com:Class", $ns))
    if ($verbs.Count -ne 1)   { $problems.Add("$($verbs.Count) Explorer command verbs on '*', expected 1 (-ExplorerCommand)") }
    if ($classes.Count -ne 1) { $problems.Add("$($classes.Count) surrogate COM classes, expected 1 (-ExplorerCommand)") }
    if (-not $hasDll)         { $problems.Add("-ExplorerCommand build does not pack $($script:ExplorerCommandDll)") }
    if ($verbs.Count -eq 1 -and $classes.Count -eq 1) {
        $vc = $verbs[0].GetAttribute('Clsid').ToUpperInvariant(); $cc = $classes[0].GetAttribute('Id').ToUpperInvariant()
        if ($vc -ne $cc) { $problems.Add("the Explorer verb names CLSID $vc but the COM class is $cc") }
        if (-not $SourceClsid) { $problems.Add("shellext\FileDOShell.cpp carries no CLSID_FileDOCommand comment to compare with") }
        elseif ($vc -ne $SourceClsid.ToUpperInvariant()) { $problems.Add("the manifest CLSID $vc differs from FileDOShell.cpp's $SourceClsid") }
        $dllPath = $classes[0].GetAttribute('Path')
        if ($Entries -notcontains $dllPath) { $problems.Add("the COM class names $dllPath, which is not in the package") }
    }
    return , $problems
}

function Get-FileDOShellClsid([string]$CppPath) {
    $cppText = Get-Content $CppPath -Raw
    if ($cppText -match '//\s*\{([0-9A-Fa-f-]{36})\}\s*\r?\n\s*const CLSID CLSID_FileDOCommand') { return $Matches[1].ToUpperInvariant() }
    return $null
}

function Test-DiskContainerAssociation {
    <# Returns the problems (strings) of a manifest's .fdd declaration (SP-0004 T6.27, spec 5.8 item 2):
       .fdd is declared exactly once, for the GUI app (filedo.diskcontainer on FileDOGui, which opens
       the container's Mount page and says this build cannot mount), with no verb of any kind - this build cannot mount, so
       the mount verbs exist only in the classic registration - and no Explorer command on .fdd.
       With -Entries, the association's logo must be in the package too. #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][System.Xml.XmlDocument]$Manifest,
        [AllowEmptyCollection()][string[]]$Entries
    )
    $problems = New-Object System.Collections.Generic.List[string]
    $ns = New-FileDOManifestNs $Manifest
    $gui = '/m:Package/m:Applications/m:Application[@Id="FileDOGui"]/m:Extensions/uap:Extension[@Category="windows.fileTypeAssociation"]/uap:FileTypeAssociation[@Name="filedo.diskcontainer"]'
    $assoc = @($Manifest.SelectNodes($gui, $ns))
    if ($assoc.Count -ne 1) {
        $problems.Add("$($assoc.Count) filedo.diskcontainer associations on FileDOGui, expected 1 (.fdd opens the GUI's info and export page)")
        return , $problems
    }
    $types = @($assoc[0].SelectNodes('uap:SupportedFileTypes/uap:FileType', $ns) | ForEach-Object { $_.InnerText })
    if (($types -join ',') -cne '.fdd') { $problems.Add("the filedo.diskcontainer association declares '$($types -join ',')', expected exactly .fdd") }
    # Anywhere in the package: .fdd belongs to that one association, on that one app.
    $allFdd = @($Manifest.SelectNodes("//*[local-name()='FileType']") | Where-Object { $_.InnerText.Trim() -ieq '.fdd' })
    if ($allFdd.Count -ne 1) { $problems.Add("the manifest declares .fdd $($allFdd.Count) times, expected once (filedo.diskcontainer on FileDOGui)") }
    # No verb: not on the association (uap:SupportedVerbs, any schema version), and no Explorer
    # command registered for the type.
    $verbs = @($assoc[0].SelectNodes(".//*[local-name()='SupportedVerbs' or local-name()='Verb']"))
    if ($verbs.Count) { $problems.Add("the .fdd association declares $($verbs.Count) verb element(s); the Store build carries no mount verb (SP-0004 spec 6.3, Q12)") }
    $menus = @($Manifest.SelectNodes("//*[local-name()='ItemType']") | Where-Object { $_.GetAttribute('Type') -ieq '.fdd' })
    if ($menus.Count) { $problems.Add("an Explorer command is declared on .fdd; the Store build carries no mount verb") }
    $logo = $assoc[0].SelectSingleNode('uap:Logo', $ns)
    if (-not $logo -or -not $logo.InnerText) { $problems.Add("the .fdd association has no logo") }
    elseif ($PSBoundParameters.ContainsKey('Entries') -and $Entries -notcontains $logo.InnerText) { $problems.Add("the .fdd association's logo $($logo.InnerText) is not in the package") }
    return , $problems
}
