' The shell's half of HKCU\Software\FileDO.
'
' The key already held GuiLang, written by the command builder that shipped before the shell - so
' this milestone extended a store rather than inventing one (SP-0006 M0 tactics, finding 2.2). Everything here is
' best-effort: a settings read that throws must never stop the window from opening, so every path
' has a defensible default and every failure is swallowed deliberately rather than by accident.
'
' Nothing in this module may be a credential. The store is plain registry values under the user's
' own key, readable by anything that runs as the user - which is correct for a window position and
' wrong for anything secret (SP-0006 section 7.2 says where credentials go instead).
Imports Microsoft.Win32

Module ShellSettings

    Private Const KeyPath As String = "Software\FileDO"

    ' Bumped when a stored placement has to be retired rather than restored - see LoadPlacement.
    Private Const PlacementVersion As Integer = 3

    Friend ValuesForTest As Dictionary(Of String, Object)
    Friend Event ValuesChanged()

    Private Function ReadValue(name As String) As Object
        If ValuesForTest IsNot Nothing Then
            Dim value As Object = Nothing
            ValuesForTest.TryGetValue(name, value)
            Return value
        End If
        Try
            Using k = Registry.CurrentUser.OpenSubKey(KeyPath)
                If k Is Nothing Then Return Nothing
                Return k.GetValue(name)
            End Using
        Catch
            Return Nothing
        End Try
    End Function

    Private Sub WriteValue(name As String, value As Object)
        If ValuesForTest IsNot Nothing Then
            ValuesForTest(name) = value
            RaiseEvent ValuesChanged()
            Return
        End If
        Try
            Using k = Registry.CurrentUser.CreateSubKey(KeyPath)
                If k IsNot Nothing Then k.SetValue(name, value)
            End Using
            RaiseEvent ValuesChanged()
        Catch
        End Try
    End Sub

    Public Function Autostart() As String
        Return StartupLaunch.Normalize(TryCast(ReadValue("ShellAutostart"), String))
    End Function
    Public Sub SetAutostart(choice As String)
        WriteValue("ShellAutostart", StartupLaunch.Normalize(choice))
    End Sub
    Public Function MinimizeToTray() As Boolean
        Dim v = ReadValue("ShellMinimizeToTray")
        Return TypeOf v Is Integer AndAlso CInt(v) <> 0
    End Function
    Public Sub SetMinimizeToTray(value As Boolean)
        WriteValue("ShellMinimizeToTray", If(value, 1, 0))
    End Sub
    Public Function FinishNotification() As Boolean
        Dim v = ReadValue("ShellFinishNotification")
        Return Not TypeOf v Is Integer OrElse CInt(v) <> 0
    End Function
    Public Sub SetFinishNotification(value As Boolean)
        WriteValue("ShellFinishNotification", If(value, 1, 0))
    End Sub
    ' Placement stamps only: no user data is removed. The next opening uses its default geometry.
    Private ReadOnly resetPrefixes As New HashSet(Of String)()
    Public Sub ResetPlacements()
        resetPrefixes.Add(ShellPrefix)
        resetPrefixes.Add(DiskManagerPrefix)
        WriteValue(ShellPrefix & "PlacementV", 0)
        WriteValue(DiskManagerPrefix & "PlacementV", 0)
    End Sub

    ' ---- theme -----------------------------------------------------------

    ' "auto" follows Windows, which is the default and what most users never change.
    Public Function ThemeChoice() As String
        Dim v = TryCast(ReadValue("ShellTheme"), String)
        If v Is Nothing Then Return "auto"
        Select Case v.ToLowerInvariant()
            Case "light", "dark", "auto" : Return v.ToLowerInvariant()
            Case Else : Return "auto"
        End Select
    End Function

    Public Sub SetThemeChoice(choice As String)
        WriteValue("ShellTheme", choice)
        Theme.Refresh()
    End Sub

    ' ---- run history -----------------------------------------------------

    ' D8's second half, the owner's addition to the recommendation: automatic run reports are on by
    ' default and silent, and turning them off is a settings action rather than a per-run question.
    ' A missing value means on, so an install that has never seen this switch behaves as specified.
    Public Function HistoryEnabled() As Boolean
        Dim v = ReadValue("ShellHistory")
        If TypeOf v Is Integer Then Return CInt(v) <> 0
        Return True
    End Function

    Public Sub SetHistoryEnabled(enabled As Boolean)
        WriteValue("ShellHistory", If(enabled, 1, 0))
    End Sub

    ' ---- rail groups -----------------------------------------------------

    ' Which rail groups the user left collapsed, by localization key, semicolon-separated. A
    ' missing value means every group is open, which is how the rail behaved before it could fold
    ' at all - so an install that never touches a chevron sees no change.
    Public Function CollapsedGroups() As HashSet(Of String)
        Dim set_ As New HashSet(Of String)(StringComparer.Ordinal)
        Dim v = TryCast(ReadValue("ShellRailCollapsed"), String)
        If String.IsNullOrEmpty(v) Then Return set_
        For Each part In v.Split(";"c)
            Dim k = part.Trim()
            If k <> "" Then set_.Add(k)
        Next
        Return set_
    End Function

    Public Sub SetCollapsedGroups(keys As IEnumerable(Of String))
        WriteValue("ShellRailCollapsed", String.Join(";", keys))
    End Sub

    ' ---- window placement ------------------------------------------------

    Public Structure Placement
        Public HasValue As Boolean
        Public X As Integer
        Public Y As Integer
        Public Width As Integer
        Public Height As Integer
        Public Maximized As Boolean
        Public Dpi As Integer          ' the DPI of the monitor it was saved on; 0 when unknown
    End Structure

    Private Function ReadInt(name As String, ByRef into As Integer) As Boolean
        Dim v = ReadValue(name)
        If TypeOf v Is Integer Then
            into = CInt(v)
            Return True
        End If
        Return False
    End Function

    ' A saved placement is the user's own decision and is restored as it stands - with one
    ' exception, and the stamp below is what makes it exactly one. Until now the window opened at a
    ' fixed 1180x780, which is a small window on the displays this program is actually used on, and
    ' every installation has that size saved from its first run: keeping it forever would mean the
    ' complaint could only be fixed for people who had never opened the program. The stamp retires
    ' those placements once; anything saved by this build carries the current number and is
    ' restored untouched, however small the user made it.
    '
    ' Version 3 (SP-0014 T13) adds ShellDpi, the DPI the rectangle was saved at, so a placement
    ' restored on a monitor of another scale is scaled rather than taken literally. A version 2
    ' placement has no DPI to scale by, so it is retired once by the same rule.
    Public Function LoadPlacement() As Placement
        Return LoadPlacementOf(ShellPrefix)
    End Function

    ' The shell's values are Shell*; the Disk Manager keeps its own beside them, DiskManager*
    ' (SP-0063 4.1), under the same stamp and the same sanity rule.
    Friend Const ShellPrefix As String = "Shell"
    Friend Const DiskManagerPrefix As String = "DiskManager"

    Public Function LoadPlacementOf(prefix As String) As Placement
        resetPrefixes.Remove(prefix)
        Dim p As New Placement With {.HasValue = False}
        Dim stamp As Integer
        If Not ReadInt(prefix & "PlacementV", stamp) OrElse stamp <> PlacementVersion Then Return p

        Dim x, y, w, h As Integer
        If Not (ReadInt(prefix & "X", x) AndAlso ReadInt(prefix & "Y", y) AndAlso
                ReadInt(prefix & "W", w) AndAlso ReadInt(prefix & "H", h)) Then Return p
        Dim d As Integer = 0
        ReadInt(prefix & "Dpi", d)
        If Not PlacementIsSane(x, y, w, h, d) Then Return p

        p.X = x : p.Y = y : p.Width = w : p.Height = h
        p.HasValue = True
        Dim m As Integer
        If ReadInt(prefix & "Max", m) Then p.Maximized = (m <> 0)
        If d > 0 Then p.Dpi = d
        Return p
    End Function

    ' SHELL-09: the values come from the user's registry, and a hand edit or a broken writer can put
    ' anything there. A value no real window has is not restored - scaling ShellX 2147483000, or a
    ' width by ShellDpi 1, overflowed in the window's constructor and kept it from opening on every
    ' start, until somebody found the key and edited it by hand. A missing DPI (0) is allowed.
    Friend Function PlacementIsSane(x As Integer, y As Integer, w As Integer, h As Integer, dpi As Integer) As Boolean
        If Math.Abs(CLng(x)) >= 100000 OrElse Math.Abs(CLng(y)) >= 100000 Then Return False
        If w <= 0 OrElse h <= 0 OrElse w >= 100000 OrElse h >= 100000 Then Return False
        If dpi <> 0 AndAlso (dpi < 48 OrElse dpi > 960) Then Return False
        Return True
    End Function

    Public Sub SavePlacement(x As Integer, y As Integer, width As Integer, height As Integer, maximized As Boolean, dpi As Integer)
        SavePlacementOf(ShellPrefix, x, y, width, height, maximized, dpi)
    End Sub

    Public Sub SavePlacementOf(prefix As String, x As Integer, y As Integer, width As Integer, height As Integer,
                               maximized As Boolean, dpi As Integer)
        If resetPrefixes.Contains(prefix) Then Return
        WriteValue(prefix & "PlacementV", PlacementVersion)
        WriteValue(prefix & "X", x)
        WriteValue(prefix & "Y", y)
        WriteValue(prefix & "W", width)
        WriteValue(prefix & "H", height)
        WriteValue(prefix & "Max", If(maximized, 1, 0))
        WriteValue(prefix & "Dpi", dpi)
    End Sub

    ' ---- the Disk Manager (SP-0063 4.1 and 7.4) ----------------------------

    ' Column widths in device pixels at the DPI they were saved at, semicolon-separated in column
    ' order; a list whose count does not match the window's columns is not used.
    Public Function DiskManagerColumns() As List(Of Integer)
        Dim out As New List(Of Integer)
        Dim v = TryCast(ReadValue("DiskManagerColumns"), String)
        If String.IsNullOrEmpty(v) Then Return out
        For Each part In v.Split(";"c)
            Dim n As Integer
            If Not Integer.TryParse(part, Globalization.NumberStyles.Integer, Globalization.CultureInfo.InvariantCulture, n) OrElse
               n < 0 OrElse n > 5000 Then Return New List(Of Integer)
            out.Add(n)
        Next
        Return out
    End Function

    Public Sub SetDiskManagerColumns(widths As IEnumerable(Of Integer))
        WriteValue("DiskManagerColumns", String.Join(";", widths.Select(Function(w) w.ToString(Globalization.CultureInfo.InvariantCulture))))
    End Sub

    ' The sort: a column index and a direction, or -1 for the default order (mounted first).
    Public Function DiskManagerSort(ByRef descending As Boolean) As Integer
        descending = False
        Dim v = TryCast(ReadValue("DiskManagerSort"), String)
        If String.IsNullOrEmpty(v) Then Return -1
        Dim parts = v.Split(","c)
        Dim col As Integer
        If parts.Length <> 2 OrElse Not Integer.TryParse(parts(0), col) OrElse col < -1 OrElse col > 50 Then Return -1
        descending = (parts(1) = "desc")
        Return col
    End Function

    Public Sub SetDiskManagerSort(column As Integer, descending As Boolean)
        WriteValue("DiskManagerSort", column.ToString(Globalization.CultureInfo.InvariantCulture) & "," & If(descending, "desc", "asc"))
    End Sub

    ' Whether the detail pane is open. A missing value means open.
    Public Function DiskManagerDetailOpen() As Boolean
        Dim v = ReadValue("DiskManagerDetail")
        If TypeOf v Is Integer Then Return CInt(v) <> 0
        Return True
    End Function

    Public Sub SetDiskManagerDetailOpen(open As Boolean)
        WriteValue("DiskManagerDetail", If(open, 1, 0))
    End Sub

    ' Whether closing the manager with disks mounted says that they stay mounted (spec 7.4). A
    ' missing value means it does; "Don't say this again" writes 0.
    Public Function DiskManagerCloseNotice() As Boolean
        Dim v = ReadValue("DiskManagerCloseNotice")
        If TypeOf v Is Integer Then Return CInt(v) <> 0
        Return True
    End Function

    Public Sub SetDiskManagerCloseNotice(show As Boolean)
        WriteValue("DiskManagerCloseNotice", If(show, 1, 0))
    End Sub

    ' Whether the first-steps window has been shown to this user (APP-BEHAVIOUR rule 11). It is a
    ' record of what happened, never a licence: it only stops the window from opening by itself again,
    ' and Help still opens it whenever the user asks. A missing value means it has not been shown.
    Public Function DiskManagerWelcomed() As Boolean
        Dim v = ReadValue("DiskManagerWelcomed")
        Return TypeOf v Is Integer AndAlso CInt(v) <> 0
    End Function

    Public Sub SetDiskManagerWelcomed(shown As Boolean)
        WriteValue("DiskManagerWelcomed", If(shown, 1, 0))
    End Sub

    ' ---- language --------------------------------------------------------
    ' GuiLang, the value the retired command builder wrote too, so a language chosen there carries
    ' over. One program, one language (SP-0006 section 13).

    ' Capture.vb's seam: a language in force for this process only, never written to HKCU.
    Friend LanguageOverride As String = Nothing

    Public Function Language() As String
        If LanguageOverride IsNot Nothing Then Return LanguageOverride
        Dim v = TryCast(ReadValue("GuiLang"), String)
        If v IsNot Nothing AndAlso Localization.Languages.Contains(v) Then Return v
        Dim c = System.Globalization.CultureInfo.CurrentUICulture.TwoLetterISOLanguageName.ToLowerInvariant()
        If Localization.Languages.Contains(c) Then Return c
        Return "en"
    End Function

    Public Sub SetLanguage(lang As String)
        If Localization.Languages.Contains(lang) Then WriteValue("GuiLang", lang)
    End Sub

End Module
