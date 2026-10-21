Imports System.IO

' CLI-EVENT-STREAM rules 6, 7 and 16 (SP-0171 W13, item T1 of the SP-0165 report): a null inside a
' `result` event's `numbers` is an absent value, and the result card still draws. A JSON null reaches
' the window as Nothing; the card used to call ToString on it and lose the whole result.
'
' The rows build a real event line, read it with the real tailer and hand the parsed result to a real
' job page, so the chain the contract describes is the chain that is tested. Rows are named
' `result-numbers:`; FILEDO_SELFTEST_ONLY=result-numbers runs just these.
Partial Public Module SelfTest

    Private Sub CheckResultNumbers()
        CheckResultNumberLines()
        CheckResultNumbersThroughTheTailer()
    End Sub

    ' The pure part: which entries of the bag become lines.
    Private Sub CheckResultNumberLines()
        Check("result-numbers:no-bag", JobView.ResultNumberLines(Nothing).Count = 0 AndAlso
              JobView.ResultNumberLines(New Dictionary(Of String, Object)()).Count = 0, "")

        Dim bag As New Dictionary(Of String, Object)()
        bag("files") = 12
        bag("nothing") = Nothing
        bag("bytes") = 1024L
        Dim lines As List(Of String) = Nothing
        Dim thrown = ""
        Try
            lines = JobView.ResultNumberLines(bag)
        Catch ex As Exception
            thrown = ex.GetType().Name
        End Try
        Check("result-numbers:null-is-absent",
              thrown = "" AndAlso lines IsNot Nothing AndAlso lines.Count = 2 AndAlso lines(0) = "files: 12" AndAlso lines(1) = "bytes: 1024",
              If(thrown <> "", "threw " & thrown, If(lines Is Nothing, "none", String.Join(" | ", lines))))

        ' Only nulls: nothing to print, nothing thrown.
        Dim onlyNulls As New Dictionary(Of String, Object)()
        onlyNulls("a") = Nothing
        onlyNulls("b") = Nothing
        Check("result-numbers:all-null", JobView.ResultNumberLines(onlyNulls).Count = 0, "")

        ' An object or an array is no `key: value`; it is left out, not printed as a type name.
        Dim shapes As New Dictionary(Of String, Object)()
        shapes("n") = 3
        shapes("nested") = New Dictionary(Of String, Object) From {{"x", 1}}
        shapes("list") = New ArrayList() From {1, 2}
        shapes("word") = "text"
        Dim shaped = JobView.ResultNumberLines(shapes)
        Check("result-numbers:non-scalar-left-out", shaped.Count = 2 AndAlso shaped(0) = "n: 3" AndAlso shaped(1) = "word: text",
              String.Join(" | ", shaped))
    End Sub

    ' The whole chain: a result line with a null number is written to a file, read by the tailer, and the
    ' parsed result drawn on the job page's card.
    Private Sub CheckResultNumbersThroughTheTailer()
        Dim path = IO.Path.Combine(IO.Path.GetTempPath(), "filedo_selftest_numbers_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Dim jv As JobView = Nothing
        Try
            File.WriteAllText(path, "")
            Dim results As New List(Of EventStream.ResultInfo)()
            Dim steps As New List(Of String)()
            Using stream As New EventStream(path)
                AddHandler stream.ResultReceived, Sub(r) results.Add(r)
                AddHandler stream.StepChanged, Sub(n, d) steps.Add("[" & n & "][" & d & "]")

                ' A step whose fields are null reads as empty fields (rule 7), and is still a step.
                AppendText(path, "{""schemaVersion"":1,""kind"":""step"",""timestamp"":""2026-10-21T10:00:00.000+02:00"",""data"":{""name"":null,""description"":null}}" & vbLf)
                ' The result of the SP-0165 report: one number is null.
                AppendText(path, "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""2026-10-21T10:00:01.000+02:00"",""data"":{""verdict"":""Passed"",""numbers"":{""files"":12,""nothing"":null,""bytes"":1024},""filesLeft"":[],""reports"":[]}}" & vbLf)
                ' A whole bag that is null, and one that is not an object: both are an empty bag.
                AppendText(path, "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""2026-10-21T10:00:02.000+02:00"",""data"":{""verdict"":""Done"",""numbers"":null}}" & vbLf)
                AppendText(path, "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""2026-10-21T10:00:03.000+02:00"",""data"":{""verdict"":""Done"",""numbers"":7}}" & vbLf)
                stream.Poll()
            End Using

            Check("result-numbers:null-step-fields", steps.Count = 1 AndAlso steps(0) = "[][]", String.Join(" ", steps))
            Check("result-numbers:parsed", results.Count = 3 AndAlso results(0).Verdict = "Passed" AndAlso
                  results(0).Numbers IsNot Nothing AndAlso results(0).Numbers.ContainsKey("files") AndAlso
                  results(1).Numbers IsNot Nothing AndAlso results(1).Numbers.Count = 0 AndAlso
                  results(2).Numbers IsNot Nothing AndAlso results(2).Numbers.Count = 0, results.Count.ToString() & " results")
            If results.Count < 3 Then Return

            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            Dim thrown = ""
            Try
                jv.ShowRunResultForTest(New Runner.RunResult With {.Verdict = "Passed", .ExitCode = 0, .Duration = TimeSpan.Zero, .ResultInfo = results(0)}, False)
            Catch ex As Exception
                thrown = ex.GetType().Name
            End Try
            Dim text = jv.ResultNumbersTextForTest
            Check("result-numbers:card-draws", thrown = "" AndAlso jv.VerdictTextForTest = Localization.T("shell_verdict_passed"),
                  If(thrown <> "", "threw " & thrown, jv.VerdictTextForTest))
            Check("result-numbers:card-lists-the-numbers-it-can-read",
                  text.Contains("files: 12") AndAlso text.Contains("bytes: 1024") AndAlso Not text.Contains("nothing"),
                  text.Replace(vbCr, "").Replace(vbLf, " / "))

            ' A null or non-object bag leaves only the summary line.
            For i = 1 To 2
                thrown = ""
                Try
                    jv.ShowRunResultForTest(New Runner.RunResult With {.Verdict = "Done", .ExitCode = 0, .Duration = TimeSpan.Zero, .ResultInfo = results(i)}, False)
                Catch ex As Exception
                    thrown = ex.GetType().Name
                End Try
                Check("result-numbers:card-without-numbers:" & i.ToString(), thrown = "" AndAlso Not jv.ResultNumbersTextForTest.Contains(vbLf),
                      If(thrown <> "", "threw " & thrown, jv.ResultNumbersTextForTest.Replace(vbCr, "").Replace(vbLf, " / ")))
            Next
        Finally
            If jv IsNot Nothing Then jv.Dispose()
            Try
                File.Delete(path)
            Catch
            End Try
        End Try
    End Sub

End Module
