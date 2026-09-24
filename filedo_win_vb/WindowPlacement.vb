' Where a restored window goes (APP-BEHAVIOUR rule 10).
'
' A saved placement is the user's own decision, and it is put back as it stands whenever the screen
' it was saved on is still there. What this module adds is the defence for the other cases: a
' monitor that is gone, a window whose title strip has slid off every screen so it cannot be
' dragged back, and a rectangle saved at one scale and restored at another. Place is a pure
' function of (saved rectangle, saved DPI, current screens) - the self-test exercises it with
' invented screens, so the rule is checked without a display.
Imports System.Runtime.InteropServices

Module WindowPlacement

    Public Class ScreenArea
        Public ReadOnly Area As Rectangle
        Public ReadOnly Dpi As Integer

        Public Sub New(area As Rectangle, dpi As Integer)
            Me.Area = area
            Me.Dpi = If(dpi > 0, dpi, 96)
        End Sub
    End Class

    ' The rectangle to open at, or Rectangle.Empty when there is no screen at all to put it on (the
    ' caller then keeps its default size and centre).
    Public Function Place(saved As Rectangle, savedDpi As Integer, screens As IList(Of ScreenArea),
                          titleHeight As Integer) As Rectangle
        If screens Is Nothing OrElse screens.Count = 0 OrElse saved.Width <= 0 OrElse saved.Height <= 0 Then
            Return Rectangle.Empty
        End If

        ' The screen the window belongs to: the one holding most of it, or - when it is on none,
        ' because its monitor was unplugged - the nearest one.
        Dim target = MostOverlapping(saved, screens)
        If target Is Nothing Then target = Nearest(saved, screens)
        Dim wa = target.Area

        ' A rectangle saved at another scale keeps its size in design terms: 1000 pixels at 144 DPI
        ' is 667 at 96.
        Dim w = saved.Width
        Dim h = saved.Height
        If savedDpi > 0 AndAlso savedDpi <> target.Dpi Then
            w = CInt(Math.Round(w * target.Dpi / CDbl(savedDpi)))
            h = CInt(Math.Round(h * target.Dpi / CDbl(savedDpi)))
        End If
        w = Math.Min(w, wa.Width)
        h = Math.Min(h, wa.Height)

        ' The title strip - the full width, one caption high - is pulled back until it is wholly on
        ' the working area, which is what keeps the window draggable. A window on its own screen
        ' already is, and does not move.
        Dim strip = Math.Max(1, Math.Min(titleHeight, h))
        Dim x = Math.Min(Math.Max(saved.X, wa.Left), wa.Right - w)
        Dim y = Math.Min(Math.Max(saved.Y, wa.Top), wa.Bottom - strip)
        Return New Rectangle(x, y, w, h)
    End Function

    Private Function MostOverlapping(r As Rectangle, screens As IList(Of ScreenArea)) As ScreenArea
        Dim best As ScreenArea = Nothing
        Dim bestArea As Long = 0
        For Each s In screens
            Dim i = Rectangle.Intersect(r, s.Area)
            Dim a = CLng(i.Width) * CLng(i.Height)
            If a > bestArea Then
                bestArea = a
                best = s
            End If
        Next
        Return best
    End Function

    Private Function Nearest(r As Rectangle, screens As IList(Of ScreenArea)) As ScreenArea
        Dim cx = r.Left + r.Width \ 2
        Dim cy = r.Top + r.Height \ 2
        Dim best As ScreenArea = Nothing
        Dim bestD As Double = Double.MaxValue
        For Each s In screens
            Dim dx = Math.Max(0, Math.Max(s.Area.Left - cx, cx - s.Area.Right))
            Dim dy = Math.Max(0, Math.Max(s.Area.Top - cy, cy - s.Area.Bottom))
            Dim d = Math.Sqrt(CDbl(dx) * dx + CDbl(dy) * dy)
            If d < bestD Then
                bestD = d
                best = s
            End If
        Next
        Return best
    End Function

    ' ---- the DPI of a real screen ----------------------------------------

    <DllImport("user32.dll")>
    Private Function MonitorFromPoint(pt As Point, flags As UInteger) As IntPtr
    End Function

    <DllImport("shcore.dll")>
    Private Function GetDpiForMonitor(hmonitor As IntPtr, dpiType As Integer, ByRef dpiX As UInteger,
                                      ByRef dpiY As UInteger) As Integer
    End Function

    ' The effective DPI of a screen, or 96 where the call does not exist (before Windows 8.1).
    Public Function DpiOf(s As Screen) As Integer
        Try
            Dim centre As New Point(s.Bounds.Left + s.Bounds.Width \ 2, s.Bounds.Top + s.Bounds.Height \ 2)
            Dim mon = MonitorFromPoint(centre, 2UI)   ' MONITOR_DEFAULTTONEAREST
            Dim dx, dy As UInteger
            If GetDpiForMonitor(mon, 0, dx, dy) = 0 AndAlso dx > 0 Then Return CInt(dx)
        Catch
        End Try
        Return 96
    End Function

End Module
