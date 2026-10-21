Imports System.Reflection

' The unified UI contracts of SP-0169 (UI-SIZING 1.1, BUTTONS 2.0, COLLAPSIBLE-GROUP 1.1), rung 1: the
' token resolver, the button geometry, the group header and the order of a dialog's answers, measured on
' real controls built (not shown) in this process. What a person must still look at - the showcase
' captures at the sizes and in the languages the contracts list - is slice S4, not here.
'
' Rows are named `ui-sizing:`, `buttons:`, `group:` and `dialog:answer-order:`; the group filter
' FILEDO_SELFTEST_ONLY=ui-contracts runs just these.
Partial Public Module SelfTest

    Private Sub CheckUiContracts()
        CheckUiSizing()
        CheckButtonsContract()
        CheckGroupContract()
        CheckDialogAnswerOrder()
    End Sub

    ' ---- UI-SIZING -------------------------------------------------------------------------------

    Private Sub CheckUiSizing()
        Check("ui-sizing:profile", UiTokens.Profile = UiTokens.InputProfile.Pointer,
              "the pointer profile, chosen at start and never changed by an input event")
        Dim baseline = UiTokens.InterfaceScale = 1.0
        Check("ui-sizing:baseline-scale", baseline, UiTokens.InterfaceScale.ToString(Globalization.CultureInfo.InvariantCulture))

        ' BUTTONS section 3, BOOLEAN-OPTION 2, COLLAPSIBLE-GROUP 3.6, SETTINGS-NAVIGATION 2.5: the ordinary
        ' pointer tokens, from one resolver.
        Dim tokens = String.Join(" ", New String() {
            "target " & UiTokens.Target().ToString(), "padX " & UiTokens.ButtonPadX().ToString(),
            "glyph " & UiTokens.CaptionGlyph().ToString(), "gap " & UiTokens.GlyphGap().ToString(),
            "radius " & UiTokens.Radius().ToString(), "btnGap " & UiTokens.ButtonGap().ToString(),
            "groupGap " & UiTokens.GroupGap().ToString(), "marker " & UiTokens.GroupMarker().ToString(),
            "box " & UiTokens.OptionBox().ToString(), "nav " & UiTokens.NavRow().ToString()})
        Check("ui-sizing:tokens",
              UiTokens.Target() = 32 AndAlso UiTokens.ButtonPadX() = 12 AndAlso UiTokens.CaptionGlyph() = 16 AndAlso
              UiTokens.GlyphGap() = 8 AndAlso UiTokens.Radius() = 4 AndAlso UiTokens.ButtonGap() = 8 AndAlso
              UiTokens.GroupGap() = 16 AndAlso UiTokens.GroupMarker() = 16 AndAlso UiTokens.GroupMarkerGap() = 8 AndAlso
              UiTokens.GroupBodyInset() = 12 AndAlso UiTokens.GroupSiblingGap() = 8 AndAlso UiTokens.OptionBox() >= 20 AndAlso
              UiTokens.NavRow() = 32 AndAlso UiTokens.NavGlyph() = 16, tokens)

        ' Rule 4: the smallest supported target is declared, reached at the compact scale, never zero.
        Check("ui-sizing:floor",
              UiTokens.Scaled(32, UiTokens.MinScale) = UiTokens.CompactFloor AndAlso UiTokens.CompactFloor = 28,
              "compact " & UiTokens.Scaled(32, UiTokens.MinScale).ToString() & " of declared " & UiTokens.CompactFloor.ToString())

        ' Rule 3: one scale, monotonic across the supported range, every token grows with it; and an
        ' unknown scale resolves into the range instead of to zero or to a shrinking target.
        Dim previous = 0
        Dim monotonic = True
        Dim line As New List(Of String)
        For Each s In New Double() {0.875, 1.0, 1.25, 1.5, 1.75, 2.0}
            Dim t = UiTokens.Scaled(32, s)
            monotonic = monotonic AndAlso t >= previous AndAlso UiTokens.Scaled(12, s) >= 1 AndAlso UiTokens.Scaled(4, s) >= 1
            previous = t
            line.Add(s.ToString("0.###", Globalization.CultureInfo.InvariantCulture) & "=" & t.ToString())
        Next
        Check("ui-sizing:scale", monotonic AndAlso UiTokens.Scaled(32, 2.0) = 64, String.Join(" ", line.ToArray()))
        Check("ui-sizing:scale-clamp",
              UiTokens.Clamp(Double.NaN) = 1.0 AndAlso UiTokens.Clamp(Double.PositiveInfinity) = 1.0 AndAlso
              UiTokens.Clamp(0) = UiTokens.MinScale AndAlso UiTokens.Clamp(-3) = UiTokens.MinScale AndAlso
              UiTokens.Clamp(50) = UiTokens.MaxScale AndAlso UiTokens.Scaled(0, 0) >= 1, "NaN, infinity, zero, negative, huge")
    End Sub

    ' ---- BUTTONS ---------------------------------------------------------------------------------

    Private Sub CheckButtonsContract()
        Dim wasScale = UiTokens.InterfaceScale
        Try
            Using host As New Form
                Dim wasDpi = Theme.CurrentDpi
                Theme.CurrentDpi = Ui.DpiFor(host)
                Try
                    host.Font = Theme.FontBody()

                    ' Section 3: padding 12 per side, glyph 16, gap 8, ordinary single-line height 32.
                    Dim plain As New GlyphButton With {.Text = "Run", .Tier = 16}
                    plain.Glyph = GlyphRef.Vocabulary("action.verify")
                    host.Controls.Add(plain)
                    Dim s = plain.GetPreferredSize(Size.Empty)
                    Dim cap = TextRenderer.MeasureText("Run", plain.Font, New Size(Integer.MaxValue, Integer.MaxValue),
                                                       TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding Or TextFormatFlags.WordBreak Or
                                                       TextFormatFlags.Left Or TextFormatFlags.Top)
                    Dim expectedWidth = Ui.Px(plain, 16) + Ui.Px(plain, 8) + cap.Width + 2 * Ui.Px(plain, 12) + 2
                    Check("buttons:tokens", s.Width = expectedWidth AndAlso s.Height >= Ui.Px(plain, 32),
                          s.Width.ToString() & "x" & s.Height.ToString() & " expected width " & expectedWidth.ToString() & ", height >= " & Ui.Px(plain, 32).ToString())

                    ' Rule 3.3: the width is content plus padding - not the container's. A button asked to
                    ' fill a wide proposed size stays its content width.
                    Check("buttons:content-width", plain.GetPreferredSize(New Size(2000, 100)).Width = s.Width,
                          plain.GetPreferredSize(New Size(2000, 100)).Width.ToString() & " beside " & s.Width.ToString())

                    ' Rule 3.4: a caption is never ellipsized or clipped - it wraps and the button grows.
                    Dim flagsField = GetType(GlyphButton).GetField("CaptionFlags", BindingFlags.NonPublic Or BindingFlags.Static)
                    Dim flags = If(flagsField Is Nothing, TextFormatFlags.EndEllipsis, CType(flagsField.GetRawConstantValue(), TextFormatFlags))
                    Check("buttons:no-ellipsis",
                          flagsField IsNot Nothing AndAlso (flags And TextFormatFlags.EndEllipsis) = 0 AndAlso (flags And TextFormatFlags.WordBreak) <> 0,
                          "caption flags " & flags.ToString())
                    Dim longOne As New GlyphButton With {.Text = "A caption that is much longer than the width a layout gives this button", .Tier = 16}
                    longOne.Glyph = GlyphRef.Vocabulary("action.verify")
                    host.Controls.Add(longOne)
                    Dim oneLine = longOne.GetPreferredSize(Size.Empty)
                    longOne.MaximumSize = New Size(Ui.Px(longOne, 220), 0)
                    Dim wrapped = longOne.GetPreferredSize(Size.Empty)
                    Check("buttons:wrap-before-clip", wrapped.Width <= Ui.Px(longOne, 220) AndAlso wrapped.Height > oneLine.Height,
                          "one line " & oneLine.Width.ToString() & "x" & oneLine.Height.ToString() & ", wrapped " & wrapped.Width.ToString() & "x" & wrapped.Height.ToString())

                    ' Section 3 rule 4: every button in one visual row has the height of that row's tallest;
                    ' widths stay content-based.
                    Using flow As New FlowLayoutPanel With {.Width = Ui.Px(host, 420), .AutoSize = True,
                                                            .AutoSizeMode = AutoSizeMode.GrowAndShrink, .WrapContents = True}
                        host.Controls.Add(flow)
                        Dim a As New GlyphButton With {.Text = "OK", .Margin = New Padding(0)}
                        Dim b As New GlyphButton With {.Text = "A very long caption in one row that wraps within its own cap",
                                                       .Margin = New Padding(0), .MaximumSize = New Size(Ui.Px(host, 160), 0)}
                        Dim c As New GlyphButton With {.Text = "Close", .Margin = New Padding(0)}
                        flow.Controls.AddRange(New Control() {a, b, c})
                        flow.PerformLayout()
                        Dim widthsBefore = a.Width.ToString() & "," & c.Width.ToString()
                        Ui.EqualizeRowHeights(flow)
                        flow.PerformLayout()
                        Dim rows = flow.Controls.Cast(Of Control)().GroupBy(Function(x) x.Top)
                        Dim equal = rows.All(Function(r) r.Select(Function(x) x.Height).Distinct().Count() = 1)
                        Dim taller = b.Height > Ui.Px(host, 32)
                        Check("buttons:row-height", equal AndAlso taller AndAlso widthsBefore = a.Width.ToString() & "," & c.Width.ToString(),
                              String.Join(" | ", rows.Select(Function(r) String.Join(",", r.Select(Function(x) x.Width.ToString() & "x" & x.Height.ToString()).ToArray())).ToArray()))
                        host.Controls.Remove(flow)
                    End Using

                    ' The scale: every dimension of the button grows with the interface scale, composed once
                    ' with the dpi; the height never falls under the declared compact target.
                    Dim lastHeight = 0
                    Dim grows = True
                    Dim sizes As New List(Of String)
                    For Each sc In New Double() {0.875, 1.0, 1.5, 2.0}
                        UiTokens.InterfaceScale = sc
                        Dim m = plain.GetPreferredSize(Size.Empty)
                        grows = grows AndAlso m.Height >= lastHeight AndAlso m.Height >= Ui.Px(plain, UiTokens.CompactFloor)
                        lastHeight = m.Height
                        sizes.Add(sc.ToString("0.###", Globalization.CultureInfo.InvariantCulture) & "=" & m.Width.ToString() & "x" & m.Height.ToString())
                    Next
                    UiTokens.InterfaceScale = wasScale
                    Check("buttons:scale", grows, String.Join(" ", sizes.ToArray()))
                    host.Controls.Remove(plain)
                    host.Controls.Remove(longOne)
                    plain.Dispose()
                    longOne.Dispose()
                Finally
                    Theme.CurrentDpi = wasDpi
                End Try
            End Using
        Finally
            UiTokens.InterfaceScale = wasScale
        End Try

        ' Rule 3.5: no answer face carries a green or red tint - every non-fill face is the ordinary
        ' neutral surface, in both palettes; only the hue of the affirmative and caution glyph differs.
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim t = If(dark, "dark", "light")
            Dim neutral As New List(Of String)
            For Each face In New GlyphButtonFace() {GlyphButtonFace.Normal, GlyphButtonFace.AnswerOk, GlyphButtonFace.AnswerWarning,
                                                    GlyphButtonFace.AnswerError, GlyphButtonFace.AnswerErrorSafe}
                Dim b As New GlyphButton With {.Face = face}
                If b.ColoursFor(p).Back.ToArgb() <> p.SurfaceAlt.ToArgb() Then neutral.Add(face.ToString())
                b.Dispose()
            Next
            Check("buttons:neutral-answers:" & t, neutral.Count = 0, If(neutral.Count = 0, "5 of 5 on the neutral face", "tinted: " & String.Join(",", neutral.ToArray())))
            Dim cancelGlyph As New GlyphButton With {.Face = ShellDialog.FaceOf(2, 1, 1, 0, -1)}
            Check("buttons:cancel-mono:" & t, cancelGlyph.ColoursFor(p).Glyph.ToArgb() = p.Text.ToArgb(), "the dismissing answer's glyph is the text ink")
            cancelGlyph.Dispose()
        Next
    End Sub

    ' ---- COLLAPSIBLE-GROUP -----------------------------------------------------------------------

    Private Sub CheckGroupContract()
        Using host As New Form
            Dim wasDpi = Theme.CurrentDpi
            Theme.CurrentDpi = Ui.DpiFor(host)
            Try
                Dim g As New SettingsGroup("probe", "A topic")
                host.Controls.Add(g)
                g.Width = Ui.Px(host, 600)
                g.PerformLayout()

                ' 3.6: header hit height 32, body inset 12, sibling gap 8.
                Check("group:tokens",
                      g.Header.Height >= Ui.Px(host, 32) AndAlso g.Body.Padding.Left = Ui.Px(host, 12) AndAlso
                      g.Body.Padding.Right = Ui.Px(host, 12) AndAlso g.Margin.Bottom = Ui.Px(host, 8),
                      "header " & g.Header.Height.ToString() & ", body inset " & g.Body.Padding.Left.ToString() & ", gap " & g.Margin.Bottom.ToString())

                ' 3.2: a filled triangle: right when collapsed, down when expanded; left when collapsed in RTL.
                g.Header.Collapsed = True
                Dim sq = g.Header.MarkerSquare(g.Header.Height)
                Dim pc = g.Header.MarkerPoints(sq)
                Dim collapsedRight = pc(2).X > pc(0).X AndAlso pc(0).X = pc(1).X AndAlso pc(0).Y < pc(1).Y
                g.Header.Collapsed = False
                Dim pe = g.Header.MarkerPoints(sq)
                Dim expandedDown = pe(2).Y > pe(0).Y AndAlso pe(0).Y = pe(1).Y AndAlso pe(0).X < pe(1).X
                g.Header.RightToLeft = RightToLeft.Yes
                g.Header.Collapsed = True
                Dim pr = g.Header.MarkerPoints(g.Header.MarkerSquare(g.Header.Height))
                Dim collapsedLeft = pr(2).X < pr(0).X
                g.Header.Collapsed = False
                Dim pd = g.Header.MarkerPoints(g.Header.MarkerSquare(g.Header.Height))
                Dim rtlExpandedDown = pd(2).Y > pd(0).Y
                g.Header.RightToLeft = RightToLeft.No
                Check("group:marker", collapsedRight AndAlso expandedDown AndAlso collapsedLeft AndAlso rtlExpandedDown,
                      "right " & collapsedRight.ToString() & ", down " & expandedDown.ToString() & ", rtl left " & collapsedLeft.ToString() & ", rtl down " & rtlExpandedDown.ToString())
                Check("group:marker-edge", g.Header.MarkerSquare(g.Header.Height).Left < g.Header.CaptionBounds(g.Header.Width, g.Header.Height).Left,
                      "the marker stands before the caption at the reading start")

                ' 3.1: a face that is not the body's, and a stronger weight than body text.
                For Each dark In New Boolean() {False, True}
                    Dim p = Theme.PaletteFor(dark)
                    Dim t = If(dark, "dark", "light")
                    Check("group:header-face:" & t,
                          GroupHeader.FaceFor(p, False).ToArgb() <> p.Surface.ToArgb() AndAlso GroupHeader.FaceFor(p, False).ToArgb() <> GroupHeader.FaceFor(p, True).ToArgb(),
                          "the header face differs from the body surface and from its own hover")
                    ContrastPair("group:contrast:" & t & ":caption", p.Text, GroupHeader.FaceFor(p, False), 4.5)
                    ContrastPair("group:contrast:" & t & ":caption-hover", p.Text, GroupHeader.FaceFor(p, True), 4.5)
                    ContrastPair("group:contrast:" & t & ":marker", p.Text, GroupHeader.FaceFor(p, False), 3.0)
                    ContrastPair("group:contrast:" & t & ":marker-hover", p.Text, GroupHeader.FaceFor(p, True), 3.0)
                Next
                Check("group:header-weight", RailEntry.LabelFont(False, True, Ui.DpiFor(host)).Bold, "bold caption")

                ' 3.3: the whole header toggles once; Enter and Space reach it; the state is reported.
                Dim before = g.Expanded
                g.Header.PerformClick()
                Dim afterOne = g.Expanded
                g.Header.PerformClick()
                Check("group:activation", afterOne <> before AndAlso g.Expanded = before, "one click flips it once, a second flips it back")
                Dim handleForAccessibility = g.Header.Handle   ' the state is read through the control's window
                Dim acc = g.Header.AccessibilityObject
                g.Expanded = True
                Dim stateOpen = (acc.State And AccessibleStates.Expanded) <> 0
                g.Expanded = False
                Dim stateShut = (acc.State And AccessibleStates.Collapsed) <> 0
                Check("group:a11y", stateOpen AndAlso stateShut AndAlso acc.Role = AccessibleRole.PushButton, "expanded and collapsed are exposed")

                ' 3.4: a collapsed body takes no layout space and holds no reachable focus target.
                g.Expanded = False
                Check("group:collapsed-body", Not g.Body.Visible, "the body is hidden")
                host.Controls.Remove(g)
                g.Dispose()
            Finally
                Theme.CurrentDpi = wasDpi
            End Try
        End Using
    End Sub

    ' ---- BUTTONS section 4 rule 4: dialog answer order --------------------------------------------

    Private Sub CheckDialogAnswerOrder()
        ' The pure rule over the shapes the dialogs are built from.
        Dim cases = New Object()() {
            New Object() {"destructive (action, cancel)", 2, 1, 1, 0, New Integer() {1, 0}},
            New Object() {"problem (action, logs, close)", 3, 2, 0, -1, New Integer() {2, 1, 0}},
            New Object() {"disk busy (wait, stop, background)", 3, 0, 0, 1, New Integer() {0, 2, 1}},
            New Object() {"lone close", 1, 0, 0, -1, New Integer() {0}},
            New Object() {"yes/no, no default change", 2, 1, 1, -1, New Integer() {1, 0}}}
        For Each c In cases
            Dim got = ShellDialog.AnswerOrder(CInt(c(1)), CInt(c(2)), CInt(c(3)), CInt(c(4)))
            Dim want = DirectCast(c(5), Integer())
            Check("dialog:answer-order:rule:" & CStr(c(0)), got.SequenceEqual(want), String.Join(",", got.Select(Function(i) i.ToString()).ToArray()) & " of " & String.Join(",", want.Select(Function(i) i.ToString()).ToArray()))
        Next

        ' Every confirmation of a destructive action, in every language, as the dialog really lays it out:
        ' Cancel is leftmost (and holds the default), the acting answer is last.
        Dim bad As New List(Of String)
        Dim total = 0
        For Each lang In Localization.Languages
            For Each entry In DestructiveDialogs.AllForTest(Localization.GetDict(lang))
                total += 1
                Dim spec = entry.Value
                Dim shown = ShellDialog.DisplayedOrderOf(spec)
                If shown.Length <> spec.Choices.Length OrElse shown.First() <> spec.Choices(spec.CancelAt) OrElse shown.Last() <> spec.Choices(spec.DangerAt) Then
                    bad.Add(entry.Key & ":" & lang)
                End If
            Next
        Next
        Check("dialog:answer-order:destructive", bad.Count = 0, If(bad.Count = 0, total.ToString() & " dialogs: Cancel first, the acting answer last", String.Join(", ", bad.Take(6).ToArray())))
    End Sub
End Module
