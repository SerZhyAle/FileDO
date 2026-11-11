Imports System.Globalization
Imports System.IO

' SP-0196 / CLI-EVENT-STREAM rule 17: a run that ends "Not proven" says why, in the language of the
' window, on both pages that show a result - the Job page's card and the Command page's verdict line -
' and never with the CLI's English sentence. A run with no token, or one this build does not know,
' gets the one plain sentence that points at the output; a bare exit code is never the only answer.
'
' The rows read every table, then build the chain the contract describes: a real event line is read by
' the real tailer, handed to the runner's own reason reader and drawn on a real page. Rows are named
' `why:`; FILEDO_SELFTEST_ONLY=why runs just these.
Partial Public Module SelfTest

    Private Const WhyMiB As Long = 1024L * 1024L
    Private Const WhyGiB As Long = 1024L * 1024L * 1024L

    Private Sub CheckRunExplanation()
        CheckWhyTables()
        CheckWhyEdges()
        CheckWhyDiskManager()
        CheckWhyReasonOf()
        CheckWhyThroughTheTailer()
        CheckWhyOnThePages()
    End Sub

    ' The owner's case: fill asked for 100 MB files on a drive with less than that free (the window
    ' sees the 96 MB of the sample; the CLI's own sentence is in Output and must never be shown).
    Private Function WhyFillResult() As Runner.RunResult
        Return New Runner.RunResult With {
            .ExitCode = 13, .Verdict = "Not proven", .Duration = TimeSpan.Zero,
            .Output = "Error: insufficient space to create even one file of 100 MB (0.09 GB free, 10.00 GB kept free)",
            .ErrorReason = RunExplanation.FillInsufficientSpace,
            .ErrorDetails = New Dictionary(Of String, Object) From {
                {"reason", RunExplanation.FillInsufficientSpace}, {"fileBytes", 100L * WhyMiB},
                {"freeBytes", 96L * WhyMiB}, {"reserveBytes", 10L * WhyGiB}}}
    End Function

    Private Function WhyResult(token As String, details As Dictionary(Of String, Object)) As Runner.RunResult
        Return New Runner.RunResult With {
            .ExitCode = 2, .Verdict = "Not proven", .Duration = TimeSpan.Zero, .Output = "",
            .ErrorReason = token, .ErrorDetails = details}
    End Function

    ' Every key in all five languages, the culture's own decimal mark and unit names, and each token's
    ' sentence in each language with its numbers filled in and no English left over.
    Private Sub CheckWhyTables()
        Dim sizes As New Dictionary(Of String, String()) From {
            {"en", New String() {"1.5 GB", "100 MB"}},
            {"ru", New String() {"1,5 ГБ", "100 МБ"}},
            {"uk", New String() {"1,5 ГБ", "100 МБ"}},
            {"de", New String() {"1,5 GB", "100 MB"}},
            {"fr", New String() {"1,5 Go", "100 Mo"}}}
        Dim enDict = Localization.GetDict("en")
        Dim enCulture = CultureInfo.GetCultureInfo("en")
        Dim fill = WhyFillResult()
        Dim tests = WhyResult(RunExplanation.TestInsufficientSpace, New Dictionary(Of String, Object) From {
            {"requiredBytes", 100L * WhyMiB}, {"availableBytes", 40L * WhyMiB}, {"freeBytes", 42L * WhyMiB}, {"reserveBytes", 0L}})
        Dim container = WhyResult(RunExplanation.ContainerInsufficientSpace, New Dictionary(Of String, Object) From {
            {"freeBytes", 3L * WhyGiB}, {"neededBytes", 8L * WhyGiB}})
        Dim full = WhyResult(RunExplanation.VolumeFull, Nothing)
        Dim inUse = WhyResult(RunExplanation.FileInUse, Nothing)
        Dim cases As New Dictionary(Of String, Runner.RunResult) From {
            {"fill", fill}, {"test", tests}, {"container", container}, {"volume-full", full}, {"file-in-use", inUse}}

        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim missing = RunExplanation.TableKeys.Where(Function(k) Not own.Contains(k)).ToList()
            Check("why:keys:" & lang, missing.Count = 0, String.Join(",", missing))

            Dim dict = Localization.GetDict(lang)
            Dim culture = CultureInfo.GetCultureInfo(lang)
            Dim onePointFive = RunExplanation.SizeText(1610612736L, dict, culture)
            Dim hundred = RunExplanation.SizeText(100L * WhyMiB, dict, culture)
            Check("why:size:" & lang, onePointFive = sizes(lang)(0) AndAlso hundred = sizes(lang)(1), onePointFive & " | " & hundred)

            For Each kv In cases
                Dim said = RunExplanation.SentenceFor(kv.Value, dict, culture)
                Dim english = RunExplanation.SentenceFor(kv.Value, enDict, enCulture)
                Check("why:sentence:" & kv.Key & ":" & lang,
                      said <> "" AndAlso Not said.Contains("{") AndAlso Not said.Contains(Localization.GetDict(lang)(RunExplanation.GenericKey)) AndAlso
                      (lang = "en" OrElse said <> english), lang & ": " & said)
            Next

            Dim fillSaid = RunExplanation.SentenceFor(fill, dict, culture)
            Dim fillNumbers = New String() {RunExplanation.SizeText(100L * WhyMiB, dict, culture),
                                            RunExplanation.SizeText(96L * WhyMiB, dict, culture),
                                            RunExplanation.SizeText(10L * WhyGiB, dict, culture)}
            Check("why:fill-numbers:" & lang, fillNumbers.All(Function(n) fillSaid.Contains(n)) AndAlso
                  Not fillSaid.Contains("insufficient") AndAlso Not fillSaid.Contains("0.09"), fillSaid)
        Next
    End Sub

    ' What gets a sentence and what does not, and what a number the contract names may and may not be.
    Private Sub CheckWhyEdges()
        Dim generic = Localization.T(RunExplanation.GenericKey)
        Dim dict = Localization.ForShell()
        Dim culture = CultureInfo.InvariantCulture

        Check("why:none-for-nothing", RunExplanation.SentenceFor(Nothing) = "", "")
        Check("why:no-token", RunExplanation.SentenceFor(WhyResult("", Nothing)) = generic, RunExplanation.SentenceFor(WhyResult("", Nothing)))
        Dim unknown = RunExplanation.SentenceFor(WhyResult("something-new", New Dictionary(Of String, Object) From {{"reason", "something-new"}}))
        Check("why:unknown-token-is-generic", unknown = generic AndAlso Not unknown.Contains("something-new"), unknown)

        For Each verdict In New String() {"Passed", "Done", "Failed", "Stopped"}
            Dim res = WhyFillResult()
            res.Verdict = verdict
            Check("why:only-not-proven:" & verdict, RunExplanation.SentenceFor(res) = "", verdict)
        Next
        Dim channel = WhyFillResult()
        channel.Reason = "shell_not_proven_reason"
        Check("why:channel-reason-keeps-its-own", RunExplanation.SentenceFor(channel) = "", "")

        ' A number the row names that cannot be read is no sentence with a zero in it.
        Dim unreadable As New Dictionary(Of String, Object()) From {
            {"absent", New Object() {Nothing}},
            {"null", New Object() {Nothing}},
            {"text", New Object() {"100"}},
            {"negative", New Object() {-1L}},
            {"fraction", New Object() {1.5R}}}
        For Each kv In unreadable
            Dim details As New Dictionary(Of String, Object) From {
                {"fileBytes", 100L * WhyMiB}, {"freeBytes", 96L * WhyMiB}}
            If kv.Key <> "absent" Then details("reserveBytes") = kv.Value(0)
            Dim said = RunExplanation.SentenceFor(WhyResult(RunExplanation.FillInsufficientSpace, details))
            Check("why:unreadable-number:" & kv.Key, said = generic, said)
        Next
        Dim whole As New Dictionary(Of String, Object) From {
            {"fileBytes", 100.0R}, {"freeBytes", 96L * WhyMiB}, {"reserveBytes", CInt(WhyMiB)}}
        Check("why:whole-number-in-any-json-type", RunExplanation.TokenSentence(RunExplanation.FillInsufficientSpace, whole, dict, culture) <> "", "")

        ' D3: the CLI's English is never quoted, even when it is all the run said.
        Dim onlyOutput = WhyResult("", Nothing)
        onlyOutput.Output = "Error: insufficient space to create even one file of 100 MB"
        Check("why:cli-english-not-quoted", Not RunExplanation.SentenceFor(onlyOutput).Contains("insufficient"), RunExplanation.SentenceFor(onlyOutput))
    End Sub

    ' SP-0202: the Disk Manager's failure line. A Disks run that ends Not proven with an exit class that has no
    ' sentence says the token's sentence when the token is one the window knows, and otherwise the fallback that
    ' points at the run report only when a report was kept. Both fallbacks exist in all five languages.
    Private Sub CheckWhyDiskManager()
        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim missing = New String() {"vd_why_report", "vd_why_no_report"}.Where(Function(k) Not own.Contains(k)).ToList()
            Check("why:disk-keys:" & lang, missing.Count = 0, String.Join(",", missing))
        Next

        Const report = "C:\FileDO\run-report.log"
        Dim bare = WhyResult("", Nothing)
        bare.ExitCode = 13
        Dim full = WhyResult(RunExplanation.VolumeFull, Nothing)
        full.ExitCode = 13
        Dim inUse = WhyResult(RunExplanation.FileInUse, Nothing)
        inUse.ExitCode = 13
        Dim unknown = WhyResult("something-new", Nothing)
        unknown.ExitCode = 13

        Check("why:disk-token-sentence", DiskManagerForm.DiskWhy(full, report) = Localization.T("shell_why_volume_full"),
              DiskManagerForm.DiskWhy(full, report))
        Check("why:disk-token-sentence-no-report", DiskManagerForm.DiskWhy(inUse, "") = Localization.T("shell_why_file_in_use"),
              DiskManagerForm.DiskWhy(inUse, ""))
        Check("why:disk-fallback-with-report", DiskManagerForm.DiskWhy(bare, report) = Localization.T("vd_why_report"),
              DiskManagerForm.DiskWhy(bare, report))
        Check("why:disk-fallback-no-report", DiskManagerForm.DiskWhy(bare, "") = Localization.T("vd_why_no_report"),
              DiskManagerForm.DiskWhy(bare, ""))
        Check("why:disk-unknown-token-falls-back", DiskManagerForm.DiskWhy(unknown, report) = Localization.T("vd_why_report"),
              DiskManagerForm.DiskWhy(unknown, report))
    End Sub

    ' The runner keeps the token of an `error` finding and nothing else.
    Private Sub CheckWhyReasonOf()
        Dim withToken As New Dictionary(Of String, Object) From {{"reason", "fill-insufficient-space"}}
        Check("why:reason-of:error", Runner.ReasonOf("error", withToken) = "fill-insufficient-space", "")
        Check("why:reason-of:other-type", Runner.ReasonOf("defect", withToken) = "" AndAlso Runner.ReasonOf("warning", withToken) = "", "")
        Check("why:reason-of:no-details", Runner.ReasonOf("error", Nothing) = "" AndAlso
              Runner.ReasonOf("error", New Dictionary(Of String, Object)()) = "", "")
        Check("why:reason-of:not-a-word", Runner.ReasonOf("error", New Dictionary(Of String, Object) From {{"reason", 5}}) = "" AndAlso
              Runner.ReasonOf("error", New Dictionary(Of String, Object) From {{"reason", ""}}) = "" AndAlso
              Runner.ReasonOf("error", New Dictionary(Of String, Object) From {{"reason", Nothing}}) = "", "")
    End Sub

    ' A real event line, the real tailer, the runner's own reason reader, the sentence.
    Private Sub CheckWhyThroughTheTailer()
        Dim path = IO.Path.Combine(IO.Path.GetTempPath(), "filedo_selftest_why_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            File.WriteAllText(path, "")
            Dim reason = ""
            Dim details As Dictionary(Of String, Object) = Nothing
            Dim verdict = ""
            Using stream As New EventStream(path)
                AddHandler stream.FindingReported,
                    Sub(t, m, d)
                        Dim token = Runner.ReasonOf(t, d)
                        If token <> "" AndAlso reason = "" Then
                            reason = token
                            details = d
                        End If
                    End Sub
                AddHandler stream.ResultReceived, Sub(r) verdict = r.Verdict
                AppendText(path, "{""schemaVersion"":1,""kind"":""finding"",""timestamp"":""2026-11-10T10:00:00.000+01:00"",""data"":{""type"":""error"",""message"":""insufficient space to create even one file of 100 MB (0.09 GB free, 10.00 GB kept free)"",""details"":{""reason"":""fill-insufficient-space"",""fileBytes"":104857600,""freeBytes"":100663296,""reserveBytes"":10737418240}}}" & vbLf)
                AppendText(path, "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""2026-11-10T10:00:01.000+01:00"",""data"":{""verdict"":""Not proven"",""numbers"":{},""filesLeft"":[],""reports"":[]}}" & vbLf)
                stream.Poll()
            End Using
            Check("why:tailer:token", reason = "fill-insufficient-space" AndAlso verdict = "Not proven", reason & " / " & verdict)

            Dim res = New Runner.RunResult With {.ExitCode = 13, .Verdict = verdict, .Duration = TimeSpan.Zero, .ErrorReason = reason, .ErrorDetails = details}
            Dim said = RunExplanation.SentenceFor(res)
            Dim dict = Localization.ForShell()
            Dim culture = CultureInfo.GetCultureInfo(ShellSettings.Language())
            Check("why:tailer:sentence", said <> "" AndAlso said.Contains(RunExplanation.SizeText(100L * WhyMiB, dict, culture)) AndAlso
                  said.Contains(RunExplanation.SizeText(96L * WhyMiB, dict, culture)) AndAlso
                  said.Contains(RunExplanation.SizeText(10L * WhyGiB, dict, culture)), said)
        Finally
            Try
                File.Delete(path)
            Catch
            End Try
        End Try
    End Sub

    ' The two pages that show a result: the Job page's card and the Command page's verdict line.
    Private Sub CheckWhyOnThePages()
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Try
            Dim generic = Localization.T(RunExplanation.GenericKey)
            Dim fill = WhyFillResult()
            Dim said = RunExplanation.SentenceFor(fill)

            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            jv.ShowRunResultForTest(fill, False)
            Check("why:card:fill", said <> "" AndAlso jv.VerdictReasonForTest = said AndAlso Not jv.VerdictReasonForTest.Contains("insufficient"), jv.VerdictReasonForTest)
            Check("why:card:keeps-the-exit-code", jv.ResultNumbersTextForTest.Contains("13"), jv.ResultNumbersTextForTest)

            jv.ShowRunResultForTest(WhyResult("something-new", Nothing), False)
            Check("why:card:unknown-token", jv.VerdictReasonForTest = generic, jv.VerdictReasonForTest)
            jv.ShowRunResultForTest(WhyResult("", Nothing), False)
            Check("why:card:no-token", jv.VerdictReasonForTest = generic, jv.VerdictReasonForTest)

            Dim channel = WhyFillResult()
            channel.Reason = "shell_not_proven_reason"
            jv.ShowRunResultForTest(channel, False)
            Check("why:card:channel-reason-alone", jv.VerdictReasonForTest = Localization.T("shell_not_proven_reason"), jv.VerdictReasonForTest)

            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Passed", .Duration = TimeSpan.Zero}, False)
            Check("why:card:passed-says-nothing", jv.VerdictReasonForTest = "", jv.VerdictReasonForTest)

            ' A container page that already has its own sentence for the run does not say it twice.
            jv.SetJob(JobCatalogue.GetJob("rail_job_unsecure"))
            Dim destName = Localization.Format(Localization.T("shell_fdsec_dest_name"), FdsecResults.RefusedCharacters)
            Dim named = WhyResult(RunExplanation.DestinationName, New Dictionary(Of String, Object) From {{"reason", RunExplanation.DestinationName}})
            named.ExitCode = 5
            named.Output = "Error: the name sealed in this container " & FdsecResults.DestinationNameRefusal & vbLf
            jv.ShowRunResultForTest(named, False)
            Dim shown = jv.VerdictReasonForTest
            Check("why:card:destination-name-once", destName <> "" AndAlso shown.Contains(destName) AndAlso
                  shown.IndexOf(destName, StringComparison.Ordinal) = shown.LastIndexOf(destName, StringComparison.Ordinal), shown)

            Dim noRoom = WhyResult(RunExplanation.ContainerInsufficientSpace, New Dictionary(Of String, Object) From {
                {"reason", RunExplanation.ContainerInsufficientSpace}, {"freeBytes", 3L * WhyGiB}, {"neededBytes", 8L * WhyGiB}})
            noRoom.ExitCode = 5
            jv.ShowRunResultForTest(noRoom, False)
            Check("why:card:container-space", jv.VerdictReasonForTest = RunExplanation.SentenceFor(noRoom) AndAlso jv.VerdictReasonForTest <> generic, jv.VerdictReasonForTest)

            ' A disk page keeps its exit-class sentence and adds no second one.
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_mount"))
            jv.SetTarget(DiskSample)
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 7, .Verdict = "Not proven", .Output = "", .Duration = TimeSpan.Zero}, False)
            Check("why:card:disk-keeps-its-own", jv.VerdictReasonForTest.Contains(Localization.T("vd_exit_7")) AndAlso Not jv.VerdictReasonForTest.Contains(generic), jv.VerdictReasonForTest)

            cv = New CommandView()
            cv.ShowRunResultForTest(fill)
            Check("why:command:fill", said <> "" AndAlso cv.VerdictLabelForTest.Text.Contains(said) AndAlso Not cv.VerdictLabelForTest.Text.Contains("insufficient"), cv.VerdictLabelForTest.Text)
            cv.ShowRunResultForTest(WhyResult("something-new", Nothing))
            Check("why:command:unknown-token", cv.VerdictLabelForTest.Text.Contains(generic), cv.VerdictLabelForTest.Text)
            cv.ShowRunResultForTest(channel)
            Check("why:command:channel-reason-alone", cv.VerdictLabelForTest.Text.Contains(Localization.T("shell_not_proven_reason")) AndAlso
                  Not cv.VerdictLabelForTest.Text.Contains(generic), cv.VerdictLabelForTest.Text)
            cv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Passed", .Duration = TimeSpan.Zero})
            Check("why:command:passed-says-nothing", Not cv.VerdictLabelForTest.Text.Contains(generic) AndAlso Not cv.VerdictLabelForTest.Text.Contains(said), cv.VerdictLabelForTest.Text)
        Finally
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

End Module
