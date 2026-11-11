' A boolean option (BOOLEAN-OPTION 2.0): a visible square checkbox at the reading start, then one text
' cell - a heading row (the meaning's vocabulary glyph when one is set, then the short name) and, when one
' is useful, an explanation under it in the next smaller text role. It is a CheckBox, so every caller keeps
' Checked, CheckedChanged, the keyboard and the one named checkbox screen readers already know; only the
' drawing and the places a pointer click toggles it are this class's own.
'
' Section 2: an 8-unit gap from the box to the cell, 4 from the heading to the explanation, 8 between the
' glyph and the name; the box is the UiTokens.OptionBox square (20 pointer) on the first heading line, not
' centred on a long explanation; the row is its content's height and never less than the ordinary target.
' The explanation and a long caption wrap inside the width a layout gives (MaximumSize) and are never cut.
' Section 3.1: a click on the box, the glyph or the caption toggles once; the explanation, the empty space
' after the heading and the background do not. Space toggles a focused option; a disabled one toggles from
' no path and shows why (DisabledReason) under its heading. The check is vector geometry drawn in the
' palette's roles - never baked black or white - so a theme switch is a repaint (section 3.5).
Public Class BooleanOption
    Inherits CheckBox

    Private explanationValue As String = ""
    Private disabledReasonValue As String = ""
    Private glyphValue As GlyphRef = Nothing
    Private hot As Boolean = False
    Private pressedOnHeading As Boolean = False
    Private noteFontValue As Font = Nothing
    Private noteFontDpi As Integer = 0

    Public Sub New()
        SetStyle(ControlStyles.UserPaint Or ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or ControlStyles.ResizeRedraw, True)
        AutoSize = True
        TabStop = True
    End Sub

    ' What enabling or disabling it does, when the caption does not make that clear (section 2.3). Empty
    ' when the option is obvious: no second row is reserved then. It is the checkbox's accessible
    ' description unless the caller set another.
    Public Property Explanation As String
        Get
            Return explanationValue
        End Get
        Set(value As String)
            Dim previous = explanationValue
            explanationValue = If(value, "")
            If String.IsNullOrEmpty(AccessibleDescription) OrElse AccessibleDescription = previous Then
                AccessibleDescription = If(explanationValue = "", Nothing, explanationValue)
            End If
            Relayout()
        End Set
    End Property

    ' Why the option cannot be changed now and what would allow it (section 3.5), shown under the
    ' heading while the option is disabled.
    Public Property DisabledReason As String
        Get
            Return disabledReasonValue
        End Get
        Set(value As String)
            disabledReasonValue = If(value, "")
            Relayout()
        End Set
    End Property

    ' The option's meaning, drawn before the name at the caption glyph tier, or Nothing.
    Public Property Glyph As GlyphRef
        Get
            Return glyphValue
        End Get
        Set(value As GlyphRef)
            glyphValue = value
            Relayout()
        End Set
    End Property

    ' The text under the heading as it stands now: the explanation, and the reason while disabled.
    Friend Function ShownNote() As String
        If Enabled OrElse disabledReasonValue = "" Then Return explanationValue
        If explanationValue = "" Then Return disabledReasonValue
        Return explanationValue & Environment.NewLine & disabledReasonValue
    End Function

    Private Sub Relayout()
        If Parent IsNot Nothing Then Parent.PerformLayout()
        Invalidate()
    End Sub

    ' The explanation's role: the caption role one step smaller (9 pt, 12 px at 96 dpi), made once per dpi.
    Private Function NoteFont() As Font
        Dim dpi = Ui.DpiFor(Me)
        If noteFontValue Is Nothing OrElse noteFontDpi <> dpi Then
            If noteFontValue IsNot Nothing Then noteFontValue.Dispose()
            noteFontValue = Theme.FontCaption(dpi)
            noteFontDpi = dpi
        End If
        Return noteFontValue
    End Function

    Private Function CaptionFlags() As TextFormatFlags
        Return TextFormatFlags.Left Or TextFormatFlags.Top Or TextFormatFlags.WordBreak Or TextFormatFlags.NoPadding Or
               If(UseMnemonic, TextFormatFlags.HidePrefix, TextFormatFlags.NoPrefix)
    End Function

    Private Const NoteFlags As TextFormatFlags = TextFormatFlags.Left Or TextFormatFlags.Top Or TextFormatFlags.WordBreak Or
                                                 TextFormatFlags.NoPadding Or TextFormatFlags.NoPrefix

    ' Where each part sits, laid out left to right (the paint and the hit test mirror it in a
    ' right-to-left layout). Exposed so the self-test reads the geometry without reading pixels.
    Friend Structure OptionLayout
        Public Box As Rectangle
        Public GlyphSquare As Rectangle
        Public Caption As Rectangle
        Public Note As Rectangle
        Public Size As Size
    End Structure

    ' The layout for a control this wide (Integer.MaxValue: as wide as the text wants).
    Friend Function LayoutFor(available As Integer) As OptionLayout
        Dim r As New OptionLayout
        Dim boxPx = Ui.Px(Me, UiTokens.OptionBox())
        Dim cellGap = Ui.Px(Me, UiTokens.OptionCellGap())
        Dim targetPx = Ui.Px(Me, UiTokens.Target())
        Dim glyphPx = If(glyphValue Is Nothing, 0, Ui.Px(Me, UiTokens.CaptionGlyph()))
        Dim cellX = boxPx + cellGap
        Dim headX = cellX + If(glyphPx > 0, glyphPx + cellGap, 0)
        Dim minText = Ui.Px(Me, 40)
        Dim cellWidth = If(available = Integer.MaxValue, Integer.MaxValue, Math.Max(minText, available - cellX))
        Dim captionWidth = If(available = Integer.MaxValue, Integer.MaxValue, Math.Max(minText, available - headX))

        Dim flags = CaptionFlags()
        Dim cap = If(String.IsNullOrEmpty(Text), Size.Empty,
                     TextRenderer.MeasureText(Text, Font, New Size(captionWidth, Integer.MaxValue), flags))
        Dim lineH = TextRenderer.MeasureText("Ag", Font, New Size(Integer.MaxValue, Integer.MaxValue), flags).Height
        Dim rowH = Math.Max(lineH, Math.Max(boxPx, glyphPx))
        Dim top = Math.Max(0, (targetPx - rowH) \ 2)
        Dim mid = top + rowH \ 2

        r.Box = New Rectangle(0, mid - boxPx \ 2, boxPx, boxPx)
        If glyphPx > 0 Then r.GlyphSquare = New Rectangle(cellX, mid - glyphPx \ 2, glyphPx, glyphPx)
        r.Caption = New Rectangle(headX, mid - lineH \ 2, cap.Width, cap.Height)
        Dim headBottom = Math.Max(r.Box.Bottom, Math.Max(r.Caption.Bottom, r.GlyphSquare.Bottom))
        Dim bottom = headBottom
        Dim note = ShownNote()
        If note <> "" Then
            Dim n = TextRenderer.MeasureText(note, NoteFont(), New Size(cellWidth, Integer.MaxValue), NoteFlags)
            r.Note = New Rectangle(cellX, headBottom + Ui.Px(Me, UiTokens.OptionExplanationGap()), n.Width, n.Height)
            bottom = r.Note.Bottom
        End If
        Dim w = Math.Max(r.Box.Right, Math.Max(r.Caption.Right, r.Note.Right)) + 1
        r.Size = New Size(w, Math.Max(targetPx, bottom + top))
        Return r
    End Function

    ' The base class applies MaximumSize and MinimumSize to the size it computes; an override has to
    ' apply them itself. The text wraps in MaximumSize.Width, or - for an option docked across its
    ' container - in the width the dock layout proposes, so the height asked for is the wrapped one.
    Public Overrides Function GetPreferredSize(proposedSize As Size) As Size
        Dim limit = If(MaximumSize.Width > 0, MaximumSize.Width, Integer.MaxValue)
        Dim spans = Dock = DockStyle.Top OrElse Dock = DockStyle.Bottom OrElse Dock = DockStyle.Fill
        If spans AndAlso proposedSize.Width > 1 Then limit = Math.Min(limit, proposedSize.Width)
        Dim s = LayoutFor(limit).Size
        Dim w = s.Width
        Dim h = s.Height
        If MaximumSize.Width > 0 Then w = Math.Min(w, MaximumSize.Width)
        If MaximumSize.Height > 0 Then h = Math.Min(h, MaximumSize.Height)
        Return New Size(Math.Max(w, MinimumSize.Width), Math.Max(h, MinimumSize.Height))
    End Function

    ' The places a pointer click toggles the option (section 3.1): the heading band from the reading
    ' start to the end of the caption - the box, the glyph and the name - above the explanation. The
    ' explanation, the space after the caption and the background below do not toggle.
    Friend Function TogglesAt(pt As Point) As Boolean
        Dim r = LayoutFor(Width)
        Dim x = If(RightToLeft = RightToLeft.Yes, Width - 1 - pt.X, pt.X)
        Dim bandBottom = If(r.Note.Height > 0, r.Note.Top, Height)
        Dim bandRight = Math.Max(r.Box.Right, Math.Max(r.Caption.Right, r.GlyphSquare.Right))
        Return x >= 0 AndAlso x < bandRight AndAlso pt.Y >= 0 AndAlso pt.Y < bandBottom
    End Function

    Private Function Mirror(r As Rectangle) As Rectangle
        If RightToLeft <> RightToLeft.Yes Then Return r
        Return New Rectangle(Width - r.Right, r.Top, r.Width, r.Height)
    End Function

    ' The colours of one paint, read from the palette in force (section 3.5): the outline and the check
    ' reach 3:1 on the surface, the caption and the explanation 4.5:1; a disabled option draws its box and
    ' name in the disabled ink and an unfilled check, so unchecked, checked and disabled differ by shape
    ' as well as by colour.
    Friend Structure OptionColours
        Public Outline As Color
        Public Fill As Color
        Public Filled As Boolean
        Public Mark As Color
        Public Caption As Color
        Public Note As Color
    End Structure

    Friend Function ColoursFor(p As Theme.Palette) As OptionColours
        Dim c As New OptionColours
        c.Note = p.MutedText
        If Not Enabled Then
            c.Outline = p.TextDisabled
            c.Mark = p.TextDisabled
            c.Caption = p.TextDisabled
            c.Filled = False
            Return c
        End If
        c.Outline = If(hot, p.Text, p.MutedText)
        c.Fill = p.Accent
        c.Filled = True
        c.Mark = p.AccentText
        ' A caller may colour the caption (a destructive option that is on); otherwise it is text.
        c.Caption = If(ForeColor.A = 255, ForeColor, p.Text)
        Return c
    End Function

    Private Function BoxPath(box As RectangleF) As Drawing2D.GraphicsPath
        Dim path As New Drawing2D.GraphicsPath()
        Dim d = CSng(Math.Max(2, 2 * Ui.Px(Me, UiTokens.Scaled(2))))
        d = Math.Min(d, Math.Min(box.Width, box.Height))
        path.AddArc(box.Left, box.Top, d, d, 180, 90)
        path.AddArc(box.Right - d, box.Top, d, d, 270, 90)
        path.AddArc(box.Right - d, box.Bottom - d, d, d, 0, 90)
        path.AddArc(box.Left, box.Bottom - d, d, d, 90, 90)
        path.CloseFigure()
        Return path
    End Function

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim p = Theme.Current
        Dim g = e.Graphics
        Dim back = If(BackColor.A = 255, BackColor, If(Parent IsNot Nothing, Parent.BackColor, p.Surface))
        Using b As New SolidBrush(back)
            g.FillRectangle(b, ClientRectangle)
        End Using
        Dim r = LayoutFor(Width)
        Dim c = ColoursFor(p)
        Dim rtl = RightToLeft = RightToLeft.Yes
        Dim box = Mirror(r.Box)
        Dim marked = CheckState <> CheckState.Unchecked
        Dim stroke = CSng(Math.Max(1, Ui.Px(Me, 1)))

        g.SmoothingMode = Drawing2D.SmoothingMode.AntiAlias
        Using path = BoxPath(New RectangleF(box.X + stroke / 2.0F, box.Y + stroke / 2.0F, box.Width - stroke, box.Height - stroke))
            If marked AndAlso c.Filled Then
                Using b As New SolidBrush(c.Fill)
                    g.FillPath(b, path)
                End Using
            End If
            Using pen As New Pen(If(marked AndAlso c.Filled, c.Fill, c.Outline), stroke)
                g.DrawPath(pen, path)
            End Using
        End Using
        If marked Then
            Using pen As New Pen(c.Mark, CSng(Math.Max(1.5, box.Width / 9.0)))
                pen.StartCap = Drawing2D.LineCap.Round
                pen.EndCap = Drawing2D.LineCap.Round
                pen.LineJoin = Drawing2D.LineJoin.Round
                If CheckState = CheckState.Indeterminate Then
                    g.DrawLine(pen, box.Left + box.Width * 0.25F, box.Top + box.Height * 0.5F,
                               box.Left + box.Width * 0.75F, box.Top + box.Height * 0.5F)
                Else
                    g.DrawLines(pen, New PointF() {
                        New PointF(box.Left + box.Width * 0.24F, box.Top + box.Height * 0.53F),
                        New PointF(box.Left + box.Width * 0.43F, box.Top + box.Height * 0.71F),
                        New PointF(box.Left + box.Width * 0.77F, box.Top + box.Height * 0.31F)})
                End If
            End Using
        End If
        g.SmoothingMode = Drawing2D.SmoothingMode.Default

        If glyphValue IsNot Nothing Then Glyphs.Draw(g, glyphValue, Mirror(r.GlyphSquare), c.Caption)
        Dim direction = If(rtl, TextFormatFlags.Right Or TextFormatFlags.RightToLeft, TextFormatFlags.Left)
        If r.Caption.Width > 0 Then
            Dim capBox = Mirror(New Rectangle(r.Caption.X, r.Caption.Y, r.Caption.Width + 1, r.Caption.Height))
            TextRenderer.DrawText(g, Text, Font, capBox, c.Caption, CaptionFlags() Or direction)
            If Focused AndAlso ShowFocusCues Then
                ControlPaint.DrawFocusRectangle(g, Rectangle.Inflate(capBox, 2, 1), c.Caption, back)
            End If
        End If
        If r.Note.Width > 0 Then
            Dim noteBox = Mirror(New Rectangle(r.Note.X, r.Note.Y, r.Note.Width + 1, r.Note.Height))
            TextRenderer.DrawText(g, ShownNote(), NoteFont(), noteBox, c.Note, NoteFlags Or direction)
        End If
    End Sub

    Protected Overrides Sub OnMouseDown(mevent As MouseEventArgs)
        pressedOnHeading = TogglesAt(mevent.Location)
        MyBase.OnMouseDown(mevent)
    End Sub

    ' CheckBox.OnMouseUp hands the click on with the pointer's place; a click that did not both start and
    ' end on the heading is not an activation. Space, a mnemonic and the accessibility action arrive with
    ' no place and toggle as before. A disabled option toggles from no path.
    Protected Overrides Sub OnClick(e As EventArgs)
        If Not Enabled Then Return
        Dim m = TryCast(e, MouseEventArgs)
        If m IsNot Nothing AndAlso Not (pressedOnHeading AndAlso TogglesAt(m.Location)) Then Return
        MyBase.OnClick(e)
    End Sub

    Protected Overrides Sub OnMouseMove(mevent As MouseEventArgs)
        MyBase.OnMouseMove(mevent)
        Dim over = TogglesAt(mevent.Location)
        If over <> hot Then
            hot = over
            Invalidate()
        End If
    End Sub

    Protected Overrides Sub OnMouseLeave(e As EventArgs)
        MyBase.OnMouseLeave(e)
        hot = False
        Invalidate()
    End Sub

    Protected Overrides Sub OnCheckStateChanged(e As EventArgs)
        MyBase.OnCheckStateChanged(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnEnabledChanged(e As EventArgs)
        MyBase.OnEnabledChanged(e)
        hot = False
        If disabledReasonValue <> "" Then Relayout() Else Invalidate()
    End Sub

    Protected Overrides Sub OnGotFocus(e As EventArgs)
        MyBase.OnGotFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnLostFocus(e As EventArgs)
        MyBase.OnLostFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnRightToLeftChanged(e As EventArgs)
        MyBase.OnRightToLeftChanged(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnDpiChangedAfterParent(e As EventArgs)
        MyBase.OnDpiChangedAfterParent(e)
        Relayout()
    End Sub

    ' For the self-test: a pointer click at a point, delivered the way CheckBox.OnMouseUp delivers it.
    Friend Sub ClickAtForTest(pt As Point)
        pressedOnHeading = TogglesAt(pt)
        OnClick(New MouseEventArgs(MouseButtons.Left, 1, pt.X, pt.Y, 0))
    End Sub

    Protected Overrides Sub Dispose(disposing As Boolean)
        If disposing AndAlso noteFontValue IsNot Nothing Then
            noteFontValue.Dispose()
            noteFontValue = Nothing
        End If
        MyBase.Dispose(disposing)
    End Sub
End Class
