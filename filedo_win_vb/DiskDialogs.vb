' The Disk Manager's three small dialogs (SP-0063 7.2), each one screen: the password, Mount as..
' and the name a container is added to the list under. Anything with more than three inputs is a
' job page, not a dialog.
'
' Each is owned by the manager, themed from the palette, and has the one answer that does nothing -
' Cancel, which Escape and the close box give (APP-BEHAVIOUR rule 1).
Public Class DiskSmallDialog
    Inherits Form

    Protected ReadOnly dict As Dictionary(Of String, String)
    Protected ReadOnly body As TableLayoutPanel
    Protected ReadOnly message As Label
    Protected ReadOnly okBtn As Button
    Protected ReadOnly cancelBtn As Button
    Private ReadOnly buttonRow As FlowLayoutPanel
    Protected ReadOnly notes As New List(Of Label)

    Protected Sub New(d As Dictionary(Of String, String), title As String, text As String, okKey As String, scaleBy As Control)
        dict = If(d, New Dictionary(Of String, String)())
        scaleOwner = scaleBy
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

        body = New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Dock = DockStyle.Fill,
            .Padding = PPad(20, 18, 20, 14),
            .Margin = New Padding(0)
        }
        body.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        message = NewNote(text)
        message.Margin = PPad(0, 0, 0, 12)
        AddContent(message)

        buttonRow = New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .Anchor = AnchorStyles.Right,
            .Margin = PPad(0, 12, 0, 0)
        }
        okBtn = New Button With {.Text = T(okKey), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                                 .Padding = PPad(10, 4, 10, 4), .Margin = PPad(8, 0, 0, 0),
                                 .DialogResult = DialogResult.OK}
        cancelBtn = New Button With {.Text = T("shell_btn_cancel"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                                     .Padding = PPad(10, 4, 10, 4), .Margin = PPad(8, 0, 0, 0),
                                     .DialogResult = DialogResult.Cancel}
        buttonRow.Controls.Add(okBtn)
        buttonRow.Controls.Add(cancelBtn)
        AcceptButton = okBtn
        CancelButton = cancelBtn
        Controls.Add(body)
    End Sub

    Protected Function T(key As String) As String
        Return Tr(dict, key)
    End Function

    ' Sizes in design pixels, taken at the owner's DPI: a dialog is built before it has a monitor of
    ' its own, and the window that opens it is already on the one it will open on.
    Protected ReadOnly scaleOwner As Control

    Protected Function P(designPixels As Integer) As Integer
        Return Ui.Px(If(scaleOwner, CType(Me, Control)), designPixels)
    End Function

    Protected Function PPad(l As Integer, t As Integer, r As Integer, b As Integer) As Padding
        Return Ui.PxPad(If(scaleOwner, CType(Me, Control)), l, t, r, b)
    End Function

    ' The same, for a subclass's call to this constructor, which runs before the instance exists.
    Protected Shared Function Tr(d As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If d IsNot Nothing AndAlso d.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

    Protected Function NewNote(text As String) As Label
        Dim l As New Label With {
            .Text = text,
            .UseMnemonic = False,
            .AutoSize = True,
            .MaximumSize = New Size(P(460), 0),
            .MinimumSize = New Size(P(320), 0),
            .Margin = PPad(0, 2, 0, 4)
        }
        notes.Add(l)
        Return l
    End Function

    Protected Sub AddContent(c As Control)
        body.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        body.Controls.Add(c, 0, body.RowStyles.Count - 1)
    End Sub

    ' The buttons go last, and the whole dialog is themed once its content is in.
    Protected Sub Finish()
        AddContent(buttonRow)
        ApplyTheme()
        Theme.Watch(Me, AddressOf ApplyTheme)
        ResumeLayout(True)
    End Sub

    Protected Sub SetAcceptable(ok As Boolean)
        okBtn.Enabled = ok
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
    End Sub

    Private Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text
        Ui.HitTargetFloor(Me)
        For Each c In AllChildren(Me)
            c.Font = Theme.FontBody()
            If TypeOf c Is Label Then
                c.ForeColor = If(notes.Contains(DirectCast(c, Label)) AndAlso c IsNot message, p.MutedText, p.Text)
            ElseIf TypeOf c Is TextBox Then
                c.BackColor = p.Surface
                c.ForeColor = p.Text
                DirectCast(c, TextBox).BorderStyle = BorderStyle.FixedSingle
            ElseIf TypeOf c Is ComboBox Then
                c.BackColor = p.Surface
                c.ForeColor = p.Text
                DirectCast(c, ComboBox).FlatStyle = FlatStyle.Flat
            ElseIf TypeOf c Is CheckBox Then
                c.ForeColor = p.Text
                c.BackColor = p.Surface
            End If
        Next
        okBtn.Font = Theme.FontBodyStrong()
        Ui.StyleButton(okBtn, p.Accent, p.AccentText, p.Accent)
        Ui.StyleButton(cancelBtn, p.SurfaceAlt, p.Text, p.Border)
    End Sub

    Private Iterator Function AllChildren(root As Control) As IEnumerable(Of Control)
        For Each c As Control In root.Controls
            Yield c
            For Each inner In AllChildren(c)
                Yield inner
            Next
        Next
    End Function

End Class

' The password of one encrypted container, for one run (spec 7.1): masked with a show toggle, the
' SP-0004 wording about what it protects, and nothing else. The dialog keeps no copy: the box is
' emptied the moment the password is taken, and the manager hands it to the child's environment
' alone as FILEDO_SHELL_CRED.
Public Class DiskPasswordDialog
    Inherits DiskSmallDialog

    Private ReadOnly box As TextBox
    Private ReadOnly showCheck As CheckBox
    Private captured As String = Nothing

    Public Sub New(d As Dictionary(Of String, String), diskName As String, owner As Control)
        MyBase.New(d, Tr(d, "vd_mgr_pw_title"),
                   Localization.Format(Tr(d, "vd_mgr_pw_text_fmt"), diskName), "vd_mgr_btn_continue", owner)
        Dim label As New Label With {.Text = T("shell_lbl_password"), .AutoSize = True, .Margin = PPad(0, 2, 0, 2)}
        box = New TextBox With {.Width = P(320), .UseSystemPasswordChar = True, .Margin = PPad(0, 0, 0, 4)}
        box.AccessibleName = T("shell_lbl_password")
        showCheck = New CheckBox With {.Text = T("shell_cred_show"), .AutoSize = True, .Margin = PPad(0, 0, 0, 4)}
        AddHandler showCheck.CheckedChanged, Sub() box.UseSystemPasswordChar = Not showCheck.Checked
        AddHandler box.TextChanged, Sub() SetAcceptable(box.Text <> "")
        AddHandler okBtn.Click, Sub()
                                    captured = box.Text
                                    box.Text = ""
                                End Sub
        AddContent(label)
        AddContent(box)
        AddContent(showCheck)
        AddContent(NewNote(T("vd_mgr_pw_note")))
        SetAcceptable(False)
        Finish()
        ActiveControl = box
    End Sub

    ' The password, once: a second call returns "".
    Public Function TakePassword() As String
        Dim p = If(captured, "")
        captured = Nothing
        Return p
    End Function

    Protected Overrides Sub OnFormClosed(e As FormClosedEventArgs)
        box.Text = ""
        MyBase.OnFormClosed(e)
    End Sub

End Class

' Mount as.. (spec 7.2): the letter from the free letters, and read-only.
Public Class DiskMountAsDialog
    Inherits DiskSmallDialog

    Private ReadOnly letterCombo As ComboBox
    Private ReadOnly roCheck As CheckBox

    Public Sub New(d As Dictionary(Of String, String), diskName As String, owner As Control)
        MyBase.New(d, Tr(d, "vd_mgr_act_mount_as"),
                   Localization.Format(Tr(d, "vd_mgr_mountas_text_fmt"), diskName), "vd_mgr_btn_mount", owner)
        Dim row As New FlowLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = New Padding(0)}
        Dim label As New Label With {.Text = T("vd_lbl_letter"), .AutoSize = True, .Margin = PPad(0, 6, 8, 0)}
        letterCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = P(160), .Margin = PPad(0, 2, 0, 2)}
        letterCombo.AccessibleName = T("vd_lbl_letter")
        letterCombo.Items.Add(T("vd_letter_auto"))
        For Each l In FreeLetters()
            letterCombo.Items.Add(l)
        Next
        letterCombo.SelectedIndex = 0
        row.Controls.Add(label)
        row.Controls.Add(letterCombo)
        roCheck = New CheckBox With {.Text = T("vd_opt_ro"), .AutoSize = True, .Margin = PPad(0, 6, 0, 2)}
        AddContent(row)
        AddContent(roCheck)
        Finish()
    End Sub

    ' The letters no drive holds now, D: to Z:.
    Friend Shared Function FreeLetters() As List(Of String)
        Dim used As New HashSet(Of Char)()
        Try
            For Each d In IO.DriveInfo.GetDrives()
                If d.Name.Length > 0 Then used.Add(Char.ToUpperInvariant(d.Name(0)))
            Next
        Catch ex As Exception
            ShellLog.Write("list drive letters", ex)
        End Try
        Dim out As New List(Of String)
        For c = AscW("D"c) To AscW("Z"c)
            If Not used.Contains(ChrW(c)) Then out.Add(ChrW(c) & ":")
        Next
        Return out
    End Function

    ' "" for the first free letter.
    Public ReadOnly Property Letter As String
        Get
            Return If(letterCombo.SelectedIndex > 0, CStr(letterCombo.SelectedItem), "")
        End Get
    End Property

    Public ReadOnly Property MountReadOnly As Boolean
        Get
            Return roCheck.Checked
        End Get
    End Property

End Class

' The name a container is added to the list under (spec 7.2): validated as `vd add .. as <name>`
' validates it, and never one the list holds already.
Public Class DiskNameDialog
    Inherits DiskSmallDialog

    Private ReadOnly box As TextBox
    Private ReadOnly verdict As Label
    Private ReadOnly taken As HashSet(Of String)

    Public Sub New(d As Dictionary(Of String, String), path As String, proposal As String, takenNames As HashSet(Of String), owner As Control)
        MyBase.New(d, Tr(d, "vd_mgr_add_title"),
                   Localization.Format(Tr(d, "vd_mgr_add_text_fmt"), IO.Path.GetFileName(path)),
                   "vd_mgr_btn_add_ok", owner)
        taken = If(takenNames, New HashSet(Of String)(StringComparer.OrdinalIgnoreCase))
        Dim label As New Label With {.Text = T("vd_mgr_add_name"), .AutoSize = True, .Margin = PPad(0, 2, 0, 2)}
        box = New TextBox With {.Width = P(240), .Text = If(proposal, ""), .Margin = PPad(0, 0, 0, 4)}
        box.AccessibleName = T("vd_mgr_add_name")
        verdict = NewNote("")
        AddHandler box.TextChanged, Sub() Revalidate()
        AddContent(label)
        AddContent(box)
        AddContent(verdict)
        Revalidate()
        Finish()
        ActiveControl = box
    End Sub

    Private Sub Revalidate()
        Dim n = box.Text.Trim()
        If Not DiskStates.IsUsableName(n) Then
            verdict.Text = T("vd_mgr_add_bad_name")
            SetAcceptable(False)
        ElseIf taken.Contains(n) Then
            verdict.Text = Localization.Format(T("vd_mgr_add_taken_fmt"), n)
            SetAcceptable(False)
        Else
            verdict.Text = T("vd_mgr_add_name_ok")
            SetAcceptable(True)
        End If
    End Sub

    Public ReadOnly Property ChosenName As String
        Get
            Return box.Text.Trim()
        End Get
    End Property

    ' A name for a file: its own name when that is usable and free, else the closest usable one - the
    ' spelling's characters kept, the rest made "-", cut to 40 - with -2, -3.. until it is free.
    Friend Shared Function Suggest(baseName As String, takenNames As HashSet(Of String)) As String
        Dim b = If(baseName, "").Trim()
        If DiskStates.IsUsableName(b) AndAlso (takenNames Is Nothing OrElse Not takenNames.Contains(b)) Then Return b
        Dim sb As New System.Text.StringBuilder()
        For Each ch In b
            If (ch >= "A"c AndAlso ch <= "Z"c) OrElse (ch >= "a"c AndAlso ch <= "z"c) OrElse (ch >= "0"c AndAlso ch <= "9"c) OrElse ch = "_"c OrElse ch = "-"c Then
                sb.Append(ch)
            Else
                sb.Append("-"c)
            End If
        Next
        Dim s = sb.ToString().Trim("-"c, "_"c)
        If s = "" Then s = "disk"
        If s.Length > 36 Then s = s.Substring(0, 36)
        If Not DiskStates.IsUsableName(s) Then s = "disk"
        Dim candidate = s
        Dim n = 2
        While takenNames IsNot Nothing AndAlso takenNames.Contains(candidate) AndAlso n < 1000
            candidate = s & "-" & n.ToString(Globalization.CultureInfo.InvariantCulture)
            n += 1
        End While
        Return candidate
    End Function

End Class
