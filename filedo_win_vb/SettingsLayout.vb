' Shared settings anatomy (WINDOWS-UI sections 3-5). These containers own no preferences.
Friend Class SettingRow
    Inherits TableLayoutPanel
    Friend ReadOnly Caption As Label
    Friend ReadOnly Hint As Label
    Friend ReadOnly Editor As Control
    Private ReadOnly words As TableLayoutPanel
    Private ReadOnly booleanRow As Boolean

    Friend Sub New(id As String, title As String, description As String, value As Control)
        Name = "setting:" & id
        Editor = value
        booleanRow = TypeOf value Is CheckBox
        Dock = DockStyle.Top
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        Margin = Ui.PxPad(Me, 0, 0, 0, 12)
        ColumnCount = If(booleanRow, 1, 2)
        RowCount = 1
        ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        If Not booleanRow Then ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, Ui.Px(Me, 260)))
        RowStyles.Add(New RowStyle(SizeType.AutoSize))
        words = New TableLayoutPanel With {.Dock = DockStyle.Top, .AutoSize = True, .ColumnCount = 1, .RowCount = 2, .Margin = New Padding(0)}
        words.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        Caption = New Label With {.AutoSize = True, .Text = title, .Margin = Ui.PxPad(Me, 0, 0, 8, 4)}
        Hint = New Label With {.AutoSize = True, .Text = description, .UseMnemonic = False, .Margin = Ui.PxPad(Me, If(booleanRow, 22, 0), 0, 8, 0)}
        If booleanRow Then
            value.Text = title
            value.Dock = DockStyle.Top
            value.Margin = New Padding(0)
            words.Controls.Add(value, 0, 0)
        Else
            words.Controls.Add(Caption, 0, 0)
            value.Dock = DockStyle.Top
            value.Margin = Ui.PxPad(Me, 8, 0, 0, 0)
        End If
        value.AccessibleName = title
        value.AccessibleDescription = description
        words.Controls.Add(Hint, 0, 1)
        Controls.Add(words, 0, 0)
        If Not booleanRow Then Controls.Add(value, 1, 0)
        AddHandler SizeChanged, Sub() FitColumns()
        AddHandler words.SizeChanged, Sub() FitText()
        ApplyTheme()
    End Sub

    Private Sub FitText()
        Dim width = Math.Max(1, words.ClientSize.Width)
        Caption.MaximumSize = New Size(Math.Max(1, width - Caption.Margin.Horizontal), 0)
        Hint.MaximumSize = New Size(Math.Max(1, width - Hint.Margin.Horizontal), 0)
    End Sub

    Friend Sub FitColumns()
        If Not booleanRow Then
            Dim available = Math.Max(2, ClientSize.Width)
            Dim labelWidth = Math.Min(Ui.Px(Me, 220), available \ 2)
            ColumnStyles(1).Width = Math.Min(Ui.Px(Me, 260), available - labelWidth)
        End If
        FitText()
    End Sub

    Friend Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        words.BackColor = p.Surface
        Caption.Font = Theme.FontBodyStrong()
        Caption.ForeColor = p.Text
        Hint.Font = Theme.FontBody()
        Hint.ForeColor = p.MutedText
        Editor.Font = Theme.FontBody()
        Editor.BackColor = p.Surface
        Editor.ForeColor = p.Text
        If TypeOf Editor Is Button Then Ui.StyleButton(DirectCast(Editor, Button), p.SurfaceAlt, p.Text, p.Border)
        Ui.HitTargetFloor(Me)
        NativeEditors.Apply(Me)
        FitColumns()
    End Sub
End Class

Friend Class SettingsGroup
    Inherits TableLayoutPanel
    Friend ReadOnly GroupId As String
    Friend ReadOnly Header As RailEntry
    Friend ReadOnly Body As TableLayoutPanel
    Friend Event ExpansionChanged()

    Friend Sub New(id As String, caption As String)
        GroupId = id
        Name = "settings-group:" & id
        Dock = DockStyle.Top
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        ColumnCount = 1
        RowCount = 2
        ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        RowStyles.Add(New RowStyle(SizeType.AutoSize))
        RowStyles.Add(New RowStyle(SizeType.AutoSize))
        Margin = Ui.PxPad(Me, 0, 0, 0, 10)
        Header = New RailEntry With {.Name = Name & ":header", .Key = id, .Text = caption, .IsGroupHeader = True,
                                    .Dock = DockStyle.Top, .TabStop = True, .Margin = New Padding(0), .Height = Ui.Px(Me, 44)}
        Body = New TableLayoutPanel With {.Dock = DockStyle.Top, .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                                         .ColumnCount = 1, .Padding = Ui.PxPad(Me, 16, 12, 16, 0), .Margin = New Padding(0)}
        Body.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        Controls.Add(Header, 0, 0)
        Controls.Add(Body, 0, 1)
        AddHandler Header.Click, Sub() Expanded = Not Expanded
        AddHandler Header.SizeChanged, Sub() Header.Height = Header.PreferredRowHeight(Header.Width)
        UpdateAccessibility()
    End Sub

    Friend Property Expanded As Boolean
        Get
            Return Not Header.Collapsed
        End Get
        Set(value As Boolean)
            If value = Expanded Then Return
            If Not value AndAlso Body.ContainsFocus Then Header.Focus()
            Header.Collapsed = Not value
            Body.Visible = value
            UpdateAccessibility()
            RaiseEvent ExpansionChanged()
        End Set
    End Property

    Friend Sub UpdateAccessibility()
        Header.AccessibleName = Header.Text
        Header.AccessibleDescription = Localization.T(If(Expanded, "rail_group_expanded", "rail_group_collapsed"))
        Header.DefaultActionText = Localization.T(If(Expanded, "rail_group_collapse", "rail_group_expand"))
    End Sub

    Friend Sub AddRow(row As SettingRow)
        Body.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        Body.RowCount = Body.RowStyles.Count
        Body.Controls.Add(row, 0, Body.RowCount - 1)
    End Sub

    Friend Sub ApplyTheme()
        BackColor = Theme.Current.Surface
        Body.BackColor = Theme.Current.Surface
        Header.RowUnit = Ui.Px(Me, 44)
        Header.Height = Header.PreferredRowHeight(Header.Width)
        Header.Invalidate()
        For Each row As SettingRow In Body.Controls.OfType(Of SettingRow)()
            row.ApplyTheme()
        Next
    End Sub
End Class
