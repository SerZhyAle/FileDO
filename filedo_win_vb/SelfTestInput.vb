Imports System.IO
Imports System.Text.RegularExpressions

' SP-0171 W10 and W11: the meanings the catalog added after the first import, and the keyboard's way
' round the windows.
'
'   icons-use - ICON-SET 0.33 action.retry and 0.35 app.tour: the vendored drawings, the catalog's names
'               on the controls that show them (rule 3), and the drawing each control asks for.
'   input     - INPUT-PARITY 0.4 section 3.2 rules 1 and 5: F6 / Shift+F6 between the areas of a window
'               (the choice proven on synthetic pane lists, then on real controls and the two real
'               windows) and the key to the Disk Manager's Help menu.
'
' What a person must still look at - the menu drawn, the focus ring on the area F6 lands in, a
' keyboard-only walk of every page - is not here (INPUT-PARITY rule 2 walk, rendered captures).
Partial Public Module SelfTest

    ' The folder the shell's sources are in, found the way MappedVocabularyIds finds it: beside the
    ' exe (bin\Release) or one folder below the repository root (exe_to_download). Nothing when the
    ' exe runs somewhere else, which the rows that need the sources then say.
    Private Function FindShellSources() As DirectoryInfo
        Dim dir = New FileInfo(Application.ExecutablePath).Directory
        For i = 1 To 6
            If dir Is Nothing Then Exit For
            If dir.GetFiles("ShellDialog.vb").Length > 0 Then Return dir
            Dim nested = Path.Combine(dir.FullName, "filedo_win_vb")
            If Directory.Exists(nested) AndAlso New DirectoryInfo(nested).GetFiles("ShellDialog.vb").Length > 0 Then Return New DirectoryInfo(nested)
            dir = dir.Parent
        Next
        Return Nothing
    End Function

    ' ---- icons-use: action.retry and app.tour -----------------------------------------------------

    ' The catalog's names (vocabulary.jsonl at ICON-SET 0.36) and the words of the neighbouring meaning
    ' that must not stand in for them: action.redo is "Повторить" / "Повторити" / "Wiederholen" /
    ' "Refaire". The record of action.retry carries three names; German and French are the product's own
    ' qualified forms, which the contract allows and which are not the redo words.
    Private Sub CheckIconMeanings()
        Check("icons:retry:vendored", Glyphs.IsDrawable("action.retry"), "assets\glyphs\action.retry.svg, embedded and verified")
        Check("icons:tour:vendored", Glyphs.IsDrawable("app.tour"), "assets\glyphs\app.tour.svg, embedded and verified")

        Dim retryName As New Dictionary(Of String, String) From {{"en", "Retry"}, {"ru", "Повторить попытку"}, {"uk", "Повторити спробу"}}
        Dim redoName As New Dictionary(Of String, String) From {{"en", "Redo"}, {"ru", "Повторить"}, {"uk", "Повторити"}, {"de", "Wiederholen"}, {"fr", "Refaire"}}
        Dim tourName As New Dictionary(Of String, String) From {
            {"en", "Welcome tour"}, {"ru", "Знакомство с программой"}, {"uk", "Знайомство з програмою"}, {"de", "Einführung"}, {"fr", "Visite guidée"}}
        For Each lang In Localization.Languages
            Dim dict = Localization.GetDict(lang)
            Dim retry = dict("shell_btn_retry")
            Dim named As String = Nothing
            If retryName.TryGetValue(lang, named) Then Check("icons:retry:name:" & lang, retry = named, retry & " vs the catalog's " & named)
            Check("icons:retry:not-redo:" & lang, retry <> redoName(lang), retry)
            Dim tour = dict("vd_help_menu_first")
            Check("icons:tour:name:" & lang, tour = tourName(lang), tour & " vs the catalog's " & tourName(lang))
            ' The welcome window names the route to itself; the route is the menu entry's own label.
            Check("icons:tour:hint-names-the-entry:" & lang, dict("vd_welcome_hint").Contains(tour), dict("vd_welcome_hint"))
        Next

        ' Every Retry button asks for action.retry (never the refresh arrow), and there is no Retry button
        ' this scan does not see: the count of the label's uses equals the count of the well-formed calls.
        Dim src = FindShellSources()
        If src Is Nothing Then
            Check("icons:retry:buttons-draw-retry", False, "the shell's sources were not found beside the exe, so the call sites could not be read")
            Return
        End If
        Dim call_ As New Regex("Localization\.T\(""shell_btn_retry""\)\s*,\s*offerLogs:=False,\s*actionGlyph:=GlyphRef\.Vocabulary\(""([a-z.-]+)""\)")
        Dim uses = 0, wellFormed = 0, drawn As New List(Of String)
        For Each srcFile In src.GetFiles("*.vb")
            If srcFile.Name.StartsWith("Localization", StringComparison.Ordinal) OrElse srcFile.Name.StartsWith("SelfTest", StringComparison.Ordinal) Then Continue For
            Dim text = File.ReadAllText(srcFile.FullName)
            uses += Regex.Matches(text, """shell_btn_retry""").Count
            For Each m As Match In call_.Matches(text)
                wellFormed += 1
                drawn.Add(srcFile.Name & ":" & m.Groups(1).Value)
            Next
        Next
        Check("icons:retry:buttons-draw-retry", uses >= 2 AndAlso uses = wellFormed AndAlso drawn.All(Function(d) d.EndsWith(":action.retry", StringComparison.Ordinal)),
              uses.ToString() & " uses, " & wellFormed.ToString() & " calls: " & String.Join(", ", drawn.ToArray()))
    End Sub

    ' ---- input: F6 and Shift+F6 --------------------------------------------------------------------

    Private Sub CheckInputParity()
        CheckPaneChoice()
        CheckPaneKeysAreInTheTable()
        CheckPaneCycleOnControls()
        CheckPaneCycleShell()
        CheckPaneCycleDiskManager()
        CheckHelpMenuKey()
    End Sub

    ' The choice of the next area, on synthetic lists: it wraps, steps over what cannot take the focus,
    ' and Shift reverses it.
    Private Sub CheckPaneChoice()
        Dim all4 As New List(Of Boolean) From {True, True, True, True}
        Check("input:pane:forward", PaneCycle.NextIndex(all4, 0, False) = 1 AndAlso PaneCycle.NextIndex(all4, 2, False) = 3,
              PaneCycle.NextIndex(all4, 0, False).ToString())
        Check("input:pane:wraps-forward", PaneCycle.NextIndex(all4, 3, False) = 0, PaneCycle.NextIndex(all4, 3, False).ToString())
        Check("input:pane:backward", PaneCycle.NextIndex(all4, 2, True) = 1, PaneCycle.NextIndex(all4, 2, True).ToString())
        Check("input:pane:wraps-backward", PaneCycle.NextIndex(all4, 0, True) = 3, PaneCycle.NextIndex(all4, 0, True).ToString())

        Dim gaps As New List(Of Boolean) From {True, False, True, False}
        Check("input:pane:skips-unusable", PaneCycle.NextIndex(gaps, 0, False) = 2 AndAlso PaneCycle.NextIndex(gaps, 2, False) = 0 AndAlso
                                           PaneCycle.NextIndex(gaps, 0, True) = 2 AndAlso PaneCycle.NextIndex(gaps, 2, True) = 0,
              PaneCycle.NextIndex(gaps, 0, False).ToString() & "," & PaneCycle.NextIndex(gaps, 2, False).ToString())
        ' The focus can sit in an area that has just become unusable: the walk still leaves it for the next usable one.
        Check("input:pane:leaves-an-unusable-current", PaneCycle.NextIndex(gaps, 1, False) = 2 AndAlso PaneCycle.NextIndex(gaps, 1, True) = 0 AndAlso
                                                       PaneCycle.NextIndex(gaps, 3, False) = 0 AndAlso PaneCycle.NextIndex(gaps, 3, True) = 2, "")
        Dim alone As New List(Of Boolean) From {False, True, False}
        Check("input:pane:only-one-stays", PaneCycle.NextIndex(alone, 1, False) = 1 AndAlso PaneCycle.NextIndex(alone, 1, True) = 1, "")
        Check("input:pane:none-usable", PaneCycle.NextIndex(New List(Of Boolean) From {False, False}, 0, False) = -1 AndAlso
                                        PaneCycle.NextIndex(New List(Of Boolean)(), -1, False) = -1, "")
        Check("input:pane:from-outside", PaneCycle.NextIndex(gaps, -1, False) = 0 AndAlso PaneCycle.NextIndex(gaps, -1, True) = 2 AndAlso
                                         PaneCycle.NextIndex(gaps, 9, False) = 0 AndAlso PaneCycle.NextIndex(gaps, 9, True) = 2, "")

        ' Every list of up to six areas, every usable one as the start: forward then Shift is the start again,
        ' and as many presses as there are usable areas visit each of them once and come back.
        Dim bad = 0, cases = 0
        For n = 1 To 6
            For mask = 0 To (1 << n) - 1
                Dim usable As New List(Of Boolean)
                For i = 0 To n - 1
                    usable.Add((mask And (1 << i)) <> 0)
                Next
                Dim count = usable.Where(Function(u) u).Count()
                For start = 0 To n - 1
                    If Not usable(start) Then Continue For
                    cases += 1
                    If PaneCycle.NextIndex(usable, PaneCycle.NextIndex(usable, start, False), True) <> start Then bad += 1
                    If PaneCycle.NextIndex(usable, PaneCycle.NextIndex(usable, start, True), False) <> start Then bad += 1
                    Dim seen As New HashSet(Of Integer)
                    Dim at = start
                    For k = 1 To count
                        at = PaneCycle.NextIndex(usable, at, False)
                        If at < 0 OrElse Not usable(at) Then bad += 1 : Exit For
                        seen.Add(at)
                    Next
                    If at <> start OrElse seen.Count <> count Then bad += 1
                Next
            Next
        Next
        Check("input:pane:exhaustive", bad = 0 AndAlso cases > 100, cases.ToString() & " cases, " & bad.ToString() & " wrong")
    End Sub

    ' Rule 5: the key is in the data table the help window, the help page and the tooltips read, and the
    ' Menu / Shift+F10 key that opens a row's menu opens the Help button's with that button focused.
    Private Sub CheckPaneKeysAreInTheTable()
        Dim fwd = DiskShortcuts.Find(Keys.F6, False)
        Dim back = DiskShortcuts.Find(Keys.Shift Or Keys.F6, False)
        Check("input:key:f6-in-table", fwd IsNot Nothing AndAlso fwd.Command = DiskCommand.NextPane AndAlso Not fwd.ListOnly, "")
        Check("input:key:shift-f6-in-table", back IsNot Nothing AndAlso back.Command = DiskCommand.PreviousPane AndAlso Not back.ListOnly, "")
        Dim rows = DiskShortcuts.HelpRows()
        Check("input:key:f6-in-help-rows", rows.Any(Function(r) r.Key = "F6 / Shift+F6" AndAlso r.Value = "vd_mgr_key_pane"),
              String.Join(" | ", rows.Select(Function(r) r.Key).ToArray()))
        For Each chord In New Keys() {Keys.Apps, Keys.Shift Or Keys.F10}
            Dim name = DiskShortcuts.KeyText(chord)
            Dim onButton = DiskShortcuts.Find(chord, False, True)
            Check("input:key:help-button-menu:" & name, onButton IsNot Nothing AndAlso onButton.Command = DiskCommand.ContextMenu AndAlso onButton.OnHelpButton, name)
            Check("input:key:menu-only-where-it-means-something:" & name,
                  DiskShortcuts.Find(chord, False, False) Is Nothing AndAlso DiskShortcuts.Find(chord, True, False) IsNot Nothing, name)
        Next
    End Sub

    ' The same walk on real controls in a shown form: a hidden area and an area with nothing enabled are
    ' stepped over, a preferred control wins when it can take the focus, a hidden one does not.
    Private Sub CheckPaneCycleOnControls()
        Using f As New Form()
            Dim hosts As New List(Of Panel)
            Dim fields As New List(Of TextBox)
            For i = 0 To 3
                Dim host As New Panel With {.Dock = DockStyle.Top, .Height = 40}
                Dim first As New TextBox With {.Left = 4, .Top = 4}
                Dim second As New TextBox With {.Left = 120, .Top = 4}
                host.Controls.Add(first)
                host.Controls.Add(second)
                f.Controls.Add(host)
                hosts.Add(host)
                fields.Add(first)
                fields.Add(second)
            Next
            ' Docked top controls stack the last added first; the panes are listed in the order wanted.
            hosts(1).Visible = False
            fields(4).Enabled = False
            fields(5).Enabled = False
            f.StartPosition = FormStartPosition.Manual
            f.ShowInTaskbar = False
            f.Bounds = New Rectangle(-32000, -32000, 400, 300)
            f.Show()
            Settle()

            Dim panes As New List(Of Pane) From {
                New Pane(hosts(0)), New Pane(hosts(1)), New Pane(hosts(2)), New Pane(hosts(3), Function() fields(7))}
            ' Area 1 is hidden and area 2 holds nothing enabled: from area 0 forward the next is area 3,
            ' and it lands on the preferred field.
            Check("input:pane:controls:skips-hidden-and-disabled", PaneCycle.Target(panes, fields(0), False) Is fields(7), "")
            Check("input:pane:controls:wraps", PaneCycle.Target(panes, fields(7), False) Is fields(0), "")
            Check("input:pane:controls:shift-reverses", PaneCycle.Target(panes, fields(0), True) Is fields(7) AndAlso PaneCycle.Target(panes, fields(7), True) Is fields(0), "")
            Check("input:pane:controls:from-outside", PaneCycle.Target(panes, Nothing, False) Is fields(0) AndAlso PaneCycle.Target(panes, Nothing, True) Is fields(7), "")
            Check("input:pane:controls:pane-of", PaneCycle.PaneOf(panes, fields(1)) = 0 AndAlso PaneCycle.PaneOf(panes, fields(6)) = 3 AndAlso
                                                 PaneCycle.PaneOf(panes, New TextBox()) = -1, "")
            ' A preferred control that is hidden or outside the area is not landed on: the first stop is.
            fields(7).Visible = False
            Check("input:pane:controls:prefer-needs-a-stop", PaneCycle.Target(panes, fields(0), False) Is fields(6), "")
            fields(7).Visible = True
            Check("input:pane:controls:first-stop-is-in-tab-order", PaneCycle.FirstStop(hosts(0)) Is fields(0), "")
            fields(0).TabIndex = 5
            fields(1).TabIndex = 2
            Check("input:pane:controls:tab-index-decides", PaneCycle.FirstStop(hosts(0)) Is fields(1), "")
            ' Nothing usable at all: the key is left to the system.
            For Each p In panes
                p.Host.Enabled = False
            Next
            Check("input:pane:controls:none-leaves-the-key", PaneCycle.Target(panes, fields(0), False) Is Nothing, "")
            Check("input:pane:controls:move-says-no", Not PaneCycle.Move(f, panes, False), "")
        End Using
    End Sub

    ' The shell: rail -> the Help button in the header -> the page, and back with Shift; the rail is
    ' entered on the row of the page on screen.
    Private Sub CheckPaneCycleShell()
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            shell.StartPosition = FormStartPosition.Manual
            shell.ShowInTaskbar = False
            shell.Show()
            shell.Bounds = New Rectangle(-32000, -32000, shell.MinimumSize.Width + Ui.Px(shell, 200), shell.MinimumSize.Height + Ui.Px(shell, 200))
            shell.ShowPageForCapture("rail_job_command", "")
            Settle()
            Dim row = shell.RailEntriesForTest().FirstOrDefault(Function(r) r.Selected)
            Check("input:shell:page-row-selected", row IsNot Nothing, "")
            If row Is Nothing Then Return
            row.Select()
            Check("input:shell:starts-in-the-rail", shell.PaneIndexForTest() = 0, shell.PaneIndexForTest().ToString())

            Dim took As New List(Of Boolean)
            Dim visited As New List(Of Integer)
            For i = 1 To 4
                took.Add(shell.HandleKeyForTest(Keys.F6))
                visited.Add(shell.PaneIndexForTest())
            Next
            Check("input:shell:f6-takes-the-key", took.All(Function(t) t), String.Join(",", took.Select(Function(t) t.ToString()).ToArray()))
            Check("input:shell:f6-rail-help-page-rail-help", visited.SequenceEqual(New Integer() {1, 2, 0, 1}), String.Join(",", visited.Select(Function(v) v.ToString()).ToArray()))
            Check("input:shell:help-button-is-the-header-stop", Help.FocusedControl(shell) Is shell.HelpButtonForTest, Help.FocusedControl(shell)?.GetType().Name)

            ' Shift+F6 from the page walks back: header, rail (on the page's row), page.
            Dim back As New List(Of Integer)
            row.Select()
            shell.HandleKeyForTest(Keys.F6)
            shell.HandleKeyForTest(Keys.F6)
            Check("input:shell:on-the-page", shell.PaneIndexForTest() = 2, shell.PaneIndexForTest().ToString())
            For i = 1 To 3
                Check("input:shell:shift-f6-takes-the-key:" & i.ToString(), shell.HandleKeyForTest(Keys.Shift Or Keys.F6), "")
                back.Add(shell.PaneIndexForTest())
            Next
            Check("input:shell:shift-f6-reverses", back.SequenceEqual(New Integer() {1, 0, 2}), String.Join(",", back.Select(Function(v) v.ToString()).ToArray()))
            ' From the page, F6 wraps to the rail and lands on the row of the page on screen, not on the first row.
            shell.HandleKeyForTest(Keys.F6)
            Check("input:shell:rail-is-entered-on-the-page-row", Help.FocusedControl(shell) Is row, Help.FocusedControl(shell)?.GetType().Name)
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

    ' The Disk Manager: the areas in Tab order, wrapping, and Shift+F6 retracing the same path.
    Private Sub CheckPaneCycleDiskManager()
        Dim wasReads = DiskManagerForm.SuppressReads
        Dim wasWelcome = DiskManagerForm.SuppressWelcome
        Dim m As DiskManagerForm = Nothing
        Try
            DiskManagerForm.SuppressReads = True
            DiskManagerForm.SuppressWelcome = True
            m = New DiskManagerForm()
            m.StartPosition = FormStartPosition.Manual
            m.ShowInTaskbar = False
            m.Show()
            m.Bounds = New Rectangle(-32000, -32000, m.MinimumSize.Width + Ui.Px(m, 300), m.MinimumSize.Height + Ui.Px(m, 200))
            Dim snap = GoldenSnapshot()
            m.ApplySnapshotForTest(snap, "")
            m.SelectForTest(snap.Disks.First(Function(d) d.Name = "work").Key)
            Settle()
            Dim hosts = m.PaneHostsForTest()
            Check("input:dm:six-areas-in-tab-order", hosts.Count = 6 AndAlso m.TabOrderForTest.Zip(m.TabOrderForTest.Skip(1), Function(x, y) x < y).All(Function(ok) ok) AndAlso
                                                   hosts(0).TabIndex < hosts(1).TabIndex AndAlso hosts(1).TabIndex < hosts(2).TabIndex AndAlso hosts(2).TabIndex < hosts(3).TabIndex AndAlso
                                                   hosts(3).TabIndex < hosts(4).TabIndex AndAlso hosts(4).TabIndex < hosts(5).TabIndex, hosts.Count.ToString())

            m.FilterBoxForTest.Select()
            Check("input:dm:starts-in-the-filter", m.PaneIndexForTest() = 1, m.PaneIndexForTest().ToString())
            Dim forward As New List(Of Integer)
            Dim allTaken = True
            For i = 1 To hosts.Count + 2
                allTaken = allTaken AndAlso m.HandleKeyForTest(Keys.F6)
                forward.Add(m.PaneIndexForTest())
            Next
            Check("input:dm:f6-takes-the-key", allTaken, "")
            ' The areas the walk visits, in order, are the usable ones: it climbs the Tab order and wraps once per lap.
            Dim lap = forward.Distinct().OrderBy(Function(x) x).ToList()
            Dim climbs = True
            For i = 0 To forward.Count - 1
                Dim previous = If(i = 0, 1, forward(i - 1))
                Dim wanted = lap.FirstOrDefault(Function(x) x > previous)
                If wanted = 0 AndAlso Not lap.Any(Function(x) x > previous) Then wanted = lap(0)
                If forward(i) <> wanted Then climbs = False
            Next
            Check("input:dm:f6-climbs-the-tab-order-and-wraps", climbs AndAlso lap.Contains(0) AndAlso lap.Contains(1) AndAlso lap.Contains(2) AndAlso forward.Count > lap.Count,
                  String.Join(",", forward.Select(Function(v) v.ToString()).ToArray()))
            Check("input:dm:details-bar-is-out-while-details-are-open", Not lap.Contains(4), String.Join(",", lap.Select(Function(v) v.ToString()).ToArray()))

            ' Shift+F6 from the list goes to the filter, and from the toolbar wraps to the last usable area.
            m.ListForTest.Select()
            Check("input:dm:shift-f6-list-to-filter", m.HandleKeyForTest(Keys.Shift Or Keys.F6) AndAlso m.PaneIndexForTest() = 1, m.PaneIndexForTest().ToString())
            Check("input:dm:shift-f6-filter-to-toolbar", m.HandleKeyForTest(Keys.Shift Or Keys.F6) AndAlso m.PaneIndexForTest() = 0, m.PaneIndexForTest().ToString())
            Check("input:dm:shift-f6-wraps", m.HandleKeyForTest(Keys.Shift Or Keys.F6) AndAlso m.PaneIndexForTest() = lap(lap.Count - 1), m.PaneIndexForTest().ToString())
            Dim there = m.PaneIndexForTest()
            Check("input:dm:f6-then-shift-f6-is-the-start", m.HandleKeyForTest(Keys.F6) AndAlso m.HandleKeyForTest(Keys.Shift Or Keys.F6) AndAlso m.PaneIndexForTest() = there, m.PaneIndexForTest().ToString())

            ' The details area is left out of the walk when the details are hidden - the bar that brings them back
            ' takes its place - and returns with them.
            m.ToggleDetailForTest()
            Settle()
            m.FilterBoxForTest.Select()
            Dim hidden As New List(Of Integer)
            For i = 1 To hosts.Count + 1
                m.HandleKeyForTest(Keys.F6)
                hidden.Add(m.PaneIndexForTest())
            Next
            m.ToggleDetailForTest()
            Settle()
            Check("input:dm:hidden-area-is-skipped", Not hidden.Contains(3) AndAlso hidden.Contains(4) AndAlso hidden.Contains(2), String.Join(",", hidden.Select(Function(v) v.ToString()).ToArray()))
        Finally
            DiskManagerForm.SuppressReads = wasReads
            DiskManagerForm.SuppressWelcome = wasWelcome
            If m IsNot Nothing Then m.Dispose()
        End Try
    End Sub

    ' INPUT-PARITY 3.2 rule 1: the Help button's menu has a key and a visible entry.
    Private Sub CheckHelpMenuKey()
        Dim wasReads = DiskManagerForm.SuppressReads
        Dim wasWelcome = DiskManagerForm.SuppressWelcome
        Dim m As DiskManagerForm = Nothing
        Try
            DiskManagerForm.SuppressReads = True
            DiskManagerForm.SuppressWelcome = True
            m = New DiskManagerForm()
            m.StartPosition = FormStartPosition.Manual
            m.ShowInTaskbar = False
            m.Show()
            m.Bounds = New Rectangle(-32000, -32000, m.MinimumSize.Width + Ui.Px(m, 300), m.MinimumSize.Height + Ui.Px(m, 200))
            m.ApplySnapshotForTest(GoldenSnapshot(), "")
            Settle()
            Dim words = Localization.GetDict(ShellSettings.Language())

            Dim opened = 0
            m.HelpMenuOpenedForTest = Sub() opened += 1
            m.HelpButtonForTest.Select()
            Check("input:help-menu:button-has-the-focus", Help.FocusedControl(m) Is m.HelpButtonForTest, Help.FocusedControl(m)?.GetType().Name)
            Dim byShiftF10 = m.HandleKeyForTest(Keys.Shift Or Keys.F10)
            Dim byMenuKey = m.HandleKeyForTest(Keys.Apps)
            Check("input:help-menu:shift-f10-and-menu-key-open-it", byShiftF10 AndAlso byMenuKey AndAlso opened = 2, opened.ToString() & " openings")
            ' The same keys on the filter box open nothing of the Help button's.
            m.FilterBoxForTest.Select()
            m.HandleKeyForTest(Keys.Shift Or Keys.F10)
            Check("input:help-menu:key-is-the-buttons-only-with-the-button", opened = 2, opened.ToString())

            ' The visible entry: the More menu (a toolbar button a Tab walk reaches) carries a Help entry whose
            ' drop-down is the Help button's own menu, entry for entry.
            Dim helpItems = m.MenuItemsForTest("help").Select(Function(i) i.Text).ToList()
            Dim more = m.MenuItemsForTest("more")
            Dim entry = more.OfType(Of ToolStripMenuItem)().FirstOrDefault(Function(i) i.Text = words("vd_mgr_name_help") AndAlso i.HasDropDownItems)
            Check("input:help-menu:more-has-a-help-entry", entry IsNot Nothing AndAlso entry.Image IsNot Nothing, String.Join(" | ", more.Select(Function(i) i.Text).ToArray()))
            If entry IsNot Nothing Then
                Dim sub_ = entry.DropDownItems.Cast(Of ToolStripItem)().Select(Function(i) i.Text).ToList()
                Check("input:help-menu:entry-is-the-same-menu", sub_.SequenceEqual(helpItems) AndAlso sub_.Contains(words("vd_help_menu_help")) AndAlso sub_.Contains(words("vd_help_menu_first")),
                      String.Join(" | ", sub_.ToArray()))
                ' The sub-menu is dressed like the menu it opens from (a light drop-down on the dark surface is
                ' the failure): the shell's renderer, the palette's surface and ink, the 16 px picture size.
                Dim drop = entry.DropDown
                Check("input:help-menu:entry-is-themed", TypeOf drop.Renderer Is ShellMenuRenderer AndAlso drop.BackColor.ToArgb() = Theme.Current.Surface.ToArgb() AndAlso
                                                         drop.ForeColor.ToArgb() = Theme.Current.Text.ToArgb() AndAlso drop.ImageScalingSize.Width = Ui.Px(m, 16),
                      drop.BackColor.ToString() & " on " & Theme.Current.Surface.ToString())
            End If

            ' The welcome tour draws app.tour, not the .fdd container's picture: the entry's image is the tour glyph.
            Dim tourItem = m.MenuItemsForTest("help").OfType(Of ToolStripMenuItem)().FirstOrDefault(Function(i) i.Text = words("vd_help_menu_first"))
            Check("input:help-menu:tour-meaning", DiskGlyphs.Tour.Id = "app.tour" AndAlso DiskGlyphs.Tour IsNot DiskGlyphs.DiskContainer, DiskGlyphs.Tour.ToString())
            If tourItem IsNot Nothing AndAlso tourItem.Image IsNot Nothing Then
                ' GlyphBitmaps keeps what it renders (a menu's pictures are shared), so neither bitmap is disposed here.
                Dim size = tourItem.Image.Width
                Dim wanted = GlyphBitmaps.Render(DiskGlyphs.Tour, size, Theme.Current.Text)
                Dim container = GlyphBitmaps.Render(DiskGlyphs.DiskContainer, size, Theme.Current.Text)
                Dim sameAsTour = SamePixels(DirectCast(tourItem.Image, Bitmap), wanted)
                Dim sameAsContainer = SamePixels(DirectCast(tourItem.Image, Bitmap), container)
                Check("input:help-menu:tour-entry-draws-app-tour", sameAsTour AndAlso Not sameAsContainer, "tour " & sameAsTour.ToString() & ", container " & sameAsContainer.ToString())
            Else
                Check("input:help-menu:tour-entry-draws-app-tour", False, "no welcome tour entry with a picture")
            End If
        Finally
            DiskManagerForm.SuppressReads = wasReads
            DiskManagerForm.SuppressWelcome = wasWelcome
            If m IsNot Nothing Then m.Dispose()
        End Try
    End Sub

    Private Function SamePixels(a As Bitmap, b As Bitmap) As Boolean
        If a Is Nothing OrElse b Is Nothing OrElse a.Size <> b.Size Then Return False
        For y = 0 To a.Height - 1
            For x = 0 To a.Width - 1
                If a.GetPixel(x, y) <> b.GetPixel(x, y) Then Return False
            Next
        Next
        Return True
    End Function

End Module
