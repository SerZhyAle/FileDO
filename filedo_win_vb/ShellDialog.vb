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
Public Class ShellDialog
    Inherits Form

    Private ReadOnly buttons As New List(Of Button)
    Private ReadOnly message As Label
    Private ReadOnly cancelIndex As Integer
    Private chosen As Integer

    ' Asks. Returns the index of the button chosen; Escape, the close box and Alt+F4 return
    ' cancelAt, which must be the answer that changes nothing. defaultAt is the button Enter
    ' presses and the one that has the focus; dangerAt, when given, is drawn in the danger colour.
    Public Shared Function Ask(owner As IWin32Window, title As String, text As String, choices As String(),
                               cancelAt As Integer, Optional defaultAt As Integer = -1,
                               Optional dangerAt As Integer = -1) As Integer
        Using dlg As New ShellDialog(title, text, choices, cancelAt, If(defaultAt < 0, cancelAt, defaultAt), dangerAt)
            If owner Is Nothing Then dlg.StartPosition = FormStartPosition.CenterScreen
            dlg.ShowDialog(owner)
            Return dlg.chosen
        End Using
    End Function

    ' Asks a question that is data (see DialogSpec) - the form every confirmation of a destructive
    ' action takes, so its default can be proven without showing it.
    Public Shared Function Ask(owner As IWin32Window, spec As DialogSpec) As Integer
        Return Ask(owner, spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt)
    End Function

    ' A notice: one button, and it is the no-action answer too.
    Public Shared Sub Notice(owner As IWin32Window, title As String, text As String)
        Ask(owner, title, text, New String() {Localization.T("shell_btn_close")}, 0)
    End Sub

    ' A failure, as the canon wants it shown: a named cause and at least one thing to do about it,
    ' never the exception's own text (APP-BEHAVIOUR rule 6). The exception has already gone to the
    ' log by the time this is called. Returns True when the user chose the extra action - try again,
    ' open the folder, copy the address - which the caller then performs.
    Public Shared Function Problem(owner As IWin32Window, text As String, Optional actionText As String = Nothing,
                                   Optional offerLogs As Boolean = True) As Boolean
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

        Dim pick = Ask(owner, Localization.T("shell_problem_title"), body, choices.ToArray(), closeAt,
                       If(String.IsNullOrEmpty(actionText), closeAt, 0))
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
                    defaultAt As Integer, dangerAt As Integer)
        cancelIndex = cancelAt
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
            .Margin = Ui.PxPad(Me, 0, 0, 0, 16)
        }
        root.Controls.Add(message, 0, 0)

        Dim row As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Anchor = AnchorStyles.Right,
            .Margin = New Padding(0)
        }
        For i = 0 To choices.Length - 1
            Dim index = i
            Dim b As New Button With {
                .Text = choices(i),
                .AutoSize = True,
                .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                .Padding = Ui.PxPad(Me, 10, 4, 10, 4),
                .Margin = Ui.PxPad(Me, 8, 0, 0, 0),
                .Tag = i
            }
            AddHandler b.Click, Sub()
                                    chosen = index
                                    DialogResult = DialogResult.OK
                                    Close()
                                End Sub
            buttons.Add(b)
            row.Controls.Add(b)
        Next
        root.Controls.Add(row, 0, 1)
        Controls.Add(root)

        If cancelAt >= 0 AndAlso cancelAt < buttons.Count Then CancelButton = buttons(cancelAt)
        If defaultAt >= 0 AndAlso defaultAt < buttons.Count Then
            AcceptButton = buttons(defaultAt)
            ActiveControl = buttons(defaultAt)
        End If

        ApplyTheme(defaultAt, dangerAt)
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

    Private Sub ApplyTheme(defaultAt As Integer, dangerAt As Integer)
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text
        message.ForeColor = p.Text
        message.Font = Theme.FontBody()
        For i = 0 To buttons.Count - 1
            Dim b = buttons(i)
            b.Font = If(i = defaultAt, Theme.FontBodyStrong(), Theme.FontBody())
            If i = dangerAt Then
                Ui.StyleButton(b, p.Danger, p.AccentText, p.Danger)
            ElseIf i = defaultAt AndAlso dangerAt < 0 Then
                ' Beside a danger button the safe answer keeps the ordinary face and only holds the
                ' focus (APP-STYLE section 4): a colour must not make the dangerous answer the easy one.
                Ui.StyleButton(b, p.Accent, p.AccentText, p.Accent)
            Else
                Ui.StyleButton(b, p.SurfaceAlt, p.Text, p.Border)
            End If
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
        Using dlg As New ShellDialog(spec.Title, spec.Text, spec.Choices, spec.CancelAt, spec.DefaultAt, spec.DangerAt)
            Dim a = TryCast(dlg.AcceptButton, Button)
            If a Is Nothing OrElse Not ReferenceEquals(a, dlg.ActiveControl) Then Return ""
            Return a.Text
        End Using
    End Function

End Class

' One question as data: what a dialog offers and which answer is which. A confirmation of a
' destructive action is built only by Destructive, so that its safe answer is the one Enter, Escape
' and the close box all give, and its acting answer is only painted danger (APP-BEHAVIOUR rule 5,
' APP-STYLE section 4) - a property of the builder, not of each call site.
Public NotInheritable Class DialogSpec
    Public ReadOnly Title As String
    Public ReadOnly Text As String
    Public ReadOnly Choices As String()
    Public ReadOnly CancelAt As Integer
    Public ReadOnly DefaultAt As Integer
    Public ReadOnly DangerAt As Integer

    Private Sub New(title As String, text As String, choices As String(), cancelAt As Integer, defaultAt As Integer, dangerAt As Integer)
        Me.Title = title
        Me.Text = text
        Me.Choices = choices
        Me.CancelAt = cancelAt
        Me.DefaultAt = defaultAt
        Me.DangerAt = dangerAt
    End Sub

    ' safeAt is the answer that changes nothing; dangerAt the one that does the destructive thing.
    Public Shared Function Destructive(title As String, text As String, choices As String(),
                                       safeAt As Integer, dangerAt As Integer) As DialogSpec
        Return New DialogSpec(title, text, choices, safeAt, safeAt, dangerAt)
    End Function
End Class

' Every confirmation of a destructive action the shell asks, as a spec built from a language's
' dictionary: the call sites ask these, and --selftest builds each of them in every language to
' prove the safe answer is the default (dialog:destructive-default-safe:*).
Public Module DestructiveDialogs

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
                                      New String() {T(d, "shell_btn_clean_remove"), T(d, "shell_btn_cancel")}, 1, 0)
    End Function

    ' Closing the shell while a run is active: Stop and close ends the run.
    Public Function StopAndClose(d As Dictionary(Of String, String), runName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "shell_close_running_title"),
                                      Localization.Format(TM(d, "shell_close_running"), runName),
                                      New String() {T(d, "shell_btn_stop_close"), T(d, "shell_btn_keep_running")}, 1, 0)
    End Function

    ' The second close, after Stop was asked and the run did not end: End it now kills the process.
    Public Function EndRunNow(d As Dictionary(Of String, String), runName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "shell_close_running_title"),
                                      Localization.Format(TM(d, "shell_close_still_running"), runName),
                                      New String() {T(d, "shell_btn_end_now"), T(d, "shell_btn_keep_waiting")}, 1, 0)
    End Function

    ' The Disk Manager closed with work running or queued: Wait is the safe answer, Stop and close
    ' stops it, and Finish in the background is offered only when nothing queued needs consent.
    Public Function CloseDiskManagerBusy(d As Dictionary(Of String, String), names As String, background As Boolean) As DialogSpec
        Dim choices As New List(Of String) From {T(d, "vd_mgr_btn_wait"), T(d, "vd_mgr_btn_stop_close")}
        If background Then choices.Add(T(d, "vd_mgr_btn_finish_bg"))
        Return DialogSpec.Destructive(T(d, "vd_mgr_close_running_title"),
                                      Localization.Format(TM(d, "vd_mgr_close_running_fmt"), names), choices.ToArray(), 0, 1)
    End Function

    ' Removing a container from the Disk Manager's list (its files stay).
    Public Function ForgetDisks(d As Dictionary(Of String, String), names As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_forget_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_forget_fmt"), names),
                                      New String() {T(d, "vd_mgr_btn_remove"), T(d, "shell_btn_cancel")}, 1, 0)
    End Function

    ' Unmounting a ram disk that holds data not yet saved to its file.
    Public Function UnmountDirtyRam(d As Dictionary(Of String, String), diskName As String, dirtyText As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_unmount_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_unmount_ram_fmt"), diskName, dirtyText),
                                      New String() {T(d, "vd_mgr_btn_unmount"), T(d, "shell_btn_cancel")}, 1, 0)
    End Function

    ' Mounting a container that was not closed cleanly: Windows checks the volume and may repair it.
    Public Function MountUnclean(d As Dictionary(Of String, String), diskName As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_mgr_confirm_unclean_title"),
                                      Localization.Format(TM(d, "vd_mgr_confirm_unclean_fmt"), diskName),
                                      New String() {T(d, "vd_mgr_btn_mount"), T(d, "shell_btn_cancel")}, 1, 0)
    End Function

    ' Creating a partition disk (SP-0148 4.2): a new partition in free space on a real disk. Nothing
    ' is destroyed, but the disk's layout changes and administrator consent follows - so Cancel is the
    ' default and Create is painted as the answer that acts.
    Public Function CreatePartition(d As Dictionary(Of String, String), text As String) As DialogSpec
        Return DialogSpec.Destructive(T(d, "vd_part_confirm_title"), Localization.Multiline(text),
                                      New String() {T(d, "vd_part_btn_create_go"), T(d, "shell_btn_cancel")}, 1, 0)
    End Function

    ' Every one of them, for the self-test: id and a spec built from the dictionary.
    Friend Function AllForTest(d As Dictionary(Of String, String)) As List(Of KeyValuePair(Of String, DialogSpec))
        Dim out As New List(Of KeyValuePair(Of String, DialogSpec))
        out.Add(New KeyValuePair(Of String, DialogSpec)("clean", CleanTestFiles(d, "D:\Test")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("stop-and-close", StopAndClose(d, "Wipe")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("end-run-now", EndRunNow(d, "Wipe")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("disk-manager-busy", CloseDiskManagerBusy(d, "Mount - Test", True)))
        out.Add(New KeyValuePair(Of String, DialogSpec)("disk-manager-busy-no-background", CloseDiskManagerBusy(d, "Mount - Test", False)))
        out.Add(New KeyValuePair(Of String, DialogSpec)("forget-disks", ForgetDisks(d, "test")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("unmount-dirty-ram", UnmountDirtyRam(d, "test", "12 MB")))
        out.Add(New KeyValuePair(Of String, DialogSpec)("mount-unclean", MountUnclean(d, "test")))
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
