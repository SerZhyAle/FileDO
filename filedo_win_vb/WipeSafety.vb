' Which wipe targets filedo.exe will only wipe after asking twice on a console.
'
' This is the window's reading of classifyWipeTarget in cmd\filedo\wipe_handler.go, and the two must
' say the same thing: a drive root, a volume root, a network share root, a reparse point (junction,
' symbolic link, mount point), the system TEMP folder, any folder that is or holds one Windows or
' FileDO cannot do without (SP-0027 WIPE-02), and anything below Windows, Program Files or FileDO's
' own data folder (AUD-28-F1), are "dangerous", and for those the CLI asks for WIPE and then for the
' path, whatever -y says - the canon's rule that a flag skips a prompt, never a safety check.
' A run started from this window has no console to answer on, so such a run would end cancelled
' after the user had typed WIPE on the page. The page therefore refuses it up front and names the
' reason (SP-0014 T11).
'
' Like the CLI, a root is recognised by its spelling first (`D:`, `D:\.`, `\\?\C:\`, `\\.\C:\`,
' `\\?\UNC\srv\share`, `\\?\Volume{..}\`, `\\?\GLOBALROOT\Device\HarddiskVolumeN`), and the guarded
' folders are compared on the path as typed and on its final path as Windows resolves it - which
' sees through a junction ancestor, an 8.3 name and a subst letter. The CLI's remaining test, file
' identity across two volume names (`\\localhost\C$\..` beside `C:\..`), is followed here only for a
' share on this machine; any other share is left to the CLI's own refusal.
Imports System.IO
Imports System.Runtime.InteropServices
Imports System.Text
Imports Microsoft.Win32.SafeHandles

Module WipeSafety

    ' The CLI's words for wipe: list_of_flags_for_wipe in cmd\filedo\main.go, which is also what the
    ' target-first grammar in command_handlers.go accepts. TestShell_WipeAliasesMatchTheCLI reads
    ' this line, so a new alias on one side fails the build until the other has it too (GUI-11).
    Public ReadOnly WipeAliases As String() = {"wipe", "w"}

    Public Function IsWipeAlias(token As String) As Boolean
        If String.IsNullOrEmpty(token) Then Return False
        For Each a In WipeAliases
            If String.Equals(token, a, StringComparison.OrdinalIgnoreCase) Then Return True
        Next
        Return False
    End Function

    ' The localization key of the reason a target is dangerous, or "" when it is not.
    Public Function DangerKey(target As String) As String
        If String.IsNullOrWhiteSpace(target) Then Return ""

        ' A root named by its spelling alone, as wipeRootBySpelling reads it.
        Dim bySpelling = RootBySpelling(target)
        If bySpelling <> "" Then Return bySpelling

        Dim full As String
        Try
            Dim t = WithoutDevicePrefix(target.Trim())
            ' "E:" is the whole drive to the CLI, not E's current folder.
            If TargetPath.IsDriveToken(t) Then t &= "\"
            full = Path.GetFullPath(t)
        Catch
            ' A path Windows cannot resolve is the CLI's to refuse, with its own message.
            Return ""
        End Try

        ' Reparse point first, as the CLI checks it first.
        Try
            If (File.GetAttributes(full) And FileAttributes.ReparsePoint) = FileAttributes.ReparsePoint Then
                Return "shell_wipe_danger_reparse"
            End If
        Catch
        End Try

        Dim trimmed = full.TrimEnd(Path.DirectorySeparatorChar)
        Dim root As String = Nothing
        Try
            root = Path.GetPathRoot(full)
        Catch
        End Try
        If Not String.IsNullOrEmpty(root) AndAlso
           String.Equals(trimmed, root.TrimEnd(Path.DirectorySeparatorChar), StringComparison.OrdinalIgnoreCase) Then
            Return If(root.StartsWith("\\"), "shell_wipe_danger_share", "shell_wipe_danger_root")
        End If

        Try
            Dim temp = Path.GetTempPath().TrimEnd(Path.DirectorySeparatorChar)
            If String.Equals(trimmed, temp, StringComparison.OrdinalIgnoreCase) Then Return "shell_wipe_danger_temp"
        Catch
        End Try

        ' The target as typed and as Windows resolves it; each guarded folder likewise.
        Dim candidates As New List(Of String) From {trimmed}
        Dim resolved = If(ResolvableHere(trimmed), Resolve(trimmed), Nothing)
        If resolved IsNot Nothing Then
            If RootBySpelling(resolved) <> "" Then Return "shell_wipe_danger_root"
            candidates.Add(resolved.TrimEnd(Path.DirectorySeparatorChar))
        End If

        For Each g In GuardedFolders()
            Dim spellings As New List(Of String) From {g.Path}
            Dim gr = Resolve(g.Path)
            If gr IsNot Nothing Then spellings.Add(gr.TrimEnd(Path.DirectorySeparatorChar))
            For Each c In candidates
                For Each s In spellings
                    If IsSameOrParent(c, s) Then Return "shell_wipe_danger_system"
                    If g.Inside AndAlso IsBelow(c, s) Then Return "shell_wipe_danger_inside"
                Next
            Next
        Next

        Return ""
    End Function

    ' ---- the spelling of a root (wipeRootBySpelling) ----------------------------------------

    ' Windows drops trailing spaces and dots from a name, so `D:\ ` and `D:\.` are `D:\`.
    Private Function RootBySpelling(p As String) As String
        Dim s = p.Trim().Replace("/"c, "\"c).TrimEnd(" "c, "."c)
        Dim upper = s.ToUpperInvariant()

        If upper.StartsWith("\\?\") OrElse upper.StartsWith("\\.\") Then
            Dim rest = s.Substring(4)
            Dim restUpper = upper.Substring(4)
            If restUpper.StartsWith("UNC\") Then
                s = "\\" & rest.Substring(4)
            ElseIf restUpper.StartsWith("GLOBALROOT") Then
                ' \\?\GLOBALROOT\Device\HarddiskVolumeN is a volume itself.
                Return If(SplitNonEmpty(rest.Substring("GLOBALROOT".Length)).Count <= 2, "shell_wipe_danger_volume", "")
            ElseIf restUpper.StartsWith("VOLUME{") Then
                Return If(SplitNonEmpty(rest).Count <= 1, "shell_wipe_danger_volume", "")
            ElseIf rest.Length >= 2 AndAlso rest(1) = ":"c AndAlso IsDriveLetter(rest(0)) Then
                s = rest
            Else
                ' Any other device path (\\.\PhysicalDrive0, \\?\HarddiskVolume3): the device itself.
                Return If(SplitNonEmpty(rest).Count <= 1, "shell_wipe_danger_root", "")
            End If
        End If

        ' `D:` alone is the drive to a wipe, and a drive is a root.
        If s.Length >= 2 AndAlso s(1) = ":"c AndAlso IsDriveLetter(s(0)) Then
            If s.Length = 2 Then Return "shell_wipe_danger_root"
            If s(2) = "\"c AndAlso CleanDepth(s.Substring(3)) = 0 Then Return "shell_wipe_danger_root"
            Return ""
        End If

        ' \\server\share or \\server
        If s.StartsWith("\\") AndAlso SplitNonEmpty(s.Substring(2)).Count <= 2 Then Return "shell_wipe_danger_share"
        Return ""
    End Function

    ' \\?\C:\x is C:\x, \\?\UNC\srv\share\x is \\srv\share\x; any other spelling is kept.
    Private Function WithoutDevicePrefix(p As String) As String
        Dim s = p.Replace("/"c, "\"c)
        If s.StartsWith("\\?\") OrElse s.StartsWith("\\.\") Then
            Dim rest = s.Substring(4)
            If rest.StartsWith("UNC\", StringComparison.OrdinalIgnoreCase) Then Return "\\" & rest.Substring(4)
            If rest.Length >= 2 AndAlso rest(1) = ":"c AndAlso IsDriveLetter(rest(0)) Then Return rest
        End If
        Return p
    End Function

    Private Function SplitNonEmpty(p As String) As List(Of String)
        Dim out As New List(Of String)
        For Each part In p.Split("\"c)
            If part <> "" AndAlso part <> "." Then out.Add(part)
        Next
        Return out
    End Function

    ' How many folders below the root a path names once `.` and `..` are applied (filepath.Clean).
    Private Function CleanDepth(p As String) As Integer
        Dim depth = 0
        For Each part In p.Split("\"c)
            If part = "" OrElse part = "." Then Continue For
            If part = ".." Then
                depth = Math.Max(0, depth - 1)
            Else
                depth += 1
            End If
        Next
        Return depth
    End Function

    Private Function IsDriveLetter(c As Char) As Boolean
        Return (c >= "A"c AndAlso c <= "Z"c) OrElse (c >= "a"c AndAlso c <= "z"c)
    End Function

    ' ---- the guarded folders (wipeProtectedPaths) -------------------------------------------

    Private Structure Guarded
        Public Path As String
        ' Inside: a wipe of anything below it is dangerous too.
        Public Inside As Boolean
    End Structure

    ' TEMP and TMP, Windows and its Temp, the user's profile and the folder of profiles, all three
    ' Program Files, FileDO's own data folder and its state folder, and the folder this program runs
    ' from. Windows, Program Files and FileDO's data folder guard everything below them as well.
    Private Function GuardedFolders() As List(Of Guarded)
        Dim found As New List(Of Guarded)
        Dim add = Sub(dir As String, inside As Boolean)
                      If String.IsNullOrWhiteSpace(dir) Then Return
                      Try
                          found.Add(New Guarded With {
                              .Path = Path.GetFullPath(dir.Trim()).TrimEnd(Path.DirectorySeparatorChar),
                              .Inside = inside})
                      Catch
                      End Try
                  End Sub
        Try
            add(Environment.GetEnvironmentVariable("TEMP"), False)
            add(Environment.GetEnvironmentVariable("TMP"), False)
            add(Path.GetTempPath(), False)
            Dim windows = Environment.GetEnvironmentVariable("SystemRoot")
            If String.IsNullOrEmpty(windows) Then windows = Environment.GetEnvironmentVariable("windir")
            add(windows, True)
            If Not String.IsNullOrEmpty(windows) Then add(Path.Combine(windows, "Temp"), False)
            Dim profile = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)
            add(profile, False)
            If Not String.IsNullOrEmpty(profile) Then add(Path.GetDirectoryName(profile.TrimEnd(Path.DirectorySeparatorChar)), False)
            Dim systemDrive = Environment.GetEnvironmentVariable("SystemDrive")
            If Not String.IsNullOrEmpty(systemDrive) Then add(systemDrive & "\Users", False)
            add(Environment.GetEnvironmentVariable("ProgramFiles"), True)
            add(Environment.GetEnvironmentVariable("ProgramFiles(x86)"), True)
            add(Environment.GetEnvironmentVariable("ProgramW6432"), True)
            Dim localData = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)
            If Not String.IsNullOrEmpty(localData) Then
                add(Path.Combine(localData, "FileDO"), True)
            End If
            ' The state root as statedir.Dir finds it: FILEDO_STATE_DIR, or FileDO\state.
            Dim stateOverride = Environment.GetEnvironmentVariable("FILEDO_STATE_DIR")
            If Not String.IsNullOrEmpty(stateOverride) Then
                add(stateOverride, False)
            ElseIf Not String.IsNullOrEmpty(localData) Then
                add(Path.Combine(localData, "FileDO", "state"), False)
            End If
            add(AppDomain.CurrentDomain.BaseDirectory, False)
        Catch
        End Try
        Return found
    End Function

    ' candidate is guarded itself, or one of the folders it holds is.
    Private Function IsSameOrParent(candidate As String, guarded As String) As Boolean
        If String.Equals(candidate, guarded, StringComparison.OrdinalIgnoreCase) Then Return True
        Return guarded.StartsWith(candidate & Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase)
    End Function

    ' candidate lies below guarded.
    Private Function IsBelow(candidate As String, guarded As String) As Boolean
        Return candidate.StartsWith(guarded & Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase)
    End Function

    ' ---- the final path (fsx.Resolve) -------------------------------------------------------

    ' A share on another machine is not opened here: the page asks on every keystroke, and an
    ' unreachable server would stall it. This machine's own shares are resolved.
    Private Function ResolvableHere(p As String) As Boolean
        If Not p.StartsWith("\\") Then Return True
        Dim host = p.Substring(2).Split("\"c)(0)
        Return String.Equals(host, "localhost", StringComparison.OrdinalIgnoreCase) OrElse host = "127.0.0.1" OrElse
               host = "." OrElse String.Equals(host, Environment.MachineName, StringComparison.OrdinalIgnoreCase)
    End Function

    ' The final path of p's nearest existing ancestor with the missing tail appended, or Nothing.
    Friend Function Resolve(p As String) As String
        Try
            Dim cur = p.TrimEnd(Path.DirectorySeparatorChar)
            Dim tail As New List(Of String)
            For guard = 0 To 64
                Dim fp = FinalPath(If(cur.EndsWith(":"), cur & "\", cur))
                If fp IsNot Nothing Then
                    For i = tail.Count - 1 To 0 Step -1
                        fp = Path.Combine(fp, tail(i))
                    Next
                    Return fp
                End If
                Dim parent = Path.GetDirectoryName(cur)
                If String.IsNullOrEmpty(parent) OrElse String.Equals(parent, cur, StringComparison.OrdinalIgnoreCase) Then Return Nothing
                tail.Add(Path.GetFileName(cur))
                cur = parent.TrimEnd(Path.DirectorySeparatorChar)
            Next
        Catch
        End Try
        Return Nothing
    End Function

    Private Const FileShareAll As UInteger = 7UI
    Private Const OpenExisting As UInteger = 3UI
    Private Const FileFlagBackupSemantics As UInteger = &H2000000UI

    <DllImport("kernel32.dll", CharSet:=CharSet.Unicode, SetLastError:=True)>
    Private Function CreateFileW(name As String, access As UInteger, share As UInteger, security As IntPtr,
                                 disposition As UInteger, flags As UInteger, template As IntPtr) As SafeFileHandle
    End Function

    <DllImport("kernel32.dll", CharSet:=CharSet.Unicode, SetLastError:=True)>
    Private Function GetFinalPathNameByHandleW(handle As SafeFileHandle, buffer As StringBuilder, size As UInteger, flags As UInteger) As UInteger
    End Function

    Private Function FinalPath(p As String) As String
        Using h = CreateFileW(p, 0UI, FileShareAll, IntPtr.Zero, OpenExisting, FileFlagBackupSemantics, IntPtr.Zero)
            If h.IsInvalid Then Return Nothing
            Dim sb As New StringBuilder(1024)
            Dim n = GetFinalPathNameByHandleW(h, sb, CUInt(sb.Capacity), 0UI)
            If n = 0UI Then Return Nothing
            If n >= CUInt(sb.Capacity) Then
                sb = New StringBuilder(CInt(n) + 1)
                n = GetFinalPathNameByHandleW(h, sb, CUInt(sb.Capacity), 0UI)
                If n = 0UI OrElse n >= CUInt(sb.Capacity) Then Return Nothing
            End If
            Dim s = sb.ToString()
            If s.StartsWith("\\?\UNC\", StringComparison.OrdinalIgnoreCase) Then Return "\\" & s.Substring(8)
            If s.StartsWith("\\?\") Then Return s.Substring(4)
            Return s
        End Using
    End Function

End Module
