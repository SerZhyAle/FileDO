' The shell's half of HKCU\Software\FileDO.
'
' The key already exists and already holds GuiLang, written by MainForm - so this milestone extends
' a store rather than inventing one (SP-0006 M0 tactics, finding 2.2). Everything here is
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
    Private Const PlacementVersion As Integer = 2

    Private Function ReadValue(name As String) As Object
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
        Try
            Using k = Registry.CurrentUser.CreateSubKey(KeyPath)
                If k IsNot Nothing Then k.SetValue(name, value)
            End Using
        Catch
        End Try
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
    Public Function LoadPlacement() As Placement
        Dim p As New Placement With {.HasValue = False}
        Dim stamp As Integer
        If Not ReadInt("ShellPlacementV", stamp) OrElse stamp <> PlacementVersion Then Return p

        Dim x, y, w, h As Integer
        If ReadInt("ShellX", x) AndAlso ReadInt("ShellY", y) AndAlso
           ReadInt("ShellW", w) AndAlso ReadInt("ShellH", h) Then
            p.X = x : p.Y = y : p.Width = w : p.Height = h
            p.HasValue = True
        End If
        Dim m As Integer
        If ReadInt("ShellMax", m) Then p.Maximized = (m <> 0)
        Return p
    End Function

    Public Sub SavePlacement(x As Integer, y As Integer, width As Integer, height As Integer, maximized As Boolean)
        WriteValue("ShellPlacementV", PlacementVersion)
        WriteValue("ShellX", x)
        WriteValue("ShellY", y)
        WriteValue("ShellW", width)
        WriteValue("ShellH", height)
        WriteValue("ShellMax", If(maximized, 1, 0))
    End Sub

    ' ---- language --------------------------------------------------------
    ' The same value MainForm reads and writes. The shell does not get a second language setting:
    ' one program, one language (SP-0006 section 13).

    Public Function Language() As String
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
