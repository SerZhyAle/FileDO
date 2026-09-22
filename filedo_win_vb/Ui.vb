' Layout helpers shared by every shell view.
'
' Two problems in WinForms make a window look broken long before the palette does, and both are
' solved here once instead of in every view:
'
'   1. **A number written in code is not a number on the screen.** A 224-pixel rail is 224 real
'      pixels at 100% and 224 real pixels at 200%, while the font inside it doubles - so the label
'      is clipped exactly on the display the user bought the program for. Every fixed size in the
'      shell goes through Px(), which turns a design pixel into a device pixel for the monitor the
'      control is currently on (SP-0006 section 11 item 1).
'
'   2. **AutoSize labels do not wrap.** An AutoSize label grows sideways for ever, so a sentence
'      that is short in English and long in German leaves the card rather than wrapping inside it.
'      Wrap() binds a label to the width it is actually given, which is what makes every string
'      visible in all five locales instead of only in the one it was written in.
Module Ui

    ' A design pixel, taken at 96 DPI, in the device units of the monitor this control is on.
    Public Function Px(owner As Control, designPixels As Integer) As Integer
        If owner Is Nothing Then Return designPixels
        Try
            Return owner.LogicalToDeviceUnits(designPixels)
        Catch
            Return designPixels
        End Try
    End Function

    Public Function PxSize(owner As Control, w As Integer, h As Integer) As Size
        Return New Size(Px(owner, w), Px(owner, h))
    End Function

    Public Function PxPad(owner As Control, l As Integer, t As Integer, r As Integer, b As Integer) As Padding
        Return New Padding(Px(owner, l), Px(owner, t), Px(owner, r), Px(owner, b))
    End Function

    ' Makes a label wrap inside whatever width its container ends up with, and keep its full text.
    '
    ' The label stays AutoSize - that is what lets a card grow to fit its content - but its maximum
    ' width is re-bound every time the container is resized, which is what turns growth sideways
    ' into growth downwards. A label that is never given a width (a zero-width container during
    ' construction) is left alone rather than collapsed to one character per line.
    Public Sub Wrap(label As Label, container As Control, Optional reserve As Integer = 0)
        If label Is Nothing OrElse container Is Nothing Then Return
        label.AutoSize = True
        Dim apply = Sub()
                        Dim avail = container.ClientSize.Width - reserve - label.Margin.Horizontal
                        If avail > Px(label, 80) Then
                            label.MaximumSize = New Size(avail, 0)
                        End If
                    End Sub
        AddHandler container.SizeChanged, Sub() apply()
        apply()
    End Sub

    ' The numbered heading of a step. The number is part of the text rather than a separate glyph,
    ' so a screen reader reads "Step 2 - what to work on" as one phrase.
    Public Function StepHeader(owner As Control, text As String) As Label
        Dim l As New Label With {
            .Text = text,
            .AutoSize = True,
            .Margin = PxPad(owner, 0, 0, 0, 8)
        }
        l.AccessibleRole = AccessibleRole.StaticText
        Return l
    End Function

    ' A flat button in the house style. FlatStyle.Flat alone still draws a border in the system
    ' colour, so the border is set here too - otherwise a dark theme shows a light rectangle.
    Public Sub StyleButton(b As Button, back As Color, fore As Color, border As Color)
        If b Is Nothing Then Return
        b.FlatStyle = FlatStyle.Flat
        b.FlatAppearance.BorderSize = 1
        b.FlatAppearance.BorderColor = border
        b.BackColor = back
        b.ForeColor = fore
        b.UseVisualStyleBackColor = False
    End Sub

End Module

' A card: the flat surface every section of a page sits on. Owner-drawn rather than a GroupBox,
' because a system group box draws a 3D frame that no palette can hide (SP-0006 section 11 item 6).
'
' The accent bar on the left edge is how a destructive section stays distinguishable when the
' colour is ignored - a printed screenshot, a colour-blind user, a high-contrast theme (item 8).
Public Class ShellCard
    Inherits Panel

    Public Property BorderColour As Color = Color.Transparent
    Public Property Accented As Boolean = False
    Public Property AccentColour As Color = Color.Transparent

    Public Sub New()
        SetStyle(ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or
                 ControlStyles.ResizeRedraw Or
                 ControlStyles.UserPaint, True)
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        Dock = DockStyle.Top
    End Sub

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim g = e.Graphics
        Using b As New SolidBrush(BackColor)
            g.FillRectangle(b, ClientRectangle)
        End Using
        If Accented Then
            Using b As New SolidBrush(AccentColour)
                g.FillRectangle(b, New Rectangle(0, 0, Ui.Px(Me, 3), Height))
            End Using
        End If
        If BorderColour <> Color.Transparent Then
            Using pen As New Pen(BorderColour, 1.0F)
                g.DrawRectangle(pen, New Rectangle(0, 0, Width - 1, Height - 1))
            End Using
        End If
        MyBase.OnPaint(e)
    End Sub

End Class
