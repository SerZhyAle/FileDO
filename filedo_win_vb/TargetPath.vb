' Where a target typed on a page really is (SP-0029 SHELL-04, GUI-18).
'
' filedo.exe runs with %LOCALAPPDATA%\FileDO as its working folder (Runner), so a relative path typed
' here would be checked in the window's own folder and then run in FileDO's data folder: `..` on the
' Wipe page is LocalAppData for the child. Every page therefore resolves its target once, here, and
' passes the absolute path; a path that is not fully qualified - `..`, `reports`, the drive-relative
' `D:x` - is refused before Run rather than resolved against a folder the user never chose.
Imports System.IO
Imports System.Runtime.InteropServices

Module TargetPath

    Private Function IsAsciiLetter(c As Char) As Boolean
        Return (c >= "A"c AndAlso c <= "Z"c) OrElse (c >= "a"c AndAlso c <= "z"c)
    End Function

    ' "E:" - a drive letter the CLI reads as the whole device, not as E's current folder.
    Public Function IsDriveToken(text As String) As Boolean
        Return text IsNot Nothing AndAlso text.Length = 2 AndAlso IsAsciiLetter(text(0)) AndAlso text(1) = ":"c
    End Function

    ' A path that names the same place from every folder: "C:\..", "C:/..", "\\server\share\..",
    ' "\\?\..". "D:x" and "\x" are rooted but not qualified, and are not.
    Public Function IsFullyQualified(text As String) As Boolean
        If String.IsNullOrEmpty(text) OrElse text.Length < 3 Then Return False
        If IsAsciiLetter(text(0)) AndAlso text(1) = ":"c AndAlso (text(2) = "\"c OrElse text(2) = "/"c) Then Return True
        Return (text(0) = "\"c OrElse text(0) = "/"c) AndAlso (text(1) = "\"c OrElse text(1) = "/"c) AndAlso
               text(2) <> "\"c AndAlso text(2) <> "/"c
    End Function

    ' The target as the child should get it: a drive token as it stands, a fully qualified path with
    ' its "." and ".." folded away, and Nothing for anything else (the caller refuses it).
    Public Function Resolve(text As String) As String
        If String.IsNullOrWhiteSpace(text) Then Return ""
        Dim t = text.Trim()
        If IsDriveToken(t) Then Return t
        If Not IsFullyQualified(t) Then Return Nothing
        Try
            Return Path.GetFullPath(t)
        Catch
            ' A mask (secure C:\x\*.txt) or a name Windows would refuse is absolute already, and the
            ' CLI says what is wrong with it in its own words.
            Return t
        End Try
    End Function

    ' Where the child will look for a path it is given: a qualified path or a drive where it
    ' says, anything else inside the child's working folder - which is what a relative path on the
    ' Command page really means to filedo.exe.
    Public Function AsChildSeesIt(text As String) As String
        If String.IsNullOrWhiteSpace(text) Then Return ""
        Dim t = text.Trim()
        Dim resolved = Resolve(t)
        If resolved IsNot Nothing Then Return resolved
        Try
            Return Path.GetFullPath(Path.Combine(Runner.GetAppDataDir(), t))
        Catch
            Return t
        End Try
    End Function

    ' ---- the system volume (GUI-18) ----------------------------------------

    <DllImport("kernel32.dll", CharSet:=CharSet.Unicode, SetLastError:=True)>
    Private Function QueryDosDevice(deviceName As String, targetPath As System.Text.StringBuilder, max As Integer) As Integer
    End Function

    ' The letter of the volume Windows runs from.
    Private Function SystemDriveLetter() As Char
        Try
            Dim root = Environment.GetEnvironmentVariable("SystemRoot")
            If String.IsNullOrEmpty(root) Then root = Environment.GetFolderPath(Environment.SpecialFolder.Windows)
            If Not String.IsNullOrEmpty(root) AndAlso root.Length >= 2 AndAlso root(1) = ":"c Then Return Char.ToUpperInvariant(root(0))
        Catch
        End Try
        Return "C"c
    End Function

    ' True when the target is the root of the system volume in any of its spellings - "C:", "C:\",
    ' "c:/", "\\?\C:\", a subst letter onto that root - and False for a folder below it. This is the
    ' CLI's redirect rule (SP-0024 CLI-18) read from the window, so the notice on the plan card says
    ' exactly what filedo.exe will do.
    Public Function IsSystemVolumeRoot(target As String) As Boolean
        If String.IsNullOrWhiteSpace(target) Then Return False
        Dim t = target.Trim().Replace("/"c, "\"c)
        If t.StartsWith("\\?\", StringComparison.Ordinal) OrElse t.StartsWith("\\.\", StringComparison.Ordinal) Then
            t = t.Substring(4)
        End If
        If Not (t.Length = 2 OrElse (t.Length = 3 AndAlso t(2) = "\"c)) Then Return False
        If Not IsDriveToken(t.Substring(0, 2)) Then Return False
        Dim letter = Char.ToUpperInvariant(t(0))

        ' A subst letter is the folder it stands for: onto the system root it is the system root, onto
        ' any folder below it it is that folder.
        Try
            Dim sb As New System.Text.StringBuilder(1024)
            If QueryDosDevice(letter & ":", sb, sb.Capacity) > 0 Then
                Dim dev = sb.ToString()
                If dev.StartsWith("\??\", StringComparison.Ordinal) Then
                    Dim onto = dev.Substring(4).TrimEnd("\"c)
                    If onto.Length <> 2 OrElse Not IsDriveToken(onto) Then Return False
                    letter = Char.ToUpperInvariant(onto(0))
                End If
            End If
        Catch
        End Try

        Return letter = SystemDriveLetter()
    End Function

End Module
