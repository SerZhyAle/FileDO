Imports System.IO
Imports System.Runtime.InteropServices
Imports System.Text
Imports System.Text.RegularExpressions
Imports System.Threading
Imports System.Threading.Tasks

' The process execution layer (SP-0006 section 7.2 / M1).
' Starts child processes with redirected I/O, manages event stream tailing and stop files,
' sets working directory to %LOCALAPPDATA%\FileDO, and passes credentials out of band.
Public Class Runner
    Implements IDisposable

    Public Class RunResult
        Public Property ExitCode As Integer
        Public Property Verdict As String ' Passed, Failed, Stopped, Done, Not proven
        Public Property Reason As String  ' a localization key explaining a verdict the run did not prove
        Public Property Output As String
        Public Property Duration As TimeSpan
        Public Property ResultInfo As EventStream.ResultInfo
        Public Property TimedOut As Boolean
    End Class

    Public Event OutputLineReceived(line As String, isError As Boolean)
    Public Event StepChanged(stepName As String, description As String)
    Public Event ProgressReported(progress As EventStream.ProgressInfo)
    Public Event FindingReported(findingType As String, message As String, details As Dictionary(Of String, Object))
    Public Event NoteReported(message As String)
    Public Event ResultReceived(result As EventStream.ResultInfo)

    ' What Windows returns when the user answers No to the UAC prompt.
    Private Const ERROR_CANCELLED As Integer = 1223

    ' D8: the run reports are kept for 30 days. The event files are working material and go sooner.
    Private Const ReportRetentionDays As Integer = 30
    Private Const RunFileRetentionDays As Integer = 7

    ' GUI-19: how long the output of an ended filedo.exe may take to reach its end.
    Private Const OutputDrainMs As Integer = 3000

    ' GUI-14: what a run keeps of its own output in memory - the start and the most recent part.
    ' The whole of it goes to the report on disk as it arrives, never through memory.
    Private Const OutputHeadChars As Integer = 64 * 1024
    Private Const OutputTailChars As Integer = 1024 * 1024

    Private currentProcess As Process
    Private currentStopFile As String
    Private currentEventStream As EventStream
    Private isRunning As Boolean = False
    Private ReadOnly outputLock As New Object()
    Private output As New BoundedText(OutputHeadChars, OutputTailChars)
    Private spool As StreamWriter
    Private spoolBroken As Boolean = False
    Private latestResultInfo As EventStream.ResultInfo
    Private disposed As Boolean = False

    ' Nonzero once the channel declared a MAJOR this build cannot read. The run
    ' is then *Not proven* and the message names the contract and says to
    ' update, which is the one clear message the compatibility law asks for.
    Private channelRefusedVersion As Integer = 0

    Public ReadOnly Property IsActive As Boolean
        Get
            Return isRunning
        End Get
    End Property

    Public Shared Function GetAppDataDir() As String
        Dim appData = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)
        Dim dir = Path.Combine(appData, "FileDO")
        If Not Directory.Exists(dir) Then
            Directory.CreateDirectory(dir)
        End If
        Return dir
    End Function

    ' SHELL-13's seam: the self-test makes the runs folder unavailable without touching the disk.
    Friend Shared RunsDirForTest As Func(Of String) = Nothing

    Public Shared Function GetRunsDir() As String
        If RunsDirForTest IsNot Nothing Then Return RunsDirForTest()
        Dim dir = Path.Combine(GetAppDataDir(), "runs")
        If Not Directory.Exists(dir) Then
            Directory.CreateDirectory(dir)
        End If
        Return dir
    End Function

    Public Shared Function GetReportsDir() As String
        Dim dir = Path.Combine(GetAppDataDir(), "reports")
        If Not Directory.Exists(dir) Then
            Directory.CreateDirectory(dir)
        End If
        Return dir
    End Function

    ' The stated retention of section 7.3 and D8, swept once per start (SHELL-02) - Program.Main
    ' calls this on a background thread after the single-instance decision. A folder that cannot be
    ' read is logged and left; the window never waits for this.
    Public Shared Sub SweepOldRuns()
        Try
            SweepDirectory(GetRunsDir(), RunFileRetentionDays)
            SweepDirectory(GetReportsDir(), ReportRetentionDays)
        Catch ex As Exception
            ShellLog.Write("sweep old runs and reports", ex)
        End Try
    End Sub

    ' Removes the files of one folder last written more than days ago; returns how many went.
    Friend Shared Function SweepDirectory(dir As String, days As Integer) As Integer
        Dim removed = 0
        Try
            Dim cutoff = DateTime.Now.AddDays(-days)
            For Each f In Directory.GetFiles(dir, "*.*")
                If File.GetLastWriteTime(f) < cutoff Then
                    Try
                        File.Delete(f)
                        removed += 1
                    Catch
                    End Try
                End If
            Next
        Catch ex As Exception
            ShellLog.Write("sweep " & dir, ex)
        End Try
        Return removed
    End Function

    Public Shared Function LocateCLI() As String
        ' 1. Sibling of running GUI
        Dim appDir = AppDomain.CurrentDomain.BaseDirectory
        Dim sibling = Path.Combine(appDir, "filedo.exe")
        If File.Exists(sibling) Then Return sibling

        ' 2. Working directory
        If File.Exists("filedo.exe") Then Return Path.GetFullPath("filedo.exe")

        ' 3. exe_to_download relative to dev root if running in IDE
        Dim devSibling = Path.Combine(appDir, "..", "..", "..", "exe_to_download", "filedo.exe")
        If File.Exists(devSibling) Then Return Path.GetFullPath(devSibling)

        ' 4. PATH search
        Dim pathVar = Environment.GetEnvironmentVariable("PATH")
        If Not String.IsNullOrEmpty(pathVar) Then
            For Each pDir In pathVar.Split(Path.PathSeparator)
                Try
                    Dim cand = Path.Combine(pDir.Trim(), "filedo.exe")
                    If File.Exists(cand) Then Return cand
                Catch
                End Try
            Next
        End If

        Return "filedo.exe"
    End Function

    ' How every child is started (GUI-04, GUI-09).
    '
    ' stdin is always a pipe, and the caller closes it right after Start: a child started with
    ' CreateNoWindow and no redirect gets a hidden console for stdin, and every question filedo.exe
    ' asks - the C: redirect, a duplicate delete, a wipe or secure without -y - then waits in Scanln
    ' for an answer nobody can type, and a stop file cannot end it. With the pipe closed each prompt
    ' reads end-of-input and takes its safe branch at once. Go writes UTF-8 to a pipe, so both
    ' output streams are read as UTF-8 - read as the ANSI code page, D:\Фото arrived as mojibake.
    Friend Shared Function NewStartInfo(fileName As String, arguments As String, workingDir As String,
                                        elevate As Boolean) As ProcessStartInfo
        Dim psi As New ProcessStartInfo() With {
            .FileName = fileName,
            .Arguments = arguments,
            .WorkingDirectory = workingDir,
            .UseShellExecute = False,
            .CreateNoWindow = True,
            .RedirectStandardOutput = True,
            .RedirectStandardError = True,
            .RedirectStandardInput = True,
            .StandardOutputEncoding = New UTF8Encoding(False),
            .StandardErrorEncoding = New UTF8Encoding(False)
        }

        If elevate Then
            ' Elevation needs ShellExecute, which cannot redirect anything.
            psi.UseShellExecute = True
            psi.Verb = "runas"
            psi.RedirectStandardOutput = False
            psi.RedirectStandardError = False
            psi.RedirectStandardInput = False
            psi.StandardOutputEncoding = Nothing
            psi.StandardErrorEncoding = Nothing
        End If
        Return psi
    End Function

    ' Writes what the caller had to say, then closes the pipe - which is the answer "no more input"
    ' every prompt of the child reads (GUI-04).
    Private Shared Sub FeedAndCloseStdin(p As Process, input As String)
        Try
            Using writer = p.StandardInput
                If input IsNot Nothing Then writer.Write(input)
            End Using
        Catch ex As IOException
            ' The child ended before it read anything; a closed pipe is what was meant anyway.
        Catch ex As InvalidOperationException
        End Try
    End Sub

    Public Async Function ExecuteAsync(args As IEnumerable(Of String),
                                         Optional envVars As Dictionary(Of String, String) = Nothing,
                                         Optional stdinInput As String = Nothing,
                                         Optional elevate As Boolean = False) As Task(Of RunResult)
        If isRunning Then
            Throw New InvalidOperationException("A command is already running.")
        End If

        isRunning = True
        Dim startTime = DateTime.Now
        SyncLock outputLock
            output = New BoundedText(OutputHeadChars, OutputTailChars)
            spoolBroken = False
        End SyncLock
        latestResultInfo = Nothing

        Dim runId = DateTime.Now.ToString("yyyyMMdd_HHmmss") & "_" & Guid.NewGuid().ToString("N").Substring(0, 6)
        Dim fullArgs As New List(Of String)()
        Dim spoolPath As String = Nothing
        Dim psi As ProcessStartInfo = Nothing

        ' SHELL-13: everything before the process exists can fail on a data folder that cannot be
        ' written. Such a run is Not proven with its reason, and the page is not left "running".
        Try
            Dim runsDir = GetRunsDir()
            Dim eventPath = Path.Combine(runsDir, runId & ".jsonl")
            currentStopFile = Path.Combine(runsDir, runId & ".stop")

            ' Prepend machine channel and stop file parameters
            fullArgs.Add("--events")
            fullArgs.Add(eventPath)
            fullArgs.Add("--stop-file")
            fullArgs.Add(currentStopFile)
            If args IsNot Nothing Then
                fullArgs.AddRange(args)
            End If

            currentEventStream = New EventStream(eventPath)
            channelRefusedVersion = 0
            AddHandler currentEventStream.UnsupportedVersion, Sub(v) channelRefusedVersion = v
            AddHandler currentEventStream.StepChanged, Sub(n, d) RaiseEvent StepChanged(n, d)
            AddHandler currentEventStream.ProgressReported, Sub(p) RaiseEvent ProgressReported(p)
            AddHandler currentEventStream.FindingReported, Sub(t, m, d) RaiseEvent FindingReported(t, m, d)
            AddHandler currentEventStream.NoteReported, Sub(m) RaiseEvent NoteReported(m)
            AddHandler currentEventStream.ResultReceived, Sub(r)
                                                              latestResultInfo = r
                                                              RaiseEvent ResultReceived(r)
                                                          End Sub

            ' The report is written as the output arrives (GUI-14) - except for a run whose output
            ' can name what a container keeps sealed, which is never written anywhere (SHELL-01).
            If ShellSettings.HistoryEnabled() AndAlso Not IsSensitiveRun(fullArgs) Then
                spoolPath = Path.Combine(runsDir, runId & ".out")
                spool = New StreamWriter(spoolPath, False, New UTF8Encoding(False))
            End If

            psi = NewStartInfo(LocateCLI(), ArgQuoting.JoinArgs(fullArgs), GetAppDataDir(), elevate)
            If envVars IsNot Nothing AndAlso Not psi.UseShellExecute Then
                For Each kvp In envVars
                    psi.EnvironmentVariables(kvp.Key) = kvp.Value
                Next
            End If
        Catch ex As Exception
            ShellLog.Write("prepare the run", ex)
            CloseSpool()
            DeleteQuietly(spoolPath)
            If currentEventStream IsNot Nothing Then
                currentEventStream.Dispose()
                currentEventStream = Nothing
            End If
            isRunning = False
            Return New RunResult With {
                .ExitCode = 2,
                .Verdict = "Not proven",
                .Reason = "shell_start_failed",
                .Output = "",
                .Duration = DateTime.Now - startTime,
                .ResultInfo = Nothing,
                .TimedOut = False
            }
        End Try

        Return Await Task.Run(
            Function() As RunResult
                Try
                    currentProcess = New Process With {.StartInfo = psi, .EnableRaisingEvents = True}

                    If Not psi.UseShellExecute Then
                        AddHandler currentProcess.OutputDataReceived, Sub(sender, e) OnOutput(e.Data, False)
                        AddHandler currentProcess.ErrorDataReceived, Sub(sender, e) OnOutput(e.Data, True)
                    End If

                    currentProcess.Start()

                    If Not psi.UseShellExecute Then
                        ChildJob.Assign(currentProcess)
                        currentProcess.BeginOutputReadLine()
                        currentProcess.BeginErrorReadLine()
                        FeedAndCloseStdin(currentProcess, stdinInput)
                    End If

                    ' Poll event stream in loop until process exits
                    While Not currentProcess.WaitForExit(100)
                        If currentEventStream IsNot Nothing Then
                            currentEventStream.Poll()
                        End If
                    End While

                    DrainOutput(currentProcess)

                    ' Process any final events flushed at exit
                    If currentEventStream IsNot Nothing Then
                        currentEventStream.Poll()
                    End If

                    Dim duration = DateTime.Now - startTime
                    Dim exitCode = currentProcess.ExitCode

                    Dim reason As String = ""
                    Dim verdict = Judge(exitCode, latestResultInfo, File.Exists(currentStopFile),
                                        channelRefusedVersion, reason)

                    ' Save run report to %LOCALAPPDATA%\FileDO\reports (D8 / 7.7)
                    CloseSpool()
                    SaveReport(runId, fullArgs, verdict, exitCode, duration, spoolPath)

                    Return New RunResult With {
                        .ExitCode = exitCode,
                        .Verdict = verdict,
                        .Reason = reason,
                        .Output = OutputText(),
                        .Duration = duration,
                        .ResultInfo = latestResultInfo,
                        .TimedOut = False
                    }
                Catch ex As System.ComponentModel.Win32Exception When ex.NativeErrorCode = ERROR_CANCELLED
                    ' A refused UAC prompt is a Stopped result with a plain explanation, not an
                    ' error (SP-0006 section 7.6).
                    Return New RunResult With {
                        .ExitCode = 2,
                        .Verdict = "Stopped",
                        .Reason = "shell_elevation_refused",
                        .Output = OutputText(),
                        .Duration = DateTime.Now - startTime,
                        .ResultInfo = Nothing,
                        .TimedOut = False
                    }
                Catch ex As Exception
                    ' The cause is the log's; the page shows the named reason (APP-BEHAVIOUR rule 6).
                    ShellLog.Write("start or watch filedo.exe", ex)
                    Return New RunResult With {
                        .ExitCode = 2,
                        .Verdict = "Not proven",
                        .Reason = "shell_start_failed",
                        .Output = OutputText(),
                        .Duration = DateTime.Now - startTime,
                        .ResultInfo = Nothing,
                        .TimedOut = False
                    }
                Finally
                    CloseSpool()
                    DeleteQuietly(spoolPath)
                    If currentEventStream IsNot Nothing Then
                        currentEventStream.Dispose()
                        currentEventStream = Nothing
                    End If
                    If currentProcess IsNot Nothing Then
                        currentProcess.Dispose()
                        currentProcess = Nothing
                    End If
                    isRunning = False
                End Try
            End Function)
    End Function

    ' One line of the child's output: kept (bounded) for the result, spooled whole for the report,
    ' and handed to the page.
    Private Sub OnOutput(line As String, isError As Boolean)
        If line Is Nothing Then Return
        SyncLock outputLock
            output.AppendLine(line)
            If spool IsNot Nothing Then
                Try
                    spool.WriteLine(line)
                Catch ex As Exception
                    ' The report then carries what memory kept; the run itself goes on.
                    ShellLog.Write("spool the run's output", ex)
                    spoolBroken = True
                    Try
                        spool.Dispose()
                    Catch
                    End Try
                    spool = Nothing
                End Try
            End If
        End SyncLock
        RaiseEvent OutputLineReceived(line, isError)
    End Sub

    Private Function OutputText() As String
        SyncLock outputLock
            Return output.ToString()
        End SyncLock
    End Function

    Private Sub CloseSpool()
        SyncLock outputLock
            If spool Is Nothing Then Return
            Try
                spool.Dispose()
            Catch ex As Exception
                ShellLog.Write("close the run's output spool", ex)
                spoolBroken = True
            End Try
            spool = Nothing
        End SyncLock
    End Sub

    Private Shared Sub DeleteQuietly(path As String)
        If String.IsNullOrEmpty(path) Then Return
        Try
            If File.Exists(path) Then File.Delete(path)
        Catch
        End Try
    End Sub

    ' GUI-19: WaitForExit(timeout) returns when the process has ended, not when its output has been
    ' read to the end - the last lines, often the ones that say why, could still be on their way.
    ' The untimed WaitForExit does wait for both readers; it is bounded because a program filedo.exe
    ' started (the viewer of a reveal) can inherit the pipe and hold it open long after filedo.exe
    ' itself has gone.
    Private Shared Sub DrainOutput(p As Process)
        If p Is Nothing Then Return
        Dim drained = False
        Try
            drained = Task.Run(Sub()
                                   Try
                                       p.WaitForExit()
                                   Catch
                                   End Try
                               End Sub).Wait(OutputDrainMs)
        Catch
        End Try
        If Not drained Then
            ShellLog.Info("the output of filedo.exe had not ended " & OutputDrainMs.ToString() &
                          " ms after it exited; a program it started may still hold it open")
        End If
    End Sub

    ' The verdict, and the two ways it is allowed to be "Not proven".
    '
    ' The shell never infers success from a process that merely exited (principle 3). A run that
    ' wrote no result event has not told the shell anything, whatever its exit code says - that is
    ' the honest-degradation path of principle 8, and the reason names the older build rather than
    ' inventing a verdict for it. When a result event *is* present, the exit code is a cross-check
    ' on it: two sources that disagree are reported as not proven, loudly, which is what catches a
    ' broken interpreter during development. Both halves are CLI-EVENT-STREAM rules 10 and 11, and
    ' the digits are that contract's supervisor vocabulary - not the container classes of
    ' FDSEC-BEHAVIOUR section 7.1, which mean something else and reach here only through a result
    ' event (rule 12).
    Friend Shared Function Judge(exitCode As Integer,
                                 result As EventStream.ResultInfo,
                                 stopped As Boolean,
                                 refusedVersion As Integer,
                                 ByRef reason As String) As String
        reason = ""

        ' A MAJOR above the one this build knows stops interpretation and is
        ' reported as a verdict, not as an error: the lines this window does
        ' understand are the ones that would not have changed the answer, so
        ' reading them and ignoring the rest is exactly what must not happen.
        If refusedVersion > EventStream.KnownSchemaVersion Then
            reason = "shell_channel_unsupported"
            Return "Not proven"
        End If

        If result Is Nothing OrElse String.IsNullOrEmpty(result.Verdict) Then
            If stopped Then Return "Stopped"
            reason = "shell_not_proven_reason"
            Return "Not proven"
        End If

        Dim verdict = result.Verdict
        If stopped AndAlso verdict <> "Stopped" Then
            Return "Stopped"
        End If

        Dim expected As Integer
        ' Verdict words are contract tokens, not presentation text. Case
        ' folding or trimming would turn an unknown future word into a result
        ' this build has no right to claim (CLI-EVENT-STREAM rule 8).
        Select Case verdict
            Case "Passed", "Done" : expected = 0
            Case "Failed" : expected = 1
            Case "Not proven" : expected = 2
            Case "Stopped" : Return "Stopped"    ' a stop carries its own ending
            Case Else
                ' A word this build does not know is a word whose meaning it
                ' cannot guess. A future verdict must not become a label on
                ' screen that looks like an answer (rule 8 applied to the one
                ' field the whole channel exists to deliver).
                reason = "shell_verdict_unknown"
                Return "Not proven"
        End Select

        ' The exit code is a cross-check only where it speaks this contract's
        ' vocabulary. A container verb's digits are FDSEC-BEHAVIOUR section
        ' 7.1 - 3 is a wrong credential, not "the code disagrees" - and rule 12
        ' says they must never be read through rule 11's three. A code outside
        ' those three is therefore not a disagreement; the result event is what
        ' carries the verdict across that seam, and it has already done so.
        If exitCode > 2 OrElse exitCode < 0 Then
            Return verdict
        End If

        If exitCode <> expected Then
            reason = "shell_verdict_mismatch"
            Return "Not proven"
        End If

        Return verdict
    End Function

    Public Sub RequestStop()
        If Not isRunning OrElse String.IsNullOrEmpty(currentStopFile) Then Return
        Try
            File.WriteAllText(currentStopFile, "stop")
        Catch ex As Exception
            ShellLog.Write("write the stop file", ex)
        End Try
    End Sub

    Public Sub ForceKill()
        If isRunning AndAlso currentProcess IsNot Nothing Then
            Try
                currentProcess.Kill()
            Catch ex As Exception
                ' It ended between the check and the call, or Windows refused; either way it is logged.
                ShellLog.Write("end filedo.exe", ex)
            End Try
        End If
    End Sub

    ' D8 is both halves: a report for every run, and a settings switch that turns the whole thing
    ' off. When it is off, no report file is written at all - not an empty one, not a shorter one.
    Private Sub SaveReport(runId As String, args As IList(Of String), verdict As String, exitCode As Integer,
                           duration As TimeSpan, spoolPath As String)
        If Not ShellSettings.HistoryEnabled() Then Return
        Try
            Dim reportFile = Path.Combine(GetReportsDir(), "report_" & runId & ".log")
            Dim fromSpool = If(spoolBroken, Nothing, spoolPath)
            WriteReport(reportFile, runId, args, verdict, exitCode, duration, fromSpool,
                        If(fromSpool Is Nothing, OutputText(), Nothing))
        Catch ex As Exception
            ShellLog.Write("save the run report", ex)
        End Try
    End Sub

    ' The report itself (SHELL-01). The Command line passes the same redaction history.json does,
    ' so a hand-typed p:<password> is p:*** here too. A run whose output can name what a container
    ' keeps sealed - a reveal, fdsec info or verify, unsecure .. start, or a batch that holds one -
    ' keeps its verdict and exit code and nothing it printed: the report is kept for 30 days, and the
    ' sealed true name is exactly what the format exists to hide (FDSEC-BEHAVIOUR 6.8). Anything
    ' else is copied from the spool line by line, so a long run never passes through memory whole.
    Friend Shared Sub WriteReport(reportFile As String, runId As String, args As IList(Of String), verdict As String,
                                  exitCode As Integer, duration As TimeSpan, spoolPath As String, inMemoryOutput As String)
        Dim sensitive = IsSensitiveRun(args)
        Using w As New StreamWriter(reportFile, False, New UTF8Encoding(False))
            w.WriteLine("FileDO Run Report")
            w.WriteLine("Run ID: " & runId)
            w.WriteLine("Timestamp: " & DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss"))
            w.WriteLine("Command: " & ArgQuoting.JoinArgs(RedactCredentialArgs(args)))
            w.WriteLine("Verdict: " & verdict)
            w.WriteLine("Exit Code: " & exitCode.ToString())
            w.WriteLine("Duration: " & Ui.FormatDuration(duration))
            w.WriteLine(New String("-"c, 60))
            If sensitive Then
                w.WriteLine("(The output of this run is not kept: it can name the file a container keeps sealed.)")
                Return
            End If
            If Not String.IsNullOrEmpty(spoolPath) AndAlso File.Exists(spoolPath) Then
                Using r As New StreamReader(spoolPath, New UTF8Encoding(False))
                    Dim line = r.ReadLine()
                    While line IsNot Nothing
                        w.WriteLine(RedactReportOutput(line))
                        line = r.ReadLine()
                    End While
                End Using
            ElseIf inMemoryOutput IsNot Nothing Then
                w.Write(RedactReportOutput(inMemoryOutput))
            End If
        End Using
    End Sub

    ' ---- redaction (SHELL-01) -------------------------------------------------

    ' The CLI's redactCredentialArgs (cmd\filedo\fdsec_redact.go, with SP-0025 FDSEC-18), step for
    ' step: the run report repeats the command line, and history.json already redacts it, so the
    ' report must not be the one place a hand-typed password survives. Both are held to the same
    ' vectors, cmd\filedo\testdata\redaction\vectors.tsv - the Go test reads the file, and the
    ' self-test reads the copy embedded in this exe.
    Friend Shared ReadOnly FdsecFamilyTokens As String() = {
        "secure", "sec", "unsecure", "uns", "unsec", "reveal", "rev", "fdsec", "fds"}
    Friend Shared ReadOnly FdsecSubVerbs As String() = {"info", "verify", "register", "unregister"}
    ' The option words that survive: the parser's own vocabulary (fdsecOptionWords), so a word the
    ' parser would take as the password is never one kept here, and -all-users of (un)register.
    Friend Shared ReadOnly FdsecKeptWords As String() = {
        "del", "delete", "wipe", "rename", "ren", "here", "suite2", "start",
        "-rw", "rw", "-keep", "keep", "-y", "y", "--force", "force", "-all-users"}

    Private Shared Function IsOneOf(token As String, words As String()) As Boolean
        Return Array.IndexOf(words, token) >= 0
    End Function

    Friend Shared Function RedactCredentialArgs(args As IList(Of String)) As List(Of String)
        Dim out As New List(Of String)()
        If args Is Nothing Then Return out
        For Each a In args
            out.Add(If(a, ""))
        Next

        ' The scan starts at 0: a batch line can begin with its family token.
        Dim verbAt = -1
        For i = 0 To out.Count - 1
            If IsOneOf(out(i).ToLowerInvariant(), FdsecFamilyTokens) Then
                verbAt = i
                Exit For
            End If
        Next
        If verbAt < 0 Then Return out

        ' A sub-verb is one only right after fdsec: info and verify name the container next, and
        ' that path is kept. In the target-first form (a.txt secure verify) the parser takes the
        ' word as the password, so it is redacted like one.
        Dim start = verbAt + 1
        Dim fam = out(verbAt).ToLowerInvariant()
        If (fam = "fdsec" OrElse fam = "fds") AndAlso start < out.Count AndAlso IsOneOf(out(start).ToLowerInvariant(), FdsecSubVerbs) Then
            Dim sub_ = out(start).ToLowerInvariant()
            start += 1
            If (sub_ = "info" OrElse sub_ = "verify") AndAlso start < out.Count Then start += 1
        End If

        Dim keepNext = False ' set after "to": a path follows
        For i = start To out.Count - 1
            Dim t = out(i)
            Dim lt = t.ToLowerInvariant()
            If keepNext Then
                keepNext = False
                Continue For
            End If
            If lt = "to" Then
                keepNext = True
            ElseIf t.StartsWith("p:", StringComparison.Ordinal) Then
                out(i) = "p:***"
            ElseIf t.StartsWith("pf:", StringComparison.Ordinal) OrElse t.StartsWith("pe:", StringComparison.Ordinal) OrElse
                   t.StartsWith("k:", StringComparison.Ordinal) Then
                ' a path or a variable name: kept, it is not the secret
            ElseIf IsOneOf(lt, FdsecKeptWords) Then
                ' an option word
            Else
                out(i) = "***"
            End If
        Next
        Return out
    End Function

    ' True when what the run prints can name a container's sealed true name: a reveal, fdsec info
    ' or verify, an unsecure .. start - or a batch list holding one, or a batch list that cannot be
    ' read to say it does not.
    Friend Shared Function IsSensitiveRun(args As IList(Of String)) As Boolean
        Return IsSensitiveRun(args, 0)
    End Function

    Private Shared Function IsSensitiveRun(args As IList(Of String), depth As Integer) As Boolean
        If args Is Nothing Then Return False
        For i = 0 To args.Count - 1
            Select Case If(args(i), "").ToLowerInvariant()
                Case "reveal", "rev"
                    Return True
                Case "fdsec", "fds"
                    If i + 1 < args.Count Then
                        Dim sub_ = If(args(i + 1), "").ToLowerInvariant()
                        If sub_ = "info" OrElse sub_ = "verify" Then Return True
                    End If
                Case "unsecure", "uns", "unsec"
                    For j = i + 1 To args.Count - 1
                        If String.Equals(args(j), "start", StringComparison.OrdinalIgnoreCase) Then Return True
                    Next
                Case "from", "batch", "script"
                    If depth > 0 OrElse i + 1 >= args.Count Then Return True
                    If BatchIsSensitive(args(i + 1), depth) Then Return True
            End Select
        Next
        Return False
    End Function

    Private Shared Function BatchIsSensitive(listPath As String, depth As Integer) As Boolean
        Try
            Dim full = TargetPath.AsChildSeesIt(listPath)
            Dim info As New FileInfo(full)
            ' A list too big to read here is not read: it is simply treated as one that could reveal.
            If Not info.Exists OrElse info.Length > 4 * 1024 * 1024 Then Return True
            For Each raw In File.ReadAllLines(full)
                Dim line = raw.Trim()
                If line = "" OrElse line.StartsWith("#") Then Continue For
                Dim fields = line.Split(New Char() {" "c, ControlChars.Tab}, StringSplitOptions.RemoveEmptyEntries)
                If IsSensitiveRun(fields, depth + 1) Then Return True
            Next
            Return False
        Catch
            Return True
        End Try
    End Function

    ' Run reports are durable, unlike the output drawer.  Keep the useful
    ' outcome while removing the only part of a reveal path that the format
    ' deliberately keeps sealed.  Every line that names a copy names it inside
    ' a sandbox directory - rv-* for reveal (fdsec_reveal.go), us-* for
    ' unsecure start (fdsec_start.go) - so everything after that directory is
    ' cut to the end of the line: the copy's name, and on unsecure's "OK" line
    ' the true name printed again in the parentheses.  The sandbox directory
    ' itself is a random name and stays (SP-0005 section 12).
    Private Shared ReadOnly RevealCopyPattern As New Regex(
        "(\\FileDO\\reveal\\(?:rv|us)-[^\\\r\n]*\\)[^\r\n]+",
        RegexOptions.IgnoreCase Or RegexOptions.CultureInvariant)

    Friend Shared Function RedactReportOutput(log As String) As String
        If String.IsNullOrEmpty(log) Then Return log
        Return RevealCopyPattern.Replace(log, "$1<protected reveal copy>")
    End Function

    ' ---- seams for SelfTest.vb -------------------------------------------

    ' GUI-04 and GUI-06 without filedo.exe: a console program that reads stdin until it ends is
    ' started exactly the way a run is. It must end on its own because its stdin was closed, and it
    ' must be inside the window's job object while it runs.
    Friend Shared Sub RunChildForTest(fileName As String, arguments As String, timeoutMs As Integer,
                                      ByRef inJob As Boolean, ByRef exited As Boolean)
        inJob = False
        exited = False
        Using p As New Process With {.StartInfo = NewStartInfo(fileName, arguments, Path.GetTempPath(), False)}
            AddHandler p.OutputDataReceived, Sub(s, e)
                                             End Sub
            AddHandler p.ErrorDataReceived, Sub(s, e)
                                            End Sub
            p.Start()
            ChildJob.Assign(p)
            inJob = ChildJob.Contains(p)
            p.BeginOutputReadLine()
            p.BeginErrorReadLine()
            FeedAndCloseStdin(p, Nothing)
            exited = p.WaitForExit(timeoutMs)
            If Not exited Then
                Try
                    p.Kill()
                Catch
                End Try
            End If
        End Using
    End Sub

    Public Sub Dispose() Implements IDisposable.Dispose
        disposed = True
        If currentEventStream IsNot Nothing Then
            currentEventStream.Dispose()
            currentEventStream = Nothing
        End If
    End Sub

End Class

' What a run keeps of its output in memory (GUI-14): the first headChars characters and the most
' recent tailChars, with a line saying how many lines between them were let go. A run that prints a
' line per progress tick for hours cannot grow the window's memory without bound this way.
Friend Class BoundedText

    Private Const MaxLineChars As Integer = 64 * 1024

    Private ReadOnly headMax As Integer
    Private ReadOnly tailMax As Integer
    Private ReadOnly head As New StringBuilder()
    Private ReadOnly tail As New Queue(Of String)()
    Private tailChars As Long = 0
    Private dropped As Long = 0

    Public Sub New(headChars As Integer, tailChars As Integer)
        headMax = headChars
        tailMax = tailChars
    End Sub

    Public Sub AppendLine(line As String)
        If line Is Nothing Then line = ""
        If line.Length > MaxLineChars Then line = line.Substring(0, MaxLineChars) & " [..]"
        If tail.Count = 0 AndAlso head.Length + line.Length + 2 <= headMax Then
            head.AppendLine(line)
            Return
        End If
        tail.Enqueue(line)
        tailChars += line.Length + 2
        While tailChars > tailMax AndAlso tail.Count > 1
            Dim gone = tail.Dequeue()
            tailChars -= gone.Length + 2
            dropped += 1
        End While
    End Sub

    Public ReadOnly Property Length As Long
        Get
            Return head.Length + tailChars
        End Get
    End Property

    Public Overrides Function ToString() As String
        Dim sb As New StringBuilder(CInt(Math.Min(Integer.MaxValue, Length + 64)))
        sb.Append(head.ToString())
        If dropped > 0 Then sb.AppendLine("[.. " & dropped.ToString() & " lines not kept here ..]")
        For Each line In tail
            sb.AppendLine(line)
        Next
        Return sb.ToString()
    End Function

End Class

' The job object every non-elevated filedo.exe runs in (SP-0029 GUI-06).
'
' If the window dies - a crash, End task in Task Manager - the job's last handle closes with it and
' Windows ends every process still in the job: a hidden filedo.exe no longer waits forever holding a
' reveal's lock and its plaintext copy (the next start's sweep reclaims the sandbox). The job is one
' for the whole window and lives as long as it does.
'
' Only filedo.exe itself is in it. SILENT_BREAKAWAY_OK keeps whatever filedo.exe starts outside:
' the program a reveal opens the copy in is the user's Word or Acrobat, possibly holding other work,
' and it must never die because this window closed. An elevated run cannot be put in the job of a
' non-elevated window, and is not.
Friend Module ChildJob

    Private Const JobObjectExtendedLimitInformation As Integer = 9
    Private Const JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK As UInteger = &H1000UI
    Private Const JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE As UInteger = &H2000UI

    <StructLayout(LayoutKind.Sequential)>
    Private Structure JOBOBJECT_BASIC_LIMIT_INFORMATION
        Public PerProcessUserTimeLimit As Long
        Public PerJobUserTimeLimit As Long
        Public LimitFlags As UInteger
        Public MinimumWorkingSetSize As UIntPtr
        Public MaximumWorkingSetSize As UIntPtr
        Public ActiveProcessLimit As UInteger
        Public Affinity As UIntPtr
        Public PriorityClass As UInteger
        Public SchedulingClass As UInteger
    End Structure

    <StructLayout(LayoutKind.Sequential)>
    Private Structure IO_COUNTERS
        Public ReadOperationCount As ULong
        Public WriteOperationCount As ULong
        Public OtherOperationCount As ULong
        Public ReadTransferCount As ULong
        Public WriteTransferCount As ULong
        Public OtherTransferCount As ULong
    End Structure

    <StructLayout(LayoutKind.Sequential)>
    Private Structure JOBOBJECT_EXTENDED_LIMIT_INFORMATION
        Public BasicLimitInformation As JOBOBJECT_BASIC_LIMIT_INFORMATION
        Public IoInfo As IO_COUNTERS
        Public ProcessMemoryLimit As UIntPtr
        Public JobMemoryLimit As UIntPtr
        Public PeakProcessMemoryUsed As UIntPtr
        Public PeakJobMemoryUsed As UIntPtr
    End Structure

    <DllImport("kernel32.dll", CharSet:=CharSet.Unicode, SetLastError:=True)>
    Private Function CreateJobObject(attributes As IntPtr, name As String) As IntPtr
    End Function

    <DllImport("kernel32.dll", SetLastError:=True)>
    Private Function SetInformationJobObject(job As IntPtr, infoClass As Integer,
                                             ByRef info As JOBOBJECT_EXTENDED_LIMIT_INFORMATION, size As UInteger) As Boolean
    End Function

    <DllImport("kernel32.dll", SetLastError:=True)>
    Private Function AssignProcessToJobObject(job As IntPtr, process As IntPtr) As Boolean
    End Function

    <DllImport("kernel32.dll", SetLastError:=True)>
    Private Function IsProcessInJob(process As IntPtr, job As IntPtr, ByRef result As Boolean) As Boolean
    End Function

    Private ReadOnly gate As New Object()
    Private jobHandle As IntPtr = IntPtr.Zero
    Private creationFailed As Boolean = False

    ' The window's job, created on first use. Its handle is never closed: closing is what ends the
    ' children, and that is for the process's own exit to do.
    Private Function Job() As IntPtr
        SyncLock gate
            If jobHandle <> IntPtr.Zero OrElse creationFailed Then Return jobHandle
            Try
                Dim h = CreateJobObject(IntPtr.Zero, Nothing)
                If h = IntPtr.Zero Then
                    creationFailed = True
                    ShellLog.Info("job object: CreateJobObject failed, error " & Marshal.GetLastWin32Error().ToString())
                    Return IntPtr.Zero
                End If
                Dim info As New JOBOBJECT_EXTENDED_LIMIT_INFORMATION()
                info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE Or JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK
                If Not SetInformationJobObject(h, JobObjectExtendedLimitInformation, info,
                                               CUInt(Marshal.SizeOf(GetType(JOBOBJECT_EXTENDED_LIMIT_INFORMATION)))) Then
                    creationFailed = True
                    ShellLog.Info("job object: SetInformationJobObject failed, error " & Marshal.GetLastWin32Error().ToString())
                    Return IntPtr.Zero
                End If
                jobHandle = h
            Catch ex As Exception
                creationFailed = True
                ShellLog.Write("create the job object", ex)
            End Try
            Return jobHandle
        End SyncLock
    End Function

    ' Puts a started child in the job. A failure is logged and the run goes on: the child then only
    ' lacks the guarantee, exactly as every run did before the job existed.
    Public Sub Assign(p As Process)
        Try
            Dim h = Job()
            If h = IntPtr.Zero OrElse p Is Nothing OrElse p.HasExited Then Return
            If Not AssignProcessToJobObject(h, p.Handle) Then
                ShellLog.Info("job object: AssignProcessToJobObject failed, error " & Marshal.GetLastWin32Error().ToString())
            End If
        Catch ex As Exception
            ShellLog.Write("put filedo.exe in the job object", ex)
        End Try
    End Sub

    Public Function Contains(p As Process) As Boolean
        Try
            Dim h = Job()
            If h = IntPtr.Zero OrElse p Is Nothing Then Return False
            Dim result As Boolean = False
            If Not IsProcessInJob(p.Handle, h, result) Then Return False
            Return result
        Catch
            Return False
        End Try
    End Function

End Module
