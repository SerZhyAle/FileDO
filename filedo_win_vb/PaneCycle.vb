' F6 and Shift+F6: the keyboard way from one area (pane) of a window to the next.
'
' INPUT-PARITY section 4 binds `move` to the arrow keys, Tab / Shift+Tab between controls, and F6
' "between panes"; section 3.2 rule 5 makes that table the binding, and rule 1 asks that nothing
' reachable by pointer is out of the keyboard's reach. FileDO's windows are made of areas that a Tab
' walk crosses only after every control in front of them (the rail alone is thirty stops), so F6 jumps
' to the next area and Shift+F6 to the previous one, wrapping at the ends.
'
' A pane is a host control; the focus goes to its preferred control when it can take it, otherwise to
' the first control in its tab order. A pane that is hidden, disabled or holds nothing that can take
' the focus is stepped over. The choice of the next pane is a pure function of a list of flags
' (NextIndex), so the self-test proves it on synthetic lists; the rest only looks at real controls.
Friend Class Pane
    Public ReadOnly Host As Control
    ' The control to land on when it is on screen and enabled inside Host (the page on screen in a list,
    ' the row of the current page), or Nothing for the first control in the tab order.
    Public ReadOnly Prefer As Func(Of Control)

    Public Sub New(host As Control, Optional prefer As Func(Of Control) = Nothing)
        Me.Host = host
        Me.Prefer = prefer
    End Sub
End Class

Friend Module PaneCycle

    ' The index of the pane F6 (or, backwards, Shift+F6) goes to, or -1 when no pane can take the
    ' focus. usable(i) is whether pane i can take it. current is the pane that has the focus, -1 when it
    ' is in none of them: forward then goes to the first usable pane and backwards to the last. The
    ' walk wraps, steps over unusable panes, and when the current pane is the only usable one it
    ' answers that pane - the key is taken and the focus stays where it is.
    Friend Function NextIndex(usable As IList(Of Boolean), current As Integer, backwards As Boolean) As Integer
        Dim n = usable.Count
        If n = 0 Then Return -1
        If current < 0 OrElse current >= n Then
            If backwards Then
                For i = n - 1 To 0 Step -1
                    If usable(i) Then Return i
                Next
            Else
                For i = 0 To n - 1
                    If usable(i) Then Return i
                Next
            End If
            Return -1
        End If
        Dim stepBy = If(backwards, -1, 1)
        For k = 1 To n
            Dim i = ((current + stepBy * k) Mod n + n) Mod n
            If usable(i) Then Return i
        Next
        Return -1
    End Function

    ' A control the keyboard can stop on: shown, enabled, in the tab order, and a control rather than a
    ' container of controls (a user control reports itself selectable, but the focus belongs to a leaf).
    Friend Function IsStop(c As Control) As Boolean
        Return c IsNot Nothing AndAlso c.Visible AndAlso c.Enabled AndAlso c.TabStop AndAlso c.CanSelect AndAlso Not TypeOf c Is ContainerControl
    End Function

    ' The first stop in the tab order at or under c (children by TabIndex, ties by their order in the
    ' collection, as the tab walk takes them); Nothing when c is hidden or disabled or holds none.
    Friend Function FirstStop(c As Control) As Control
        If c Is Nothing OrElse Not c.Visible OrElse Not c.Enabled Then Return Nothing
        If IsStop(c) Then Return c
        For Each child In c.Controls.Cast(Of Control)().OrderBy(Function(k) k.TabIndex)
            Dim found = FirstStop(child)
            If found IsNot Nothing Then Return found
        Next
        Return Nothing
    End Function

    ' Where the focus goes in a pane: its preferred control if that is a stop inside it, else the first stop.
    Friend Function FocusTarget(p As Pane) As Control
        If p Is Nothing OrElse p.Host Is Nothing OrElse Not p.Host.Visible OrElse Not p.Host.Enabled Then Return Nothing
        If p.Prefer IsNot Nothing Then
            Dim wanted = p.Prefer()
            If IsStop(wanted) AndAlso (wanted Is p.Host OrElse p.Host.Contains(wanted)) Then Return wanted
        End If
        Return FirstStop(p.Host)
    End Function

    ' The pane that holds the control with the focus, -1 when none does.
    Friend Function PaneOf(panes As IList(Of Pane), focused As Control) As Integer
        If focused Is Nothing Then Return -1
        For i = 0 To panes.Count - 1
            Dim host = panes(i).Host
            If host Is focused OrElse host.Contains(focused) Then Return i
        Next
        Return -1
    End Function

    ' The control F6 / Shift+F6 would focus from where the focus is now, or Nothing.
    Friend Function Target(panes As IList(Of Pane), focused As Control, backwards As Boolean) As Control
        Dim targets = panes.Select(Function(p) FocusTarget(p)).ToList()
        Dim at = NextIndex(targets.Select(Function(t) t IsNot Nothing).ToList(), PaneOf(panes, focused), backwards)
        Return If(at < 0, Nothing, targets(at))
    End Function

    ' The key itself: moves the focus within form and says whether it took the key. A window with no pane
    ' to go to leaves the key to the system.
    Friend Function Move(form As ContainerControl, panes As IList(Of Pane), backwards As Boolean) As Boolean
        Dim t = Target(panes, Help.FocusedControl(form), backwards)
        If t Is Nothing Then Return False
        t.Select()
        Return True
    End Function

End Module
