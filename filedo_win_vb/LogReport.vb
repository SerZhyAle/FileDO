' Packs the diagnostic files FileDO writes on this machine into one zip and hands it to the
' user's default mail program.
'
' User-initiated only. Nothing in here runs on a timer, at startup, or on a failure path: the
' single entry point is the "Send logs" button. FileDO opens no socket of its own - the archive
' is left on disk and the mail is composed and sent by the user in their own client.
'
' mailto: cannot carry an attachment (RFC 6068 excludes it and every major client drops
' attachment=), so the archive is revealed in Explorer and its path is put on the clipboard;
' the user performs the one manual attach step. See docs/spec-send-logs-to-author.md.

Imports System.IO
Imports System.IO.Compression
Imports System.Text

Module LogReport

    Public Const AuthorEmail As String = "serzhyale@gmail.com"

    ' An archive a mail provider rejects is worse than no archive.
    Private Const MaxFiles As Integer = 40
    Private Const MaxFileBytes As Long = 8L * 1024L * 1024L
    Private Const MaxTotalBytes As Long = 20L * 1024L * 1024L

    ' Only names FileDO itself produces. No wildcards that could sweep in someone else's files.
    Private ReadOnly logPatterns As String() = {
        "filedo_win_debug.log",
        "history.json",
        "check_report_*.log",
        "check_report_*.json",
        "check_report_*.csv",
        "check_state.json",
        "compare_report_*.log",
        "delete_report_*.log",
        "skip_files.list",
        "damaged_files.log"
    }

    Private Class Candidate
        Public Tag As String            ' short name of the directory it came from
        Public FullPath As String
        Public Length As Long
        Public Modified As DateTime
        Public Skip As String = ""      ' non-empty means it was left out, with this reason
    End Class

    ' ---- collection -------------------------------------------------------

    ' Directories FileDO can leave artifacts in. One level deep on purpose: recursing through a
    ' user profile is slow and picks up files that are none of our business.
    Private Function SearchRoots() As List(Of KeyValuePair(Of String, String))
        Dim roots As New List(Of KeyValuePair(Of String, String))
        Dim seen As New List(Of String)

        Dim add As Action(Of String, String) =
            Sub(tag As String, dir As String)
                If String.IsNullOrEmpty(dir) Then Return
                Dim full As String
                Try
                    full = Path.GetFullPath(dir).TrimEnd(Path.DirectorySeparatorChar)
                Catch
                    Return
                End Try
                For Each s As String In seen
                    If String.Equals(s, full, StringComparison.OrdinalIgnoreCase) Then Return
                Next
                seen.Add(full)
                roots.Add(New KeyValuePair(Of String, String)(tag, full))
            End Sub

        add("app", AppFolder())
        add("profile", Environment.GetFolderPath(Environment.SpecialFolder.UserProfile))
        add("temp-ops", Path.Combine(Path.GetTempPath(), "FileDO_Operations"))
        add("temp", Path.GetTempPath())
        Return roots
    End Function

    Private Function Collect() As List(Of Candidate)
        Dim found As New List(Of Candidate)

        For Each root As KeyValuePair(Of String, String) In SearchRoots()
            If Not Directory.Exists(root.Value) Then Continue For
            For Each pattern As String In logPatterns
                Dim hits As String()
                Try
                    hits = Directory.GetFiles(root.Value, pattern, SearchOption.TopDirectoryOnly)
                Catch
                    Continue For
                End Try
                For Each hit As String In hits
                    Dim already As Boolean = False
                    For Each c As Candidate In found
                        If String.Equals(c.FullPath, hit, StringComparison.OrdinalIgnoreCase) Then
                            already = True
                            Exit For
                        End If
                    Next
                    If already Then Continue For
                    Try
                        Dim fi As New FileInfo(hit)
                        found.Add(New Candidate With {
                            .Tag = root.Key,
                            .FullPath = fi.FullName,
                            .Length = fi.Length,
                            .Modified = fi.LastWriteTime
                        })
                    Catch
                    End Try
                Next
            Next
        Next

        ' Newest first, then apply the caps so the newest evidence is the evidence that survives.
        found.Sort(Function(a, b) b.Modified.CompareTo(a.Modified))

        Dim budget As Long = MaxTotalBytes
        Dim kept As Integer = 0
        For Each c As Candidate In found
            If kept >= MaxFiles Then
                c.Skip = "left out: file count cap of " & MaxFiles.ToString() & " reached"
            ElseIf c.Length > MaxFileBytes Then
                c.Skip = "left out: larger than " & (MaxFileBytes \ (1024L * 1024L)).ToString() & " MB"
            ElseIf c.Length > budget Then
                c.Skip = "left out: total payload cap of " & (MaxTotalBytes \ (1024L * 1024L)).ToString() & " MB reached"
            Else
                budget -= c.Length
                kept += 1
            End If
        Next

        Return found
    End Function

    ' ---- archive ----------------------------------------------------------

    ''' <summary>
    ''' Builds the archive under %TEMP%\FileDO_Logs. Returns the archive path, or an empty
    ''' string when no FileDO artifact exists on this machine. fileCount reports how many
    ''' collected files ended up inside.
    ''' </summary>
    Public Function BuildArchive(guiLang As String, ByRef fileCount As Integer) As String
        fileCount = 0
        Dim items As List(Of Candidate) = Collect()
        If items.Count = 0 Then Return ""

        Dim stamp As String = DateTime.Now.ToString("yyyyMMdd-HHmmss")
        Dim dir As String = Path.Combine(Path.GetTempPath(), "FileDO_Logs")
        Directory.CreateDirectory(dir)
        ' Never overwrite an archive the user may be about to attach, even on a second press
        ' inside the same second.
        Dim zipPath As String = Path.Combine(dir, "filedo-logs-" & stamp & ".zip")
        Dim n As Integer = 2
        While File.Exists(zipPath) AndAlso n < 100
            zipPath = Path.Combine(dir, "filedo-logs-" & stamp & "-" & n.ToString() & ".zip")
            n += 1
        End While

        Using fs As New FileStream(zipPath, FileMode.Create, FileAccess.Write, FileShare.None)
            Using zip As New ZipArchive(fs, ZipArchiveMode.Create)
                For Each c As Candidate In items
                    If c.Skip <> "" Then Continue For
                    Try
                        AddFile(zip, c)
                        fileCount += 1
                    Catch ex As Exception
                        c.Skip = "left out: could not be read (" & ex.Message & ")"
                    End Try
                Next

                ' The report goes in last so its manifest reflects what actually made it in.
                Dim entry As ZipArchiveEntry = zip.CreateEntry("filedo-report.txt", CompressionLevel.Optimal)
                Using sw As New StreamWriter(entry.Open(), New UTF8Encoding(False))
                    sw.Write(BuildReport(guiLang, items))
                End Using
            End Using
        End Using

        Return zipPath
    End Function

    Private Sub AddFile(zip As ZipArchive, c As Candidate)
        ' Same name can exist in two roots (history.json in both the app folder and the profile),
        ' so the source directory tag becomes the entry folder.
        Dim entryName As String = c.Tag & "/" & Path.GetFileName(c.FullPath)
        Dim entry As ZipArchiveEntry = zip.CreateEntry(entryName, CompressionLevel.Optimal)

        ' The zip format cannot store a year before 1980, and a log may still be open for
        ' writing, so read it as permissively as possible.
        If c.Modified.Year >= 1980 Then entry.LastWriteTime = New DateTimeOffset(c.Modified)

        Using src As New FileStream(c.FullPath, FileMode.Open, FileAccess.Read,
                                    FileShare.ReadWrite Or FileShare.Delete)
            Using dst As Stream = entry.Open()
                src.CopyTo(dst)
            End Using
        End Using
    End Sub

    Private Function BuildReport(guiLang As String, items As List(Of Candidate)) As String
        Dim b As New StringBuilder()
        b.AppendLine("FileDO log report")
        b.AppendLine("=================")
        b.AppendLine()
        b.AppendLine("Collected (local): " & DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss"))
        b.AppendLine("Collected (UTC):   " & DateTime.UtcNow.ToString("yyyy-MM-dd HH:mm:ss"))
        b.AppendLine()
        b.AppendLine("GUI:        filedo_win.exe " & BuildStamp())
        b.AppendLine("GUI path:   " & SafeExecutablePath())
        b.AppendLine("CLI:        " & CliDescription())
        b.AppendLine("OS:         " & Environment.OSVersion.ToString())
        b.AppendLine("64-bit OS:  " & Environment.Is64BitOperatingSystem.ToString())
        b.AppendLine("64-bit app: " & Environment.Is64BitProcess.ToString())
        b.AppendLine("CLR:        " & Environment.Version.ToString())
        b.AppendLine("UI culture: " & System.Globalization.CultureInfo.CurrentUICulture.Name)
        b.AppendLine("GUI lang:   " & guiLang)
        b.AppendLine()
        b.AppendLine("Included files")
        b.AppendLine("--------------")
        Dim any As Boolean = False
        For Each c As Candidate In items
            If c.Skip <> "" Then Continue For
            any = True
            b.AppendLine(c.Tag & "/" & Path.GetFileName(c.FullPath))
            b.AppendLine("    from " & c.FullPath)
            b.AppendLine("    " & c.Length.ToString() & " bytes, modified " &
                         c.Modified.ToString("yyyy-MM-dd HH:mm:ss"))
        Next
        If Not any Then b.AppendLine("(none)")

        Dim skipped As Boolean = False
        For Each c As Candidate In items
            If c.Skip = "" Then Continue For
            If Not skipped Then
                b.AppendLine()
                b.AppendLine("Skipped files")
                b.AppendLine("-------------")
                skipped = True
            End If
            b.AppendLine(c.FullPath)
            b.AppendLine("    " & c.Length.ToString() & " bytes - " & c.Skip)
        Next

        b.AppendLine()
        b.AppendLine("This archive was built by the user pressing 'Send logs' in the FileDO GUI.")
        b.AppendLine("It contains only files FileDO wrote, plus this report.")
        Return b.ToString()
    End Function

    ' ---- identity ---------------------------------------------------------

    ' The GUI's own file and folder, taken from this assembly rather than from
    ' Application.ExecutablePath so the answer is the same however the code is hosted.
    Private Function SafeExecutablePath() As String
        Try
            Dim loc As String = Reflection.Assembly.GetExecutingAssembly().Location
            If Not String.IsNullOrEmpty(loc) Then Return loc
        Catch
        End Try
        Try
            Return Application.ExecutablePath
        Catch
        End Try
        Return "(unknown)"
    End Function

    Private Function AppFolder() As String
        Try
            Dim dir As String = Path.GetDirectoryName(SafeExecutablePath())
            If Not String.IsNullOrEmpty(dir) Then Return dir
        Catch
        End Try
        Try
            Return Application.StartupPath
        Catch
            Return ""
        End Try
    End Function

    ''' <summary>
    ''' Best available build identity for the GUI. The GUI carries no version resource, so the
    ''' file's own write time - which is what the yyMMddHHmm version scheme is derived from - is
    ''' the honest fallback.
    ''' </summary>
    Public Function BuildStamp() As String
        Dim exe As String = SafeExecutablePath()
        Try
            Dim v As String = CleanVersion(FileVersionInfo.GetVersionInfo(exe).FileVersion)
            If v <> "" Then Return v
        Catch
        End Try
        Try
            Return File.GetLastWriteTime(exe).ToString("yyMMddHHmm")
        Catch
            Return "unknown"
        End Try
    End Function

    ' A version resource carries trailing prose on some builds ("1.2.3 (WinBuild..)"), and the
    ' stamp lands in a mail subject - keep the first token only, and treat a placeholder as absent.
    Private Function CleanVersion(raw As String) As String
        If String.IsNullOrEmpty(raw) Then Return ""
        Dim v As String = raw.Trim().Split(" "c)(0)
        If v = "" OrElse v = "0.0.0.0" Then Return ""
        Return v
    End Function

    ''' <summary>
    ''' Short CLI identity for the About window: the version if the binary carries one, else its
    ''' build stamp, else a plain statement that it is not sitting next to the GUI.
    ''' </summary>
    Public Function CliVersion() As String
        Dim dir As String = AppFolder()
        If dir = "" Then Return "(unknown)"
        Dim exe As String = Path.Combine(dir, "filedo.exe")
        If Not File.Exists(exe) Then Return "not next to the GUI (PATH or Store alias)"
        Try
            Dim v As String = CleanVersion(FileVersionInfo.GetVersionInfo(exe).FileVersion)
            If v <> "" Then Return v
            Return File.GetLastWriteTime(exe).ToString("yyMMddHHmm")
        Catch
            Return "(unknown)"
        End Try
    End Function

    Private Function CliDescription() As String
        Dim dir As String = AppFolder()
        If dir = "" Then Return "(unknown)"
        Dim exe As String = Path.Combine(dir, "filedo.exe")
        If Not File.Exists(exe) Then Return "filedo.exe not found next to the GUI (PATH or Store alias in use)"
        Try
            Dim fi As New FileInfo(exe)
            Dim v As String = CleanVersion(FileVersionInfo.GetVersionInfo(exe).FileVersion)
            If v = "" Then v = "no version resource"
            Return "filedo.exe " & v & ", " & fi.Length.ToString() & " bytes, modified " &
                   fi.LastWriteTime.ToString("yyyy-MM-dd HH:mm:ss")
        Catch ex As Exception
            Return "filedo.exe found, could not be read (" & ex.Message & ")"
        End Try
    End Function

    ' ---- hand-off ---------------------------------------------------------

    ' Each step reports its own failure into problems and lets the others run: a machine with no
    ' default mail client should still end up with a finished archive and an open folder.

    Public Sub RevealInExplorer(archivePath As String, problems As List(Of String))
        Try
            Process.Start("explorer.exe", "/select,""" & archivePath & """")
        Catch ex As Exception
            problems.Add("Explorer: " & ex.Message)
        End Try
    End Sub

    Public Sub CopyPathToClipboard(archivePath As String, problems As List(Of String))
        Try
            Clipboard.SetText(archivePath)
        Catch ex As Exception
            problems.Add("Clipboard: " & ex.Message)
        End Try
    End Sub

    Public Sub OpenMailClient(archivePath As String, problems As List(Of String))
        Try
            Dim url As String = "mailto:" & AuthorEmail &
                                "?subject=" & Uri.EscapeDataString(MailSubject()) &
                                "&body=" & Uri.EscapeDataString(MailBody(archivePath))
            Process.Start(New ProcessStartInfo() With {.FileName = url, .UseShellExecute = True})
        Catch ex As Exception
            problems.Add("Mail program: " & ex.Message)
        End Try
    End Sub

    ' Subject and body stay English whatever the GUI language is - they are read by the author.
    Public Function MailSubject() As String
        Return "FileDO logs " & BuildStamp() & " " & DateTime.Now.ToString("yyyyMMdd-HHmmss")
    End Function

    Public Function MailBody(archivePath As String) As String
        Dim b As New StringBuilder()
        b.AppendLine("Hello,")
        b.AppendLine()
        b.AppendLine("FileDO logs are attached.")
        b.AppendLine()
        b.AppendLine("1) What I did:")
        b.AppendLine("2) What happened:")
        b.AppendLine("3) What I expected instead:")
        b.AppendLine()
        b.AppendLine("Attach this file (the path is already on the clipboard):")
        b.AppendLine(archivePath)
        b.AppendLine()
        b.AppendLine("FileDO GUI " & BuildStamp())
        Return b.ToString()
    End Function

End Module
