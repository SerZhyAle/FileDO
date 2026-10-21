' APP-STYLE 0.13, the status fill roles (SP-0171 W9): status.ok.fill, status.warning.fill,
' status.error.fill, status.unknown.fill and the one status.ink, measured rather than claimed.
'
' The contract states one number: the ink reaches 4.5:1 against every fill it is drawn on, in each
' theme, and "measure every rendered pair". It states no number for the fill against the surface it
' sits on; these rows hold that pair to 3:1, the portfolio's threshold for a non-text mark
' (ICON-RENDER rule 3), and print the measured ratio either way, so a palette edit shows what it did.
' The state is never carried by the fill alone - the badge also says the word and has the state's
' glyph beside it - and an unknown state is not a disabled one, so its fill is a role of its own.
'
' Rows are named `state-fill:`; FILEDO_SELFTEST_ONLY=state-fill runs just these. Nothing here names a
' colour: every value is read from Theme.
Partial Public Module SelfTest

    Private ReadOnly StateFillKinds As Theme.StatusKind() = {
        Theme.StatusKind.Ok, Theme.StatusKind.Warning, Theme.StatusKind.Failure, Theme.StatusKind.Unknown}

    ' The five words the badge can say, and a sixth this build does not know (it is "not proven"
    ' to the shell and the fill of "no completed result" to the palette).
    Private ReadOnly StateFillVerdicts As String() = {"Passed", "Done", "Stopped", "Failed", "Not proven"}

    Private Sub CheckStateFill()
        CheckStateFillRoles()
        CheckStateFillMapping()
        CheckStateFillBadge()
        CheckStateFillHighContrast()
    End Sub

    ' The roles in the two palettes: the ink against each fill, each fill against the three surfaces
    ' the badge can be drawn on (the card it sits on, the window behind it, the rail's step), the
    ' fills told apart, and the unknown one told apart from everything that means "off".
    Private Sub CheckStateFillRoles()
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim t = If(dark, "dark", "light")
            For Each kind In StateFillKinds
                Dim role = Theme.StatusRoleName(kind)
                Dim fill = Theme.StatusFill(kind, p)
                ' Text on the fill: 4.5:1 (APP-STYLE 0.13 "one ink per theme ... against every fill").
                ContrastPair("state-fill:" & t & ":" & role & ":ink", Theme.StatusInkOn(kind, p), fill, 4.5)
                ' The fill itself, as a mark on its surface: 3:1.
                ContrastPair("state-fill:" & t & ":" & role & ":on-card", fill, p.Surface, 3.0)
                ContrastPair("state-fill:" & t & ":" & role & ":on-window", fill, p.Background, 3.0)
                ContrastPair("state-fill:" & t & ":" & role & ":on-rail", fill, p.SurfaceAlt, 3.0)
            Next

            ' The four fills are four colours.
            Dim distinct = True
            Dim seen As New HashSet(Of Integer)()
            For Each kind In StateFillKinds
                distinct = distinct AndAlso seen.Add(Theme.StatusFill(kind, p).ToArgb())
            Next
            Check("state-fill:" & t & ":fills-distinct", distinct, "four fills, " & seen.Count.ToString() & " colours")

            ' Unknown is not disabled: it shares no colour with the sunken or ordinary surfaces, the
            ' border, or the disabled and muted text - the roles that mean "off", "quiet" or "nothing".
            Dim unknown = Theme.StatusFill(Theme.StatusKind.Unknown, p).ToArgb()
            Dim offRoles = New Integer() {p.Surface.ToArgb(), p.SurfaceAlt.ToArgb(), p.Background.ToArgb(), p.SurfaceBand.ToArgb(),
                                          p.Border.ToArgb(), p.TextDisabled.ToArgb(), p.MutedText.ToArgb()}
            Check("state-fill:" & t & ":unknown-is-not-disabled", Not offRoles.Contains(unknown),
                  HexOf(Theme.StatusFill(Theme.StatusKind.Unknown, p)))

            ' One ink per theme: no verdict, known or not, asks for a second one.
            Dim oneInk = True
            For Each verdict In StateFillVerdicts.Concat({"Quarantined", "", Nothing})
                oneInk = oneInk AndAlso Theme.VerdictFore(verdict, p).ToArgb() = p.StatusInk.ToArgb()
            Next
            Check("state-fill:" & t & ":one-ink", oneInk, HexOf(p.StatusInk))
        Next
    End Sub

    ' Which verdict is which state, and that the badge's fill and text are exactly the role's.
    Private Sub CheckStateFillMapping()
        Dim cases = New Object()() {
            New Object() {"Passed", Theme.StatusKind.Ok},
            New Object() {"Done", Theme.StatusKind.Ok},
            New Object() {"Stopped", Theme.StatusKind.Warning},
            New Object() {"Failed", Theme.StatusKind.Failure},
            New Object() {"Not proven", Theme.StatusKind.Unknown},
            New Object() {"Quarantined", Theme.StatusKind.Unknown},
            New Object() {"", Theme.StatusKind.Unknown},
            New Object() {Nothing, Theme.StatusKind.Unknown}
        }
        For Each c In cases
            Dim verdict = DirectCast(c(0), String)
            Dim want = DirectCast(c(1), Theme.StatusKind)
            Check("state-fill:map:" & If(verdict, "(nothing)"), Theme.StatusOf(verdict) = want, Theme.StatusOf(verdict).ToString())
        Next
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim t = If(dark, "dark", "light")
            Dim exact = True
            For Each c In cases
                Dim verdict = DirectCast(c(0), String)
                Dim kind = DirectCast(c(1), Theme.StatusKind)
                exact = exact AndAlso Theme.VerdictBack(verdict, p).ToArgb() = Theme.StatusFill(kind, p).ToArgb() AndAlso
                        Theme.VerdictFore(verdict, p).ToArgb() = Theme.StatusInkOn(kind, p).ToArgb()
            Next
            Check("state-fill:" & t & ":verdict-draws-the-role", exact, "")
        Next
    End Sub

    ' The badge as it is drawn on the job page: each verdict on each palette, its fill and its text read
    ' back off the control, the pair measured, and the word and the glyph beside the colour checked.
    Private Sub CheckStateFillBadge()
        Dim jv As JobView = Nothing
        Try
            Theme.UsePaletteForTest(False)
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            For Each dark In New Boolean() {False, True}
                Theme.UsePaletteForTest(dark)
                Dim p = Theme.PaletteFor(dark)
                Dim t = If(dark, "dark", "light")
                For Each verdict In StateFillVerdicts
                    jv.ShowResultForTest(verdict)
                    Dim badge = jv.VerdictBadgeForTest
                    Dim kind = Theme.StatusOf(verdict)
                    Check("state-fill:" & t & ":badge:" & verdict,
                          badge.BackColor.ToArgb() = Theme.StatusFill(kind, p).ToArgb() AndAlso
                          badge.ForeColor.ToArgb() = p.StatusInk.ToArgb(),
                          Theme.StatusRoleName(kind) & " " & HexOf(badge.BackColor) & ", ink " & HexOf(badge.ForeColor))
                    ContrastPair("state-fill:" & t & ":badge-pair:" & verdict, badge.ForeColor, badge.BackColor, 4.5)
                    Dim glyph = jv.VerdictGlyphForTest.Glyph
                    Check("state-fill:" & t & ":badge-word-and-glyph:" & verdict,
                          badge.Text.Trim().Length > 0 AndAlso Not badge.Text.StartsWith("shell_verdict_", StringComparison.Ordinal) AndAlso
                          glyph IsNot Nothing AndAlso glyph.IsVocabulary AndAlso Glyphs.IsDrawable(glyph.Id),
                          "'" & badge.Text & "', " & If(glyph Is Nothing, "no glyph", glyph.ToString()))
                Next
            Next
        Finally
            Theme.UsePaletteForTest(Nothing)
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' Under high contrast the fills are the window colour, the ink the window text colour, and an
    ' unknown state is drawn in the disabled text colour - the state then lives in the glyph and the
    ' word. The pair is measured with whatever system colours this session has (a normal session:
    ' white, black and the grey of disabled text); a session in a real high-contrast theme is the
    ' same rows with that theme's colours, and is not claimed here.
    Private Sub CheckStateFillHighContrast()
        Dim hc = Theme.SystemContrastPalette()
        Dim window = SystemColors.Window.ToArgb()
        Dim fillsAreWindow = True
        For Each kind In StateFillKinds
            fillsAreWindow = fillsAreWindow AndAlso Theme.StatusFill(kind, hc).ToArgb() = window
        Next
        Check("state-fill:high-contrast:fills-are-window", fillsAreWindow AndAlso hc.HighContrast, "")
        Check("state-fill:high-contrast:ink-is-window-text", hc.StatusInk.ToArgb() = SystemColors.WindowText.ToArgb(), "")
        Check("state-fill:high-contrast:unknown-ink-is-disabled-text",
              Theme.StatusInkOn(Theme.StatusKind.Unknown, hc).ToArgb() = SystemColors.GrayText.ToArgb() AndAlso
              Theme.StatusInkOn(Theme.StatusKind.Ok, hc).ToArgb() = SystemColors.WindowText.ToArgb() AndAlso
              Theme.StatusInkOn(Theme.StatusKind.Warning, hc).ToArgb() = SystemColors.WindowText.ToArgb() AndAlso
              Theme.StatusInkOn(Theme.StatusKind.Failure, hc).ToArgb() = SystemColors.WindowText.ToArgb(), "")
        For Each kind In StateFillKinds
            ContrastPair("state-fill:high-contrast:" & Theme.StatusRoleName(kind) & ":ink", Theme.StatusInkOn(kind, hc), Theme.StatusFill(kind, hc), 4.5)
        Next
        For Each verdict In StateFillVerdicts
            ContrastPair("state-fill:high-contrast:badge-pair:" & verdict, Theme.VerdictFore(verdict, hc), Theme.VerdictBack(verdict, hc), 4.5)
        Next
    End Sub

End Module
