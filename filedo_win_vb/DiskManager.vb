Imports System.IO
Imports System.Threading.Tasks

' The Disk Manager window (SP-0063): every virtual disk FileDO knows about as a row with its live
' state, and every operation that applies to that row one gesture away.
'
' It is additional to the Disks jobs, not a replacement. The everyday operations - mount, unmount,
' open, save, info, verify, auto-mount, the list's names - run from here as filedo.exe runs built by
' DiskCommands.Build (principle 4: one command line per operation), through a Runner exactly as a job
' runs them: --events, --stop-file, the run report, history.json, the verdict from the result event.
' Everything with parameters or with no way back - new, export, compact, grow, seal, clone, pass,
' format, destroy - opens the existing job page in the shell with the container chosen (spec 7.3), so
' the page that builds a destructive line stays the only one.
'
' Five rules shape this file:
'
'   The window learns state from filedo.exe and nothing else (spec 8). A read is one child process,
'   `vd status json`, coalesced (one at a time, a request while one runs reads again after it) and
'   killed after eight seconds. A read that fails keeps the last good state on screen and says so -
'   never an empty list that looks like "nothing is mounted" (principle 6).
'
'   A state is said in words (principle 1), and obfuscated is never called encrypted (principle 3).
'
'   A disabled action says why (principle 2): in its tooltip, in the menu, and in the detail pane.
'
'   A mount outlives the window (principle 5): closing never unmounts, saves or stops anything; with
'   disks mounted it says so, once per session.
'
'   A credential is asked per operation and lives only in the child's environment, as
'   FILEDO_SHELL_CRED, for the life of that run (spec 7.1) - never in a log, the snapshot, the strip,
'   a tooltip or history.json.
Partial Public Class DiskManagerForm
    Inherits Form

    ' A job page to open in the shell, on a target, with a preset (spec 7.3).
    Public Event JobRequested(key As String, target As String, preset As String)

    ' The FileDO main window, brought forward - the button "Main window" and its key. The other way
    ' round, the shell has its own button that opens this window (ShellForm.BuildHeader).
    Public Event ShellRequested()

    ' The smallest the window may be made, in design pixels - and never more than the working area
    ' of its screen (WindowPlacement.MinimumFor). Below what the pieces need, the window scrolls.
    Friend Const MinWidth As Integer = 700
    Friend Const MinHeight As Integer = 480
    ' The list is the window's point: whatever else is open, it is given this much height (design
    ' pixels, about four rows and the heading) and the window scrolls for the rest.
    Private Const ListMinHeight As Integer = 150
    Private Const PollMs As Integer = 5000
    Private Const SettleMs As Integer = 400

    Private Const WM_SETTINGCHANGE As Integer = &H1A
    Private Const WM_DEVICECHANGE As Integer = &H219
    Private Const DBT_DEVICEARRIVAL As Integer = &H8000
    Private Const DBT_DEVICEREMOVECOMPLETE As Integer = &H8004

    ' The list's columns, in order. Path and Mounted since start hidden (width 0) and are shown
    ' from the More menu.
    Private Enum Col
        Name = 0
        Drive
        State
        Profile
        Protection
        Size
        Auto
        Path
        Since
        ' SP-0148: File or Partition - where the container's bytes live. Last, so a saved layout of
        ' the columns before it keeps its widths.
        Carrier
    End Enum

    Private Shared ReadOnly ColumnKeys As String() = {
        "vd_col_name", "vd_col_drive", "vd_col_state", "vd_col_profile", "vd_mgr_col_protection",
        "vd_col_size", "vd_mgr_col_auto", "vd_col_file", "vd_mgr_col_since", "vd_mgr_col_carrier"}
    Private Shared ReadOnly ColumnDesign As Integer() = {140, 52, 210, 64, 96, 64, 72, 0, 0, 72}
    Private Shared ReadOnly ColumnShown As Integer() = {150, 56, 240, 70, 100, 76, 72, 280, 140, 80}

    ' The toolbar's own row actions and the detail pane's buttons, in the order they are offered.
    Private Shared ReadOnly DetailActions As DiskAction() = {
        DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.Unmount, DiskAction.UnmountImage, DiskAction.OpenDrive,
        DiskAction.SaveNow, DiskAction.Info, DiskAction.Verify, DiskAction.RepairVolume,
        DiskAction.AutoOn, DiskAction.AutoOff,
        DiskAction.Autostart, DiskAction.AddToList, DiskAction.ChangePassword, DiskAction.ImageToFile,
        DiskAction.ShareDisk, DiskAction.OpenShared, DiskAction.CloseShared, DiskAction.UnshareDisk,
        DiskAction.ShareAutoOn, DiskAction.ShareAutoOff}
    Private Shared ReadOnly ToolbarRowActions As DiskAction() = {
        DiskAction.Mount, DiskAction.Unmount, DiskAction.OpenDrive, DiskAction.SaveNow}

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly tips As New ToolTip()

    Private root As TableLayoutPanel
    Private settingsBtn As GlyphButton
    Private toolbar As FlowLayoutPanel
    Private filterUnit As FlowLayoutPanel
    Private newBtn As GlyphButton
    Private addBtn As GlyphButton
    Private mountBtn As GlyphButton
    Private unmountBtn As GlyphButton
    Private openBtn As GlyphButton
    Private saveBtn As GlyphButton
    Private moreBtn As GlyphButton
    Private refreshBtn As GlyphButton
    Private helpBtn As GlyphButton
    Private mainBtn As GlyphButton
    Private filterLabel As Label
    Private filterBox As TextBox
    Private filterClear As GlyphButton
    Private list As DiskListView
    Private glyphImages As ImageList
    Private emptyLabel As Label
    Private emptyHost As TableLayoutPanel
    Private emptyActions As FlowLayoutPanel
    Private emptyNew As GlyphButton
    Private emptyAdd As GlyphButton
    Private emptyLearn As LinkLabel
    Private listHost As Panel
    Private detailPanel As TableLayoutPanel
    Private detailHeader As TableLayoutPanel
    Private detailTitle As Label
    Private detailClose As GlyphButton
    Private detailText As Label
    Private detailButtons As FlowLayoutPanel
    Private detailBar As FlowLayoutPanel
    Private detailShow As GlyphButton
    Private strip As TableLayoutPanel
    Private summaryLabel As Label
    Private opFlow As FlowLayoutPanel
    Private opLabel As Label
    Private opBar As ProgressBar
    Private stopBtn As GlyphButton
    Private clearQueueBtn As GlyphButton
    Private reportLink As LinkLabel
    Private rowMenu As ContextMenuStrip
    Private spaceMenu As ContextMenuStrip
    Private moreMenu As ContextMenuStrip
    Private helpMenu As ContextMenuStrip
    ' The row under the pointer, for its hover tint (-1 for none).
    Private hoverRow As Integer = -1

    Private pollTimer As Windows.Forms.Timer
    Private clockTimer As Windows.Forms.Timer
    Private settleTimer As Windows.Forms.Timer
    Private watcher As FileSystemWatcher

    ' ---- what the window knows ------------------------------------------------

    Private snapshot As DiskSnapshot = Nothing
    Private lastGoodAt As DateTime = DateTime.MinValue
    Private staleKey As String = ""
    Private reading As Boolean = False
    Private readAgain As Boolean = False
    ' Reads still owed after an operation ended: the first read after it can fail while the machine
    ' is busy letting go of the disk (the iSCSI session of a repair), and the row would stay as it was
    ' for a whole poll. A failed one is asked again at once, a few times, instead of waiting.
    Private postJobReads As Integer = 0
    Private shown_ As New List(Of DiskRecord)
    Private sortColumn As Integer = -1
    Private sortDescending As Boolean = False

    ' An operation started from this window: its row, its line and the runner it runs on.
    Private Class DiskOp
        Public Action As DiskAction
        Public Record As DiskRecord
        Public Args As List(Of String)
        Public Env As Dictionary(Of String, String)
        Public Run As Runner
        Public Stopped As Boolean
        ' SP-0148: a run that changes a disk's layout or a partition's name reads the disk list again.
        Public ChangesDisks As Boolean
        Public SharingSteps As List(Of DiskSharingStep)
        Public CompletedSharing As New List(Of String)
        Public SharingFailure As String = ""
    End Class

    Private ReadOnly queue As New List(Of DiskOp)
    Private ReadOnly running As New List(Of DiskOp)
    ' The verb running (or "queued") on a row, by the row's key.
    Private ReadOnly busy As New Dictionary(Of String, String)(StringComparer.Ordinal)
    ' What `info` last said about a row, for the detail pane.
    Private ReadOnly infoByKey As New Dictionary(Of String, String)(StringComparer.Ordinal)
    Private lastOutcome As String = ""
    Private lastOutcomeWarns As Boolean = False
    Private lastReport As String = ""

    ' Closing while something runs (spec 7.4): the window closes itself when the last run has ended.
    Private closeWhenIdle As Boolean = False
    ' The "your disks stay mounted" notice is said once per session.
    Private Shared closeNoticeSaid As Boolean = False

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        Theme.Refresh()
        sortColumn = ShellSettings.DiskManagerSort(sortDescending)
        BuildLayout()
        ApplyTheme()
        RestorePlacement()
        SetDetailOpen(ShellSettings.DiskManagerDetailOpen())
        Theme.Watch(Me, AddressOf ApplyTheme)
        ' Nothing on the toolbar has the keyboard when the window opens: a focused button draws its
        ' frame at once and looks like a chosen default. The filter has it until the list has rows.
        ActiveControl = filterBox

        pollTimer = New Windows.Forms.Timer With {.Interval = PollMs}
        AddHandler pollTimer.Tick, Sub()
                                       ' Liveness and a ram buffer's dirt change no file: they are read
                                       ' on a clock, and only while somebody can see the answer.
                                       If Visible AndAlso WindowState <> FormWindowState.Minimized Then RequestRead()
                                   End Sub
        clockTimer = New Windows.Forms.Timer With {.Interval = 1000}
        AddHandler clockTimer.Tick, Sub() UpdateStrip()
        settleTimer = New Windows.Forms.Timer With {.Interval = SettleMs}
        AddHandler settleTimer.Tick, Sub()
                                         settleTimer.Stop()
                                         RequestRead()
                                     End Sub
        RefreshView()
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
        Text = L("vd_mgr_title")
        AutoScaleMode = AutoScaleMode.Font
        MinimumSize = WindowPlacement.MinimumFor(Me, MinWidth, MinHeight)
        Size = WindowPlacement.FitMinimum(Ui.PxSize(Me, 980, 600), Screen.PrimaryScreen.WorkingArea.Size)
        StartPosition = FormStartPosition.CenterScreen
        AutoScroll = True
        KeyPreview = True
        DoubleBuffered = True
        ShowInTaskbar = True

        root = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 6,
            .Margin = New Padding(0),
            .Padding = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        BuildToolbar()
        BuildList()
        BuildDetail()
        BuildStrip()
        BuildMenus()

        root.Controls.Add(toolbar, 0, 0)
        root.Controls.Add(filterUnit, 0, 1)
        root.Controls.Add(listHost, 0, 2)
        root.Controls.Add(detailPanel, 0, 3)
        root.Controls.Add(detailBar, 0, 4)
        root.Controls.Add(strip, 0, 5)
        Controls.Add(root)
        ' The tab order the spec names (6.2): toolbar, filter, list, detail, strip.
        toolbar.TabIndex = 0
        filterUnit.TabIndex = 1
        listHost.TabIndex = 2
        detailPanel.TabIndex = 3
        detailBar.TabIndex = 4
        strip.TabIndex = 5
        ' Everything above and below the list is as tall as its content needs, so in a short window the
        ' list was what gave way - to nothing, under two rows of buttons and the detail text. The
        ' list keeps a floor and the window scrolls instead (compact screens).
        For Each c As Control In New Control() {toolbar, filterUnit, detailPanel, detailBar, strip}
            AddHandler c.SizeChanged, Sub() KeepListRoom()
        Next
        ResumeLayout(True)
        KeepListRoom()
    End Sub

    ' A button with a glyph and its caption, painted in the palette (GlyphButton). The tooltip and the
    ' accessible name are the label; a button with no caption takes the meaning's canonical name.
    Private Function NewGlyphButton(labelKey As String, glyph As GlyphRef, Optional tier As Integer = 24,
                                    Optional iconOnly As Boolean = False) As GlyphButton
        Dim b As New GlyphButton With {
            .Glyph = glyph,
            .Tier = tier,
            .IconOnly = iconOnly,
            .Text = If(iconOnly, "", L(labelKey)),
            .Margin = Ui.PxPad(Me, 0, 0, 6, 6)
        }
        b.AccessibleName = L(labelKey)
        If bodyFont IsNot Nothing Then b.Font = bodyFont
        Return b
    End Function

    Private Sub BuildToolbar()
        ' One flow of buttons that wraps when the window is narrow or the captions are long. (A wrapping
        ' flow inside an auto-sized table row is measured for no width at all and comes out as tall as a
        ' column of every button - so the toolbar is the flow itself.) The filter is not in it: it is
        ' one unit of its own, a row under the toolbar.
        toolbar = New FlowLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Padding = Ui.PxPad(Me, 12, 10, 12, 4),
            .Margin = New Padding(0)
        }

        newBtn = NewGlyphButton("vd_mgr_btn_new", DiskGlyphs.For(DiskAction.NewDisk))
        AddHandler newBtn.Click, Sub() Perform(DiskAction.NewDisk)
        addBtn = NewGlyphButton("vd_mgr_btn_add", DiskGlyphs.For(DiskAction.AddToList))
        AddHandler addBtn.Click, Sub() BrowseAndAdd()
        mountBtn = NewGlyphButton("vd_mgr_btn_mount", DiskGlyphs.For(DiskAction.Mount))
        AddHandler mountBtn.Click, Sub() Perform(DiskAction.Mount)
        unmountBtn = NewGlyphButton("vd_mgr_btn_unmount", DiskGlyphs.For(DiskAction.Unmount))
        AddHandler unmountBtn.Click, Sub() Perform(UnmountActionFor(SelectedRecords()))
        openBtn = NewGlyphButton("vd_mgr_btn_open", DiskGlyphs.For(DiskAction.OpenDrive))
        AddHandler openBtn.Click, Sub() Perform(DiskAction.OpenDrive)
        saveBtn = NewGlyphButton("vd_mgr_btn_save", DiskGlyphs.For(DiskAction.SaveNow))
        AddHandler saveBtn.Click, Sub() Perform(DiskAction.SaveNow)
        moreBtn = NewGlyphButton("vd_mgr_btn_more", DiskGlyphs.More)
        AddHandler moreBtn.Click, Sub()
                                      BuildActionMenu(moreMenu, SelectedRecords(), True)
                                      moreMenu.Show(moreBtn, 0, moreBtn.Height)
                                  End Sub
        refreshBtn = NewGlyphButton("vd_mgr_btn_refresh", DiskGlyphs.For(DiskAction.Refresh), 24, True)
        AddHandler refreshBtn.Click, Sub() RequestRead()
        settingsBtn = NewGlyphButton("rail_job_settings", GlyphRef.Vocabulary("app.settings"), 24, True)
        AddHandler settingsBtn.Click, Sub()
                                         If AppHost.Current IsNot Nothing Then
                                             AppHost.Current.ShowSettings()
                                         Else
                                             Using dlg As New DiskSettingsDialog()
                                                 dlg.ShowDialog(Me)
                                             End Using
                                         End If
                                     End Sub
        helpBtn = NewGlyphButton("vd_mgr_name_help", DiskGlyphs.Help, 24, True)
        AddHandler helpBtn.Click, Sub() helpMenu.Show(helpBtn, 0, helpBtn.Height)
        ' The way to the main window: the product's own mark and its name, at the end of the toolbar.
        mainBtn = NewGlyphButton("vd_mgr_btn_main", Nothing, 24)
        mainBtn.Picture = AppIcon.Mark(Ui.Px(Me, 24))
        AddHandler mainBtn.Click, Sub() RaiseEvent ShellRequested()

        ' The filter: its label, its box and the cross that clears it, one unit - a row of its own under
        ' the toolbar, so the action buttons keep the whole width and the unit never comes apart.
        filterUnit = New FlowLayoutPanel With {
            .Anchor = AnchorStyles.Left,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 12, 0, 12, 6)
        }
        filterLabel = New Label With {.Text = L("vd_mgr_filter"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 6, 0)}
        filterBox = New TextBox With {.Width = Ui.Px(Me, 150), .Margin = Ui.PxPad(Me, 0, 4, 0, 0)}
        filterBox.AccessibleName = L("vd_mgr_filter_name")
        filterLabel.AccessibleName = L("vd_mgr_filter_name")
        AddHandler filterBox.TextChanged, Sub()
                                              filterClear.Enabled = (filterBox.Text <> "")
                                              RefreshView()
                                          End Sub
        filterClear = NewGlyphButton("vd_mgr_name_clear", DiskGlyphs.ClearInput, 16, True)
        filterClear.Margin = Ui.PxPad(Me, 2, 2, 0, 2)
        filterClear.Enabled = False
        AddHandler filterClear.Click, Sub()
                                          filterBox.Clear()
                                          filterBox.Focus()
                                      End Sub
        For Each c As Control In New Control() {filterLabel, filterBox, filterClear}
            filterUnit.Controls.Add(c)
        Next

        For Each c As Control In New Control() {newBtn, addBtn, mountBtn, unmountBtn, openBtn, saveBtn, moreBtn, refreshBtn, settingsBtn, helpBtn, mainBtn}
            toolbar.Controls.Add(c)
        Next
        ' The groups, told apart by a wider gap: the list, the disk, and the window's own.
        For Each c As Control In New Control() {addBtn, saveBtn, moreBtn, helpBtn}
            c.Margin = Ui.PxPad(Me, 0, 0, 20, 6)
        Next
        For i = 0 To toolbar.Controls.Count - 1
            toolbar.Controls(i).TabIndex = i
        Next
        filterLabel.TabIndex = 0
        filterBox.TabIndex = 1
        filterClear.TabIndex = 2
        filterLabel.TabStop = False

        ' A disabled button shows no tooltip of its own, and a disabled action must still say why
        ' (principle 2): the toolbar shows it for the button under the pointer.
        AddHandler toolbar.MouseMove, AddressOf Toolbar_MouseMove
    End Sub

    Private hoveredDisabled As Control = Nothing

    Private Sub Toolbar_MouseMove(sender As Object, e As MouseEventArgs)
        Dim c = toolbar.GetChildAtPoint(e.Location)
        If c Is hoveredDisabled Then Return
        hoveredDisabled = Nothing
        tips.Hide(toolbar)
        If c Is Nothing OrElse c.Enabled Then Return
        Dim reason = TryCast(c.Tag, String)
        If String.IsNullOrEmpty(reason) Then Return
        hoveredDisabled = c
        tips.Show(reason, toolbar, c.Left, c.Bottom + Ui.Px(Me, 2), 6000)
    End Sub

    Private Sub BuildList()
        listHost = New Panel With {.Dock = DockStyle.Fill, .Padding = Ui.PxPad(Me, 12, 0, 12, 8), .Margin = New Padding(0)}
        list = New DiskListView With {
            .Dock = DockStyle.Fill,
            .View = View.Details,
            .FullRowSelect = True,
            .HideSelection = False,
            .MultiSelect = True,
            .AllowDrop = True,
            .OwnerDraw = True,
            .ShowItemToolTips = True,
            .BorderStyle = BorderStyle.FixedSingle
        }
        list.AccessibleName = L("vd_mgr_list_name")
        For i = 0 To ColumnKeys.Length - 1
            list.Columns.Add(L(ColumnKeys(i)), Ui.Px(Me, ColumnDesign(i)))
        Next
        glyphImages = New ImageList With {.ColorDepth = ColorDepth.Depth32Bit, .ImageSize = Ui.PxSize(Me, 16, 16)}
        list.SmallImageList = glyphImages
        AddHandler list.DrawColumnHeader, AddressOf List_DrawColumnHeader
        ' The rows are drawn from the palette (List_DrawSubItem): the system's highlight is a dark teal
        ' with dark text on it, unreadable in both themes.
        AddHandler list.DrawItem, Sub(s, e) e.DrawDefault = False
        AddHandler list.DrawSubItem, AddressOf List_DrawSubItem
        AddHandler list.MouseMove, AddressOf List_MouseMove
        AddHandler list.MouseLeave, Sub() SetHoverRow(-1)
        AddHandler list.SelectedIndexChanged, Sub() SelectionChanged()
        AddHandler list.SizeChanged, Sub() FitStateColumn()
        AddHandler list.ColumnWidthChanged, Sub() FitStateColumn()
        AddHandler list.GotFocus, Sub() list.Invalidate()
        AddHandler list.LostFocus, Sub() list.Invalidate()
        AddHandler list.MouseDoubleClick, AddressOf List_MouseDoubleClick
        AddHandler list.MouseUp, AddressOf List_MouseUp
        AddHandler list.ColumnClick, AddressOf List_ColumnClick
        AddHandler list.DragEnter, AddressOf List_DragEnter
        AddHandler list.DragDrop, AddressOf List_DragDrop
        AddHandler list.HandleCreated, Sub() ApplyListChrome()

        ' What an empty list means, said in words over the list rather than left as a blank table - and,
        ' when nothing is in the list yet, the ways to start: create a disk, add a file, read the guide.
        emptyHost = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 3,
            .Visible = False,
            .Margin = New Padding(0)
        }
        emptyHost.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        emptyHost.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))
        emptyHost.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        emptyHost.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))
        emptyLabel = New Label With {
            .AutoSize = True,
            .Anchor = AnchorStyles.Bottom,
            .TextAlign = ContentAlignment.MiddleCenter,
            .Margin = Ui.PxPad(Me, 16, 16, 16, 8)
        }
        Ui.Wrap(emptyLabel, emptyHost, Ui.Px(Me, 40))
        emptyActions = New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Anchor = AnchorStyles.None,
            .WrapContents = True,
            .Margin = New Padding(0)
        }
        emptyNew = NewGlyphButton("vd_mgr_act_new", DiskGlyphs.For(DiskAction.NewDisk))
        AddHandler emptyNew.Click, Sub() Perform(DiskAction.NewDisk)
        emptyAdd = NewGlyphButton("vd_mgr_btn_add", DiskGlyphs.For(DiskAction.AddToList))
        AddHandler emptyAdd.Click, Sub() BrowseAndAdd()
        emptyLearn = New LinkLabel With {.Text = L("vd_mgr_learn_more"), .AutoSize = True, .Margin = Ui.PxPad(Me, 8, 6, 0, 0)}
        emptyLearn.AccessibleName = L("vd_mgr_learn_more")
        AddHandler emptyLearn.LinkClicked, Sub() Links.Open(Me, Links.DiskGuide)
        emptyActions.Controls.Add(emptyNew)
        emptyActions.Controls.Add(emptyAdd)
        emptyActions.Controls.Add(emptyLearn)
        emptyHost.Controls.Add(emptyLabel, 0, 0)
        emptyHost.Controls.Add(emptyActions, 0, 1)
        AddHandler emptyHost.MouseUp, Sub(s, e)
                                          If e.Button = MouseButtons.Right Then spaceMenu.Show(emptyHost, e.Location)
                                      End Sub
        listHost.Controls.Add(emptyHost)
        listHost.Controls.Add(list)
    End Sub

    Private Sub BuildDetail()
        detailPanel = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 3,
            .Padding = Ui.PxPad(Me, 12, 8, 12, 8),
            .Margin = New Padding(0)
        }
        detailPanel.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        For i = 0 To 2
            detailPanel.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        Next
        ' The title and, at the right edge of the block, the cross that hides it (nav.close: it closes
        ' the panel and nothing is lost). Showing it again is the bar below the list, or the More menu.
        detailHeader = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 2,
            .RowCount = 1,
            .Margin = New Padding(0)
        }
        detailHeader.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        detailHeader.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        detailHeader.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        detailTitle = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 8, 4)}
        detailClose = NewGlyphButton("vd_mgr_name_close", DiskGlyphs.CloseIt, 16, True)
        detailClose.Anchor = AnchorStyles.Top Or AnchorStyles.Right
        detailClose.Margin = New Padding(0)
        AddHandler detailClose.Click, Sub() ToggleDetail()
        detailHeader.Controls.Add(detailTitle, 0, 0)
        detailHeader.Controls.Add(detailClose, 1, 0)
        detailText = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}
        detailButtons = New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Fill,
            .Margin = New Padding(0)
        }
        detailPanel.Controls.Add(detailHeader, 0, 0)
        detailPanel.Controls.Add(detailText, 0, 1)
        detailPanel.Controls.Add(detailButtons, 0, 2)
        Ui.Wrap(detailTitle, detailHeader, Ui.Px(Me, 40))
        Ui.Wrap(detailText, detailPanel, Ui.Px(Me, 24))

        ' What stands in the pane's place while it is hidden: one slim button that brings it back.
        detailBar = New FlowLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = False,
            .Padding = Ui.PxPad(Me, 12, 4, 12, 4),
            .Margin = New Padding(0),
            .Visible = False
        }
        detailShow = NewGlyphButton("vd_mgr_detail_show", DiskGlyphs.ShowDetails, 16)
        detailShow.Margin = New Padding(0)
        AddHandler detailShow.Click, Sub() ToggleDetail()
        detailBar.Controls.Add(detailShow)
    End Sub

    Private Sub ToggleDetail()
        SetDetailOpen(Not detailOpenState)
        ShellSettings.SetDiskManagerDetailOpen(detailOpenState)
    End Sub

    Private Sub BuildStrip()
        ' Two lines: what is known (how many mounted, the transport, how fresh), then what runs now or
        ' how the last run ended. A long sentence in any locale wraps instead of being cut.
        strip = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 2,
            .Padding = Ui.PxPad(Me, 12, 6, 12, 6),
            .Margin = New Padding(0)
        }
        strip.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        strip.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        strip.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        summaryLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        summaryLabel.AccessibleRole = AccessibleRole.StatusBar

        opFlow = New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .FlowDirection = FlowDirection.LeftToRight,
            .Dock = DockStyle.Fill,
            .Margin = New Padding(0)
        }
        opLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 8, 0)}
        opLabel.AccessibleRole = AccessibleRole.StatusBar
        opBar = New ProgressBar With {.Width = Ui.Px(Me, 120), .Height = Ui.Px(Me, 14), .Margin = Ui.PxPad(Me, 0, 5, 8, 0), .Visible = False}
        stopBtn = NewGlyphButton("vd_mgr_btn_stop", DiskGlyphs.StopRun, 16)
        stopBtn.Visible = False
        AddHandler stopBtn.Click, Sub() StopStoppable()
        clearQueueBtn = NewGlyphButton("vd_mgr_btn_clear_queue", DiskGlyphs.ClearQueue, 16)
        clearQueueBtn.Visible = False
        AddHandler clearQueueBtn.Click, Sub() ClearQueue()
        reportLink = New LinkLabel With {.Text = L("vd_mgr_report_link"), .AutoSize = True, .Visible = False, .Margin = Ui.PxPad(Me, 0, 4, 0, 0)}
        reportLink.AccessibleName = L("vd_mgr_report_link")
        AddHandler reportLink.LinkClicked, Sub() OpenLastReport()
        For Each c As Control In New Control() {opLabel, opBar, stopBtn, clearQueueBtn, reportLink}
            opFlow.Controls.Add(c)
        Next
        strip.Controls.Add(summaryLabel, 0, 0)
        strip.Controls.Add(opFlow, 0, 1)
        Ui.Wrap(summaryLabel, strip, Ui.Px(Me, 24))
        Ui.Wrap(opLabel, strip, Ui.Px(Me, 320))
    End Sub

    Private Sub BuildMenus()
        rowMenu = New ContextMenuStrip With {.ShowItemToolTips = True}
        moreMenu = New ContextMenuStrip With {.ShowItemToolTips = True}
        spaceMenu = New ContextMenuStrip With {.ShowItemToolTips = True}
        helpMenu = New ContextMenuStrip With {.ShowItemToolTips = True}
        BuildSpaceMenu()
        BuildHelpMenu()
        ' Whether this build can mount an image is known once the state has been read.
        AddHandler spaceMenu.Opening, Sub() BuildSpaceMenu()
        AddHandler helpMenu.Opening, Sub() BuildHelpMenu()
    End Sub

    ' Right-click on empty space (spec 6.2): New disk.., Add.., Mount image.., Refresh.
    Private Sub BuildSpaceMenu()
        spaceMenu.Items.Clear()
        spaceMenu.Items.Add(MenuItem(DiskAction.NewDisk, Nothing))
        Dim addItem As New ToolStripMenuItem(L("vd_mgr_btn_add")) With {.ShortcutKeyDisplayString = DiskShortcuts.KeyText(Keys.Control Or Keys.O)}
        AddHandler addItem.Click, Sub() BrowseAndAdd()
        addItem.Image = MenuImage(DiskGlyphs.For(DiskAction.AddToList), True, False)
        spaceMenu.Items.Add(addItem)
        If Not DiskStates.HiddenInBuild(DiskAction.MountImage, Context()) Then spaceMenu.Items.Add(MenuItem(DiskAction.MountImage, Nothing))
        If Not DiskStates.HiddenInBuild(DiskAction.Adopt, Context()) Then spaceMenu.Items.Add(MenuItem(DiskAction.Adopt, Nothing))
        spaceMenu.Items.Add(New ToolStripSeparator())
        spaceMenu.Items.Add(MenuItem(DiskAction.Refresh, Nothing))
        ThemeMenu(spaceMenu)
    End Sub

    ' The Help button's menu: the help window, the first steps, and the places to read more - the
    ' guide of this window, the guides, the site, the project's documentation and its issue tracker.
    ' A link is followed only when it is clicked (APP-BEHAVIOUR rule 4).
    Private Sub BuildHelpMenu()
        helpMenu.Items.Clear()
        Dim helpItem As New ToolStripMenuItem(L("vd_help_menu_help")) With {.ShortcutKeyDisplayString = DiskShortcuts.KeyText(Keys.F1)}
        helpItem.Image = MenuImage(DiskGlyphs.Help, True, False)
        AddHandler helpItem.Click, Sub() ShowHelp()
        helpMenu.Items.Add(helpItem)
        Dim firstItem As New ToolStripMenuItem(L("vd_help_menu_first"))
        firstItem.Image = MenuImage(DiskGlyphs.DiskContainer, True, False)
        AddHandler firstItem.Click, Sub() ShowWelcome()
        helpMenu.Items.Add(firstItem)
        helpMenu.Items.Add(New ToolStripSeparator())
        For Each kv In DiskHelpLinks.All
            Dim url = kv.Value
            Dim item As New ToolStripMenuItem(L(kv.Key)) With {.ToolTipText = url}
            item.Image = MenuImage(DiskGlyphs.OpenExternal, True, False)
            AddHandler item.Click, Sub() Links.Open(Me, url)
            helpMenu.Items.Add(item)
        Next
        ThemeMenu(helpMenu)
    End Sub

    ' The menu picture of a meaning: 16 px at this window's scale, in the text colour of the menu, the
    ' disabled text colour when the item does not apply, the danger colour for a destructive one.
    Private Function MenuImage(g As GlyphRef, enabled As Boolean, danger As Boolean) As Image
        Dim p = Theme.Current
        Dim colour = If(Not enabled, p.TextDisabled, If(danger, p.Danger, p.Text))
        Return GlyphBitmaps.Render(g, Ui.Px(Me, 16), colour)
    End Function

    Private Sub ShowHelp()
        Using dlg As New DiskHelpDialog(dict, Me, Context().Packaged)
            dlg.ShowDialog(Me)
        End Using
    End Sub

    ' The first-steps window, from Help or the first time the window opens. What the user picks is
    ' done afterwards: create a disk (the New page), add a file (the file dialog), read the guide.
    Private Sub ShowWelcome()
        Dim answer As DiskWelcomeAnswer
        Using dlg As New DiskWelcomeDialog(dict, Me, Context().Packaged)
            dlg.ShowDialog(Me)
            answer = dlg.Answer
        End Using
        Select Case answer
            Case DiskWelcomeAnswer.CreateDisk : Perform(DiskAction.NewDisk)
            Case DiskWelcomeAnswer.AddExisting : BrowseAndAdd()
            Case DiskWelcomeAnswer.OpenGuide : Links.Open(Me, Links.DiskGuide)
        End Select
    End Sub

    ' ---- the rows --------------------------------------------------------

    ' The disks as the filter and the sort show them.
    Private Function VisibleRecords() As List(Of DiskRecord)
        Dim out As New List(Of DiskRecord)
        If snapshot Is Nothing Then Return out
        Dim needle = filterBox.Text.Trim()
        For Each r In snapshot.Disks
            If needle = "" OrElse Matches(r, needle) Then out.Add(r)
        Next
        out.Sort(AddressOf CompareRows)
        Return out
    End Function

    Private Function Matches(r As DiskRecord, needle As String) As Boolean
        For Each hay In New String() {DisplayName(r), r.Path, r.Letter, RowStateText(r), r.Profile, r.PartitionPlace, CarrierText(r)}
            If hay IsNot Nothing AndAlso hay.IndexOf(needle, StringComparison.CurrentCultureIgnoreCase) >= 0 Then Return True
        Next
        Return False
    End Function

    Private Function CompareRows(a As DiskRecord, b As DiskRecord) As Integer
        If sortColumn < 0 Then Return DiskStates.DefaultOrder(a, b)
        Dim c As Integer
        Select Case CType(sortColumn, Col)
            Case Col.Drive : c = String.Compare(a.Letter, b.Letter, StringComparison.OrdinalIgnoreCase)
            Case Col.State : c = CInt(RowState(a)).CompareTo(CInt(RowState(b)))
            Case Col.Profile : c = String.Compare(DiskStates.ProfileText(a), DiskStates.ProfileText(b), StringComparison.OrdinalIgnoreCase)
            Case Col.Protection : c = CInt(a.Protection).CompareTo(CInt(b.Protection))
            Case Col.Size : c = a.LogicalSize.CompareTo(b.LogicalSize)
            Case Col.Auto : c = a.AutoMount.CompareTo(b.AutoMount)
            Case Col.Path : c = String.Compare(a.Path, b.Path, StringComparison.OrdinalIgnoreCase)
            Case Col.Since : c = Nullable.Compare(a.MountedAt, b.MountedAt)
            Case Col.Carrier : c = String.Compare(a.Carrier, b.Carrier, StringComparison.Ordinal)
            Case Else : c = String.Compare(DisplayName(a), DisplayName(b), StringComparison.CurrentCultureIgnoreCase)
        End Select
        If c = 0 Then c = DiskStates.DefaultOrder(a, b)
        Return If(sortDescending, -c, c)
    End Function

    Private Function DisplayName(r As DiskRecord) As String
        If r.IsImage Then Return L("vd_mgr_name_image")
        Return r.BaseName
    End Function

    Private Function BusyVerb(r As DiskRecord) As String
        Dim v As String = Nothing
        If r IsNot Nothing AndAlso busy.TryGetValue(r.Key, v) Then Return v
        Return ""
    End Function

    Private Function RowState(r As DiskRecord) As DiskRowState
        Return DiskStates.StateOf(r, BusyVerb(r))
    End Function

    Private Function RowStateText(r As DiskRecord) As String
        Return DiskStates.StateText(r, RowState(r), BusyVerb(r), dict)
    End Function

    Private Function ImageIndexOf(state As DiskRowState) As Integer
        Dim g = DiskStates.GlyphOf(state)
        If g Is Nothing Then Return 0
        Select Case g.Id
            Case "status.ok" : Return 1
            Case "status.warning" : Return 2
            Case "status.error" : Return 3
        End Select
        Return 0
    End Function

    Private Function CellsOf(r As DiskRecord) As String()
        Dim since = ""
        If r.MountedAt.HasValue Then since = r.MountedAt.Value.LocalDateTime.ToString("g")
        Return New String() {
            DisplayName(r),
            r.Letter,
            RowStateText(r),
            DiskStates.ProfileText(r),
            DiskStates.ProtectionText(r, dict),
            If(r.IsImage OrElse r.LogicalSize <= 0, "-", DiskStates.SizeText(r.LogicalSize)),
            If(r.AutoMount, L("vd_mgr_auto_yes"), ""),
            If(r.IsPartition, r.PartitionPlace, r.Path),
            since,
            CarrierText(r)}
    End Function

    ' The Carrier column (SP-0148 10): the same disk glyph for both, told apart by this word.
    Private Function CarrierText(r As DiskRecord) As String
        If r.IsImage Then Return ""
        Return L(If(r.IsPartition, "vd_part_carrier_partition", "vd_part_carrier_file"))
    End Function

    ' The whole view from what the window knows: the list (updated in place when the rows are the
    ' same rows in the same order, so a refresh every five seconds neither flickers nor loses the
    ' scroll position), the detail pane, the toolbar and the strip.
    Private Sub RefreshView()
        Dim keepSelected As New HashSet(Of String)(SelectedRecords().Select(Function(r) r.Key), StringComparer.Ordinal)
        Dim focusedKey As String = Nothing
        If list.FocusedItem IsNot Nothing Then focusedKey = TryCast(list.FocusedItem.Tag, String)

        Dim wanted = VisibleRecords()
        Dim sameRows = wanted.Count = list.Items.Count
        If sameRows Then
            For i = 0 To wanted.Count - 1
                If Not String.Equals(TryCast(list.Items(i).Tag, String), wanted(i).Key, StringComparison.Ordinal) Then
                    sameRows = False
                    Exit For
                End If
            Next
        End If
        shown_ = wanted

        rebuilding = True
        list.BeginUpdate()
        Try
            If sameRows Then
                For i = 0 To wanted.Count - 1
                    Dim it = list.Items(i)
                    Dim cells = CellsOf(wanted(i))
                    For c = 0 To cells.Length - 1
                        If it.SubItems(c).Text <> cells(c) Then it.SubItems(c).Text = cells(c)
                    Next
                    Dim idx = ImageIndexOf(RowState(wanted(i)))
                    If it.ImageIndex <> idx Then it.ImageIndex = idx
                    Dim tip = RowTip(wanted(i))
                    If it.ToolTipText <> tip Then it.ToolTipText = tip
                Next
            Else
                list.Items.Clear()
                For Each r In wanted
                    Dim cells = CellsOf(r)
                    Dim it As New ListViewItem(cells(0), ImageIndexOf(RowState(r))) With {.Tag = r.Key, .ToolTipText = RowTip(r)}
                    For c = 1 To cells.Length - 1
                        it.SubItems.Add(cells(c))
                    Next
                    list.Items.Add(it)
                Next
                For Each it As ListViewItem In list.Items
                    Dim k = TryCast(it.Tag, String)
                    it.Selected = keepSelected.Contains(k)
                    If k = focusedKey Then it.Focused = True
                Next
            End If
        Finally
            list.EndUpdate()
            rebuilding = False
        End Try

        ' An empty list says what it means: nothing registered, a filter that hides everything, or a
        ' state that could not be read yet - never a blank table that reads as "nothing is mounted".
        Dim emptyText = ""
        If snapshot Is Nothing Then
            emptyText = If(staleKey = "", L("vd_mgr_reading"), Localization.Format(L("vd_mgr_never_read_fmt"), L(staleKey)))
        ElseIf wanted.Count = 0 Then
            emptyText = If(snapshot.Disks.Count = 0, L("vd_mgr_empty"), L("vd_mgr_filter_empty"))
        End If
        emptyLabel.Text = emptyText
        emptyHost.Visible = (emptyText <> "")
        ' The ways to start are offered when nothing is in the list yet - not while a state is being
        ' read, and not over a filter that hides everything.
        emptyActions.Visible = (snapshot IsNot Nothing AndAlso snapshot.Disks.Count = 0)
        list.Visible = (emptyText = "")
        emptyShown = emptyText
        ' The first time the list has rows the keyboard goes to it - not to a button, whose focus frame
        ' would look like a chosen default the moment the window opens. Only while nothing else was
        ' chosen: a user who has gone to the filter keeps it.
        If Not listFocusGiven AndAlso list.Visible AndAlso IsHandleCreated Then
            ' After the layout that shows the list: a list that has just become visible has no window
            ' to take the focus yet.
            BeginInvoke(New MethodInvoker(AddressOf GiveListTheKeyboard))
        End If

        UpdateToolbar()
        UpdateDetail()
        UpdateStrip()
        ' The Autostart dialog reads the window's state through delegates; a refresh here is a
        ' refresh there, so a switch that has just run shows the console's answer, not its hope.
        If autostartDlg IsNot Nothing AndAlso Not autostartDlg.IsDisposed Then autostartDlg.RefreshState()
    End Sub

    Private Sub GiveListTheKeyboard()
        If listFocusGiven OrElse IsDisposed OrElse Not list.Visible Then Return
        ' Only while nothing else was chosen: a user who has gone to the filter keeps it.
        If (ActiveControl Is Nothing OrElse ActiveControl Is filterBox) AndAlso filterBox.Text = "" Then
            If list.Focus() Then listFocusGiven = True
        Else
            listFocusGiven = True
        End If
    End Sub

    Private Function SelectedRecords() As List(Of DiskRecord)
        Dim out As New List(Of DiskRecord)
        If list Is Nothing Then Return out
        ' Item by item rather than SelectedIndices, which a list that has no window yet reports as
        ' empty whatever its items say.
        For i = 0 To Math.Min(list.Items.Count, shown_.Count) - 1
            If list.Items(i).Selected Then out.Add(shown_(i))
        Next
        Return out
    End Function

    Private Function StatesOf(rows As IList(Of DiskRecord)) As List(Of DiskRowState)
        Return rows.Select(Function(r) RowState(r)).ToList()
    End Function

    Private Function Context() As DiskContext
        If snapshot Is Nothing Then Return New DiskContext With {.Packaged = Packaging.IsPackaged()}
        Return New DiskContext With {
            .Packaged = snapshot.Packaged OrElse Packaging.IsPackaged(),
            .TransportReady = snapshot.TransportReady,
            .TransportReason = snapshot.TransportReason,
            .FMSAvailability = If(staleKey = "", snapshot.FMSAvailability, "unknown")
        }
    End Function

    ' "" when the action applies to the selection; otherwise the sentence of why not.
    Private Function WhyText(a As DiskAction, rows As List(Of DiskRecord)) As String
        Dim key = DiskStates.WhyNotAll(a, rows, StatesOf(rows), Context())
        If key = "" Then Return ""
        Return L(key)
    End Function

    Private Function UnmountActionFor(rows As List(Of DiskRecord)) As DiskAction
        If rows.Count > 0 AndAlso rows.All(Function(r) r.IsImage) Then Return DiskAction.UnmountImage
        Return DiskAction.Unmount
    End Function

    ' Set while the list is rebuilt: its selection events are one change, handled once after it.
    Private rebuilding As Boolean = False
    Private listFocusGiven As Boolean = False

    ' What the empty-list label says, "" while the list is shown. Kept apart from the controls'
    ' Visible, which a window not on screen reports as false for every child.
    Private emptyShown As String = ""

    Private Sub SelectionChanged()
        If rebuilding Then Return
        UpdateToolbar()
        UpdateDetail()
    End Sub

    Private Sub UpdateToolbar()
        Dim sel = SelectedRecords()
        SetOffered(mountBtn, DiskAction.Mount, sel, "vd_tip_mount")
        SetOffered(unmountBtn, UnmountActionFor(sel), sel, "vd_tip_unmount")
        SetOffered(openBtn, DiskAction.OpenDrive, sel, "vd_tip_open")
        SetOffered(saveBtn, DiskAction.SaveNow, sel, "vd_tip_save")
        SetOffered(newBtn, DiskAction.NewDisk, sel, "vd_tip_new")
        SetTip(addBtn, L("vd_mgr_btn_add") & " (" & DiskShortcuts.KeyText(Keys.Control Or Keys.O) & ")", L("vd_mgr_btn_add_tip"))
        SetTip(refreshBtn, L("vd_mgr_btn_refresh") & DiskShortcuts.Suffix(DiskAction.Refresh), L("vd_tip_refresh"))
        SetTip(moreBtn, L("vd_mgr_btn_more"), L("vd_mgr_btn_more_tip"))
        SetTip(settingsBtn, L("rail_job_settings"), L("settings_startup"))
        SetTip(helpBtn, L("vd_mgr_name_help") & " (" & DiskShortcuts.KeyText(Keys.F1) & ")", L("vd_tip_help"))
        SetTip(mainBtn, L("vd_mgr_btn_main") & " (" & DiskShortcuts.KeyText(Keys.Control Or Keys.Shift Or Keys.O) & ")", L("vd_tip_main"))
        SetTip(filterBox, L("vd_mgr_filter_name") & " (" & DiskShortcuts.KeyText(Keys.Control Or Keys.F) & ")", L("vd_tip_filter"))
        SetTip(filterClear, L("vd_mgr_name_clear") & " (" & DiskShortcuts.KeyText(Keys.Escape) & ")", L("vd_tip_clear_filter"))
        SetTip(detailClose, L("vd_mgr_detail_hide"), L("vd_tip_detail_close"))
        SetTip(detailShow, L("vd_mgr_detail_show"), L("vd_tip_detail_show"))
        SetTip(emptyNew, L("vd_mgr_act_new") & DiskShortcuts.Suffix(DiskAction.NewDisk), L("vd_tip_new"))
        SetTip(emptyAdd, L("vd_mgr_btn_add") & " (" & DiskShortcuts.KeyText(Keys.Control Or Keys.O) & ")", L("vd_mgr_btn_add_tip"))
        SetTip(emptyLearn, L("vd_mgr_learn_more"), Links.DiskGuide)
        SetTip(stopBtn, L("vd_mgr_btn_stop"), "")
        SetTip(clearQueueBtn, L("vd_mgr_btn_clear_queue"), "")
        ' A control this build can never run is hidden, not disabled (APP-BEHAVIOUR rule 11): the Store
        ' build cannot mount, so it shows no Mount, Unmount or Save.
        Dim ctx = Context()
        SetBuildVisibility(mountBtn, DiskStates.HiddenInBuild(DiskAction.Mount, ctx))
        SetBuildVisibility(unmountBtn, DiskStates.HiddenInBuild(DiskAction.Unmount, ctx))
        SetBuildVisibility(saveBtn, DiskStates.HiddenInBuild(DiskAction.SaveNow, ctx))
    End Sub

    ' A tooltip of two lines: what the control is called (with its key) and what it does.
    Private Sub SetTip(c As Control, title As String, body As String)
        tips.SetToolTip(c, If(String.IsNullOrEmpty(body), title, title & Environment.NewLine & body))
    End Sub

    ' A toolbar button: enabled when the action applies, and its reason kept for the tooltip either
    ' way (principle 2).
    Private Sub SetOffered(b As Button, a As DiskAction, sel As List(Of DiskRecord), tipKey As String)
        Dim why = WhyText(a, sel)
        b.Enabled = (why = "")
        Dim label = L(DiskStates.LabelKey(a, Nothing)) & DiskShortcuts.Suffix(a)
        Dim about = If(tipKey = "", "", L(tipKey))
        b.Tag = If(why = "", Nothing, label & Environment.NewLine & why)
        tips.SetToolTip(b, If(why = "", If(about = "", label, label & Environment.NewLine & about),
                              label & Environment.NewLine & why))
        b.AccessibleDescription = If(why = "", about, why)
    End Sub

    ' ---- the detail pane (spec 5.3) --------------------------------------

    ' Whether the detail pane is open, kept as a value: a control of a window that is not on screen
    ' reports Visible false whatever it was set to, and the toggle must not depend on that.
    Private detailOpenState As Boolean = True

    Private Sub SetDetailOpen(open As Boolean)
        detailOpenState = open
        detailPanel.Visible = open
        detailBar.Visible = Not open
        KeepListRoom()
    End Sub

    ' The window's scrolling floor: what every part that is not the list needs, plus the list's own
    ' least height. Equal to the client height or less, there is no scroll bar and nothing changes.
    ' The parts change height inside the root's own layout pass (a wrapped toolbar, a longer detail
    ' text), and the scroll floor they set resizes that same root: done there and then, the table is
    ' left laid out for the old height. So the floor is set once the pass is over.
    Private listRoomPending As Boolean = False

    Private Sub KeepListRoom()
        If toolbar Is Nothing OrElse strip Is Nothing OrElse detailPanel Is Nothing OrElse listHost Is Nothing Then Return
        If Not IsHandleCreated Then
            ApplyListRoom()
            Return
        End If
        If listRoomPending Then Return
        listRoomPending = True
        BeginInvoke(New MethodInvoker(Sub()
                                          listRoomPending = False
                                          If Not IsDisposed Then ApplyListRoom()
                                      End Sub))
    End Sub

    Private Sub ApplyListRoom()
        Dim need = Ui.Px(Me, ListMinHeight) + listHost.Padding.Vertical
        For Each c As Control In New Control() {toolbar, filterUnit, strip, If(detailOpenState, CType(detailPanel, Control), detailBar)}
            need += c.Height + c.Margin.Vertical
        Next
        If AutoScrollMinSize.Height = need Then Return
        AutoScrollMinSize = New Size(0, need)
        root.PerformLayout()
    End Sub

    ' The self-test's view: the height the list has been given.
    Friend ReadOnly Property ListHeightForTest As Integer
        Get
            Return list.Height
        End Get
    End Property

    ' Where the window's height goes, for a failing row's detail.
    Friend ReadOnly Property HeightReportForTest As String
        Get
            Return "client " & ClientSize.Height.ToString() & ", floor " & AutoScrollMinSize.Height.ToString() & ", toolbar " & toolbar.Height.ToString() &
                   ", filter " & filterUnit.Height.ToString() & ", detail " & If(detailOpenState, detailPanel.Height, detailBar.Height).ToString() &
                   ", strip " & strip.Height.ToString() & ", list " & list.Height.ToString() & ", root " & root.Height.ToString() &
                   ", rows " & String.Join("/", root.GetRowHeights().Select(Function(n) n.ToString()).ToArray())
        End Get
    End Property

    ' The detail pane's buttons as they stand, so a refresh that changes nothing rebuilds nothing -
    ' a button rebuilt every five seconds would take the focus away from the keyboard user on it.
    Private shownDetailActions As New List(Of DiskAction)

    Private Sub UpdateDetail()
        Dim sel = SelectedRecords()
        Dim lines As New List(Of String)

        Dim title As String
        If sel.Count = 0 Then
            title = L("vd_mgr_detail_none_title")
            lines.Add(L("vd_mgr_detail_none"))
        ElseIf sel.Count > 1 Then
            title = Localization.Format(L("vd_mgr_detail_many_fmt"), sel.Count)
            lines.Add(L("vd_mgr_detail_many"))
        Else
            Dim r = sel(0)
            Dim where = If(r.IsPartition, If(r.PartitionPlace <> "", r.PartitionPlace, L("vd_part_carrier_partition")), r.Path)
            title = DisplayName(r) & "  -  " & where
            lines.AddRange(DetailSentences(r))
        End If
        If detailTitle.Text <> title Then detailTitle.Text = title

        ' What the toolbar's row actions would do and why they are not offered, grouped by reason so
        ' a disk at rest says "not mounted" once rather than three times.
        If sel.Count > 0 Then
            Dim byReason As New Dictionary(Of String, List(Of String))
            Dim order As New List(Of String)
            For Each a In New DiskAction() {DiskAction.Mount, UnmountActionFor(sel), DiskAction.OpenDrive, DiskAction.SaveNow}
                Dim why = WhyText(a, sel)
                If why = "" Then Continue For
                If Not byReason.ContainsKey(why) Then
                    byReason(why) = New List(Of String)
                    order.Add(why)
                End If
                byReason(why).Add(L(DiskStates.LabelKey(a, Nothing)))
            Next
            For Each why In order
                lines.Add(Localization.Format(L("vd_mgr_detail_why_fmt"), String.Join(", ", byReason(why).ToArray()), why))
            Next
        End If
        Dim text = String.Join(Environment.NewLine, lines.ToArray())
        If detailText.Text <> text Then detailText.Text = text

        Dim wanted As New List(Of DiskAction)
        If sel.Count > 0 Then
            For Each a In DetailActions
                If Offered(a, sel) AndAlso WhyText(a, sel) = "" Then wanted.Add(a)
            Next
        End If
        If wanted.SequenceEqual(shownDetailActions) Then Return
        shownDetailActions = wanted

        detailButtons.SuspendLayout()
        For Each c As Control In detailButtons.Controls.Cast(Of Control)().ToList()
            detailButtons.Controls.Remove(c)
            tips.SetToolTip(c, Nothing)
            c.Dispose()
        Next
        For Each a In wanted
            Dim action = a
            Dim b = NewGlyphButton(DiskStates.LabelKey(a, Nothing), DiskGlyphs.For(a), 20)
            b.Danger = (DiskStates.KindOf(a) = DiskActionKind.Destructive)
            tips.SetToolTip(b, b.Text & DiskShortcuts.Suffix(a))
            AddHandler b.Click, Sub() Perform(action)
            detailButtons.Controls.Add(b)
        Next
        detailButtons.ResumeLayout(True)
    End Sub

    ' Whether an action belongs in a row's offers at all: an image has only its drive; auto-mount
    ' offers the switch it does not have; "add" is for a mount the list does not name.
    Private Function Offered(a As DiskAction, rows As List(Of DiskRecord)) As Boolean
        If rows.Count = 0 Then Return False
        Dim allImages = rows.All(Function(r) r.IsImage)
        Dim anyImage = rows.Any(Function(r) r.IsImage)
        Select Case a
            Case DiskAction.ShareDisk : Return rows.Count = 1 AndAlso Not rows(0).IsShared AndAlso Not rows(0).IsImage
            Case DiskAction.OpenShared, DiskAction.CloseShared, DiskAction.UnshareDisk : Return rows.Count = 1 AndAlso rows(0).IsShared
            Case DiskAction.ShareAutoOn : Return rows.Count = 1 AndAlso rows(0).IsShared AndAlso Not rows(0).Autostart
            Case DiskAction.ShareAutoOff : Return rows.Count = 1 AndAlso rows(0).IsShared AndAlso rows(0).Autostart
            Case DiskAction.ShowInFolder
                ' A partition disk has no file to show (SP-0148).
                Return Not rows.Any(Function(r) r.IsPartition)
            Case DiskAction.ImageToFile
                Return rows.Count = 1 AndAlso rows(0).IsPartition
            Case DiskAction.UnmountImage, DiskAction.OpenDrive, DiskAction.CopyPath
                Return a <> DiskAction.UnmountImage OrElse allImages
            Case DiskAction.AutoOn
                Return rows.Count = 1 AndAlso rows(0).Registered AndAlso Not rows(0).AutoMount
            Case DiskAction.AutoOff
                Return rows.Count = 1 AndAlso rows(0).Registered AndAlso rows(0).AutoMount
            Case DiskAction.Autostart
                ' The consolidated view of everything automatic (SP-0080 5): it opens from a
                ' registered row, and the packaged build never shows it (HiddenInBuild).
                Return rows.Count = 1 AndAlso rows(0).Registered AndAlso Not rows(0).IsImage
            Case DiskAction.AddToList
                Return rows.Count = 1 AndAlso Not rows(0).Registered AndAlso Not rows(0).IsImage
            Case DiskAction.Forget
                Return rows.All(Function(r) r.Registered)
            Case DiskAction.RepairVolume
                ' Only for the disk Windows would check by itself; every other row has nothing to repair.
                Return rows.Count = 1 AndAlso Not anyImage AndAlso RowState(rows(0)) = DiskRowState.Unclean
        End Select
        Return Not anyImage
    End Function

    ' The selected row in sentences (spec 5.3): the protection in the G3-swept words, the mount and
    ' its server in words, a ram buffer's unsaved amount, the clean marker, auto-mount.
    Private Function DetailSentences(r As DiskRecord) As List(Of String)
        Dim out As New List(Of String)
        If r.IsImage Then
            out.Add(L("vd_mgr_detail_image"))
            If r.IsMounted Then out.Add(Localization.Format(L("vd_mgr_detail_mounted_fmt"), r.Letter, WhenText(r.MountedAt)))
            Return out
        End If
        If Not r.Registered Then out.Add(L("vd_mgr_detail_unregistered"))
        ' SP-0148: a partition disk says where it is, by what it is addressed, and the facts of 4.7
        ' that touch its use.
        If r.IsPartition Then
            If r.PartitionDetail <> "" Then out.Add(r.PartitionDetail)
            If r.Locator <> "" Then out.Add(Localization.Format(L("vd_part_detail_locator_fmt"), r.Locator))
            out.Add(LText("vd_part_detail_consent"))
            out.Add(LText("vd_part_detail_refusals"))
        End If
        Select Case r.FileState
            Case "missing" : out.Add(L(If(r.IsPartition, "vd_part_detail_missing", "vd_mgr_detail_missing")))
            Case "different" : out.Add(L(If(r.IsPartition, "vd_part_detail_different", "vd_mgr_detail_different")))
            Case "unreadable" : out.Add(Localization.Format(L("vd_mgr_detail_unreadable_fmt"), r.FileError))
        End Select
        Select Case r.Protection
            Case DiskProtection.Obfuscated : out.Add(L("vd_facts_obfuscated"))
            Case DiskProtection.Encrypted
                out.Add(L("vd_facts_encrypted"))
                If r.IsMounted Then out.Add(L("vd_mgr_detail_open_while_mounted"))
        End Select
        If r.IsMounted Then
            out.Add(Localization.Format(L(If(r.ReadOnly, "vd_mgr_detail_mounted_ro_fmt", "vd_mgr_detail_mounted_fmt")), r.Letter, WhenText(r.MountedAt)))
            out.Add(If(r.ServerAlive, L("vd_mgr_detail_server_alive"), Localization.Format(L("vd_mgr_detail_server_gone_fmt"), r.Letter)))
            If r.HasRam AndAlso r.ServerAlive Then
                out.Add(Localization.Format(L("vd_mgr_detail_ram_fmt"), DiskStates.SizeText(r.RamDirty), WhenText(r.RamLastGoodSave)))
                If r.RamSaving Then out.Add(L("vd_mgr_detail_ram_saving"))
                ' AUD-35-F5: the loss is visible before the unmount, not after.
                If r.RamSaveError <> "" Then out.Add(Localization.Format(L("vd_mgr_detail_save_failing_fmt"), r.RamSaveError))
            End If
        ElseIf r.FileState = "ok" Then
            If r.Clean.HasValue Then out.Add(L(If(r.Clean.Value, "vd_mgr_detail_clean_yes", "vd_mgr_detail_clean_no")))
            out.Add(Localization.Format(L("vd_mgr_detail_last_save_fmt"), WhenText(r.LastGoodSave)))
        End If
        If r.Registered Then out.Add(L(If(r.AutoMount, "vd_mgr_detail_auto_on", "vd_mgr_detail_auto_off")))
        out.AddRange(DiskStates.SharedSentences(r, dict))
        If r.IsShared AndAlso snapshot IsNot Nothing AndAlso snapshot.FMSAvailability = "ready" AndAlso
            (snapshot.FMSMode = "session" OrElse snapshot.FMSMode = "service") Then out.Add(L("vd_share_host_" & snapshot.FMSMode))
        Dim info As String = Nothing
        If infoByKey.TryGetValue(r.Key, info) AndAlso info <> "" Then
            out.Add(L("vd_mgr_detail_info_title"))
            out.Add(info)
        End If
        Return out
    End Function

    Private Function WhenText(t As DateTimeOffset?) As String
        If Not t.HasValue Then Return L("vd_mgr_detail_never")
        Return t.Value.LocalDateTime.ToString("g")
    End Function

    ' ---- the strip -------------------------------------------------------

    Private Sub UpdateStrip()
        If summaryLabel Is Nothing Then Return
        If snapshot Is Nothing Then
            summaryLabel.Text = If(staleKey = "", L("vd_mgr_reading"), Localization.Format(L("vd_mgr_never_read_fmt"), L(staleKey)))
        Else
            Dim transport As String
            If snapshot.Packaged Then
                transport = L("vd_mgr_transport_packaged")
            ElseIf DiskStates.TransportWhy(Context()) <> "" Then
                transport = L("vd_mgr_transport_not_ready")
            Else
                transport = L("vd_mgr_transport_ready")
            End If
            Dim summary = Localization.Format(L("vd_mgr_status_fmt"), snapshot.MountedCount, transport, AgoText())
            ' The shutdown guard gets one word while it is on - the state in words, never colour
            ' alone (SP-0080 5); the Autostart dialog is where its switch and its report live.
            If snapshot.Guard IsNot Nothing AndAlso snapshot.Guard.Installed Then
                summary &= "  " & L(If(snapshot.Guard.Running, "vd_mgr_strip_guard_running", "vd_mgr_strip_guard_stale"))
            End If
            If staleKey <> "" Then
                summary = Localization.Format(L("vd_mgr_stale_fmt"), lastGoodAt.ToString("t"), L(staleKey)) & Environment.NewLine & summary
            End If
            summaryLabel.Text = summary
        End If

        Dim current = running.FirstOrDefault()
        If current IsNot Nothing Then
            opLabel.Text = Localization.Format(L("vd_mgr_running_fmt"), L(DiskStates.BusyKey(DiskStates.VerbOf(current.Action))), OpName(current))
            If queue.Count > 0 Then opLabel.Text &= "  " & Localization.Format(L("vd_mgr_queue_fmt"), queue.Count)
        Else
            opLabel.Text = lastOutcome
        End If
        opLabel.Visible = (opLabel.Text <> "")
        Dim stoppable = running.Any(Function(o) o.Action = DiskAction.Verify)
        stopBtn.Visible = stoppable
        opBar.Visible = stoppable
        clearQueueBtn.Visible = queue.Count > 0
        reportLink.Visible = (current Is Nothing AndAlso lastOutcomeWarns AndAlso lastReport <> "")
        ApplyStripTheme()
    End Sub

    Private Function AgoText() As String
        Dim secs = CInt(Math.Max(0, (DateTime.Now - lastGoodAt).TotalSeconds))
        If secs < 2 Then Return L("vd_mgr_updated_now")
        If secs < 120 Then Return Localization.Format(L("vd_mgr_updated_secs_fmt"), secs)
        Return Localization.Format(L("vd_mgr_updated_at_fmt"), lastGoodAt.ToString("t"))
    End Function

    Private Function OpName(op As DiskOp) As String
        Dim r = op.Record
        Dim name = If(r.IsImage, IO.Path.GetFileName(r.Path), DisplayName(r))
        Return If(r.Letter <> "", name & " (" & r.Letter & ")", name)
    End Function

    Private Sub OpenLastReport()
        If lastReport = "" Then Return
        Try
            Process.Start("notepad.exe", """" & lastReport & """")
        Catch ex As Exception
            ShellLog.Write("open the run report", ex)
            Ui.OpenFolder(Me, IO.Path.GetDirectoryName(lastReport))
        End Try
    End Sub

    ' ---- reading the state (spec 8.2) --------------------------------------

    Private Class ReadResult
        Public Snapshot As DiskSnapshot
        Public Problem As String
    End Class

    Private Shared Function ReadOnce() As ReadResult
        Dim problem As String = ""
        Dim s = DiskStateProbe.Read(problem)
        Return New ReadResult With {.Snapshot = s, .Problem = problem}
    End Function

    ' Coalesced: at most one snapshot child at a time; a request while one runs reads once more
    ' after it, however many requests came in meanwhile.
    Friend Async Sub RequestRead()
        If IsDisposed OrElse SuppressReads Then Return
        If reading Then
            readAgain = True
            Return
        End If
        reading = True
        Try
            Do
                readAgain = False
                Dim res = Await Task.Run(AddressOf ReadOnce)
                If IsDisposed Then Return
                ApplyRead(res.Snapshot, res.Problem)
            Loop While readAgain AndAlso Not IsDisposed
        Catch ex As Exception
            ShellLog.Write("refresh the disk list", ex)
        Finally
            reading = False
        End Try
    End Sub

    Private Sub ApplyRead(s As DiskSnapshot, problem As String)
        If s IsNot Nothing Then
            snapshot = s
            ' SP-0148: each partition row's disk and extent, from the last disk list; the first row of
            ' a partition disk asks for one.
            If s.Disks.Any(Function(d) d.IsPartition) Then
                PartitionCommands.Join(disksDoc, s.Disks, dict)
                If disksDoc Is Nothing AndAlso Not disksTried Then RequestDisksRead()
            End If
            lastGoodAt = DateTime.Now
            staleKey = ""
            postJobReads = 0
            If watcher Is Nothing AndAlso Visible Then StartWatcher()
        Else
            ' The last good state stays on screen, marked as stale (principle 6).
            staleKey = If(problem = "", "vd_mgr_stale_failed", problem)
            If postJobReads > 0 Then
                postJobReads -= 1
                ScheduleRead()
            End If
        End If
        RefreshView()
    End Sub

    ' A change seen by the watcher or the device broadcast is let settle for a moment and then read:
    ' a mount writes the state file, the registry and a status file within a second.
    Private Sub ScheduleRead()
        If settleTimer Is Nothing OrElse IsDisposed Then Return
        settleTimer.Stop()
        settleTimer.Start()
    End Sub

    ' The state root as filedo.exe finds it (statedir.Dir): FILEDO_STATE_DIR, or
    ' %LOCALAPPDATA%\FileDO\state. The window watches the folder and never reads a file in it.
    Friend Shared Function StateRoot() As String
        Dim over = Environment.GetEnvironmentVariable("FILEDO_STATE_DIR")
        If Not String.IsNullOrEmpty(over) Then Return over
        Return IO.Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "FileDO", "state")
    End Function

    ' The files whose change means a disk's state may have changed.
    Friend Shared Function IsWatchedName(name As String) As Boolean
        Dim n = If(name, "").ToLowerInvariant()
        Return n = "vdisk-state.json" OrElse n = "vd-registry.json" OrElse
               (n.StartsWith("vd-", StringComparison.Ordinal) AndAlso n.EndsWith(".status.json", StringComparison.Ordinal))
    End Function

    Private Sub StartWatcher()
        If watcher IsNot Nothing Then Return
        Try
            Dim dir = StateRoot()
            If Not Directory.Exists(dir) Then Return
            watcher = New FileSystemWatcher(dir, "*.json") With {
                .NotifyFilter = NotifyFilters.FileName Or NotifyFilters.LastWrite,
                .IncludeSubdirectories = False,
                .SynchronizingObject = Me
            }
            Dim onChange As FileSystemEventHandler = Sub(s, e)
                                                         If IsWatchedName(e.Name) Then ScheduleRead()
                                                     End Sub
            AddHandler watcher.Changed, onChange
            AddHandler watcher.Created, onChange
            AddHandler watcher.Deleted, onChange
            AddHandler watcher.Renamed, Sub(s, e)
                                            If IsWatchedName(e.Name) OrElse IsWatchedName(e.OldName) Then ScheduleRead()
                                        End Sub
            ' A watcher that overflows or dies has missed changes: read now and rely on the clock.
            AddHandler watcher.Error, Sub(s, e) ScheduleRead()
            watcher.EnableRaisingEvents = True
        Catch ex As Exception
            ShellLog.Write("watch the disk state folder", ex)
            If watcher IsNot Nothing Then watcher.Dispose()
            watcher = Nothing
        End Try
    End Sub

    Protected Overrides Sub WndProc(ByRef m As Message)
        MyBase.WndProc(m)
        Select Case m.Msg
            Case WM_DEVICECHANGE
                ' A volume arrived or went (a disk ejected in Explorer, a letter gone).
                Dim ev = m.WParam.ToInt64()
                If ev = DBT_DEVICEARRIVAL OrElse ev = DBT_DEVICEREMOVECOMPLETE Then ScheduleRead()
            Case WM_SETTINGCHANGE
                If ShellSettings.ThemeChoice() = "auto" Then
                    Dim wasDark = Theme.Current.IsDark
                    Theme.Refresh()
                    If Theme.Current.IsDark <> wasDark Then ApplyTheme()
                End If
        End Select
    End Sub

    ' ---- actions ---------------------------------------------------------

    ' One entry for every gesture: the toolbar, the menu, the detail pane, a key, a double-click.
    Friend Sub Perform(a As DiskAction)
        Dim sel = SelectedRecords()
        If DiskStates.WhyNotAll(a, sel, StatesOf(sel), Context()) <> "" Then Return
        If DiskStates.IsSharing(a) Then
            PrepareSharing(a, sel(0))
            Return
        End If
        ' SP-0148: a new disk is first a choice of where it lives; a partition disk is deleted by its
        ' typed name here (its job page works on files); a found partition is adopted here.
        Select Case a
            Case DiskAction.NewDisk
                ChooseNewDisk()
                Return
            Case DiskAction.Adopt
                OpenAdopt()
                Return
            Case DiskAction.Destroy
                If sel.Count = 1 AndAlso sel(0).IsPartition Then
                    DestroyPartition(sel(0))
                    Return
                End If
        End Select
        Select Case DiskStates.KindOf(a)
            Case DiskActionKind.Local
                DoLocal(a, sel)
            Case DiskActionKind.Delegated, DiskActionKind.Destructive
                ' Spec 7.3: the job page, with the container chosen; its plan card, its consequences
                ' and its typed word stay where they are.
                RaiseEvent JobRequested(DiskStates.JobKeyOf(a), If(sel.Count > 0 AndAlso a <> DiskAction.NewDisk, sel(0).Path, ""), "")
            Case Else
                If a = DiskAction.MountImage Then
                    BrowseAndMountImage()
                Else
                    DoQuick(a, sel)
                End If
        End Select
    End Sub

    Private Sub DoLocal(a As DiskAction, sel As List(Of DiskRecord))
        Select Case a
            Case DiskAction.Refresh
                RequestRead()
            Case DiskAction.Autostart
                OpenAutostart()
            Case DiskAction.OpenDrive
                For Each r In sel
                    Ui.OpenFolder(Me, r.Letter & "\")
                Next
            Case DiskAction.CopyPath
                ' A partition disk has no path: its locator is what addresses it.
                Ui.CopyText(Me, String.Join(Environment.NewLine, sel.Select(Function(r) If(r.IsPartition, r.Locator, r.Path)).ToArray()))
            Case DiskAction.ShowInFolder
                Dim r = sel(0)
                Try
                    Process.Start("explorer.exe", "/select,""" & r.Path & """")
                Catch ex As Exception
                    ShellLog.Write("show the container in its folder", ex)
                    If ShellDialog.Problem(Me, Localization.Format(L("shell_folder_open_failed"), r.Path, Problems.Cause(ex)),
                                           L("shell_btn_copy_path")) Then
                        Ui.CopyText(Me, r.Path)
                    End If
                End Try
        End Select
    End Sub

    ' A quick action (spec 7.1): its question first when it has one, the password when the
    ' container is encrypted, then one run per disk.
    Private Sub DoQuick(a As DiskAction, sel As List(Of DiskRecord))
        If sel.Count = 0 Then Return

        ' D5: several disks that each need consent - each asks once, and the window says so first.
        If sel.Count > 1 AndAlso DiskStates.Elevates(a) Then
            If Ask("vd_mgr_confirm_multi_title", Localization.Format(LText("vd_mgr_confirm_multi_fmt"), sel.Count),
                   "vd_mgr_btn_continue") <> 0 Then Return
        End If

        Dim o As New DiskOptions()
        Select Case a
            Case DiskAction.MountAs
                Using dlg As New DiskMountAsDialog(dict, DisplayName(sel(0)), Me)
                    If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
                    o.Letter = dlg.Letter
                    o.ReadOnly = dlg.MountReadOnly
                End Using
            Case DiskAction.AutoOn
                If Ask("vd_mgr_confirm_auto_title", Localization.Format(LText("vd_mgr_confirm_auto_on_fmt"), DisplayName(sel(0))),
                       "vd_mgr_btn_turn_on") <> 0 Then Return
            Case DiskAction.AutoOff
                If Ask("vd_mgr_confirm_auto_title", Localization.Format(LText("vd_mgr_confirm_auto_off_fmt"), DisplayName(sel(0))),
                       "vd_mgr_btn_turn_off") <> 0 Then Return
            Case DiskAction.RepairVolume
                ' chkdsk /f writes to the volume, so its question is asked before anything starts.
                If ShellDialog.Ask(Me, DestructiveDialogs.RepairVolume(dict, DisplayName(sel(0)))) <> 0 Then Return
            Case DiskAction.Forget
                Dim names = String.Join(", ", sel.Select(Function(r) r.Name).ToArray())
                If ShellDialog.Ask(Me, DestructiveDialogs.ForgetDisks(dict, names)) <> 0 Then Return
            Case DiskAction.AddToList
                Dim name = AskName(sel(0).Path, sel(0).BaseName, force:=True)
                If name = "" Then Return
                o.Name = name
            Case DiskAction.ImageToFile
                Dim dest = AskImageFile(sel(0))
                If dest = "" Then Return
                o.Dest = dest
        End Select

        For Each r In sel
            Dim state = RowState(r)
            Dim ro As New DiskOptions With {.Letter = o.Letter, .ReadOnly = o.ReadOnly, .Name = o.Name, .Dest = o.Dest}
            ' A ram disk with unsaved data says so before it is unmounted (spec 6.1); the unmount
            ' saves it first, which is why it can take a while.
            If a = DiskAction.Unmount AndAlso state = DiskRowState.Unsaved Then
                If ShellDialog.Ask(Me, DestructiveDialogs.UnmountDirtyRam(dict, DisplayName(r), DiskStates.SizeText(r.RamDirty))) <> 0 Then Continue For
            End If
            ' A disk not closed cleanly is mounted knowingly: Windows checks it as after a power loss.
            If (a = DiskAction.Mount OrElse a = DiskAction.MountReadOnly OrElse a = DiskAction.MountAs) AndAlso
               state = DiskRowState.Unclean AndAlso Not ro.ReadOnly AndAlso a <> DiskAction.MountReadOnly Then
                If ShellDialog.Ask(Me, DestructiveDialogs.MountUnclean(dict, DisplayName(r))) <> 0 Then Continue For
            End If
            Dim env As Dictionary(Of String, String) = Nothing
            If DiskStates.AsksPassword(a, r) Then
                Using dlg As New DiskPasswordDialog(dict, DisplayName(r), Me)
                    If dlg.ShowDialog(Me) <> DialogResult.OK Then Continue For
                    env = New Dictionary(Of String, String) From {{DiskCommands.CredentialEnvName, dlg.TakePassword()}}
                End Using
                ro.HasCredential = True
            End If
            Dim args = DiskStates.QuickCommand(a, r, ro)
            If args Is Nothing Then Continue For
            Enqueue(New DiskOp With {.Action = a, .Record = r, .Args = args, .Env = env})
        Next
    End Sub

    ' A plain question whose go answer is the default. A confirmation of a destructive action is not
    ' asked here: it is a DestructiveDialogs spec, whose safe answer is the default.
    Private Function Ask(titleKey As String, text As String, goKey As String) As Integer
        Return ShellDialog.Ask(Me, L(titleKey), text, New String() {L(goKey), L("shell_btn_cancel")}, 1, 0)
    End Function

    ' The name a container is added under: the file's name when it is a usable one and free, else
    ' what the user types (spec 6.2: a name dialog when the name is taken). force asks either way.
    Private Function AskName(path As String, suggested As String, force As Boolean) As String
        Dim taken As New HashSet(Of String)(StringComparer.OrdinalIgnoreCase)
        If snapshot IsNot Nothing Then
            For Each r In snapshot.Disks
                If r.Registered AndAlso r.Name <> "" Then taken.Add(r.Name)
            Next
        End If
        Dim proposal = DiskNameDialog.Suggest(suggested, taken)
        If Not force AndAlso String.Equals(proposal, suggested, StringComparison.Ordinal) Then Return proposal
        Using dlg As New DiskNameDialog(dict, path, proposal, taken, Me)
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return ""
            Return dlg.ChosenName
        End Using
    End Function

    Private Sub BrowseAndAdd()
        Using dlg As New OpenFileDialog()
            dlg.Title = L("vd_mgr_btn_add")
            dlg.Filter = DiskCommands.FileFilter(L("vd_filter_fdd"), "*.fdd", L("vd_filter_all"))
            dlg.Multiselect = True
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
            AddFiles(dlg.FileNames)
        End Using
    End Sub

    Private Sub BrowseAndMountImage()
        If Context().Packaged Then Return
        Using dlg As New OpenFileDialog()
            dlg.Title = L("vd_mgr_act_mount_image")
            dlg.Filter = DiskCommands.FileFilter(L("vd_mgr_filter_images"), "*.vhd;*.vhdx;*.iso", L("vd_filter_all"))
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
            MountImageFile(dlg.FileName, confirm:=False)
        End Using
    End Sub

    Private Sub MountImageFile(path As String, confirm As Boolean)
        If Context().Packaged Then
            ShellDialog.Notice(Me, L("vd_mgr_act_mount_image"), L("vd_packaged"))
            Return
        End If
        If confirm AndAlso Ask("vd_mgr_act_mount_image", Localization.Format(LText("vd_mgr_confirm_mount_image_fmt"), IO.Path.GetFileName(path)),
                               "vd_mgr_btn_mount") <> 0 Then Return
        Dim r As New DiskRecord With {.Kind = "image", .Path = path}
        Enqueue(New DiskOp With {.Action = DiskAction.MountImage, .Record = r, .Args = DiskStates.QuickCommand(DiskAction.MountImage, r, Nothing)})
    End Sub

    ' ---- partition disks (SP-0148 section 10) -------------------------------------

    ' The last disk list `vd disks json` gave: it names each partition row's disk and extent, and it
    ' is what the new-partition and adopt dialogs offer. Read when a partition row first appears,
    ' when a dialog opens, and after an operation that changes a disk's layout.
    Private disksDoc As VdDisksDoc = Nothing
    Private disksReading As Boolean = False
    Private disksTried As Boolean = False

    Private Sub OpenNewFileJob()
        RaiseEvent JobRequested(DiskStates.JobKeyOf(DiskAction.NewDisk), "", "")
    End Sub

    ' The first choice of a new disk: a file on a drive (the New page, as before) or a partition in
    ' free disk space. The Store build has no partition disks and says so instead of offering them.
    Friend Function NewDiskChoice(ByRef title As String, ByRef text As String) As String()
        title = L("vd_part_new_choice_title")
        If Context().Packaged Then
            text = LText("vd_part_new_choice_store_text")
            Return New String() {L("vd_part_btn_file"), L("shell_btn_cancel")}
        End If
        text = LText("vd_part_new_choice_text")
        Return New String() {L("vd_part_btn_file"), L("vd_part_btn_partition"), L("shell_btn_cancel")}
    End Function

    Private Sub ChooseNewDisk()
        Dim title = "", text = ""
        Dim choices = NewDiskChoice(title, text)
        Dim pick = ShellDialog.Ask(Me, title, text, choices, choices.Length - 1, 0)
        If pick = 0 Then
            OpenNewFileJob()
        ElseIf pick = 1 AndAlso choices.Length = 3 Then
            OpenNewPartition()
        End If
    End Sub

    Private Shared Function ReadDisksOnce() As VdDisksDoc
        Dim problem As String = ""
        Return PartitionCommands.Read(problem)
    End Function

    ' One read of the disk list, kept and joined to the rows when it succeeds.
    Private Async Function ReadDisksAsync() As Task(Of VdDisksDoc)
        Dim doc = Await Task.Run(AddressOf ReadDisksOnce)
        If doc IsNot Nothing AndAlso Not IsDisposed Then SetDisks(doc)
        Return doc
    End Function

    Private Sub SetDisks(doc As VdDisksDoc)
        disksDoc = doc
        If snapshot IsNot Nothing Then PartitionCommands.Join(disksDoc, snapshot.Disks, dict)
        RefreshView()
    End Sub

    ' A read in the background, at most one at a time, for the rows' places.
    Private Async Sub RequestDisksRead()
        If disksReading OrElse IsDisposed OrElse SuppressReads OrElse Not DiskProbe.Enabled OrElse Context().Packaged Then Return
        disksReading = True
        disksTried = True
        Try
            Await ReadDisksAsync()
        Catch ex As Exception
            ShellLog.Write("list the disks", ex)
        Finally
            disksReading = False
        End Try
    End Sub

    ' The names the list holds already: a new name is never one of them.
    Private Function TakenNames() As HashSet(Of String)
        Dim taken As New HashSet(Of String)(StringComparer.OrdinalIgnoreCase)
        If snapshot IsNot Nothing Then
            For Each r In snapshot.Disks
                If r.Registered AndAlso r.Name <> "" Then taken.Add(r.Name)
            Next
        End If
        Return taken
    End Function

    Private Async Sub OpenNewPartition()
        If Context().Packaged Then Return
        UseWaitCursor = True
        Dim doc As VdDisksDoc
        Try
            doc = Await ReadDisksAsync()
        Finally
            UseWaitCursor = False
        End Try
        If IsDisposed Then Return
        If doc Is Nothing Then
            ShellDialog.Notice(Me, L("vd_part_title"), L("vd_part_read_failed"))
            Return
        End If
        If Not doc.Available Then
            ShellDialog.Notice(Me, L("vd_part_title"), L("vd_part_store"))
            Return
        End If
        Using dlg As New DiskNewPartitionDialog(dict, doc, TakenNames(), Me)
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
            Dim args = dlg.CommandLine()
            If args Is Nothing Then Return
            Dim env As Dictionary(Of String, String) = Nothing
            If dlg.HasCredential Then env = New Dictionary(Of String, String) From {{DiskCommands.CredentialEnvName, dlg.TakePassword()}}
            Dim r As New DiskRecord With {.Name = dlg.ChosenName, .Carrier = "partition", .Locator = "new:" & dlg.ChosenName}
            Enqueue(New DiskOp With {.Action = DiskAction.NewDisk, .Record = r, .Args = args, .Env = env, .ChangesDisks = True})
        End Using
    End Sub

    Private Async Sub OpenAdopt()
        If Context().Packaged Then Return
        UseWaitCursor = True
        Dim doc As VdDisksDoc
        Try
            doc = Await ReadDisksAsync()
        Finally
            UseWaitCursor = False
        End Try
        If IsDisposed Then Return
        If doc Is Nothing Then
            ShellDialog.Notice(Me, L("vd_part_adopt_title"), L("vd_part_read_failed"))
            Return
        End If
        Dim found = doc.Unregistered()
        If found.Count = 0 Then
            ShellDialog.Notice(Me, L("vd_part_adopt_title"), L("vd_part_adopt_none"))
            Return
        End If
        Using dlg As New DiskAdoptDialog(dict, found, TakenNames(), Me)
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
            Dim r As New DiskRecord With {.Name = dlg.ChosenName, .Carrier = "partition", .Locator = "fdpart:{" & dlg.ChosenGuid & "}"}
            Enqueue(New DiskOp With {.Action = DiskAction.Adopt, .Record = r, .Args = PartitionCommands.Adopt(dlg.ChosenGuid, dlg.ChosenName),
                                     .ChangesDisks = True})
        End Using
    End Sub

    ' Delete (spec 4.5): the disk's name typed in the window's own dialog, Cancel the default; the
    ' console re-proves the partition's type and container id before it deletes anything.
    Private Sub DestroyPartition(r As DiskRecord)
        Using dlg As New DiskDestroyPartitionDialog(dict, r.Target, r.PartitionPlace, Me)
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
            Enqueue(New DiskOp With {.Action = DiskAction.Destroy, .Record = r, .Args = PartitionCommands.Destroy(r.Target, dlg.Wipe),
                                     .ChangesDisks = True})
        End Using
    End Sub

    ' Image to file (spec 4.4): a new .fdd - never an existing one, which the console refuses too.
    Private Function AskImageFile(r As DiskRecord) As String
        Using dlg As New SaveFileDialog()
            dlg.Title = L("vd_mgr_act_image")
            dlg.Filter = DiskCommands.FileFilter(L("vd_filter_fdd"), "*.fdd", L("vd_filter_all"))
            dlg.DefaultExt = "fdd"
            dlg.AddExtension = True
            dlg.OverwritePrompt = False
            dlg.FileName = r.BaseName & ".fdd"
            If dlg.ShowDialog(Me) <> DialogResult.OK Then Return ""
            If IO.File.Exists(dlg.FileName) OrElse IO.Directory.Exists(dlg.FileName) Then
                ShellDialog.Notice(Me, L("vd_mgr_act_image"), L("vd_part_image_exists"))
                Return ""
            End If
            Return dlg.FileName
        End Using
    End Function

    ' ---- Autostart (SP-0080 5) ------------------------------------------------

    ' The open Autostart dialog, so a refresh of the window's own state is the dialog's too.
    Private autostartDlg As DiskAutostartDialog

    ' The consolidated view of everything automatic (SP-0080 5): the per-container logon mounts and
    ' the shutdown guard. The dialog reads this window's snapshot through delegates and acts through
    ' this window's own flows - DoQuick for a logon switch exactly as the row runs it, RunGuardSwitch
    ' for the guard - so there is no second way to write anything.
    Private Sub OpenAutostart()
        If Context().Packaged Then Return ' HiddenInBuild keeps the entry out of every menu already.
        If autostartDlg IsNot Nothing Then
            autostartDlg.RefreshState()
            autostartDlg.Activate()
            Return
        End If
        Dim dlg As New DiskAutostartDialog(dict, Me,
                                           AddressOf RegisteredRecordsForAutostart,
                                           AddressOf GuardForAutostart,
                                           AddressOf DoQuick,
                                           AddressOf RunGuardSwitch)
        AddHandler dlg.Disposed, Sub() autostartDlg = Nothing
        autostartDlg = dlg
        ' A form shown with ShowDialog is hidden, not disposed, when it closes: without this the held
        ' dialog answers the next open with "already open" and shows nothing (AUD-88-F2).
        Try
            dlg.ShowDialog(Me)
        Finally
            autostartDlg = Nothing
            dlg.Dispose()
        End Try
    End Sub

    Private Function RegisteredRecordsForAutostart() As List(Of DiskRecord)
        If snapshot Is Nothing Then Return New List(Of DiskRecord)
        Return snapshot.Disks.Where(Function(d) d.Registered AndAlso Not d.IsImage).ToList()
    End Function

    Private Function GuardForAutostart() As DiskGuardState
        Return If(snapshot Is Nothing, Nothing, snapshot.Guard)
    End Function

    ' The guard's switch: one command line, built where every line is built, run through the same
    ' queue as a row's actions - with its run report, its history line and its verdict.
    Private Sub RunGuardSwitch(turnOn As Boolean)
        Enqueue(New DiskOp With {.Action = DiskAction.Autostart, .Record = GuardRecord(),
                                 .Args = DiskCommands.Build("guard", "", New DiskOptions With {.GuardOn = turnOn})})
    End Sub

    ' A stand-in row for the guard's own operations: nothing in the list is busy when the guard
    ' switches, and the strip says what runs by name.
    Private Function GuardRecord() As DiskRecord
        Return New DiskRecord With {.Name = L("vd_auto_name_guard")}
    End Function

    ' Containers dropped or chosen: one already in the list is selected, every other one is added
    ' under its file's name - asked for only when that name is taken or not usable.
    Private Sub AddFiles(paths As IEnumerable(Of String))
        For Each p In paths
            If Not DiskCommands.IsFddPath(p) Then Continue For
            Dim known = If(snapshot Is Nothing, Nothing,
                           snapshot.Disks.FirstOrDefault(Function(d) d.Registered AndAlso String.Equals(d.Path, p, StringComparison.OrdinalIgnoreCase)))
            If known IsNot Nothing Then
                SelectKey(known.Key)
                Continue For
            End If
            Dim baseName = IO.Path.GetFileNameWithoutExtension(p)
            Dim name = AskName(p, baseName, force:=False)
            If name = "" Then Continue For
            Dim r As New DiskRecord With {.Path = p, .Name = name}
            Enqueue(New DiskOp With {.Action = DiskAction.AddToList, .Record = r,
                                     .Args = DiskCommands.Build("add", p, New DiskOptions With {.Remember = True, .Name = name})})
        Next
    End Sub

    Private Sub SelectKey(key As String)
        For Each it As ListViewItem In list.Items
            it.Selected = String.Equals(TryCast(it.Tag, String), key, StringComparison.Ordinal)
            If it.Selected Then
                it.Focused = True
                it.EnsureVisible()
            End If
        Next
    End Sub

    ' ---- running (spec 7.1, D4) --------------------------------------------

    Private Sub Enqueue(op As DiskOp)
        busy(op.Record.Key) = "queued"
        queue.Add(op)
        Pump()
        RefreshView()
    End Sub

    ' One operation that changes something at a time, in the order asked; reads beside it (D4).
    Private Sub Pump()
        Dim serialRunning = running.Any(Function(o) DiskStates.Serial(o.Action))
        For Each op In queue.ToArray()
            If DiskStates.Serial(op.Action) Then
                If serialRunning Then Continue For
                serialRunning = True
            End If
            queue.Remove(op)
            StartOp(op)
        Next
    End Sub

    Private Async Sub StartOp(op As DiskOp)
        running.Add(op)
        busy(op.Record.Key) = DiskStates.VerbOf(op.Action)
        op.Run = New Runner()
        If op.Action = DiskAction.Verify Then
            RunProgress.Begin(opBar)
            AddHandler op.Run.ProgressReported, Sub(p)
                                                    Try
                                                        BeginInvoke(New Action(Sub() RunProgress.Apply(opBar, p)))
                                                    Catch
                                                    End Try
                                                End Sub
        End If
        RefreshView()

        Dim res As Runner.RunResult = Nothing
        Try
            If op.SharingSteps Is Nothing Then
                res = Await op.Run.ExecuteAsync(op.Args, envVars:=op.Env)
            Else
                res = Await ExecuteSharing(op)
            End If
        Catch ex As Exception
            ShellLog.Write("run a disk operation", ex)
        Finally
            ' The password lived in the child's environment for this run and goes with it.
            If op.Env IsNot Nothing Then op.Env.Clear()
            op.Env = Nothing
        End Try

        running.Remove(op)
        If AppHost.Current IsNot Nothing Then AppHost.Current.NotifyFinished()
        busy.Remove(op.Record.Key)
        ShowOutcome(op, res)
        If op.SharingSteps IsNot Nothing Then
            lastOutcome &= " " & Localization.Format(L("vd_share_progress_fmt"),
                If(op.CompletedSharing.Count = 0, L("vd_share_none"), String.Join(", ", op.CompletedSharing.ToArray())),
                If(op.SharingFailure = "", L("vd_share_finished"), L(op.SharingFailure)))
            RefreshView()
        End If
        op.Run.Dispose()
        If IsDisposed Then Return
        Pump()
        postJobReads = 3
        RequestRead()
        If op.ChangesDisks Then RequestDisksRead()
        If closeWhenIdle AndAlso running.Count = 0 AndAlso queue.Count = 0 Then
            BeginInvoke(New MethodInvoker(AddressOf Close))
        End If
    End Sub

    ' The outcome on the strip: done, or the sentence of what went wrong with the run report one
    ' click away. A declined consent prompt is a normal outcome, said as one (spec 7.1).
    Private Sub ShowOutcome(op As DiskOp, res As Runner.RunResult)
        lastReport = If(op.Run IsNot Nothing, op.Run.LastReportPath, "")
        Dim name = OpName(op)
        Dim label = L(DiskStates.LabelKey(op.Action, op.Record))
        If res Is Nothing Then
            lastOutcome = Localization.Format(L("vd_mgr_failed_fmt"), label, name, L("shell_start_failed"))
            lastOutcomeWarns = True
        ElseIf res.Verdict = "Done" OrElse res.Verdict = "Passed" Then
            Dim doneKey = "vd_mgr_done_fmt"
            Dim chkExit As Integer
            If ChkdskExitOf(res, chkExit) AndAlso DiskStates.ChkdskDoneKey(op.Action, chkExit) <> "" Then
                doneKey = DiskStates.ChkdskDoneKey(op.Action, chkExit)
            End If
            lastOutcome = Localization.Format(L(doneKey), label, name)
            lastOutcomeWarns = False
            If op.Action = DiskAction.Info Then infoByKey(op.Record.Key) = InfoLines(res.Output)
        ElseIf res.Verdict = "Stopped" AndAlso op.Stopped Then
            lastOutcome = Localization.Format(L("vd_mgr_stopped_fmt"), label, name)
            lastOutcomeWarns = False
        Else
            Dim why As String
            If If(res.Output, "").Contains("administrator consent was not given") OrElse res.Reason = "shell_elevation_refused" Then
                why = L("vd_mgr_elevation_refused")
            ElseIf DiskCommands.ExitKeyFor(DiskStates.VerbOf(op.Action), res.ExitCode, res.Output) <> "" Then
                why = L(DiskCommands.ExitKeyFor(DiskStates.VerbOf(op.Action), res.ExitCode, res.Output))
            ElseIf Not String.IsNullOrEmpty(res.Reason) Then
                why = L(res.Reason)
            Else
                why = L("shell_verdict_" & res.Verdict.ToLowerInvariant().Replace(" ", "_"))
            End If
            lastOutcome = Localization.Format(L("vd_mgr_failed_fmt"), label, name, why)
            lastOutcomeWarns = True
        End If
        RefreshView()
    End Sub

    ' chkdsk's exit code as the run reported it ("chkdsk_exit" among the result's numbers); False when
    ' the run did not say - an older filedo.exe - and the line stays the plain "done".
    Friend Shared Function ChkdskExitOf(res As Runner.RunResult, ByRef code As Integer) As Boolean
        Try
            Dim nums = If(res Is Nothing OrElse res.ResultInfo Is Nothing, Nothing, res.ResultInfo.Numbers)
            Dim v As Object = Nothing
            If nums IsNot Nothing AndAlso nums.TryGetValue("chkdsk_exit", v) AndAlso v IsNot Nothing Then
                code = Convert.ToInt32(v, Globalization.CultureInfo.InvariantCulture)
                Return True
            End If
        Catch
        End Try
        Return False
    End Function

    ' What `info` printed, without the run's own opening and closing lines.
    Friend Shared Function InfoLines(output As String) As String
        Dim keep As New List(Of String)
        For Each raw In If(output, "").Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries)
            Dim t = raw.TrimEnd()
            If t.Trim() = "" Then Continue For
            If System.Text.RegularExpressions.Regex.IsMatch(t, "^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d ") Then Continue For
            If t.TrimStart().StartsWith("Finish:", StringComparison.Ordinal) Then Continue For
            keep.Add(t)
        Next
        Return String.Join(Environment.NewLine, keep.ToArray())
    End Function

    Friend Sub StopStoppable()
        For Each op In running
            If op.Action = DiskAction.Verify AndAlso op.Run IsNot Nothing Then
                op.Stopped = True
                op.Run.RequestStop()
            End If
        Next
    End Sub

    Private Sub ClearQueue()
        For Each op In queue
            busy.Remove(op.Record.Key)
            If op.Env IsNot Nothing Then op.Env.Clear()
        Next
        queue.Clear()
        RefreshView()
    End Sub

    ' Opened again - from the rail, from a second start - while it is on screen, minimized, or hidden
    ' waiting for a run to end: it comes forward, and a close that was waiting for the run is off.
    Friend Sub BringBack()
        closeWhenIdle = False
        If Not Visible Then Show()
        If WindowState = FormWindowState.Minimized Then WindowState = lastShownState
        Activate()
        If AppHost.Current IsNot Nothing Then AppHost.Current.Restored(Me)
    End Sub

    ' The tray's count of mounted disks - the containers Unmount all acts on, not the images the user
    ' mounted in Explorer (AUD-85-F3). The window's own status line counts every drive, as before.
    Friend ReadOnly Property MountedCount As Integer
        Get
            Return If(snapshot Is Nothing, 0, snapshot.Disks.Where(Function(r) r.IsMounted AndAlso Not r.IsImage).Count())
        End Get
    End Property
    Friend ReadOnly Property JobCount As Integer
        Get
            Return running.Count
        End Get
    End Property
    Friend ReadOnly Property ClosePending As Boolean
        Get
            Return closeWhenIdle
        End Get
    End Property
    Friend Sub UnmountAllFromTray()
        If Context().Packaged OrElse snapshot Is Nothing Then Return
        Dim mounted = UnmountAllTargets(snapshot, busy, Context())
        For Each group In mounted.GroupBy(Function(r) UnmountActionFor(New List(Of DiskRecord) From {r}))
            DoQuick(group.Key, group.ToList())
        Next
    End Sub

    ' The rows the tray's Unmount all acts on (SP-0081 5): the mounted containers this window owns that
    ' the Unmount of a row accepts right now - the one predicate every other gesture passes, so a row
    ' with an operation running or queued takes no second one, and the Store build takes none. An image
    ' the user mounted in Explorer stays a per-row action of this window, never the tray's.
    Friend Shared Function UnmountAllTargets(snap As DiskSnapshot, busyVerbs As IDictionary(Of String, String), ctx As DiskContext) As List(Of DiskRecord)
        Dim out As New List(Of DiskRecord)
        If snap Is Nothing Then Return out
        For Each r In snap.Disks
            If r.IsImage OrElse Not r.IsMounted Then Continue For
            Dim verb As String = Nothing
            If busyVerbs IsNot Nothing Then busyVerbs.TryGetValue(r.Key, verb)
            If DiskStates.WhyNot(DiskAction.Unmount, r, DiskStates.StateOf(r, verb), ctx) = "" Then out.Add(r)
        Next
        Return out
    End Function

    Friend Function UnmountAllTargetsForTest() As List(Of DiskRecord)
        Return UnmountAllTargets(snapshot, busy, Context())
    End Function

    Friend ReadOnly Property IsBusy As Boolean
        Get
            Return running.Count > 0 OrElse queue.Count > 0
        End Get
    End Property

    ' ---- gestures (spec 6.2) ---------------------------------------------

    Private Sub List_MouseDoubleClick(sender As Object, e As MouseEventArgs)
        If e.Button <> MouseButtons.Left Then Return
        Dim hit = list.HitTest(e.Location)
        If hit.Item Is Nothing Then Return
        For Each it As ListViewItem In list.Items
            it.Selected = (it Is hit.Item)
        Next
        DoDefaultAction()
    End Sub

    ' D3: a disk at rest is mounted, a mounted one opened; anything else only shows its detail.
    Private Sub DoDefaultAction()
        Dim sel = SelectedRecords()
        If sel.Count <> 1 Then Return
        Dim a = DiskStates.DefaultAction(RowState(sel(0)))
        If a.HasValue Then Perform(a.Value)
    End Sub

    Private Sub List_MouseUp(sender As Object, e As MouseEventArgs)
        If e.Button <> MouseButtons.Right Then Return
        Dim hit = list.HitTest(e.Location)
        If hit.Item Is Nothing Then
            spaceMenu.Show(list, e.Location)
            Return
        End If
        If Not hit.Item.Selected Then
            For Each it As ListViewItem In list.Items
                it.Selected = (it Is hit.Item)
            Next
        End If
        BuildActionMenu(rowMenu, SelectedRecords(), False)
        rowMenu.Show(list, e.Location)
    End Sub

    Private Sub ShowRowMenuAtFocus()
        Dim sel = SelectedRecords()
        If sel.Count = 0 Then
            spaceMenu.Show(list, Ui.Px(Me, 8), Ui.Px(Me, 8))
            Return
        End If
        Dim anchor = If(list.FocusedItem, list.SelectedItems(0))
        BuildActionMenu(rowMenu, sel, False)
        rowMenu.Show(list, anchor.Bounds.Left + Ui.Px(Me, 16), anchor.Bounds.Bottom)
    End Sub

    ' The context menu (and the More menu): quick actions first, delegated ones under a separator,
    ' the destructive ones last and set apart. A disabled item says why in its tooltip.
    Private Sub BuildActionMenu(menu As ContextMenuStrip, sel As List(Of DiskRecord), withGlobals As Boolean)
        menu.Items.Clear()
        Dim groups = New DiskAction()() {
            New DiskAction() {DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.Unmount, DiskAction.UnmountImage,
                              DiskAction.OpenDrive, DiskAction.SaveNow, DiskAction.Info, DiskAction.Verify, DiskAction.CheckVolume,
                              DiskAction.RepairVolume, DiskAction.AutoOn, DiskAction.AutoOff, DiskAction.Autostart, DiskAction.AddToList, DiskAction.Forget, DiskAction.ShowInFolder, DiskAction.CopyPath},
            New DiskAction() {DiskAction.ImageToFile, DiskAction.Export, DiskAction.Compact, DiskAction.Grow, DiskAction.Seal, DiskAction.Clone, DiskAction.ChangePassword},
            New DiskAction() {DiskAction.ShareDisk, DiskAction.OpenShared, DiskAction.CloseShared, DiskAction.UnshareDisk, DiskAction.ShareAutoOn, DiskAction.ShareAutoOff},
            New DiskAction() {DiskAction.Format, DiskAction.Destroy}}
        For Each g In groups
            Dim added = False
            For Each a In g
                ' The More menu carries Autostart among its globals, whatever is selected.
                If withGlobals AndAlso a = DiskAction.Autostart Then Continue For
                If Not Offered(a, sel) Then Continue For
                If DiskStates.HiddenInBuild(a, Context()) Then Continue For
                If sel.Count > 1 AndAlso Not DiskStates.AppliesToMany(a) Then Continue For
                If Not added AndAlso menu.Items.Count > 0 Then menu.Items.Add(New ToolStripSeparator())
                added = True
                menu.Items.Add(MenuItem(a, sel))
            Next
        Next
        If withGlobals Then
            If menu.Items.Count > 0 Then menu.Items.Add(New ToolStripSeparator())
            If Not DiskStates.HiddenInBuild(DiskAction.MountImage, Context()) Then menu.Items.Add(MenuItem(DiskAction.MountImage, sel))
            If Not DiskStates.HiddenInBuild(DiskAction.Adopt, Context()) Then menu.Items.Add(MenuItem(DiskAction.Adopt, sel))
            ' The shutdown guard belongs to the account, not to a row: reachable with nothing selected
            ' and with no disk registered at all (SP-0080 5).
            If Not DiskStates.HiddenInBuild(DiskAction.Autostart, Context()) Then menu.Items.Add(MenuItem(DiskAction.Autostart, sel))
            Dim pathCol As New ToolStripMenuItem(L("vd_mgr_show_path_column")) With {.Checked = list.Columns(Col.Path).Width > 0}
            AddHandler pathCol.Click, Sub() ToggleColumn(Col.Path)
            Dim sinceCol As New ToolStripMenuItem(L("vd_mgr_show_since_column")) With {.Checked = list.Columns(Col.Since).Width > 0}
            AddHandler sinceCol.Click, Sub() ToggleColumn(Col.Since)
            menu.Items.Add(pathCol)
            menu.Items.Add(sinceCol)
            ' The detail pane's way back when it has been hidden, and a way to hide it from here too.
            Dim detailItem As New ToolStripMenuItem(L(If(detailOpenState, "vd_mgr_detail_hide", "vd_mgr_detail_show")))
            AddHandler detailItem.Click, Sub() ToggleDetail()
            menu.Items.Add(detailItem)
        End If
        ThemeMenu(menu)
    End Sub

    Private Function MenuItem(a As DiskAction, sel As List(Of DiskRecord)) As ToolStripMenuItem
        Dim action = a
        Dim item As New ToolStripMenuItem(L(DiskStates.LabelKey(a, Nothing)).Replace("&", "&&"))
        Dim keys = DiskShortcuts.TextFor(a)
        If keys <> "" Then item.ShortcutKeyDisplayString = keys
        Dim why = If(sel Is Nothing, WhyText(a, New List(Of DiskRecord)), WhyText(a, sel))
        item.Enabled = (why = "")
        If why <> "" Then item.ToolTipText = why
        Dim danger = (DiskStates.KindOf(a) = DiskActionKind.Destructive)
        If danger Then item.Tag = "danger"
        item.Image = MenuImage(DiskGlyphs.For(a), why = "", danger)
        AddHandler item.Click, Sub() Perform(action)
        Return item
    End Function

    Private Sub ToggleColumn(c As Col)
        Dim h = list.Columns(c)
        h.Width = If(h.Width > 0, 0, Ui.Px(Me, ColumnShown(c)))
    End Sub

    Private Sub List_ColumnClick(sender As Object, e As ColumnClickEventArgs)
        If sortColumn = e.Column Then
            sortDescending = Not sortDescending
        Else
            sortColumn = e.Column
            sortDescending = False
        End If
        list.Invalidate()
        RefreshView()
    End Sub

    Private Sub List_DragEnter(sender As Object, e As DragEventArgs)
        e.Effect = DragDropEffects.None
        Dim files = TryCast(e.Data.GetData(DataFormats.FileDrop), String())
        If files Is Nothing Then Return
        If files.Any(Function(f) DiskCommands.IsFddPath(f) OrElse IsImagePath(f)) Then e.Effect = DragDropEffects.Copy
    End Sub

    Private Sub List_DragDrop(sender As Object, e As DragEventArgs)
        Dim files = TryCast(e.Data.GetData(DataFormats.FileDrop), String())
        If files Is Nothing Then Return
        ' Handled after the drop returns, so the dialogs it may open do not hold Explorer's drag.
        BeginInvoke(New Action(Sub()
                                   AddFiles(files.Where(Function(f) DiskCommands.IsFddPath(f)))
                                   For Each img In files.Where(Function(f) IsImagePath(f))
                                       MountImageFile(img, confirm:=True)
                                   Next
                                   If Not files.Any(Function(f) DiskCommands.IsFddPath(f) OrElse IsImagePath(f)) Then
                                       ShellDialog.Notice(Me, L("vd_mgr_title"), L("vd_mgr_drop_not_container"))
                                   End If
                               End Sub))
    End Sub

    Friend Shared Function IsImagePath(p As String) As Boolean
        Dim e = If(p, "").Trim().ToLowerInvariant()
        Return e.EndsWith(".vhd") OrElse e.EndsWith(".vhdx") OrElse e.EndsWith(".iso")
    End Function

    ' The keyboard map of spec 6.2, read from DiskShortcuts.All - the table the help window lists and
    ' the tooltips and menus quote. Delete removes from the list and is never Destroy (spec 9).
    Protected Overrides Function ProcessCmdKey(ByRef msg As Message, keyData As Keys) As Boolean
        Dim s = DiskShortcuts.Find(keyData, list.Focused)
        If s IsNot Nothing AndAlso RunShortcut(s) Then Return True
        Return MyBase.ProcessCmdKey(msg, keyData)
    End Function

    Friend Function RunShortcut(s As DiskShortcut) As Boolean
        Select Case s.Command
            Case DiskCommand.Perform
                Dim a = s.Action.Value
                If a = DiskAction.Unmount Then a = UnmountActionFor(SelectedRecords())
                If a = DiskAction.Refresh Then
                    RequestRead()
                Else
                    Perform(a)
                End If
                Return True
            Case DiskCommand.Add
                BrowseAndAdd()
                Return True
            Case DiskCommand.Help
                ShowHelp()
                Return True
            Case DiskCommand.MainWindow
                RaiseEvent ShellRequested()
                Return True
            Case DiskCommand.Filter
                filterBox.Focus()
                filterBox.SelectAll()
                Return True
            Case DiskCommand.ClearFilter
                If filterBox.Text = "" Then Return False
                filterBox.Clear()
                Return True
            Case DiskCommand.SelectAll
                rebuilding = True
                For Each it As ListViewItem In list.Items
                    it.Selected = True
                Next
                rebuilding = False
                SelectionChanged()
                Return True
            Case DiskCommand.CopyPath
                If SelectedRecords().Count = 0 Then Return False
                Perform(DiskAction.CopyPath)
                Return True
            Case DiskCommand.ContextMenu
                ShowRowMenuAtFocus()
                Return True
            Case DiskCommand.DefaultAction
                DoDefaultAction()
                Return True
        End Select
        Return False
    End Function

    ' ---- the window's life -----------------------------------------------

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
        DiskManagerIcon.Apply(Me)
    End Sub

    ' The self-test and the capture tool open the window without a first run (they must not write the
    ' user's settings, and a dialog nobody answers would hang them).
    Friend Shared SuppressWelcome As Boolean = False
    ' ...and without reading the disks' state itself, for the picture of a state that is handed to it.
    Friend Shared SuppressReads As Boolean = False

    Protected Overrides Sub OnShown(e As EventArgs)
        MyBase.OnShown(e)
        StartWatcher()
        pollTimer.Start()
        clockTimer.Start()
        RequestRead()
        ' First run (APP-BEHAVIOUR rule 11): the first-steps window opens once, by itself, and again
        ' whenever Help asks. It is recorded as shown before it is shown, so a window that is closed
        ' by a crash is not the one that comes back on every start.
        If NeedsWelcome(ShellSettings.DiskManagerWelcomed(), SuppressWelcome) Then
            ShellSettings.SetDiskManagerWelcomed(True)
            BeginInvoke(New MethodInvoker(AddressOf ShowWelcome))
        End If
    End Sub

    ' Whether the first-steps window opens by itself: once, and never for a tool that only looks.
    Friend Shared Function NeedsWelcome(alreadyShown As Boolean, suppressed As Boolean) As Boolean
        Return Not suppressed AndAlso Not alreadyShown
    End Function

    Protected Overrides Sub OnVisibleChanged(e As EventArgs)
        MyBase.OnVisibleChanged(e)
        If Visible AndAlso snapshot IsNot Nothing Then RequestRead()
    End Sub

    Protected Overrides Sub OnResize(e As EventArgs)
        MyBase.OnResize(e)
        If WindowState <> FormWindowState.Minimized Then lastShownState = WindowState
        ' Back from the taskbar: what changed while nobody looked is read at once.
        If WindowState <> FormWindowState.Minimized AndAlso wasMinimized Then RequestRead()
        wasMinimized = (WindowState = FormWindowState.Minimized)
        If wasMinimized AndAlso Visible AndAlso ShellSettings.MinimizeToTray() AndAlso AppHost.Current IsNot Nothing Then
            AppHost.Current.MinimizeWindow(Me)
        End If
    End Sub

    Private lastShownState As FormWindowState = FormWindowState.Normal
    Private wasMinimized As Boolean = False

    ' The constructor runs before the window has a monitor, so every size it states in design pixels
    ' is at the system's DPI - which, on a second screen at another scaling, is not the window's.
    ' From here on the monitor is known: the sizes are stated again for it.
    Protected Overrides Sub OnLoad(e As EventArgs)
        MyBase.OnLoad(e)
        ApplyDpiSizes()
        If Not placementRestored Then
            Size = OpeningSize()
            CenterToScreen()
        End If
        RestoreColumns()
        FitStateColumn()
        If Not pendingBounds.IsEmpty Then
            Location = pendingBounds.Location
            BeginInvoke(New MethodInvoker(AddressOf FinishPendingPlacement))
        End If
        layoutApplied = True
    End Sub

    ' Set once OnLoad has put the user's saved columns and placement on the window. The host builds a
    ' manager it never shows (a hidden start, the first minimize-to-tray) so that the tray has a
    ' snapshot to count; such a window holds the defaults, and closing it at Exit or at sign-out must
    ' not write them over the user's (AUD-85-F1).
    Private layoutApplied As Boolean = False

    Protected Overrides Sub OnActivated(e As EventArgs)
        MyBase.OnActivated(e)
        Theme.CurrentDpi = DeviceDpi
    End Sub

    ' Moving to a display with another scaling: Windows moves the frame, the rest follows. The fonts
    ' made from now on are for that display (Theme.vb, "fonts and the display's scaling").
    Protected Overrides Sub OnDpiChanged(e As DpiChangedEventArgs)
        Theme.CurrentDpi = e.DeviceDpiNew
        MyBase.OnDpiChanged(e)
        ApplyDpiSizes()
        If e.DeviceDpiOld > 0 Then
            For Each h As ColumnHeader In list.Columns
                h.Width = CInt(h.Width * e.DeviceDpiNew / CDbl(e.DeviceDpiOld))
            Next
        End If
        ' WinForms re-sizes the fonts attached to controls itself; the paint fonts (the list header,
        ' the menus, the buttons' captions) are fields and come out at the old display's size unless
        ' they are made again here.
        ApplyTheme()
    End Sub

    Private placementRestored As Boolean = False

    Private Sub ApplyDpiSizes()
        MinimumSize = WindowPlacement.MinimumFor(Me, MinWidth, MinHeight)
        toolbar.Padding = Ui.PxPad(Me, 12, 10, 12, 4)
        listHost.Padding = Ui.PxPad(Me, 12, 0, 12, 8)
        detailPanel.Padding = Ui.PxPad(Me, 12, 8, 12, 8)
        detailBar.Padding = Ui.PxPad(Me, 12, 4, 12, 4)
        strip.Padding = Ui.PxPad(Me, 12, 6, 12, 6)
        filterLabel.Margin = Ui.PxPad(Me, 0, 6, 6, 0)
        filterUnit.Margin = Ui.PxPad(Me, 12, 0, 12, 6)
        filterBox.Width = Ui.Px(Me, 150)
        filterBox.Margin = Ui.PxPad(Me, 0, 4, 0, 0)
        filterClear.Margin = Ui.PxPad(Me, 2, 2, 0, 2)
        detailTitle.Margin = Ui.PxPad(Me, 0, 4, 8, 4)
        emptyLabel.Margin = Ui.PxPad(Me, 16, 16, 16, 8)
        opBar.Size = Ui.PxSize(Me, 120, 14)
        For Each b As Control In New Control() {newBtn, addBtn, mountBtn, unmountBtn, openBtn, saveBtn, moreBtn,
                                                refreshBtn, settingsBtn, helpBtn, mainBtn, emptyNew, emptyAdd, stopBtn, clearQueueBtn}
            b.Margin = Ui.PxPad(Me, 0, 0, 6, 6)
        Next
        For Each b As Control In New Control() {addBtn, saveBtn, moreBtn, helpBtn}
            b.Margin = Ui.PxPad(Me, 0, 0, 20, 6)
        Next
        For Each b In detailButtons.Controls.Cast(Of Control)()
            b.Margin = Ui.PxPad(Me, 0, 0, 6, 6)
        Next
        mainBtn.Picture = AppIcon.Mark(Ui.Px(Me, 24))
        RebuildGlyphs()
        GlyphBitmaps.Clear()
    End Sub

    ' Most of the screen the window opens on, never more than it, never below the minimum - the way
    ' the shell sizes its first opening. A saved placement overrides it.
    Private Function OpeningSize() As Size
        Dim work = Screen.FromHandle(Handle).WorkingArea
        Dim least = WindowPlacement.MinimumFor(Me, MinWidth, MinHeight)
        Dim most = Ui.PxSize(Me, 1180, 760)
        Dim w = Math.Min(Math.Max(CInt(work.Width * 0.6), least.Width), most.Width)
        Dim h = Math.Min(Math.Max(CInt(work.Height * 0.65), least.Height), most.Height)
        Return New Size(Math.Min(w, work.Width), Math.Min(h, work.Height))
    End Function

    ' Spec 7.4. A run started from here lives in the process's job, so "let it finish" hides the
    ' window and closes it when the run has ended (D7: only for a run that neither elevates nor
    ' destroys). Closing never unmounts, saves or stops a disk (principle 5).
    Friend Enum CloseDecision
        Allow
        Ask
        WaitForRun
    End Enum

    Friend Shared Function DecisionOnClose(busyNow As Boolean, closePending As Boolean, forced As Boolean) As CloseDecision
        If forced OrElse Not busyNow Then Return CloseDecision.Allow
        If closePending Then Return CloseDecision.WaitForRun
        Return CloseDecision.Ask
    End Function

    Protected Overrides Sub OnFormClosing(e As FormClosingEventArgs)
        Dim forced = (e.CloseReason = CloseReason.WindowsShutDown OrElse e.CloseReason = CloseReason.TaskManagerClosing)
        If forced AndAlso AppHost.Current IsNot Nothing Then AppHost.Current.EndingSession()
        Dim decision = DecisionOnClose(IsBusy, closeWhenIdle, forced)
        If decision <> CloseDecision.Allow Then
            e.Cancel = True
            ' A second close while Stop and close is pending must not let the process job end the
            ' child before its run, report and cleanup have finished.
            If decision = CloseDecision.WaitForRun Then Return
            Dim names = String.Join(", ", running.Concat(queue).Select(Function(o) L(DiskStates.LabelKey(o.Action, o.Record)) & " - " & OpName(o)).Distinct().ToArray())
            Dim background = running.Concat(queue).All(Function(o) Not DiskStates.Elevates(o.Action) AndAlso
                                                                   DiskStates.KindOf(o.Action) <> DiskActionKind.Destructive)
            Dim pick = ShellDialog.Ask(Me, DestructiveDialogs.CloseDiskManagerBusy(dict, names, background))
            Select Case pick
                Case 1
                    ClearQueue()
                    closeWhenIdle = True
                    For Each op In running
                        op.Stopped = True
                        If op.Run IsNot Nothing Then op.Run.RequestStop()
                    Next
                Case 2
                    ClearQueue()
                    closeWhenIdle = True
                    Hide()
            End Select
            Return
        End If

        If Not forced AndAlso Visible AndAlso Not closeNoticeSaid AndAlso snapshot IsNot Nothing AndAlso
           snapshot.MountedCount > 0 AndAlso ShellSettings.DiskManagerCloseNotice() Then
            closeNoticeSaid = True
            Dim pick = ShellDialog.Ask(Me, L("vd_mgr_close_mounted_title"), LText("vd_mgr_close_mounted"),
                                       New String() {L("shell_btn_close"), L("vd_mgr_btn_dont_say")}, 0, 0)
            If pick = 1 Then ShellSettings.SetDiskManagerCloseNotice(False)
        End If

        If layoutApplied Then SaveLayout()
        pollTimer.Stop()
        clockTimer.Stop()
        settleTimer.Stop()
        If watcher IsNot Nothing Then
            watcher.EnableRaisingEvents = False
            watcher.Dispose()
            watcher = Nothing
        End If
        MyBase.OnFormClosing(e)
    End Sub

    Private Sub SaveLayout()
        Try
            Dim b = If(WindowState = FormWindowState.Normal, Bounds, RestoreBounds)
            ShellSettings.SavePlacementOf(ShellSettings.DiskManagerPrefix, b.X, b.Y, b.Width, b.Height,
                                          ShellForm.SavesMaximized(WindowState, lastShownState), DeviceDpi)
            Dim widths As New List(Of Integer)
            Dim dpi = Math.Max(DeviceDpi, 48)
            For Each h As ColumnHeader In list.Columns
                widths.Add(CInt(Math.Round(h.Width * 96.0 / dpi)))
            Next
            ShellSettings.SetDiskManagerColumns(widths)
            ShellSettings.SetDiskManagerSort(sortColumn, sortDescending)
        Catch ex As Exception
            ShellLog.Write("save the disk manager's layout", ex)
        End Try
    End Sub

    ' The rectangle the window has to end up at, when that is on a display whose scaling is not the
    ' one the layout was built for: the window is created on the primary display and moved in
    ' OnLoad, which is what makes WinForms re-scale it (ShellForm.RestorePlacement says why).
    Private pendingBounds As Rectangle = Rectangle.Empty
    Private pendingMaximized As Boolean = False

    ' APP-BEHAVIOUR rule 10, as the shell does it (WindowPlacement.Place).
    Private Sub RestorePlacement()
        Try
            Dim p = ShellSettings.LoadPlacementOf(ShellSettings.DiskManagerPrefix)
            If Not p.HasValue Then Return
            Dim screens As New List(Of WindowPlacement.ScreenArea)
            For Each s As Screen In Screen.AllScreens
                screens.Add(New WindowPlacement.ScreenArea(s.WorkingArea, WindowPlacement.DpiOf(s)))
            Next
            Dim r = WindowPlacement.Place(New Rectangle(p.X, p.Y, p.Width, p.Height), p.Dpi, screens, SystemInformation.CaptionHeight)
            If Not r.IsEmpty Then
                StartPosition = FormStartPosition.Manual
                placementRestored = True
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
            ShellLog.Write("restore the disk manager's placement", ex)
        End Try
    End Sub

    Private Sub FinishPendingPlacement()
        If pendingBounds.IsEmpty Then Return
        Dim b = pendingBounds
        pendingBounds = Rectangle.Empty
        Try
            Bounds = b
            If pendingMaximized Then WindowState = FormWindowState.Maximized
        Catch ex As Exception
            ShellLog.Write("finish the disk manager's placement", ex)
        End Try
    End Sub

    ' The widths are kept in design pixels, so a list saved at one scaling opens right at another. A
    ' first opening widens each shown column to its own heading in this language (APP-BEHAVIOUR
    ' rule 2: a heading is not cut); a width the user chose is theirs.
    Private Sub RestoreColumns()
        Dim widths = ShellSettings.DiskManagerColumns()
        ' A layout saved before the Carrier column (SP-0148) keeps its widths; the new column takes its
        ' design width.
        If widths.Count = list.Columns.Count - 1 Then widths.Add(ColumnDesign(list.Columns.Count - 1))
        Dim saved = (widths.Count = list.Columns.Count)
        If Not saved Then widths = ColumnDesign.ToList()
        Using g = list.CreateGraphics()
            For i = 0 To widths.Count - 1
                Dim w = Ui.Px(Me, widths(i))
                If Not saved AndAlso w > 0 Then
                    ' Measured as List_DrawColumnHeader draws it, with room for the sort arrow.
                    Dim heading = TextRenderer.MeasureText(g, list.Columns(i).Text & " " & ChrW(&H25B4), If(headerFont, list.Font)).Width + Ui.Px(Me, 16)
                    w = Math.Max(w, heading)
                    ' ...and to the longest word the column can hold in this language, so "obfuscated"
                    ' is not cut to "obfusca..." (rule 2: growing text grows the layout).
                    For Each sample In SampleTexts(i)
                        w = Math.Max(w, TextRenderer.MeasureText(g, sample, list.Font).Width + Ui.Px(Me, 20) + If(i = Col.Name, Ui.Px(Me, 22), 0))
                    Next
                End If
                list.Columns(i).Width = w
            Next
        End Using
    End Sub

    ' The State column takes what the others leave, so the list fills its width and shows no
    ' horizontal scroll bar (a white bar on the dark theme) until the window is narrower than the
    ' columns need. Its own width is therefore never the user's to keep: the others' are.
    Private fitting As Boolean = False

    Private Sub FitStateColumn()
        If fitting OrElse list Is Nothing OrElse list.Columns.Count <= Col.State OrElse Not list.IsHandleCreated Then Return
        fitting = True
        Try
            Dim others = 0
            For i = 0 To list.Columns.Count - 1
                If i <> Col.State Then others += list.Columns(i).Width
            Next
            Dim room = list.ClientSize.Width - others
            list.Columns(Col.State).Width = Math.Max(Ui.Px(Me, 110), room - Ui.Px(Me, 4))
        Finally
            fitting = False
        End Try
    End Sub

    ' The words a column can hold that are known before any disk is read: the protection words, the
    ' profiles, every state word. A name, a path and a size are as long as the disk makes them.
    Private Function SampleTexts(column As Integer) As String()
        Select Case CType(column, Col)
            Case Col.Protection
                Return New String() {L("vd_mgr_prot_obfuscated"), L("vd_mgr_prot_encrypted")}
            Case Col.Profile
                Return New String() {"plain", "sealed", "vault", "vhdx"}
            Case Col.State
                Dim words As New List(Of String)
                For Each key In New String() {"vd_mgr_state_server_gone", "vd_mgr_state_mounted_ro", "vd_mgr_state_image", "vd_mgr_state_different",
                                              "vd_mgr_state_unclean", "vd_mgr_state_busy_unmount", "vd_mgr_state_queued"}
                    words.Add(L(key))
                Next
                words.Add(Localization.Format(L("vd_mgr_state_unsaved_fmt"), "180 MiB"))
                Return words.ToArray()
            Case Col.Drive
                Return New String() {"W:"}
            Case Col.Carrier
                Return New String() {L("vd_part_carrier_file"), L("vd_part_carrier_partition")}
        End Select
        Return New String() {}
    End Function

    <Runtime.InteropServices.DllImport("uxtheme.dll", CharSet:=Runtime.InteropServices.CharSet.Unicode)>
    Private Shared Function SetWindowTheme(hwnd As IntPtr, appName As String, idList As String) As Integer
    End Function

    <Runtime.InteropServices.DllImport("user32.dll")>
    Private Shared Function SendMessage(hwnd As IntPtr, msg As Integer, wParam As IntPtr, lParam As IntPtr) As IntPtr
    End Function

    Private Const LVM_GETHEADER As Integer = &H101F

    ' The header's chrome in the theme: the headings are drawn here, but the header's empty end is
    ' Windows', and without its dark theme it stays a white band on the dark window. (The list's own
    ' dark theme is not taken: it rules a line down every column.) A Windows without the theme keeps
    ' its own, which is no worse than before.
    Private Sub ApplyListChrome()
        If list Is Nothing OrElse Not list.IsHandleCreated Then Return
        Try
            Dim dark = Theme.Current.IsDark
            Dim header = SendMessage(list.Handle, LVM_GETHEADER, IntPtr.Zero, IntPtr.Zero)
            If header <> IntPtr.Zero Then SetWindowTheme(header, If(dark, "DarkMode_ItemsView", "ItemsView"), Nothing)
        Catch ex As Exception
            ShellLog.Write("theme the disk list's frame", ex)
        End Try
    End Sub

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
        MakePaintFonts()
        SuspendLayout()
        Try
            BackColor = p.Background
            ForeColor = p.Text
            root.BackColor = p.Background
            toolbar.BackColor = p.Background
            filterUnit.BackColor = p.Background
            listHost.BackColor = p.Background
            detailPanel.BackColor = p.Surface
            detailHeader.BackColor = p.Surface
            detailButtons.BackColor = p.Surface
            detailBar.BackColor = p.Surface
            strip.BackColor = p.SurfaceAlt
            opFlow.BackColor = p.SurfaceAlt

            filterLabel.Font = Theme.FontBody()
            filterLabel.ForeColor = p.Text
            filterBox.Font = Theme.FontBody()
            filterBox.BackColor = p.Surface
            filterBox.ForeColor = p.Text
            filterBox.BorderStyle = BorderStyle.FixedSingle

            list.Font = Theme.FontBody()
            list.BackColor = p.Surface
            list.ForeColor = p.Text
            emptyHost.BackColor = p.Surface
            emptyActions.BackColor = p.Surface
            emptyLabel.Font = Theme.FontBody()
            emptyLabel.ForeColor = p.MutedText
            emptyLabel.BackColor = p.Surface
            emptyLearn.Font = Theme.FontBody()
            emptyLearn.LinkColor = p.Link
            emptyLearn.ActiveLinkColor = p.Link
            emptyLearn.VisitedLinkColor = p.Link
            emptyLearn.BackColor = p.Surface

            detailTitle.Font = Theme.FontBodyStrong()
            detailTitle.ForeColor = p.Text
            detailText.Font = Theme.FontBody()
            detailText.ForeColor = p.Text

            reportLink.Font = Theme.FontBody()
            reportLink.LinkColor = p.Link
            reportLink.ActiveLinkColor = p.Link
            reportLink.VisitedLinkColor = p.Link

            ' Glyph buttons paint themselves from Theme.Current; they only need their font.
            For Each b In AllGlyphButtons()
                b.Font = bodyFont
            Next
            ApplyStripTheme()
            GlyphBitmaps.Clear()
            For Each m In New ContextMenuStrip() {rowMenu, moreMenu, spaceMenu, helpMenu}
                ThemeMenu(m)
            Next
            RebuildGlyphs()
            ApplyListChrome()
        Finally
            ResumeLayout(True)
        End Try
        Chrome.Apply(Me)
        Invalidate(True)
    End Sub

    ' Every glyph button the window holds now, wherever it sits - the detail pane's are rebuilt with
    ' the selection, so they are looked up rather than kept.
    Private Function AllGlyphButtons() As List(Of GlyphButton)
        Dim out As New List(Of GlyphButton)
        Dim found As Action(Of Control) = Nothing
        found = Sub(c)
                    For Each child As Control In c.Controls
                        Dim g = TryCast(child, GlyphButton)
                        If g IsNot Nothing Then out.Add(g)
                        found(child)
                    Next
                End Sub
        found(Me)
        Return out
    End Function

    Private Sub ApplyStripTheme()
        Dim p = Theme.Current
        summaryLabel.Font = Theme.FontBody()
        summaryLabel.ForeColor = If(staleKey <> "", p.Warning, p.MutedText)
        opLabel.Font = Theme.FontBody()
        opLabel.ForeColor = If(running.Count = 0 AndAlso lastOutcomeWarns, p.Warning, p.Text)
    End Sub

    ' The state glyphs, drawn in their tones for the palette in force (ICON-RENDER rule 2): index 0
    ' is the empty picture of a disk at rest, then ok, warning and error.
    ' The pictures the rows draw themselves (List_DrawSubItem), by image index: 1 ok, 2 warning, 3
    ' error. The image list holds blank pictures of the row's height only - it is what gives an
    ' owner-drawn list its row height, and its indexes are the rows' ImageIndex.
    Private ReadOnly stateBitmaps As New Dictionary(Of Integer, Bitmap)
    ' The key of the picture for a row background: the image index plus this step per background
    ' (0 plain, 1 hovered, 2 selected).
    Private Const StateVariantStep As Integer = 100

    Private Sub RebuildGlyphs()
        Dim p = Theme.Current
        Dim size = Ui.Px(Me, 16)
        For Each old In stateBitmaps.Values
            old.Dispose()
        Next
        stateBitmaps.Clear()
        glyphImages.Images.Clear()
        glyphImages.ImageSize = New Size(size, Ui.Px(Me, 28)) ' a row is a hit target: 28 logical px, the ICON-RENDER 5 floor under a mouse or a pen (0.15; it was 24)
        glyphImages.Images.Add(New Bitmap(glyphImages.ImageSize.Width, glyphImages.ImageSize.Height, Imaging.PixelFormat.Format32bppArgb))
        For Each state In New DiskRowState() {DiskRowState.Mounted, DiskRowState.Unsaved, DiskRowState.ServerGone}
            glyphImages.Images.Add(New Bitmap(glyphImages.ImageSize.Width, glyphImages.ImageSize.Height, Imaging.PixelFormat.Format32bppArgb))
            Dim index = glyphImages.Images.Count - 1
            ' One picture per row background (plain, hover, selected): the tone is chosen against the
            ' background it sits on, so each reaches 3:1 there (DiskStates.ToneOn).
            For look = 0 To 2
                Dim bmp As New Bitmap(size, size, Imaging.PixelFormat.Format32bppArgb)
                Using g = Graphics.FromImage(bmp)
                    Glyphs.Draw(g, DiskStates.GlyphOf(state), New Rectangle(0, 0, size, size),
                                DiskStates.ToneOn(state, p, RowBackColour(p, look = 2, look = 1)))
                End Using
                stateBitmaps(index + StateVariantStep * look) = bmp
            Next
        Next
        If list IsNot Nothing Then
            For i = 0 To Math.Min(list.Items.Count, shown_.Count) - 1
                list.Items(i).ImageIndex = ImageIndexOf(RowState(shown_(i)))
            Next
        End If
    End Sub

    ' ---- the rows, drawn from the palette -----------------------------------

    Private Sub SetHoverRow(index As Integer)
        If index = hoverRow Then Return
        Dim old = hoverRow
        hoverRow = index
        If old >= 0 AndAlso old < list.Items.Count Then list.RedrawItems(old, old, False)
        If index >= 0 AndAlso index < list.Items.Count Then list.RedrawItems(index, index, False)
    End Sub

    Private Sub List_MouseMove(sender As Object, e As MouseEventArgs)
        Dim hit = list.HitTest(e.Location)
        SetHoverRow(If(hit.Item Is Nothing, -1, hit.Item.Index))
    End Sub

    ' A row is the palette's: the surface, the hover tint, and the selection's own role with the
    ' ordinary text colour on it. The system highlight is a dark teal that the ordinary text cannot be
    ' read on, and a list that draws its selection itself is the only way to say what the selection's
    ' text colour is (APP-STYLE section 3: no colour resolved by anything but the palette).
    Private Sub List_DrawSubItem(sender As Object, e As DrawListViewSubItemEventArgs)
        Dim it = e.Item
        PaintCell(e.Graphics, CellBounds(it, e.ColumnIndex, e.Bounds), e.ColumnIndex, it.SubItems.Count, e.SubItem.Text,
                  it.ImageIndex, it.Selected, it.Index = hoverRow, it.Focused AndAlso list.Focused)
    End Sub

    ' The first column's bounds are the whole row's in the list's API: it ends where the second begins.
    Private Shared Function CellBounds(it As ListViewItem, column As Integer, reported As Rectangle) As Rectangle
        If column = 0 AndAlso it.SubItems.Count > 1 Then
            Return New Rectangle(reported.Left, reported.Top, Math.Max(0, it.SubItems(1).Bounds.Left - reported.Left), reported.Height)
        End If
        Return reported
    End Function

    ' One cell of a row, from the palette: the back, the state's picture in the first column, the text in
    ' the ordinary text colour, and the row's outline in the accent while the list has the keyboard. The
    ' list's handler and the picture Capture.vb makes of the window both come through here.
    Private Sub PaintCell(g As Graphics, cell As Rectangle, column As Integer, columns As Integer, text As String,
                          imageIndex As Integer, selected As Boolean, hovered As Boolean, keyboardHere As Boolean)
        Dim p = Theme.Current
        Using b As New SolidBrush(RowBackColour(p, selected, hovered))
            g.FillRectangle(b, cell)
        End Using
        Dim x = cell.Left + Ui.Px(Me, 6)
        If column = 0 Then
            Dim bmp As Bitmap = Nothing
            Dim look = If(selected, 2, If(hovered, 1, 0))
            If stateBitmaps.TryGetValue(imageIndex + StateVariantStep * look, bmp) Then
                g.DrawImage(bmp, x, cell.Top + (cell.Height - bmp.Height) \ 2, bmp.Width, bmp.Height)
            End If
            x += Ui.Px(Me, 16) + Ui.Px(Me, 6)
        End If
        Dim textArea As New Rectangle(x, cell.Top, Math.Max(0, cell.Right - x - Ui.Px(Me, 4)), cell.Height)
        TextRenderer.DrawText(g, text, list.Font, textArea, p.Text,
                              TextFormatFlags.VerticalCenter Or TextFormatFlags.Left Or TextFormatFlags.EndEllipsis Or TextFormatFlags.NoPrefix Or TextFormatFlags.PreserveGraphicsClipping)
        If keyboardHere Then
            Using pen As New Pen(p.Accent)
                g.DrawLine(pen, cell.Left, cell.Top, cell.Right, cell.Top)
                g.DrawLine(pen, cell.Left, cell.Bottom - 1, cell.Right, cell.Bottom - 1)
                If column = 0 Then g.DrawLine(pen, cell.Left, cell.Top, cell.Left, cell.Bottom - 1)
                If column = columns - 1 Then g.DrawLine(pen, cell.Right - 1, cell.Top, cell.Right - 1, cell.Bottom - 1)
            End Using
        End If
    End Sub

    ' The list as a picture, drawn by the same painters, for Capture.vb: a native list view is not
    ' something DrawToBitmap can print. origin is where the list's client area sits in the bitmap.
    Friend Sub PaintListForCapture(g As Graphics, origin As Point)
        Dim p = Theme.Current
        Dim top = 0
        If list.Items.Count > 0 Then top = list.Items(0).Bounds.Top
        If top <= 0 Then top = Ui.Px(Me, 24)
        Dim area As New Rectangle(origin, list.ClientSize)
        Dim oldClip = g.Clip
        g.SetClip(area)
        Using b As New SolidBrush(p.Surface)
            g.FillRectangle(b, area)
        End Using
        Dim x = 0
        For i = 0 To list.Columns.Count - 1
            Dim w = list.Columns(i).Width
            If w > 0 Then PaintHeaderCell(g, New Rectangle(origin.X + x, origin.Y, w, top), list.Columns(i).Text, i)
            x += w
        Next
        For Each it As ListViewItem In list.Items
            For j = 0 To it.SubItems.Count - 1
                Dim r = it.SubItems(j).Bounds
                If j = 0 AndAlso it.SubItems.Count > 1 Then r = New Rectangle(0, it.Bounds.Top, it.SubItems(1).Bounds.Left, it.Bounds.Height)
                If r.Width <= 0 Then Continue For
                r.Offset(origin)
                PaintCell(g, r, j, it.SubItems.Count, it.SubItems(j).Text, it.ImageIndex, it.Selected, False, False)
            Next
        Next
        g.Clip = oldClip
        Using pen As New Pen(p.Border)
            g.DrawRectangle(pen, New Rectangle(origin.X - 1, origin.Y - 1, list.ClientSize.Width + 1, list.ClientSize.Height + 1))
        End Using
    End Sub

    ' Where the list's client area sits in the window's layout - the origin PaintListForCapture wants.
    Friend Function ListOriginForCapture() As Point
        Return root.PointToClient(list.PointToScreen(Point.Empty))
    End Function

    ' The back of a row: the selection's own role, the hover tint, or the surface - and the text on any
    ' of them is the palette's Text, which the self-test holds to 4.5:1 on each in both themes.
    Friend Shared Function RowBackColour(p As Theme.Palette, selected As Boolean, hovered As Boolean) As Color
        If selected Then Return p.SurfaceSelected
        If hovered Then Return p.ControlHover
        Return p.Surface
    End Function

    ' What the hover tooltip of a row says: the disk, where its file is, and its state in words.
    Private Function RowTip(r As DiskRecord) As String
        Dim name = DisplayName(r)
        Dim where = If(r.IsPartition, r.PartitionPlace, r.Path)
        Dim head = If(where = "", name, name & " - " & where)
        Return head & Environment.NewLine & RowStateText(r)
    End Function

    ' The header of the list in the palette (a system header stays light on the dark theme), with
    ' the sort shown as an arrow beside the column's name.
    Private Sub List_DrawColumnHeader(sender As Object, e As DrawListViewColumnHeaderEventArgs)
        PaintHeaderCell(e.Graphics, e.Bounds, e.Header.Text, e.ColumnIndex)
    End Sub

    Private Sub PaintHeaderCell(g As Graphics, bounds As Rectangle, headerText As String, column As Integer)
        Dim p = Theme.Current
        Using back As New SolidBrush(p.SurfaceAlt)
            g.FillRectangle(back, bounds)
        End Using
        Using line As New Pen(p.Border)
            g.DrawLine(line, bounds.Left, bounds.Bottom - 1, bounds.Right, bounds.Bottom - 1)
            g.DrawLine(line, bounds.Right - 1, bounds.Top + 4, bounds.Right - 1, bounds.Bottom - 4)
        End Using
        Dim text = headerText
        If column = sortColumn Then text &= If(sortDescending, " " & ChrW(&H25BE), " " & ChrW(&H25B4))
        Dim r = New Rectangle(bounds.Left + Ui.Px(Me, 6), bounds.Top, Math.Max(0, bounds.Width - Ui.Px(Me, 8)), bounds.Height)
        TextRenderer.DrawText(g, text, If(headerFont, list.Font), r, p.Text,
                              TextFormatFlags.VerticalCenter Or TextFormatFlags.Left Or TextFormatFlags.EndEllipsis Or TextFormatFlags.NoPrefix Or TextFormatFlags.PreserveGraphicsClipping)
    End Sub

    ' The fonts the window paints with, made when the theme is applied - never once per paint, which
    ' leaks a GDI object each time (SP-0014 T1).
    ' The fonts the window paints with, made when the theme is applied - never once per paint, which
    ' leaks a GDI object each time (SP-0014 T1). They are made for THIS window's monitor, not the
    ' process's last-active one: a font held in a field rides no control, so WinForms' own rescale
    ' on a display change misses it - at the system's 150 % the header painted in 150 % type on a
    ' 100 % screen and was cut away (the release queue's "header captions clip at 100 %").
    Private Sub MakePaintFonts()
        Dim dpi = WindowDpi()
        headerFont = Theme.FontBodyStrong(dpi)
        bodyFont = Theme.FontBody(dpi)
    End Sub

    ' This window's monitor's dpi, once the window has one; before that, the process's last-active
    ' one. Every font the window makes goes through this: the theme can be applied while the window
    ' is behind another one on a display of another scaling (AppHost.ThemeChanged), and a font made
    ' for the window in front cuts its text here.
    Private Function WindowDpi() As Integer
        Return If(DeviceDpi > 0, DeviceDpi, Theme.CurrentDpi)
    End Function

    Private headerFont As Font
    Private bodyFont As Font

    Private Sub ThemeMenu(menu As ContextMenuStrip)
        If menu Is Nothing Then Return
        Dim p = Theme.Current
        If Not TypeOf menu.Renderer Is ShellMenuRenderer Then menu.Renderer = New ShellMenuRenderer()
        menu.BackColor = p.Surface
        menu.ForeColor = p.Text
        menu.ImageScalingSize = Ui.PxSize(Me, 16, 16)
        If bodyFont IsNot Nothing Then menu.Font = bodyFont
        For Each item As ToolStripItem In menu.Items
            item.ForeColor = If(TryCast(item.Tag, String) = "danger", p.Danger, p.Text)
        Next
    End Sub

    ' ---- seams for SelfTest.vb -------------------------------------------

    Friend Function PendingCloseCanceledForTest() As Boolean
        Dim fake As New DiskOp With {.Action = DiskAction.Info, .Record = New DiskRecord With {.Name = "pending"}}
        Dim previous = closeWhenIdle
        running.Add(fake)
        closeWhenIdle = True
        Try
            Dim e As New FormClosingEventArgs(CloseReason.UserClosing, False)
            OnFormClosing(e)
            Return e.Cancel AndAlso Not IsDisposed AndAlso running.Contains(fake)
        Finally
            running.Remove(fake)
            closeWhenIdle = previous
        End Try
    End Function

    Friend Sub ApplySnapshotForTest(s As DiskSnapshot, problem As String)
        ApplyRead(s, problem)
    End Sub

    ' SP-0148: a disk list as `vd disks json` would give it, without a filedo.exe.
    Friend Sub ApplyDisksForTest(doc As VdDisksDoc)
        SetDisks(doc)
    End Sub

    ' The Autostart dialog while one is held (Nothing when none is): a row opens it through Perform and
    ' closes it from a timer, because the dialog is modal.
    Friend ReadOnly Property AutostartDialogForTest As DiskAutostartDialog
        Get
            Return autostartDlg
        End Get
    End Property

    Friend Sub SelectForTest(ParamArray keys As String())
        For Each it As ListViewItem In list.Items
            it.Selected = keys.Contains(TryCast(it.Tag, String))
        Next
        SelectionChanged()
    End Sub

    Friend Sub SetBusyForTest(key As String, verb As String)
        If verb = "" Then busy.Remove(key) Else busy(key) = verb
        RefreshView()
    End Sub

    Friend ReadOnly Property RowTextsForTest As List(Of String())
        Get
            Dim out As New List(Of String())
            For Each it As ListViewItem In list.Items
                out.Add(it.SubItems.Cast(Of ListViewItem.ListViewSubItem)().Select(Function(s) s.Text).ToArray())
            Next
            Return out
        End Get
    End Property

    ' The self-test's view: the height a list row is given. The image list's picture height is what
    ' sets an owner-drawn list's row height, and a row is a hit target (ICON-RENDER rule 5).
    Friend ReadOnly Property ListRowHeightForTest As Integer
        Get
            Return If(list Is Nothing OrElse glyphImages Is Nothing, 0, glyphImages.ImageSize.Height)
        End Get
    End Property

    Friend ReadOnly Property ListShownForTest As Boolean
        Get
            Return emptyShown = ""
        End Get
    End Property

    Friend ReadOnly Property EmptyTextForTest As String
        Get
            Return emptyShown
        End Get
    End Property

    Friend ReadOnly Property SummaryForTest As String
        Get
            Return summaryLabel.Text
        End Get
    End Property

    Friend ReadOnly Property DetailForTest As String
        Get
            Return detailTitle.Text & Environment.NewLine & detailText.Text
        End Get
    End Property

    Friend ReadOnly Property DetailButtonsForTest As List(Of String)
        Get
            Return detailButtons.Controls.Cast(Of Control)().Select(Function(c) c.Text).ToList()
        End Get
    End Property

    Friend Function ToolbarStateForTest(which As String, ByRef reason As String) As Boolean
        Dim b As Button = Nothing
        Select Case which
            Case "mount" : b = mountBtn
            Case "unmount" : b = unmountBtn
            Case "open" : b = openBtn
            Case "save" : b = saveBtn
        End Select
        reason = If(b Is Nothing, "", If(TryCast(b.Tag, String), ""))
        Return b IsNot Nothing AndAlso b.Enabled
    End Function

    ' Whether a toolbar button is on the window at all - hidden in a build that cannot run it.
    ' Draws the window's client area for Capture.vb (the guide screenshots), off screen.
    Friend Sub DrawClientForCapture(bmp As Bitmap)
        root.DrawToBitmap(bmp, New Rectangle(0, 0, bmp.Width, bmp.Height))
    End Sub

    Friend Function ToolbarVisibleForTest(which As String) As Boolean
        Select Case which
            Case "mount" : Return Not hiddenByBuild.Contains(mountBtn)
            Case "unmount" : Return Not hiddenByBuild.Contains(unmountBtn)
            Case "open" : Return Not hiddenByBuild.Contains(openBtn)
            Case "save" : Return Not hiddenByBuild.Contains(saveBtn)
        End Select
        Return False
    End Function

    ' The buttons this build hides. A control of a window that is not on screen reports Visible false
    ' whatever was set, so the decision is kept as a value (and is what the test reads).
    Private ReadOnly hiddenByBuild As New HashSet(Of Control)

    Private Sub SetBuildVisibility(b As Control, hidden As Boolean)
        b.Visible = Not hidden
        If hidden Then hiddenByBuild.Add(b) Else hiddenByBuild.Remove(b)
    End Sub

    Friend ReadOnly Property GlyphButtonsForTest As List(Of GlyphButton)
        Get
            Return AllGlyphButtons()
        End Get
    End Property

    Friend Function TipForTest(c As Control) As String
        Return tips.GetToolTip(c)
    End Function

    Friend ReadOnly Property DetailOpenForTest As Boolean
        Get
            Return detailOpenState
        End Get
    End Property

    Friend Sub ToggleDetailForTest()
        SetDetailOpen(Not detailOpenState)
    End Sub

    Friend ReadOnly Property DetailCloseForTest As GlyphButton
        Get
            Return detailClose
        End Get
    End Property

    Friend ReadOnly Property DetailShowForTest As GlyphButton
        Get
            Return detailShow
        End Get
    End Property

    ' The filter as one unit: whether its label, its box and its clear button share a parent, and the
    ' clear button's state.
    Friend ReadOnly Property FilterIsOneUnitForTest As Boolean
        Get
            Return filterLabel.Parent Is filterUnit AndAlso filterBox.Parent Is filterUnit AndAlso filterClear.Parent Is filterUnit
        End Get
    End Property

    Friend Sub SetFilterForTest(text As String)
        filterBox.Text = text
    End Sub

    Friend ReadOnly Property FilterClearForTest As GlyphButton
        Get
            Return filterClear
        End Get
    End Property

    Friend ReadOnly Property FilterTextForTest As String
        Get
            Return filterBox.Text
        End Get
    End Property

    ' The regions in the order Tab visits them (spec 6.2): toolbar, filter, list, detail, its bar, strip.
    Friend ReadOnly Property TabOrderForTest As Integer()
        Get
            Return New Integer() {toolbar.TabIndex, filterUnit.TabIndex, listHost.TabIndex, detailPanel.TabIndex, detailBar.TabIndex, strip.TabIndex}
        End Get
    End Property

    Friend Function MenuItemsForTest(which As String) As List(Of ToolStripItem)
        Dim m As ContextMenuStrip
        Select Case which
            Case "row" : BuildActionMenu(rowMenu, SelectedRecords(), False) : m = rowMenu
            Case "more" : BuildActionMenu(moreMenu, SelectedRecords(), True) : m = moreMenu
            Case "help" : BuildHelpMenu() : m = helpMenu
            Case Else : BuildSpaceMenu() : m = spaceMenu
        End Select
        Return m.Items.Cast(Of ToolStripItem)().ToList()
    End Function

    Friend Function MenuForTest(rows As Boolean) As List(Of String)
        Dim m = If(rows, rowMenu, spaceMenu)
        If rows Then BuildActionMenu(rowMenu, SelectedRecords(), False)
        Dim out As New List(Of String)
        For Each item As ToolStripItem In m.Items
            If TypeOf item Is ToolStripSeparator Then
                out.Add("-")
            Else
                out.Add(item.Text & If(item.Enabled, "", " [" & item.ToolTipText & "]"))
            End If
        Next
        Return out
    End Function

End Class

' The list of disks: a details list that is drawn by the window (owner-drawn) and double-buffered, so
' painting every row from the palette does not flicker when the pointer moves or a row changes.
Friend Class DiskListView
    Inherits ListView

    Public Sub New()
        DoubleBuffered = True
    End Sub

End Class

' The palette's roles for a context menu: its surface, its selection and its separators, so the
' dark theme does not open a light menu (APP-STYLE section 5).
Friend Class MenuColours
    Inherits ProfessionalColorTable

    Public Overrides ReadOnly Property ToolStripDropDownBackground As Color
        Get
            Return Theme.Current.Surface
        End Get
    End Property
    Public Overrides ReadOnly Property ImageMarginGradientBegin As Color
        Get
            Return Theme.Current.Surface
        End Get
    End Property
    Public Overrides ReadOnly Property ImageMarginGradientMiddle As Color
        Get
            Return Theme.Current.Surface
        End Get
    End Property
    Public Overrides ReadOnly Property ImageMarginGradientEnd As Color
        Get
            Return Theme.Current.Surface
        End Get
    End Property
    Public Overrides ReadOnly Property MenuBorder As Color
        Get
            Return Theme.Current.Border
        End Get
    End Property
    Public Overrides ReadOnly Property MenuItemBorder As Color
        Get
            Return Theme.Current.Border
        End Get
    End Property
    Public Overrides ReadOnly Property MenuItemSelected As Color
        Get
            Return Theme.Current.ControlHover
        End Get
    End Property
    Public Overrides ReadOnly Property MenuItemSelectedGradientBegin As Color
        Get
            Return Theme.Current.ControlHover
        End Get
    End Property
    Public Overrides ReadOnly Property MenuItemSelectedGradientEnd As Color
        Get
            Return Theme.Current.ControlHover
        End Get
    End Property
    Public Overrides ReadOnly Property SeparatorDark As Color
        Get
            Return Theme.Current.Border
        End Get
    End Property
    Public Overrides ReadOnly Property SeparatorLight As Color
        Get
            Return Theme.Current.Surface
        End Get
    End Property
End Class

' The menu renderer: the colours above, and a disabled item's caption in the palette's disabled
' text rather than the system grey, which is unreadable on the dark surface.
Friend Class ShellMenuRenderer
    Inherits ToolStripProfessionalRenderer

    Public Sub New()
        MyBase.New(New MenuColours())
    End Sub

    Protected Overrides Sub OnRenderItemText(e As ToolStripItemTextRenderEventArgs)
        Dim p = Theme.Current
        If Not e.Item.Enabled Then
            e.TextColor = p.TextDisabled
        ElseIf TryCast(e.Item.Tag, String) = "danger" Then
            e.TextColor = p.Danger
        Else
            e.TextColor = p.Text
        End If
        MyBase.OnRenderItemText(e)
    End Sub

    Protected Overrides Sub OnRenderArrow(e As ToolStripArrowRenderEventArgs)
        e.ArrowColor = Theme.Current.Text
        MyBase.OnRenderArrow(e)
    End Sub
End Class
