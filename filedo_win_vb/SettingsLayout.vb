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
        ' A boolean option carries its explanation under its own heading (BOOLEAN-OPTION 2.0 section 2.3),
        ' so the row's hint label stays out of the layout.
        Dim boolOption = TryCast(value, BooleanOption)
        If boolOption IsNot Nothing Then boolOption.Explanation = description
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
        If boolOption Is Nothing Then words.Controls.Add(Hint, 0, 1)
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
        If TypeOf Editor Is BooleanOption Then Editor.MaximumSize = New Size(Math.Max(1, width - Editor.Margin.Horizontal), 0)
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

' The header of a collapsible group (COLLAPSIBLE-GROUP 1.1 section 3): a filled state triangle at the
' extreme reading start, then the caption, on a face that is visibly the header and not the body, in a
' stronger weight. The whole header - triangle, caption and empty space - is one hit target that toggles
' the group once; the body does not. The triangle is control-owned vector geometry (ICON-RENDER 0.24
' section 20), not a vocabulary glyph and not a typed Unicode arrow: it reports the group's current
' state (right when collapsed, down when expanded; collapsed points left in a right-to-left layout), not
' the action the next click would take. Height is the ordinary target and grows with a wrapped caption.
Friend Class GroupHeader
    Inherits Control

    Friend Property DefaultActionText As String = ""

    Private collapsedValue As Boolean = False
    Private hot As Boolean = False

    Private Const LabelFlags As TextFormatFlags = TextFormatFlags.Left Or TextFormatFlags.WordBreak Or
                                                  TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding

    Friend Sub New()
        SetStyle(ControlStyles.AllPaintingInWmPaint Or ControlStyles.OptimizedDoubleBuffer Or
                 ControlStyles.ResizeRedraw Or ControlStyles.UserPaint, True)
        TabStop = True
    End Sub

    Friend Property Collapsed As Boolean
        Get
            Return collapsedValue
        End Get
        Set(value As Boolean)
            If collapsedValue <> value Then
                collapsedValue = value
                Invalidate()
            End If
        End Set
    End Property

    Private ReadOnly Property Mirrored As Boolean
        Get
            Return RightToLeft = RightToLeft.Yes
        End Get
    End Property

    ' The space before the marker (the extreme reading start) and after the caption.
    Private Function EdgePx() As Integer
        Return Ui.Px(Me, UiTokens.GroupMarkerGap())
    End Function

    Friend Function MarkerPx() As Integer
        Return Ui.Px(Me, UiTokens.GroupMarker())
    End Function

    ' Where the caption starts, measured from the reading-start edge.
    Private Function TextInset() As Integer
        Return EdgePx() + MarkerPx() + Ui.Px(Me, UiTokens.GroupMarkerGap())
    End Function

    Private Function VerticalPad() As Integer
        Return Ui.Px(Me, UiTokens.Scaled(6))
    End Function

    Friend Function CaptionBounds(width As Integer, height As Integer) As Rectangle
        Dim w = Math.Max(1, width - TextInset() - EdgePx())
        Return If(Mirrored, New Rectangle(EdgePx(), 0, w, height), New Rectangle(TextInset(), 0, w, height))
    End Function

    Private Function CaptionFont() As Font
        Return RailEntry.LabelFont(False, True, Ui.DpiFor(Me))   ' the bold body face, cached per dpi
    End Function

    ' The header's height at this width: the ordinary target, or more when the caption wraps.
    Friend Function PreferredHeightFor(width As Integer) As Integer
        Dim box = CaptionBounds(width, Integer.MaxValue)
        Dim textH = TextRenderer.MeasureText(If(Text, ""), CaptionFont(), New Size(box.Width, Integer.MaxValue), LabelFlags).Height
        Return Math.Max(Ui.Px(Me, UiTokens.GroupHeaderHeight()), textH + 2 * VerticalPad())
    End Function

    ' The marker's square, centred on the caption's first line.
    Friend Function MarkerSquare(height As Integer) As Rectangle
        Dim side = MarkerPx()
        Dim lineH = TextRenderer.MeasureText("Ag", CaptionFont(), New Size(Integer.MaxValue, Integer.MaxValue), LabelFlags).Height
        Dim top = Math.Max(0, Math.Min((height - side) \ 2, VerticalPad() + (lineH - side) \ 2))
        If Mirrored Then Return New Rectangle(Width - EdgePx() - side, top, side, side)
        Return New Rectangle(EdgePx(), top, side, side)
    End Function

    ' The three points of the filled triangle in its square: right (collapsed, LTR), left (collapsed,
    ' RTL) or down (expanded). Exposed so the self-test can read the direction without reading pixels.
    Friend Function MarkerPoints(square As Rectangle) As Point()
        Dim cx = square.Left + square.Width \ 2
        Dim cy = square.Top + square.Height \ 2
        Dim w = Math.Max(2, CInt(square.Width * 0.5))
        Dim h = Math.Max(2, CInt(square.Width * 0.62))
        If Not collapsedValue Then
            Return {New Point(cx - h \ 2, cy - w \ 3), New Point(cx + h \ 2, cy - w \ 3), New Point(cx, cy + 2 * w \ 3)}
        End If
        If Mirrored Then
            Return {New Point(cx + w \ 3, cy - h \ 2), New Point(cx + w \ 3, cy + h \ 2), New Point(cx - 2 * w \ 3, cy)}
        End If
        Return {New Point(cx - w \ 3, cy - h \ 2), New Point(cx - w \ 3, cy + h \ 2), New Point(cx + 2 * w \ 3, cy)}
    End Function

    ' The surfaces a header is painted on, one function so the paint and the self-test's contrast rows
    ' read the same roles.
    Friend Shared Function FaceFor(p As Theme.Palette, hover As Boolean) As Color
        Return If(hover, p.BandHover, p.SurfaceBand)
    End Function

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim p = Theme.Current
        Dim g = e.Graphics
        g.TextRenderingHint = Drawing.Text.TextRenderingHint.ClearTypeGridFit
        Using b As New SolidBrush(FaceFor(p, hot))
            g.FillRectangle(b, ClientRectangle)
        End Using
        g.SmoothingMode = Drawing2D.SmoothingMode.AntiAlias
        Using b As New SolidBrush(p.Text)
            g.FillPolygon(b, MarkerPoints(MarkerSquare(Height)))
        End Using
        g.SmoothingMode = Drawing2D.SmoothingMode.Default
        Dim box = CaptionBounds(Width, Height)
        Dim size = TextRenderer.MeasureText(g, If(Text, ""), CaptionFont(), New Size(box.Width, Integer.MaxValue), LabelFlags)
        Dim top = Math.Max(0, (Height - size.Height) \ 2)
        TextRenderer.DrawText(g, If(Text, ""), CaptionFont(), New Rectangle(box.Left, top, box.Width, Math.Min(size.Height, Height)),
                              p.Text, LabelFlags Or If(Mirrored, TextFormatFlags.Right Or TextFormatFlags.RightToLeft, 0))
        If Focused Then
            Using pen As New Pen(p.Accent, 2.0F)
                Dim r = ClientRectangle
                r.Inflate(-2, -2)
                g.DrawRectangle(pen, r)
            End Using
        End If
    End Sub

    Protected Overrides Sub OnMouseEnter(e As EventArgs)
        MyBase.OnMouseEnter(e)
        hot = True
        Invalidate()
    End Sub

    Protected Overrides Sub OnMouseLeave(e As EventArgs)
        MyBase.OnMouseLeave(e)
        hot = False
        Invalidate()
    End Sub

    Protected Overrides Sub OnGotFocus(e As EventArgs)
        MyBase.OnGotFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnLostFocus(e As EventArgs)
        MyBase.OnLostFocus(e)
        Invalidate()
    End Sub

    ' Enter and Space toggle the group (COLLAPSIBLE-GROUP 4.5); focus stays on the header.
    Protected Overrides Function IsInputKey(keyData As Keys) As Boolean
        If keyData = Keys.Space OrElse keyData = Keys.Enter Then Return True
        Return MyBase.IsInputKey(keyData)
    End Function

    Protected Overrides Sub OnKeyDown(e As KeyEventArgs)
        MyBase.OnKeyDown(e)
        If e.KeyCode = Keys.Space OrElse e.KeyCode = Keys.Enter Then
            OnClick(EventArgs.Empty)
            e.Handled = True
        End If
    End Sub

    Friend Sub PerformClick()
        OnClick(EventArgs.Empty)
    End Sub

    ' For the self-test: paint the header into any Graphics in the state set.
    Friend Sub PaintForTest(g As Graphics, hover As Boolean)
        hot = hover
        OnPaint(New PaintEventArgs(g, New Rectangle(0, 0, Width, Height)))
        hot = False
    End Sub

    Protected Overrides Function CreateAccessibilityInstance() As AccessibleObject
        Return New GroupHeaderAccessible(Me)
    End Function

    ' The header as UI Automation sees it: its name, expanded or collapsed, and a default action that
    ' does what a click does.
    Private Class GroupHeaderAccessible
        Inherits ControlAccessibleObject

        Private ReadOnly header As GroupHeader

        Public Sub New(owner As GroupHeader)
            MyBase.New(owner)
            header = owner
        End Sub

        Public Overrides ReadOnly Property Role As AccessibleRole
            Get
                Return AccessibleRole.PushButton
            End Get
        End Property

        Public Overrides ReadOnly Property DefaultAction As String
            Get
                Return If(header.DefaultActionText, "")
            End Get
        End Property

        Public Overrides Sub DoDefaultAction()
            header.PerformClick()
        End Sub

        Public Overrides ReadOnly Property State As AccessibleStates
            Get
                Return MyBase.State Or If(header.Collapsed, AccessibleStates.Collapsed, AccessibleStates.Expanded)
            End Get
        End Property
    End Class
End Class

Friend Class SettingsGroup
    Inherits TableLayoutPanel
    Friend ReadOnly GroupId As String
    Friend ReadOnly Header As GroupHeader
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
        ' 8 units between sibling groups (COLLAPSIBLE-GROUP 3.6).
        Margin = New Padding(0, 0, 0, Ui.Px(Me, UiTokens.GroupSiblingGap()))
        Header = New GroupHeader With {.Name = Name & ":header", .Text = caption,
                                       .Dock = DockStyle.Top, .TabStop = True, .Margin = New Padding(0),
                                       .Height = Ui.Px(Me, UiTokens.GroupHeaderHeight())}
        ' The body spans the content width below its header, inset 12 (3.5, 3.6).
        Body = New TableLayoutPanel With {.Dock = DockStyle.Top, .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                                         .ColumnCount = 1,
                                         .Padding = New Padding(Ui.Px(Me, UiTokens.GroupBodyInset()), Ui.Px(Me, UiTokens.GroupMarkerGap()),
                                                                Ui.Px(Me, UiTokens.GroupBodyInset()), 0),
                                         .Margin = New Padding(0)}
        Body.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        Controls.Add(Header, 0, 0)
        Controls.Add(Body, 0, 1)
        AddHandler Header.Click, Sub() Expanded = Not Expanded
        AddHandler Header.SizeChanged, Sub() Header.Height = Header.PreferredHeightFor(Header.Width)
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
        Header.Height = Header.PreferredHeightFor(Header.Width)
        Header.Invalidate()
        For Each row As SettingRow In Body.Controls.OfType(Of SettingRow)()
            row.ApplyTheme()
        Next
    End Sub
End Class
