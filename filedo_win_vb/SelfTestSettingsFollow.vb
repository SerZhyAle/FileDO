' APP-SETTINGS 0.7 rule 1 (SP-0171 W12): the settings window is "brought to the monitor the user is
' working on". The decision is a pure function of rectangles (WindowPlacement.ScreenToFollow and
' CentreOn), so it is checked here with invented screens: two of them, a window spread over both, and
' a monitor that has been unplugged. The real windows are only checked for the part that needs no
' second display - that a window on another screen is brought and one already there is left alone.
' Dragging a window between two physical displays of different scaling is NOT claimed by these rows.
'
' Rows are named `settings-follow:`; FILEDO_SELFTEST_ONLY=settings-follow runs just these.
Partial Public Module SelfTest

    Private Sub CheckSettingsFollow()
        CheckSettingsFollowDecision()
        CheckSettingsFollowCentre()
        CheckSettingsFollowWindow()
    End Sub

    Private Sub CheckSettingsFollowDecision()
        Dim primary As New WindowPlacement.ScreenArea(New Rectangle(0, 0, 1920, 1040), 96)
        Dim second As New WindowPlacement.ScreenArea(New Rectangle(1920, 0, 2560, 1400), 144)
        Dim leftScreen As New WindowPlacement.ScreenArea(New Rectangle(-1920, 0, 1920, 1080), 96)
        Dim both = New List(Of WindowPlacement.ScreenArea) From {primary, second}
        Dim onlyPrimary = New List(Of WindowPlacement.ScreenArea) From {primary}
        Dim onPrimary As New Point(500, 500)
        Dim onSecond As New Point(3000, 600)

        ' The window the user is in decides, whatever the pointer says.
        Check("settings-follow:window-on-second", WindowPlacement.ScreenToFollow(New Rectangle(2000, 100, 1200, 800), onPrimary, both) Is second, "")
        Check("settings-follow:window-on-primary", WindowPlacement.ScreenToFollow(New Rectangle(100, 100, 1200, 800), onSecond, both) Is primary, "")

        ' Spread over both: the screen holding most of it.
        Check("settings-follow:straddle-mostly-second", WindowPlacement.ScreenToFollow(New Rectangle(1500, 100, 1200, 800), onPrimary, both) Is second, "")
        Check("settings-follow:straddle-mostly-primary", WindowPlacement.ScreenToFollow(New Rectangle(1100, 100, 1200, 800), onSecond, both) Is primary, "")

        ' Spread exactly evenly (600 pixels either side of the boundary): the pointer breaks the tie, and
        ' with no pointer the first screen in the list does.
        Dim even As New Rectangle(1320, 100, 1200, 800)
        Check("settings-follow:tie-pointer-second", WindowPlacement.ScreenToFollow(even, onSecond, both) Is second, "")
        Check("settings-follow:tie-pointer-primary", WindowPlacement.ScreenToFollow(even, onPrimary, both) Is primary, "")
        Check("settings-follow:tie-no-pointer", WindowPlacement.ScreenToFollow(even, Nothing, both) Is primary, "")

        ' A monitor on the left of the primary one (negative coordinates).
        Dim three = New List(Of WindowPlacement.ScreenArea) From {primary, leftScreen, second}
        Check("settings-follow:left-monitor", WindowPlacement.ScreenToFollow(New Rectangle(-1500, 100, 1000, 700), onSecond, three) Is leftScreen, "")

        ' The monitor the window was last on is gone: its rectangle is on no screen, so the pointer
        ' decides; with no pointer it is the nearest screen.
        Dim stale As New Rectangle(4700, 100, 1200, 800)
        Check("settings-follow:closed-monitor-pointer", WindowPlacement.ScreenToFollow(stale, onPrimary, both) Is primary, "")
        Check("settings-follow:closed-monitor-no-pointer", WindowPlacement.ScreenToFollow(stale, Nothing, both) Is second, "")
        Check("settings-follow:closed-monitor-only-one-left", WindowPlacement.ScreenToFollow(New Rectangle(2200, 200, 1200, 800), onPrimary, onlyPrimary) Is primary AndAlso
                                                            WindowPlacement.ScreenToFollow(New Rectangle(2200, 200, 1200, 800), Nothing, onlyPrimary) Is primary, "")

        ' No window (the tray menu): the pointer. On the taskbar it is below every working area, and is
        ' counted for the screen it is nearest to.
        Check("settings-follow:pointer-second", WindowPlacement.ScreenToFollow(Nothing, onSecond, both) Is second, "")
        Check("settings-follow:pointer-on-taskbar", WindowPlacement.ScreenToFollow(Nothing, New Point(500, 1060), both) Is primary AndAlso
                                                    WindowPlacement.ScreenToFollow(Nothing, New Point(3000, 1410), both) Is second, "")
        Check("settings-follow:empty-window-uses-pointer", WindowPlacement.ScreenToFollow(New Rectangle(100, 100, 0, 0), onSecond, both) Is second, "")

        ' Nothing to follow: say so, and leave the window where it is.
        Check("settings-follow:nothing-to-follow", WindowPlacement.ScreenToFollow(Nothing, Nothing, both) Is Nothing, "")
        Check("settings-follow:no-screens", WindowPlacement.ScreenToFollow(New Rectangle(0, 0, 800, 600), onPrimary, New List(Of WindowPlacement.ScreenArea)()) Is Nothing AndAlso
                                            WindowPlacement.ScreenToFollow(Nothing, Nothing, Nothing) Is Nothing, "")
    End Sub

    ' Where a window lands on the screen it is brought to.
    Private Sub CheckSettingsFollowCentre()
        Dim r = WindowPlacement.CentreOn(New Size(900, 600), New Rectangle(1920, 0, 2560, 1400))
        Check("settings-follow:centred", r = New Rectangle(2750, 400, 900, 600), r.ToString())
        r = WindowPlacement.CentreOn(New Size(800, 600), New Rectangle(-1920, 0, 1920, 1080))
        Check("settings-follow:centred-negative-origin", r = New Rectangle(-1360, 240, 800, 600), r.ToString())
        r = WindowPlacement.CentreOn(New Size(4000, 3000), New Rectangle(0, 0, 1920, 1040))
        Check("settings-follow:no-larger-than-the-screen", r = New Rectangle(0, 0, 1920, 1040), r.ToString())
        r = WindowPlacement.CentreOn(New Size(1200, 800), New Rectangle(0, 0, 1366, 720))
        Check("settings-follow:fits-a-smaller-screen", New Rectangle(0, 0, 1366, 720).Contains(r) AndAlso r.Height = 720, r.ToString())
    End Sub

    ' The real window: brought to a screen it is not on, left alone on the one it is on, untouched when
    ' nobody asks. The "other" screen is invented (off the right edge of this machine's), so no second
    ' display is needed.
    Private Sub CheckSettingsFollowWindow()
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            Dim wa = Screen.PrimaryScreen.WorkingArea
            shell.Location = New Point(wa.Left + 20, wa.Top + 20)
            Dim before = shell.Bounds

            Check("settings-follow:window-nothing-given", Not shell.MoveToScreen(Nothing) AndAlso shell.Bounds = before, shell.Bounds.ToString())

            Dim here As New WindowPlacement.ScreenArea(wa, 96)
            Check("settings-follow:window-already-there", Not shell.MoveToScreen(here) AndAlso shell.Bounds = before, shell.Bounds.ToString())

            Dim elsewhere As New WindowPlacement.ScreenArea(New Rectangle(wa.Right + 100, wa.Top, 1600, 1000), 96)
            Dim moved = shell.MoveToScreen(elsewhere)
            Dim want = WindowPlacement.CentreOn(before.Size, elsewhere.Area)
            Check("settings-follow:window-brought", moved AndAlso shell.Location = want.Location, shell.Bounds.ToString() & " want " & want.ToString())
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

End Module
