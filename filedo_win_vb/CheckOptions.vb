' Every option the CLI's `check` verb takes (cmd/filedo/check.go), as one panel.
'
' The verb has twenty-nine flags and no two pages should model a different subset of them: the
' damaged-files job page and the expert builder both host this control, so what the shell can ask
' for is one list in one file, and adding a flag to the CLI is one line here.
'
' Three rules shape it:
'
'   The flag name is the label. `--min-mb` is a CLI token, not prose, and translating it would
'   produce a control whose text cannot be typed into the command line it writes. What is
'   translated is the group heading above it and the tooltip on it - the two things that say what
'   the token means.
'
'   An empty field is not an argument. The CLI's own defaults, and the environment variables it
'   reads, stay in force for every flag the user did not fill in; the panel never writes a value
'   the user did not choose.
'
'   A number that is not a number never reaches the command line. It is shown in the danger
'   colour and left out, so a mistyped field cannot turn into a run that fails a minute later with
'   a parse error.
Public Class CheckOptionsPanel
    Inherits FlowLayoutPanel

    Public Event Changed()

    Private Enum FlagKind
        Bool
        Number
        Text
        Choice
    End Enum

    Private Class FlagControl
        Public Property Name As String
        Public Property Kind As FlagKind
        Public Property Editor As Control
        Public Property Caption As Label
    End Class

    ' The number options `check` reads with ParseFloat (check.go); every other one is an Int. A
    ' value is valid when the CLI's own parser takes it, in every locale (GUI-15): "2,5" and "1,000"
    ' are not numbers to it, and IsNumeric's say-so used to put them on the line anyway.
    Private Shared ReadOnly DecimalOptions As String() = {
        "min-mb", "max-mb", "max-seconds", "threshold", "warmup", "warmup-idle",
        "ewma-alpha", "ewma-high-frac", "ewma-low-frac"}

    Private Shared Function IsValidNumber(name As String, value As String) As Boolean
        If Array.IndexOf(DecimalOptions, name) >= 0 Then Return Ui.IsDecimalNumber(value)
        Return Ui.IsWholeNumber(value)
    End Function

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly tips As New ToolTip()
    Private ReadOnly flags As New List(Of FlagControl)
    Private ReadOnly groupHeaders As New List(Of Label)
    Private noticeLabel As Label
    Private suspendEvents As Boolean = False

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        Dock = DockStyle.Top
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        FlowDirection = FlowDirection.TopDown
        WrapContents = False
        Margin = Ui.PxPad(Me, 0, 6, 0, 0)
        BuildContent()
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    ' The four groups are the four questions the verb actually answers: which files it reads, how
    ' it reads them, how hard it is allowed to push the drive, and what it leaves behind.
    Private Sub BuildContent()
        noticeLabel = New Label With {
            .Text = L("chk_panel_hint"),
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 6)
        }
        Controls.Add(noticeLabel)

        BuildGroup("chk_group_scope", New String() {
            "min-mb|number", "max-mb|number", "include-ext|text", "exclude-ext|text",
            "max-files|number", "max-seconds|number", "good-list|text"
        })

        BuildGroup("chk_group_reading", New String() {
            "mode|choice|quick,balanced,deep", "threshold|number", "warmup|number",
            "warmup-idle|number", "workers|number", "buf-kb|number",
            "balanced-min-mb|number", "single-reader|choice|auto,on,off"
        })

        BuildGroup("chk_group_pacing", New String() {
            "hdd-sleep-ms|number", "max-sleep-ms|number", "sleep-step-ms|number",
            "ewma-alpha|number", "ewma-high-frac|number", "ewma-low-frac|number"
        })

        BuildGroup("chk_group_run", New String() {
            "precount|bool", "no-precount|bool", "dry-run|bool", "resume|bool",
            "verbose|bool", "quiet|bool", "report|choice|csv,json", "report-file|text"
        })
    End Sub

    Private Sub BuildGroup(titleKey As String, specs As String())
        Dim head As New Label With {
            .Text = L(titleKey),
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 8, 0, 2)
        }
        groupHeaders.Add(head)
        Controls.Add(head)

        Dim flow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Margin = New Padding(0),
            .MaximumSize = New Size(Ui.Px(Me, 720), 0)
        }

        For Each spec In specs
            Dim parts = spec.Split("|"c)
            Dim name = parts(0)
            Select Case parts(1)
                Case "bool" : flow.Controls.Add(MakeBool(name))
                Case "choice" : flow.Controls.Add(MakeChoice(name, parts(2).Split(","c)))
                Case "number" : flow.Controls.Add(MakeField(name, FlagKind.Number))
                Case Else : flow.Controls.Add(MakeField(name, FlagKind.Text))
            End Select
        Next

        Controls.Add(flow)
    End Sub

    Private Function MakeBool(name As String) As Control
        Dim c As New CheckBox With {
            .Text = "--" & name,
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 2, 18, 2)
        }
        c.AccessibleName = "--" & name
        tips.SetToolTip(c, L("chk_" & name))
        AddHandler c.CheckedChanged, Sub() Raise()
        flags.Add(New FlagControl With {.Name = name, .Kind = FlagKind.Bool, .Editor = c})
        Return c
    End Function

    Private Function MakeChoice(name As String, choices As String()) As Control
        Dim host = NewFieldHost()
        Dim caption = NewCaption(name)
        Dim c As New ComboBox With {
            .DropDownStyle = ComboBoxStyle.DropDownList,
            .Width = Ui.Px(Me, 104),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 0)
        }
        c.Items.Add(L("chk_default"))
        c.Items.AddRange(choices.Cast(Of Object)().ToArray())
        c.SelectedIndex = 0
        c.AccessibleName = "--" & name
        tips.SetToolTip(c, L("chk_" & name))
        tips.SetToolTip(caption, L("chk_" & name))
        AddHandler c.SelectedIndexChanged, Sub() Raise()

        host.Controls.Add(caption)
        host.Controls.Add(c)
        flags.Add(New FlagControl With {.Name = name, .Kind = FlagKind.Choice, .Editor = c, .Caption = caption})
        Return host
    End Function

    Private Function MakeField(name As String, kind As FlagKind) As Control
        Dim host = NewFieldHost()
        Dim caption = NewCaption(name)
        Dim box As New TextBox With {
            .Width = If(kind = FlagKind.Number, Ui.Px(Me, 72), Ui.Px(Me, 130)),
            .Margin = New Padding(0)
        }
        box.AccessibleName = "--" & name
        tips.SetToolTip(box, L("chk_" & name))
        tips.SetToolTip(caption, L("chk_" & name))
        AddHandler box.TextChanged, Sub() Raise()

        host.Controls.Add(caption)
        host.Controls.Add(box)
        flags.Add(New FlagControl With {.Name = name, .Kind = kind, .Editor = box, .Caption = caption})
        Return host
    End Function

    Private Function NewFieldHost() As FlowLayoutPanel
        Return New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 2, 18, 2)
        }
    End Function

    Private Function NewCaption(name As String) As Label
        Return New Label With {
            .Text = "--" & name,
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 4, 6, 0)
        }
    End Function

    Private Sub Raise()
        If suspendEvents Then Return
        RaiseEvent Changed()
    End Sub

    ' Everything the user actually filled in, in the order the flags are declared above. A number
    ' that will not parse is left out and marked instead - see the file header.
    Public Function ToArgs() As List(Of String)
        Dim args As New List(Of String)()
        For Each f In flags
            Select Case f.Kind
                Case FlagKind.Bool
                    If CType(f.Editor, CheckBox).Checked Then args.Add("--" & f.Name)

                Case FlagKind.Choice
                    Dim c = CType(f.Editor, ComboBox)
                    If c.SelectedIndex > 0 Then
                        args.Add("--" & f.Name)
                        args.Add(c.SelectedItem.ToString())
                    End If

                Case Else
                    Dim box = CType(f.Editor, TextBox)
                    Dim v = box.Text.Trim()
                    If v = "" Then Continue For
                    If f.Kind = FlagKind.Number AndAlso Not IsValidNumber(f.Name, v) Then Continue For
                    args.Add("--" & f.Name)
                    args.Add(v)
            End Select
        Next
        Return args
    End Function

    ' True when some number field holds something that is not a number, so the page above can say
    ' so next to the button rather than letting the run fail on a parse error.
    Public Function HasInvalidNumber() As Boolean
        For Each f In flags
            If f.Kind <> FlagKind.Number Then Continue For
            Dim v = CType(f.Editor, TextBox).Text.Trim()
            If v <> "" AndAlso Not IsValidNumber(f.Name, v) Then Return True
        Next
        Return False
    End Function

    Public Sub Reset()
        suspendEvents = True
        For Each f In flags
            Select Case f.Kind
                Case FlagKind.Bool : CType(f.Editor, CheckBox).Checked = False
                Case FlagKind.Choice : CType(f.Editor, ComboBox).SelectedIndex = 0
                Case Else : CType(f.Editor, TextBox).Text = ""
            End Select
        Next
        suspendEvents = False
        Raise()
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text

        noticeLabel.Font = Theme.FontBody()
        noticeLabel.ForeColor = p.MutedText

        For Each h In groupHeaders
            h.Font = Theme.FontBodyStrong()
            h.ForeColor = p.Text
        Next

        For Each f In flags
            If f.Caption IsNot Nothing Then
                f.Caption.Font = Theme.FontMono()
                f.Caption.ForeColor = p.MutedText
            End If

            Select Case f.Kind
                Case FlagKind.Bool
                    Dim c = CType(f.Editor, CheckBox)
                    c.Font = Theme.FontMono()
                    c.ForeColor = p.Text

                Case FlagKind.Choice
                    Dim c = CType(f.Editor, ComboBox)
                    c.Font = Theme.FontBody()
                    c.BackColor = p.SurfaceAlt
                    c.ForeColor = p.Text
                    c.FlatStyle = FlatStyle.Flat

                Case Else
                    Dim box = CType(f.Editor, TextBox)
                    box.Font = Theme.FontBody()
                    box.BackColor = p.SurfaceAlt
                    box.BorderStyle = BorderStyle.FixedSingle
                    ' The one piece of state this panel shows in colour, and it is also shown by
                    ' the value being absent from the command line above.
                    Dim v = box.Text.Trim()
                    box.ForeColor = If(f.Kind = FlagKind.Number AndAlso v <> "" AndAlso Not IsValidNumber(f.Name, v), p.Danger, p.Text)
            End Select
        Next

        Invalidate(True)
    End Sub

End Class
