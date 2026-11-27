' The command guide (SP-0204): what every operation of the Command page's list is, in plain words, at the
' two places a person meets the list - the dialog that opens from Browse commands.. while choosing, and the
' Commands page (OperationGuideView.vb) for reading about them outside of choosing.
'
' One table feeds both: OperationGuide.Entries holds each word of CommandView.Operations once, with the group
' it is sorted into and an example line; the words themselves are keys of the language tables - op_<word>
' for the one-line description (also the hint under the list), opfull_<word> for the full one
' (OperationStrings.vb). The self-test holds the table against the list, so a verb added to the page cannot
' reach the dropdown without an explanation (SelfTestOperationGuide.vb).

' One operation of the list: the word as it is written in the dropdown ("fdsec info"), the group of the
' dialog and the page it is shown under, and a line a person could type.
Friend Class OperationEntry
    Public ReadOnly Word As String
    Public ReadOnly Group As String
    Public ReadOnly Example As String

    Public Sub New(word As String, group As String, example As String)
        Me.Word = word
        Me.Group = group
        Me.Example = example
    End Sub

    ' "fdsec info" -> "fdsec_info": the word as the tail of a language key.
    Public ReadOnly Property Key As String
        Get
            Return Word.Replace(" "c, "_"c)
        End Get
    End Property

    Public ReadOnly Property ShortKey As String
        Get
            Return "op_" & Key
        End Get
    End Property

    Public ReadOnly Property FullKey As String
        Get
            Return "opfull_" & Key
        End Get
    End Property
End Class

Friend Module OperationGuide

    ' The groups in the order both surfaces show them: the rail's own four (Check, Tidy, Erase, Protect), the
    ' copy family that stands alone in the rail, and the tools that belong to no job.
    Friend ReadOnly Groups As String() = {"check", "tidy", "copy", "erase", "protect", "tools"}

    ' The key of a group's heading. The four that the rail names say it in the rail's own words, so the
    ' guide and the rail never disagree about what Check or Protect is called.
    Friend Function GroupKey(group As String) As String
        Select Case group
            Case "check", "tidy", "erase", "protect"
                Return "rail_group_" & group
            Case Else
                Return "opguide_group_" & group
        End Select
    End Function

    ' Every word of CommandView.Operations, once. The examples are the CLI's own forms, as `filedo.exe help`
    ' prints them - never translated, because they are what is typed.
    Friend ReadOnly Entries As OperationEntry() = {
        New OperationEntry("info", "check", "filedo.exe D: info"),
        New OperationEntry("speed", "check", "filedo.exe E: speed 100"),
        New OperationEntry("test", "check", "filedo.exe F: test del"),
        New OperationEntry("check", "check", "filedo.exe check D:\Data"),
        New OperationEntry("probe", "check", "filedo.exe D: probe"),
        New OperationEntry("recover", "check", "filedo.exe D: recover"),
        New OperationEntry("cd", "tidy", "filedo.exe C:\temp cd"),
        New OperationEntry("compare", "tidy", "filedo.exe compare D:\Source E:\Target"),
        New OperationEntry("clean", "tidy", "filedo.exe E: clean"),
        New OperationEntry("copy", "copy", "filedo.exe copy D:\Source E:\Target"),
        New OperationEntry("fastcopy", "copy", "filedo.exe fastcopy D:\LargeFolder E:\Backup"),
        New OperationEntry("synccopy", "copy", "filedo.exe synccopy D:\Source E:\Target"),
        New OperationEntry("balanced", "copy", "filedo.exe balanced D:\Source E:\Target"),
        New OperationEntry("maxcopy", "copy", "filedo.exe maxcopy D:\Source E:\Target"),
        New OperationEntry("smartcopy", "copy", "filedo.exe smartcopy C:\Source D:\Target"),
        New OperationEntry("safecopy", "copy", "filedo.exe safecopy F:\Photos E:\Backup"),
        New OperationEntry("rehash", "copy", "filedo.exe rehash D:\Photos"),
        New OperationEntry("fill", "erase", "filedo.exe E: fill 1000"),
        New OperationEntry("wipe", "erase", "filedo.exe D:\Temp wipe"),
        New OperationEntry("secure", "protect", "filedo.exe secret.txt secure"),
        New OperationEntry("unsecure", "protect", "filedo.exe secret.fd-sec unsecure"),
        New OperationEntry("reveal", "protect", "filedo.exe holiday.fd-sec reveal"),
        New OperationEntry("fdsec info", "protect", "filedo.exe fdsec info c.fd-sec"),
        New OperationEntry("fdsec verify", "protect", "filedo.exe fdsec verify c.fd-sec"),
        New OperationEntry("backup", "protect", "filedo.exe backup D:\Photos"),
        New OperationEntry("backup sec", "protect", "filedo.exe backup D:\Photos sec"),
        New OperationEntry("restore", "protect", "filedo.exe restore Photos-2611101530.zip"),
        New OperationEntry("from", "tools", "filedo.exe from commands.txt"),
        New OperationEntry("hist", "tools", "filedo.exe hist"),
        New OperationEntry("help", "tools", "filedo.exe help"),
        New OperationEntry("fdsec register", "tools", "filedo.exe fdsec register"),
        New OperationEntry("fdsec unregister", "tools", "filedo.exe fdsec unregister")
    }

    ' The entries of one group, in table order.
    Friend Function EntriesOf(group As String) As List(Of OperationEntry)
        Dim found As New List(Of OperationEntry)
        For Each entry In Entries
            If entry.Group = group Then found.Add(entry)
        Next
        Return found
    End Function

End Module

' How the picker was left. Cancel, Escape and the close box are None and change nothing.
Friend Enum OperationPickerResult
    None
    Use      ' the selected operation is to be set in the Command page's list
    Guide    ' the full description of the selected operation is to be read
End Enum

' The dialog that opens from Browse commands.. beside the operation list: every operation under the heading of
' its group with its one-line description, one radio button each, so the arrow keys walk the whole list and a
' screen reader hears the word and then what it does. The operation the list shows now is selected to begin
' with. It is a DiskPageDialog (DiskHelp.vb): owned and centred, themed from the palette, its text wraps and the
' window grows with it up to most of the screen instead of cutting a sentence, in any of the five languages.
Friend Class OperationPickerDialog
    Inherits DiskPageDialog

    Private ReadOnly radios As New List(Of RadioButton)
    Private ReadOnly useBtn As GlyphButton
    Private ReadOnly guideBtn As GlyphButton
    Private ReadOnly cancelBtn As GlyphButton
    Private resultValue As OperationPickerResult = OperationPickerResult.None

    Public Sub New(d As Dictionary(Of String, String), owner As Control, current As String)
        MyBase.New(d, Tr(d, "opguide_picker_title"), owner, 700)
        AddParagraph(T("opguide_picker_intro"), muted:=True)

        Dim wordWidth = P(190)
        Dim textWidth = contentWidth - wordWidth - P(8)
        Dim grid As New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 2,
            .Margin = New Padding(0)
        }
        grid.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, wordWidth))
        grid.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, contentWidth - wordWidth))

        For Each group In OperationGuide.Groups
            Dim head = NewLabel(T(OperationGuide.GroupKey(group)), contentWidth, strong:=True)
            head.Margin = PPad(0, 14, 0, 4)
            AddGridRow(grid, head, Nothing)
            For Each entry In OperationGuide.EntriesOf(group)
                Dim radio As New RadioButton With {
                    .Text = entry.Word,
                    .AutoSize = True,
                    .MaximumSize = New Size(wordWidth, 0),
                    .Margin = PPad(0, 2, 6, 2),
                    .Tag = entry.Word
                }
                Dim hint = NewLabel(T(entry.ShortKey), textWidth, muted:=True)
                hint.Margin = PPad(0, 4, 0, 2)
                hint.Tag = radio
                radio.AccessibleName = entry.Word
                radio.AccessibleDescription = hint.Text
                AddHandler radio.DoubleClick, AddressOf Radio_DoubleClick
                AddHandler hint.Click, AddressOf Hint_Click
                AddHandler hint.DoubleClick, AddressOf Hint_DoubleClick
                radios.Add(radio)
                AddGridRow(grid, radio, hint)
            Next
        Next
        AddRow(grid)

        useBtn = NewButton(T("opguide_use"), GlyphRef.Vocabulary("action.confirm"))
        useBtn.Face = GlyphButtonFace.AnswerOk
        guideBtn = NewButton(T("opguide_picker_guide"), GlyphRef.Vocabulary("app.help"))
        cancelBtn = NewButton(T("shell_btn_cancel"), GlyphRef.Vocabulary("action.cancel"))
        cancelBtn.DialogResult = DialogResult.Cancel
        cancelBtn.Margin = PPad(8, 0, 0, 0)
        CancelButton = cancelBtn
        AcceptButton = useBtn
        AddHandler useBtn.Click, Sub() UseSelected()
        AddHandler guideBtn.Click, Sub() GuideSelected()
        ' The footer lays out from the right: the acting answer first, so Cancel stands leftmost
        ' (BUTTONS: Cancel first, the acting answer last).
        footer.Controls.Add(useBtn)
        footer.Controls.Add(guideBtn)
        footer.Controls.Add(cancelBtn)

        SelectWord(current)
        Finish()
        ThemeRadios()
        ActiveControl = SelectedRadio()
    End Sub

    ' One row of the grid: a heading across both columns, or a word and its description.
    Private Shared Sub AddGridRow(grid As TableLayoutPanel, left As Control, right As Control)
        grid.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        Dim row = grid.RowStyles.Count - 1
        grid.Controls.Add(left, 0, row)
        If right Is Nothing Then
            grid.SetColumnSpan(left, 2)
        Else
            grid.Controls.Add(right, 1, row)
        End If
    End Sub

    ' What the base's theme does not reach: the radio buttons.
    Private Sub ThemeRadios()
        Dim p = Theme.Current
        For Each rb In radios
            rb.Font = Theme.FontBodyStrong()
            rb.ForeColor = p.Text
            rb.BackColor = p.Surface
        Next
    End Sub

    Private Sub SelectWord(word As String)
        Dim chosen As RadioButton = Nothing
        For Each rb In radios
            If CStr(rb.Tag) = word Then chosen = rb
        Next
        If chosen Is Nothing AndAlso radios.Count > 0 Then chosen = radios(0)
        If chosen IsNot Nothing Then chosen.Checked = True
    End Sub

    Private Function SelectedRadio() As RadioButton
        For Each rb In radios
            If rb.Checked Then Return rb
        Next
        Return Nothing
    End Function

    ' A click on the description selects its word, as a click on a label of a radio button would.
    Private Sub Hint_Click(sender As Object, e As EventArgs)
        Dim rb = TryCast(DirectCast(sender, Label).Tag, RadioButton)
        If rb IsNot Nothing Then
            rb.Checked = True
            rb.Focus()
        End If
    End Sub

    Private Sub Hint_DoubleClick(sender As Object, e As EventArgs)
        Hint_Click(sender, e)
        UseSelected()
    End Sub

    Private Sub Radio_DoubleClick(sender As Object, e As EventArgs)
        UseSelected()
    End Sub

    Private Sub UseSelected()
        If SelectedRadio() Is Nothing Then Return
        resultValue = OperationPickerResult.Use
        DialogResult = DialogResult.OK
    End Sub

    Private Sub GuideSelected()
        If SelectedRadio() Is Nothing Then Return
        resultValue = OperationPickerResult.Guide
        DialogResult = DialogResult.OK
    End Sub

    ' How the dialog was left, and the word that was selected then ("" when nothing was).
    Public ReadOnly Property Result As OperationPickerResult
        Get
            Return resultValue
        End Get
    End Property

    Public ReadOnly Property Choice As String
        Get
            Dim rb = SelectedRadio()
            Return If(rb Is Nothing, "", CStr(rb.Tag))
        End Get
    End Property

    ' For SelfTest.vb: the rows, a selection, the two ways out, and every text the window shows.
    Friend ReadOnly Property RowCountForTest As Integer
        Get
            Return radios.Count
        End Get
    End Property

    Friend Sub SelectForTest(word As String)
        SelectWord(word)
    End Sub

    Friend Sub UseForTest()
        UseSelected()
    End Sub

    Friend Sub GuideForTest()
        GuideSelected()
    End Sub

    Friend Function TextsForTest() As List(Of String)
        Dim texts As New List(Of String)
        For Each c In AllChildren(page)
            If TypeOf c Is Label OrElse TypeOf c Is RadioButton Then texts.Add(c.Text)
        Next
        texts.Add(useBtn.Text)
        texts.Add(guideBtn.Text)
        texts.Add(cancelBtn.Text)
        Return texts
    End Function

    Friend ReadOnly Property LeaveButtonsForTest As GlyphButton()
        Get
            Return New GlyphButton() {useBtn, guideBtn, cancelBtn}
        End Get
    End Property

End Class
