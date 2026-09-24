Imports System.IO
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

    Private currentProcess As Process
    Private currentStopFile As String
    Private currentEventStream As EventStream
    Private isRunning As Boolean = False
    Private readonly outputBuilder As New System.Text.StringBuilder()
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

    Public Shared Function GetRunsDir() As String
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

    ' The stated retention of section 7.3 and D8, swept on start.
    Public Shared Sub SweepOldRuns()
        SweepDirectory(GetRunsDir(), RunFileRetentionDays)
        SweepDirectory(GetReportsDir(), ReportRetentionDays)
    End Sub

    Private Shared Sub SweepDirectory(dir As String, days As Integer)
        Try
            Dim cutoff = DateTime.Now.AddDays(-days)
            For Each f In Directory.GetFiles(dir, "*.*")
                If File.GetLastWriteTime(f) < cutoff Then
                    Try
                        File.Delete(f)
                    Catch
                    End Try
                End If
            Next
        Catch
        End Try
    End Sub

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

    Public Async Function ExecuteAsync(args As IEnumerable(Of String),
                                         Optional envVars As Dictionary(Of String, String) = Nothing,
                                         Optional stdinInput As String = Nothing,
                                         Optional elevate As Boolean = False) As Task(Of RunResult)
        If isRunning Then
            Throw New InvalidOperationException("A command is already running.")
        End If

        isRunning = True
        outputBuilder.Clear()
        latestResultInfo = Nothing

        Dim runId = DateTime.Now.ToString("yyyyMMdd_HHmmss") & "_" & Guid.NewGuid().ToString("N").Substring(0, 6)
        Dim runsDir = GetRunsDir()
        Dim eventPath = Path.Combine(runsDir, runId & ".jsonl")
        currentStopFile = Path.Combine(runsDir, runId & ".stop")

        ' Prepend machine channel and stop file parameters
        Dim fullArgs As New List(Of String)()
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

        Dim cliPath = LocateCLI()
        Dim workingDir = GetAppDataDir()
        Dim commandLine = ArgQuoting.JoinArgs(fullArgs)

        Dim psi As New ProcessStartInfo() With {
            .FileName = cliPath,
            .Arguments = commandLine,
            .WorkingDirectory = workingDir,
            .UseShellExecute = False,
            .CreateNoWindow = True,
            .RedirectStandardOutput = True,
            .RedirectStandardError = True,
            .RedirectStandardInput = (stdinInput IsNot Nothing)
        }

        If elevate Then
            ' If elevation is requested, UseShellExecute = True is required on Windows
            psi.UseShellExecute = True
            psi.Verb = "runas"
            psi.RedirectStandardOutput = False
            psi.RedirectStandardError = False
            psi.RedirectStandardInput = False
        End If

        If envVars IsNot Nothing AndAlso Not psi.UseShellExecute Then
            For Each kvp In envVars
                psi.EnvironmentVariables(kvp.Key) = kvp.Value
            Next
        End If

        Dim startTime = DateTime.Now

        Return Await Task.Run(
            Function() As RunResult
                Try
                    currentProcess = New Process With {.StartInfo = psi, .EnableRaisingEvents = True}

                    If Not psi.UseShellExecute Then
                        AddHandler currentProcess.OutputDataReceived,
                            Sub(sender, e)
                                If e.Data IsNot Nothing Then
                                    SyncLock outputBuilder
                                        outputBuilder.AppendLine(e.Data)
                                    End SyncLock
                                    RaiseEvent OutputLineReceived(e.Data, False)
                                End If
                            End Sub

                        AddHandler currentProcess.ErrorDataReceived,
                            Sub(sender, e)
                                If e.Data IsNot Nothing Then
                                    SyncLock outputBuilder
                                        outputBuilder.AppendLine(e.Data)
                                    End SyncLock
                                    RaiseEvent OutputLineReceived(e.Data, True)
                                End If
                            End Sub
                    End If

                    currentProcess.Start()

                    If Not psi.UseShellExecute Then
                        currentProcess.BeginOutputReadLine()
                        currentProcess.BeginErrorReadLine()

                        If stdinInput IsNot Nothing Then
                            Using writer = currentProcess.StandardInput
                                writer.Write(stdinInput)
                            End Using
                        End If
                    End If

                    ' Poll event stream in loop until process exits
                    While Not currentProcess.WaitForExit(100)
                        If currentEventStream IsNot Nothing Then
                            currentEventStream.Poll()
                        End If
                    End While

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
                    SaveReport(runId, commandLine, verdict, exitCode, duration, outputBuilder.ToString(), latestResultInfo)

                    Return New RunResult With {
                        .ExitCode = exitCode,
                        .Verdict = verdict,
                        .Reason = reason,
                        .Output = outputBuilder.ToString(),
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
                        .Output = outputBuilder.ToString(),
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
                        .Output = outputBuilder.ToString(),
                        .Duration = DateTime.Now - startTime,
                        .ResultInfo = Nothing,
                        .TimedOut = False
                    }
                Finally
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
    Private Sub SaveReport(runId As String, cmd As String, verdict As String, exitCode As Integer, duration As TimeSpan, log As String, resultInfo As EventStream.ResultInfo)
        If Not ShellSettings.HistoryEnabled() Then Return
        Try
            Dim reportsDir = GetReportsDir()
            Dim reportFile = Path.Combine(reportsDir, "report_" & runId & ".log")
            Dim sb As New System.Text.StringBuilder()
            sb.AppendLine("FileDO Run Report")
            sb.AppendLine("Run ID: " & runId)
            sb.AppendLine("Timestamp: " & DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss"))
            sb.AppendLine("Command: " & cmd)
            sb.AppendLine("Verdict: " & verdict)
            sb.AppendLine("Exit Code: " & exitCode.ToString())
            sb.AppendLine("Duration: " & duration.ToString("mm\:ss"))
            sb.AppendLine(New String("-"c, 60))
            sb.AppendLine(log)
            File.WriteAllText(reportFile, sb.ToString())
        Catch ex As Exception
            ShellLog.Write("save the run report", ex)
        End Try
    End Sub

    Public Sub Dispose() Implements IDisposable.Dispose
        disposed = True
        If currentEventStream IsNot Nothing Then
            currentEventStream.Dispose()
            currentEventStream = Nothing
        End If
    End Sub

End Class
