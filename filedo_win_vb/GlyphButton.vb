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
'
' The Face is where the button's colour is spent (BUTTONS 2.0 section 3 rule 5): the ordinary neutral
' face, the accent fill of a primary action outside a dialog, or the danger fill of an irreversible
' acting answer or an enabled Stop. A dialog answer keeps the neutral face: an affirmative answer's
' glyph takes state.ok and a caution answer's state.warning, and Cancel, No and a plain Close stay
' mono - "non-destructive answer faces MUST NOT acquire decorative green or red tints" and "Cancel,
' No and plain Close MUST use neutral treatment". AnswerError and AnswerErrorSafe are the names the
' dismissing answers were built with before BUTTONS 2.0; they now draw exactly as Normal and stay
' only so the call sites that name them keep compiling (the polarity rule, ShellDialog.FaceOf, no
' longer returns them). A disabled button mutes glyph and caption together and shows no hue; under
' high contrast the palette itself replaces hue with the system colours.
Public Enum GlyphButtonFace
    Normal
    AccentFill
    DangerFill
    AnswerOk
    AnswerError
    AnswerWarning
    AnswerErrorSafe
End Enum

Public Class GlyphButton
    Inherits Button

    Private glyphValue As GlyphRef = Nothing
    Private tierValue As Integer = 24
    Private iconOnlyValue As Boolean = False
    Private dangerValue As Boolean = False
    Private faceValue As GlyphButtonFace = GlyphButtonFace.Normal
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

    ' Where the button's colour is spent (WINDOWS-UI 0.4 section 11). Normal is the mono look.
    Public Property Face As GlyphButtonFace
        Get
            Return faceValue
        End Get
        Set(value As GlyphButtonFace)
            faceValue = value
            Invalidate()
        End Set
    End Property

    ' The colours of one paint: the face the button draws on, the ink of its caption, the ink of its
    ' glyph, and its border. Every value is read from the palette in force at paint time, so a theme
    ' switch needs no plumbing beyond the repaint (APP-STYLE section 3).
    Friend Structure FaceColours
        Public Back As Color
        Public Caption As Color
        Public Glyph As Color
        Public Border As Color
        Public FocusRing As Color
    End Structure

    Friend Function ColoursFor(p As Theme.Palette) As FaceColours
        Dim c As New FaceColours
        If Not Enabled Then
            ' Unavailable: glyph and caption mute together to text.disabled and show no hue
            ' (section 11 item 6), on the ordinary face.
            c.Back = p.SurfaceAlt
            c.Caption = p.TextDisabled
            c.Glyph = p.TextDisabled
            c.Border = p.Border
            c.FocusRing = p.Border
            Return c
        End If
        c.Caption = p.Text
        c.Border = p.Border
        c.FocusRing = p.Accent
        Select Case faceValue
            Case GlyphButtonFace.AccentFill
                c.Back = p.Accent
                c.Caption = p.AccentText
                c.Glyph = p.AccentText
                c.Border = p.Accent
                c.FocusRing = p.AccentText
            Case GlyphButtonFace.DangerFill
                c.Back = p.Danger
                c.Caption = p.AccentText
                c.Glyph = p.AccentText
                c.Border = p.Danger
                c.FocusRing = p.AccentText
            Case GlyphButtonFace.AnswerOk
                ' An affirmative answer: the neutral face, the glyph in state.ok (BUTTONS 3.5).
                c.Back = p.SurfaceAlt
                c.Glyph = p.StateOk
            Case GlyphButtonFace.AnswerWarning
                ' A caution answer: the neutral face, the glyph in state.warning. The shared warning
                ' tone cannot always reach 3:1 on the light face; there the palette's own darker warning
                ' ink stands in - the dated registry exception, decided by WarningGlyphOn.
                c.Back = p.SurfaceAlt
                c.Glyph = Theme.WarningGlyphOn(p, p.SurfaceAlt)
            Case Else
                ' Normal, and the retired AnswerError / AnswerErrorSafe: the neutral face, mono ink.
                c.Back = p.SurfaceAlt
                c.Glyph = If(dangerValue, p.Danger, p.Text)
                c.Caption = c.Glyph
        End Select
        Return c
    End Function

    ' Every face but the accent and danger fills is the neutral one, which follows the pointer and the
    ' press; a filled face keeps its colour while it is worked (it is the button's rank).
    Private Function IsNeutralFace() As Boolean
        Return faceValue <> GlyphButtonFace.AccentFill AndAlso faceValue <> GlyphButtonFace.DangerFill
    End Function

    Private Sub Relayout()
        If Parent IsNot Nothing Then Parent.PerformLayout()
        Invalidate()
    End Sub

    ' A caption is never ellipsized or clipped (BUTTONS 2.0 section 3 rule 4): it is measured and drawn
    ' with word breaking, so when the width a layout gives the button (MaximumSize) is narrower than
    ' the caption the caption wraps and the button grows in height instead.
    Private Const CaptionFlags As TextFormatFlags = TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding Or
                                                    TextFormatFlags.WordBreak Or TextFormatFlags.Left Or
                                                    TextFormatFlags.Top

    Private Const BorderPx As Integer = 1

    Private Function GlyphPx() As Integer
        Return If(glyphValue Is Nothing AndAlso pictureValue Is Nothing, 0, Ui.Px(Me, UiTokens.Scaled(tierValue)))
    End Function

    Private Function PadXPx() As Integer
        Return Ui.Px(Me, UiTokens.ButtonPadX())
    End Function

    Private Function GapPx() As Integer
        Return Ui.Px(Me, UiTokens.GlyphGap())
    End Function

    ' The widest a caption may be before it wraps: from the width cap a layout set (MaximumSize), or
    ' unbounded. Never less than a few characters, so a degenerate cap cannot make a one-letter column.
    Private Function CaptionLimit() As Integer
        If MaximumSize.Width <= 0 Then Return Integer.MaxValue
        Dim gp = GlyphPx()
        Dim avail = MaximumSize.Width - 2 * BorderPx - 2 * PadXPx() - If(gp > 0, gp + GapPx(), 0)
        Return Math.Max(Ui.Px(Me, 40), avail)
    End Function

    Private Function CaptionSize(limit As Integer) As Size
        If iconOnlyValue OrElse String.IsNullOrEmpty(Text) Then Return Size.Empty
        Return TextRenderer.MeasureText(Text, Font, New Size(limit, Integer.MaxValue), CaptionFlags)
    End Function

    ' The smallest this button is allowed to be on either axis for its kind.
    Private Function TargetPx() As Integer
        Return Ui.Px(Me, UiTokens.Target())
    End Function

    Public Overrides Function GetPreferredSize(proposedSize As Size) As Size
        Dim gp = GlyphPx()
        Dim cap = CaptionSize(CaptionLimit())
        ' UI-SIZING rule 4 and BUTTONS section 3: the ordinary pointer target is 32 logical units and the
        ' height of a single-line button. The padding rounds down at some scalings, so the floor is
        ' stated here rather than left to the rounding. A multiline caption grows the height past it.
        Dim floorPx = TargetPx()
        If iconOnlyValue Then
            Dim side = Math.Max(gp + 2 * Ui.Px(Me, UiTokens.Scaled(5)) + 2 * BorderPx, floorPx)
            Return WithinLimits(New Size(side, side))
        End If
        Dim content = gp + cap.Width
        If gp > 0 AndAlso cap.Width > 0 Then content += GapPx()
        Dim w = Math.Max(content + 2 * PadXPx() + 2 * BorderPx, If(gp > 0 AndAlso cap.Width = 0, floorPx, 0))
        Dim h = Math.Max(Math.Max(gp, cap.Height) + 2 * Ui.Px(Me, UiTokens.Scaled(5)) + 2 * BorderPx, floorPx)
        Return WithinLimits(New Size(w, h))
    End Function

    ' The base class applies MaximumSize and MinimumSize to the size it computes; overriding
    ' GetPreferredSize bypasses that, so it is stated here - a layout that equalizes the heights of a
    ' row (Ui.EqualizeRowHeights) sets MinimumSize and the AutoSize button has to honour it.
    Private Function WithinLimits(size As Size) As Size
        Dim w = size.Width
        Dim h = size.Height
        If MaximumSize.Width > 0 Then w = Math.Min(w, MaximumSize.Width)
        If MaximumSize.Height > 0 Then h = Math.Min(h, MaximumSize.Height)
        Return New Size(Math.Max(w, MinimumSize.Width), Math.Max(h, MinimumSize.Height))
    End Function

    ' A rounded rectangle with the contract's modest radius (never a pill).
    Private Function FacePath(r As Rectangle) As Drawing2D.GraphicsPath
        Dim path As New Drawing2D.GraphicsPath()
        Dim d = Math.Max(2, 2 * Ui.Px(Me, UiTokens.Radius()))
        d = Math.Min(d, Math.Min(r.Width, r.Height))
        path.AddArc(r.Left, r.Top, d, d, 180, 90)
        path.AddArc(r.Right - d, r.Top, d, d, 270, 90)
        path.AddArc(r.Right - d, r.Bottom - d, d, d, 0, 90)
        path.AddArc(r.Left, r.Bottom - d, d, d, 90, 90)
        path.CloseFigure()
        Return path
    End Function

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim p = Theme.Current
        Dim g = e.Graphics
        Dim c = ColoursFor(p)
        ' Only the mono face follows the pointer and the press; a filled or tinted face keeps its
        ' colour while it is worked, because the fill is the button's rank (the accent of a primary
        ' action, an answer's hue) and would lose it exactly when the pointer confirms it.
        Dim back = c.Back
        If IsNeutralFace() Then
            If Enabled AndAlso pressed AndAlso hot Then
                back = p.SurfaceSelected
            ElseIf Enabled AndAlso hot Then
                back = p.ControlHover
            End If
        End If

        ' The corners outside the rounded face take the surface the button sits on, so a 4-radius
        ' rectangle is not a rectangle with four stray corner pixels of the control's own colour.
        Using b As New SolidBrush(If(Parent IsNot Nothing, Parent.BackColor, back))
            g.FillRectangle(b, ClientRectangle)
        End Using
        g.SmoothingMode = Drawing2D.SmoothingMode.AntiAlias
        Using path = FacePath(New Rectangle(0, 0, Width - 1, Height - 1))
            Using b As New SolidBrush(back)
                g.FillPath(b, path)
            End Using
            Using pen As New Pen(c.Border)
                g.DrawPath(pen, path)
            End Using
        End Using
        ' The keyboard's place, two pixels inside the border. A pointer click gives the button the
        ' focus without asking for a cue (ShowFocusCues is what Windows says is wanted).
        If Enabled AndAlso ((Focused AndAlso ShowFocusCues) OrElse defaultLook) Then
            Using path = FacePath(New Rectangle(1, 1, Width - 3, Height - 3))
                Using pen As New Pen(c.FocusRing, 2.0F)
                    g.DrawPath(pen, path)
                End Using
            End Using
        End If
        g.SmoothingMode = Drawing2D.SmoothingMode.Default

        Dim gp = GlyphPx()
        Dim gap = GapPx()
        ' The caption is laid out in the width the button actually has, so a button a layout made
        ' narrower than its caption wraps the caption (and the preferred size above grew its height)
        ' instead of cutting it.
        Dim capLimit = Math.Max(1, Width - 2 * BorderPx - 2 * PadXPx() - If(gp > 0, gp + gap, 0))
        Dim cap = CaptionSize(capLimit)
        Dim content = gp + cap.Width
        If gp > 0 AndAlso cap.Width > 0 Then content += gap
        Dim x = Math.Max(BorderPx, (Width - content) \ 2)
        If gp > 0 Then
            Dim square As New Rectangle(x, (Height - gp) \ 2, gp, gp)
            If glyphValue IsNot Nothing Then
                Glyphs.Draw(g, glyphValue, square, c.Glyph)
            ElseIf pictureValue IsNot Nothing Then
                g.DrawImage(pictureValue, square)
            End If
            x += gp + If(cap.Width > 0, gap, 0)
        End If
        If cap.Width > 0 Then
            TextRenderer.DrawText(g, Text, Font, New Rectangle(x, Math.Max(0, (Height - cap.Height) \ 2), cap.Width + 1, cap.Height),
                                  c.Caption, CaptionFlags)
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
