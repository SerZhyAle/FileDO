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

        ' A window larger than the screen it sits on - a size saved on a bigger monitor, a minimum
        ' stated for another scaling - is brought wholly onto that screen: no larger than its working
        ' area, and moved until every edge is on it, so the title strip and the buttons at the bottom
        ' are both reachable. Only a window that really lies across two monitors is left to SHELL-08.
        If (w > wa.Width OrElse h > wa.Height) AndAlso ScreensTouched(New Rectangle(saved.X, saved.Y, w, h), screens) < 2 Then
            Return FitInside(saved.X, saved.Y, w, h, wa)
        End If

        ' SHELL-08: a window the user spread across two monitors is theirs to keep that way. It is
        ' clamped to the space all the screens make together, and it stays where it is as long as a
        ' piece of its title strip can still be grabbed on some screen.
        Dim all = UnionOf(screens)
        Dim keptW = Math.Min(w, all.Width)
        Dim keptH = Math.Min(h, all.Height)
        If StripReachable(New Rectangle(saved.X, saved.Y, keptW, Math.Max(1, Math.Min(titleHeight, keptH))), screens, titleHeight) Then
            If keptW <> w OrElse keptH <> h Then Return FitInside(saved.X, saved.Y, keptW, keptH, all)
            Return New Rectangle(saved.X, saved.Y, keptW, keptH)
        End If

        ' Otherwise it moves onto the screen it belongs to, at no more than that screen's size, and
        ' the title strip - the full width, one caption high - is pulled back until it is wholly on
        ' the working area, which is what keeps the window draggable.
        w = Math.Min(w, wa.Width)
        h = Math.Min(h, wa.Height)
        Dim strip = Math.Max(1, Math.Min(titleHeight, h))
        Dim x = Math.Min(Math.Max(saved.X, wa.Left), wa.Right - w)
        Dim y = Math.Min(Math.Max(saved.Y, wa.Top), wa.Bottom - strip)
        Return New Rectangle(x, y, w, h)
    End Function

    ' A rectangle of at most the area's size, as near the wanted corner as keeps all of it inside.
    Private Function FitInside(x As Integer, y As Integer, w As Integer, h As Integer, area As Rectangle) As Rectangle
        Dim fw = Math.Min(w, area.Width)
        Dim fh = Math.Min(h, area.Height)
        Return New Rectangle(Math.Min(Math.Max(x, area.Left), area.Right - fw),
                             Math.Min(Math.Max(y, area.Top), area.Bottom - fh), fw, fh)
    End Function

    ' How many screens the rectangle overlaps at all.
    Private Function ScreensTouched(r As Rectangle, screens As IList(Of ScreenArea)) As Integer
        Dim n = 0
        For Each s In screens
            Dim i = Rectangle.Intersect(r, s.Area)
            If i.Width > 0 AndAlso i.Height > 0 Then n += 1
        Next
        Return n
    End Function

    ' ---- the smallest a window may be made -------------------------------

    ' A minimum size is a design size, and no window may demand more than the screen it is on can
    ' show: a form whose MinimumSize is larger than the working area has a title strip or a bottom
    ' row of buttons that no drag can bring into view. Where the screen is smaller than the design
    ' minimum (a 1366x768 laptop at 150 %, a 1080p panel at 200 %), the minimum is the working area
    ' and the page inside scrolls instead (compact use of the screen, UI_UX).
    Public Function FitMinimum(wanted As Size, work As Size) As Size
        Return New Size(Math.Min(wanted.Width, Math.Max(1, work.Width)), Math.Min(wanted.Height, Math.Max(1, work.Height)))
    End Function

    ' The minimum for a form, from its design size, at the scale of the window it is for and held to
    ' the working area of the screen it is on - or, before it has a handle, of the primary screen,
    ' which is where a new window opens. Called again once the window is shown and when it changes
    ' monitor, because both change the answer.
    Public Function MinimumFor(form As Form, designWidth As Integer, designHeight As Integer) As Size
        Dim work As Rectangle
        Try
            work = If(form IsNot Nothing AndAlso form.IsHandleCreated, Screen.FromHandle(form.Handle), Screen.PrimaryScreen).WorkingArea
        Catch
            work = Screen.PrimaryScreen.WorkingArea
        End Try
        Return FitMinimum(Ui.PxSize(form, designWidth, designHeight), work.Size)
    End Function

    Private Function UnionOf(screens As IList(Of ScreenArea)) As Rectangle
        Dim r = screens(0).Area
        For Each s In screens
            r = Rectangle.Union(r, s.Area)
        Next
        Return r
    End Function

    ' A title strip is reachable when some screen shows the whole of its height over a width a
    ' pointer can grab - three captions, or the whole strip when it is narrower than that.
    Private Function StripReachable(strip As Rectangle, screens As IList(Of ScreenArea), titleHeight As Integer) As Boolean
        Dim grab = Math.Min(strip.Width, Math.Max(1, titleHeight) * 3)
        For Each s In screens
            Dim i = Rectangle.Intersect(strip, s.Area)
            If i.Height >= strip.Height AndAlso i.Width >= grab Then Return True
        Next
        Return False
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
