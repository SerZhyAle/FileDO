' Packs the diagnostic files FileDO writes on this machine into one zip and hands it to the
' user's default mail program.
'
' User-initiated only. Nothing in here runs on a timer, at startup, or on a failure path: the
' single entry point is the "Send logs" button. FileDO opens no socket of its own - the archive
' is left on disk and the mail is composed and sent by the user in their own client.
'
' mailto: cannot carry an attachment (RFC 6068 excludes it and every major client drops
' attachment=), so the archive is revealed in Explorer and its path is put on the clipboard;
' the user performs the one manual attach step.

Imports System.IO
Imports System.IO.Compression
Imports System.Text

Module LogReport

    Public Const AuthorEmail As String = "sza@ukr.net"

    ' An archive a mail provider rejects is worse than no archive.
    Private Const MaxFiles As Integer = 40
    Private Const MaxFileBytes As Long = 16L * 1024L * 1024L
    Private Const MaxTotalBytes As Long = 20L * 1024L * 1024L

    ' Only names FileDO itself produces. No wildcards that could sweep in someone else's files.
    ' The check, compare and delete reports and damaged_files.log are not here on purpose: they are
    ' lists of the user's files, which the confirmation promises are excluded, and the sanitizer cannot
    ' tell a relative path from prose (AUD-90-F1).
    Private ReadOnly logPatterns As String() = {
        "filedo_win.log",
        "filedo_win.log.1",
        "filedo_win.session-*.log",
        "filedo_win_debug.log"
    }

    ' In the data folder itself, the shell's own log and its one older generation - nothing else
    ' (the CLI's files live in its state folder).
    Private ReadOnly shellLogPatterns As String() = {"filedo_win.log", "filedo_win.log.1", "filedo_win.session-*.log"}

    ' Send logs leaves no pile behind: an archive this old is gone at the next Send logs (SHELL-05).
    Private Const ArchiveRetentionDays As Integer = 7

    Private Class Candidate
        Public Tag As String            ' short name of the directory it came from
        Public FullPath As String
        Public Length As Long
        Public Modified As DateTime
        Public Skip As String = ""      ' non-empty means it was left out, with this reason
        Public EntryName As String
    End Class

    Private Class SearchRoot
        Public Tag As String
        Public Dir As String
        Public Patterns As String()
    End Class

    ' ---- collection -------------------------------------------------------

    ' The folders that are FileDO's own, and only those (SHELL-05): the state folder the CLI keeps
    ' its files in (%LOCALAPPDATA%\FileDO\state - history, the skip and check lists, the check
    ' state), the shell's log beside it, and the folder the programs run from. The profile root and
    ' %TEMP% are not searched any more: a history.json or check_state.json there can be another
    ' program's, and the report promises "only files FileDO wrote". One level deep on purpose.
    Private Function SearchRoots() As List(Of SearchRoot)
        Dim roots As New List(Of SearchRoot)
        Dim seen As New List(Of String)

        Dim add As Action(Of String, String, String()) =
            Sub(tag As String, dir As String, patterns As String())
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
                roots.Add(New SearchRoot With {.Tag = tag, .Dir = full, .Patterns = patterns})
            End Sub

        add("app", If(RootsForTest Is Nothing, AppFolder(), RootsForTest(0)), logPatterns)
        Try
            Dim data = If(RootsForTest Is Nothing, Runner.GetAppDataDir(), RootsForTest(1))
            add("state", Path.Combine(data, "state"), logPatterns)
            add("appdata", data, shellLogPatterns)
        Catch
        End Try
        Return roots
    End Function

    ' How many files an archive would hold, looked up before the user is asked anything: a question
    ' about an empty archive is a question about nothing (APP-BEHAVIOUR rule 5).
    Public Function CountAvailable() As Integer
        Dim n As Integer = 0
        For Each c As Candidate In Collect()
            If c.Skip = "" Then n += 1
        Next
        Return n
    End Function

    Private Function Collect() As List(Of Candidate)
        Dim found As New List(Of Candidate)

        For Each root As SearchRoot In SearchRoots()
            If Not Directory.Exists(root.Dir) Then Continue For
            For Each pattern As String In root.Patterns
                Dim hits As String()
                Try
                    hits = Directory.GetFiles(root.Dir, pattern, SearchOption.TopDirectoryOnly)
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
                        If (fi.Attributes And FileAttributes.ReparsePoint) <> 0 Then Continue For
                        found.Add(New Candidate With {
                            .Tag = root.Tag,
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
            ElseIf Math.Min(c.Length, MaxFileBytes) > budget Then
                c.Skip = "left out: total payload cap of " & (MaxTotalBytes \ (1024L * 1024L)).ToString() & " MB reached"
            Else
                budget -= Math.Min(c.Length, MaxFileBytes)
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

        Return WriteArchive(items, guiLang, Path.Combine(Path.GetTempPath(), "FileDO_Logs"), fileCount)
    End Function

    Private Function WriteArchive(items As List(Of Candidate), guiLang As String, dir As String, ByRef fileCount As Integer) As String
        Dim stamp As String = DateTime.Now.ToString("yyyyMMdd-HHmmss")
        Directory.CreateDirectory(dir)
        SweepOldArchives(dir)
        ' Never overwrite an archive the user may be about to attach, even on a second press
        ' inside the same second.
        Dim zipPath As String = Path.Combine(dir, "filedo-logs-" & stamp & ".zip")
        Dim n As Integer = 2
        While File.Exists(zipPath) AndAlso n < 100
            zipPath = Path.Combine(dir, "filedo-logs-" & stamp & "-" & n.ToString() & ".zip")
            n += 1
        End While

        Using fs As New FileStream(zipPath, FileMode.CreateNew, FileAccess.Write, FileShare.None)
            Using zip As New ZipArchive(fs, ZipArchiveMode.Create)
                Dim remaining As Long = MaxTotalBytes
                Dim sequence As Integer = 0
                For Each c As Candidate In items
                    If c.Skip <> "" Then Continue For
                    Try
                        sequence += 1
                        c.EntryName = "logs/log-" & sequence.ToString("D2") & ".txt"
                        remaining -= AddFile(zip, c, remaining)
                        fileCount += 1
                    Catch ex As Exception
                        ' The archive's own manifest, read by the author - never the screen.
                        c.Skip = "left out: could not be read (" & ex.GetType().Name & ")"
                    End Try
                Next

                ' The report goes in last so its manifest reflects what actually made it in.
                Dim entry As ZipArchiveEntry = zip.CreateEntry("filedo-report.txt", CompressionLevel.Optimal)
                Using sw As New StreamWriter(entry.Open(), New UTF8Encoding(False))
                    sw.Write(BuildReport(guiLang, items))
                End Using
                Dim envEntry = zip.CreateEntry("environment.txt", CompressionLevel.Optimal)
                Using sw As New StreamWriter(envEntry.Open(), New UTF8Encoding(False))
                    sw.Write(EnvironmentSummary(guiLang, fileCount))
                End Using
            End Using
        End Using

        Return zipPath
    End Function

    ' The archives an earlier Send logs left, older than a week. Only this program's own names.
    Friend Function SweepOldArchives(dir As String) As Integer
        Dim removed = 0
        Try
            Dim cutoff = DateTime.Now.AddDays(-ArchiveRetentionDays)
            For Each f In Directory.GetFiles(dir, "filedo-logs-*.zip", SearchOption.TopDirectoryOnly)
                Try
                    If File.GetLastWriteTime(f) < cutoff Then
                        File.Delete(f)
                        removed += 1
                    End If
                Catch
                End Try
            Next
        Catch ex As Exception
            ShellLog.Write("send logs: sweep old archives", ex)
        End Try
        Return removed
    End Function

    Private Function AddFile(zip As ZipArchive, c As Candidate, remaining As Long) As Long
        ' Report file names can themselves contain user paths. Entry names are generated.
        Dim entry As ZipArchiveEntry = zip.CreateEntry(c.EntryName, CompressionLevel.Optimal)

        ' The zip format cannot store a year before 1980, and a log may still be open for
        ' writing, so read it as permissively as possible.
        If c.Modified.Year >= 1980 Then entry.LastWriteTime = New DateTimeOffset(c.Modified)

        Using src As New FileStream(c.FullPath, FileMode.Open, FileAccess.Read,
                                    FileShare.ReadWrite Or FileShare.Delete)
            ' Read a bounded snapshot before sanitizing: a concurrent writer cannot exhaust RAM.
            Dim length = src.Length
            If length > MaxFileBytes Then
                ' A truncated tail could start inside a private-key block. Do not export fragments.
                ' The marker is the one DIAGNOSTIC-REPORT 0.12 section 8 C names.
                Dim marker = "[Diag] LOG OMITTED | reason=oversized_untrusted | source_bytes=" &
                    length.ToString(System.Globalization.CultureInfo.InvariantCulture) & Environment.NewLine
                Using dst As New StreamWriter(entry.Open(), New UTF8Encoding(False))
                    dst.Write(marker)
                End Using
                Return Encoding.UTF8.GetByteCount(marker)
            End If
            Dim bytes(CInt(Math.Min(length, MaxFileBytes)) - 1) As Byte
            Dim used As Integer = 0
            While used < bytes.Length
                Dim wanted = bytes.Length - used
                Dim n = src.Read(bytes, used, wanted)
                If n = 0 Then Exit While
                used += n
            End While
            Dim text = DiagnosticText.Sanitize(Encoding.UTF8.GetString(bytes, 0, used))
            Dim encoded = Encoding.UTF8.GetBytes(text)
            If encoded.LongLength > Math.Min(MaxFileBytes, remaining) Then
                ' Redaction can expand a very short line. Omit rather than split an opaque value.
                text = "[Diag] LOG OMITTED | sanitized payload exceeds archive budget" & Environment.NewLine
                encoded = Encoding.UTF8.GetBytes(text)
            End If
            If encoded.LongLength > remaining Then Throw New IOException("Archive budget exhausted")
            Using dst As New StreamWriter(entry.Open(), New UTF8Encoding(False))
                dst.Write(text)
            End Using
            Return encoded.LongLength
        End Using
    End Function

    Private Function BuildReport(guiLang As String, items As List(Of Candidate)) As String
        Dim b As New StringBuilder()
        b.AppendLine("FileDO log report")
        b.AppendLine("=================")
        b.AppendLine()
        b.AppendLine("Collected (local): " & DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss"))
        b.AppendLine("Collected (UTC):   " & DateTime.UtcNow.ToString("yyyy-MM-dd HH:mm:ss"))
        b.AppendLine()
        b.AppendLine("GUI:        filedo_win.exe " & BuildStamp())
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
            b.AppendLine(c.EntryName)
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
            b.AppendLine("log omitted")
            b.AppendLine("    " & c.Length.ToString() & " bytes - " & c.Skip)
        Next

        b.AppendLine()
        b.AppendLine("This archive was built by the user pressing 'Send logs' in the FileDO GUI.")
        b.AppendLine("It contains sanitized text logs and build facts; history and file lists are excluded.")
        Return DiagnosticText.Sanitize(b.ToString())
    End Function

    Friend Function EnvironmentSummary(guiLang As String, logCount As Integer) As String
        Dim b As New StringBuilder()
        b.AppendLine("schemaVersion=1")
        b.AppendLine("app_id=FileDO")
        b.AppendLine("app_version=" & BuildStamp())
        b.AppendLine("app_build=" & BuildStamp())
        b.AppendLine("app_edition=" & If(Packaging.IsPackaged(), "store", "desktop"))
        Using graphics = System.Drawing.Graphics.FromHwnd(IntPtr.Zero)
            b.AppendLine("dpi_scale=" & (graphics.DpiX / 96.0).ToString("0.###", Globalization.CultureInfo.InvariantCulture))
        End Using
        b.AppendLine("media_backend=none")
        b.AppendLine("backend_stats=unavailable")
        b.AppendLine("os=" & Environment.OSVersion.ToString())
        b.AppendLine("os_arch=" & If(Environment.Is64BitOperatingSystem, "X64", "X86"))
        b.AppendLine("locale=" & Globalization.CultureInfo.CurrentUICulture.Name)
        b.AppendLine("ui_language=" & guiLang)
        b.AppendLine("generated_utc=" & DateTime.UtcNow.ToString("o", Globalization.CultureInfo.InvariantCulture))
        b.AppendLine("logs_total=" & logCount.ToString())
        ' Media-library metrics do not exist in this product. Never package history records.
        For Each key In New String() {"channels_total", "channels_catalog", "channels_manual", "channels_imported", "channels_pinned", "channels_hidden", "collections", "history_entries"}
            b.AppendLine(key & "=0")
        Next
        b.AppendLine("volume_metrics_capability=unavailable")
        b.AppendLine("catalog_refreshed_utc=")
        Return DiagnosticText.Sanitize(b.ToString())
    End Function

    ' The self-test points the search at folders of its own - {the program folder, the data folder} -
    ' so a row never reads, or packs, the machine's real logs.
    Friend RootsForTest As String() = Nothing

    Friend Function ArchiveCollectedForTest(destination As String, ByRef fileCount As Integer) As String
        fileCount = 0
        Return WriteArchive(Collect(), "en", destination, fileCount)
    End Function

    Friend Function ArchiveForTest(source As String, destination As String) As String
        Dim fi As New FileInfo(source)
        Dim items As New List(Of Candidate) From {New Candidate With {.FullPath = fi.FullName, .Length = fi.Length, .Modified = fi.LastWriteTime, .Tag = "test"}}
        Dim count As Integer = 0
        Return WriteArchive(items, "en", destination, count)
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
    ''' The GUI's build identity: the release stamp build.ps1 compiled in (BuildVersion.Stamp, the
    ''' same yyMMddHHmm the CLI and the tag carry). The PE version is not it - it is the stamp
    ''' remapped into four 16-bit fields, and an MSIX build may carry 0.0.0.0 there (SHELL-14). A
    ''' local build has no stamp and says so: "dev", with the time the exe was written.
    ''' </summary>
    Public Function BuildStamp() As String
        Dim stamp As String = FileDOGUI.BuildVersion.Stamp
        If Not String.IsNullOrEmpty(stamp) AndAlso stamp <> "dev" Then Return stamp
        Try
            Return "dev (" & File.GetLastWriteTime(SafeExecutablePath()).ToString("yyMMddHHmm") & ")"
        Catch
            Return "dev"
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
    ''' <param name="missingText">What to say when filedo.exe is not there. The log archive is an
    ''' English artifact and takes the default; a window is read by the user and passes its own
    ''' translated string.</param>
    Public Function CliVersion(Optional missingText As String = "not next to the GUI (PATH or Store alias)") As String
        Dim dir As String = AppFolder()
        If dir = "" Then Return "(unknown)"
        Dim exe As String = Path.Combine(dir, "filedo.exe")
        If Not File.Exists(exe) Then Return missingText
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
            ' The archive's report, read by the author - never the screen.
            Return "filedo.exe found, could not be read (" & ex.GetType().Name & ")"
        End Try
    End Function

    ' ---- hand-off ---------------------------------------------------------

    ' Each step reports its own failure into problems and lets the others run: a machine with no
    ' default mail client should still end up with a finished archive and an open folder. What goes
    ' into problems is a localization key naming the step that failed; the exception goes to the
    ' shell's log.

    Public Sub RevealInExplorer(archivePath As String, problems As List(Of String))
        Try
            Process.Start("explorer.exe", "/select,""" & archivePath & """")
        Catch ex As Exception
            ShellLog.Write("send logs: explorer", ex)
            problems.Add("logs_problem_explorer")
        End Try
    End Sub

    Public Sub CopyPathToClipboard(archivePath As String, problems As List(Of String))
        Try
            Clipboard.SetText(archivePath)
        Catch ex As Exception
            ShellLog.Write("send logs: clipboard", ex)
            problems.Add("logs_problem_clipboard")
        End Try
    End Sub

    Public Sub OpenMailClient(archivePath As String, problems As List(Of String))
        Try
            Dim url As String = "mailto:" & AuthorEmail &
                                "?subject=" & Uri.EscapeDataString(MailSubject()) &
                                "&body=" & Uri.EscapeDataString(MailBody(archivePath))
            Process.Start(New ProcessStartInfo() With {.FileName = url, .UseShellExecute = True})
        Catch ex As Exception
            ShellLog.Write("send logs: mail program", ex)
            problems.Add("logs_problem_mail")
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
