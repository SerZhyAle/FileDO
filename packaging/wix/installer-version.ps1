# ============================================================================
#  Installer version read-back - shared by build.ps1 and release.ps1.
#
#  Functions only: dot-sourcing this file defines functions and nothing else - no variable, no file, no
#  command runs at load, which is why two scripts that do real work can share it.
#
#  PACKAGE-VERSIONING P5 / W4: every artifact carries the release stamp, or its mechanical remap, in a
#  field that is READ BACK from the produced file - not taken from the argument that was meant to put it
#  there. The MSI and the setup EXE carry yy.M.<minute of the month> (RELEASE.md "Version scheme"):
#
#    MSI        the ProductVersion row of its Property table (Windows Installer reads three fields)
#    setup EXE  the Burn bundle's own version resource, FileVersion and ProductVersion (WiX writes both
#               from the bundle's Version; Burn compares four fields and reads a missing fourth as 0)
#
#  A version is compared as four fields with a missing one read as 0, so "26.10.14356" (the MSI row and
#  the remap) equals 26.10.14356.0 (the bundle's raw version).
# ============================================================================

# The ProductVersion a built or published MSI really carries, read from its Property table. Read, not
# derived: the arithmetic that maps a stamp to a version is exactly what a read-back must not trust.
function Get-MsiProductVersion([string]$msiPath) {
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $db = $installer.OpenDatabase($msiPath, 0)   # 0 = read-only
    $view = $db.OpenView("SELECT ``Value`` FROM ``Property`` WHERE ``Property``='ProductVersion'")
    try {
        # [void]: a COM method without a result still writes a $null into the output.
        [void]$view.Execute()
        $record = $view.Fetch()
        if ($null -eq $record) { throw "$msiPath has no ProductVersion" }
        return [version]$record.StringData(1)
    } finally {
        [void]$view.Close()
        foreach ($com in @($view, $db, $installer)) { [void][Runtime.InteropServices.Marshal]::ReleaseComObject($com) }
    }
}

# The FileVersion and ProductVersion of a setup EXE (a Burn bundle), from its version resource. A file with no
# version resource answers $null in both.
function Get-SetupExeVersion([string]$exePath) {
    $info = (Get-Item -LiteralPath $exePath -ErrorAction Stop).VersionInfo
    # FileVersionRaw is 0.0.0.0 for a file with no resource; the string is what tells the two apart.
    [pscustomobject]@{
        File    = if ($info.FileVersion) { $info.FileVersionRaw } else { $null }
        Product = if ($info.ProductVersion) { $info.ProductVersionRaw } else { $null }
    }
}

# A version with a missing field read as 0, so three- and four-field spellings of one version compare equal.
function ConvertTo-FourFieldVersion($version) {
    $v = [version]"$version"
    [version]::new($v.Major, $v.Minor, [Math]::Max(0, $v.Build), [Math]::Max(0, $v.Revision))
}

# Reads the version back from the MSI and the setup EXE and compares both with the one the stamp maps to.
#   Code 0  both files carry $Expected
#   Code 1  a file was read and carries another version, or none (a defect)
#   Code 2  a file could not be read - nothing was inspected there (not a pass, not a defect report)
# A defect outranks "could not verify", as the gate's precedence has it.
function Test-InstallerVersions([string]$Expected, [string]$MsiPath, [string]$SetupPath) {
    $want = ConvertTo-FourFieldVersion $Expected
    $problems = [System.Collections.Generic.List[string]]::new()
    $unverified = [System.Collections.Generic.List[string]]::new()
    $msiName = Split-Path $MsiPath -Leaf
    $setupName = Split-Path $SetupPath -Leaf

    $msiRead = $null
    if (-not (Test-Path -LiteralPath $MsiPath -PathType Leaf)) { $unverified.Add("$msiName is missing, so its ProductVersion could not be read") }
    else {
        try { $msiRead = ConvertTo-FourFieldVersion (Get-MsiProductVersion $MsiPath) }
        catch { $unverified.Add("$msiName could not be opened as a Windows Installer database, so its ProductVersion could not be read ($($_.Exception.Message))") }
    }
    if ($null -ne $msiRead -and $msiRead -ne $want) {
        $problems.Add("$msiName carries ProductVersion $msiRead, the stamp maps to $want")
    }

    $exeRead = $null
    if (-not (Test-Path -LiteralPath $SetupPath -PathType Leaf)) { $unverified.Add("$setupName is missing, so its version could not be read") }
    else {
        try { $exeRead = Get-SetupExeVersion $SetupPath }
        catch { $unverified.Add("the version of $setupName could not be read ($($_.Exception.Message))") }
    }
    if ($null -ne $exeRead) {
        foreach ($field in 'File', 'Product') {
            $got = $exeRead.$field
            if ($null -eq $got) {
                $problems.Add("$setupName carries no ${field}Version")
            } elseif ((ConvertTo-FourFieldVersion $got) -ne $want) {
                $problems.Add("$setupName carries ${field}Version $got, the stamp maps to $want")
            }
        }
    }

    $code = if ($problems.Count) { 1 } elseif ($unverified.Count) { 2 } else { 0 }
    [pscustomobject]@{
        Code       = $code
        Expected   = $want
        Msi        = $msiRead
        Setup      = $exeRead
        Problems   = $problems.ToArray()
        Unverified = $unverified.ToArray()
    }
}
