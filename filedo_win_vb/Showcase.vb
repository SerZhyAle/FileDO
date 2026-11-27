' `ui-showcase capture --out <folder>` (frozen spelling `--capture-ui-showcase <folder>`): the
' unified UI showcase of SP-0169 slice S4 (UI-SIZING 1.1 section 5, BUTTONS 2.0 section 5,
' BOOLEAN-OPTION 2.0 section 5, COLLAPSIBLE-GROUP 1.1, SETTINGS-NAVIGATION 2.0, WINDOWS-UI section 15).
'
' One page of the shared components - every button rank and state, a stateful button, the boolean
' option's shapes, an open and a shut group, the rail's row kinds - and one destructive dialog, built
' from the product's own classes in the product's own words, then drawn off-screen and saved as PNG.
' The combinations are a baseline plus the risky ones the contracts name (UI-SIZING 5: "baseline plus
' risk-based coverage rather than the full Cartesian product"): every language at the ordinary size,
' both palettes and the high-contrast system colours, compact to huge, an independent text
' enlargement, a narrow width, and the longest translations at the largest size in the narrow width.
'
' Each combination is also measured, and every measurement is a row of geometry.csv: hit targets
' against the scale's ordinary target, captions not clipped, siblings not overlapping, a visual row of
' buttons at one height, nothing wider than the content width, dialog answers inside the dialog, and
' the contrast of every enabled caption, glyph and box. The run exits 0 when every file was written
' and every row passed, 1 otherwise; summary.txt names the failures.
'
' The interface scale is the resolver's (UiTokens.InterfaceScale) and the monitor's DPI stays what it
' is; the DPI the run saw is in every row. Nothing is written to HKCU: the language, the palette and
' the scale are forced in process and put back.
Module Showcase

    Private NotInheritable Class Combo
        Friend ReadOnly Id As String
        Friend ReadOnly Palette As String
        Friend ReadOnly Scale As Double
        Friend ReadOnly TextScale As Double
        Friend ReadOnly Lang As String
        Friend ReadOnly Narrow As Boolean

        Friend Sub New(palette As String, scale As Double, textScale As Double, lang As String, narrow As Boolean)
            Me.Palette = palette
            Me.Scale = scale
            Me.TextScale = textScale
            Me.Lang = lang
            Me.Narrow = narrow
            Id = palette & "-s" & Num(scale) & "-t" & Num(textScale) & "-" & lang & "-" & If(narrow, "narrow", "wide")
        End Sub
    End Class

    ' Design pixels of the content column: an ordinary page, and the narrowest the shell's page keeps.
    Private Const WideWidth As Integer = 960
    Private Const NarrowWidth As Integer = 360

    Private Function Combos() As List(Of Combo)
        Dim list As New List(Of Combo)
        ' Baseline: every authored language at the ordinary size, light, wide.
        For Each lang In Localization.Languages
            list.Add(New Combo("light", 1.0, 1.0, lang, False))
        Next
        ' Palettes.
        list.Add(New Combo("dark", 1.0, 1.0, "en", False))
        list.Add(New Combo("contrast", 1.0, 1.0, "en", False))
        ' Sizes, compact to huge.
        For Each s In New Double() {UiTokens.MinScale, 1.25, 1.5, UiTokens.MaxScale}
            list.Add(New Combo("light", s, 1.0, "en", False))
        Next
        ' Text enlargement on its own, and the narrow width on its own.
        list.Add(New Combo("light", 1.0, 1.5, "en", False))
        list.Add(New Combo("light", 1.0, 1.0, "en", True))
        ' Risky combinations: the longest translations large and narrow; dark narrow; compact narrow;
        ' high contrast with enlarged text.
        list.Add(New Combo("light", UiTokens.MaxScale, 1.5, "de", True))
        list.Add(New Combo("dark", 1.0, 1.0, "fr", True))
        list.Add(New Combo("light", UiTokens.MinScale, 1.0, "ru", True))
        list.Add(New Combo("contrast", 1.5, 1.5, "uk", False))
        list.Add(New Combo("dark", UiTokens.MaxScale, 1.0, "de", False))
        Return list
    End Function

    Private rows As List(Of String)
    Private failures As List(Of String)
    Private current As Combo
    Private dpiSeen As Integer

    ' 0 when every file was written and every measurement passed, 1 otherwise.
    Public Function Run(folder As String) As Integer
        rows = New List(Of String) From {"combo,palette,scale,text,lang,width,dpi,kind,name,check,value,need,result"}
        failures = New List(Of String)
        Dim failed = False
        Dim wasScale = UiTokens.InterfaceScale
        Dim wasDpi = Theme.CurrentDpi
        Dim shots = 0
        Try
            IO.Directory.CreateDirectory(folder)
            For Each c In Combos()
                current = c
                Try
                    ApplyCombo(c)
                    Dim page = IO.Path.Combine(folder, "showcase-" & c.Id & ".png")
                    Try
                        CapturePage(page)
                    Finally
                        ReleaseFonts()
                    End Try
                    Console.WriteLine(page)
                    shots += 1
                    Dim dlg = IO.Path.Combine(folder, "dialog-" & c.Id & ".png")
                    CaptureDialog(dlg)
                    Console.WriteLine(dlg)
                    shots += 1
                Catch ex As Exception
                    failed = True
                    failures.Add(c.Id & ": " & ex.GetType().Name)
                    ShellLog.Write("ui-showcase " & c.Id, ex)
                    Console.Error.WriteLine("showcase failed: " & c.Id & " (" & ex.GetType().Name & ", see the shell log)")
                End Try
            Next
            IO.File.WriteAllLines(IO.Path.Combine(folder, "geometry.csv"), rows.ToArray(), New Text.UTF8Encoding(False))
            Dim summary As New List(Of String)
            summary.Add("ui-showcase: " & If(failed OrElse failures.Count > 0, "FAIL", "PASS"))
            summary.Add("combinations " & Combos().Count.ToString() & ", pictures " & shots.ToString() & ", measurements " & (rows.Count - 1).ToString() &
                        ", failed " & failures.Count.ToString() & ", dpi " & dpiSeen.ToString())
            summary.AddRange(failures)
            IO.File.WriteAllLines(IO.Path.Combine(folder, "summary.txt"), summary.ToArray(), New Text.UTF8Encoding(False))
            Console.WriteLine(summary(0) & " (" & summary(1) & ")")
        Catch ex As Exception
            failed = True
            ShellLog.Write("ui-showcase", ex)
            Console.Error.WriteLine("showcase failed (" & ex.GetType().Name & ", see the shell log)")
        Finally
            UiTokens.InterfaceScale = wasScale
            Theme.CurrentDpi = wasDpi
            ShellSettings.LanguageOverride = Nothing
            Localization.ResetShellDict()
            Theme.UsePaletteForTest(Nothing)
        End Try
        Return If(failed OrElse failures.Count > 0, 1, 0)
    End Function

    Private Sub ApplyCombo(c As Combo)
        ShellSettings.LanguageOverride = c.Lang
        Localization.ResetShellDict()
        Select Case c.Palette
            Case "dark" : Theme.UsePaletteForTest(True)
            Case "contrast" : Theme.UseContrastPaletteForTest()
            Case Else : Theme.UsePaletteForTest(False)
        End Select
        UiTokens.InterfaceScale = c.Scale
    End Sub

    ' ---- the page ---------------------------------------------------------------------------------

    Private Function NewHost() As Form
        Dim host As New Form With {.StartPosition = FormStartPosition.Manual, .ShowInTaskbar = False,
                                   .FormBorderStyle = FormBorderStyle.None, .Location = New Point(-32000, -32000)}
        host.CreateControl()
        Dim handle = host.Handle   ' the monitor's DPI is read from a window that exists
        Theme.CurrentDpi = Ui.DpiFor(host)
        dpiSeen = Theme.CurrentDpi
        Return host
    End Function

    ' The body font at the interface scale and the text enlargement, composed once with the DPI.
    Private ReadOnly fonts As New List(Of Font)

    ' Every font a page made is disposed once the page is (ReleaseFonts).
    Private Function BodyFont(Optional bold As Boolean = False) As Font
        Dim made As New Font(Theme.UiFamily(), CSng(Theme.ScaledPoints(10.0F, Theme.CurrentDpi) * current.Scale * current.TextScale),
                             If(bold, FontStyle.Bold, FontStyle.Regular), GraphicsUnit.Point)
        fonts.Add(made)
        Return made
    End Function

    Private Sub ReleaseFonts()
        For Each made In fonts
            made.Dispose()
        Next
        fonts.Clear()
    End Sub

    ' A design pixel in device pixels at the DPI of the run (the controls are built before they have a window).
    Private Function Dp(designPixels As Integer) As Integer
        Return CInt(Math.Round(designPixels * Theme.CurrentDpi / 96.0))
    End Function

    Private Function L(key As String) As String
        Return Localization.T(key)
    End Function

    Private Sub CapturePage(file As String)
        Using host = NewHost()
            Dim p = Theme.Current
            Dim width = Ui.Px(host, If(current.Narrow, NarrowWidth, WideWidth))
            Dim root As New TableLayoutPanel With {.ColumnCount = 1, .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                                                   .Width = width, .MinimumSize = New Size(width, 0), .MaximumSize = New Size(width, 0),
                                                   .BackColor = p.Surface, .ForeColor = p.Text, .Padding = New Padding(0), .Margin = New Padding(0)}
            root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
            host.BackColor = p.Surface
            host.Font = BodyFont()
            host.Controls.Add(root)

            Dim column = width
            Dim buttons = AddFlow(root, column)
            BuildButtons(buttons)
            Dim stateful = AddFlow(root, column)
            BuildStateful(stateful)
            Dim options = AddFlow(root, column, True)
            BuildOptions(options, column)
            Dim groups = BuildGroups(root)
            Dim rail = AddFlow(root, column, True)
            BuildRail(rail, host)

            host.ClientSize = New Size(width, 10)
            host.Show()
            Pump()
            For Each flow In New FlowLayoutPanel() {buttons, stateful}
                Ui.EqualizeRowHeights(flow)
            Next
            root.PerformLayout()
            Pump()
            host.ClientSize = New Size(width, Math.Max(1, root.Height))
            Pump()

            MeasureButtons(buttons, p, column)
            MeasureButtons(stateful, p, column)
            MeasureOptions(options, p, column)
            MeasureGroups(groups, column)
            MeasureRail(rail, column)

            Using bmp As New Bitmap(Math.Max(1, root.Width), Math.Max(1, root.Height))
                root.DrawToBitmap(bmp, New Rectangle(0, 0, bmp.Width, bmp.Height))
                bmp.Save(file, Imaging.ImageFormat.Png)
            End Using
        End Using
    End Sub

    Private Sub Pump()
        For i = 1 To 4
            Application.DoEvents()
            Threading.Thread.Sleep(20)
        Next
    End Sub

    Private Function AddFlow(root As TableLayoutPanel, width As Integer, Optional topDown As Boolean = False) As FlowLayoutPanel
        Dim gap = Ui.Px(root, UiTokens.GroupGap())
        Dim flow As New FlowLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .WrapContents = Not topDown,
                                              .FlowDirection = If(topDown, FlowDirection.TopDown, FlowDirection.LeftToRight),
                                              .MaximumSize = New Size(width, 0), .Margin = New Padding(0, 0, 0, gap), .Padding = New Padding(0),
                                              .BackColor = Theme.Current.Surface}
        root.Controls.Add(flow)
        Return flow
    End Function

    Private Function MakeButton(text As String, glyph As String, face As GlyphButtonFace) As GlyphButton
        Dim b As New GlyphButton With {.Text = text, .Tier = UiTokens.CaptionGlyph(), .Face = face, .Font = BodyFont(),
                                       .Margin = New Padding(0, 0, Dp(UiTokens.ButtonGap()), Dp(UiTokens.ButtonGap()))}
        If glyph IsNot Nothing Then b.Glyph = GlyphRef.Vocabulary(glyph)
        b.AccessibleName = text
        Return b
    End Function

    ' Every rank and state: ordinary with and without a glyph, primary, destructive, icon-only, disabled,
    ' the four answer faces, and a long caption capped so that it wraps.
    Private Sub BuildButtons(flow As FlowLayoutPanel)
        flow.Controls.Add(MakeButton(L("shell_btn_copy_cmd"), "action.copy", GlyphButtonFace.Normal))
        flow.Controls.Add(MakeButton(L("shell_btn_close"), Nothing, GlyphButtonFace.Normal))
        flow.Controls.Add(MakeButton(L("shell_btn_browse"), "action.choose-folder", GlyphButtonFace.AccentFill))
        Dim danger = MakeButton(L("shell_btn_clean_remove"), "action.delete", GlyphButtonFace.DangerFill)
        danger.Danger = True
        flow.Controls.Add(danger)
        Dim iconOnly = MakeButton(L("shell_btn_copy_cmd"), "action.copy", GlyphButtonFace.Normal)
        iconOnly.IconOnly = True
        flow.Controls.Add(iconOnly)
        Dim off = MakeButton(L("shell_btn_copy_cmd"), "action.copy", GlyphButtonFace.Normal)
        off.Enabled = False
        flow.Controls.Add(off)
        flow.Controls.Add(MakeButton(L("shell_btn_close"), "action.verify", GlyphButtonFace.AnswerOk))
        flow.Controls.Add(MakeButton(L("shell_btn_cancel"), "action.cancel", GlyphButtonFace.AnswerWarning))
        flow.Controls.Add(MakeButton(L("shell_btn_end_run_now"), "action.cancel", GlyphButtonFace.AnswerError))
        flow.Controls.Add(MakeButton(L("shell_btn_cancel"), "action.cancel", GlyphButtonFace.AnswerErrorSafe))
        Dim capped = MakeButton(L("shell_btn_remove_copy") & " - " & L("shell_btn_browse_dest"), "action.delete", GlyphButtonFace.Normal)
        capped.MaximumSize = New Size(Dp(UiTokens.Scaled(180)), 0)
        flow.Controls.Add(capped)
    End Sub

    ' A stateful button, reserved to its widest caption, beside its plain neighbours.
    Private Sub BuildStateful(flow As FlowLayoutPanel)
        Dim b = MakeButton(L("shell_btn_stop"), "action.cancel", GlyphButtonFace.Normal)
        b.ReservedCaptionKeys = New String() {"shell_btn_remove_copy", "shell_btn_end_run_now"}
        b.Name = "stateful"
        flow.Controls.Add(b)
        flow.Controls.Add(MakeButton(L("shell_btn_close"), Nothing, GlyphButtonFace.Normal))
    End Sub

    Private Function OptionOf(text As String, explanation As String, glyph As String, checkedValue As Boolean) As BooleanOption
        Dim o As New BooleanOption With {.Text = text, .Font = BodyFont(), .AutoSize = True, .Checked = checkedValue,
                                         .ForeColor = Theme.Current.Text, .Margin = New Padding(0, 0, 0, Dp(UiTokens.Scaled(8)))}
        If explanation IsNot Nothing Then o.Explanation = explanation
        If glyph IsNot Nothing Then o.Glyph = GlyphRef.Vocabulary(glyph)
        o.AccessibleName = text
        Return o
    End Function

    ' Checked, unchecked, disabled with its reason, with and without a glyph and an explanation, and the
    ' long translation the column has to wrap.
    Private Sub BuildOptions(flow As FlowLayoutPanel, width As Integer)
        Dim cap = New Size(width, 0)
        Dim items As New List(Of BooleanOption)
        items.Add(OptionOf(L("shell_settings_history"), L("shell_settings_history_hint"), Nothing, True))
        items.Add(OptionOf(L("shell_settings_history"), L("shell_settings_history_hint"), "action.verify", False))
        items.Add(OptionOf(L("shell_cred_show"), Nothing, Nothing, False))
        Dim off = OptionOf(L("vd_share_read_only"), Nothing, Nothing, True)
        off.DisabledReason = L("vd_note_seal")
        off.Enabled = False
        items.Add(off)
        items.Add(OptionOf(L("vd_part_destroy_wipe"), L("settings_theme_hint") & " " & L("shell_settings_history_hint"), "action.delete", False))
        For Each o In items
            o.MaximumSize = cap
            flow.Controls.Add(o)
        Next
    End Sub

    ' An open group holding a real Settings row, and a shut one beside it.
    Private Function BuildGroups(root As TableLayoutPanel) As List(Of SettingsGroup)
        Dim list As New List(Of SettingsGroup)
        Dim open As New SettingsGroup("showcase-open", L("shell_settings_history_title"))
        Dim opt As New BooleanOption With {.Font = BodyFont(), .ForeColor = Theme.Current.Text}
        open.AddRow(New SettingRow("showcase-history", L("shell_settings_history"), L("shell_settings_history_hint"), opt))
        open.Header.Font = BodyFont(True)
        open.Expanded = True
        Dim shut As New SettingsGroup("showcase-shut", L("settings_startup"))
        shut.Header.Font = BodyFont(True)
        shut.Expanded = False
        For Each g In New SettingsGroup() {open, shut}
            g.Dock = DockStyle.Top
            root.Controls.Add(g)
            g.ApplyTheme()
            list.Add(g)
        Next
        Return list
    End Function

    ' The rail's kinds: a header, a child, the selected child, a lone row, About last.
    Private Sub BuildRail(flow As FlowLayoutPanel, host As Form)
        Dim width = Math.Min(Ui.Px(host, UiTokens.Scaled(ShellForm.RailWidth)), flow.MaximumSize.Width)
        Dim unit = Ui.Px(host, UiTokens.NavRow())
        Dim shown = New String() {"rail_group_check", "rail_job_info", "rail_job_damaged", "rail_job_settings", "rail_job_about"}
        For Each key In shown
            Dim row = RailRow.All.FirstOrDefault(Function(r) r.Key = key)
            If row Is Nothing Then Continue For
            Dim e As New RailEntry With {.Key = key, .Name = "rail:" & key, .Text = L(key), .Glyph = row.Glyph, .Hue = row.Hue,
                                         .IsGroupHeader = row.IsGroup, .IsChild = row.IsChild, .RowUnit = unit,
                                         .Width = width, .Height = unit, .Margin = New Padding(0)}
            e.Selected = key = "rail_job_damaged"
            e.AccessibleName = e.Text
            e.Height = e.PreferredRowHeight(width)
            flow.Controls.Add(e)
        Next
    End Sub

    ' ---- the dialog -------------------------------------------------------------------------------

    ' A confirmation of a destructive action: the dismissing answer first and the default, the acting one
    ' last on the danger face. Its answers must all sit inside it, side by side, not overlapping.
    Private Sub CaptureDialog(file As String)
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Using dlg = ShellDialog.ForTest(DestructiveDialogs.CleanTestFiles(dict, "D:\Test"))
            dlg.StartPosition = FormStartPosition.Manual
            dlg.ShowInTaskbar = False
            dlg.Location = New Point(-32000, -32000)
            dlg.Show()
            Pump()
            Dim client = dlg.ClientRectangle
            Dim answers = Ui.AllControls(dlg).OfType(Of GlyphButton)().Where(Function(b) b.Visible).ToList()
            Row("dialog", "clean", "answers", answers.Count.ToString(), ">= 2", answers.Count >= 2)
            For Each b In answers
                Dim r = dlg.RectangleToClient(b.RectangleToScreen(b.ClientRectangle))
                Row("dialog", b.Text, "inside", RectText(r), RectText(client), client.Contains(r))
                Row("dialog", b.Text, "target", b.Height.ToString(), ">= " & Ui.Px(dlg, UiTokens.CompactFloor).ToString(),
                    b.Height >= Ui.Px(dlg, UiTokens.CompactFloor))
            Next
            CheckOverlap("dialog", answers.Cast(Of Control)().ToList())
            Using bmp As New Bitmap(Math.Max(1, dlg.Width), Math.Max(1, dlg.Height))
                dlg.DrawToBitmap(bmp, New Rectangle(0, 0, bmp.Width, bmp.Height))
                bmp.Save(file, Imaging.ImageFormat.Png)
            End Using
            dlg.Hide()
        End Using
    End Sub

    ' ---- measurements -----------------------------------------------------------------------------

    Private Sub MeasureButtons(flow As FlowLayoutPanel, p As Theme.Palette, column As Integer)
        Dim target = Ui.Px(flow, UiTokens.Target())
        Dim list = flow.Controls.OfType(Of GlyphButton)().ToList()
        For Each b In list
            Dim name = b.Face.ToString() & If(b.IconOnly, ":icon", "") & If(b.Enabled, "", ":disabled") & ":" & ClipText(b.Text)
            Row("button", name, "target", b.Height.ToString(), ">= " & target.ToString(), b.Height >= target)
            Dim want = b.GetPreferredSize(Size.Empty)
            Row("button", name, "not-clipped", b.Width.ToString() & "x" & b.Height.ToString(), ">= " & want.Width.ToString() & "x" & want.Height.ToString(),
                b.Width >= want.Width AndAlso b.Height >= want.Height)
            Row("button", name, "in-column", b.Right.ToString(), "<= " & column.ToString(), b.Right <= column)
            If b.Enabled Then
                Dim c = b.ColoursFor(p)
                If Not b.IconOnly Then Contrast("button", name, "caption", c.Caption, c.Back, 4.5)
                If b.Glyph IsNot Nothing Then Contrast("button", name, "glyph", c.Glyph, c.Back, 3.0)
            End If
        Next
        For Each visualRow In list.GroupBy(Function(b) b.Top)
            Dim heights = visualRow.Select(Function(b) b.Height).Distinct().ToList()
            Row("button-row", "top " & visualRow.Key.ToString(), "row-height", String.Join("/", heights.Select(Function(h) h.ToString()).ToArray()),
                "one height", heights.Count = 1)
        Next
        Dim stateful = list.FirstOrDefault(Function(b) b.Name = "stateful")
        If stateful IsNot Nothing Then
            Dim widths As New List(Of Integer)
            Dim original = stateful.Text
            For Each key In New String() {"shell_btn_stop", "shell_btn_remove_copy", "shell_btn_end_run_now"}
                stateful.Text = L(key)
                widths.Add(stateful.GetPreferredSize(Size.Empty).Width)
            Next
            stateful.Text = original
            Row("button", "stateful", "state-width", String.Join(",", widths.Select(Function(w) w.ToString()).ToArray()), "one width",
                widths.Distinct().Count() = 1)
        End If
        CheckOverlap("button", list.Cast(Of Control)().ToList())
    End Sub

    Private Sub MeasureOptions(flow As FlowLayoutPanel, p As Theme.Palette, column As Integer)
        Dim target = Ui.Px(flow, UiTokens.Target())
        Dim list = flow.Controls.OfType(Of BooleanOption)().ToList()
        For Each o In list
            Dim name = If(o.Enabled, "", "disabled:") & If(o.Glyph IsNot Nothing, "glyph:", "") & ClipText(o.Text)
            Row("option", name, "target", o.Height.ToString(), ">= " & target.ToString(), o.Height >= target)
            Dim lay = o.LayoutFor(o.Width)
            Row("option", name, "not-clipped", o.Width.ToString() & "x" & o.Height.ToString(),
                ">= " & lay.Size.Width.ToString() & "x" & lay.Size.Height.ToString(), o.Height >= lay.Size.Height AndAlso lay.Caption.Right <= o.Width)
            Row("option", name, "box-leading", lay.Box.Left.ToString(), "0", lay.Box.Left = 0)
            Row("option", name, "in-column", o.Right.ToString(), "<= " & column.ToString(), o.Right <= column)
            If Not o.Enabled Then
                Row("option", name, "reason-shown", ClipText(o.ShownNote()), "holds the reason", o.ShownNote().Contains(o.DisabledReason))
            Else
                Dim c = o.ColoursFor(p)
                Contrast("option", name, "caption", c.Caption, p.Surface, 4.5)
                Contrast("option", name, "outline", c.Outline, p.Surface, 3.0)
                If o.Explanation <> "" Then Contrast("option", name, "explanation", c.Note, p.Surface, 4.5)
            End If
        Next
        CheckOverlap("option", list.Cast(Of Control)().ToList())
    End Sub

    Private Sub MeasureGroups(groups As List(Of SettingsGroup), column As Integer)
        Dim target = Ui.Px(groups(0), UiTokens.GroupHeaderHeight())
        For Each g In groups
            Dim name = g.GroupId & If(g.Expanded, ":open", ":shut")
            Row("group", name, "target", g.Header.Height.ToString(), ">= " & target.ToString(), g.Header.Height >= target)
            Row("group", name, "body", g.Body.Visible.ToString(), g.Expanded.ToString(), g.Body.Visible = g.Expanded)
            Row("group", name, "full-width", g.Header.Width.ToString(), "= " & g.Width.ToString(), g.Header.Width >= g.ClientSize.Width - 1 AndAlso g.Right <= column)
            Dim sq = g.Header.MarkerSquare(g.Header.Height)
            Row("group", name, "marker-leading", sq.Left.ToString(), "< caption " & g.Header.CaptionBounds(g.Header.Width, g.Header.Height).Left.ToString(),
                sq.Left < g.Header.CaptionBounds(g.Header.Width, g.Header.Height).Left)
        Next
        CheckOverlap("group", groups.Cast(Of Control)().ToList())
    End Sub

    Private Sub MeasureRail(flow As FlowLayoutPanel, column As Integer)
        Dim list = flow.Controls.OfType(Of RailEntry)().ToList()
        For Each e In list
            Row("rail", e.Key, "target", e.Height.ToString(), ">= " & e.RowUnit.ToString(), e.Height >= e.RowUnit)
            Dim detail As String = ""
            Dim fits = e.LabelFits(e.Width, detail)
            Row("rail", e.Key, "label-fits", ClipText(detail), "fits", fits)
            Row("rail", e.Key, "in-column", e.Right.ToString(), "<= " & column.ToString(), e.Right <= column)
        Next
        Row("rail", "order", "about-last", If(list.Count > 0, list.Last().Key, ""), "rail_job_about", list.Count > 0 AndAlso list.Last().Key = "rail_job_about")
        CheckOverlap("rail", list.Cast(Of Control)().ToList())
    End Sub

    Private Sub CheckOverlap(kind As String, controls As List(Of Control))
        Dim hits As New List(Of String)
        For i = 0 To controls.Count - 1
            For j = i + 1 To controls.Count - 1
                If controls(i).Parent Is controls(j).Parent AndAlso controls(i).Bounds.IntersectsWith(controls(j).Bounds) Then
                    hits.Add(i.ToString() & "/" & j.ToString())
                End If
            Next
        Next
        Row(kind, "siblings", "no-overlap", If(hits.Count = 0, "none", String.Join(" ", hits.ToArray())), "none", hits.Count = 0)
    End Sub

    Private Sub Contrast(kind As String, name As String, part As String, fore As Color, back As Color, need As Double)
        Dim ratio = Theme.ContrastRatio(fore, back)
        Row(kind, name, "contrast:" & part, ratio.ToString("0.00", Globalization.CultureInfo.InvariantCulture),
            ">= " & need.ToString("0.0", Globalization.CultureInfo.InvariantCulture), ratio >= need)
    End Sub

    Private Sub Row(kind As String, name As String, check As String, value As String, need As String, pass As Boolean)
        Dim c = current
        rows.Add(String.Join(",", New String() {c.Id, c.Palette, Num(c.Scale), Num(c.TextScale), c.Lang, If(c.Narrow, "narrow", "wide"),
                                                dpiSeen.ToString(), kind, Csv(name), check, Csv(value), Csv(need), If(pass, "PASS", "FAIL")}))
        If Not pass Then failures.Add(c.Id & " " & kind & " " & name & " " & check & ": " & value & " (need " & need & ")")
    End Sub

    Private Function Num(v As Double) As String
        Return v.ToString("0.###", Globalization.CultureInfo.InvariantCulture)
    End Function

    Private Function RectText(r As Rectangle) As String
        Return r.X.ToString() & " " & r.Y.ToString() & " " & r.Width.ToString() & "x" & r.Height.ToString()
    End Function

    Private Function Csv(s As String) As String
        Dim v = If(s, "").Replace(vbCr, " ").Replace(vbLf, " ")
        If v.IndexOfAny(New Char() {","c, """"c}) >= 0 Then Return """" & v.Replace("""", """""") & """"
        Return v
    End Function

    ' A short name for a row: the text as it is (the CSV is UTF-8), cut so a long translation stays one cell.
    Private Function ClipText(s As String) As String
        Dim v = If(s, "")
        Return If(v.Length > 40, v.Substring(0, 40), v)
    End Function

End Module
