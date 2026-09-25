' Which wipe targets filedo.exe will only wipe after asking twice on a console.
'
' This is the window's reading of classifyWipeTarget in cmd\filedo\wipe_handler.go, and the two must
' say the same thing: a drive root, a network share root, a reparse point (junction, symbolic link,
' mount point), the system TEMP folder, and - SP-0027 WIPE-02 - any folder that is or holds one
' Windows or FileDO cannot do without, are "dangerous", and for those the CLI asks for WIPE and then
' for the path, whatever -y says - the canon's rule that a flag skips a prompt, never a safety check.
' A run started from this window has no console to answer on, so such a run would end cancelled
' after the user had typed WIPE on the page. The page therefore refuses it up front and names the
' reason (SP-0014 T11).
Imports System.IO

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
        Dim full As String
        Try
            Dim t = target.Trim()
            ' "E:" is the whole drive to the CLI, not E's current folder.
            If TargetPath.IsDriveToken(t) Then t &= "\"
            full = Path.GetFullPath(t)
        Catch
            ' A path Windows cannot resolve is the CLI's to refuse, with its own message.
            Return ""
        End Try

        ' Reparse point first, as the CLI checks it first.
        Try
            If Directory.Exists(full) AndAlso
               (File.GetAttributes(full) And FileAttributes.ReparsePoint) = FileAttributes.ReparsePoint Then
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

        For Each guarded In GuardedFolders()
            If IsSameOrParent(trimmed, guarded) Then Return "shell_wipe_danger_system"
        Next

        Return ""
    End Function

    ' The folders WIPE-02 guards, and every folder above them: TEMP and TMP, Windows and its Temp,
    ' the user's profile and the folder of profiles, both Program Files, FileDO's own data folder,
    ' and the folder this program runs from.
    Private Function GuardedFolders() As List(Of String)
        Dim found As New List(Of String)
        Dim add = Sub(dir As String)
                      If String.IsNullOrWhiteSpace(dir) Then Return
                      Try
                          found.Add(Path.GetFullPath(dir.Trim()).TrimEnd(Path.DirectorySeparatorChar))
                      Catch
                      End Try
                  End Sub
        Try
            add(Environment.GetEnvironmentVariable("TEMP"))
            add(Environment.GetEnvironmentVariable("TMP"))
            Dim windows = Environment.GetEnvironmentVariable("SystemRoot")
            add(windows)
            If Not String.IsNullOrEmpty(windows) Then add(Path.Combine(windows, "Temp"))
            Dim profile = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)
            add(profile)
            If Not String.IsNullOrEmpty(profile) Then add(Path.GetDirectoryName(profile.TrimEnd(Path.DirectorySeparatorChar)))
            add(Environment.GetEnvironmentVariable("ProgramFiles"))
            add(Environment.GetEnvironmentVariable("ProgramFiles(x86)"))
            add(Environment.GetEnvironmentVariable("ProgramW6432"))
            add(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "FileDO"))
            add(AppDomain.CurrentDomain.BaseDirectory)
        Catch
        End Try
        Return found
    End Function

    ' candidate is guarded itself, or one of the folders it holds is.
    Private Function IsSameOrParent(candidate As String, guarded As String) As Boolean
        If String.Equals(candidate, guarded, StringComparison.OrdinalIgnoreCase) Then Return True
        Return guarded.StartsWith(candidate & Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase)
    End Function

End Module
