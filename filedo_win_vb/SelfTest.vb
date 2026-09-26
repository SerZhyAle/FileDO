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

        ' The failures some rows provoke on purpose are logged; they go to a file of the self-test's
        ' own, never to the user's filedo_win.log.
        Dim testLog = Path.Combine(Path.GetTempPath(), "filedo_selftest_shell_" & Guid.NewGuid().ToString("N") & ".log")
        ShellLog.PathForTest = testLog
        Try
            RunAll()
        Finally
            ShellLog.PathForTest = Nothing
            Try
                File.Delete(testLog)
            Catch
            End Try
        End Try
        Return WriteLog()
    End Function

    Private Sub RunAll()
        ' SHELL-15: every group of rows runs inside Guard, so a check that throws is a FAIL row with
        ' the exception's type - and the rows after it still run - instead of a process that dies
        ' before the log is written.
        Guard("ArgQuoting", Sub() Check("ArgQuoting", ArgQuotingTests.RunTests()))
        Guard("argquoting", AddressOf CheckArgQuotingCases)
        Guard("catalogue", AddressOf CheckCatalogue)
        Guard("page", AddressOf CheckJobPages)
        Guard("expert", AddressOf CheckExpertPage)
        Guard("rail", AddressOf CheckRailTargets)
        Guard("verdict", AddressOf CheckVerdictTable)
        Guard("glyph", AddressOf CheckVerdictGlyphs)

        ' SP-0016: the iconography contracts - the vendored drawings, the rail's glyph map, the shared
        ' state tones, and the contrast of every glyph against the surface it is drawn on.
        Guard("icons", AddressOf CheckGlyphProvenance)
        Guard("rail-glyph", AddressOf CheckRailGlyphs)
        Guard("menu-icon", AddressOf CheckMenuIcons)
        Guard("state-tone", AddressOf CheckStateTones)
        Guard("contrast", AddressOf CheckGlyphContrast)

        Guard("tailer", AddressOf CheckEventStreamTailer)
        Guard("tailer-long-line", AddressOf CheckEventStreamLongLine)
        Guard("sample", AddressOf CheckEventStreamSample)
        Guard("fdsec", AddressOf CheckFdsecReportRedaction)

        ' SP-0014: the shared desktop contracts APP-BEHAVIOUR and APP-STYLE, rung by rung.
        Guard("palette", AddressOf CheckPaletteCompleteness)
        Guard("theme", AddressOf CheckThemeRoundTrip)
        Guard("disabled-caption", AddressOf CheckDisabledCaptions)
        Guard("rail-paint", AddressOf CheckRailPaint)
        Guard("rail-label", AddressOf CheckRailLabels)
        Guard("progress", AddressOf CheckProgressRule)
        Guard("format", AddressOf CheckLocalizedFormat)
        Guard("a11y", AddressOf CheckAccessibleNames)
        Guard("placement", AddressOf CheckPlacement)
        Guard("wipe", AddressOf CheckWipeRules)
        Guard("dialog", AddressOf CheckDialogEscape)

        ' SP-0029: the shell's robustness remediation, ticket by ticket.
        Guard("target", AddressOf CheckTargetRules)
        Guard("dup", AddressOf CheckDuplicatesPage)
        Guard("command-cred", AddressOf CheckCommandCredential)
        Guard("redirect", AddressOf CheckRedirectNotice)
        Guard("runner", AddressOf CheckRunnerChild)
        Guard("runner-start", AddressOf CheckStartFailure)
        Guard("report", AddressOf CheckReportRedaction)
        Guard("history", AddressOf CheckHistory)
        Guard("output", AddressOf CheckOutputBounds)
        Guard("params", AddressOf CheckParameters)
        Guard("check-options", AddressOf CheckCheckOptions)
        Guard("clean", AddressOf CheckCleanCarriesTarget)
        Guard("cause", AddressOf CheckCauses)
        Guard("duration", AddressOf CheckDurationFormat)
        Guard("about", AddressOf CheckAboutStamp)
        Guard("logs", AddressOf CheckLogArchives)
    End Sub

    Private Sub Guard(name As String, body As Action)
        Try
            body()
        Catch ex As Exception
            Check(name, False, "threw " & ex.GetType().Name & ": " & ex.Message)
        End Try
    End Sub

    Private Function LogFilePath() As String
        Return Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "filedo_win_selftest.log")
    End Function

    Private Function WriteLog() As Integer
        Dim logFile = LogFilePath()
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

    ' The last resort of Program.Main: something outside every Guard threw. The rows written so far
    ' and the crash still reach the log, and the gate fails rather than going silent.
    Public Function WriteCrash(ex As Exception) As Integer
        Try
            Check("selftest-crash", False, If(ex Is Nothing, "unknown", ex.GetType().Name & ": " & ex.Message))
            Return WriteLog()
        Catch
            Return 2
        End Try
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

                ' SP-0025 FDSEC-19: filedo.exe refuses a pe: variable that is empty, so an
                ' empty password - the page's documented obfuscation-only choice - travels as
                ' the visible p:, and a real one by variable name.
                If job.DefaultVerb = "secure" OrElse job.DefaultVerb = "unsecure" OrElse job.DefaultVerb = "reveal" Then
                    view.SetCredentialForTest("")
                    Dim emptyCmd = view.CurrentCommand()
                    Check("page:" & job.Id & ":empty-cred-is-p", emptyCmd.EndsWith(" p:") AndAlso Not emptyCmd.Contains("pe:"), emptyCmd)
                    view.SetCredentialForTest("selftest-pw")
                    Dim credCmd = view.CurrentCommand()
                    Check("page:" & job.Id & ":cred-by-variable", credCmd.Contains("pe:FILEDO_SHELL_CRED") AndAlso Not credCmd.Contains("selftest-pw"), credCmd)
                End If
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

    ' SP-0005 section 12: a reveal's sealed true name is allowed on screen,
    ' but never in the shell's retained report.  Exercise every output shape
    ' that names a copy - reveal's "Revealed" line and indented fallback,
    ' unsecure start's "OK" line with the name repeated in parentheses - in
    ' both line endings, and leave ordinary output and the sandbox intact.
    Private Sub CheckFdsecReportRedaction()
        Const sealedName As String = "fdsec report (name) token.txt"
        Const box As String = "C:\Users\user\AppData\Local\FileDO\reveal\"
        Dim lines = {
            "Revealed C:\box.fd-sec -> " & box & "rv-test\" & sealedName & " (12 B)",
            "  " & box & "rv-test\" & sealedName,
            "OK C:\box.fd-sec -> " & box & "us-test\" & sealedName & " (" & sealedName & ", 12 B)",
            "ordinary output"}
        For Each eol In {vbLf, vbCrLf}
            Dim cleaned = Runner.RedactReportOutput(String.Join(eol, lines) & eol)
            Dim ok = Not cleaned.Contains("report (name)") AndAlso
                     cleaned.Contains(box & "rv-test\<protected reveal copy>" & eol) AndAlso
                     cleaned.Contains(box & "us-test\<protected reveal copy>" & eol) AndAlso
                     cleaned.Contains(eol & "ordinary output" & eol)
            Check("fdsec:report-redaction" & If(eol = vbLf, "", "-crlf"), ok, cleaned)
        Next
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
    ' does not know, shows its state glyph in its state colour. Passed, Done and Failed draw the
    ' vocabulary's status.ok and status.error, Stopped and Not proven status.stopped and
    ' status.not-proven (ICON-SET 0.14) - every verdict a vocabulary drawing. The plain check (the
    ' vocabulary's action.confirm, Segoe E73E) and the plain cross (nav.close, E711) are the two
    ' pictures listed as distinct from status.ok and status.error, so neither may come back.
    Private Sub CheckVerdictGlyphs()
        Dim p = Theme.Current
        Dim cases = New Object()() {
            New Object() {"Passed", "status.ok", p.StateOk},
            New Object() {"Done", "status.ok", p.StateOk},
            New Object() {"Failed", "status.error", p.StateError},
            New Object() {"Stopped", "status.stopped", p.StateWarning},
            New Object() {"Not proven", "status.not-proven", p.MutedText},
            New Object() {"Quarantined", "status.not-proven", p.MutedText}
        }
        For Each c In cases
            Dim verdict = DirectCast(c(0), String)
            Dim glyph = Theme.VerdictGlyph(verdict)
            Check("glyph:" & verdict & ":meaning", glyph.Meaning = DirectCast(c(1), String), glyph.ToString())
            Dim code = AscW(glyph.Interim) And &HFFFF
            Check("glyph:" & verdict & ":not-confirm-or-close", code <> &HE73E AndAlso code <> &HE711, glyph.ToString())
            Check("glyph:" & verdict & ":drawn", glyph.IsVocabulary AndAlso Glyphs.IsDrawable(glyph.Id), glyph.ToString())
            Dim want = DirectCast(c(2), Color)
            Dim got = Theme.VerdictColor(verdict, p)
            Check("glyph:" & verdict & ":colour", got.ToArgb() = want.ToArgb(), got.ToString())
        Next
    End Sub

    ' ---- SP-0016: the vocabulary's drawings -----------------------------------

    ' Every vocabulary id the shell draws: the rail's glyph map, the group chevrons, the verdicts,
    ' and the Explorer icons it writes (MenuIcons.vb).
    Private Function MappedVocabularyIds() As HashSet(Of String)
        Dim ids As New HashSet(Of String)(MenuIcons.Ids, StringComparer.Ordinal)
        Dim refs As New List(Of GlyphRef)()
        For Each row In RailRow.All
            If row.Glyph IsNot Nothing Then refs.Add(row.Glyph)
        Next
        refs.Add(Theme.ChevronGlyph(True))
        refs.Add(Theme.ChevronGlyph(False))
        For Each verdict In New String() {"Passed", "Done", "Failed", "Stopped", "Not proven"}
            refs.Add(Theme.VerdictGlyph(verdict))
        Next
        For Each r In refs
            If r.IsVocabulary Then ids.Add(r.Id)
        Next
        Return ids
    End Function

    ' T10, ICON-EXTERNAL rule 5 and the compatibility law's "a higher MAJOR is refused cleanly":
    ' every vendored file is embedded and matches its SHA-256 line in PROVENANCE.txt, which was
    ' mapped against this build's ICON-SET MAJOR; every vocabulary id the shell draws is among
    ' them and every one of them is drawn by something; each lands on its 24 grid and actually
    ' paints - the pixel test, because a reader that misses part of the SVG subset draws another
    ' picture rather than failing.
    Private Sub CheckGlyphProvenance()
        Dim problems = Glyphs.Problems()
        Check("icons:provenance", problems.Count = 0,
              If(problems.Count = 0, Glyphs.RecordedFileCount().ToString() & " files verified, " & Glyphs.CatalogVersions(),
                 String.Join("; ", problems.ToArray())))
        Check("icons:icon-set-major", Glyphs.CatalogVersions().StartsWith("ICON-SET " & Glyphs.MappedIconSetMajor.ToString() & ".",
                                                                          StringComparison.Ordinal),
              Glyphs.CatalogVersions())

        Dim mapped = MappedVocabularyIds()
        For Each id In mapped.OrderBy(Function(s) s)
            Check("icons:mapped:" & id, Glyphs.IsDrawable(id),
                  If(Glyphs.IsDrawable(id), "vendored, verified", "not vendored or not verified - run assets\sync-icon-glyphs.ps1"))
        Next

        Dim p = Theme.PaletteFor(False)
        For Each id In Glyphs.DrawableIds().OrderBy(Function(s) s)
            Check("icons:used:" & id, mapped.Contains(id), If(mapped.Contains(id), "drawn by the shell", "vendored but drawn by nothing"))
            Dim box = Glyphs.InkBounds(id)
            Check("icons:grid:" & id, box.Width > 0 AndAlso box.Height > 0 AndAlso box.Left >= 0 AndAlso box.Top >= 0 AndAlso
                                      box.Right <= 24 AndAlso box.Bottom <= 24, box.ToString())
            Dim inked = 0
            Using bmp As New Bitmap(24, 24)
                Using g = Graphics.FromImage(bmp)
                    Using back As New SolidBrush(p.Surface)
                        g.FillRectangle(back, 0, 0, 24, 24)
                    End Using
                    Glyphs.Draw(g, GlyphRef.Vocabulary(id), New Rectangle(0, 0, 24, 24), p.Text)
                End Using
                For y = 0 To 23
                    For x = 0 To 23
                        If bmp.GetPixel(x, y).ToArgb() <> p.Surface.ToArgb() Then inked += 1
                    Next
                Next
            End Using
            Check("icons:paints:" & id, inked >= 24, inked.ToString() & " of 576 px inked at 24 px")
        Next
    End Sub

    ' The rows that draw a stand-in. Zero since ICON-SET 0.14 (2026-09-25) took the twelve meanings
    ' FileDO proposed; a new row never joins them - a new control starts from the vocabulary (ICON-SET
    ' rule 5) - so raising this is an amendment with a reason, not a fix.
    Private Const WaitingRailRowsBaseline As Integer = 0

    ' T1, ICON-SET rules 1, 4 and 5 on the rail: every job row shows a glyph; a vocabulary one is
    ' drawable; a waiting one names the id proposed for it and a Segoe stand-in in the private-use
    ' area with that font's own name for it (ICON-EXTERNAL rule 5); and no two rows that show
    ' different meanings share a picture - neither one vocabulary drawing nor one stand-in.
    Private Sub CheckRailGlyphs()
        Dim meaningOf As New Dictionary(Of String, String)(StringComparer.Ordinal)
        Dim waiting As New List(Of String)()
        For Each row In RailRow.All
            Dim glyph = row.Glyph
            If row.IsGroup Then
                Check("rail-glyph:" & row.Key, glyph Is Nothing, "a group header draws its chevron, not a glyph")
                Continue For
            End If
            If glyph Is Nothing Then
                Check("rail-glyph:" & row.Key, False, "no glyph")
                Continue For
            End If
            Dim code = AscW(glyph.Interim) And &HFFFF
            If glyph.IsVocabulary Then
                Check("rail-glyph:" & row.Key, Glyphs.IsDrawable(glyph.Id), glyph.ToString())
            Else
                waiting.Add(row.Key)
                Check("rail-glyph:" & row.Key, glyph.Pending.Contains(".") AndAlso code >= &HE000 AndAlso code <= &HF8FF AndAlso
                                               glyph.FontName <> "", glyph.ToString())
            End If
            Dim picture = If(glyph.IsVocabulary, glyph.Id, "U+" & code.ToString("X4"))
            Dim other As String = Nothing
            If meaningOf.TryGetValue(picture, other) Then
                Check("rail-glyph:shared:" & row.Key, other = glyph.Meaning, picture & " already shows " & other)
            Else
                meaningOf(picture) = glyph.Meaning
            End If
        Next
        Check("rail-glyph:waiting", waiting.Count <= WaitingRailRowsBaseline,
              waiting.Count.ToString() & " rows draw a stand-in (baseline " & WaitingRailRowsBaseline.ToString() &
              ", PROPOSAL-2026-09-23-filedo-meanings.md): " & String.Join(", ", waiting.ToArray()))
    End Sub

    ' T8, ICON-SET rule 7 and ICON-RENDER 0.12 rule 9 on the Explorer surfaces: every icon the
    ' writers name is embedded (it came from assets\menu-icons\), carries every size, paints in the
    ' one menu tone, and still is the drawing of its glyph - each size is drawn afresh and compared
    ' with the copy. The tolerance is for anti-aliasing that may differ by a step between Windows
    ' builds; a changed drawing moves whole pixels and fails. The fix is to re-run
    ' `filedo_win.exe --write-menu-icons assets\menu-icons` and rebuild.
    Private Sub CheckMenuIcons()
        Const tolerance As Integer = 24
        For Each id In MenuIcons.Ids
            Dim ico = MenuIcons.EmbeddedIco(id)
            If ico Is Nothing Then
                Check("menu-icon:" & id, False, "not embedded - assets\menu-icons\" & id & ".ico is missing")
                Continue For
            End If
            Dim images = MenuIcons.ReadIco(ico)
            Try
                For Each size In MenuIcons.Sizes
                    Dim stored As Bitmap = Nothing
                    If Not images.TryGetValue(size, stored) Then
                        Check("menu-icon:" & id & ":" & size.ToString(), False, "no " & size.ToString() & " px image")
                        Continue For
                    End If
                    Dim worst = 0, inked = 0, offTone = 0
                    Using fresh = MenuIcons.Render(id, size)
                        For y = 0 To size - 1
                            For x = 0 To size - 1
                                Dim a = fresh.GetPixel(x, y), b = stored.GetPixel(x, y)
                                worst = Math.Max(worst, Math.Abs(CInt(a.A) - b.A))
                                If b.A > 0 Then
                                    inked += 1
                                    If b.A >= 32 AndAlso (Math.Abs(CInt(b.R) - MenuIcons.Tone.R) > 3 OrElse Math.Abs(CInt(b.G) - MenuIcons.Tone.G) > 3 OrElse
                                       Math.Abs(CInt(b.B) - MenuIcons.Tone.B) > 3) Then offTone += 1
                                End If
                            Next
                        Next
                    End Using
                    Check("menu-icon:" & id & ":" & size.ToString(), worst <= tolerance AndAlso offTone = 0 AndAlso inked * 16 >= size * size,
                          "alpha differs by " & worst.ToString() & " at most, " & inked.ToString() & " px inked, " &
                          offTone.ToString() & " off the menu tone")
                Next
            Finally
                For Each bmp In images.Values
                    bmp.Dispose()
                Next
            End Try
        Next
    End Sub

    ' T11, ICON-RENDER section 10 item D taken as exact tones (SP-0016 D2): each state role of both
    ' palettes is the vendored palette.json's day or night tone - except the light warning, whose
    ' day tone fails 3:1 (the exception in Theme.vb). That row fails the day the catalog's tone
    ' reaches 3:1 on the light card, so the exception cannot outlive its reason.
    Private Sub CheckStateTones()
        Dim json = Glyphs.VerifiedData("palette.json")
        If json Is Nothing Then
            Check("state-tone:palette", False, "palette.json is not vendored or did not verify")
            Return
        End If
        Dim hues As Dictionary(Of String, Object) = Nothing
        Try
            Dim root = New Web.Script.Serialization.JavaScriptSerializer().Deserialize(Of Dictionary(Of String, Object))(json)
            hues = DirectCast(root("hues"), Dictionary(Of String, Object))
        Catch ex As Exception
            Check("state-tone:palette", False, ex.GetType().Name & ": " & ex.Message)
            Return
        End Try

        Dim light = Theme.PaletteFor(False)
        Dim dark = Theme.PaletteFor(True)
        Dim rows = New Object()() {
            New Object() {"state.ok", "day", light.StateOk},
            New Object() {"state.ok", "night", dark.StateOk},
            New Object() {"state.error", "day", light.StateError},
            New Object() {"state.error", "night", dark.StateError},
            New Object() {"state.warning", "night", dark.StateWarning}
        }
        For Each r In rows
            Dim key = DirectCast(r(0), String)
            Dim tone = DirectCast(r(1), String)
            Dim got = DirectCast(r(2), Color)
            Dim want = PaletteTone(hues, key, tone)
            Check("state-tone:" & key & ":" & tone, Not want.IsEmpty AndAlso want.ToArgb() = got.ToArgb(),
                  HexOf(got) & " (palette.json " & HexOf(want) & ")")
        Next

        Dim warnDay = PaletteTone(hues, "state.warning", "day")
        Dim ratio = Theme.ContrastRatio(warnDay, light.Surface)
        Check("state-tone:state.warning:day-exception",
              Not warnDay.IsEmpty AndAlso ratio < 3.0 AndAlso light.StateWarning.ToArgb() = light.Warning.ToArgb(),
              "palette.json " & HexOf(warnDay) & " is " & ratio.ToString("0.00", Globalization.CultureInfo.InvariantCulture) &
              ":1 on the light card; the light StateWarning stays the shell's " & HexOf(light.StateWarning))
    End Sub

    Private Function PaletteTone(hues As Dictionary(Of String, Object), key As String, tone As String) As Color
        Dim hue As Object = Nothing
        If hues Is Nothing OrElse Not hues.TryGetValue(key, hue) Then Return Nothing
        Dim fields = TryCast(hue, Dictionary(Of String, Object))
        Dim value As Object = Nothing
        If fields Is Nothing OrElse Not fields.TryGetValue(tone, value) Then Return Nothing
        Return Theme.FromHex(TryCast(value, String))
    End Function

    Private Function HexOf(c As Color) As String
        If c.IsEmpty Then Return "(none)"
        Return "#" & c.R.ToString("X2") & c.G.ToString("X2") & c.B.ToString("X2")
    End Function

    ' ICON-RENDER rule 3 and conformance rung 4, measured rather than claimed: every glyph the
    ' shell draws against every surface it is drawn on, in both palettes - 3:1 for a glyph, and
    ' 4.5:1 for the verdict word the Command page paints in the glyph's colour and for the verdict
    ' badge's word on its fill.
    Private Sub CheckGlyphContrast()
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim t = If(dark, "dark", "light")
            ContrastPair("contrast:" & t & ":rail-glyph:plain", p.Text, p.SurfaceAlt, 3.0)
            ContrastPair("contrast:" & t & ":rail-glyph:hover", p.Text, p.ControlHover, 3.0)
            ContrastPair("contrast:" & t & ":rail-glyph:selected", p.Text, p.SurfaceSelected, 3.0)
            ContrastPair("contrast:" & t & ":chevron:plain", p.MutedText, p.SurfaceAlt, 3.0)
            ContrastPair("contrast:" & t & ":chevron:hover", p.MutedText, p.ControlHover, 3.0)
            For Each verdict In New String() {"Passed", "Done", "Failed", "Stopped", "Not proven"}
                ContrastPair("contrast:" & t & ":verdict:" & verdict, Theme.VerdictColor(verdict, p), p.Surface, 4.5)
                ContrastPair("contrast:" & t & ":badge:" & verdict, Theme.VerdictFore(verdict, p), Theme.VerdictBack(verdict, p), 4.5)
            Next
        Next
    End Sub

    Private Sub ContrastPair(name As String, fore As Color, back As Color, need As Double)
        Dim ratio = Theme.ContrastRatio(fore, back)
        Dim inv = Globalization.CultureInfo.InvariantCulture
        Check(name, ratio >= need, ratio.ToString("0.00", inv) & ":1, needs " & need.ToString("0.0", inv) & " - " &
              HexOf(fore) & " on " & HexOf(back))
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
    ' screen under one palette, the other palette applied, and the badge and both verdict glyphs
    ' must carry the new palette's StateError. The palette is switched through Theme's test seam,
    ' never through HKCU.
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
            Check("theme:job-badge-light", jv.VerdictBadgeForTest.BackColor.ToArgb() = light.StateError.ToArgb(),
                  jv.VerdictBadgeForTest.BackColor.ToString())

            Theme.UsePaletteForTest(True)
            jv.ApplyTheme()
            cv.ApplyTheme()
            Dim dark = Theme.PaletteFor(True)
            Check("theme:job-badge-follows", jv.VerdictBadgeForTest.BackColor.ToArgb() = dark.StateError.ToArgb(),
                  jv.VerdictBadgeForTest.BackColor.ToString())
            Check("theme:job-glyph-follows", jv.VerdictGlyphForTest.ForeColor.ToArgb() = dark.StateError.ToArgb() AndAlso
                  jv.VerdictGlyphForTest.Glyph IsNot Nothing AndAlso jv.VerdictGlyphForTest.Glyph.Meaning = "status.error",
                  jv.VerdictGlyphForTest.ForeColor.ToString())
            Check("theme:command-verdict-follows", cv.VerdictLabelForTest.ForeColor.ToArgb() = dark.StateError.ToArgb(),
                  cv.VerdictLabelForTest.ForeColor.ToString())
            Check("theme:command-glyph-follows", cv.VerdictGlyphForTest.ForeColor.ToArgb() = dark.StateError.ToArgb() AndAlso
                  cv.VerdictGlyphForTest.Glyph IsNot Nothing AndAlso cv.VerdictGlyphForTest.Glyph.Meaning = "status.error",
                  cv.VerdictGlyphForTest.ForeColor.ToString())
            Check("theme:command-line-has-no-glyph", Not cv.VerdictLabelForTest.Text.Any(Function(ch) AscW(ch) >= &HE000 AndAlso AscW(ch) <= &HF8FF),
                  cv.VerdictLabelForTest.Text)
        Catch ex As Exception
            Check("theme", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Theme.UsePaletteForTest(Nothing)
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' A disabled control's caption is readable and stays where it was. WinForms draws it in a darker
    ' shade of the back colour, which on the dark theme left the Run button of a page waiting for its
    ' target near-black on near-black (Ui.KeepCaptionsReadable). Each kind the shell disables is drawn
    ' twice on the palette's own colours: disabled, and enabled with TextDisabled as its fore colour -
    ' the caption WinForms itself draws in that colour. Both must put their ink - pixels at 3:1 or more
    ' against the control's back colour - in the same place, and there must be some.
    Private Sub CheckDisabledCaptions()
        For Each dark In New Boolean() {False, True}
            Theme.UsePaletteForTest(dark)
            Dim themeName = If(dark, "dark", "light")
            Try
                Dim p = Theme.Current
                For Each kind In New String() {"button", "check", "radio"}
                    Using host As New Panel With {.BackColor = p.Surface, .Size = New Size(900, 200)}
                        Dim twin = CaptionControl(kind, p)
                        Dim off = CaptionControl(kind, p)
                        off.Enabled = False
                        host.Controls.Add(twin)
                        host.Controls.Add(off)
                        Ui.KeepCaptionsReadable(host)
                        Dim want = CaptionInk(twin)
                        Dim got = CaptionInk(off)
                        Check("disabled-caption:" & themeName & ":" & kind, Not got.IsEmpty AndAlso got = want,
                              "enabled " & want.ToString() & ", disabled " & got.ToString())
                    End Using
                Next
            Catch ex As Exception
                Check("disabled-caption:" & themeName, False, ex.GetType().Name & ": " & ex.Message)
            Finally
                Theme.UsePaletteForTest(Nothing)
            End Try
        Next

        ' The window hooks every button, check box and radio button it has, not only the ones above.
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            Dim missed As New List(Of String)
            For Each c In AllControls(shell)
                Dim b = TryCast(c, ButtonBase)
                If b IsNot Nothing AndAlso Not Ui.CaptionKeptReadable(b) Then missed.Add(b.GetType().Name & " '" & b.Text & "'")
            Next
            Check("disabled-caption:window", missed.Count = 0, String.Join("; ", missed.Take(8).ToArray()))
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

    ' A control of one kind as the shell builds it: the Run button's style, or a body-text option.
    Private Function CaptionControl(kind As String, p As Theme.Palette) As ButtonBase
        Dim c As ButtonBase
        Select Case kind
            Case "button"
                c = New Button With {.Font = Theme.FontBodyStrong(), .Padding = New Padding(18, 7, 18, 7)}
                Ui.StyleButton(DirectCast(c, Button), p.SurfaceAlt, p.TextDisabled, p.Border)
            Case "check"
                c = New CheckBox With {.Font = Theme.FontBody(), .ForeColor = p.TextDisabled}
            Case Else
                c = New RadioButton With {.Font = Theme.FontBody(), .ForeColor = p.TextDisabled}
        End Select
        c.Text = "Run: check for damage"
        c.Size = c.GetPreferredSize(Size.Empty)
        Return c
    End Function

    ' The bounds of a control's caption ink, right of a check box's or radio button's glyph.
    Private Function CaptionInk(c As ButtonBase) As Rectangle
        Using bmp As New Bitmap(c.Width, c.Height)
            c.DrawToBitmap(bmp, New Rectangle(0, 0, c.Width, c.Height))
            Dim fromX = 0
            If Not TypeOf c Is Button Then
                Using g = Graphics.FromImage(bmp)
                    Dim glyph = If(TypeOf c Is CheckBox,
                                   CheckBoxRenderer.GetGlyphSize(g, VisualStyles.CheckBoxState.UncheckedNormal).Width,
                                   RadioButtonRenderer.GetGlyphSize(g, VisualStyles.RadioButtonState.UncheckedNormal).Width)
                    fromX = c.Padding.Left + glyph + 1
                End Using
            End If
            Dim back = c.BackColor
            Dim minX = Integer.MaxValue, minY = Integer.MaxValue, maxX = -1, maxY = -1
            For y = 0 To bmp.Height - 1
                For x = fromX To bmp.Width - 1
                    If Theme.ContrastRatio(bmp.GetPixel(x, y), back) < 3.0 Then Continue For
                    minX = Math.Min(minX, x) : minY = Math.Min(minY, y)
                    maxX = Math.Max(maxX, x) : maxY = Math.Max(maxY, y)
                Next
            Next
            If maxX < 0 Then Return Rectangle.Empty
            Return Rectangle.FromLTRB(minX, minY, maxX + 1, maxY + 1)
        End Using
    End Function

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
                    Dim text = RailText(dict, row, "rail-paint:" & themeName & ":" & row.Key)
                    If text Is Nothing Then Continue For
                    For Each stateName In New String() {"plain", "hover", "selected", "collapsed-holding"}
                        If stateName = "collapsed-holding" AndAlso Not row.IsGroup Then Continue For
                        If stateName = "selected" AndAlso row.IsGroup Then Continue For
                        Using e As New RailEntry With {
                            .Key = row.Key,
                            .Glyph = row.Glyph,
                            .IsGroupHeader = row.IsGroup,
                            .Text = text,
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

    ' A rail row's label as the window draws it, or Nothing - with a FAIL row under the check's name -
    ' when the table has no such key (SHELL-15: a missing key used to throw KeyNotFoundException out
    ' of the whole run).
    Private Function RailText(dict As Dictionary(Of String, String), row As RailRow, name As String) As String
        Dim v As String = Nothing
        If dict Is Nothing OrElse Not dict.TryGetValue(row.Key, v) Then
            Check(name, False, "no text for " & row.Key)
            Return Nothing
        End If
        Return If(row.IsGroup, v.ToUpperInvariant(), v)
    End Function

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
                Dim text = RailText(dict, row, "rail-label:" & lang & ":" & row.Key)
                If text Is Nothing Then Continue For
                Using e As New RailEntry With {
                    .Key = row.Key,
                    .IsGroupHeader = row.IsGroup,
                    .Glyph = row.Glyph,
                    .RowUnit = unit,
                    .Text = text
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

        ' SHELL-08: a window spread over both monitors, its title strip on the first, stays spread.
        r = WindowPlacement.Place(New Rectangle(1500, 100, 1200, 800), 144, both, caption)
        Check("placement:spanning", r = New Rectangle(1500, 100, 1200, 800), r.ToString())

        ' SHELL-09: values no real window has are not restored at all - they used to overflow the
        ' scaling and keep the window from opening.
        Check("placement:huge-x", Not ShellSettings.PlacementIsSane(2147483000, 100, 1200, 800, 96), "")
        Check("placement:tiny-dpi", Not ShellSettings.PlacementIsSane(100, 100, 1200, 800, 1), "")
        Check("placement:zero-width", Not ShellSettings.PlacementIsSane(100, 100, 0, 800, 96), "")
        Check("placement:sane", ShellSettings.PlacementIsSane(-1920, 0, 1200, 800, 144) AndAlso
                                ShellSettings.PlacementIsSane(100, 100, 1200, 800, 0), "")

        ' SHELL-07: maximized, then minimized, then closed - it reopens maximized.
        Check("placement:min-after-max", ShellForm.SavesMaximized(FormWindowState.Minimized, FormWindowState.Maximized) AndAlso
                                         Not ShellForm.SavesMaximized(FormWindowState.Minimized, FormWindowState.Normal) AndAlso
                                         ShellForm.SavesMaximized(FormWindowState.Maximized, FormWindowState.Maximized), "")
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

            ' GUI-11: the CLI's other word for wipe asks for the typed word too, and so does a batch
            ' whose list wipes.
            Check("command:wipe-alias-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\sample w -y", ""), "")
            Check("command:wipe-alias-typed-runs", cv.RunEnabledForTest("filedo.exe C:\sample w -y", "WIPE"), "")
            Dim listDir = Path.Combine(Path.GetTempPath(), "filedo_selftest_lists_" & Guid.NewGuid().ToString("N"))
            Directory.CreateDirectory(listDir)
            Try
                Dim wipingList = Path.Combine(listDir, "wipes.lst")
                File.WriteAllText(wipingList, "# a batch" & vbLf & "C:\sample-x info" & vbLf & "C:\sample-x W -y" & vbLf)
                Dim plainList = Path.Combine(listDir, "plain.lst")
                File.WriteAllText(plainList, "C:\sample-x info" & vbLf)
                Check("command:from-wipe-asks-word", Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(wipingList), ""), "")
                Check("command:from-plain-no-word", cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(plainList), ""), "")
                Check("command:from-missing-asks-word",
                      Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(Path.Combine(listDir, "none.lst")), ""), "")
            Finally
                Try
                    Directory.Delete(listDir, True)
                Catch
                End Try
            End Try

            ' SHELL-04 on the Command page: a relative target is read where filedo.exe runs, and
            ' ".." there is LocalAppData - which holds FileDO's own data.
            Check("command:relative-wipe-refused", Not cv.RunEnabledForTest("filedo.exe .. wipe -y", "WIPE"), "")
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

    ' ---- SP-0029: the shell's robustness remediation --------------------------

    ' GUI-20, GUI-21: the argument rules, one row per case.
    Private Sub CheckArgQuotingCases()
        For Each c In ArgQuotingTests.NamedCases()
            Check("argquoting:" & c.Key, c.Value)
        Next
    End Sub

    ' GUI-01 and SHELL-04: what a page does with the text of step 2.
    Private Sub CheckTargetRules()
        Dim en = Localization.GetDict(ShellSettings.Language())
        Dim notAbsolute = en("shell_target_not_absolute")
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()

            ' GUI-01: a folder with "(" in its name is a folder, not a drive row.
            jv.SetJob(JobCatalogue.GetJob("rail_job_duplicates"))
            jv.SetTarget("D:\Photos (2019)")
            Dim cmd = jv.CurrentCommand()
            Check("page:cd:paren", cmd.Contains("""D:\Photos (2019)""") AndAlso Not cmd.Contains(" D: "), cmd)

            jv.SetJob(JobCatalogue.GetJob("rail_job_reveal"))
            jv.SetTarget("C:\taxes (1).pdf.fd-sec")
            cmd = jv.CurrentCommand()
            Check("page:reveal:paren", cmd.Contains("""C:\taxes (1).pdf.fd-sec"""), cmd)

            ' A drive row the page listed itself is still read as its letter.
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            Dim row = jv.TargetTextForTest
            cmd = jv.CurrentCommand()
            Check("page:drive-row-is-a-letter", row = "" OrElse Not row.Contains("(") OrElse
                                                (Not cmd.Contains("(") AndAlso cmd.Contains(" " & row.Substring(0, 2) & " ")),
                  row & " -> " & cmd)

            ' SHELL-04: relative and drive-relative targets are refused with the reason.
            jv.SetJob(JobCatalogue.GetJob("rail_job_wipe"))
            jv.SetTarget("..")
            jv.SetWipeInputsForTest("WIPE", True)
            Dim runs = jv.RunStateForTest(reason)
            Check("wipe:relative-refused", Not runs AndAlso reason = notAbsolute, runs.ToString() & " / " & reason)
            jv.SetTarget("D:x")
            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:drive-relative-refused", Not runs AndAlso reason = notAbsolute, runs.ToString() & " / " & reason)
            jv.SetTarget("C:\sample-that-is-not-there\sub\..")
            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("wipe:absolute-folded", runs AndAlso cmd.Contains("C:\sample-that-is-not-there wipe") AndAlso Not cmd.Contains(".."),
                  cmd & " / " & reason)

            jv.SetJob(JobCatalogue.GetJob("rail_job_compare"))
            jv.SetTarget("C:\sample-a")
            runs = jv.RunStateForTest(reason)
            Check("page:compare-needs-dest", Not runs, runs.ToString())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try

        Check("target:resolve-folds-dots", TargetPath.Resolve("C:\a\..\b") = "C:\b", TargetPath.Resolve("C:\a\..\b"))
        Check("target:drive-token-kept", TargetPath.Resolve("E:") = "E:", TargetPath.Resolve("E:"))
        Check("target:relative-refused", TargetPath.Resolve("reports") Is Nothing AndAlso TargetPath.Resolve("D:x") Is Nothing AndAlso
                                         TargetPath.Resolve("\x") Is Nothing, "")
        Check("target:unc-kept", TargetPath.Resolve("\\server\share\x") = "\\server\share\x", TargetPath.Resolve("\\server\share\x"))

        ' SHELL-04: the Explorer start - a bare name is the file in the folder it was typed in.
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_start_" & Guid.NewGuid().ToString("N"))
        Dim oldCwd = Environment.CurrentDirectory
        Try
            Directory.CreateDirectory(dir)
            File.WriteAllText(Path.Combine(dir, "notes.txt"), "x")
            Environment.CurrentDirectory = dir
            Dim got = Program.StartupTargetFrom(New String() {"filedo_win.exe", "notes.txt"})
            Check("startup:relative-target", String.Equals(got, Path.Combine(dir, "notes.txt"), StringComparison.OrdinalIgnoreCase), got)
            Check("startup:switch-skipped", Program.StartupTargetFrom(New String() {"filedo_win.exe", "-debug"}) Is Nothing, "")
        Finally
            Environment.CurrentDirectory = oldCwd
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try

        ' SP-0027 WIPE-02, mirrored: FileDO's data, LocalAppData above it and the profile are asked
        ' about twice on a console, so the Wipe page refuses them up front.
        Dim localApp = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)
        For Each pair In New String()() {
            New String() {"wipe-safety:filedo-data", Path.Combine(localApp, "FileDO")},
            New String() {"wipe-safety:localappdata", localApp},
            New String() {"wipe-safety:profile", Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)},
            New String() {"wipe-safety:windows", Environment.GetEnvironmentVariable("SystemRoot")}}
            Dim key = WipeSafety.DangerKey(pair(1))
            Check(pair(0), key = "shell_wipe_danger_system", pair(1) & " -> " & key)
        Next
        Check("wipe-safety:drive-token-is-root", WipeSafety.DangerKey("E:") = "shell_wipe_danger_root", WipeSafety.DangerKey("E:"))
    End Sub

    ' GUI-03: Delete and Move are destructive on the page, and pass -y only behind a typed word.
    Private Sub CheckDuplicatesPage()
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_duplicates"))
            jv.SetTarget("C:\sample-dups")

            Dim runs = jv.RunStateForTest(reason)
            Dim cmd = jv.CurrentCommand()
            Check("dup:report-is-reversible", runs AndAlso Not jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_reversible") AndAlso Not cmd.Contains("-y"), cmd)
            Check("count:dup-report-does-not-count", Not jv.CountRowShownForTest, "")

            jv.SetDupActionForTest("delete")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("dup:delete-is-destructive", Not runs AndAlso jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_permanent"), cmd)
            Check("count:dup-delete-counts", jv.CountRowShownForTest, "")
            jv.SetConfirmWordForTest("delete")
            Check("dup:delete-word-is-exact", Not jv.RunStateForTest(reason), "")
            jv.SetConfirmWordForTest("DELETE")
            runs = jv.RunStateForTest(reason)
            Check("dup:delete-typed-runs", runs AndAlso cmd.Contains(" del -y"), cmd)

            jv.SetDupActionForTest("move", "C:\sample-dups-moved")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("dup:move-is-destructive", Not runs AndAlso jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_partly_reversible"), cmd)
            jv.SetConfirmWordForTest("MOVE")
            runs = jv.RunStateForTest(reason)
            Check("dup:move-typed-runs", runs AndAlso cmd.Contains("move C:\sample-dups-moved -y"), cmd)

            jv.SetDupActionForTest("report")
            runs = jv.RunStateForTest(reason)
            Check("dup:back-to-report", runs AndAlso Not jv.DestructiveBadgeShownForTest AndAlso Not jv.CurrentCommand().Contains("-y"),
                  jv.CurrentCommand())

            ' GUI-17: pages that delete nothing do not walk the target.
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            jv.SetTarget("E:")
            Check("count:fill-does-not-count", Not jv.CountRowShownForTest, "")
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' GUI-02: a line handed over from a Protect page carries its password, and one handed over
    ' without it is not run with an empty one.
    Private Sub CheckCommandCredential()
        Dim cv As CommandView = Nothing
        Try
            cv = New CommandView()
            cv.SelectOperation(0)
            cv.SetCommand("filedo.exe C:\a.txt secure pe:FILEDO_SHELL_CRED")
            Check("command:open-secure-without-cred", Not cv.RunEnabledNowForTest AndAlso cv.CredentialBlockShownForTest,
                  "run " & cv.RunEnabledNowForTest.ToString() & ", block " & cv.CredentialBlockShownForTest.ToString())

            cv.SetCommand("filedo.exe C:\a.txt secure pe:FILEDO_SHELL_CRED", "selftest-handed-over")
            Check("command:open-secure-with-cred", cv.RunEnabledNowForTest AndAlso cv.CredentialBlockShownForTest,
                  "run " & cv.RunEnabledNowForTest.ToString() & ", block " & cv.CredentialBlockShownForTest.ToString())

            ' Handing over a line with no password clears the one an earlier line left.
            cv.SetCommand("filedo.exe C:\b.txt unsecure pe:FILEDO_SHELL_CRED")
            Check("command:stale-cred-not-reused", Not cv.RunEnabledNowForTest, cv.RunEnabledNowForTest.ToString())

            cv.SetCommand("filedo.exe E: info")
            Check("command:no-cred-block-for-info", Not cv.CredentialBlockShownForTest AndAlso cv.RunEnabledNowForTest, "")
        Finally
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' GUI-18: the redirect notice is shown for the root of the system volume in its spellings, and
    ' for nothing below it.
    Private Sub CheckRedirectNotice()
        Dim sys = Environment.GetEnvironmentVariable("SystemRoot")
        Dim letter = If(String.IsNullOrEmpty(sys), "C", sys.Substring(0, 1))
        Dim jv As JobView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            For Each shown In New String() {letter & ":", letter & ":\", letter.ToLowerInvariant() & ":/", "\\?\" & letter & ":\"}
                jv.SetTarget(shown)
                Check("redirect:shown:" & shown, jv.RedirectNoticeShownForTest, jv.CurrentCommand())
            Next
            For Each hidden In New String() {letter & ":\x", letter & ":\Users"}
                jv.SetTarget(hidden)
                Check("redirect:hidden:" & hidden, Not jv.RedirectNoticeShownForTest, jv.CurrentCommand())
            Next
            jv.SetJob(JobCatalogue.GetJob("rail_job_clean"))
            jv.SetTarget(letter & ":\")
            Check("redirect:clean-shown", jv.RedirectNoticeShownForTest, jv.CurrentCommand())
            jv.SetJob(JobCatalogue.GetJob("rail_job_info"))
            jv.SetTarget(letter & ":\")
            Check("redirect:info-hidden", Not jv.RedirectNoticeShownForTest, jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' GUI-04 and GUI-06: a child that reads stdin until it ends is started the way a run is. It ends
    ' by itself because its stdin is closed, and it is in the window's job while it runs.
    Private Sub CheckRunnerChild()
        Dim sortExe = Path.Combine(Environment.SystemDirectory, "sort.exe")
        If Not File.Exists(sortExe) Then
            Check("runner:stdin-closed", False, "no " & sortExe)
            Return
        End If
        Dim inJob = False
        Dim exited = False
        Dim sw = Diagnostics.Stopwatch.StartNew()
        Runner.RunChildForTest(sortExe, "", 5000, inJob, exited)
        Check("runner:stdin-closed", exited, "sort.exe ended after " & sw.ElapsedMilliseconds.ToString() & " ms")
        Check("runner:child-in-job", inJob, "")
    End Sub

    ' SHELL-13: a data folder that cannot be written ends the run Not proven, with the page idle.
    Private Sub CheckStartFailure()
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Runner.RunsDirForTest = Function() As String
                                    Throw New UnauthorizedAccessException("selftest: the runs folder is unavailable")
                                End Function
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_info"))
            jv.SetTarget(Path.GetTempPath())
            jv.PressRunForTest()
            Check("runner:start-failure-not-proven", jv.VerdictTextForTest = dict("shell_verdict_not_proven") AndAlso Not jv.IsRunning,
                  jv.VerdictTextForTest & " / running " & jv.IsRunning.ToString())
        Finally
            Runner.RunsDirForTest = Nothing
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' SHELL-01: the report's Command line is redacted like history.json, and a run that can print a
    ' sealed name keeps no output at all.
    Private Sub CheckReportRedaction()
        ' The vectors the CLI's redactCredentialArgs is tested against, from the copy embedded here.
        Dim vectors As String = Nothing
        Using source = GetType(SelfTest).Assembly.GetManifestResourceStream("FileDOGUI.redaction.vectors.tsv")
            If source IsNot Nothing Then
                Using r As New StreamReader(source, Encoding.UTF8)
                    vectors = r.ReadToEnd()
                End Using
            End If
        End Using
        If vectors Is Nothing Then
            Check("report:vectors", False, "missing FileDOGUI.redaction.vectors.tsv")
        Else
            Dim n = 0
            For Each raw In vectors.Replace(vbCr, "").Split(ControlChars.Lf)
                If raw.Trim() = "" OrElse raw.StartsWith("#") Then Continue For
                n += 1
                Dim sep = raw.IndexOf(vbTab & "=>" & vbTab, StringComparison.Ordinal)
                If sep < 0 Then
                    Check("report:vector:" & n.ToString(), False, "no => in " & raw)
                    Continue For
                End If
                Dim input = raw.Substring(0, sep).Split(ControlChars.Tab)
                Dim want = String.Join(vbTab, raw.Substring(sep + 4).Split(ControlChars.Tab))
                Dim got = String.Join(vbTab, Runner.RedactCredentialArgs(input))
                Check("report:vector:" & n.ToString(), got = want, got.Replace(vbTab, " "))
            Next
            Check("report:vectors", n > 0, n.ToString() & " vectors")
        End If

        ' SP-0025 FDSEC-18, which this port follows: a sub-verb is one only right after fdsec. In
        ' the target-first form the parser takes it as the password, so it is redacted like one;
        ' "start" is an option word of unsecure and is kept.
        Dim redacted = Runner.RedactCredentialArgs(New String() {"a.txt", "secure", "verify", "hunter2"})
        Check("report:target-first-subverb-redacted", String.Join(" ", redacted) = "a.txt secure *** ***", String.Join(" ", redacted))
        redacted = Runner.RedactCredentialArgs(New String() {"a.fd-sec", "unsecure", "start", "pe:FILEDO_SHELL_CRED"})
        Check("report:start-kept", String.Join(" ", redacted) = "a.fd-sec unsecure start pe:FILEDO_SHELL_CRED", String.Join(" ", redacted))

        Check("report:sensitive-verbs",
              Runner.IsSensitiveRun(New String() {"C:\x.fd-sec", "rev"}) AndAlso
              Runner.IsSensitiveRun(New String() {"fdsec", "info", "C:\x.fd-sec"}) AndAlso
              Runner.IsSensitiveRun(New String() {"C:\x.fd-sec", "unsecure", "start"}) AndAlso
              Not Runner.IsSensitiveRun(New String() {"C:\x", "secure", "p:secret"}) AndAlso
              Not Runner.IsSensitiveRun(New String() {"E:", "info"}), "")

        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_report_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim spool = Path.Combine(dir, "run.out")
            File.WriteAllText(spool, "Revealed C:\box.fd-sec -> C:\Users\u\AppData\Local\FileDO\reveal\rv-1\secret-name.docx (12 B)" & vbLf &
                                     "true name: secret-name.docx" & vbLf)
            Dim report1 = Path.Combine(dir, "report_1.log")
            Runner.WriteReport(report1, "1", New String() {"--events", "e.jsonl", "C:\box.fd-sec", "reveal", "pe:FILEDO_SHELL_CRED"},
                               "Done", 0, TimeSpan.FromSeconds(3), spool, Nothing)
            Dim text1 = File.ReadAllText(report1)
            Check("report:fdsec-redaction:reveal", Not text1.Contains("secret-name") AndAlso text1.Contains("Verdict: Done") AndAlso
                                                   text1.Contains("Exit Code: 0"), text1.Replace(vbCrLf, " | "))

            File.WriteAllText(spool, "Secured C:\x.txt" & vbLf)
            Dim report2 = Path.Combine(dir, "report_2.log")
            Runner.WriteReport(report2, "2", New String() {"C:\x.txt", "secure", "p:secret"}, "Done", 0, TimeSpan.FromSeconds(3), spool, Nothing)
            Dim text2 = File.ReadAllText(report2)
            Check("report:fdsec-redaction:typed-password", Not text2.Contains("secret") AndAlso text2.Contains("p:***") AndAlso
                                                           text2.Contains("Secured C:\x.txt"), text2.Replace(vbCrLf, " | "))
        Finally
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try
    End Sub

    ' SHELL-02 and SHELL-03: the sweep keeps what is young and removes what is old, and a report too
    ' big to show whole is shown by its head and its end.
    Private Sub CheckHistory()
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_sweep_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim old = Path.Combine(dir, "report_old.log")
            Dim young = Path.Combine(dir, "report_young.log")
            File.WriteAllText(old, "old")
            File.WriteAllText(young, "young")
            File.SetLastWriteTime(old, DateTime.Now.AddDays(-40))
            File.SetLastWriteTime(young, DateTime.Now.AddDays(-5))
            Dim removed = Runner.SweepDirectory(dir, 30)
            Check("history:sweep", removed = 1 AndAlso Not File.Exists(old) AndAlso File.Exists(young), removed.ToString())

            Dim big = Path.Combine(dir, "report_big.log")
            Using w As New StreamWriter(big, False, New UTF8Encoding(False))
                w.WriteLine("FileDO Run Report")
                w.WriteLine("Verdict: Failed")
                For i = 1 To 40000
                    w.WriteLine("line " & i.ToString() & " of a long run's output - Фото")
                Next
                w.WriteLine("the last line")
            End Using
            Dim view = HistoryView.ReadReportView(big, "TRUNCATED")
            Check("history:report-tail", view.StartsWith("FileDO Run Report") AndAlso view.Contains("Verdict: Failed") AndAlso
                                         view.Contains("TRUNCATED") AndAlso view.TrimEnd().EndsWith("the last line") AndAlso
                                         view.Length < HistoryView.ReportHeadBytes + HistoryView.ReportTailBytes + 200,
                  view.Length.ToString() & " chars")
            Dim smallView = HistoryView.ReadReportView(young, "TRUNCATED")
            Check("history:report-small-whole", smallView = "young", smallView)
        Finally
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try
    End Sub

    ' GUI-10: a result line past the old 2 MB limit is read, and its verdict received.
    Private Sub CheckEventStreamLongLine()
        Dim eventsPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_long_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            Dim sb As New StringBuilder()
            sb.Append("{""schemaVersion"":1,""kind"":""result"",""data"":{""verdict"":""Failed"",""filesLeft"":[")
            Dim n = 0
            While sb.Length < 3 * 1024 * 1024
                If n > 0 Then sb.Append(","c)
                sb.Append("""E:\\FILL_").Append(n.ToString("D8")).Append(".tmp""")
                n += 1
            End While
            sb.Append("]}}").Append(vbLf)
            File.WriteAllText(eventsPath, sb.ToString())
            Dim verdict = ""
            Dim left = 0
            Using stream As New EventStream(eventsPath)
                AddHandler stream.ResultReceived, Sub(r)
                                                      verdict = r.Verdict
                                                      left = r.FilesLeft.Count
                                                  End Sub
                stream.Poll()
            End Using
            Check("tailer:3mb-result", verdict = "Failed" AndAlso left = n, verdict & " / " & left.ToString() & " of " & n.ToString())
        Finally
            Try
                File.Delete(eventsPath)
            Catch
            End Try
        End Try
    End Sub

    ' GUI-14: the output box and the runner's memory both stay bounded, and both keep the end.
    Private Sub CheckOutputBounds()
        Using box As New TextBox With {.Multiline = True}
            Dim h = box.Handle
            Dim pane As New OutputPane(box)
            For i = 1 To 200000
                pane.Add("output line " & i.ToString("D6") & " - some text to make it longer")
                If i Mod 5000 = 0 Then pane.Flush()
            Next
            pane.Flush()
            Check("output:pane-bounded", box.TextLength <= OutputPane.MaxChars AndAlso box.Text.TrimEnd().EndsWith("line 200000 - some text to make it longer"),
                  box.TextLength.ToString() & " chars")
        End Using

        Dim bounded As New BoundedText(64 * 1024, 1024 * 1024)
        For i = 1 To 500000
            bounded.AppendLine("line " & i.ToString("D6") & " of a long run")
        Next
        Dim kept = bounded.ToString()
        Check("output:runner-bounded", kept.Length < 64 * 1024 + 1024 * 1024 + 200 AndAlso kept.StartsWith("line 000001") AndAlso
                                       kept.TrimEnd().EndsWith("line 500000 of a long run") AndAlso kept.Contains("lines not kept"),
              kept.Length.ToString() & " chars")
    End Sub

    ' GUI-15: the size box takes what Atoi takes.
    Private Sub CheckParameters()
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            jv.SetTarget("E:")
            For Each bad In New String() {"1,000", "1e3", "2.5", "-5"}
                jv.SetSizeForTest(bad)
                Check("params:size-refused:" & bad, Not jv.RunStateForTest(reason), jv.CurrentCommand())
            Next
            jv.SetSizeForTest("1000")
            Check("params:size-accepted:1000", jv.RunStateForTest(reason) AndAlso jv.CurrentCommand().Contains(" test 1000"), jv.CurrentCommand())

            ' AUD-26-F1: `test` and `fill` take `del`; `speed` takes only `nodel` and `short`, and
            ' any other word is a usage error (CLI-25), so the Speed page never offers or writes `del`.
            jv.SetDeleteOptionsForTest(True, False)
            Check("params:test-keeps-del", Array.IndexOf(jv.CurrentCommand().Split(" "c), "del") >= 0, jv.CurrentCommand())
            jv.SetDeleteOptionsForTest(False, False)
            jv.SetJob(JobCatalogue.GetJob("rail_job_speed"))
            jv.SetTarget("E:")
            jv.SetDeleteOptionsForTest(True, False)
            Check("params:speed-no-del", Not jv.AutoDelOfferedForTest AndAlso
                  Array.IndexOf(jv.CurrentCommand().Split(" "c), "del") < 0, jv.CurrentCommand())
            jv.SetDeleteOptionsForTest(True, True)
            Check("params:speed-nodel-stays-free", jv.NoDelEnabledForTest AndAlso
                  Array.IndexOf(jv.CurrentCommand().Split(" "c), "nodel") >= 0, jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
        Check("params:decimal-invariant", Ui.IsDecimalNumber("2.5") AndAlso Not Ui.IsDecimalNumber("2,5") AndAlso Not Ui.IsWholeNumber("2.5"), "")
    End Sub

    ' AUD-28-F3: the check verb reads its Int options with Go's flag package, which parses base 0:
    ' "09" is a parse error after the run has started and "010" is octal 8. A value with a leading
    ' zero is therefore not a number to the panel - it is flagged and left off the line - while "0",
    ' "8" and "10" still go through, and a decimal option keeps its base-10 reading.
    Private Sub CheckCheckOptions()
        Using panel As New CheckOptionsPanel()
            Dim workers As TextBox = Nothing
            Dim minMb As TextBox = Nothing
            For Each c In AllControls(panel)
                If TypeOf c Is TextBox AndAlso c.AccessibleName = "--workers" Then workers = CType(c, TextBox)
                If TypeOf c Is TextBox AndAlso c.AccessibleName = "--min-mb" Then minMb = CType(c, TextBox)
            Next
            Check("check-options:fields-found", workers IsNot Nothing AndAlso minMb IsNot Nothing, "")
            If workers Is Nothing OrElse minMb Is Nothing Then Return
            For Each bad In New String() {"09", "010", "00"}
                workers.Text = bad
                Check("check-options:leading-zero:" & bad, panel.HasInvalidNumber() AndAlso
                      panel.ToArgs().IndexOf("--workers") < 0, String.Join(" ", panel.ToArgs()))
            Next
            For Each good In New String() {"0", "8", "10"}
                workers.Text = good
                Dim args = panel.ToArgs()
                Dim i = args.IndexOf("--workers")
                Check("check-options:whole-number:" & good, Not panel.HasInvalidNumber() AndAlso
                      i >= 0 AndAlso i + 1 < args.Count AndAlso args(i + 1) = good, String.Join(" ", args))
            Next
            workers.Text = ""
            minMb.Text = "010.5"
            Check("check-options:decimal-leading-zero-kept", Not panel.HasInvalidNumber() AndAlso
                  panel.ToArgs().IndexOf("--min-mb") >= 0, String.Join(" ", panel.ToArgs()))
        End Using
    End Sub

    ' GUI-12: "Clean files" opens Clean on the target that was tested.
    Private Sub CheckCleanCarriesTarget()
        Dim jv As JobView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            jv.CleanAfterRunForTest("Q:")
            Check("clean:carries-target", jv.CurrentJobId = "rail_job_clean" AndAlso jv.TargetTextForTest = "Q:" AndAlso
                                          jv.CurrentCommand().Contains("Q: clean"), jv.CurrentJobId & " / " & jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' SHELL-10: only the clipboard's own code is "another program is using it".
    Private Sub CheckCauses()
        Dim gdi As New Runtime.InteropServices.ExternalException("A generic error occurred in GDI+.", &H80004005)
        Check("cause:gdi", Problems.CauseKey(gdi) = "shell_cause_unexpected", Problems.CauseKey(gdi))
        Dim clip As New Runtime.InteropServices.ExternalException("Requested Clipboard operation did not succeed.", CInt(Problems.ClipboardCantOpen - &H100000000L))
        Check("cause:clipboard", Problems.CauseKey(clip) = "shell_cause_busy", Problems.CauseKey(clip))
        Dim seh As New Runtime.InteropServices.SEHException()
        Check("cause:seh", Problems.CauseKey(seh) = "shell_cause_unexpected", Problems.CauseKey(seh))
    End Sub

    ' SHELL-11: one duration format, with hours from one hour up.
    Private Sub CheckDurationFormat()
        Check("format:duration", Ui.FormatDuration(TimeSpan.FromMinutes(203)) = "3:23:00", Ui.FormatDuration(TimeSpan.FromMinutes(203)))
        Check("format:duration-short", Ui.FormatDuration(New TimeSpan(0, 5, 7)) = "05:07", Ui.FormatDuration(New TimeSpan(0, 5, 7)))
        Check("format:duration-days", Ui.FormatDuration(TimeSpan.FromHours(26.5)) = "26:30:00", Ui.FormatDuration(TimeSpan.FromHours(26.5)))
    End Sub

    ' SHELL-14: About and Send logs show the release stamp, or say the build is a local one.
    Private Sub CheckAboutStamp()
        Dim stamp = LogReport.BuildStamp()
        Check("about:stamp", Text.RegularExpressions.Regex.IsMatch(stamp, "^\d{10}$") OrElse stamp.StartsWith("dev"), stamp)
    End Sub

    ' SHELL-05: an archive older than a week is gone at the next Send logs; a new one stays.
    Private Sub CheckLogArchives()
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_logs_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim old = Path.Combine(dir, "filedo-logs-20260101-000000.zip")
            Dim young = Path.Combine(dir, "filedo-logs-20260920-000000.zip")
            Dim other = Path.Combine(dir, "someone-else.zip")
            For Each f In New String() {old, young, other}
                File.WriteAllText(f, "x")
                File.SetLastWriteTime(f, DateTime.Now.AddDays(If(f = young, -2, -30)))
            Next
            Dim removed = LogReport.SweepOldArchives(dir)
            Check("logs:old-archives-swept", removed = 1 AndAlso Not File.Exists(old) AndAlso File.Exists(young) AndAlso File.Exists(other),
                  removed.ToString())
        Finally
            Try
                Directory.Delete(dir, True)
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

    ' The paths hold a "(" on purpose (GUI-01): a page that read any text with a colon and a "(" as
    ' a drive row cut them to "C:", and the target row fails the moment that comes back.
    Private Function SampleTargetFor(job As JobDefinition) As String
        Select Case job.TargetKind
            Case JobDefinition.TargetType.File : Return "C:\sample (1)\one.fd-sec"
            Case JobDefinition.TargetType.Drive : Return "E:"
            Case Else : Return "C:\sample (1)\x"
        End Select
    End Function

End Module
