' The left rail - the whole navigation of the shell (SP-0006 section 5.1).
'
' Two rules from section 11 shape this file more than anything else:
'
'   Item 6, flat and owner-drawn. WinForms' own button draws a 3D border and a grey face that no
'   amount of colour tokens can hide, so the entries paint themselves. Double buffering is on
'   because a rail that flickers on resize reads as cheap however good the palette is.
'
'   Item 8, accessibility is part of "good-looking". Every entry is a real focusable control with
'   an AccessibleName, the focus ring is drawn, and **selection is never colour alone**: the
'   selected entry carries an accent bar on its left edge and a bold label, so it is still the
'   selected entry in a screenshot printed in grey.
'
' The rail's content is SP-0006 section 5.2, with one correction from the owner: the rows name the
' job, they do not ask the reader a question. "Is this drive honest?" is a line of copy; "Capacity
' test (fake drives)" is what the row runs, and it is the shorter of the two in every locale.

' One row of the rail. A group header is a RailEntry too, drawn differently - and, since the groups
' collapse, it is a control the user operates: it is focusable, it answers Space and Enter like any
' other row, and it draws a chevron that says whether the group is open or shut.
Public Class RailEntry
    Inherits Control

    Public Property Glyph As String = ""
    Public Property Key As String = ""          ' the localization key, kept for a relayout
    Public Property IsGroupHeader As Boolean = False

    Private selectedValue As Boolean = False
    Private collapsedValue As Boolean = False
    Private hasSelectedChildValue As Boolean = False
    Private hot As Boolean = False

    ' Group header only: whether its rows are hidden. The chevron follows it.
    Public Property Collapsed As Boolean
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

    ' Group header only: the selected row is inside this group. When the group is shut, the header
    ' is the only thing left on screen that can say where the user is - so it shows the accent bar
    ' the hidden row would have shown.
    Public Property HasSelectedChild As Boolean
        Get
            Return hasSelectedChildValue
        End Get
        Set(value As Boolean)
            If hasSelectedChildValue <> value Then
                hasSelectedChildValue = value
                Invalidate()
            End If
        End Set
    End Property

    Public Property Selected As Boolean
        Get
            Return selectedValue
        End Get
        Set(value As Boolean)
            If selectedValue <> value Then
                selectedValue = value
                Invalidate()
            End If
        End Set
    End Property

    Public Sub New()
        SetStyle(ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or
                 ControlStyles.ResizeRedraw Or
                 ControlStyles.UserPaint, True)
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

    ' Space and Enter choose the entry, because a rail that can be reached with Tab and not used
    ' with the keyboard is worse than one that cannot be reached at all.
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

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim p = Theme.Current
        Dim g = e.Graphics
        g.TextRenderingHint = Drawing.Text.TextRenderingHint.ClearTypeGridFit

        Using b As New SolidBrush(p.SurfaceAlt)
            g.FillRectangle(b, ClientRectangle)
        End Using

        If IsGroupHeader Then
            If hot Then
                Using b As New SolidBrush(Theme.Blend(p.SurfaceAlt, p.Text, 0.07F))
                    g.FillRectangle(b, ClientRectangle)
                End Using
            End If

            ' A shut group still has to say that the current job is inside it, and it says so the
            ' same way a selected row does - an accent bar, not a colour on the text.
            If Collapsed AndAlso HasSelectedChild Then
                Dim barHeight = CInt(Height * 0.55)
                Using b As New SolidBrush(p.Accent)
                    g.FillRectangle(b, New Rectangle(0, (Height - barHeight) \ 2,
                                                     Math.Max(3, CInt(Width * 0.015)), barHeight))
                End Using
            End If

            Dim chevronWidth = CInt(Height * 0.9)
            Using f = Theme.FontChevron()
                TextRenderer.DrawText(g, Theme.ChevronGlyph(Collapsed), f,
                                      New Rectangle(CInt(Height * 0.15), 0, chevronWidth, Height),
                                      p.MutedText,
                                      TextFormatFlags.HorizontalCenter Or TextFormatFlags.VerticalCenter Or
                                      TextFormatFlags.NoPrefix)
            End Using

            Dim headerLeft = CInt(Height * 0.15) + chevronWidth
            TextRenderer.DrawText(g, Text, Theme.FontCaption(),
                                  New Rectangle(headerLeft, 0, Width - headerLeft - 4, Height),
                                  p.MutedText,
                                  TextFormatFlags.Left Or TextFormatFlags.VerticalCenter Or
                                  TextFormatFlags.EndEllipsis Or TextFormatFlags.NoPrefix)

            If Focused Then
                Using pen As New Pen(p.Accent, 2.0F)
                    Dim hr = ClientRectangle
                    hr.Inflate(-2, -2)
                    g.DrawRectangle(pen, hr)
                End Using
            End If
            Return
        End If

        ' The row's own background: selected, hot, or nothing at all.
        If Selected Then
            Using b As New SolidBrush(Theme.Blend(p.SurfaceAlt, p.Accent, If(p.IsDark, 0.22F, 0.14F)))
                g.FillRectangle(b, ClientRectangle)
            End Using
        ElseIf hot Then
            Using b As New SolidBrush(Theme.Blend(p.SurfaceAlt, p.Text, 0.07F))
                g.FillRectangle(b, ClientRectangle)
            End Using
        End If

        ' The accent bar. This is the part that survives a grey print-out, and it is why selection
        ' does not depend on the fill above.
        If Selected Then
            Dim barHeight = CInt(Height * 0.55)
            Dim bar As New Rectangle(0, (Height - barHeight) \ 2, Math.Max(3, CInt(Width * 0.015)), barHeight)
            Using b As New SolidBrush(p.Accent)
                g.FillRectangle(b, bar)
            End Using
        End If

        Dim fore = p.Text
        Dim glyphWidth = CInt(Height * 1.1)

        If Glyph <> "" Then
            TextRenderer.DrawText(g, Theme.Glyph(Glyph), Theme.FontGlyph(),
                                  New Rectangle(CInt(Height * 0.2), 0, glyphWidth, Height), fore,
                                  TextFormatFlags.Left Or TextFormatFlags.VerticalCenter Or
                                  TextFormatFlags.NoPrefix)
        End If

        Dim labelFont = If(Selected, Theme.FontBodyStrong(), Theme.FontBody())
        Dim labelRect As New Rectangle(CInt(Height * 0.2) + glyphWidth, 0,
                                       Width - (CInt(Height * 0.2) + glyphWidth) - 4, Height)
        TextRenderer.DrawText(g, Text, labelFont, labelRect, fore,
                              TextFormatFlags.Left Or TextFormatFlags.VerticalCenter Or
                              TextFormatFlags.EndEllipsis Or TextFormatFlags.NoPrefix)

        ' The focus ring, drawn rather than inherited: WinForms' dotted rectangle is invisible on a
        ' dark surface.
        If Focused Then
            Using pen As New Pen(p.Accent, 2.0F)
                Dim r = ClientRectangle
                r.Inflate(-2, -2)
                g.DrawRectangle(pen, r)
            End Using
        End If
    End Sub

End Class
