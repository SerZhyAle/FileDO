' The palette, and the only file in this project allowed to name a colour.
'
' SP-0006 section 11 item 2: one set of tokens, defined once, with a dark variant, and nothing in
' the code naming a colour directly. The rule is checkable - a search for Color., FromArgb or RGB(
' across filedo_win_vb finds them here and nowhere else, and cmd/filedo's surfaces test makes that
' search on every build - which is why it is written as a rule rather than as an intention.
'
' Item 8 of the same section makes contrast part of "good-looking": every text token below is meant
' to be at least 4.5:1 against the surface it is used on, in both themes. For the text tokens that is
' still a claim; for every glyph and every verdict colour it is a measurement - the self-test's
' contrast: rows compute each pair from this table (ContrastRatio below). Where a colour carries
' meaning (success, warning, danger) it never carries it alone - the control that uses it also shows
' a word and a glyph.
'
' APP-STYLE (the shared theme contract, FileDO a consumer). Section 2: three modes - Follow Windows
' (the default), light, dark - applied live and following WM_SETTINGCHANGE (ShellForm.WndProc).
' Section 3: this table is the one source, and every view re-reads it in ApplyTheme, so nothing holds
' a colour resolved once. Section 4, the role names of this table:
'   Background      surface.window        Text            text.primary
'   Surface         surface.raised        MutedText       text.muted
'   SurfaceAlt      surface.sunken        TextDisabled    text.disabled
'   Border          border                Link            link
'   ControlHover    control.hover         Accent          accent
'   SurfaceSelected surface.selected      AccentText      accent.ink
'   Success, Warning, Danger              success, warning, danger (proposed roles)
'   StateOk, StateWarning, StateError     ICON-RENDER's state role: a status glyph and a verdict plate
' Section 5, the surfaces that stay outside the theme, and why. They are native Win32 controls that
' WinForms cannot recolour without owner-drawing them whole:
'   - the scroll bars of text boxes, list boxes and scrolled pages (system-drawn);
'   - ProgressBar (drawn by the visual-styles engine in the system accent colour);
'   - the drop-down list of a ComboBox, and its arrow button;
'   - the Open-file and Browse-for-folder common dialogs (owned by the shell, not by FileDO);
'   - tooltips (system-drawn).
' Every question and notice the shell asks is its own themed ShellDialog, not a system message box,
' so no dialog of FileDO's own is on this list.
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
        Public Property TextDisabled As Color    ' the caption of a control that cannot be used now
        Public Property Link As Color
        Public Property Accent As Color
        Public Property AccentText As Color      ' text drawn on top of Accent
        Public Property ControlHover As Color    ' a row or button under the pointer
        Public Property SurfaceSelected As Color ' the chosen row of the rail
        Public Property Success As Color
        Public Property Warning As Color
        Public Property Danger As Color
        Public Property StateOk As Color         ' palette.json state.ok
        Public Property StateWarning As Color    ' palette.json state.warning (dark only, see below)
        Public Property StateError As Color      ' palette.json state.error
    End Class

    ' The tokens are the ones the product's own pages use, so the window and the site are visibly
    ' the same program: docs/kit/sza-kit.css and the FileDO landing page - a green-black surface,
    ' a green accent and a gold secondary. The hex values below are those variables, converted
    ' once here and named nowhere else.
    '   --bg #eef3ea / #0a0f0a, --acc #2f8f3a / #3fb950, --gold #9a6a13 / #e3b341,
    '   --danger #e5534b, --text #16210f / #f1f5ee, --muted #5f6b54 / #94a08c
    ' Where the site's token would fall under 4.5:1 as text (the light-mode green), the stronger
    ' variant of the same token is used, because item 8 of section 11 outranks an exact match.
    '
    ' The three State roles are not the site's: they are the portfolio's shared state hues
    ' (ICON-RENDER section 10 item D, the catalog's palette.json, vendored in assets/glyphs/), which
    ' the owner chose to take as exact tones on 2026-09-25 once a contrast proof held (SP-0016 D2).
    ' They paint the verdict glyph, the verdict line of the Command page and the verdict badge. The
    ' proof is the self-test's contrast: rows, and its state-tone: rows hold each role to the
    ' vendored palette - so a changed tone in the catalog fails here rather than drifting.
    ' One tone did not hold: state.warning's day tone #F57C00 is 2.70:1 against white, under the 3:1
    ' of rule 3 and of item D itself. The light StateWarning therefore stays the shell's own Warning
    ' - a dated exception, reported to the owner - and the self-test fails the day the catalog's tone
    ' reaches 3:1, which is the day it should be taken. The text roles Success, Warning and Danger
    ' stay APP-STYLE's own, because they are held to 4.5:1 as text on every surface of the shell,
    ' where state.error's day tone is 4.4:1 on the window and 4.1:1 on the rail.

    Private ReadOnly LightPalette As Palette = Derive(New Palette With {
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
        .Danger = Color.FromArgb(179, 38, 30),
        .StateOk = Color.FromArgb(46, 125, 50),        ' #2E7D32 state.ok day
        .StateWarning = Color.FromArgb(126, 86, 15),   ' the shell's Warning: #F57C00 fails 3:1
        .StateError = Color.FromArgb(211, 47, 47)      ' #D32F2F state.error day
    })

    Private ReadOnly DarkPalette As Palette = Derive(New Palette With {
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
        .Danger = Color.FromArgb(229, 83, 75),
        .StateOk = Color.FromArgb(129, 199, 132),      ' #81C784 state.ok night
        .StateWarning = Color.FromArgb(255, 183, 77),  ' #FFB74D state.warning night
        .StateError = Color.FromArgb(239, 83, 80)      ' #EF5350 state.error night
    })

    ' The roles that are a mix of two others, mixed here once per palette (APP-STYLE section 4). A
    ' hover tint or a selection fill is as much a palette decision as the palette itself, so the
    ' amounts live in this file and nowhere else.
    Private Function Derive(p As Palette) As Palette
        p.ControlHover = Blend(p.SurfaceAlt, p.Text, 0.07F)
        p.SurfaceSelected = Blend(p.SurfaceAlt, p.Accent, If(p.IsDark, 0.22F, 0.14F))
        p.TextDisabled = Blend(p.MutedText, p.SurfaceAlt, 0.2F)
        p.Link = p.Accent
        Return p
    End Function

    Private currentPalette As Palette = Nothing
    Private testPalette As Palette = Nothing

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

    ' The self-test's seam: put one palette in force, or Nothing to go back to the user's. It
    ' never writes HKCU, because the gate runs on a developer's machine and must leave their theme
    ' setting alone.
    Friend Sub UsePaletteForTest(dark As Boolean?)
        If dark.HasValue Then
            testPalette = If(dark.Value, DarkPalette, LightPalette)
        Else
            testPalette = Nothing
        End If
        Refresh()
    End Sub

    Friend Function PaletteFor(dark As Boolean) As Palette
        Return If(dark, DarkPalette, LightPalette)
    End Function

    Private Function Resolve() As Palette
        If testPalette IsNot Nothing Then Return testPalette
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

    ' Mixes two tokens. It is private, because mixing colours is naming one by another name - and
    ' the rule in this file's header is that only this file does that. A mix a view needs becomes a
    ' named role of the palette (Derive above) instead.
    '
    ' The channels are widened to Integer before they are subtracted. Color.R is a Byte, and in VB
    ' a Byte minus a larger Byte is an OverflowException - which is what painted the light theme's
    ' selected rail row as a red cross (SP-0014 T1): every light mix goes from a light surface
    ' towards a darker token, so every one of them subtracted downwards, while the dark theme's
    ' mixes all go upwards and never did.
    Private Function Blend(from As Color, towards As Color, amount As Single) As Color
        Return Color.FromArgb(255, Mix(from.R, towards.R, amount), Mix(from.G, towards.G, amount),
                              Mix(from.B, towards.B, amount))
    End Function

    Private Function Mix(a As Byte, b As Byte, amount As Single) As Integer
        Dim ia As Integer = a
        Dim ib As Integer = b
        Return Math.Max(0, Math.Min(255, CInt(Math.Round(ia + (ib - ia) * amount))))
    End Function

    ' A role colour at a glyph part's own opacity (SvgPath reads opacity from the catalog's file).
    ' The colour is still the role's; only its alpha is the drawing's.
    Friend Function Faded(c As Color, opacity As Single) As Color
        Dim a = Math.Max(0, Math.Min(255, CInt(Math.Round(c.A * opacity))))
        Return Color.FromArgb(a, c)
    End Function

    ' The WCAG 2.1 contrast ratio of two opaque colours, 1 to 21 - what the self-test measures the
    ' glyph and verdict pairs of this table with (ICON-RENDER rule 3: 3:1 for a glyph; 4.5:1 for text).
    Friend Function ContrastRatio(a As Color, b As Color) As Double
        Dim la = RelativeLuminance(a)
        Dim lb = RelativeLuminance(b)
        Return (Math.Max(la, lb) + 0.05) / (Math.Min(la, lb) + 0.05)
    End Function

    Private Function RelativeLuminance(c As Color) As Double
        Return 0.2126 * Linear(c.R) + 0.7152 * Linear(c.G) + 0.0722 * Linear(c.B)
    End Function

    Private Function Linear(channel As Byte) As Double
        Dim s = channel / 255.0
        Return If(s <= 0.04045, s / 12.92, Math.Pow((s + 0.055) / 1.055, 2.4))
    End Function

    ' "#2E7D32" as a colour, for the self-test's reading of the vendored palette.json. Nothing
    ' outside this file may name a colour, and a hex string read from a file is still one.
    Friend Function FromHex(hex As String) As Color
        Dim h = If(hex, "").Trim().TrimStart("#"c)
        If h.Length <> 6 Then Return Color.Empty
        Try
            Return Color.FromArgb(255, Convert.ToInt32(h.Substring(0, 2), 16),
                                  Convert.ToInt32(h.Substring(2, 2), 16), Convert.ToInt32(h.Substring(4, 2), 16))
        Catch
            Return Color.Empty
        End Try
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
    ' Item 4 said icons are glyphs, not bitmaps, so the shell gains no image resources and stays
    ' crisp at every DPI. That still holds, one level down: a glyph is now the vocabulary's own
    ' vector drawing (Glyphs.vb, SP-0016 D1), filled as a path at the size of its tier. The platform
    ' icon font stays for one job only - the stand-in of a meaning the vocabulary has no record for
    ' yet (GlyphRef.Waiting). Segoe Fluent Icons is present on Windows 11; Segoe MDL2 Assets covers
    ' Windows 10. If a machine has neither, the label still carries the meaning and the glyph falls
    ' back to a character every font has - the rail must never become unreadable over an icon.

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

    ' The chevron of a collapsible rail group. ICON-SET: a shut group offers nav.expand (chevron
    ' down), an open one nav.collapse (chevron up). A chevron pointing right is nav.go-to - "this
    ' row opens its own screen" - which the vocabulary lists as distinct from expand, so it is not
    ' used here (SP-0016 T3). The fallback is a pair of geometric arrows every UI font on Windows
    ' has rather than a bullet, because on a group header the arrow is the only thing that carries
    ' the state (ICON-RENDER rule 4).
    Private ReadOnly ExpandGlyph As GlyphRef = GlyphRef.Vocabulary("nav.expand", ChrW(&H25BE))
    Private ReadOnly CollapseGlyph As GlyphRef = GlyphRef.Vocabulary("nav.collapse", ChrW(&H25B4))

    Public Function ChevronGlyph(collapsed As Boolean) As GlyphRef
        Return If(collapsed, ExpandGlyph, CollapseGlyph)
    End Function

    ' The one tone of the Explorer icons (MenuIcons.vb, SP-0016 T8). Explorer draws them on a light
    ' or a dark menu without asking FileDO which, so no palette role applies: ICON-RENDER 0.12
    ' rule 9 names #808080, which holds 3:1 on both (3.9:1 on white, 3.6:1 on #2B2B2B).
    Public ReadOnly MenuIconTone As Color = Color.FromArgb(&H80, &H80, &H80)

    ' ---- verdict glyphs --------------------------------------------------
    ' One mapping for both pages that show a verdict (the job page and the Command page), so the
    ' two cannot drift. Passed and Done draw status.ok (check mark in a filled circle), Failed
    ' draws status.error (exclamation mark in a circle) - the plain check is action.confirm and
    ' the plain cross nav.close, both listed as distinct from those states (SP-0016 T2).
    ' Stopped draws status.stopped (a square cut out of a filled circle - not media.stop's transport
    ' square) and Not proven status.not-proven (a filled square with a dash - not app.help's question
    ' mark); both entered the vocabulary in ICON-SET 0.14 at FileDO's request (SP-0016 B3).
    Private ReadOnly OkGlyph As GlyphRef = GlyphRef.Vocabulary("status.ok")
    Private ReadOnly ErrorGlyph As GlyphRef = GlyphRef.Vocabulary("status.error")
    Private ReadOnly StoppedGlyph As GlyphRef = GlyphRef.Vocabulary("status.stopped")
    Private ReadOnly NotProvenGlyph As GlyphRef = GlyphRef.Vocabulary("status.not-proven")

    Public Function VerdictGlyph(verdict As String) As GlyphRef
        Select Case If(verdict, "").ToLowerInvariant()
            Case "passed", "done" : Return OkGlyph
            Case "failed" : Return ErrorGlyph
            Case "stopped" : Return StoppedGlyph
            Case Else : Return NotProvenGlyph
        End Select
    End Function

    ' A state glyph takes the state's colour (ICON-RENDER rules 2 and 10C), and so does the
    ' Command page's verdict line beside it - the contrast: rows hold each at 4.5:1 on the card.
    Public Function VerdictColor(verdict As String, p As Palette) As Color
        Select Case If(verdict, "").ToLowerInvariant()
            Case "passed", "done" : Return p.StateOk
            Case "failed" : Return p.StateError
            Case "stopped" : Return p.StateWarning
            Case Else : Return p.MutedText
        End Select
    End Function

    ' The verdict badge: its fill and its text, as one function of (verdict, palette). The result
    ' path and ApplyTheme both call it, so a theme switch with a result on screen repaints the badge
    ' in the new palette instead of keeping the old one (APP-STYLE section 3, "a reference resolved
    ' once at load"). The fill is the state's own hue, the plate of the glyph beside it.
    Public Function VerdictBack(verdict As String, p As Palette) As Color
        Select Case If(verdict, "").ToLowerInvariant()
            Case "passed" : Return p.StateOk
            Case "failed" : Return p.StateError
            Case "stopped" : Return p.StateWarning
            Case "done" : Return p.Accent
            Case Else : Return p.SurfaceAlt
        End Select
    End Function

    Public Function VerdictFore(verdict As String, p As Palette) As Color
        Select Case If(verdict, "").ToLowerInvariant()
            Case "passed", "failed", "stopped", "done" : Return p.AccentText
            Case Else : Return p.Text
        End Select
    End Function

End Module
