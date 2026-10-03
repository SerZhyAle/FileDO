' A button that draws a vocabulary glyph beside its caption, in the palette in force.
'
' The shell's plain buttons are flat WinForms buttons whose caption Ui.KeepCaptionsReadable repaints
' when they are disabled - and that repair stands down for a button with an image (a disabled caption
' is then the near-black WinForms derives from the back colour, unreadable on the dark theme). A
' glyph button therefore paints everything itself: its back from the palette's roles, its border,
' the glyph (ICON-RENDER rule 2: the `content` role, the colour of text on that surface - the danger
' colour for a destructive action, the disabled text colour when the action does not apply) and its
' caption. Nothing is resolved once: every paint reads Theme.Current, so a theme switch needs no
' plumbing, only a repaint.
'
' A button with a caption and a glyph is not "glyph-only" (APP-BEHAVIOUR rule 9): the caption is its
' accessible name. A button with no caption (IconOnly) carries the meaning's canonical name as its
' accessible name and its tooltip - the caller sets both, and the self-test fails one that does not.
Public Class GlyphButton
    Inherits Button

    Private glyphValue As GlyphRef = Nothing
    Private tierValue As Integer = 24
    Private iconOnlyValue As Boolean = False
    Private dangerValue As Boolean = False
    Private hot As Boolean = False
    Private pressed As Boolean = False

    Public Sub New()
        SetStyle(ControlStyles.UserPaint Or ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or ControlStyles.ResizeRedraw, True)
        FlatStyle = FlatStyle.Flat
        UseVisualStyleBackColor = False
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        TabStop = True
    End Sub

    ' The meaning drawn, or Nothing for a plain caption.
    Public Property Glyph As GlyphRef
        Get
            Return glyphValue
        End Get
        Set(value As GlyphRef)
            glyphValue = value
            Relayout()
        End Set
    End Property

    ' The product's own mark, drawn where no glyph is set: for the one button that stands for the
    ' product itself - "the main window" - which is not a meaning of the vocabulary (ICON-SET rule 7:
    ' the product's mark is artwork, never used to stand for a meaning; here it stands for FileDO).
    Private pictureValue As Image = Nothing

    Public Property Picture As Image
        Get
            Return pictureValue
        End Get
        Set(value As Image)
            pictureValue = value
            Relayout()
        End Set
    End Property

    ' The size tier in design pixels (ICON-RENDER rule 5: 16, 20, 24, 32, 40, 48).
    Public Property Tier As Integer
        Get
            Return tierValue
        End Get
        Set(value As Integer)
            tierValue = value
            Relayout()
        End Set
    End Property

    ' Draws the glyph alone, in a square button.
    Public Property IconOnly As Boolean
        Get
            Return iconOnlyValue
        End Get
        Set(value As Boolean)
            iconOnlyValue = value
            Relayout()
        End Set
    End Property

    ' A destructive action: the glyph and the caption take the danger colour, so it is set apart in
    ' colour as well as by its place (never by colour alone - its caption says what it does).
    Public Property Danger As Boolean
        Get
            Return dangerValue
        End Get
        Set(value As Boolean)
            dangerValue = value
            Invalidate()
        End Set
    End Property

    Private Sub Relayout()
        If Parent IsNot Nothing Then Parent.PerformLayout()
        Invalidate()
    End Sub

    Private Const TextFlags As TextFormatFlags = TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding Or
                                                 TextFormatFlags.SingleLine Or TextFormatFlags.VerticalCenter Or
                                                 TextFormatFlags.EndEllipsis

    Private Function GlyphPx() As Integer
        Return If(glyphValue Is Nothing AndAlso pictureValue Is Nothing, 0, Ui.Px(Me, tierValue))
    End Function

    Private Function CaptionSize() As Size
        If iconOnlyValue OrElse String.IsNullOrEmpty(Text) Then Return Size.Empty
        Return TextRenderer.MeasureText(Text, Font, New Size(Integer.MaxValue, Integer.MaxValue),
                                        TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding Or TextFormatFlags.SingleLine)
    End Function

    Public Overrides Function GetPreferredSize(proposedSize As Size) As Size
        Dim gp = GlyphPx()
        Dim cap = CaptionSize()
        Dim border = 1
        ' ICON-RENDER rule 3.5: a Windows hit target is at least 28 logical px under a mouse or a pen.
        ' The padding rounds down at some scalings (a 16 px tier came out at 27.2 logical px at 125 %),
        ' so the floor is stated here rather than left to the rounding.
        Dim floorPx = Ui.Px(Me, 28)
        If iconOnlyValue Then
            Dim side = Math.Max(gp + 2 * Ui.Px(Me, 5) + 2 * border, floorPx)
            Return New Size(side, side)
        End If
        Dim content = gp + cap.Width
        If gp > 0 AndAlso cap.Width > 0 Then content += Ui.Px(Me, 6)
        Dim w = content + 2 * Ui.Px(Me, 8) + 2 * border
        Dim h = Math.Max(Math.Max(gp, cap.Height) + 2 * Ui.Px(Me, 5) + 2 * border, floorPx)
        Return New Size(w, h)
    End Function

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim p = Theme.Current
        Dim g = e.Graphics
        Dim back As Color
        If Not Enabled Then
            back = p.SurfaceAlt
        ElseIf pressed AndAlso hot Then
            back = p.SurfaceSelected
        ElseIf hot Then
            back = p.ControlHover
        Else
            back = p.SurfaceAlt
        End If
        Dim fore As Color = If(Enabled, If(dangerValue, p.Danger, p.Text), p.TextDisabled)

        Using b As New SolidBrush(back)
            g.FillRectangle(b, ClientRectangle)
        End Using
        Using pen As New Pen(p.Border)
            g.DrawRectangle(pen, 0, 0, Width - 1, Height - 1)
        End Using
        ' The keyboard's place: the accent, two pixels inside the border. A pointer click gives the
        ' button the focus without asking for a cue (ShowFocusCues is what Windows says is wanted).
        If Enabled AndAlso ((Focused AndAlso ShowFocusCues) OrElse defaultLook) Then
            Using pen As New Pen(p.Accent, 2.0F)
                g.DrawRectangle(pen, 1, 1, Width - 3, Height - 3)
            End Using
        End If

        Dim gp = GlyphPx()
        Dim cap = CaptionSize()
        Dim content = gp + cap.Width
        If gp > 0 AndAlso cap.Width > 0 Then content += Ui.Px(Me, 6)
        Dim x = (Width - content) \ 2
        If gp > 0 Then
            Dim square As New Rectangle(x, (Height - gp) \ 2, gp, gp)
            If glyphValue IsNot Nothing Then
                Glyphs.Draw(g, glyphValue, square, fore)
            ElseIf pictureValue IsNot Nothing Then
                g.DrawImage(pictureValue, square)
            End If
            x += gp + If(cap.Width > 0, Ui.Px(Me, 6), 0)
        End If
        If cap.Width > 0 Then
            TextRenderer.DrawText(g, Text, Font, New Rectangle(x, 0, Math.Max(0, Width - x - 2), Height), fore, TextFlags)
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
        pressed = False
        Invalidate()
    End Sub

    Protected Overrides Sub OnMouseDown(mevent As MouseEventArgs)
        MyBase.OnMouseDown(mevent)
        If mevent.Button = MouseButtons.Left Then
            pressed = True
            Invalidate()
        End If
    End Sub

    Protected Overrides Sub OnMouseUp(mevent As MouseEventArgs)
        MyBase.OnMouseUp(mevent)
        pressed = False
        Invalidate()
    End Sub

    Protected Overrides Sub OnKeyDown(kevent As KeyEventArgs)
        MyBase.OnKeyDown(kevent)
        If kevent.KeyCode = Keys.Space Then
            pressed = True
            hot = True
            Invalidate()
        End If
    End Sub

    Protected Overrides Sub OnKeyUp(kevent As KeyEventArgs)
        MyBase.OnKeyUp(kevent)
        If kevent.KeyCode = Keys.Space Then
            pressed = False
            hot = ClientRectangle.Contains(PointToClient(Cursor.Position))
            Invalidate()
        End If
    End Sub

    Protected Overrides Sub OnEnabledChanged(e As EventArgs)
        MyBase.OnEnabledChanged(e)
        If Not Enabled Then
            hot = False
            pressed = False
        End If
        Invalidate()
    End Sub

    Protected Overrides Sub OnGotFocus(e As EventArgs)
        MyBase.OnGotFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnLostFocus(e As EventArgs)
        MyBase.OnLostFocus(e)
        pressed = False
        Invalidate()
    End Sub

    ' The dialog's default button (the one Enter presses) is told so and looks it: the accent as its
    ' border, the way the keyboard's place is shown, without waiting for the focus to arrive.
    Private defaultLook As Boolean = False

    Public Overrides Sub NotifyDefault(value As Boolean)
        MyBase.NotifyDefault(value)
        defaultLook = value
        Invalidate()
    End Sub

    Protected Overrides Sub OnTextChanged(e As EventArgs)
        MyBase.OnTextChanged(e)
        Relayout()
    End Sub

    Protected Overrides Sub OnFontChanged(e As EventArgs)
        MyBase.OnFontChanged(e)
        Relayout()
    End Sub

    Protected Overrides Sub OnDpiChangedAfterParent(e As EventArgs)
        MyBase.OnDpiChangedAfterParent(e)
        Relayout()
    End Sub

    Protected Overrides Sub OnCreateControl()
        MyBase.OnCreateControl()
        AccessibleRole = AccessibleRole.PushButton
    End Sub

End Class

' A glyph drawn into a small bitmap, for the places that take a picture rather than a paint - the
' image of a menu item. One bitmap per (meaning, size, colour), made once and kept until the theme or
' the scale changes; a bitmap made per menu opening would leak a GDI object each time.
Friend Module GlyphBitmaps

    Private ReadOnly cache As New Dictionary(Of String, Bitmap)(StringComparer.Ordinal)

    Friend Function Render(glyph As GlyphRef, px As Integer, colour As Color) As Bitmap
        If glyph Is Nothing OrElse px <= 0 Then Return Nothing
        Dim key = glyph.Meaning & "|" & px.ToString() & "|" & colour.ToArgb().ToString("X8")
        Dim bmp As Bitmap = Nothing
        If cache.TryGetValue(key, bmp) Then Return bmp
        bmp = New Bitmap(px, px, Imaging.PixelFormat.Format32bppArgb)
        Using g = Graphics.FromImage(bmp)
            Glyphs.Draw(g, glyph, New Rectangle(0, 0, px, px), colour)
        End Using
        cache(key) = bmp
        Return bmp
    End Function

    ' Dropped when the palette or the scale changes: the pictures are stale then, and a menu that
    ' still shows one is rebuilt on its next opening. A picture is disposed one generation late, so a
    ' menu that is open at that very moment paints its old images rather than a disposed one.
    Private retired As New List(Of Bitmap)

    Friend Sub Clear()
        For Each b In retired
            b.Dispose()
        Next
        retired = cache.Values.ToList()
        cache.Clear()
    End Sub

    Friend ReadOnly Property Count As Integer
        Get
            Return cache.Count
        End Get
    End Property

End Module
