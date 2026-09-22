Imports System.IO
Imports System.Text

' `filedo_win.exe --selftest` - the shell's own gate.
'
' The window has no console and no test runner, so until now the only way to find out that a page
' threw on its way up was to click it. That is a poor gate for a page whose whole job is to write
' a command line correctly, and it is why ArgQuotingTests sat in the project unreferenced.
'
' The run builds every job page the catalogue knows, reads back the command each one would run,
' and checks the shape against what the CLI parses (cmd\filedo\main.go, command_handlers.go).
' It writes a line per check to filedo_win_selftest.log next to the exe and exits 0 or 1, so
' build.ps1 can treat it the way it treats `go test`.
Public Module SelfTest

    Private ReadOnly report As New StringBuilder()
    Private failures As Integer = 0

    Public Function Run() As Integer
        report.Clear()
        failures = 0

        Check("ArgQuoting", ArgQuotingTests.RunTests())
        CheckCatalogue()
        CheckJobPages()
        CheckExpertPage()
        CheckVerdictTable()
        CheckEventStreamTailer()

        Dim logFile As String = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "filedo_win_selftest.log")
        Try
            File.WriteAllText(logFile, report.ToString())
        Catch
        End Try

        Return If(failures = 0, 0, 1)
    End Function

    Private Sub Check(name As String, ok As Boolean, Optional detail As String = "")
        If Not ok Then failures += 1
        report.AppendLine(If(ok, "PASS ", "FAIL ") & name & If(detail = "", "", " - " & detail))
    End Sub

    ' Every rail row that leads to a job page must have a catalogue entry, and every catalogue
    ' entry must have a label, a purpose and a verb. A missing key shows up as the key itself on
    ' screen, in one locale, on one page - exactly the kind of thing nobody clicks through to.
    Private Sub CheckCatalogue()
        Dim dict = Localization.GetDict("en")
        For Each job In JobCatalogue.GetAllJobs()
            Check("catalogue:" & job.Id & ":verb", Not String.IsNullOrEmpty(job.DefaultVerb))
            Check("catalogue:" & job.Id & ":label", dict.ContainsKey(job.LabelKey))
            Check("catalogue:" & job.Id & ":purpose", dict.ContainsKey(job.PurposeKey))
        Next
    End Sub

    ' Each page is built, given a target, and asked what it would run. The command has to start
    ' with filedo.exe, name the job's verb, and carry the target - the three things that are
    ' wrong when a page has been given a control the command builder does not know about.
    Private Sub CheckJobPages()
        For Each job In JobCatalogue.GetAllJobs()
            Dim view As JobView = Nothing
            Try
                view = New JobView()
                view.SetJob(job)
                view.SetTarget(SampleTargetFor(job))

                Dim cmd = view.CurrentCommand()
                Check("page:" & job.Id & ":builds", cmd.StartsWith("filedo.exe "), cmd)
                Check("page:" & job.Id & ":verb", CommandNames(cmd, job.DefaultVerb), cmd)
                Check("page:" & job.Id & ":target", cmd.Contains(SampleTargetFor(job)), cmd)
            Catch ex As Exception
                Check("page:" & job.Id, False, ex.GetType().Name & ": " & ex.Message)
            Finally
                If view IsNot Nothing Then view.Dispose()
            End Try
        Next
    End Sub

    ' The expert page: every operation in its dropdown has to write a command and explain itself.
    ' An explanation that comes back as its own key - "op_probe" rather than a sentence - is the
    ' failure this catches, and it is the one that used to reach users: the page shipped with ten
    ' operations while the other thirteen sat translated and unreachable.
    Private Sub CheckExpertPage()
        Dim view As CommandView = Nothing
        Try
            view = New CommandView()
            Check("expert:operations", view.OperationCount() >= 20, view.OperationCount().ToString())

            ' A password no user would type, so that finding it in a command line is unambiguous.
            Const secret As String = "selftest-pa55phrase-not-a-real-one"
            view.SetCredentialForTest(secret)

            For i = 0 To view.OperationCount() - 1
                Dim op = view.SelectOperation(i)
                view.SetCredentialForTest(secret)
                Dim cmd = view.CurrentCommandLine()
                Dim hint = view.CurrentHint()
                Check("expert:" & op & ":builds", cmd.StartsWith("filedo.exe "), cmd)
                Check("expert:" & op & ":names", cmd.Contains(op.Split(" "c)(0)), cmd)
                Check("expert:" & op & ":explained", Not hint.StartsWith("op_"), hint)

                ' The rule this page lives by: a password is never on the line, and the five
                ' verbs that need one name the variable it travels in instead.
                Check("expert:" & op & ":no-secret", Not cmd.Contains(secret), cmd)
                If IsCredentialOp(op) Then
                    Check("expert:" & op & ":by-variable", cmd.Contains("pe:FILEDO_SHELL_CRED"), cmd)
                End If
            Next
        Catch ex As Exception
            Check("expert", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If view IsNot Nothing Then view.Dispose()
        End Try
    End Sub

    ' Rung 2 of the CLI-EVENT-STREAM conformance ladder: every (verdict, exit
    ' code, stop-file, channel version) combination the shell can meet, and the
    ' verdict it must produce. Both ways of reaching *Not proven* are here,
    ' because inferring success from a process that merely exited is the one
    ' failure this channel exists to prevent.
    Private Sub CheckVerdictTable()
        Dim cases = New Object()() {
            New Object() {"a run that passed and said so", 0, "Passed", False, 0, "Passed"},
            New Object() {"a run that acted and said so", 0, "Done", False, 0, "Done"},
            New Object() {"a run that failed and said so", 1, "Failed", False, 0, "Failed"},
            New Object() {"a run that could not judge", 2, "Not proven", False, 0, "Not proven"},
            New Object() {"an older build that wrote no result", 0, Nothing, False, 0, "Not proven"},
            New Object() {"a result that disagrees with the code", 0, "Failed", False, 0, "Not proven"},
            New Object() {"the stop file this shell created", 0, "Passed", True, 0, "Stopped"},
            New Object() {"a stop with nothing written", 0, Nothing, True, 0, "Stopped"},
            New Object() {"a verdict word this build does not know", 0, "Quarantined", False, 0, "Not proven"},
            New Object() {"a container class, not a supervisor code", 3, "Failed", False, 0, "Failed"},
            New Object() {"a channel from a newer FileDO", 0, "Passed", False, 2, "Not proven"}
        }

        For Each c In cases
            Dim label = DirectCast(c(0), String)
            Dim code = CInt(c(1))
            Dim info As EventStream.ResultInfo = Nothing
            If c(2) IsNot Nothing Then
                info = New EventStream.ResultInfo With {.Verdict = DirectCast(c(2), String)}
            End If
            Dim reason As String = ""
            Dim got = Runner.Judge(code, info, CBool(c(3)), CInt(c(4)), reason)
            Check("verdict:" & label, got = DirectCast(c(5), String), got & " (reason " & reason & ")")
        Next
    End Sub

    ' Rung 3 of the same ladder, plus the two ways a line can be lost. A reader
    ' that consumes an incomplete final line drops exactly one event, at
    ' random, under load - and the event most likely to be half-written is the
    ' last one, which is the one carrying the verdict.
    Private Sub CheckEventStreamTailer()
        Dim eventsPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_events_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            File.WriteAllText(eventsPath, "")

            ' Half a line, then a poll: nothing may be dispatched and nothing
            ' may be consumed.
            Dim results As New List(Of String)()
            Dim refused As Integer = 0
            Using stream As New EventStream(eventsPath)
                AddHandler stream.ResultReceived, Sub(r) results.Add(r.Verdict)
                AddHandler stream.UnsupportedVersion, Sub(v) refused = v

                Const head As String = "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""2026-09-22T14:31:07.418+02:00"",""data"":{""verd"
                Const tail As String = "ict"":""Passed""}}" & vbLf
                AppendText(eventsPath, head)
                stream.Poll()
                Check("tailer:partial-line-not-consumed", results.Count = 0, results.Count.ToString())

                ' The other half arrives: the event must now be read whole.
                AppendText(eventsPath, tail)
                stream.Poll()
                Check("tailer:line-completed", results.Count = 1 AndAlso results(0) = "Passed",
                      String.Join(",", results.ToArray()))

                ' A timestamp nobody can parse costs the field, never the
                ' event that carried it.
                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""not-a-date"",""data"":{""verdict"":""Failed""}}" & vbLf)
                stream.Poll()
                Check("tailer:bad-timestamp-keeps-event", results.Count = 2 AndAlso results(1) = "Failed",
                      String.Join(",", results.ToArray()))

                ' A newer MAJOR stops interpretation and does not resume.
                AppendText(eventsPath, "{""schemaVersion"":2,""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:newer-major-refused", refused = 2, refused.ToString())
                Check("tailer:newer-major-not-interpreted", results.Count = 2, results.Count.ToString())

                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:refusal-is-final", results.Count = 2, results.Count.ToString())
            End Using
        Catch ex As Exception
            Check("tailer", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Try
                File.Delete(eventsPath)
            Catch
            End Try
        End Try
    End Sub

    Private Sub AppendText(path As String, text As String)
        Using fs As New FileStream(path, FileMode.Append, FileAccess.Write, FileShare.ReadWrite)
            Dim bytes = Encoding.UTF8.GetBytes(text)
            fs.Write(bytes, 0, bytes.Length)
        End Using
    End Sub

    Private Function IsCredentialOp(op As String) As Boolean
        Return Array.IndexOf(New String() {"secure", "unsecure", "reveal",
                                           "fdsec info", "fdsec verify"}, op) >= 0
    End Function

    ' `info` is written as `short` when the brief report is asked for, and copy is written as
    ' whichever strategy is chosen - so the check is "names the verb or one of its faces".
    Private Function CommandNames(cmd As String, verb As String) As Boolean
        If cmd.Contains(" " & verb) Then Return True
        If verb = "info" AndAlso cmd.Contains(" short") Then Return True
        If verb = "copy" Then
            For Each v In CliRules.CopyVerbs
                If cmd.Contains(" " & v & " ") Then Return True
            Next
        End If
        Return False
    End Function

    Private Function SampleTargetFor(job As JobDefinition) As String
        Select Case job.TargetKind
            Case JobDefinition.TargetType.File : Return "C:\sample\one.fd-sec"
            Case JobDefinition.TargetType.Drive : Return "E:"
            Case Else : Return "C:\sample"
        End Select
    End Function

End Module
