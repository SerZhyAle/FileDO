' The palette, and the only file in this project allowed to name a colour.
'
' SP-0006 section 11 item 2: one set of tokens, defined once, with a dark variant, and nothing in
' the code naming a colour directly. The rule is checkable - a search for Color., FromArgb or RGB(
' across filedo_win_vb should find them here and nowhere else in the shell - which is why it is
' written as a rule rather than as an intention. MainForm.vb predates the shell and is not held to
' it until it is replaced.
'
' Item 8 of the same section makes contrast part of "good-looking": every text token below is at
' least 4.5:1 against the surface it is used on, in both themes. Where a colour carries meaning
' (success, warning, danger) it never carries it alone - the control that uses it also shows a word
' and a glyph.
Module Theme

    ' A token set. One instance for light, one for dark; nothing else constructs one.
    Public Class Palette
        Public Property IsDark As Boolean
        Public Property Background As Color      ' the window behind everything
        Public Property Surface As Color         ' a card, a panel, the page host
        Public Property SurfaceAlt As Color      ' the rail, and anything one step back
        Public Property Border As Color
        Public Property Text As Color
        Public Property MutedText As Color
        Public Property Accent As Color
        Public Property AccentText As Color      ' text drawn on top of Accent
        Public Property Success As Color
        Public Property Warning As Color
        Public Property Danger As Color
    End Class

    ' The tokens are the ones the product's own pages use, so the window and the site are visibly
    ' the same program: docs/kit/sza-kit.css and the FileDO landing page - a green-black surface,
    ' a green accent and a gold secondary. The hex values below are those variables, converted
    ' once here and named nowhere else.
    '   --bg #eef3ea / #0a0f0a, --acc #2f8f3a / #3fb950, --gold #9a6a13 / #e3b341,
    '   --danger #e5534b, --text #16210f / #f1f5ee, --muted #5f6b54 / #94a08c
    ' Where the site's token would fall under 4.5:1 as text (the light-mode green), the stronger
    ' variant of the same token is used, because item 8 of section 11 outranks an exact match.

    Private ReadOnly LightPalette As New Palette With {
        .IsDark = False,
        .Background = Color.FromArgb(238, 243, 234),
        .Surface = Color.FromArgb(255, 255, 255),
        .SurfaceAlt = Color.FromArgb(228, 236, 223),
        .Border = Color.FromArgb(214, 223, 208),
        .Text = Color.FromArgb(22, 33, 15),
        .MutedText = Color.FromArgb(87, 98, 76),
        .Accent = Color.FromArgb(38, 122, 48),
        .AccentText = Color.FromArgb(255, 255, 255),
        .Success = Color.FromArgb(38, 122, 48),
        .Warning = Color.FromArgb(126, 86, 15),
        .Danger = Color.FromArgb(179, 38, 30)
    }

    Private ReadOnly DarkPalette As New Palette With {
        .IsDark = True,
        .Background = Color.FromArgb(10, 15, 10),
        .Surface = Color.FromArgb(19, 28, 20),
        .SurfaceAlt = Color.FromArgb(15, 22, 16),
        .Border = Color.FromArgb(44, 56, 45),
        .Text = Color.FromArgb(241, 245, 238),
        .MutedText = Color.FromArgb(148, 160, 140),
        .Accent = Color.FromArgb(63, 185, 80),
        .AccentText = Color.FromArgb(4, 19, 12),
        .Success = Color.FromArgb(86, 211, 100),
        .Warning = Color.FromArgb(227, 179, 65),
        .Danger = Color.FromArgb(229, 83, 75)
    }

    Private currentPalette As Palette = Nothing

    ' The palette in force. Resolved once and reused, so that a repaint cannot disagree with a
    ' layout about which theme is on.
    Public ReadOnly Property Current As Palette
        Get
            If currentPalette Is Nothing Then currentPalette = Resolve()
            Return currentPalette
        End Get
    End Property

    ' Re-reads the setting and the system preference. Called when the user changes the override and
    ' when Windows tells the window its theme changed.
    Public Sub Refresh()
        currentPalette = Resolve()
    End Sub

    Private Function Resolve() As Palette
        Select Case ShellSettings.ThemeChoice()
            Case "light" : Return LightPalette
            Case "dark" : Return DarkPalette
            Case Else : Return If(SystemPrefersDark(), DarkPalette, LightPalette)
        End Select
    End Function

    ' Windows records the app theme as AppsUseLightTheme: 0 means the user asked for dark. A missing
    ' value means light, which is what Windows itself assumes.
    Public Function SystemPrefersDark() As Boolean
        Try
            Using k = Microsoft.Win32.Registry.CurrentUser.OpenSubKey(
                "Software\Microsoft\Windows\CurrentVersion\Themes\Personalize")
                If k IsNot Nothing Then
                    Dim v = k.GetValue("AppsUseLightTheme")
                    If TypeOf v Is Integer Then Return CInt(v) = 0
                End If
            End Using
        Catch
        End Try
        Return False
    End Function

    ' Mixes two tokens. It lives here rather than where it is used, because mixing colours is
    ' naming one by another name - and the rule in this file's header is that only this file does
    ' that. A hover tint is as much a palette decision as the palette itself.
    Public Function Blend(from As Color, towards As Color, amount As Single) As Color
        Dim r = CInt(from.R + (towards.R - from.R) * amount)
        Dim g = CInt(from.G + (towards.G - from.G) * amount)
        Dim b = CInt(from.B + (towards.B - from.B) * amount)
        Return Color.FromArgb(255, r, g, b)
    End Function

    ' ---- type ------------------------------------------------------------
    ' Item 5: Segoe UI Variable where available, Segoe UI otherwise, and a fixed scale of five
    ' sizes. Sizes are in points and scale with the DPI declaration rather than against it.

    Private uiFamilyCache As String = Nothing

    Public Function UiFamily() As String
        If uiFamilyCache IsNot Nothing Then Return uiFamilyCache
        uiFamilyCache = If(FamilyExists("Segoe UI Variable Text"), "Segoe UI Variable Text", "Segoe UI")
        Return uiFamilyCache
    End Function

    Public Function FamilyExists(name As String) As Boolean
        Try
            Using f As New FontFamily(name)
                Return True
            End Using
        Catch
            Return False
        End Try
    End Function

    Public Function FontCaption() As Font
        Return New Font(UiFamily(), 9.0F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

    Public Function FontBody() As Font
        Return New Font(UiFamily(), 10.0F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

    Public Function FontBodyStrong() As Font
        Return New Font(UiFamily(), 10.0F, FontStyle.Bold, GraphicsUnit.Point)
    End Function

    Public Function FontSubtitle() As Font
        Return New Font(UiFamily(), 13.0F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

    Public Function FontTitle() As Font
        Return New Font(UiFamily(), 17.0F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

    Public Function FontMono() As Font
        Return New Font("Consolas", 9.5F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

    ' ---- glyphs ----------------------------------------------------------
    ' Item 4: icons are glyphs, not bitmaps, so the shell gains no image resources and stays crisp
    ' at every DPI. Segoe Fluent Icons is present on Windows 11; Segoe MDL2 Assets covers Windows
    ' 10. If a machine has neither, the label still carries the meaning and the glyph falls back to
    ' a character every font has - the rail must never become unreadable over an icon.

    Private glyphFamilyCache As String = Nothing

    Public Function GlyphFamily() As String
        If glyphFamilyCache IsNot Nothing Then Return glyphFamilyCache
        If FamilyExists("Segoe Fluent Icons") Then
            glyphFamilyCache = "Segoe Fluent Icons"
        ElseIf FamilyExists("Segoe MDL2 Assets") Then
            glyphFamilyCache = "Segoe MDL2 Assets"
        Else
            glyphFamilyCache = ""
        End If
        Return glyphFamilyCache
    End Function

    Public Function HasGlyphFont() As Boolean
        Return GlyphFamily() <> ""
    End Function

    Public Function FontGlyph() As Font
        If HasGlyphFont() Then Return New Font(GlyphFamily(), 12.0F, FontStyle.Regular, GraphicsUnit.Point)
        Return New Font(UiFamily(), 10.0F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

    ' The fallback character is deliberately a bullet rather than a letter: a letter would read as
    ' part of the label.
    Public Function Glyph(code As String) As String
        If HasGlyphFont() Then Return code
        Return ChrW(&H2022)
    End Function

    ' The chevron of a collapsible rail group. It gets its own pair rather than going through
    ' Glyph() above, because the bullet fallback would say nothing about open or shut - and on a
    ' group header the arrow is the only thing that carries the state. The fallback characters are
    ' geometric arrows that every UI font on Windows has.
    Public Function ChevronGlyph(collapsed As Boolean) As String
        If HasGlyphFont() Then Return If(collapsed, ChrW(&HE76C), ChrW(&HE70D))
        Return If(collapsed, ChrW(&H25B8), ChrW(&H25BE))
    End Function

    Public Function FontChevron() As Font
        If HasGlyphFont() Then Return New Font(GlyphFamily(), 8.0F, FontStyle.Regular, GraphicsUnit.Point)
        Return New Font(UiFamily(), 9.0F, FontStyle.Regular, GraphicsUnit.Point)
    End Function

End Module
