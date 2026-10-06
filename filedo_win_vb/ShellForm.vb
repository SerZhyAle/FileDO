' The shell's main window (SP-0006).
' Hosts the rail navigation, the 3-card job view, history & reports view, and expert command view.
'
' Two shared desktop contracts bind this window, FileDO a consumer of both: APP-BEHAVIOUR (the
' twelve moments - here rule 3, a run that navigation and closing cannot lose; rule 10, placement)
' and APP-STYLE (the theme, applied live below). docs/contracts/ carries what FileDO owes them.
Public Class ShellForm
    Inherits Form

    ' ICON-RENDER's Windows target size. The value is in design pixels and is passed through
    ' Ui.Px at every layout site, so it scales with the rest of the window at higher DPI.
    Friend Const RailTargetHeight As Integer = 44

    ' The rail's column, in design pixels (it was 268). The labels wrap inside it and a row grows to
    ' hold them (APP-BEHAVIOUR rule 2), so a narrower rail costs height, not text - but a single word
    ' cannot wrap and may not be cut, so the floor is the longest word in any language
    ' ("Datentraegerverwaltung", 159 design pixels, plus the glyph column and the scroll bar).
    ' CheckRailLabels measures every label of every language at exactly this width.
    Friend Const RailWidth As Integer = 240

    ' The smallest the window may be made, in design pixels. Below this the cards stop being
    ' readable; but a screen smaller than this at its scaling (1366x768 at 150 %) is the stronger
    ' fact, so the real minimum is WindowPlacement.MinimumFor, never more than the working area.
    Friend Const MinDesignWidth As Integer = 760
    Friend Const MinDesignHeight As Integer = 520

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly entries As New List(Of RailEntry)

    ' The rail's groups, and the rows each one owns. Some thirty rows and six headers do not fit on
    ' a laptop screen at once, so a group folds: the header stays, its rows go, and the state is
    ' remembered per user. Only one group is open at a time (GroupHeader_Click). groupOf answers the
    ' other direction - which header to open when a job is selected from somewhere other than a
    ' click on its own row.
    Private ReadOnly groupHeaders As New List(Of RailEntry)
    Private ReadOnly groupMembers As New Dictionary(Of String, List(Of RailEntry))(StringComparer.Ordinal)
    Private ReadOnly groupOf As New Dictionary(Of String, String)(StringComparer.Ordinal)
    Private currentGroupKey As String = Nothing

    Private rail As FlowLayoutPanel
    Private railHost As Panel
    Private pageHost As Panel
    Private header As Panel
    Private titleLabel As Label
    Private headerRow As TableLayoutPanel
    Private subtitleLabel As Label
    Private statusLabel As Label
    Private emptyTitle As Label
    Private emptyHint As Label
    Private emptyCentre As TableLayoutPanel
    Private root As TableLayoutPanel
    Private rightSide As TableLayoutPanel

    Private jobViewValue As JobView
    Private historyView As HistoryView
    Private commandView As CommandView
    Private aboutView As AboutView
    Private settingsView As SettingsView
    Private ReadOnly tips As New ToolTip()

    ' WM_SETTINGCHANGE - Windows broadcasts it when the user switches light and dark, among many other things.
    Private Const WM_SETTINGCHANGE As Integer = &H1A

    ' Closing while a run is active (APP-BEHAVIOUR rule 3, SP-0006 section 14): the user chose Stop
    ' and close, the stop file is written, and the window closes itself when the run has ended.
    Private closeWhenIdle As Boolean = False
    Private stopAskedAt As DateTime = DateTime.MinValue

    Public Sub New()
        Me.New(Nothing)
    End Sub

    ' openTarget is a file the shell was started on - a double-click on a
    ' .fd-sec container (SP-0005 9.4), or a path passed on the command line.
    ' The window then opens on the page that file is for, with step 2 already
    ' answered, instead of on the builder the user did not ask for.
    Public Sub New(openTarget As String)
        Me.New(openTarget, Nothing)
    End Sub

    ' diskStart is how a start on a `.fdd` was answered before the window existed (DiskRoute,
    ' SP-0004 6.4): which Disks page, read-only or not, the facts `info` gave, and whether the run
    ' has nothing left to ask and goes at once when the window is shown.
    Private diskStart As DiskRoute.Decision = Nothing

    Public Sub New(openTarget As String, diskDecision As DiskRoute.Decision)
        dict = Localization.GetDict(ShellSettings.Language())
        Theme.Refresh()
        BuildLayout()
        ApplyTheme()
        RestorePlacement()

        diskStart = diskDecision
        If Not OpenOnTarget(openTarget) Then
            ' D5: Default to Command page after first-run tour / on startup
            SelectJobByKey(ShellSettings.LastPage())
        End If
        Theme.Watch(Me, AddressOf ApplyTheme)
    End Sub

    ' A container opens its reveal page; any other file opens the page that
    ' makes one. Nothing else is guessed: a path that is not a file at all
    ' leaves the window where it would have opened anyway.
    '
    ' A `.fdd` is a disk container (SP-0004 6.4) and never opens the secure page - packing a disk
    ' image into a .fd-sec is not what dropping or double-clicking one means. It opens the Disks
    ' page its start asked for: Mount, or Unmount for --unmount.
    Private Function OpenOnTarget(path As String) As Boolean
        If String.IsNullOrWhiteSpace(path) Then Return False
        Try
            If Not IO.File.Exists(path) Then Return False
        Catch
            Return False
        End Try

        If DiskRoute.IsContainer(path) Then
            If diskStart Is Nothing Then
                diskStart = DiskRoute.Decide(path, DiskRoute.StartMode.Mount, ContainerFacts.Unknown(path), Packaging.IsPackaged())
            End If
            SelectJobByKey(diskStart.JobKey)
            jobView.SetTarget(path)
            jobView.ApplyDiskStart(diskStart)
            Return True
        End If

        Dim key = If(path.EndsWith(".fd-sec", StringComparison.OrdinalIgnoreCase),
                     "rail_job_reveal", "rail_job_secure")
        SelectJobByKey(key)
        jobView.SetTarget(path)
        Return True
    End Function

    ' A double-click with nothing left to ask runs when the window is on screen (spec 6.4: the
    ' open path goes straight to the mount and runs it). The page runs it only if it offers Run.
    Protected Overrides Sub OnShown(e As EventArgs)
        MyBase.OnShown(e)
        If diskStart IsNot Nothing AndAlso diskStart.AutoRun AndAlso jobViewValue IsNot Nothing AndAlso jobViewValue.CurrentJobId = diskStart.JobKey Then
            diskStart.AutoRun = False
            jobView.RunFromShell()
        End If
    End Sub

    ' For SelfTest.vb: the page a start opened.
    Friend ReadOnly Property CurrentJobIdForTest As String
        Get
            Return jobView.CurrentJobId
        End Get
    End Property

    Friend ReadOnly Property JobViewForTest As JobView
        Get
            Return jobView
        End Get
    End Property

    ' The job page is built the first time a job is chosen, not with the window: the window opens on
    ' the Command page, and building the job page (a hundred controls, the disk options among them)
    ' was about half a second of the wait for nothing the user had asked for. Anything that only
    ' wants to know about a run in progress asks jobViewValue and leaves it unbuilt.
    Private ReadOnly Property jobView As JobView
        Get
            If jobViewValue Is Nothing Then CreateJobView()
            Return jobViewValue
        End Get
    End Property

    Private Function JobViewRunning() As Boolean
        Return jobViewValue IsNot Nothing AndAlso jobViewValue.IsRunning
    End Function

    Private Sub CreateJobView()
        Dim view As New JobView() With {.Visible = False}
        jobViewValue = view
        AddHandler view.OpenInCommandRequested,
            Sub(cmd, credential)
                SelectJobByKey("rail_job_command")
                commandView.SetCommand(cmd, credential)
            End Sub
        AddHandler view.CleanRequested, AddressOf OpenCleanOn
        AddHandler view.DiskJobRequested, AddressOf OpenDiskJobOn
        AddHandler view.DiskManagerRequested, Sub() AppHost.OpenDiskManager()
        AddHandler view.RunFinished, AddressOf AnyRunFinished
        pageHost.Controls.Add(view)
        view.ApplyTheme()
        Ui.KeepCaptionsReadable(view)
        Ui.HitTargetFloor(view)
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function LText(key As String) As String
        Return Localization.Multiline(L(key))
    End Function

    ' ---- layout ----------------------------------------------------------

    Private Sub BuildLayout()
        SuspendLayout()

        Text = L("shell_title")
        AutoScaleMode = AutoScaleMode.Font
        ' The minimum is a design size, so it has to be told what a design pixel is worth on this
        ' monitor: 900 device pixels at 200% scaling is half a window, and every string inside it
        ' is clipped. OnDpiChanged re-states it when the window moves to another display, and OnLoad
        ' once the screen it opens on is known; it is never more than that screen's working area.
        MinimumSize = WindowPlacement.MinimumFor(Me, MinDesignWidth, MinDesignHeight)
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
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, CSng(Ui.Px(Me, RailWidth))))
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
        AddHandler rail.HandleCreated, Sub()
                                           ThemeRailScrollBar()
                                           SyncRailScroll()
                                       End Sub

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
        LayoutRail()
    End Sub

    ' The window a first run opens at. A fixed 1180x780 is a small window on the display most of
    ' this program's users actually have, and the shell is a two-column page that gets the cards
    ' out of each other's way as soon as it is given the room - so the opening size is taken from
    ' the desktop rather than written down: most of it, never more than it, and never below the
    ' minimum that keeps the rail and the cards readable. A saved placement overrides this.
    Private Function OpeningSize() As Size
        Dim work = Screen.PrimaryScreen.WorkingArea
        Dim least = WindowPlacement.MinimumFor(Me, MinDesignWidth, MinDesignHeight)
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
            .RowCount = 3,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Margin = New Padding(0)
        }
        stack.RowStyles.Add(New RowStyle(SizeType.AutoSize))
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

        ' The line that says why the rail did not do what was clicked: a job is running, and the
        ' page it runs on stays in front until it has ended (T5). Empty the rest of the time.
        statusLabel = New Label With {
            .Text = "",
            .AutoSize = True,
            .Visible = False,
            .Margin = New Padding(0, 6, 0, 0)
        }

        stack.Controls.Add(titleLabel, 0, 0)
        stack.Controls.Add(subtitleLabel, 0, 1)
        stack.Controls.Add(statusLabel, 0, 2)

        headerRow = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 1,
            .Margin = New Padding(0)
        }
        headerRow.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        headerRow.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        headerRow.Controls.Add(stack, 0, 0)
        header.Controls.Add(headerRow)
        ' The title and subtitle wrap within the available header width.
        Ui.Wrap(titleLabel, stack, 4)
        Ui.Wrap(subtitleLabel, stack, 4)
        Ui.Wrap(statusLabel, stack, 4)
    End Sub

    ' Ctrl+Shift+D opens the Disk manager from anywhere in the window.
    Protected Overrides Function ProcessCmdKey(ByRef msg As Message, keyData As Keys) As Boolean
        If keyData = Keys.Escape AndAlso settingsView IsNot Nothing AndAlso settingsView.Visible Then
            SelectJobByKey("rail_job_command")
            Return True
        End If
        If keyData = (Keys.Control Or Keys.Shift Or Keys.D) Then
            AppHost.OpenDiskManager()
            Return True
        End If
        Return MyBase.ProcessCmdKey(msg, keyData)
    End Function

    Private Sub BuildPageHost()
        pageHost = New Panel With {.Dock = DockStyle.Fill, .Padding = New Padding(24, 0, 24, 24)}

        historyView = New HistoryView() With {.Visible = False}
        commandView = New CommandView() With {.Visible = False}
        AddHandler commandView.RunFinished, AddressOf AnyRunFinished
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

        pageHost.Controls.Add(historyView)
        pageHost.Controls.Add(commandView)
        pageHost.Controls.Add(aboutView)
        pageHost.Controls.Add(settingsView)
        pageHost.Controls.Add(emptyCentre)
    End Sub

    ' The rail rows are SP-0006 section 5.2, listed once in RailRow.All (Rail.vb) so the self-test
    ' measures exactly the rows this builds.
    Private Sub BuildRail()
        ' A block is a header with the rows it holds, or a lone job; neighbouring blocks take
        ' alternate bands (RailEntry.Band), which is what keeps folded headers from running together.
        Dim block = -1
        For Each row In RailRow.All
            If row.IsGroup Then
                block += 1
                AddGroup(row, block Mod 2)
            ElseIf row.IsAlone Then
                block += 1
                currentGroupKey = Nothing
                AddEntry(row.Key, row.Glyph, row.Hue, block Mod 2)
            Else
                AddEntry(row.Key, row.Glyph, "", block Mod 2)
            End If
        Next
    End Sub

    ' A group header. It is a row the user can operate - click, Space or Enter folds the group -
    ' so it is in the tab order and carries its own accessible name and state. The rows of the rail
    ' touch (no margins): the band is the separation, and a gap would show the other band's colour.
    Private Sub AddGroup(row As RailRow, band As Integer)
        Dim key = row.Key
        Dim e As New RailEntry With {
            .Key = key,
            .Name = "rail:" & key,
            .Text = L(key).ToUpperInvariant(),
            .IsGroupHeader = True,
            .Glyph = row.Glyph,
            .Hue = row.Hue,
            .Band = band,
            .TabStop = True,
            .RowUnit = Ui.Px(Me, RailTargetHeight),
            .Width = Ui.Px(Me, RailWidth),
            .Height = Ui.Px(Me, RailTargetHeight),
            .Margin = New Padding(0)
        }
        e.AccessibleRole = AccessibleRole.ButtonDropDown
        AddHandler e.Click, AddressOf GroupHeader_Click
        currentGroupKey = key
        groupHeaders.Add(e)
        groupMembers(key) = New List(Of RailEntry)
        rail.Controls.Add(e)
        UpdateGroupAccessibility(e)
    End Sub

    Private Sub AddEntry(key As String, glyph As GlyphRef, hue As String, band As Integer)
        Dim e As New RailEntry With {
            .Key = key,
            .Name = "rail:" & key,
            .Text = L(key),
            .Glyph = glyph,
            .Hue = hue,
            .Band = band,
            .TabStop = True,
            .RowUnit = Ui.Px(Me, RailTargetHeight),
            .DefaultActionText = L("shell_acc_open"),
            .Width = Ui.Px(Me, RailWidth),
            .Height = Ui.Px(Me, RailTargetHeight),
            .Margin = New Padding(0)
        }
        If key = RailRow.DiskManagerKey Then
            e.ProductIcon = DiskManagerIcon.DiskManagerIcon()
            e.AccessibleDescription = L("purpose_job_vd_manager")
        End If
        e.AccessibleName = L(key)
        e.AccessibleRole = AccessibleRole.PushButton
        If currentGroupKey IsNot Nothing Then
            groupMembers(currentGroupKey).Add(e)
            groupOf(key) = currentGroupKey
        End If
        ' The label wraps and the row grows to hold it (LayoutRail), so the whole label is always on
        ' screen; the tooltip is where the job's purpose is, in every locale.
        Dim shortcut = If(key = RailRow.DiskManagerKey,
                          " (" & DiskShortcuts.KeyText(Keys.Control Or Keys.Shift Or Keys.D) & ")", "")
        tips.SetToolTip(e, L(key) & shortcut & Environment.NewLine & L(PurposeKeyFor(key)))
        AddHandler e.Click, AddressOf RailEntry_Click
        entries.Add(e)
        rail.Controls.Add(e)
    End Sub

    Private Shared Function PurposeKeyFor(railKey As String) As String
        Return "purpose_" & railKey.Replace("rail_job_", "job_")
    End Function

    ' ---- groups ----------------------------------------------------------

    ' The groups are an accordion (owner, 2026-09-30): opening one shuts the rest, so the rail is
    ' the headers and at most one group's rows. Shutting the open one leaves them all shut.
    Private Sub GroupHeader_Click(sender As Object, e As EventArgs)
        Dim head = TryCast(sender, RailEntry)
        If head Is Nothing Then Return
        ToggleGroup(head)
        SaveCollapsedGroups()
    End Sub

    Private Sub ToggleGroup(head As RailEntry)
        SetGroupCollapsed(head, Not head.Collapsed)
        rail.ScrollControlIntoView(head)
    End Sub

    ' The self-test's view of the rail, and a click on a header that does not write the user's
    ' remembered groups to HKCU.
    Friend Function RailEntriesForTest() As List(Of RailEntry)
        Return rail.Controls.OfType(Of RailEntry)().ToList()
    End Function

    Friend Sub ToggleGroupForTest(key As String)
        Dim head = HeaderFor(key)
        If head IsNot Nothing Then ToggleGroup(head)
    End Sub

    Private Sub OpenGroupOnly(head As RailEntry)
        rail.SuspendLayout()
        SetGroupCollapsed(head, False)
        rail.ResumeLayout(True)
    End Sub

    Private Sub SetGroupCollapsed(head As RailEntry, collapsed As Boolean)
        If head Is Nothing Then Return
        If collapsed AndAlso groupMembers.ContainsKey(head.Key) AndAlso groupMembers(head.Key).Any(Function(m) m.ContainsFocus) Then head.Focus()
        head.Collapsed = collapsed

        Dim members As List(Of RailEntry) = Nothing
        If groupMembers.TryGetValue(head.Key, members) Then
            rail.SuspendLayout()
            For Each m In members
                m.Visible = Not collapsed
            Next
            rail.ResumeLayout(True)
            SyncRailScroll()
        End If

        UpdateGroupAccessibility(head)
        RefreshGroupSelectionMarks()
    End Sub

    ' The header's spoken name says the group and its state, because the chevron alone is a glyph a
    ' screen reader has no word for.
    Private Sub UpdateGroupAccessibility(head As RailEntry)
        Dim state = If(head.Collapsed, L("rail_group_collapsed"), L("rail_group_expanded"))
        head.AccessibleName = L(head.Key) & " - " & state
        head.DefaultActionText = If(head.Collapsed, L("rail_group_expand"), L("rail_group_collapse"))
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
        OpenGroupOnly(head)
        SaveCollapsedGroups()
    End Sub

    ' What was open last time, cut down to the accordion: the first group the saved list does not
    ' shut stays open and every other one is shut. Nothing saved (a first run, or a list from before
    ' the accordion that shut nothing) therefore opens the first group.
    Private Sub RestoreCollapsedGroups()
        Dim saved = ShellSettings.CollapsedGroups()
        For Each head In groupHeaders
            SetGroupCollapsed(head, saved.Contains(head.Key))
        Next
    End Sub

    Private Sub SaveCollapsedGroups()
        Dim shut As New List(Of String)
        For Each head In groupHeaders
            If head.Collapsed Then shut.Add(head.Key)
        Next
        ShellSettings.SetCollapsedGroups(shut)
    End Sub

    ' A folded group leaves the rail's scroll range as tall as it was while the group was open, so a
    ' scroll bar stays up beside rows that fit, and the rows run over it: a pale strip below the last
    ' row. Switching AutoScroll off and on makes the panel measure what is in it now, and the rows
    ' are then sized to the width that is left.
    Private Sub SyncRailScroll()
        If rail Is Nothing OrElse Not rail.IsHandleCreated Then Return
        rail.SuspendLayout()
        rail.AutoScroll = False
        rail.AutoScroll = True
        rail.ResumeLayout(True)
        LayoutRail()
    End Sub

    <Runtime.InteropServices.DllImport("uxtheme.dll", CharSet:=Runtime.InteropServices.CharSet.Unicode)>
    Private Shared Function SetWindowTheme(hwnd As IntPtr, appName As String, idList As String) As Integer
    End Function

    ' The rail's own scroll bar in the theme: left to Windows it is a light bar on a dark rail.
    ' A Windows without the theme keeps its own bar, which is no worse than before.
    Private Sub ThemeRailScrollBar()
        If rail Is Nothing OrElse Not rail.IsHandleCreated Then Return
        Try
            SetWindowTheme(rail.Handle, If(Theme.Current.IsDark, "DarkMode_Explorer", "Explorer"), Nothing)
        Catch ex As Exception
            ShellLog.Write("theme the rail's scroll bar", ex)
        End Try
    End Sub

    ' Every row as wide as the rail and as tall as its label needs: one unit, or more where the label
    ' wraps (APP-BEHAVIOUR rule 2).
    Private Sub LayoutRail()
        Dim w = rail.ClientSize.Width
        If w < Ui.Px(Me, 120) Then Return
        rail.SuspendLayout()
        For Each c As Control In rail.Controls
            Dim row = TryCast(c, RailEntry)
            If row Is Nothing Then
                c.Width = w
                Continue For
            End If
            row.RowUnit = Ui.Px(Me, RailTargetHeight)
            row.Size = New Size(w, row.PreferredRowHeight(w))
        Next
        rail.ResumeLayout(True)
    End Sub

    ' Capture.vb's seams: open a page as a click on its row would, optionally on a target, and
    ' draw what the client area shows - the window's own frame is Windows', not the product's.
    ' The self-test's view of the button that opens the Disk manager, and of its tooltip.
    Friend ReadOnly Property DiskManagerButtonForTest As RailEntry
        Get
            Return entries.FirstOrDefault(Function(e) e.Key = RailRow.DiskManagerKey)
        End Get
    End Property

    ' The driven UI acceptance run's view of the settings surface (SP-0150, UiDrive.vb).
    Friend ReadOnly Property SettingsPanelForTest As SettingsPanel
        Get
            Return settingsView.Panel
        End Get
    End Property

    ' The self-test's view of the header at the window's minimum size: whether the title and its
    ' subtitle lie wholly inside the column they are drawn in (rule 2: nothing is cut off).
    Friend ReadOnly Property CommandViewForTest As CommandView
        Get
            Return commandView
        End Get
    End Property

    Friend Function HeaderTextFitsForTest() As Boolean
        For Each part In New Label() {titleLabel, subtitleLabel}
            If part.Parent Is Nothing OrElse part.Right > part.Parent.ClientSize.Width Then Return False
        Next
        Return True
    End Function

    Friend Function TipForTest(c As Control) As String
        Return tips.GetToolTip(c)
    End Function

    Friend Sub ShowPageForCapture(key As String, target As String)
        SelectJobByKey(key)
        If Not String.IsNullOrEmpty(target) AndAlso JobCatalogue.GetJob(key) IsNot Nothing Then jobView.SetTarget(target)
    End Sub

    Friend Sub DrawClientForCapture(bmp As Bitmap)
        root.DrawToBitmap(bmp, New Rectangle(0, 0, bmp.Width, bmp.Height))
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

        ' The Disk Manager is a window of its own (SP-0063 D1): its row opens it, or brings it
        ' forward, and leaves the page on screen here as it was.
        If chosen.Key = RailRow.DiskManagerKey Then
            AppHost.OpenDiskManager()
            Return
        End If

        ' A job is running on the job page. Another job's row would reset that page - hiding its run
        ' strip and its Stop button while the run carries on - so it is refused: the running job's
        ' row is chosen again and the header says why (APP-BEHAVIOUR rule 3). History, Settings,
        ' About and Command are other views and stay reachable; the job page keeps its run while
        ' they are on screen.
        Dim chosenJob = JobCatalogue.GetJob(chosen.Key)
        If chosenJob IsNot Nothing AndAlso JobViewRunning() AndAlso chosen.Key <> jobView.CurrentJobId Then
            Dim runningRow = EntryFor(jobView.CurrentJobId)
            If runningRow IsNot Nothing Then
                RailEntry_Click(runningRow, EventArgs.Empty)
                ShowBusy(runningRow.Text)
                Return
            End If
        End If
        HideBusy()

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
        titleLabel.Text = If(numbered, Localization.Format(L("shell_step1_fmt"), chosen.Text), chosen.Text)
        subtitleLabel.Text = L(PurposeKeyFor(chosen.Key))

        If jobViewValue IsNot Nothing Then jobViewValue.Visible = False
        historyView.Visible = False
        commandView.Visible = False
        aboutView.Visible = False
        settingsView.Visible = False
        emptyCentre.Visible = False

        ' The view that comes to the front is laid out once, when it is all in place. Showing it
        ' with its layout live made every nested AutoSize panel re-measure its labels and check
        ' boxes on each Visible and text change below: Settings took 0.6-3 s and Damaged files
        ' 2 s to appear, and with the tree suspended 0.02 s and 0.4 s (Ui.SuspendTree).
        Dim jobDef = JobCatalogue.GetJob(chosen.Key)
        Dim front As Control = Nothing
        If chosen.Key = "rail_job_history" Then
            front = historyView
        ElseIf chosen.Key = "rail_job_command" Then
            front = commandView
        ElseIf chosen.Key = "rail_job_about" Then
            front = aboutView
        ElseIf chosen.Key = "rail_job_settings" Then
            front = settingsView
        ElseIf jobDef IsNot Nothing Then
            front = jobView
        End If

        Ui.SuspendTree(front)
        Try
            If chosen.Key = "rail_job_history" Then
                historyView.RefreshReports()
                historyView.Visible = True
            ElseIf chosen.Key = "rail_job_command" Then
                commandView.Visible = True
            ElseIf chosen.Key = "rail_job_about" Then
                aboutView.Visible = True
            ElseIf chosen.Key = "rail_job_settings" Then
                settingsView.Visible = True
            ElseIf jobDef IsNot Nothing Then
                ' The page of a job that is running, or whose result arrived while another view was
                ' in front, is shown as it stands - choosing its row again must not throw either
                ' one away.
                If Not (chosen.Key = jobView.CurrentJobId AndAlso jobView.HoldsRunOrUnseenResult) Then
                    jobView.SetJob(jobDef)
                End If
                jobView.Visible = True
                jobView.MarkResultSeen()
            Else
                emptyTitle.Text = chosen.Text
                emptyHint.Text = L("shell_empty_hint")
                emptyCentre.Visible = True
            End If
        Finally
            Ui.ResumeTree(front)
        End Try
    End Sub

    ' GUI-12: "Clean files" after a test opens Clean on the drive that was tested, with Clean's rail
    ' row selected - not on whichever drive happens to be listed first.
    Private Sub OpenCleanOn(target As String)
        SelectJobByKey("rail_job_clean")
        If jobView.CurrentJobId = "rail_job_clean" AndAlso Not String.IsNullOrEmpty(target) Then
            jobView.SetTarget(target)
        End If
    End Sub

    ' A delegated operation of the Disk Manager (SP-0063 7.3): the shell comes forward - shown when
    ' the manager was started alone - on the job page with the container chosen, which keeps its plan
    ' card, its consequences and its typed confirmation.
    Friend Sub OpenDiskJob(key As String, target As String, preset As String)
        BringBack()
        OpenDiskJobOn(key, target, preset)
    End Sub

    ' The window in front of the user: shown if it was hidden, restored if it was minimized. The
    ' Disk manager's "FileDO operations" button and its key come here; a delegated operation does as well.
    Friend Sub BringBack()
        If Not Visible Then Show()
        If WindowState = FormWindowState.Minimized Then WindowState = lastShownState
        Activate()
        If AppHost.Current IsNot Nothing Then AppHost.Current.Restored(Me)
    End Sub

    ' The Disks list's next steps (SP-0004 P6): Unmount or Turn off auto-mount open their own page
    ' on the row's container, with the rail row selected, and wait for Run there.
    Private Sub OpenDiskJobOn(key As String, target As String, preset As String)
        SelectJobByKey(key)
        If jobView.CurrentJobId <> key Then Return
        If Not String.IsNullOrEmpty(target) Then jobView.SetTarget(target)
        jobView.ApplyDiskPreset(preset)
    End Sub

    Private Function EntryFor(key As String) As RailEntry
        For Each it In entries
            If it.Key = key Then Return it
        Next
        Return Nothing
    End Function

    Private Sub ShowBusy(jobName As String)
        statusLabel.Text = Localization.Format(L("shell_busy_fmt"), jobName)
        statusLabel.Visible = True
    End Sub

    Private Sub HideBusy()
        If closeWhenIdle Then Return
        statusLabel.Visible = False
        statusLabel.Text = ""
    End Sub

    ' ---- a run and the window's life -------------------------------------

    Friend Function AnyRunActive() As Boolean
        Return JobViewRunning() OrElse commandView.IsRunning
    End Function

    Friend Sub StopFromTray()
        If jobViewValue IsNot Nothing Then jobViewValue.RequestStopFromShell()
        commandView.RequestStopFromShell()
    End Sub
    Friend ReadOnly Property ClosePending As Boolean
        Get
            Return closeWhenIdle
        End Get
    End Property
    Friend Sub OpenSettings()
        BringBack()
        SelectJobByKey("rail_job_settings")
        settingsView.Panel.Reveal("theme")
    End Sub

    ' The name of what is running, for the close question.
    Private Function RunningName() As String
        If JobViewRunning() Then
            Dim row = EntryFor(jobView.CurrentJobId)
            If row IsNot Nothing Then Return row.Text
        End If
        Return L("rail_job_command")
    End Function

    Private Sub AnyRunFinished()
        ' A run here may have changed a disk: the Disk Manager, when it is open, reads the state.
        AppHost.ShellRunFinished()
        If Not AnyRunActive() Then HideBusy()
        If closeWhenIdle AndAlso Not AnyRunActive() Then
            ' The run the user asked to stop has ended and its report is written; the close they
            ' asked for goes ahead now.
            BeginInvoke(New MethodInvoker(AddressOf Close))
        End If
    End Sub

    ' Closing while a run is active asks one question (APP-BEHAVIOUR rule 1 and rule 3). Keep
    ' running is the answer that changes nothing, and it is what Escape, the close box and Enter
    ' give. Stop and close writes the stop file and closes when the run has ended - its own cleanup
    ' runs and its report is written - so nothing is left running unseen. If the run has not ended
    ' after a stop was asked for a while ago, a second close offers to end the process outright.
    Private Function ConfirmCloseWhileRunning() As Boolean
        If closeWhenIdle Then
            If (DateTime.Now - stopAskedAt).TotalSeconds < 10 Then Return False
            Dim pickEnd = ShellDialog.Ask(Me, DestructiveDialogs.EndRunNow(dict, RunningName()))
            If pickEnd = 0 Then
                ShellLog.Info("close: the user ended a run that had not stopped")
                If jobViewValue IsNot Nothing Then jobViewValue.ForceEnd()
                commandView.ForceEnd()
            End If
            Return False
        End If

        Dim pick = ShellDialog.Ask(Me, DestructiveDialogs.StopAndClose(dict, RunningName()))
        If pick <> 0 Then Return False

        closeWhenIdle = True
        stopAskedAt = DateTime.Now
        statusLabel.Text = L("shell_closing_after_stop")
        statusLabel.Visible = True
        ShellLog.Info("close: stop requested, the window closes when the run ends")
        If jobViewValue IsNot Nothing Then jobViewValue.RequestStopFromShell()
        commandView.RequestStopFromShell()
        Return False
    End Function

    ' ---- theme -----------------------------------------------------------

    Friend Sub ApplyTheme()
        Ui.SuspendTree(Me)
        Try
            ApplyThemeCore()
        Finally
            Ui.ResumeTree(Me)
        End Try
    End Sub

    Private Sub ApplyThemeCore()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        railHost.BackColor = p.SurfaceAlt
        rail.BackColor = p.SurfaceAlt
        ThemeRailScrollBar()

        rightSide.BackColor = p.Background
        header.BackColor = p.Background
        headerRow.BackColor = p.Background
        pageHost.BackColor = p.Surface

        titleLabel.Font = Theme.FontTitle()
        titleLabel.ForeColor = p.Text
        subtitleLabel.Font = Theme.FontBody()
        subtitleLabel.ForeColor = p.MutedText
        statusLabel.Font = Theme.FontBodyStrong()
        statusLabel.ForeColor = p.Warning

        emptyTitle.Font = Theme.FontSubtitle()
        emptyTitle.ForeColor = p.Text
        emptyHint.Font = Theme.FontBody()
        emptyHint.ForeColor = p.MutedText

        For Each c As Control In rail.Controls
            c.Invalidate()
        Next

        If jobViewValue IsNot Nothing Then jobViewValue.ApplyTheme()
        historyView.ApplyTheme()
        commandView.ApplyTheme()
        aboutView.ApplyTheme()
        settingsView.ApplyTheme()
        Ui.KeepCaptionsReadable(Me)
        Ui.HitTargetFloor(Me)

        Chrome.Apply(Me)
        Invalidate(True)
        ' One theme for the process: a choice made on the Settings page reaches the Disk Manager too.
        AppHost.ThemeChanged()
    End Sub

    ' Moving the window to a display with another scaling changes what a design pixel is worth, so
    ' the minimum size is re-stated rather than left at the old monitor's value.
    Protected Overrides Sub OnDpiChanged(e As DpiChangedEventArgs)
        ShellLog.Debug("shell dpi " & e.DeviceDpiOld.ToString() & " -> " & e.DeviceDpiNew.ToString())
        ' Fonts made from here on are for the new display; the fonts that exist are re-sized by
        ' WinForms once this event returns (Theme.vb, "fonts and the display's scaling").
        Theme.CurrentDpi = e.DeviceDpiNew
        MyBase.OnDpiChanged(e)
        MinimumSize = WindowPlacement.MinimumFor(Me, MinDesignWidth, MinDesignHeight)
        LayoutRail()
    End Sub

    Protected Overrides Sub OnActivated(e As EventArgs)
        MyBase.OnActivated(e)
        Theme.CurrentDpi = DeviceDpi
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

    ' APP-BEHAVIOUR rule 10: the saved rectangle is put back defensively - onto a screen that
    ' exists, with its title strip reachable, at the scale of the monitor it lands on. The decision
    ' is WindowPlacement.Place, a pure function the self-test exercises without a display.
    ' A placement that cannot be restored is logged and the window opens at its default size and
    ' place (SHELL-09): settings never stop the window from opening.
    ' The rectangle a window has to end up at when that is on a display whose scaling is not the
    ' one the layout was built for (the system's), and whether it ends up maximized. A window
    ' created straight onto such a display gets no WM_DPICHANGED - its children stay at the scaling
    ' of the display it was built for, at the wrong size for this one (SP-0016 T4, the mixed-DPI
    ' defect). So it is created on the primary display and moved in OnLoad, the way a user drags it,
    ' which does make WinForms re-scale it.
    Private pendingBounds As Rectangle = Rectangle.Empty
    Private pendingMaximized As Boolean = False

    Private Sub RestorePlacement()
        Try
            Dim p = ShellSettings.LoadPlacement()
            If Not p.HasValue Then Return
            Dim screens As New List(Of WindowPlacement.ScreenArea)
            For Each s As Screen In Screen.AllScreens
                screens.Add(New WindowPlacement.ScreenArea(s.WorkingArea, WindowPlacement.DpiOf(s)))
            Next
            Dim r = WindowPlacement.Place(New Rectangle(p.X, p.Y, p.Width, p.Height), p.Dpi, screens,
                                          SystemInformation.CaptionHeight)
            If Not r.IsEmpty Then
                StartPosition = FormStartPosition.Manual
                Dim targetDpi = WindowPlacement.DpiOf(Screen.FromRectangle(r))
                If targetDpi = Theme.SystemDpi() Then
                    Bounds = r
                Else
                    pendingBounds = r
                    pendingMaximized = p.Maximized
                    Dim wa = Screen.PrimaryScreen.WorkingArea
                    Dim k = Theme.SystemDpi() / CDbl(targetDpi)
                    Bounds = New Rectangle(wa.Left + 40, wa.Top + 40,
                                           Math.Min(wa.Width, CInt(Math.Round(r.Width * k))),
                                           Math.Min(wa.Height, CInt(Math.Round(r.Height * k))))
                    Return
                End If
            End If
            If p.Maximized Then WindowState = FormWindowState.Maximized
        Catch ex As Exception
            ShellLog.Write("restore the window placement", ex)
        End Try
    End Sub

    ' The handle exists, on the primary display: move to the saved display, and once WinForms has
    ' re-scaled for it put the saved rectangle back.
    Protected Overrides Sub OnLoad(e As EventArgs)
        MyBase.OnLoad(e)
        ' The screen the window opens on is known now: the minimum is held to its working area.
        MinimumSize = WindowPlacement.MinimumFor(Me, MinDesignWidth, MinDesignHeight)
        If Not pendingBounds.IsEmpty Then
            Location = pendingBounds.Location
            BeginInvoke(New MethodInvoker(AddressOf FinishPendingPlacement))
        End If
    End Sub

    Private Sub FinishPendingPlacement()
        If pendingBounds.IsEmpty Then Return
        Dim b = pendingBounds
        pendingBounds = Rectangle.Empty
        Try
            Bounds = b
            If pendingMaximized Then WindowState = FormWindowState.Maximized
            ShellLog.Debug("shell placement finished on dpi " & DeviceDpi.ToString() & " at " & b.ToString())
        Catch ex As Exception
            ShellLog.Write("finish the window placement", ex)
        End Try
    End Sub

    ' SHELL-07: the state the window was last shown in, other than minimized. A window maximized,
    ' then minimized, then closed from the taskbar reopens maximized.
    Private lastShownState As FormWindowState = FormWindowState.Normal

    Protected Overrides Sub OnResize(e As EventArgs)
        MyBase.OnResize(e)
        If WindowState <> FormWindowState.Minimized Then lastShownState = WindowState
        If WindowState = FormWindowState.Minimized AndAlso Visible AndAlso ShellSettings.MinimizeToTray() AndAlso AppHost.Current IsNot Nothing Then
            AppHost.Current.MinimizeWindow(Me)
        End If
    End Sub

    Friend Shared Function SavesMaximized(state As FormWindowState, lastShown As FormWindowState) As Boolean
        If state = FormWindowState.Minimized Then Return lastShown = FormWindowState.Maximized
        Return state = FormWindowState.Maximized
    End Function

    Protected Overrides Sub OnFormClosing(e As FormClosingEventArgs)
        If (e.CloseReason = CloseReason.WindowsShutDown OrElse e.CloseReason = CloseReason.TaskManagerClosing) AndAlso AppHost.Current IsNot Nothing Then AppHost.Current.EndingSession()
        ' A close with a run still active is a question, not a close (T5) - whether it came from the
        ' close box, Alt+F4 or another program's WM_CLOSE (which WinForms reports as reason None).
        ' Windows shutting down, or Task Manager, cannot wait for an answer: the run is asked to
        ' stop and the window goes.
        If AnyRunActive() Then
            If e.CloseReason <> CloseReason.WindowsShutDown AndAlso e.CloseReason <> CloseReason.TaskManagerClosing Then
                If Not ConfirmCloseWhileRunning() Then
                    e.Cancel = True
                    Return
                End If
            Else
                If jobViewValue IsNot Nothing Then jobViewValue.RequestStopFromShell()
                commandView.RequestStopFromShell()
            End If
        End If

        Dim b = If(WindowState = FormWindowState.Normal, Bounds, RestoreBounds)
        settingsView.Panel.SaveContext()
        Dim selected = entries.FirstOrDefault(Function(entry) entry.Selected)
        If selected IsNot Nothing Then ShellSettings.SetLastPage(selected.Key)
        ShellSettings.SavePlacement(b.X, b.Y, b.Width, b.Height, SavesMaximized(WindowState, lastShownState), DeviceDpi)
        ShellLog.Debug("shell closed")
        MyBase.OnFormClosing(e)
    End Sub

    ' -debug adds the frame's diagnostics to the shell log.
    Private Sub WriteDiagnosticsIfAsked()
        ShellLog.Debug("shell frame: " & Chrome.Report() & " | dpi=" & DeviceDpi.ToString() &
                       " | size=" & Width.ToString() & "x" & Height.ToString())
    End Sub

End Class
