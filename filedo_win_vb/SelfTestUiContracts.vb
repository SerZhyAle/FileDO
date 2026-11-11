Imports System.Reflection

' The unified UI contracts of SP-0169 (UI-SIZING 1.1, BUTTONS 2.0, COLLAPSIBLE-GROUP 1.1), rung 1: the
' token resolver, the button geometry, the group header and the order of a dialog's answers, measured on
' real controls built (not shown) in this process. What a person must still look at - the showcase
' captures at the sizes and in the languages the contracts list - is slice S4, not here.
'
' Rows are named `ui-sizing:`, `buttons:`, `group:`, `dialog:answer-order:`, `option:` (BOOLEAN-OPTION
' 2.0), `settings-nav:` and `about:` (SETTINGS-NAVIGATION 2.0); the group filter
' FILEDO_SELFTEST_ONLY=ui-contracts runs just these.
Partial Public Module SelfTest

    Private Sub CheckUiContracts()
        CheckUiSizing()
        CheckButtonsContract()
        CheckButtonStateWidth()
        CheckGroupContract()
        CheckDialogAnswerOrder()
        CheckBooleanOptionContract()
        CheckOptionsAdopted()
        CheckSettingsNavigation()
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

    ' BUTTONS section 3 rule 3: a stateful button is as wide in every state as in its widest, and no
    ' narrower than a plain button showing that widest caption.
    Private Sub CheckButtonStateWidth()
        Using host As New Form
            Dim wasDpi = Theme.CurrentDpi
            Theme.CurrentDpi = Ui.DpiFor(host)
            Try
                host.Font = Theme.FontBody()
                Dim states = New String() {"shell_btn_stop", "shell_btn_remove_copy", "shell_btn_end_run_now"}
                Dim stateful As New GlyphButton With {.Text = Localization.T(states(0)), .Tier = 16}
                stateful.Glyph = GlyphRef.Vocabulary("action.cancel")
                host.Controls.Add(stateful)
                stateful.ReservedCaptionKeys = New String() {states(1), states(2)}
                Dim widths As New List(Of Integer)
                Dim widest = 0
                For Each key In states
                    stateful.Text = Localization.T(key)
                    widths.Add(stateful.GetPreferredSize(Size.Empty).Width)
                    Dim plain As New GlyphButton With {.Text = Localization.T(key), .Tier = 16}
                    plain.Glyph = GlyphRef.Vocabulary("action.cancel")
                    host.Controls.Add(plain)
                    widest = Math.Max(widest, plain.GetPreferredSize(Size.Empty).Width)
                    host.Controls.Remove(plain)
                    plain.Dispose()
                Next
                Check("buttons:state-width", widths.Distinct().Count() = 1 AndAlso widths(0) = widest,
                      String.Join(",", widths.Select(Function(w) w.ToString()).ToArray()) & " of widest " & widest.ToString())
                host.Controls.Remove(stateful)
                stateful.Dispose()
            Finally
                Theme.CurrentDpi = wasDpi
            End Try
        End Using
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

    ' ---- BOOLEAN-OPTION --------------------------------------------------------------------------

    Private Sub CheckBooleanOptionContract()
        Using host As New Form
            Dim wasDpi = Theme.CurrentDpi
            Theme.CurrentDpi = Ui.DpiFor(host)
            Try
                host.Font = Theme.FontBody()
                Dim o As New BooleanOption With {.Text = "Keep history", .Font = Theme.FontBody()}
                o.Explanation = "Records every run on this PC so it can be opened again from the History page."
                host.Controls.Add(o)
                Dim lay = o.LayoutFor(Integer.MaxValue)
                Dim boxPx = Ui.Px(o, UiTokens.OptionBox())
                Dim cellGap = Ui.Px(o, UiTokens.OptionCellGap())
                Dim headBottom = Math.Max(lay.Box.Bottom, lay.Caption.Bottom)

                ' 2.4: the box is at least 20, 8 from the cell; the explanation is 4 under the heading.
                Check("option:tokens",
                      lay.Box.Width = boxPx AndAlso lay.Box.Height = boxPx AndAlso boxPx >= Ui.Px(o, 20) AndAlso
                      lay.Caption.Left - lay.Box.Right = cellGap AndAlso lay.Note.Top - headBottom = Ui.Px(o, UiTokens.OptionExplanationGap()),
                      "box " & boxPx.ToString() & ", cell gap " & (lay.Caption.Left - lay.Box.Right).ToString() & ", note gap " & (lay.Note.Top - headBottom).ToString())

                ' 2.1: the box at the reading start, on the first heading line - also when the explanation is long.
                Dim lineH = TextRenderer.MeasureText("Ag", o.Font).Height
                Dim boxMid = lay.Box.Top + lay.Box.Height \ 2
                Dim narrow = o.LayoutFor(Ui.Px(o, 200))
                Check("option:anatomy",
                      lay.Box.Left = 0 AndAlso lay.Note.Left = lay.Caption.Left AndAlso lay.Note.Top >= lay.Caption.Bottom AndAlso
                      boxMid >= lay.Caption.Top AndAlso boxMid <= lay.Caption.Top + lineH AndAlso
                      narrow.Note.Height > lay.Note.Height AndAlso narrow.Box.Top = lay.Box.Top,
                      "box " & lay.Box.ToString() & ", caption " & lay.Caption.ToString() & ", narrow note " & narrow.Note.Height.ToString())

                ' 2.2: the glyph stands before the name, 8 from the box and 8 from the caption, at tier 16.
                o.Glyph = GlyphRef.Vocabulary("action.verify")
                Dim withGlyph = o.LayoutFor(Integer.MaxValue)
                Check("option:heading",
                      withGlyph.GlyphSquare.Width = Ui.Px(o, UiTokens.CaptionGlyph()) AndAlso
                      withGlyph.GlyphSquare.Left - withGlyph.Box.Right = cellGap AndAlso
                      withGlyph.Caption.Left - withGlyph.GlyphSquare.Right = cellGap AndAlso withGlyph.Note.Left = withGlyph.GlyphSquare.Left,
                      "glyph " & withGlyph.GlyphSquare.ToString() & ", caption " & withGlyph.Caption.ToString())
                o.Glyph = Nothing

                ' 2.3 and 2.6: an obvious option reserves no second row and is one ordinary target high.
                Dim bare As New BooleanOption With {.Text = "Quiet", .Font = Theme.FontBody()}
                host.Controls.Add(bare)
                Check("option:no-empty-row", bare.LayoutFor(Integer.MaxValue).Note.Height = 0 AndAlso
                      bare.GetPreferredSize(Size.Empty).Height = Ui.Px(bare, UiTokens.Target()),
                      bare.GetPreferredSize(Size.Empty).ToString())

                ' 2.5: long text wraps in the width a layout gives and grows the row; nothing is cut.
                Dim wide = o.GetPreferredSize(Size.Empty)
                o.MaximumSize = New Size(Ui.Px(o, 200), 0)
                Dim wrapped = o.GetPreferredSize(Size.Empty)
                Check("option:wrap-before-clip", wrapped.Width <= Ui.Px(o, 200) AndAlso wrapped.Height > wide.Height,
                      "one line " & wide.ToString() & ", wrapped " & wrapped.ToString())
                o.MaximumSize = Size.Empty
                o.Size = o.GetPreferredSize(Size.Empty)
                lay = o.LayoutFor(o.Width)

                ' 3.1: the box and the caption toggle once each; the explanation and the space after the
                ' caption do not.
                o.Checked = False
                Dim boxPoint As New Point(lay.Box.Left + lay.Box.Width \ 2, lay.Box.Top + lay.Box.Height \ 2)
                Dim capPoint As New Point(lay.Caption.Left + lay.Caption.Width \ 2, lay.Caption.Top + lay.Caption.Height \ 2)
                Dim notePoint As New Point(lay.Note.Left + lay.Note.Width \ 2, lay.Note.Top + lay.Note.Height \ 2)
                Dim afterCaption As New Point(Math.Min(o.Width - 1, lay.Caption.Right + cellGap), capPoint.Y)
                o.ClickAtForTest(boxPoint)
                Dim byBox = o.Checked
                o.ClickAtForTest(capPoint)
                Dim byCaption = Not o.Checked
                o.ClickAtForTest(notePoint)
                Dim noteInert = Not o.Checked
                o.ClickAtForTest(afterCaption)
                Dim afterInert = Not o.Checked OrElse afterCaption.X < lay.Caption.Right
                Check("option:activation", byBox AndAlso byCaption AndAlso noteInert AndAlso afterInert,
                      "box " & byBox.ToString() & ", caption " & byCaption.ToString() & ", explanation inert " & noteInert.ToString() & ", after caption inert " & afterInert.ToString())

                ' Right to left: the box is mirrored to the right edge and toggles there; the left edge does not.
                o.RightToLeft = RightToLeft.Yes
                o.Checked = False
                o.ClickAtForTest(New Point(o.Width - 1 - boxPoint.X, boxPoint.Y))
                Dim rtlBox = o.Checked
                o.ClickAtForTest(boxPoint)
                Dim rtlLeftInert = o.Checked
                o.RightToLeft = RightToLeft.No
                Check("option:rtl", rtlBox AndAlso rtlLeftInert, "mirrored box " & rtlBox.ToString() & ", left edge inert " & rtlLeftInert.ToString())

                ' 3.5: a disabled option toggles from no path and shows why under its heading.
                o.Checked = False
                o.DisabledReason = "Locked by the sealed copy."
                o.Enabled = False
                o.ClickAtForTest(boxPoint)
                o.ClickAtForTest(capPoint)
                Check("option:disabled", Not o.Checked AndAlso o.ShownNote().Contains("Locked by the sealed copy.") AndAlso
                      o.ColoursFor(Theme.Current).Caption.ToArgb() = Theme.Current.TextDisabled.ToArgb(),
                      "checked " & o.Checked.ToString() & ", note " & o.ShownNote())
                o.Enabled = True
                Check("option:disabled-reason-hidden-when-enabled", o.ShownNote() = o.Explanation, o.ShownNote())

                ' 3.3: one named checkbox, the caption its name and the explanation its description.
                Dim handleForAccessibility = o.Handle
                Dim acc = o.AccessibilityObject
                Check("option:a11y", acc.Role = AccessibleRole.CheckButton AndAlso acc.Name = o.Text AndAlso acc.Description = o.Explanation,
                      acc.Role.ToString() & " '" & acc.Name & "' / '" & acc.Description & "'")

                ' 3.5: the outline and the check reach 3:1 on the surface, caption and explanation 4.5:1.
                For Each dark In New Boolean() {False, True}
                    Dim p = Theme.PaletteFor(dark)
                    Dim t = If(dark, "dark", "light")
                    o.ForeColor = p.Text
                    Dim c = o.ColoursFor(p)
                    ContrastPair("option:contrast:" & t & ":outline", c.Outline, p.Surface, 3.0)
                    ContrastPair("option:contrast:" & t & ":checked-box", c.Fill, p.Surface, 3.0)
                    ContrastPair("option:contrast:" & t & ":mark", c.Mark, c.Fill, 3.0)
                    ContrastPair("option:contrast:" & t & ":caption", c.Caption, p.Surface, 4.5)
                    ContrastPair("option:contrast:" & t & ":explanation", c.Note, p.Surface, 4.5)
                Next

                host.Controls.Remove(o)
                host.Controls.Remove(bare)
                o.Dispose()
                bare.Dispose()
            Finally
                Theme.CurrentDpi = wasDpi
            End Try
        End Using
    End Sub

    ' Every boolean option the shell and the Disk Manager build is the shared component (no native
    ' check box is left), and the Settings rows carry their explanation in it. In one flow of glyph
    ' buttons with uncapped captions every button asks for the same height (BUTTONS 3.4).
    Private Sub CheckOptionsAdopted()
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            Dim native = AllControls(shell).Where(Function(c) TypeOf c Is CheckBox AndAlso Not TypeOf c Is BooleanOption).
                Select(Function(c) c.Name & " '" & c.Text & "'").ToList()
            Check("option:adopted:shell", native.Count = 0, If(native.Count = 0, "no native check box", String.Join("; ", native.Take(8).ToArray())))
            Dim uneven As New List(Of String)
            For Each flow In AllControls(shell).OfType(Of FlowLayoutPanel)()
                Dim heights = flow.Controls.OfType(Of GlyphButton)().
                    Where(Function(b) Not b.IconOnly AndAlso b.MaximumSize.Width <= 0).
                    Select(Function(b) b.GetPreferredSize(Size.Empty).Height).Distinct().ToList()
                If heights.Count > 1 Then uneven.Add(flow.Name & " " & String.Join("/", heights.Select(Function(h) h.ToString()).ToArray()))
            Next
            Check("buttons:page-row-heights", uneven.Count = 0, If(uneven.Count = 0, "every flow of buttons asks for one height", String.Join("; ", uneven.Take(6).ToArray())))
        Catch ex As Exception
            Check("option:adopted:shell", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try

        Dim m As DiskManagerForm = Nothing
        Try
            m = New DiskManagerForm()
            Dim native = AllControls(m).Where(Function(c) TypeOf c Is CheckBox AndAlso Not TypeOf c Is BooleanOption).
                Select(Function(c) c.Name & " '" & c.Text & "'").ToList()
            Check("option:adopted:disk-manager", native.Count = 0, If(native.Count = 0, "no native check box", String.Join("; ", native.Take(8).ToArray())))
        Catch ex As Exception
            Check("option:adopted:disk-manager", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If m IsNot Nothing Then m.Dispose()
        End Try

        Dim page As SettingsView = Nothing
        Try
            page = New SettingsView()
            Dim rows = page.Panel.Groups.SelectMany(Function(g) g.Body.Controls.OfType(Of SettingRow)()).
                Where(Function(r) TypeOf r.Editor Is CheckBox).ToList()
            Dim bad = rows.Where(Function(r) Not TypeOf r.Editor Is BooleanOption OrElse DirectCast(r.Editor, BooleanOption).Explanation = "").
                Select(Function(r) r.Name).ToList()
            Check("option:settings-explained", rows.Count > 0 AndAlso bad.Count = 0,
                  rows.Count.ToString() & " options" & If(bad.Count = 0, "", ", unexplained: " & String.Join(", ", bad.ToArray())))
        Catch ex As Exception
            Check("option:settings-explained", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If page IsNot Nothing Then page.Dispose()
        End Try
    End Sub

    ' ---- SETTINGS-NAVIGATION ---------------------------------------------------------------------

    Private Sub CheckSettingsNavigation()
        ' Section 2.5 (decision D4): a 32 row, a 16 glyph, 8 from glyph to caption; section 2.4: a child
        ' stands in by one shared token, 12, together with its glyph, so its glyph-to-caption gap holds.
        Dim unit = ShellForm.RailTargetHeight
        Using lone As New RailEntry With {.RowUnit = unit, .Size = New Size(240, unit), .Text = "x"},
              child As New RailEntry With {.RowUnit = unit, .Size = New Size(240, unit), .Text = "x", .IsChild = True}
            Dim g = lone.GlyphSquare(lone.Height)
            Dim gap = lone.LabelBounds(lone.Width, lone.Height).Left - g.Right
            Dim indent = child.GlyphSquare(child.Height).Left - g.Left
            Dim labelShift = child.LabelBounds(child.Width, child.Height).Left - lone.LabelBounds(lone.Width, lone.Height).Left
            Check("settings-nav:tokens", unit = 32 AndAlso g.Width = 16 AndAlso g.Height = 16 AndAlso gap = 8 AndAlso
                  indent = RailEntry.ChildIndentDesign AndAlso labelShift = indent,
                  "row " & unit.ToString() & ", glyph " & g.Width.ToString() & ", gap " & gap.ToString() &
                  ", child indent " & indent.ToString() & ", child label shift " & labelShift.ToString())
        End Using

        ' Section 2.3 (decision D3): one glyph treatment for the whole rail, identity-coloured - every row
        ' names a hue, and a row inside a group names its group's (the rail-hue rows hold each one).
        Dim mono = RailRow.All.Where(Function(r) r.Hue = "").Select(Function(r) r.Key).ToList()
        Check("settings-nav:ink", mono.Count = 0,
              If(mono.Count = 0, "identity-coloured: " & RailRow.All.Length.ToString() & " rows", "mono: " & String.Join(", ", mono.ToArray())))

        CheckAboutSet()
        CheckSettingsDialogNavigation()
        CheckNarrowNavigation()
    End Sub

    ' Section 2.6 (decision D2): below the documented breakpoint the rail becomes a navigation view of
    ' its own - the Pages button opens it, Escape or a chosen row brings back the page - and the design
    ' minimum never reaches the breakpoint. Driven through the shell's width seam, not a shown window.
    Private Sub CheckNarrowNavigation()
        Check("settings-nav:breakpoint", ShellForm.RailWidth + ShellForm.NarrowContentWidth < ShellForm.MinDesignWidth - 20,
              "narrow below a client width of " & (ShellForm.RailWidth + ShellForm.NarrowContentWidth).ToString() &
              ", design minimum " & ShellForm.MinDesignWidth.ToString() & " outer")
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            shell.SetClientWidthForTest(Ui.Px(shell, 600))
            Dim narrowPage = shell.NarrowStateForTest
            shell.OpenNavigationForTest()
            Dim list = shell.NarrowStateForTest
            shell.HandleKeyForTest(Keys.Escape)
            Dim escaped = shell.NarrowStateForTest
            shell.OpenNavigationForTest()
            Dim row = shell.RailEntriesForTest().First(Function(e) e.Key = "rail_job_history")
            row.PerformClick()
            Dim chosen = shell.NarrowStateForTest
            Dim chosenPage = shell.CurrentRailKey()
            shell.SetClientWidthForTest(Ui.Px(shell, 1000))
            Dim wide = shell.NarrowStateForTest
            Dim ok = narrowPage = "narrow/page/rail Absolute 0/pages shown" AndAlso
                     list = "narrow/list/rail Percent 100/pages hidden" AndAlso
                     escaped = narrowPage AndAlso chosen = narrowPage AndAlso chosenPage = "rail_job_history" AndAlso
                     wide = "wide/page/rail Absolute " & Ui.Px(shell, ShellForm.RailWidth).ToString() & "/pages hidden"
            Check("settings-nav:narrow", ok, narrowPage & " | " & list & " | " & escaped & " | " & chosen & " " & chosenPage & " | " & wide)
        Catch ex As Exception
            Check("settings-nav:narrow", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

    ' Section 2.1 / 2.4 (decision D5): the standalone Disk manager's settings window keeps page
    ' navigation - Settings first, About last, the shell's own rows - and section 4.3: switching pages
    ' keeps the window's size and the values on the Settings page, and selection is not colour alone.
    Private Sub CheckSettingsDialogNavigation()
        Dim dlg As DiskSettingsDialog = Nothing
        Try
            dlg = New DiskSettingsDialog()
            Dim rows = dlg.NavRowsForTest
            Dim order = String.Join(",", rows.Select(Function(r) r.Key).ToArray())
            Dim sizeBefore = dlg.Size
            Dim startupBefore = dlg.Panel.StartupCombo.SelectedIndex
            dlg.ShowPage("rail_job_about")
            Dim onAbout = dlg.CurrentPage = "rail_job_about" AndAlso dlg.AboutForTest IsNot Nothing AndAlso
                          rows.Single(Function(r) r.Selected).Key = "rail_job_about"
            dlg.ShowPage("rail_job_settings")
            Dim back = dlg.CurrentPage = "rail_job_settings" AndAlso rows.Single(Function(r) r.Selected).Key = "rail_job_settings"
            Dim kept = dlg.Size = sizeBefore AndAlso dlg.Panel.StartupCombo.SelectedIndex = startupBefore
            Dim sameRows = rows.All(Function(r) r.Glyph IsNot Nothing AndAlso r.Hue <> "" AndAlso
                                                r.Hue = RailRow.All.First(Function(x) x.Key = r.Key).Hue)
            Check("settings-nav:dialog", order = "rail_job_settings,rail_job_about" AndAlso onAbout AndAlso back AndAlso kept AndAlso sameRows,
                  "rows " & order & ", about " & onAbout.ToString() & ", back " & back.ToString() &
                  ", size and values kept " & kept.ToString() & ", shell rows " & sameRows.ToString())
        Catch ex As Exception
            Check("settings-nav:dialog", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If dlg IsNot Nothing Then dlg.Dispose()
        End Try
    End Sub

    ' Section 3.3: About carries the complete set - description, running version, licence and privacy,
    ' help, the site, support, the diagnostic export, download/update information, the repository and
    ' the resource index - and section 3.4: the related programs from the one inventory, each name and
    ' purpose in every language. Built, not shown; opening it contacts nothing (the links are clicks).
    Private Sub CheckAboutSet()
        Dim view As AboutView = Nothing
        Try
            view = New AboutView()
            Dim all = AllControls(view).ToList()
            Dim urls = all.OfType(Of LinkLabel)().Select(Function(l) CStr(l.Tag)).ToList()
            Dim required As New Dictionary(Of String, String) From {
                {"site", Links.Site}, {"releases", Links.Releases}, {"repository", Links.GitHub},
                {"support", Links.Support}, {"issues", Links.Issues}, {"privacy", Links.Privacy},
                {"license", Links.License}, {"index", Links.Topics}}
            Dim missing = required.Where(Function(r) Not urls.Contains(r.Value)).Select(Function(r) r.Key).ToList()
            Dim description = Localization.T("about_description")
            If Not all.OfType(Of Label)().Any(Function(l) l.Text = description AndAlso description <> "about_description") Then missing.Add("description")
            If Not urls.Contains("") Then missing.Add("help")
            If Not all.Any(Function(c) c.Name = "help:build") Then missing.Add("version")
            If Not all.Any(Function(c) c.Name = "help:logs") Then missing.Add("diagnostic export")
            Check("about:resources", missing.Count = 0,
                  If(missing.Count = 0, required.Count.ToString() & " addresses, help, version, description, diagnostic export",
                     "missing: " & String.Join(", ", missing.ToArray())))

            Dim relatedFlow = all.FirstOrDefault(Function(c) c.Name = "about:related")
            Dim shown = If(relatedFlow Is Nothing, New List(Of String),
                           relatedFlow.Controls.OfType(Of LinkLabel)().Select(Function(l) CStr(l.Tag)).ToList())
            Dim untranslated As New List(Of String)
            For Each lang In Localization.Languages
                Dim dict = Localization.GetDict(lang)
                For Each product In Links.Related
                    For Each key In New String() {product(1), product(2)}
                        Dim v As String = Nothing
                        If Not dict.TryGetValue(key, v) OrElse v = "" Then untranslated.Add(lang & ":" & key)
                    Next
                Next
            Next
            Dim absent = Links.Related.Where(Function(p) Not shown.Contains(p(0))).Select(Function(p) p(0)).ToList()
            Check("about:related", Links.Related.Length > 0 AndAlso absent.Count = 0 AndAlso untranslated.Count = 0,
                  Links.Related.Length.ToString() & " related" &
                  If(absent.Count = 0, "", ", not shown: " & String.Join(", ", absent.ToArray())) &
                  If(untranslated.Count = 0, "", ", untranslated: " & String.Join(", ", untranslated.ToArray())))
        Catch ex As Exception
            Check("about:resources", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If view IsNot Nothing Then view.Dispose()
        End Try
    End Sub
End Module
