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
' It writes a line per check to filedo_win_selftest.log next to the exe, ends the log with a
' `selftest: PASS (n)` or `selftest: FAIL (n): names` line, and exits 0 or 1 - or 2 when the log
' cannot be written, because a verdict nobody can read proves nothing.
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
        CheckRailTargets()
        CheckVerdictTable()
        CheckVerdictGlyphs()
        CheckEventStreamTailer()
        CheckEventStreamSample()

        ' SP-0014: the shared desktop contracts APP-BEHAVIOUR and APP-STYLE, rung by rung.
        CheckPaletteCompleteness()
        CheckThemeRoundTrip()
        CheckRailPaint()
        CheckRailLabels()
        CheckProgressRule()
        CheckLocalizedFormat()
        CheckAccessibleNames()
        CheckPlacement()
        CheckWipeRules()
        CheckDialogEscape()

        Dim logFile As String = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "filedo_win_selftest.log")
        Dim verdict As String = If(failures = 0,
                                   "selftest: PASS (" & CountChecks().ToString() & ")",
                                   "selftest: FAIL (" & failures.ToString() & "): " & FailureNames())
        report.AppendLine(verdict)
        Try
            File.WriteAllText(logFile, report.ToString())
        Catch ex As Exception
            Console.Error.WriteLine("selftest: NOT VERIFIED (cannot write " & logFile & ": " & ex.Message & ")")
            Return 2
        End Try

        Return If(failures = 0, 0, 1)
    End Function

    Private Sub Check(name As String, ok As Boolean, Optional detail As String = "")
        If Not ok Then failures += 1
        report.AppendLine(If(ok, "PASS ", "FAIL ") & name & If(detail = "", "", " - " & detail))
    End Sub

    Private Function CountChecks() As Integer
        Return report.ToString().Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries).
            Count(Function(line) line.StartsWith("PASS ") OrElse line.StartsWith("FAIL "))
    End Function

    Private Function FailureNames() As String
        Dim names As New List(Of String)()
        For Each line In report.ToString().Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries)
            If line.StartsWith("FAIL ") Then names.Add(line.Substring(5).Split(" "c)(0))
        Next
        Return String.Join(", ", names.ToArray())
    End Function

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

    ' SP-0016 T4: both the group headers and the selectable rows use the Windows 44 px target.
    ' ShellForm applies this design-pixel value through Ui.Px, so it scales together with the form.
    Private Sub CheckRailTargets()
        Check("rail:target-height", ShellForm.RailTargetHeight >= 44, ShellForm.RailTargetHeight.ToString())
    End Sub

    ' Rung 2 of the CLI-EVENT-STREAM conformance ladder: every (verdict, exit
    ' code, stop-file, channel version) combination the shell can meet, and the
    ' verdict it must produce. Both ways of reaching *Not proven* are here,
    ' because inferring success from a process that merely exited is the one
    ' failure this channel exists to prevent.
    Private Sub CheckVerdictTable()
        Dim words = New String() {"Passed", "Failed", "Done", "Stopped", "Not proven"}
        For Each word In words
            For Each code In New Integer() {0, 1, 2}
                For Each stopped In New Boolean() {False, True}
                    CheckVerdictCase(word & "/" & code.ToString() & "/stop=" & stopped.ToString(), code, word, stopped, 0)
                Next
            Next
        Next

        ' The values outside the 30 normal triples name the degradation cases
        ' in the rule text: no result, unknown token, foreign code and refusal.
        CheckVerdictCase("no-result", 0, Nothing, False, 0)
        CheckVerdictCase("unknown-word", 0, "Quarantined", False, 0)
        CheckVerdictCase("wrong-case", 0, "passed", False, 0)
        CheckVerdictCase("foreign-code-3", 3, "Failed", False, 0)
        CheckVerdictCase("foreign-code-minus-1", -1, "Passed", False, 0)
        CheckVerdictCase("refused-version", 0, "Passed", False, EventStream.KnownSchemaVersion + 1)
    End Sub

    ' The expected verdict is the contract text in executable form, used for
    ' every row rather than a hand-picked subset of the possible triples.
    Private Function ExpectedVerdict(word As String, code As Integer, stopped As Boolean, refusedVersion As Integer) As String
        If refusedVersion > EventStream.KnownSchemaVersion Then Return "Not proven"
        If String.IsNullOrEmpty(word) Then Return If(stopped, "Stopped", "Not proven")
        If stopped Then Return "Stopped"
        Dim expected As Integer
        Select Case word
            Case "Passed", "Done" : expected = 0
            Case "Failed" : expected = 1
            Case "Not proven" : expected = 2
            Case "Stopped" : Return "Stopped"
            Case Else : Return "Not proven"
        End Select
        If code < 0 OrElse code > 2 Then Return word
        Return If(code = expected, word, "Not proven")
    End Function

    Private Sub CheckVerdictCase(label As String, code As Integer, word As String, stopped As Boolean, refusedVersion As Integer)
        Dim info As EventStream.ResultInfo = Nothing
        If word IsNot Nothing Then info = New EventStream.ResultInfo With {.Verdict = word}
        Dim reason As String = ""
        Dim got = Runner.Judge(code, info, stopped, refusedVersion, reason)
        Dim want = ExpectedVerdict(word, code, stopped, refusedVersion)
        Check("verdict:" & label, got = want, got & " (want " & want & ", reason " & reason & ")")
    End Sub

    ' ICON-SET / ICON-RENDER (SP-0016 T2): each verdict of CLI-EVENT-STREAM, plus one this build
    ' does not know, draws its state glyph in its state colour. The plain check (E73E, the
    ' vocabulary's action.confirm) and the plain cross (E711, nav.close) are the two pictures the
    ' vocabulary lists as distinct from status.ok and status.error, so they must never come back.
    Private Sub CheckVerdictGlyphs()
        Dim p = Theme.Current
        Dim cases = New Object()() {
            New Object() {"Passed", &HEC61, p.Success},
            New Object() {"Done", &HEC61, p.Success},
            New Object() {"Failed", &HE783, p.Danger},
            New Object() {"Stopped", &HE71A, p.Warning},
            New Object() {"Not proven", &HE9CE, p.MutedText},
            New Object() {"Quarantined", &HE9CE, p.MutedText}
        }
        For Each c In cases
            Dim verdict = DirectCast(c(0), String)
            Dim glyph = Theme.VerdictGlyph(verdict)
            Dim code = AscW(glyph(0)) And &HFFFF
            Check("glyph:" & verdict & ":codepoint", code = CInt(c(1)), code.ToString("X4"))
            Check("glyph:" & verdict & ":not-confirm-or-close", code <> &HE73E AndAlso code <> &HE711, code.ToString("X4"))
            Dim want = DirectCast(c(2), Color)
            Dim got = Theme.VerdictColor(verdict, p)
            Check("glyph:" & verdict & ":colour", got.ToArgb() = want.ToArgb(), got.ToString())
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
            Dim received As New List(Of String)()
            Dim refused As Integer = 0
            Using stream As New EventStream(eventsPath)
                AddHandler stream.EventReceived, Sub(e) received.Add(e.Kind)
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

                ' Omitted carrier defaults to MAJOR 1 for this document.
                AppendText(eventsPath, "{""kind"":""result"",""data"":{""verdict"":""Done""}}" & vbLf)
                stream.Poll()
                Check("tailer:absent-carrier-is-v1", results.Count = 3 AndAlso results(2) = "Done",
                      String.Join(",", results.ToArray()))

                ' Case variants and unknown kinds are delivered as raw events
                ' only; neither is dispatched as a result (rule 8).
                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""RESULT"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:kind-is-case-sensitive", results.Count = 3 AndAlso received.Contains("RESULT"),
                      String.Join(",", received.ToArray()))

                ' A newer MAJOR stops interpretation and does not resume.
                AppendText(eventsPath, "{""schemaVersion"":2,""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:newer-major-refused", refused = 2, refused.ToString())
                Check("tailer:newer-major-not-interpreted", results.Count = 3, results.Count.ToString())

                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:refusal-is-final", results.Count = 3, results.Count.ToString())
            End Using

            ' A present carrier of the wrong JSON type is a refusal, not an
            ' implicit v1. It needs a fresh tailer because refusal is final.
            Dim garbledPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_garbled_" & Guid.NewGuid().ToString("N") & ".jsonl")
            Try
                File.WriteAllText(garbledPath, "{""schemaVersion"":""two"",""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                Dim garbledRefused As Integer = 0
                Dim garbledResults As Integer = 0
                Using stream As New EventStream(garbledPath)
                    AddHandler stream.UnsupportedVersion, Sub(v) garbledRefused = v
                    AddHandler stream.ResultReceived, Sub(r) garbledResults += 1
                    stream.Poll()
                End Using
                Check("tailer:garbled-carrier-refused", garbledRefused > EventStream.KnownSchemaVersion AndAlso garbledResults = 0,
                      garbledRefused.ToString() & "/" & garbledResults.ToString())
            Finally
                Try
                    File.Delete(garbledPath)
                Catch
                End Try
            End Try
        Catch ex As Exception
            Check("tailer", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Try
                File.Delete(eventsPath)
            Catch
            End Try
        End Try
    End Sub

    ' Rung 1: the checked-in producer-generated sample is linked as a resource
    ' so the consumer proves it can read exactly the bytes another implementation
    ' would receive, not a hand-written approximation of them.
    Private Sub CheckEventStreamSample()
        Dim tempPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_sample_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            Using source = GetType(SelfTest).Assembly.GetManifestResourceStream("FileDOGUI.events.sample-v1.jsonl")
                If source Is Nothing Then
                    Check("sample:resource", False, "missing FileDOGUI.events.sample-v1.jsonl")
                    Return
                End If
                Using destination As New FileStream(tempPath, FileMode.Create, FileAccess.Write)
                    source.CopyTo(destination)
                End Using
            End Using
            Dim kinds As New List(Of String)()
            Dim stepName As String = ""
            Dim progressItems As Long = 0
            Dim findingMessage As String = ""
            Dim noteMessage As String = ""
            Dim verdict As String = ""
            Using stream As New EventStream(tempPath)
                AddHandler stream.EventReceived, Sub(e) kinds.Add(e.Kind)
                AddHandler stream.StepChanged, Sub(n, d) stepName = n
                AddHandler stream.ProgressReported, Sub(p) progressItems = p.DoneItems
                AddHandler stream.FindingReported, Sub(t, m, d) findingMessage = m
                AddHandler stream.NoteReported, Sub(m) noteMessage = m
                AddHandler stream.ResultReceived, Sub(r) verdict = r.Verdict
                stream.Poll()
            End Using
            Dim want = New String() {"run", "step", "progress", "finding", "note", "result"}
            Check("sample:all-kinds", kinds.SequenceEqual(want), String.Join(",", kinds.ToArray()))
            Check("sample:fields", stepName = "write" AndAlso progressItems = 1 AndAlso findingMessage = "An example finding" AndAlso noteMessage = "An example note" AndAlso verdict = "Passed")
        Catch ex As Exception
            Check("sample", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Try
                File.Delete(tempPath)
            Catch
            End Try
        End Try
    End Sub

    ' ---- SP-0014: APP-STYLE --------------------------------------------------

    ' APP-STYLE rung 2: both palettes give every role a colour. A role left empty paints as black
    ' or transparent in exactly one theme, which is the kind of thing nobody finds by looking.
    Private Sub CheckPaletteCompleteness()
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim name = If(dark, "dark", "light")
            For Each prop In GetType(Theme.Palette).GetProperties()
                If prop.PropertyType IsNot GetType(Color) Then Continue For
                Dim c = DirectCast(prop.GetValue(p), Color)
                Check("palette:" & name & ":" & prop.Name, Not c.IsEmpty AndAlso c.A = 255, c.ToString())
            Next
        Next
    End Sub

    ' APP-STYLE section 3, the defect class "a reference resolved once at load": a Failed result on
    ' screen under one palette, the other palette applied, and the badge must carry the new
    ' palette's Danger. The palette is switched through Theme's test seam, never through HKCU.
    Private Sub CheckThemeRoundTrip()
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Try
            Theme.UsePaletteForTest(False)
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            jv.ShowResultForTest("Failed")
            cv = New CommandView()
            cv.ShowVerdictForTest("Failed")
            Dim light = Theme.PaletteFor(False)
            Check("theme:job-badge-light", jv.VerdictBadgeForTest.BackColor.ToArgb() = light.Danger.ToArgb(),
                  jv.VerdictBadgeForTest.BackColor.ToString())

            Theme.UsePaletteForTest(True)
            jv.ApplyTheme()
            cv.ApplyTheme()
            Dim dark = Theme.PaletteFor(True)
            Check("theme:job-badge-follows", jv.VerdictBadgeForTest.BackColor.ToArgb() = dark.Danger.ToArgb(),
                  jv.VerdictBadgeForTest.BackColor.ToString())
            Check("theme:command-verdict-follows", cv.VerdictLabelForTest.ForeColor.ToArgb() = dark.Danger.ToArgb(),
                  cv.VerdictLabelForTest.ForeColor.ToString())
        Catch ex As Exception
            Check("theme", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Theme.UsePaletteForTest(Nothing)
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' ---- SP-0014: the rail ----------------------------------------------------

    ' T1: every kind of rail row, in every state it can be drawn in, painted under both palettes.
    ' A paint that throws is what WinForms turns into the red-cross placeholder; a pure-red pixel
    ' in the bitmap is that placeholder, since no palette role is pure red.
    Private Sub CheckRailPaint()
        Dim dict = Localization.GetDict("en")
        For Each dark In New Boolean() {False, True}
            Theme.UsePaletteForTest(dark)
            Dim themeName = If(dark, "dark", "light")
            Try
                For Each row In RailRow.All
                    For Each stateName In New String() {"plain", "hover", "selected", "collapsed-holding"}
                        If stateName = "collapsed-holding" AndAlso Not row.IsGroup Then Continue For
                        If stateName = "selected" AndAlso row.IsGroup Then Continue For
                        Using e As New RailEntry With {
                            .Key = row.Key,
                            .Glyph = row.Glyph,
                            .IsGroupHeader = row.IsGroup,
                            .Text = If(row.IsGroup, dict(row.Key).ToUpperInvariant(), dict(row.Key)),
                            .RowUnit = 44,
                            .Size = New Size(236, 44)
                        }
                            If stateName = "selected" Then e.Selected = True
                            If stateName = "collapsed-holding" Then
                                e.Collapsed = True
                                e.HasSelectedChild = True
                            End If
                            e.Height = e.PreferredRowHeight(e.Width)
                            Dim label = "rail-paint:" & themeName & ":" & row.Key & ":" & stateName
                            Try
                                Using bmp As New Bitmap(e.Width, e.Height)
                                    Using g = Graphics.FromImage(bmp)
                                        e.PaintForTest(g, stateName = "hover")
                                    End Using
                                    Check(label, CountPureRed(bmp) = 0, "pure red in the bitmap")
                                End Using
                            Catch ex As Exception
                                Check(label, False, ex.GetType().Name & ": " & ex.Message)
                            End Try
                        End Using
                    Next
                Next
            Finally
                Theme.UsePaletteForTest(Nothing)
            End Try
        Next
    End Sub

    Private Function CountPureRed(bmp As Bitmap) As Integer
        Dim n = 0
        For y = 0 To bmp.Height - 1
            For x = 0 To bmp.Width - 1
                Dim c = bmp.GetPixel(x, y)
                If c.R = 255 AndAlso c.G = 0 AndAlso c.B = 0 Then n += 1
            Next
        Next
        Return n
    End Function

    ' T4, APP-BEHAVIOUR rule 2: every rail label, in all five languages, fits the rectangle its row
    ' draws it into - wrapped where it has to be, and with no single word wider than the label. The
    ' width is the narrowest the rail gives a row (its column less the margins and a scroll bar),
    ' at this machine's DPI, because the text is measured at this machine's DPI too.
    Private Sub CheckRailLabels()
        Dim scale As Single = 1.0F
        Try
            Using g = Graphics.FromHwnd(IntPtr.Zero)
                scale = g.DpiX / 96.0F
            End Using
        Catch
        End Try
        Dim rowWidth = CInt((268 - 26) * scale) - SystemInformation.VerticalScrollBarWidth
        Dim unit = CInt(ShellForm.RailTargetHeight * scale)

        For Each lang In Localization.Languages
            Dim dict = Localization.GetDict(lang)
            For Each row In RailRow.All
                Using e As New RailEntry With {
                    .Key = row.Key,
                    .IsGroupHeader = row.IsGroup,
                    .Glyph = row.Glyph,
                    .RowUnit = unit,
                    .Text = If(row.IsGroup, dict(row.Key).ToUpperInvariant(), dict(row.Key))
                }
                    Dim detail As String = ""
                    Dim fits = e.LabelFits(rowWidth, detail)
                    Check("rail-label:" & lang & ":" & row.Key, fits, detail)
                End Using
            Next
        Next
    End Sub

    ' ---- SP-0014: APP-BEHAVIOUR ----------------------------------------------

    ' T6, rule 3: both run pages draw progress by one rule - a filling bar when the run said how
    ' much there is, a marquee when it did not.
    Private Sub CheckProgressRule()
        Dim withTotals As New EventStream.ProgressInfo With {.DoneBytes = 50, .TotalBytes = 200}
        Dim itemsOnly As New EventStream.ProgressInfo With {.DoneItems = 3, .TotalItems = 4}
        Dim noTotals As New EventStream.ProgressInfo With {.DoneItems = 7}
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            cv = New CommandView()
            For Each pair In New Object()() {
                New Object() {"job", CType(Sub(p As EventStream.ProgressInfo) jv.FeedProgressForTest(p), Action(Of EventStream.ProgressInfo)), jv.ProgressBarForTest},
                New Object() {"command", CType(Sub(p As EventStream.ProgressInfo) cv.FeedProgressForTest(p), Action(Of EventStream.ProgressInfo)), cv.ProgressBarForTest}}
                Dim name = DirectCast(pair(0), String)
                Dim feed = DirectCast(pair(1), Action(Of EventStream.ProgressInfo))
                Dim bar = DirectCast(pair(2), ProgressBar)
                feed(withTotals)
                Check("progress:" & name & ":bytes", bar.Style = ProgressBarStyle.Continuous AndAlso bar.Value = 25,
                      bar.Style.ToString() & " " & bar.Value.ToString())
                feed(itemsOnly)
                Check("progress:" & name & ":items", bar.Style = ProgressBarStyle.Continuous AndAlso bar.Value = 75,
                      bar.Style.ToString() & " " & bar.Value.ToString())
                feed(noTotals)
                Check("progress:" & name & ":marquee", bar.Style = ProgressBarStyle.Marquee, bar.Style.ToString())
            Next
        Catch ex As Exception
            Check("progress", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' T8, rule 7: rendering a translated template cannot throw. The cases are the ways a template
    ' goes wrong in a translation edit - a brace dropped, an index that does not exist.
    Private Sub CheckLocalizedFormat()
        Dim cases = New Object()() {
            New Object() {"plain", "a {0} b", New Object() {"x"}, "a x b"},
            New Object() {"two", "{1}-{0}", New Object() {"x", "y"}, "y-x"},
            New Object() {"missing-arg", "a {1} b", New Object() {"x"}, "a  b"},
            New Object() {"no-args", "a {0}", New Object() {}, "a "},
            New Object() {"unclosed", "bad {0", New Object() {"x"}, "bad {0"},
            New Object() {"stray-close", "bad } here", New Object() {"x"}, "bad } here"},
            New Object() {"not-a-number", "bad {x} here", New Object() {"x"}, "bad {x} here"},
            New Object() {"escaped", "{{0}} {0}", New Object() {"x"}, "{0} x"},
            New Object() {"align", "[{0,3}]", New Object() {"x"}, "[  x]"},
            New Object() {"format", "{0:F1}", New Object() {1.25}, (1.25).ToString("F1")},
            New Object() {"bad-format", "{0:Q}", New Object() {5}, "5"}
        }
        For Each c In cases
            Dim got As String
            Try
                got = Localization.Format(DirectCast(c(1), String), DirectCast(c(2), Object()))
            Catch ex As Exception
                got = "threw " & ex.GetType().Name
            End Try
            Check("format:" & DirectCast(c(0), String), got = DirectCast(c(3), String), got)
        Next
    End Sub

    ' T12, rule 9, and B2 of the ladder: every control a user can operate has a name - its text or
    ' its accessible name - and every rail row has a default action, which is what UI Automation's
    ' Invoke reaches. The whole window is built and walked, every job page included.
    Private Sub CheckAccessibleNames()
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            Dim problems As New List(Of String)
            WalkAccessible(shell, problems)
            Check("a11y:window", problems.Count = 0, String.Join("; ", problems.Take(8).ToArray()))

            Dim jv As JobView = Nothing
            For Each c In AllControls(shell)
                jv = TryCast(c, JobView)
                If jv IsNot Nothing Then Exit For
            Next
            If jv IsNot Nothing Then
                For Each job In JobCatalogue.GetAllJobs()
                    jv.SetJob(job)
                    Dim jobProblems As New List(Of String)
                    WalkAccessible(jv, jobProblems)
                    Check("a11y:" & job.Id, jobProblems.Count = 0, String.Join("; ", jobProblems.Take(8).ToArray()))
                Next
            Else
                Check("a11y:job-page", False, "the window has no job page")
            End If
        Catch ex As Exception
            Check("a11y", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

    Private Sub WalkAccessible(root As Control, problems As List(Of String))
        For Each c In AllControls(root)
            Dim interactive = TypeOf c Is ButtonBase OrElse TypeOf c Is ComboBox OrElse TypeOf c Is TextBoxBase OrElse
                              TypeOf c Is LinkLabel OrElse TypeOf c Is ListBox OrElse TypeOf c Is RailEntry
            If Not interactive Then Continue For
            Dim named = Not String.IsNullOrWhiteSpace(c.Text) OrElse Not String.IsNullOrWhiteSpace(c.AccessibleName)
            ' A text box's Text is what the user typed, not its name: it must carry one of its own.
            If TypeOf c Is TextBoxBase OrElse TypeOf c Is ComboBox OrElse TypeOf c Is ListBox Then
                named = Not String.IsNullOrWhiteSpace(c.AccessibleName)
            End If
            If Not named Then problems.Add(c.GetType().Name & " " & If(c.Name, "") & " has no name")
            Dim row = TryCast(c, RailEntry)
            If row IsNot Nothing AndAlso String.IsNullOrEmpty(row.AccessibilityObject.DefaultAction) Then
                problems.Add("rail row " & row.Key & " has no default action")
            End If
        Next
    End Sub

    Private Iterator Function AllControls(root As Control) As IEnumerable(Of Control)
        For Each c As Control In root.Controls
            Yield c
            For Each inner In AllControls(c)
                Yield inner
            Next
        Next
    End Function

    ' T13, rule 10: where a saved window is put back, for screens that are invented here.
    Private Sub CheckPlacement()
        Dim primary As New WindowPlacement.ScreenArea(New Rectangle(0, 0, 1920, 1040), 96)
        Dim second As New WindowPlacement.ScreenArea(New Rectangle(1920, 0, 2560, 1400), 144)
        Dim one = New List(Of WindowPlacement.ScreenArea) From {primary}
        Dim both = New List(Of WindowPlacement.ScreenArea) From {primary, second}
        Const caption As Integer = 30

        Dim r = WindowPlacement.Place(New Rectangle(100, 100, 1200, 800), 96, both, caption)
        Check("placement:kept", r = New Rectangle(100, 100, 1200, 800), r.ToString())

        ' The second monitor is gone: moved onto the nearest screen, at its saved size.
        r = WindowPlacement.Place(New Rectangle(2200, 200, 1200, 800), 96, one, caption)
        Check("placement:vanished-monitor", primary.Area.Contains(r) AndAlso r.Size = New Size(1200, 800), r.ToString())

        ' The title strip is above the top of the screen: pulled down until it is on it.
        r = WindowPlacement.Place(New Rectangle(100, -200, 1200, 800), 96, one, caption)
        Check("placement:title-strip", r.Top >= 0 AndAlso r.Top + caption <= primary.Area.Bottom, r.ToString())

        ' Hanging off the bottom with only a sliver showing: pulled up so the strip is reachable.
        r = WindowPlacement.Place(New Rectangle(100, 1030, 1200, 800), 96, one, caption)
        Check("placement:off-bottom", r.Top + caption <= primary.Area.Bottom, r.ToString())

        ' Saved at 144 DPI, restored on a 96 DPI screen: two thirds of the size.
        r = WindowPlacement.Place(New Rectangle(100, 100, 1500, 900), 144, one, caption)
        Check("placement:dpi-scaled", r.Size = New Size(1000, 600), r.ToString())

        ' Larger than the screen: no larger than its working area.
        r = WindowPlacement.Place(New Rectangle(0, 0, 4000, 3000), 96, one, caption)
        Check("placement:clamped", r.Width <= 1920 AndAlso r.Height <= 1040, r.ToString())

        r = WindowPlacement.Place(New Rectangle(0, 0, 800, 600), 96, New List(Of WindowPlacement.ScreenArea)(), caption)
        Check("placement:no-screens", r.IsEmpty, r.ToString())
    End Sub

    ' T10 and T11, rule 5: the Wipe page confirms what is really there, and only what it can run.
    Private Sub CheckWipeRules()
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Dim reason As String = ""
        Try
            Dim wipe = JobCatalogue.GetJob("rail_job_wipe")
            jv = New JobView()
            jv.SetJob(wipe)
            jv.SetTarget("C:\sample-that-is-not-there")

            jv.SetWipeInputsForTest("WIPE", False)
            Dim runs = jv.RunStateForTest(reason)
            Check("wipe:needs-y", Not runs AndAlso reason <> "", runs.ToString() & " / " & reason)

            jv.SetWipeInputsForTest("", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:needs-typed-word", Not runs, runs.ToString())

            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:typed-and-y-runs", runs, runs.ToString() & " / " & reason)

            jv.SetCountForTest(0, 0)
            runs = jv.RunStateForTest(reason)
            Check("wipe:empty-is-nothing", Not runs AndAlso reason <> "", runs.ToString() & " / " & reason)

            jv.SetCountForTest(0, 3)
            runs = jv.RunStateForTest(reason)
            Check("wipe:empty-folders-still-confirm", runs, runs.ToString() & " / " & reason)

            jv.SetTarget("C:\")
            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:drive-root-needs-console", Not runs AndAlso reason <> "", runs.ToString() & " / " & reason)

            Check("wipe-safety:root", WipeSafety.DangerKey("D:\") = "shell_wipe_danger_root", WipeSafety.DangerKey("D:\"))
            Check("wipe-safety:share", WipeSafety.DangerKey("\\server\share") = "shell_wipe_danger_share",
                  WipeSafety.DangerKey("\\server\share"))
            Check("wipe-safety:temp", WipeSafety.DangerKey(IO.Path.GetTempPath()) = "shell_wipe_danger_temp",
                  WipeSafety.DangerKey(IO.Path.GetTempPath()))
            Check("wipe-safety:folder", WipeSafety.DangerKey("C:\sample\folder") = "", WipeSafety.DangerKey("C:\sample\folder"))

            cv = New CommandView()
            Check("command:wipe-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\sample wipe -y", ""), "")
            Check("command:wipe-typed-runs", cv.RunEnabledForTest("filedo.exe C:\sample wipe -y", "WIPE"), "")
            Check("command:wipe-without-y-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\sample wipe", ""), "")
            Check("command:wipe-root-refused", Not cv.RunEnabledForTest("filedo.exe D:\ wipe -y", "WIPE"), "")
            Check("command:no-wipe-no-word", cv.RunEnabledForTest("filedo.exe E: info", ""), "")
        Catch ex As Exception
            Check("wipe", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' T9, rule 1: a question's Escape presses its no-action answer.
    Private Sub CheckDialogEscape()
        Try
            Dim cancel = ShellDialog.CancelOf(New String() {"Stop and close", "Keep running"}, 1)
            Check("dialog:escape-is-no-action", cancel = "Keep running", cancel)
        Catch ex As Exception
            Check("dialog", False, ex.GetType().Name & ": " & ex.Message)
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
