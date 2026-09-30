' The Autostart dialog (SP-0080 5): the two halves of what FileDO does by itself, in one place.
'
'   At logon - the per-container mount tasks the console already makes, seen together: one row per
'   registered container with its task on or off, and the same switch the row carries, run through
'   the manager's own quick-action flow (one command line per operation, DiskCommands.Build).
'
'   At shutdown - the shutdown guard: the task that starts a watcher at logon, which saves dirty
'   ram disks and unmounts every mounted container when the session ends. Its state is said in
'   words, and its last run is told in sentences, the empty case included.
'
' The dialog learns state only from what the manager has already read (the `vd status json`
' snapshot and its rows) and acts only through the delegates the manager hands it - it never runs
' filedo.exe itself, never reads the state folder, and holds nothing back from the window's rules:
' a disabled control says why, the leave path is one, and the packaged build never opens it at all
' (the whole entry is hidden there, DiskStates.HiddenInBuild).

' The guard's words, as pure functions, so the self-test holds the state matrix and the last-run
' rendering in every locale without a window, a CLI or a container.
Public Module DiskAutostart

    ' The guard's state in words (SP-0080 5): "On and running" / "On, but not running" / "Off".
    ' Nothing - a snapshot without the guard field, an older CLI's answer - reads as off.
    Public Function GuardWordKey(g As DiskGuardState) As String
        If g Is Nothing OrElse Not g.Installed Then Return "vd_auto_guard_off"
        Return If(g.Running, "vd_auto_guard_on_running", "vd_auto_guard_on_stale")
    End Function

    ' The last run in sentences (SP-0080 5), the empty case included. A run that closed everything
    ' is one sentence; a run that left something behind names every row it did not, in the guard's
    ' own word for the outcome and the console's own sentence for the reason.
    Public Function GuardRunLines(g As DiskGuardState, dict As Dictionary(Of String, String)) As List(Of String)
        Dim out As New List(Of String)
        If g Is Nothing OrElse Not g.LastRun.HasValue Then
            out.Add(T(dict, "vd_auto_guard_run_never"))
            Return out
        End If
        Dim at = g.LastRun.Value.LocalDateTime.ToString("g")
        Dim rows = g.Containers
        If rows.Count = 0 Then
            out.Add(Localization.Format(T(dict, "vd_auto_guard_run_when_fmt"), at) & " " & T(dict, "vd_auto_guard_run_empty"))
            Return out
        End If
        Dim good = rows.Where(Function(r) r.Outcome = "saved" OrElse r.Outcome = "unmounted").Count()
        If good = rows.Count Then
            out.Add(Localization.Format(T(dict, "vd_auto_guard_run_ok_fmt"), at, good))
            Return out
        End If
        out.Add(Localization.Format(T(dict, "vd_auto_guard_run_left_fmt"), at, good, rows.Count))
        For Each r In rows
            If r.Outcome = "saved" OrElse r.Outcome = "unmounted" Then Continue For
            Dim word = T(dict, If(r.Outcome = "unfinished", "vd_auto_guard_outcome_unfinished", "vd_auto_guard_outcome_skipped"))
            Dim line = If(r.Name <> "", r.Name, r.Path) & ": " & word
            If r.Reason <> "" Then line &= " - " & r.Reason
            out.Add(line)
        Next
        Return out
    End Function

    ' The word for a logon row: whether its container mounts at logon.
    Public Function RowWordKey(r As DiskRecord) As String
        Return If(r.AutoMount, "vd_auto_row_on", "vd_auto_row_off")
    End Function

    Friend Function T(dict As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

End Module

Friend Class DiskAutostartDialog
    Inherits DiskPageDialog

    ' The manager's live answers and the manager's own ways of acting: the dialog holds no state of
    ' its own beyond what these return, and runs nothing the manager would not have run.
    Private ReadOnly recordsFn As Func(Of List(Of DiskRecord))
    Private ReadOnly guardFn As Func(Of DiskGuardState)
    Private ReadOnly performFn As Action(Of DiskAction, List(Of DiskRecord))
    Private ReadOnly guardRunFn As Action(Of Boolean)

    ' The rebuilt half of the page: the logon rows and the guard's block. The static sentences
    ' around them are built once.
    Private ReadOnly logonHost As TableLayoutPanel
    Private ReadOnly guardHost As TableLayoutPanel
    Private ReadOnly logonWord As Label
    Private ReadOnly logonIntro As Label

    Public Sub New(d As Dictionary(Of String, String), owner As Control,
                   records As Func(Of List(Of DiskRecord)), guard As Func(Of DiskGuardState),
                   perform As Action(Of DiskAction, List(Of DiskRecord)), guardRun As Action(Of Boolean))
        MyBase.New(d, Tr(d, "vd_auto_title"), owner, 620)
        recordsFn = records
        guardFn = guard
        performFn = perform
        guardRunFn = guardRun

        AddTitle(T("vd_auto_title"))
        AddParagraph(T("vd_auto_intro"))

        AddSection(T("vd_auto_logon_title"))
        logonWord = NewLabel("", contentWidth, strong:=True)
        AddRow(logonWord)
        logonIntro = NewLabel("", contentWidth, muted:=True)
        AddRow(logonIntro)
        logonHost = New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Margin = New Padding(0)
        }
        logonHost.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        AddRow(logonHost)

        AddSection(T("vd_auto_guard_title"))
        guardHost = New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Margin = New Padding(0)
        }
        guardHost.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        AddRow(guardHost)

        AddSection(T("vd_auto_notes_title"))
        AddParagraph(T("vd_auto_note_consent"), muted:=True)
        AddParagraph(T("vd_auto_note_signout"), muted:=True)
        AddParagraph(T("vd_auto_note_encrypted"), muted:=True)
        AddParagraph(T("vd_auto_note_uninstall"), muted:=True)

        RefreshState()

        Dim close = NewButton(T("shell_btn_close"), Nothing, True)
        close.Margin = PPad(0, 0, 0, 0)
        footer.Controls.Add(close)
        Finish()
        ActiveControl = close
    End Sub

    ' Rebuilds the two hosts from the manager's current answers. The manager calls it when its own
    ' view refreshes, so a switch that has just run turns back into the state the console reports -
    ' never into the state the dialog hoped for.
    Friend Sub RefreshState()
        Dim rows = RegisteredRows()
        If rows.Count = 0 Then
            logonWord.Text = T("vd_auto_logon_none")
        Else
            logonWord.Text = Localization.Format(T("vd_auto_logon_count_fmt"), rows.Count)
        End If
        logonIntro.Text = T("vd_auto_logon_intro")
        FillLogonRows(rows)
        FillGuardBlock()
        ApplyTheme()
        Invalidate(True)
    End Sub

    Private Function RegisteredRows() As List(Of DiskRecord)
        If recordsFn Is Nothing Then Return New List(Of DiskRecord)
        Dim rows = recordsFn()
        If rows Is Nothing Then Return New List(Of DiskRecord)
        Return rows
    End Function

    Private Function CurrentGuard() As DiskGuardState
        If guardFn Is Nothing Then Return Nothing
        Return guardFn()
    End Function

    Private Sub FillLogonRows(rows As List(Of DiskRecord))
        logonHost.SuspendLayout()
        For Each c As Control In logonHost.Controls.Cast(Of Control)().ToList()
            logonHost.Controls.Remove(c)
            c.Dispose()
        Next
        logonHost.RowStyles.Clear()
        Dim ctx As New DiskContext With {.Packaged = False}
        For Each r In rows
            logonHost.RowStyles.Add(New RowStyle(SizeType.AutoSize))
            logonHost.Controls.Add(LogonRow(r, ctx), 0, logonHost.RowStyles.Count - 1)
        Next
        logonHost.ResumeLayout(True)
    End Sub

    ' One logon row: the container's name and its task in words, and the same switch the manager's
    ' row carries - disabled with the reason beside it when the console would refuse it (an
    ' encrypted container is never mounted at logon automatically, and no credential is stored).
    Private Function LogonRow(r As DiskRecord, ctx As DiskContext) As Control
        Dim name = If(r.Name <> "", r.Name, r.BaseName)
        Dim row As New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 3,
            .Margin = PPad(0, 0, 0, 6)
        }
        row.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        row.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        row.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        row.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        Dim text As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = New Padding(0)
        }
        text.Controls.Add(NewLabel(name, P(240), strong:=True))
        text.Controls.Add(NewLabel(T(DiskAutostart.RowWordKey(r)), P(240), muted:=True))
        row.Controls.Add(text, 0, 0)

        Dim switchingOn = Not r.AutoMount
        Dim action = If(switchingOn, DiskAction.AutoOn, DiskAction.AutoOff)
        Dim why = DiskStates.WhyNot(action, r, DiskStates.StateOf(r, Nothing), ctx)
        Dim b = NewButton(T(DiskStates.LabelKey(action, Nothing)), DiskGlyphs.AutoMount)
        b.Enabled = (why = "")
        b.Margin = PPad(0, 0, P(8), 0)
        b.AccessibleName = b.Text & " - " & name
        AddHandler b.Click, Sub()
                                If performFn IsNot Nothing Then performFn(action, New List(Of DiskRecord) From {r})
                            End Sub
        row.Controls.Add(b, 1, 0)

        If why <> "" Then
            row.Controls.Add(NewLabel(T(why), P(220), muted:=True), 2, 0)
        Else
            row.Controls.Add(NewLabel("", P(10)), 2, 0)
        End If
        Return row
    End Function

    Private Sub FillGuardBlock()
        guardHost.SuspendLayout()
        For Each c As Control In guardHost.Controls.Cast(Of Control)().ToList()
            guardHost.Controls.Remove(c)
            c.Dispose()
        Next
        guardHost.RowStyles.Clear()
        Dim g = CurrentGuard()
        Dim isOn = g IsNot Nothing AndAlso g.Installed

        guardHost.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        guardHost.Controls.Add(NewLabel(T(DiskAutostart.GuardWordKey(g)), contentWidth, strong:=True), 0, guardHost.RowStyles.Count - 1)

        Dim switch = NewButton(T(If(isOn, "vd_mgr_btn_turn_off", "vd_mgr_btn_turn_on")), Nothing)
        switch.Margin = PPad(0, 4, 8, 6)
        switch.AccessibleName = switch.Text & " - " & T("vd_auto_name_guard")
        If isOn Then
            AddHandler switch.Click, Sub() AskGuard(False)
        Else
            AddHandler switch.Click, Sub() AskGuard(True)
        End If
        Dim actions As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = False,
            .Margin = New Padding(0)
        }
        actions.Controls.Add(switch)
        guardHost.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        guardHost.Controls.Add(actions, 0, guardHost.RowStyles.Count - 1)

        Dim runText = String.Join(Environment.NewLine, DiskAutostart.GuardRunLines(g, dict).ToArray())
        Dim runLabel As New List(Of Label)
        Dim first = True
        For Each line In runText.Split(New String() {Environment.NewLine}, StringSplitOptions.None)
            Dim l = NewLabel(line, contentWidth, muted:=Not first)
            If first Then first = False
            guardHost.RowStyles.Add(New RowStyle(SizeType.AutoSize))
            guardHost.Controls.Add(l, 0, guardHost.RowStyles.Count - 1)
        Next
        guardHost.ResumeLayout(True)
    End Sub

    ' The switch asks first (the window asks before it does anything); the manager's own flow runs
    ' the command line and reports how it ended.
    Private Sub AskGuard(turnOn As Boolean)
        Dim titleKey = If(turnOn, "vd_auto_switch_on_title", "vd_auto_switch_off_title")
        Dim textKey = If(turnOn, "vd_auto_switch_on_text", "vd_auto_switch_off_text")
        Dim goKey = If(turnOn, "vd_mgr_btn_turn_on", "vd_mgr_btn_turn_off")
        If ShellDialog.Ask(Me, T(titleKey), T(textKey), New String() {T(goKey), T("shell_btn_cancel")}, 1, 0) <> 0 Then Return
        If guardRunFn IsNot Nothing Then guardRunFn(turnOn)
    End Sub

    ' Friend surface for the self-test: the texts the dialog shows right now, and the controls the
    ' matrix reads.
    Friend ReadOnly Property TextsForTest As List(Of String)
        Get
            Dim out As New List(Of String)
            For Each c In AllChildren(page)
                If (TypeOf c Is Label OrElse TypeOf c Is Button) AndAlso Not String.IsNullOrEmpty(c.Text) Then out.Add(c.Text)
            Next
            Return out
        End Get
    End Property

    Friend ReadOnly Property LogonRowCountForTest As Integer
        Get
            Return logonHost.Controls.Count
        End Get
    End Property

    ' The logon switches, in row order, with whether they are enabled - the half of the state
    ' matrix the pure words cannot see.
    Friend Function LogonSwitchesForTest() As List(Of KeyValuePair(Of String, Boolean))
        Dim out As New List(Of KeyValuePair(Of String, Boolean))
        For Each row As Control In logonHost.Controls
            Dim b = row.Controls.OfType(Of GlyphButton)().FirstOrDefault()
            If b Is Nothing Then Continue For
            out.Add(New KeyValuePair(Of String, Boolean)(b.AccessibleName, b.Enabled))
        Next
        Return out
    End Function

    Friend Function GuardSwitchForTest() As GlyphButton
        For Each row As Control In guardHost.Controls
            Dim b = row.Controls.OfType(Of GlyphButton)().FirstOrDefault()
            If b IsNot Nothing Then Return b
        Next
        Return Nothing
    End Function

End Class
