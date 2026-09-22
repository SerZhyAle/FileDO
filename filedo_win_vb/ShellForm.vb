' The shell's main window (SP-0006).
' Hosts the rail navigation, the 3-card job view, history & reports view, and expert command view.
Public Class ShellForm
    Inherits Form

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly entries As New List(Of RailEntry)

    ' The rail's groups, and the rows each one owns. Eighteen rows and eight headers do not fit on
    ' a laptop screen at once, so a group folds: the header stays, its rows go, and the state is
    ' remembered per user. groupOf answers the other direction - which header to open when a job is
    ' selected from somewhere other than a click on its own row.
    Private ReadOnly groupHeaders As New List(Of RailEntry)
    Private ReadOnly groupMembers As New Dictionary(Of String, List(Of RailEntry))(StringComparer.Ordinal)
    Private ReadOnly groupOf As New Dictionary(Of String, String)(StringComparer.Ordinal)
    Private currentGroupKey As String = Nothing

    Private rail As FlowLayoutPanel
    Private railHost As Panel
    Private pageHost As Panel
    Private header As Panel
    Private titleLabel As Label
    Private subtitleLabel As Label
    Private emptyTitle As Label
    Private emptyHint As Label
    Private emptyCentre As TableLayoutPanel
    Private root As TableLayoutPanel
    Private rightSide As TableLayoutPanel

    Private jobView As JobView
    Private historyView As HistoryView
    Private commandView As CommandView
    Private aboutView As AboutView
    Private settingsView As SettingsView
    Private ReadOnly tips As New ToolTip()

    ' WM_SETTINGCHANGE - Windows broadcasts it when the user switches light and dark, among many other things.
    Private Const WM_SETTINGCHANGE As Integer = &H1A

    Public Sub New()
        Me.New(Nothing)
    End Sub

    ' openTarget is a file the shell was started on - a double-click on a
    ' .fd-sec container (SP-0005 9.4), or a path passed on the command line.
    ' The window then opens on the page that file is for, with step 2 already
    ' answered, instead of on the builder the user did not ask for.
    Public Sub New(openTarget As String)
        dict = Localization.GetDict(ShellSettings.Language())
        Theme.Refresh()
        BuildLayout()
        ApplyTheme()
        RestorePlacement()

        If Not OpenOnTarget(openTarget) Then
            ' D5: Default to Command page after first-run tour / on startup
            SelectJobByKey("rail_job_command")
        End If
    End Sub

    ' A container opens its reveal page; any other file opens the page that
    ' makes one. Nothing else is guessed: a path that is not a file at all
    ' leaves the window where it would have opened anyway.
    Private Function OpenOnTarget(path As String) As Boolean
        If String.IsNullOrWhiteSpace(path) Then Return False
        Try
            If Not IO.File.Exists(path) Then Return False
        Catch
            Return False
        End Try

        Dim key = If(path.EndsWith(".fd-sec", StringComparison.OrdinalIgnoreCase),
                     "rail_job_reveal", "rail_job_secure")
        SelectJobByKey(key)
        jobView.SetTarget(path)
        Return True
    End Function

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    ' ---- layout ----------------------------------------------------------

    Private Sub BuildLayout()
        SuspendLayout()

        Text = L("shell_title")
        AutoScaleMode = AutoScaleMode.Font
        ' The minimum is a design size, so it has to be told what a design pixel is worth on this
        ' monitor: 900 device pixels at 200% scaling is half a window, and every string inside it
        ' is clipped. OnDpiChanged re-states it when the window moves to another display.
        MinimumSize = Ui.PxSize(Me, 940, 640)
        Size = OpeningSize()
        StartPosition = FormStartPosition.CenterScreen
        DoubleBuffered = True
        KeyPreview = True

        root = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 2,
            .RowCount = 1,
            .Margin = New Padding(0),
            .Padding = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, CSng(Ui.Px(Me, 268))))
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        root.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))

        railHost = New Panel With {.Dock = DockStyle.Fill, .Padding = Ui.PxPad(Me, 0, 6, 0, 6)}
        rail = New FlowLayoutPanel With {
            .Dock = DockStyle.Fill,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .AutoScroll = True,
            .Margin = New Padding(0),
            .Padding = New Padding(0)
        }
        railHost.Controls.Add(rail)
        BuildRail()
        RestoreCollapsedGroups()
        ' A rail row is as wide as the rail it is in, whatever the window is doing: a fixed width
        ' is a clipped label in the first locale whose word is longer than English's.
        AddHandler rail.SizeChanged, Sub() LayoutRail()

        rightSide = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 2,
            .Margin = New Padding(0),
            .Padding = New Padding(0)
        }
        rightSide.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        rightSide.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        rightSide.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))

        BuildHeader()
        BuildPageHost()

        rightSide.Controls.Add(header, 0, 0)
        rightSide.Controls.Add(pageHost, 0, 1)

        root.Controls.Add(railHost, 0, 0)
        root.Controls.Add(rightSide, 1, 0)
        Controls.Add(root)

        ResumeLayout(True)
    End Sub

    ' The window a first run opens at. A fixed 1180x780 is a small window on the display most of
    ' this program's users actually have, and the shell is a two-column page that gets the cards
    ' out of each other's way as soon as it is given the room - so the opening size is taken from
    ' the desktop rather than written down: most of it, never more than it, and never below the
    ' minimum that keeps the rail and the cards readable. A saved placement overrides this.
    Private Function OpeningSize() As Size
        Dim work = Screen.PrimaryScreen.WorkingArea
        Dim least = Ui.PxSize(Me, 940, 640)
        Dim most = Ui.PxSize(Me, 1760, 1120)

        Dim w = Math.Min(Math.Max(CInt(work.Width * 0.8), least.Width), most.Width)
        Dim h = Math.Min(Math.Max(CInt(work.Height * 0.85), least.Height), most.Height)

        Return New Size(Math.Min(w, work.Width), Math.Min(h, work.Height))
    End Function

    Private Sub BuildHeader()
        header = New Panel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Padding = New Padding(24, 18, 24, 14)
        }

        Dim stack As New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 2,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Margin = New Padding(0)
        }
        stack.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        stack.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        titleLabel = New Label With {
            .Text = L("shell_title"),
            .AutoSize = True,
            .Margin = New Padding(0)
        }
        subtitleLabel = New Label With {
            .Text = L("shell_subtitle"),
            .AutoSize = True,
            .Margin = New Padding(0, 2, 0, 0)
        }

        stack.Controls.Add(titleLabel, 0, 0)
        stack.Controls.Add(subtitleLabel, 0, 1)
        header.Controls.Add(stack)
    End Sub

    Private Sub BuildPageHost()
        pageHost = New Panel With {.Dock = DockStyle.Fill, .Padding = New Padding(24, 0, 24, 24)}

        jobView = New JobView() With {.Visible = False}
        AddHandler jobView.OpenInCommandRequested,
            Sub(cmd)
                SelectJobByKey("rail_job_command")
                commandView.SetCommand(cmd)
            End Sub

        historyView = New HistoryView() With {.Visible = False}
        commandView = New CommandView() With {.Visible = False}
        aboutView = New AboutView() With {.Visible = False}
        settingsView = New SettingsView() With {.Visible = False}
        AddHandler settingsView.ThemeChanged, Sub() ApplyTheme()

        emptyCentre = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 4,
            .Margin = New Padding(0)
        }
        emptyCentre.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        emptyCentre.RowStyles.Add(New RowStyle(SizeType.Percent, 50.0F))
        emptyCentre.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        emptyCentre.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        emptyCentre.RowStyles.Add(New RowStyle(SizeType.Percent, 50.0F))

        emptyTitle = New Label With {
            .Text = L("shell_empty_title"),
            .AutoSize = True,
            .Anchor = AnchorStyles.None,
            .Margin = New Padding(0)
        }
        emptyHint = New Label With {
            .Text = L("shell_empty_hint"),
            .AutoSize = True,
            .Anchor = AnchorStyles.None,
            .Margin = New Padding(0, 8, 0, 0),
            .MaximumSize = New Size(520, 0),
            .TextAlign = ContentAlignment.MiddleCenter
        }

        emptyCentre.Controls.Add(emptyTitle, 0, 1)
        emptyCentre.Controls.Add(emptyHint, 0, 2)

        pageHost.Controls.Add(jobView)
        pageHost.Controls.Add(historyView)
        pageHost.Controls.Add(commandView)
        pageHost.Controls.Add(aboutView)
        pageHost.Controls.Add(settingsView)
        pageHost.Controls.Add(emptyCentre)
    End Sub

    ' The rail rows are SP-0006 section 5.2.
    '
    ' Every row here leads somewhere today. A row for a job that is still being written is not
    ' listed as "coming later": a rail is navigation, not a roadmap, and a row that answers nothing
    ' when it is clicked costs the reader more than the announcement is worth. The job appears in
    ' the rail on the day its page does.
    Private Sub BuildRail()
        AddGroup("rail_group_check")
        AddEntry("rail_job_capacity", ChrW(&HE7BA))
        AddEntry("rail_job_speed", ChrW(&HE72C))
        AddEntry("rail_job_info", ChrW(&HE946))
        AddEntry("rail_job_damaged", ChrW(&HE721))
        AddEntry("rail_job_probe", ChrW(&HE9D9))
        AddEntry("rail_job_recover", ChrW(&HE777))

        AddGroup("rail_group_tidy")
        AddEntry("rail_job_duplicates", ChrW(&HE8C8))
        AddEntry("rail_job_compare", ChrW(&HE8B7))
        AddEntry("rail_job_clean", ChrW(&HE74D))

        AddGroup("rail_group_move")
        AddEntry("rail_job_copy", ChrW(&HE896))

        AddGroup("rail_group_erase")
        AddEntry("rail_job_fill", ChrW(&HE74E))
        AddEntry("rail_job_wipe", ChrW(&HE74D))

        AddGroup("rail_group_protect")
        AddEntry("rail_job_secure", ChrW(&HE72E))
        AddEntry("rail_job_unsecure", ChrW(&HE785))
        AddEntry("rail_job_reveal", ChrW(&HE8A7))

        AddGroup("rail_group_records")
        AddEntry("rail_job_history", ChrW(&HE81C))

        AddGroup("rail_group_expert")
        AddEntry("rail_job_command", ChrW(&HE756))

        ' The program itself: where the settings live, and where the links to the site, the source,
        ' the issue tracker and the author are.
        AddGroup("rail_group_program")
        AddEntry("rail_job_settings", ChrW(&HE713))
        AddEntry("rail_job_about", ChrW(&HE946))
    End Sub

    ' A group header. It is a row the user can operate - click, Space or Enter folds the group -
    ' so it is in the tab order and carries its own accessible name and state.
    Private Sub AddGroup(key As String)
        Dim e As New RailEntry With {
            .Key = key,
            .Text = L(key).ToUpperInvariant(),
            .IsGroupHeader = True,
            .TabStop = True,
            .Width = Ui.Px(Me, 236),
            .Height = Ui.Px(Me, 26),
            .Margin = Ui.PxPad(Me, 10, 8, 10, 1)
        }
        e.AccessibleRole = AccessibleRole.ButtonDropDown
        AddHandler e.Click, AddressOf GroupHeader_Click
        currentGroupKey = key
        groupHeaders.Add(e)
        groupMembers(key) = New List(Of RailEntry)
        rail.Controls.Add(e)
        UpdateGroupAccessibility(e)
    End Sub

    Private Sub AddEntry(key As String, glyph As String)
        Dim e As New RailEntry With {
            .Key = key,
            .Text = L(key),
            .Glyph = glyph,
            .TabStop = True,
            .Width = Ui.Px(Me, 236),
            .Height = Ui.Px(Me, 34),
            .Margin = Ui.PxPad(Me, 10, 0, 10, 0)
        }
        e.AccessibleName = L(key)
        e.AccessibleRole = AccessibleRole.ListItem
        If currentGroupKey IsNot Nothing Then
            groupMembers(currentGroupKey).Add(e)
            groupOf(key) = currentGroupKey
        End If
        ' The row is one line, so a label that does not fit is shortened on screen - the tooltip is
        ' where the whole label and the job's purpose stay readable, in every locale.
        tips.SetToolTip(e, L(key) & Environment.NewLine & L(PurposeKeyFor(key)))
        AddHandler e.Click, AddressOf RailEntry_Click
        entries.Add(e)
        rail.Controls.Add(e)
    End Sub

    Private Shared Function PurposeKeyFor(railKey As String) As String
        Return "purpose_" & railKey.Replace("rail_job_", "job_")
    End Function

    ' ---- groups ----------------------------------------------------------

    Private Sub GroupHeader_Click(sender As Object, e As EventArgs)
        Dim head = TryCast(sender, RailEntry)
        If head Is Nothing Then Return
        SetGroupCollapsed(head, Not head.Collapsed)
        SaveCollapsedGroups()
    End Sub

    Private Sub SetGroupCollapsed(head As RailEntry, collapsed As Boolean)
        If head Is Nothing Then Return
        head.Collapsed = collapsed

        Dim members As List(Of RailEntry) = Nothing
        If groupMembers.TryGetValue(head.Key, members) Then
            rail.SuspendLayout()
            For Each m In members
                m.Visible = Not collapsed
            Next
            rail.ResumeLayout(True)
        End If

        UpdateGroupAccessibility(head)
        RefreshGroupSelectionMarks()
    End Sub

    ' The header's spoken name says the group and its state, because the chevron alone is a glyph a
    ' screen reader has no word for.
    Private Sub UpdateGroupAccessibility(head As RailEntry)
        Dim state = If(head.Collapsed, L("rail_group_collapsed"), L("rail_group_expanded"))
        head.AccessibleName = L(head.Key) & " - " & state
        tips.SetToolTip(head, L(head.Key) & Environment.NewLine &
                              If(head.Collapsed, L("rail_group_expand"), L("rail_group_collapse")))
    End Sub

    ' A shut group shows the accent bar when the current job is one of its hidden rows.
    Private Sub RefreshGroupSelectionMarks()
        For Each head In groupHeaders
            Dim members As List(Of RailEntry) = Nothing
            Dim holds = False
            If groupMembers.TryGetValue(head.Key, members) Then
                For Each m In members
                    If m.Selected Then holds = True : Exit For
                Next
            End If
            head.HasSelectedChild = holds
        Next
    End Sub

    Private Function HeaderFor(groupKey As String) As RailEntry
        For Each head In groupHeaders
            If head.Key = groupKey Then Return head
        Next
        Return Nothing
    End Function

    ' The group of the chosen job is opened whatever the user left folded: a page must never be on
    ' screen with no row in the rail pointing at it.
    Private Sub EnsureGroupOpenFor(jobKey As String)
        Dim groupKey As String = Nothing
        If Not groupOf.TryGetValue(jobKey, groupKey) Then Return
        Dim head = HeaderFor(groupKey)
        If head Is Nothing OrElse Not head.Collapsed Then Return
        SetGroupCollapsed(head, False)
        SaveCollapsedGroups()
    End Sub

    Private Sub RestoreCollapsedGroups()
        Dim saved = ShellSettings.CollapsedGroups()
        If saved.Count = 0 Then Return
        For Each head In groupHeaders
            If saved.Contains(head.Key) Then SetGroupCollapsed(head, True)
        Next
    End Sub

    Private Sub SaveCollapsedGroups()
        Dim shut As New List(Of String)
        For Each head In groupHeaders
            If head.Collapsed Then shut.Add(head.Key)
        Next
        ShellSettings.SetCollapsedGroups(shut)
    End Sub

    Private Sub LayoutRail()
        Dim w = rail.ClientSize.Width - Ui.Px(Me, 26)
        If w < Ui.Px(Me, 120) Then Return
        For Each c As Control In rail.Controls
            c.Width = w
        Next
    End Sub

    Private Sub SelectJobByKey(key As String)
        For Each it In entries
            If it.Key = key Then
                RailEntry_Click(it, EventArgs.Empty)
                Exit For
            End If
        Next
    End Sub

    Private Sub RailEntry_Click(sender As Object, e As EventArgs)
        Dim chosen = TryCast(sender, RailEntry)
        If chosen Is Nothing Then Return

        For Each it In entries
            it.Selected = (it Is chosen)
        Next
        EnsureGroupOpenFor(chosen.Key)
        RefreshGroupSelectionMarks()

        ' Step 1 of the owner's flow is this rail, so the header says so and names the answer: the
        ' page below it then opens at step 2 and the numbering runs unbroken down one screen. Only
        ' a row that actually leads to a run is numbered - History, Settings and About are places,
        ' not steps, and numbering them would make the count mean nothing. Command is a place too:
        ' the rail only chose the builder, and the operation - the real step 1 - is asked on the
        ' page itself, so the header must not answer step 1 before the page does.
        Dim numbered = (JobCatalogue.GetJob(chosen.Key) IsNot Nothing)
        titleLabel.Text = If(numbered, String.Format(L("shell_step1_fmt"), chosen.Text), chosen.Text)
        subtitleLabel.Text = L(PurposeKeyFor(chosen.Key))

        jobView.Visible = False
        historyView.Visible = False
        commandView.Visible = False
        aboutView.Visible = False
        settingsView.Visible = False
        emptyCentre.Visible = False

        If chosen.Key = "rail_job_history" Then
            historyView.RefreshReports()
            historyView.Visible = True
        ElseIf chosen.Key = "rail_job_command" Then
            commandView.Visible = True
        ElseIf chosen.Key = "rail_job_about" Then
            aboutView.Visible = True
        ElseIf chosen.Key = "rail_job_settings" Then
            settingsView.Visible = True
        Else
            Dim jobDef = JobCatalogue.GetJob(chosen.Key)
            If jobDef IsNot Nothing Then
                jobView.SetJob(jobDef)
                jobView.Visible = True
            Else
                emptyTitle.Text = chosen.Text
                emptyHint.Text = L("shell_empty_hint")
                emptyCentre.Visible = True
            End If
        End If
    End Sub

    ' ---- theme -----------------------------------------------------------

    Private Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        railHost.BackColor = p.SurfaceAlt
        rail.BackColor = p.SurfaceAlt

        rightSide.BackColor = p.Background
        header.BackColor = p.Background
        pageHost.BackColor = p.Surface

        titleLabel.Font = Theme.FontTitle()
        titleLabel.ForeColor = p.Text
        subtitleLabel.Font = Theme.FontBody()
        subtitleLabel.ForeColor = p.MutedText

        emptyTitle.Font = Theme.FontSubtitle()
        emptyTitle.ForeColor = p.Text
        emptyHint.Font = Theme.FontBody()
        emptyHint.ForeColor = p.MutedText

        For Each c As Control In rail.Controls
            c.Invalidate()
        Next

        jobView.ApplyTheme()
        historyView.ApplyTheme()
        commandView.ApplyTheme()
        aboutView.ApplyTheme()
        settingsView.ApplyTheme()

        Chrome.Apply(Me)
        Invalidate(True)
    End Sub

    ' Moving the window to a display with another scaling changes what a design pixel is worth, so
    ' the minimum size is re-stated rather than left at the old monitor's value.
    Protected Overrides Sub OnDpiChanged(e As DpiChangedEventArgs)
        MyBase.OnDpiChanged(e)
        MinimumSize = Ui.PxSize(Me, 940, 640)
        LayoutRail()
    End Sub

    Protected Overrides Sub WndProc(ByRef m As Message)
        MyBase.WndProc(m)
        If m.Msg = WM_SETTINGCHANGE Then
            If ShellSettings.ThemeChoice() = "auto" Then
                Dim wasDark = Theme.Current.IsDark
                Theme.Refresh()
                If Theme.Current.IsDark <> wasDark Then ApplyTheme()
            End If
        End If
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
        AppIcon.Apply(Me)
        WriteDiagnosticsIfAsked()
    End Sub

    ' ---- placement -------------------------------------------------------

    Private Sub RestorePlacement()
        Dim p = ShellSettings.LoadPlacement()
        If Not p.HasValue Then Return
        Dim r As New Rectangle(p.X, p.Y, p.Width, p.Height)
        Dim visible = False
        For Each s As Screen In Screen.AllScreens
            If s.WorkingArea.IntersectsWith(r) Then visible = True
        Next
        If visible Then
            StartPosition = FormStartPosition.Manual
            Bounds = r
        End If
        If p.Maximized Then WindowState = FormWindowState.Maximized
    End Sub

    Protected Overrides Sub OnFormClosing(e As FormClosingEventArgs)
        Dim b = If(WindowState = FormWindowState.Normal, Bounds, RestoreBounds)
        ShellSettings.SavePlacement(b.X, b.Y, b.Width, b.Height, WindowState = FormWindowState.Maximized)
        MyBase.OnFormClosing(e)
    End Sub

    Private Sub WriteDiagnosticsIfAsked()
        Try
            If Not Environment.GetCommandLineArgs().Contains("-debug") Then Return
            Dim line = DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss") & " shell frame: " & Chrome.Report() &
                       " | dpi=" & DeviceDpi.ToString() & " | size=" & Width.ToString() & "x" & Height.ToString()
            IO.File.AppendAllText("filedo_win_shell.log", line & Environment.NewLine)
        Catch
        End Try
    End Sub

End Class
