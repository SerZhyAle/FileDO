' Which wipe targets filedo.exe will only wipe after asking twice on a console.
'
' This is the window's reading of classifyWipeTarget in cmd\filedo\wipe_handler.go, and the two must
' say the same thing: a drive root, a network share root, a reparse point (junction, symbolic link,
' mount point) and the system TEMP folder are "dangerous", and for those the CLI asks for WIPE and
' then for the path, whatever -y says - the canon's rule that a flag skips a prompt, never a safety
' check. A run started from this window has no console to answer on, so such a run would end
' cancelled after the user had typed WIPE on the page. The page therefore refuses it up front and
' names the reason (SP-0014 T11).
Imports System.IO

Module WipeSafety

    ' The localization key of the reason a target is dangerous, or "" when it is not.
    Public Function DangerKey(target As String) As String
        If String.IsNullOrWhiteSpace(target) Then Return ""
        Dim full As String
        Try
            full = Path.GetFullPath(target.Trim())
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

        Return ""
    End Function

End Module
