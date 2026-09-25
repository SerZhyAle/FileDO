' Modern window chrome, and the honest handling of the fact that it might not be there.
'
' SP-0006 section 11 item 3 asks for a dark title bar and rounded corners through
' DwmSetWindowAttribute, and marks both **assumed** - to be confirmed by a spike in M0. Two things
' follow from "assumed" that are easy to get wrong:
'
'   1. An unknown attribute is not an error in this program. Windows returns E_INVALIDARG for an
'      attribute it does not know, and the correct response is a window with an ordinary title bar,
'      not an exception dialog. Every call here is wrapped, and the failure path is "nothing
'      happens".
'   2. A spike that is not recorded proves nothing. Report() returns exactly what the calls
'      returned on this machine, so the milestone can quote a result instead of an intention.
Imports System.Runtime.InteropServices

Module Chrome

    ' Pre-20H1 builds used 19 for the same meaning; 20 is the documented one from 20H1 onward.
    Private Const DWMWA_USE_IMMERSIVE_DARK_MODE_OLD As Integer = 19
    Private Const DWMWA_USE_IMMERSIVE_DARK_MODE As Integer = 20
    Private Const DWMWA_WINDOW_CORNER_PREFERENCE As Integer = 33

    ' DWM_WINDOW_CORNER_PREFERENCE
    Private Const DWMWCP_ROUND As Integer = 2

    <DllImport("dwmapi.dll", PreserveSig:=True)>
    Private Function DwmSetWindowAttribute(hwnd As IntPtr, attr As Integer,
                                           ByRef value As Integer, size As Integer) As Integer
    End Function

    Private lastDark As String = "not attempted"
    Private lastCorners As String = "not attempted"

    ' Applies the chrome to a window that already has a handle. Safe to call again after a theme
    ' change, which is the only reason it is not done once at creation.
    Public Sub Apply(form As Form)
        If form Is Nothing OrElse Not form.IsHandleCreated Then Return
        ApplyDarkTitleBar(form.Handle, Theme.Current.IsDark)
        ApplyRoundedCorners(form.Handle)
    End Sub

    Public Sub ApplyDarkTitleBar(hwnd As IntPtr, dark As Boolean)
        Dim value As Integer = If(dark, 1, 0)
        Try
            Dim hr = DwmSetWindowAttribute(hwnd, DWMWA_USE_IMMERSIVE_DARK_MODE, value, 4)
            If hr <> 0 Then
                ' Older build: the same meaning lived at 19 before it was documented at 20.
                Dim hrOld = DwmSetWindowAttribute(hwnd, DWMWA_USE_IMMERSIVE_DARK_MODE_OLD, value, 4)
                lastDark = "attr20=0x" & hr.ToString("X8") & " attr19=0x" & hrOld.ToString("X8")
            Else
                lastDark = "attr20=0x" & hr.ToString("X8")
            End If
        Catch ex As Exception
            ' dwmapi.dll is present on every supported build, so this is defence rather than an
            ' expected path - but a missing DLL must still not stop the window from opening.
            lastDark = "threw: " & ex.GetType().Name
        End Try
    End Sub

    Public Sub ApplyRoundedCorners(hwnd As IntPtr)
        Dim value As Integer = DWMWCP_ROUND
        Try
            Dim hr = DwmSetWindowAttribute(hwnd, DWMWA_WINDOW_CORNER_PREFERENCE, value, 4)
            lastCorners = "attr33=0x" & hr.ToString("X8")
        Catch ex As Exception
            lastCorners = "threw: " & ex.GetType().Name
        End Try
    End Sub

    ' What the spike of M0 work item 8 records. 0x00000000 is S_OK; 0x80070057 is E_INVALIDARG,
    ' which is what a build that does not know the attribute returns and is not a defect.
    Public Function Report() As String
        Return "dark-title-bar: " & lastDark &
               " | rounded-corners: " & lastCorners &
               " | glyphs: " & GlyphReport() &
               " | glyph-font: " & If(Theme.HasGlyphFont(), Theme.GlyphFamily(), "(none - falling back)") &
               " | ui-font: " & Theme.UiFamily() &
               " | theme: " & If(Theme.Current.IsDark, "dark", "light") &
               " (setting: " & ShellSettings.ThemeChoice() & ")"
    End Function

    ' How many vendored drawings verified against PROVENANCE.txt, and the first reason one did not -
    ' the line that explains a rail of bullets in a log a user sent (Glyphs.vb).
    Private Function GlyphReport() As String
        Dim problems = Glyphs.Problems()
        Dim drawn = Glyphs.DrawableIds().Count()
        If problems.Count = 0 Then Return drawn.ToString() & " verified (" & Glyphs.CatalogVersions() & ")"
        Return drawn.ToString() & " verified, " & problems.Count.ToString() & " refused: " & problems(0)
    End Function

End Module
