' SP-0204: the command guide - the dialog that opens from Browse commands.. and the Commands page. The rows hold
' the one table against the Command page's list (a verb added to the list cannot reach the dropdown without an
' explanation), every text against the five languages (a key that falls back to English or comes back as itself is
' a defect), the choosing path that sets the list, the dialog's three exits and the page's lazy build. Rows are
' named `opguide:`; FILEDO_SELFTEST_ONLY=opguide runs just these.
Partial Public Module SelfTest

    Private Sub CheckOperationGuide()
        CheckGuideTable()
        CheckGuideTexts()
        CheckGuideChoose()
        CheckGuidePicker()
        CheckGuidePage()
    End Sub

    ' A text that came back as its own key, or as the key of a neighbour, is not text.
    Private Function GuideIsRawKey(text As String) As Boolean
        Return text.StartsWith("op_", StringComparison.Ordinal) OrElse
               text.StartsWith("opfull_", StringComparison.Ordinal) OrElse
               text.StartsWith("opguide_", StringComparison.Ordinal) OrElse
               text.StartsWith("rail_", StringComparison.Ordinal) OrElse
               text.StartsWith("shell_", StringComparison.Ordinal)
    End Function

    Private Sub CheckGuideTable()
        Dim listWords = CommandView.Operations
        Dim tableWords = OperationGuide.Entries.Select(Function(e) e.Word).ToList()
        Dim missing = listWords.Where(Function(w) Not tableWords.Contains(w)).ToList()
        Dim extra = tableWords.Where(Function(w) Array.IndexOf(listWords, w) < 0).ToList()
        Check("opguide:covers-list",
              missing.Count = 0 AndAlso extra.Count = 0 AndAlso tableWords.Distinct().Count() = tableWords.Count,
              "missing: " & String.Join(",", missing.ToArray()) & "; extra: " & String.Join(",", extra.ToArray()))

        For Each group In OperationGuide.Entries.Select(Function(e) e.Group).Distinct().ToList()
            Check("opguide:group-known:" & group, Array.IndexOf(OperationGuide.Groups, group) >= 0, group)
        Next
        For Each group In OperationGuide.Groups
            Check("opguide:group-used:" & group, OperationGuide.EntriesOf(group).Count > 0, group)
        Next
        Check("opguide:rail-row", RailRow.All.Any(Function(r) r.Key = "rail_job_commands" AndAlso r.IsAlone), "")

        ' The examples are what is typed, so each is a filedo.exe line that names its verb.
        For Each entry In OperationGuide.Entries
            Dim verb = entry.Word.Split(" "c)(0)
            Check("opguide:example:" & entry.Word,
                  entry.Example.StartsWith("filedo.exe ", StringComparison.Ordinal) AndAlso entry.Example.Split(" "c).Contains(verb),
                  entry.Example)
        Next
    End Sub

    ' Every word has its short line and its full text in the language's own table (not the English fallback),
    ' and the full text keeps its three paragraphs: what it does, when to use it, what it changes.
    Private Sub CheckGuideTexts()
        Dim uiKeys = New String() {"opguide_intro", "opguide_use", "opguide_example_fmt", "opguide_browse", "opguide_browse_tip",
                                   "opguide_picker_title", "opguide_picker_intro", "opguide_picker_guide",
                                   "opguide_search", "opguide_search_none",
                                   "rail_job_commands", "purpose_job_commands"}
        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim d = Localization.GetDict(lang)
            For Each key In uiKeys
                Check("opguide:ui-key:" & lang & ":" & key, own.Contains(key) AndAlso d.ContainsKey(key) AndAlso d(key).Trim() <> "", "")
            Next
            For Each group In OperationGuide.Groups
                Dim key = OperationGuide.GroupKey(group)
                Check("opguide:group-key:" & lang & ":" & group, own.Contains(key) AndAlso d.ContainsKey(key) AndAlso d(key).Trim() <> "", key)
            Next
            For Each entry In OperationGuide.Entries
                Dim brief = If(d.ContainsKey(entry.ShortKey), d(entry.ShortKey), "")
                Dim full = If(d.ContainsKey(entry.FullKey), d(entry.FullKey), "")
                Dim paragraphs = full.Split(New String() {"\n"}, StringSplitOptions.None).Length
                Check("opguide:text:" & lang & ":" & entry.Word,
                      own.Contains(entry.ShortKey) AndAlso own.Contains(entry.FullKey) AndAlso
                      brief.Length >= 10 AndAlso brief.Length <= 150 AndAlso Not GuideIsRawKey(brief) AndAlso
                      full.Length >= 100 AndAlso Not GuideIsRawKey(full) AndAlso paragraphs = 3,
                      entry.Word & ": short " & brief.Length.ToString() & " chars, full " & full.Length.ToString() & " chars, " & paragraphs.ToString() & " paragraphs")
            Next
        Next
    End Sub

    ' Choosing from the picker or the page is ChooseOperation: it moves the Command page's list, and a word the
    ' list does not have moves nothing.
    Private Sub CheckGuideChoose()
        Dim view As CommandView = Nothing
        Try
            view = New CommandView()
            For Each entry In OperationGuide.Entries
                Dim ok = view.ChooseOperation(entry.Word)
                Check("opguide:choose:" & entry.Word, ok AndAlso view.CurrentOperationForTest() = entry.Word, view.CurrentOperationForTest())
            Next
            Dim before = view.CurrentOperationForTest()
            Check("opguide:choose-unknown",
                  Not view.ChooseOperation("no such operation") AndAlso view.CurrentOperationForTest() = before, view.CurrentOperationForTest())
        Catch ex As Exception
            Check("opguide:choose", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If view IsNot Nothing Then view.Dispose()
        End Try
    End Sub

    ' The dialog in every language: a radio button for each operation, the current one selected, no raw key on
    ' it, one leave path that is not the acting answer, the window within the screen, and the three exits.
    Private Sub CheckGuidePicker()
        Dim first = OperationGuide.EntriesOf(OperationGuide.Groups(0))(0).Word
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            Try
                Using dlg As New OperationPickerDialog(d, Nothing, "cd")
                    Dim raw = dlg.TextsForTest().Where(Function(t) GuideIsRawKey(t)).Take(5).ToList()
                    Check("opguide:picker:no-raw-keys:" & lang, raw.Count = 0, String.Join(",", raw.ToArray()))
                    Check("opguide:picker:rows:" & lang, dlg.RowCountForTest = OperationGuide.Entries.Length, dlg.RowCountForTest.ToString())
                    Check("opguide:picker:preselected:" & lang, dlg.Choice = "cd", dlg.Choice)
                    Check("opguide:picker:nothing-yet:" & lang, dlg.Result = OperationPickerResult.None, dlg.Result.ToString())
                    Check("opguide:picker:leave:" & lang,
                          dlg.CancelButton IsNot Nothing AndAlso dlg.AcceptButton IsNot Nothing AndAlso dlg.AcceptButton IsNot dlg.CancelButton, "")
                    Check("opguide:picker:fits-screen:" & lang,
                          dlg.ClientSize.Height <= Screen.PrimaryScreen.WorkingArea.Height AndAlso dlg.ClientSize.Width > 300, dlg.ClientSize.ToString())
                    dlg.SelectForTest("wipe")
                    dlg.UseForTest()
                    Check("opguide:picker:use:" & lang, dlg.Result = OperationPickerResult.Use AndAlso dlg.Choice = "wipe", dlg.Result.ToString() & " " & dlg.Choice)
                End Using
                Using dlg As New OperationPickerDialog(d, Nothing, "copy")
                    dlg.GuideForTest()
                    Check("opguide:picker:guide:" & lang, dlg.Result = OperationPickerResult.Guide AndAlso dlg.Choice = "copy", dlg.Result.ToString() & " " & dlg.Choice)
                End Using
                Using dlg As New OperationPickerDialog(d, Nothing, "")
                    Check("opguide:picker:default:" & lang, dlg.Choice = first, dlg.Choice)
                End Using
            Catch ex As Exception
                Check("opguide:picker:" & lang, False, ex.GetType().Name & ": " & ex.Message)
            End Try
        Next
    End Sub

    ' The page: built on first use and not before, one entry for each operation, no raw key on it, and a click
    ' on an entry's button names that operation.
    Private Sub CheckGuidePage()
        Dim page As OperationGuideView = Nothing
        Try
            page = New OperationGuideView()
            Check("opguide:page:lazy", Not page.BuiltForTest, "")
            page.EnsureBuilt()
            Dim words = page.EntryWordsForTest
            Check("opguide:page:entries",
                  words.Count = OperationGuide.Entries.Length AndAlso OperationGuide.Entries.All(Function(e) words.Contains(e.Word)), words.Count.ToString())
            Dim raw = page.TextsForTest().Where(Function(t) GuideIsRawKey(t)).Take(5).ToList()
            Check("opguide:page:no-raw-keys", raw.Count = 0, String.Join(",", raw.ToArray()))
            Dim got As String = ""
            AddHandler page.UseRequested, Sub(w) got = w
            page.UseForTest("fastcopy")
            Check("opguide:page:use", got = "fastcopy", got)

            ' Every entry starts collapsed; a click on the header opens it and a second one closes it.
            Check("opguide:page:collapsed", words.All(Function(w) Not page.IsExpandedForTest(w) OrElse w = "fastcopy"), "")
            page.ToggleForTest("info")
            Dim opened = page.IsExpandedForTest("info")
            page.ToggleForTest("info")
            Check("opguide:page:toggle", opened AndAlso Not page.IsExpandedForTest("info"), opened.ToString())

            ' The search narrows by name and by the words of the descriptions, in any case, and says so when
            ' nothing is left; an empty box brings every entry back.
            page.SearchForTest("WIPE")
            Dim found = page.ShownWordsForTest()
            Check("opguide:page:search-name", found.Contains("wipe") AndAlso found.Count < words.Count, String.Join(",", found.ToArray()))
            page.SearchForTest("zzzzqqqq")
            Check("opguide:page:search-none", page.ShownWordsForTest().Count = 0 AndAlso page.NoMatchShownForTest, "")
            page.SearchForTest("")
            Check("opguide:page:search-clear", page.ShownWordsForTest().Count = words.Count AndAlso Not page.NoMatchShownForTest,
                  page.ShownWordsForTest().Count.ToString())
            page.ApplyTheme()
        Catch ex As Exception
            Check("opguide:page", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If page IsNot Nothing Then page.Dispose()
        End Try
    End Sub

End Module
