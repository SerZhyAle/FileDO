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
            ElseIf i = defaultAt Then
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

End Class

' Names the cause of a failure from the exception's TYPE, never from its message: the message is
' the platform's text in the platform's language, and it goes to the log (APP-BEHAVIOUR rule 6).
Module Problems

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
        ' The clipboard throws ExternalException while another program holds it open.
        If TypeOf ex Is Runtime.InteropServices.ExternalException Then Return "shell_cause_busy"
        Return "shell_cause_unexpected"
    End Function

    Public Function Cause(ex As Exception) As String
        Return Localization.T(CauseKey(ex))
    End Function

End Module
