' Every question and every notice the shell puts in front of the user.
'
' APP-BEHAVIOUR rule 1: a dialog has exactly one answer that does nothing, it is modal to the window
' that asked, owned by it and centred on it - and Escape and the close box both give that answer. A
' Win32 message box cannot promise the last part: one with Yes and No has no Cancel, so it has no
' close box and ignores Escape. This form is the shell's replacement, and nothing in the shell calls
' MessageBox.Show (cmd/filedo's surfaces test holds that).
'
' It is themed from the palette like every page, so a question is not the one light rectangle on a
' dark screen (APP-STYLE section 5), and it grows with its text instead of cutting it (rule 2).
'
' The answer buttons carry a glyph and a colour chosen by the answer's polarity (WINDOWS-UI 0.4
' section 11): the answer Escape and the close box give (cancelAt) is the dismissing one - action.cancel
' in state.error over the tinted face, or the tint taken away when the dialog confirms something
' irreversible, where the acting button alone keeps the danger face (item 7). The answer Enter
' confirms (defaultAt) is the affirmative one - the verb's own glyph where the caller passes one,
' action.confirm otherwise - in state.ok. A lone Close, which is a pure exit and not an answer to
' anything, keeps the mono look with nav.close. Everything the caller passes in glyphs() is the
' meaning's own drawing from the vendored vocabulary, never a private picture.
Public Class ShellDialog
    Inherits Form

    ' The meanings the dialog itself answers with, from the vendored vocabulary: every dismissing
    ' answer is action.cancel, a lone pure exit is nav.close, and the offered log report is
    ' action.email (the mail composer LogSender opens).
    Private Shared ReadOnly CancelGlyph As GlyphRef = GlyphRef.Vocabulary("action.cancel")
    Private Shared ReadOnly NavCloseGlyph As GlyphRef = GlyphRef.Vocabulary("nav.close")
    Private Shared ReadOnly SendLogsGlyph As GlyphRef = GlyphRef.Vocabulary("action.email")

    Private ReadOnly buttons As New List(Of GlyphButton)
    Private ReadOnly glyphs As GlyphRef()
    Private ReadOnly dangerIndex As Integer
    Private ReadOnly defaultIndex As Integer
    Private ReadOnly message As Label
    Private ReadOnly answerRow As FlowLayoutPanel
    Private ReadOnly cancelIndex As Integer
    Private chosen As Integer

    ' The face of the answer at an index, from its polarity alone - the same rule for every dialog.
    ' A single-button dialog has no answers at all: its Close is a pure exit and keeps the mono look.
    Private Function FaceAt(index As Integer) As GlyphButtonFace
        Return FaceOf(buttons.Count, index, cancelIndex, defaultIndex, dangerIndex)
    End Function

    ' The polarity rule the self-test holds every dialog to (WINDOWS-UI 0.4 section 11): the answer
    ' Escape gives is the dismissing one (the tint taken away beside a danger answer), the acting
    ' answer keeps the danger face, the answer Enter confirms is the affirmative one, and a lone
    ' pure exit is no answer at all - the mono look.
    Friend Shared Function FaceOf(total As Integer, index As Integer, cancelAt As Integer,
                                  defaultAt As Integer, dangerAt As Integer) As GlyphButtonFace
        If total = 1 Then Return GlyphButtonFace.Normal
        If index = dangerAt Then Return GlyphButtonFace.DangerFill
        ' BUTTONS 2.0 section 3 rule 5: Cancel, No and a plain Close are neutral - no red glyph, no tint.
        If index = cancelAt Then Return GlyphButtonFace.Normal
        If index = defaultAt AndAlso dangerAt < 0 Then Return GlyphButtonFace.AnswerOk
        Return GlyphButtonFace.Normal
    End Function

    ' The order the answers are laid out in (BUTTONS 2.0 section 4 rule 4): reading order is Cancel, any
    ' middle answer (Apply), then the final affirmative - the destructive answer of a destructive
    ' confirmation. The indices every caller passes stay as they are (the Go sweep over the call sites
    ' reads them positionally and `chosen` is still the caller's index); only the left-to-right place of
    ' each button, and so the Tab order, follows this list.
    Friend Shared Function AnswerOrder(total As Integer, cancelAt As Integer,
                                       defaultAt As Integer, dangerAt As Integer) As Integer()
        Dim first = If(cancelAt >= 0 AndAlso cancelAt < total, cancelAt, -1)
        Dim last = -1
        If dangerAt >= 0 AndAlso dangerAt < total AndAlso dangerAt <> first Then
            last = dangerAt
        ElseIf defaultAt >= 0 AndAlso defaultAt < total AndAlso defaultAt <> first Then
            last = defaultAt
        End If
        Dim order As New List(Of Integer)
        If first >= 0 Then order.Add(first)
        For i = 0 To total - 1
            If i <> first AndAlso i <> last Then order.Add(i)
        Next
        If last >= 0 Then order.Add(last)
        Return order.ToArray()
    End Function

    ' The meaning drawn at an index, decided while the buttons are built: the caller's verb, the
    ' dismissing answer's action.cancel, or nav.close for the lone pure exit.
    Private Function GlyphAt(index As Integer, total As Integer) As GlyphRef
        If total = 1 Then Return NavCloseGlyph
        If index = cancelIndex Then Return CancelGlyph
        If glyphs IsNot Nothing AndAlso index >= 0 AndAlso index < glyphs.Length Then Return glyphs(index)
        Return Nothing
    End Function

    ' Asks. Returns the index of the button chosen; Escape, the close box and Alt+F4 return
    ' cancelAt, which must be the answer that changes nothing. defaultAt is the button Enter
    ' presses and the one that has the focus; dangerAt, when given, is drawn in the danger colour.
    ' glyphs(i), when given, is the vocabulary meaning of the verb answer i names.
    Public Shared Function Ask(owner As IWin32Window, title As String, text As String, choices As String(),
                               cancelAt As Integer, Optional defaultAt As Integer = -1,
                               Optional dangerAt As Integer = -1, Optional glyphs As GlyphRef() = Nothing) As Integer
        Using dlg As New ShellDialog(title, text, choices, cancelAt, If(defaultAt < 0, cancelAt, defaultAt), dangerAt, glyphs)
            If owner Is Nothing Then dlg.StartPosition = FormStartPosition.CenterScreen
            dlg.ShowDialog(owner)
            Return dlg.chosen
        End Using
    End Function

    ' Asks a question that is data (see DialogSpec) - the form every confirmation of a destructive
    ' action takes, so its default can be proven without showing it.
    Public Shared Function Ask(owner As IWin32Window, spec As DialogSpec) As Integer
        Return Ask(owner, spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt, spec.Glyphs)
    End Function

    ' A plain question that names its answers' glyphs calls this form with the answers' meanings
    ' named: Ask(..., cancelAt, defaultAt, glyphs:=New GlyphRef() {...}). Nothing lands in the danger
    ' slot, which the Go sweep over these calls reads positionally - a written -1 there would read as
    ' a named danger button (cmd/filedo's TestShell_DestructiveDialogDefaultsToTheSafeAnswer).

    ' A notice: one button, and it is the no-action answer too. It is not an answer to a question,
    ' so it keeps the mono look and nav.close.
    Public Shared Sub Notice(owner As IWin32Window, title As String, text As String)
        Ask(owner, title, text, New String() {Localization.T("shell_btn_close")}, 0)
    End Sub

    ' A failure, as the canon wants it shown: a named cause and at least one thing to do about it,
    ' never the exception's own text (APP-BEHAVIOUR rule 6). The exception has already gone to the
    ' log by the time this is called. Returns True when the user chose the extra action - try again,
    ' open the folder, copy the address - which the caller then performs. actionGlyph is that
    ' action's meaning (Retry - action.retry, Copy the path - action.copy).
    Public Shared Function Problem(owner As IWin32Window, text As String, Optional actionText As String = Nothing,
                                   Optional offerLogs As Boolean = True, Optional actionGlyph As GlyphRef = Nothing) As Boolean
        Dim choices As New List(Of String)
        If Not String.IsNullOrEmpty(actionText) Then choices.Add(actionText)
        Dim logsAt = -1
        If offerLogs Then
            logsAt = choices.Count
            choices.Add(Localization.T("shell_btn_send_logs"))
        End If
        choices.Add(Localization.T("shell_btn_close"))
        Dim closeAt = choices.Count - 1

        Dim body = text
        If offerLogs Then body &= Environment.NewLine & Environment.NewLine & Localization.T("shell_problem_logged")

        Dim glyphs As GlyphRef() = Nothing
        If choices.Count > 1 Then
            glyphs = New GlyphRef(choices.Count - 1) {}
            If Not String.IsNullOrEmpty(actionText) Then glyphs(0) = actionGlyph
            glyphs(logsAt) = SendLogsGlyph
        End If

        Dim pick = Ask(owner, Localization.T("shell_problem_title"), body, choices.ToArray(), closeAt,
                       If(String.IsNullOrEmpty(actionText), closeAt, 0), -1, glyphs)
        If pick = logsAt Then
            LogSender.Run(owner)
            Return False
        End If
        Return (Not String.IsNullOrEmpty(actionText)) AndAlso pick = 0
    End Function

    ' The owner as a control's form, or Nothing when the control is not on one (the self-test builds
    ' pages that are never shown).
    Public Shared Function OwnerOf(c As Control) As IWin32Window
        If c Is Nothing Then Return Nothing
        Return c.FindForm()
    End Function

    Private Sub New(title As String, text As String, choices As String(), cancelAt As Integer,
                    defaultAt As Integer, dangerAt As Integer, Optional glyphs As GlyphRef() = Nothing)
        cancelIndex = cancelAt
        dangerIndex = dangerAt
        defaultIndex = If(defaultAt < 0, cancelAt, defaultAt)
        Me.glyphs = glyphs
        chosen = cancelAt

        SuspendLayout()
        Me.Text = title
        FormBorderStyle = FormBorderStyle.FixedDialog
        MinimizeBox = False
        MaximizeBox = False
        ShowInTaskbar = False
        ShowIcon = False
        KeyPreview = True
        StartPosition = FormStartPosition.CenterParent
        AutoScaleMode = AutoScaleMode.Font
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        Font = Theme.FontBody()

        Dim root As New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 2,
            .Dock = DockStyle.Fill,
            .Padding = Ui.PxPad(Me, 20, 18, 20, 14),
            .Margin = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        message = New Label With {
            .Text = text,
            .AutoSize = True,
            .MaximumSize = New Size(Ui.Px(Me, 520), 0),
            .MinimumSize = New Size(Ui.Px(Me, 300), 0),
            .Margin = New Padding(0, 0, 0, Ui.Px(Me, UiTokens.GroupGap() - UiTokens.ButtonGap()))
        }
        root.Controls.Add(message, 0, 0)

        ' The answer row never makes the dialog wider than its own text budget (the message's 520 px
        ' plus the root's side padding): when the translated captions do not fit one line, the row
        ' wraps whole buttons onto the next line and no caption is ever cut (WINDOWS-UI 0.4 section
        ' 11 item 3, APP-BEHAVIOUR rule 2).
        Dim row As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Anchor = AnchorStyles.Right,
            .MaximumSize = New Size(Ui.Px(Me, 560), 0),
            .Margin = New Padding(0)
        }
        answerRow = row
        Dim gap = Ui.Px(Me, UiTokens.ButtonGap())
        For i = 0 To choices.Length - 1
            Dim index = i
            ' The gap token sits left of and above each button: between answers and between wrapped lines.
            Dim b As New GlyphButton With {
                .Text = choices(i),
                .Glyph = GlyphAt(i, choices.Length),
                .AutoSize = True,
                .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                .MaximumSize = New Size(Ui.Px(Me, 560), 0),
                .Margin = New Padding(gap, gap, 0, 0),
                .Tag = index
            }
            AddHandler b.Click, Sub()
                                    chosen = index
                                    DialogResult = DialogResult.OK
                                    Close()
                                End Sub
            buttons.Add(b)
        Next
        ' Left to right: Cancel, the middle answers, the final affirmative (see AnswerOrder).
        For Each i In AnswerOrder(choices.Length, cancelAt, defaultIndex, dangerAt)
            row.Controls.Add(buttons(i))
        Next
        ' Every answer in one visual row has the height of that row's tallest (BUTTONS section 3 rule 4),
        ' recalculated when the row is laid out again (a wrapped line, a font or a dpi change).
        AddHandler row.SizeChanged, Sub() Ui.EqualizeRowHeights(row)
        root.Controls.Add(row, 0, 1)
        Controls.Add(root)

        If cancelAt >= 0 AndAlso cancelAt < buttons.Count Then CancelButton = buttons(cancelAt)
        If defaultIndex >= 0 AndAlso defaultIndex < buttons.Count Then
            AcceptButton = buttons(defaultIndex)
            ActiveControl = buttons(defaultIndex)
        End If

        ApplyTheme()
        Theme.Watch(Me, AddressOf ApplyTheme)
        ResumeLayout(True)
    End Sub

    ' Whatever closed the dialog other than a button - Escape through CancelButton, the close box,
    ' Alt+F4 - leaves chosen at the cancel answer it was set to in the constructor.
    Protected Overrides Sub OnFormClosing(e As FormClosingEventArgs)
        If DialogResult <> DialogResult.OK Then chosen = cancelIndex
        MyBase.OnFormClosing(e)
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
    End Sub

    Private Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text
        message.ForeColor = p.Text
        message.Font = Theme.FontBody()
        For i = 0 To buttons.Count - 1
            Dim b = buttons(i)
            b.Font = If(i = defaultIndex, Theme.FontBodyStrong(), Theme.FontBody())
            b.Face = FaceAt(i)
        Next
    End Sub

    ' For the self-test: the buttons a dialog would offer, and which one Escape presses.
    Friend Shared Function CancelOf(choices As String(), cancelAt As Integer) As String
        Using dlg As New ShellDialog("t", "t", choices, cancelAt, cancelAt, -1)
            Dim b = TryCast(dlg.CancelButton, Button)
            Return If(b Is Nothing, "", b.Text)
        End Using
    End Function

    ' For the self-test: the button Enter presses in a dialog built from a spec, which must also be
    ' the one that has the focus ("" when the two differ or there is none).
    Friend Shared Function DefaultOf(spec As DialogSpec) As String
        Using dlg As New ShellDialog(spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt, spec.Glyphs)
            Dim a = TryCast(dlg.AcceptButton, Button)
            If a Is Nothing OrElse Not ReferenceEquals(a, dlg.ActiveControl) Then Return ""
            Return a.Text
        End Using
    End Function

    ' For the self-test: "face:meaning" per answer of a spec, in order - the polarity reading the
    ' dialog would give each button, and the meaning drawn on it ("" where no glyph is drawn).
    ' LastAnswerRowWidth then holds the answer row's width in logical px after layout (WINDOWS-UI
    ' section 11 item 3: the narrow buttons of one row wrap, never clip).
    Friend Shared Function AnswersOf(spec As DialogSpec) As String()
        Using dlg As New ShellDialog(spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt, spec.Glyphs)
            dlg.PerformLayout()
            lastRowWidth = CInt(Math.Round(dlg.answerRow.Width * 96.0 / dlg.DeviceDpi))
            Return dlg.buttons.Select(Function(b) b.Face.ToString().ToLowerInvariant() & ":" &
                                      If(b.Glyph Is Nothing, "", b.Glyph.Meaning)).ToArray()
        End Using
    End Function

    ' For the self-test: the answers' captions in the order the row lays them out, left to right.
    Friend Shared Function DisplayedOrderOf(spec As DialogSpec) As String()
        Using dlg As New ShellDialog(spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt, spec.Glyphs)
            Return dlg.answerRow.Controls.Cast(Of Control)().Select(Function(c) c.Text).ToArray()
        End Using
    End Function

    ' For the self-test: the width the last AnswersOf measured, in logical pixels.
    Friend Shared Function LastAnswerRowWidth() As Integer
        Return lastRowWidth
    End Function

    Private Shared lastRowWidth As Integer = 0

    ' For the self-test and the driven run: a dialog built from a spec but not shown - the caller
    ' shows it, shoots it and disposes it.
    Friend Shared Function ForTest(spec As DialogSpec) As ShellDialog
        Return New ShellDialog(spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt, spec.Glyphs)
    End Function

End Class

' One question as data: what a dialog offers and which answer is which. A confirmation of a
' destructive action is built only by Destructive, so that its safe answer is the one Enter, Escape
' and the close box all give, and its acting answer is only painted danger (APP-BEHAVIOUR rule 5,
' APP-STYLE section 4) - a property of the builder, not of each call site. Glyphs, when given, name
' each answer's vocabulary meaning (WINDOWS-UI 0.4 section 11 item 4: the verb's own glyph).
Public NotInheritable Class DialogSpec
    Public ReadOnly Title As String
    Public ReadOnly Text As String
    Public ReadOnly Choices As String()
    Public ReadOnly CancelAt As Integer
    Public ReadOnly DefaultAt As Integer
    Public ReadOnly DangerAt As Integer
    Public ReadOnly Glyphs As GlyphRef()

    Private Sub New(title As String, text As String, choices As String(), cancelAt As Integer,
                    defaultAt As Integer, dangerAt As Integer, Optional glyphs As GlyphRef() = Nothing)
        Me.Title = title
        Me.Text = text
        Me.Choices = choices
        Me.CancelAt = cancelAt
        Me.DefaultAt = defaultAt
        Me.DangerAt = dangerAt
        Me.Glyphs = glyphs
    End Sub

    ' safeAt is the answer that changes nothing; dangerAt the one that does the destructive thing.
    Public Shared Function Destructive(title As String, text As String, choices As String(),
                                       safeAt As Integer, dangerAt As Integer,
                                       Optional glyphs As GlyphRef() = Nothing) As DialogSpec
        Return New DialogSpec(title, text, choices, safeAt, safeAt, dangerAt, glyphs)
    End Function
End Class

' Every confirmation of a destructive action the shell asks, as a spec built from a language's
' dictionary: the call sites ask these, and --selftest builds each of them in every language to
' prove the safe answer is the default (dialog:destructive-default-safe:*).
Public Module DestructiveDialogs

    ' The verb each answer names, from the vendored vocabulary (WINDOWS-UI 0.4 section 11 item 4:
    ' the acting answer keeps its verb's glyph, the dismissing answer keeps action.cancel). Ending a
    ' running task is action.cancel, not media.stop (ICON-SET 0.14 note d).
    Private ReadOnly GoCancel As GlyphRef = GlyphRef.Vocabulary("action.cancel")

    Private Function T(d As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If d IsNot Nothing AndAlso d.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function TM(d As Dictionary(Of String, String), key As String) As String
        Return Localization.Multiline(T(d, key))
    End Function

    ' Clean: removes the test files FileDO wrote in a folder.
    Public Function CleanTestFiles(d As Dictionary(Of String, String), target As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "shell_clean_confirm_title"),
                                      Localization.Format(T(d, "shell_clean_confirm_fmt"), target),
                                      New String() {T(d, "shell_btn_clean_remove"), T(d, "shell_btn_cancel")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.delete"), GoCancel})
    End Function

    ' Test files FileDO wrote earlier are still in the redirect folder (SP-0194): the acting answer
    ' only opens the page that deletes them, where the plan is shown and Run is the second step, and
    ' Keep them - what Enter, Escape and the close box all give - leaves every file where it is.
    Public Function LeftoverTestFiles(d As Dictionary(Of String, String), folder As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "shell_leftover_title"),
                                      Localization.Format(T(d, "shell_leftover_fmt"), folder),
                                      New String() {T(d, "shell_btn_leftover_open"), T(d, "shell_btn_leftover_keep")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.delete"), GoCancel})
    End Function

    ' Closing the shell while a run is active: Stop and close ends the run.
    Public Function StopAndClose(d As Dictionary(Of String, String), runName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "shell_close_running_title"),
                                      Localization.Format(TM(d, "shell_close_running"), runName),
                                      New String() {T(d, "shell_btn_stop_close"), T(d, "shell_btn_keep_running")}, 1, 0,
                                      New GlyphRef() {GoCancel, GoCancel})
    End Function

    ' The second close, after Stop was asked and the run did not end: End it now kills the process.
    Public Function EndRunNow(d As Dictionary(Of String, String), runName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "shell_close_running_title"),
                                      Localization.Format(TM(d, "shell_close_still_running"), runName),
                                      New String() {T(d, "shell_btn_end_now"), T(d, "shell_btn_keep_waiting")}, 1, 0,
                                      New GlyphRef() {GoCancel, GoCancel})
    End Function

    ' The Disk Manager closed with work running or queued: Wait is the safe answer, Stop and close
    ' stops it, and Finish in the background is offered only when nothing queued needs consent.
    Public Function CloseDiskManagerBusy(d As Dictionary(Of String, String), names As String, background As Boolean) As DialogSpec
        Dim choices As New List(Of String) From {T(d, "vd_mgr_btn_wait"), T(d, "vd_mgr_btn_stop_close")}
        Dim glyphs As New List(Of GlyphRef) From {GoCancel, GoCancel}
        If background Then
            choices.Add(T(d, "vd_mgr_btn_finish_bg"))
            ' The window closes, the run goes on and nothing is lost - nav.close's own meaning.
            glyphs.Add(GlyphRef.Vocabulary("nav.close"))
        End If
        Return DialogSpec.Destructive(T(d, "vd_mgr_close_running_title"),
                                      Localization.Format(TM(d, "vd_mgr_close_running_fmt"), names), choices.ToArray(), 0, 1,
                                      glyphs.ToArray())
    End Function

    ' Removing a container from the Disk Manager's list (its files stay).
    Public Function ForgetDisks(d As Dictionary(Of String, String), names As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_forget_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_forget_fmt"), names),
                                      New String() {T(d, "vd_mgr_btn_remove"), T(d, "shell_btn_cancel")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.remove"), GoCancel})
    End Function

    ' Unmounting a ram disk that holds data not yet saved to its file.
    Public Function UnmountDirtyRam(d As Dictionary(Of String, String), diskName As String, dirtyText As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_unmount_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_unmount_ram_fmt"), diskName, dirtyText),
                                      New String() {T(d, "vd_mgr_btn_unmount"), T(d, "shell_btn_cancel")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.unmount-disk"), GoCancel})
    End Function

    ' Mounting a container that was not closed cleanly: Windows checks the volume and may repair it.
    Public Function MountUnclean(d As Dictionary(Of String, String), diskName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_unclean_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_unclean_fmt"), diskName),
                                      New String() {T(d, "vd_mgr_btn_mount"), T(d, "shell_btn_cancel")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.mount-disk"), GoCancel})
    End Function

    ' Repairing the volume of a container that was not closed cleanly: chkdsk /f, which writes to the
    ' volume and can drop what it cannot recover. Cancel is the default.
    Public Function RepairVolume(d As Dictionary(Of String, String), diskName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_repair_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_repair_fmt"), diskName),
                                      New String() {T(d, "vd_mgr_btn_repair"), T(d, "shell_btn_cancel")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.recover-drive"), GoCancel})
    End Function

    ' Creating a partition disk (SP-0148 4.2): a new partition in free space on a real disk. Nothing
    ' is destroyed, but the disk's layout changes and administrator consent follows - so Cancel is the
    ' default and Create is painted as the answer that acts.
    Public Function CreatePartition(d As Dictionary(Of String, String), text As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_part_confirm_title"), Localization.Multiline(text),
                                      New String() {T(d, "vd_part_btn_create_go"), T(d, "shell_btn_cancel")}, 1, 0,
                                      New GlyphRef() {GlyphRef.Vocabulary("action.create-disk"), GoCancel})
    End Function

    ' Every one of them, for the self-test: id and a spec built from the dictionary.
    Friend Function AllForTest(d As Dictionary(Of String, String)) As List(Of KeyValuePair(Of String, DialogSpec))
        Dim out As New List(Of KeyValuePair(Of String, DialogSpec))
        out.Add(New KeyValuePair(Of String, DialogSpec)("clean", CleanTestFiles(d, "D:\Test")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("leftover-test-files", LeftoverTestFiles(d, "C:\Temp\FileDO_Operations")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("stop-and-close", StopAndClose(d, "Wipe")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("end-run-now", EndRunNow(d, "Wipe")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("disk-manager-busy", CloseDiskManagerBusy(d, "Mount - Test", True)))
        out.Add(New KeyValuePair(Of String, DialogSpec)("disk-manager-busy-no-background", CloseDiskManagerBusy(d, "Mount - Test", False)))
        out.Add(New KeyValuePair(Of String, DialogSpec)("forget-disks", ForgetDisks(d, "test")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("unmount-dirty-ram", UnmountDirtyRam(d, "test", "12 MB")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("mount-unclean", MountUnclean(d, "test")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("repair-volume", RepairVolume(d, "test")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("create-partition", CreatePartition(d, "Disk 2")))
        Return out
    End Function

End Module

' Names the cause of a failure from the exception's TYPE, never from its message: the message is
' the platform's text in the platform's language, and it goes to the log (APP-BEHAVIOUR rule 6).
Module Problems

    ' CLIPBRD_E_CANT_OPEN, the one ExternalException that means "another program has it".
    Friend Const ClipboardCantOpen As Long = &H800401D0L

    Public Function CauseKey(ex As Exception) As String
        If ex Is Nothing Then Return "shell_cause_unexpected"

        Dim w32 = TryCast(ex, System.ComponentModel.Win32Exception)
        If w32 IsNot Nothing Then
            Select Case w32.NativeErrorCode
                Case 2, 3 : Return "shell_cause_missing"          ' file or path not found
                Case 5 : Return "shell_cause_access"             ' access denied
                Case 32, 33 : Return "shell_cause_busy"          ' sharing or lock violation
                Case 1155, 1156, 1157 : Return "shell_cause_no_program" ' no association
                Case Else : Return "shell_cause_unexpected"
            End Select
        End If

        If TypeOf ex Is UnauthorizedAccessException OrElse TypeOf ex Is Security.SecurityException Then
            Return "shell_cause_access"
        End If
        If TypeOf ex Is IO.FileNotFoundException OrElse TypeOf ex Is IO.DirectoryNotFoundException OrElse
           TypeOf ex Is IO.DriveNotFoundException Then
            Return "shell_cause_missing"
        End If
        If TypeOf ex Is IO.PathTooLongException Then Return "shell_cause_missing"
        If TypeOf ex Is IO.IOException Then
            Select Case ex.HResult And &HFFFF
                Case 32, 33 : Return "shell_cause_busy"
                Case 39, 112 : Return "shell_cause_disk_full"
                Case Else : Return "shell_cause_io"
            End Select
        End If
        ' The clipboard throws ExternalException with CLIPBRD_E_CANT_OPEN while another program holds
        ' it open - that code, and no other (SHELL-10). GDI+'s "generic error" and an SEHException
        ' are ExternalExceptions too, and neither means somebody else is using anything.
        If TypeOf ex Is Runtime.InteropServices.ExternalException AndAlso
           (CLng(ex.HResult) And &HFFFFFFFFL) = ClipboardCantOpen Then Return "shell_cause_busy"
        Return "shell_cause_unexpected"
    End Function

    Public Function Cause(ex As Exception) As String
        Return Localization.T(CauseKey(ex))
    End Function

End Module
