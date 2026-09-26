' Layout helpers shared by every shell view.
'
' Two problems in WinForms make a window look broken long before the palette does, and both are
' solved here once instead of in every view:
'
'   1. **A number written in code is not a number on the screen.** A 224-pixel rail is 224 real
'      pixels at 100% and 224 real pixels at 200%, while the font inside it doubles - so the label
'      is clipped exactly on the display the user bought the program for. Every fixed size in the
'      shell goes through Px(), which turns a design pixel into a device pixel for the monitor the
'      control is currently on (SP-0006 section 11 item 1).
'
'   2. **AutoSize labels do not wrap.** An AutoSize label grows sideways for ever, so a sentence
'      that is short in English and long in German leaves the card rather than wrapping inside it.
'      Wrap() binds a label to the width it is actually given, which is what makes every string
'      visible in all five locales instead of only in the one it was written in.
Module Ui

    ' A design pixel, taken at 96 DPI, in the device units of the monitor this control is on.
    Public Function Px(owner As Control, designPixels As Integer) As Integer
        If owner Is Nothing Then Return designPixels
        Try
            Return owner.LogicalToDeviceUnits(designPixels)
        Catch
            Return designPixels
        End Try
    End Function

    Public Function PxSize(owner As Control, w As Integer, h As Integer) As Size
        Return New Size(Px(owner, w), Px(owner, h))
    End Function

    Public Function PxPad(owner As Control, l As Integer, t As Integer, r As Integer, b As Integer) As Padding
        Return New Padding(Px(owner, l), Px(owner, t), Px(owner, r), Px(owner, b))
    End Function

    ' Makes a label wrap inside whatever width its container ends up with, and keep its full text.
    '
    ' The label stays AutoSize - that is what lets a card grow to fit its content - but its maximum
    ' width is re-bound every time the container is resized, which is what turns growth sideways
    ' into growth downwards. A label that is never given a width (a zero-width container during
    ' construction) is left alone rather than collapsed to one character per line.
    Public Sub Wrap(label As Label, container As Control, Optional reserve As Integer = 0)
        If label Is Nothing OrElse container Is Nothing Then Return
        label.AutoSize = True
        Dim apply = Sub()
                        Dim avail = container.ClientSize.Width - reserve - label.Margin.Horizontal
                        If avail > Px(label, 80) Then
                            label.MaximumSize = New Size(avail, 0)
                        End If
                    End Sub
        AddHandler container.SizeChanged, Sub() apply()
        apply()
    End Sub

    ' The numbered heading of a step. The number is part of the text rather than a separate glyph,
    ' so a screen reader reads "Step 2 - what to work on" as one phrase.
    Public Function StepHeader(owner As Control, text As String) As Label
        Dim l As New Label With {
            .Text = text,
            .AutoSize = True,
            .Margin = PxPad(owner, 0, 0, 0, 8)
        }
        l.AccessibleRole = AccessibleRole.StaticText
        Return l
    End Function

    ' A flat button in the house style. FlatStyle.Flat alone still draws a border in the system
    ' colour, so the border is set here too - otherwise a dark theme shows a light rectangle.
    Public Sub StyleButton(b As Button, back As Color, fore As Color, border As Color)
        If b Is Nothing Then Return
        b.FlatStyle = FlatStyle.Flat
        b.FlatAppearance.BorderSize = 1
        b.FlatAppearance.BorderColor = border
        b.BackColor = back
        b.ForeColor = fore
        b.UseVisualStyleBackColor = False
        KeepCaptionReadable(b)
    End Sub

    ' A disabled button, check box or radio button draws its caption in the palette's TextDisabled.
    '
    ' WinForms ignores ForeColor on a disabled ButtonBase and derives the caption from the back
    ' colour instead - a darker shade of it - so on the dark theme a disabled control was near-black
    ' text on a near-black surface: the Run button of a page still waiting for its target could not
    ' be read at all. The caption is painted again over the one WinForms drew, in the rectangle
    ' WinForms drew it in, so nothing moves when a control is disabled. The whole window is walked
    ' once its views are built (ShellForm.ApplyTheme), and every StyleButton call covers the buttons
    ' of a dialog; the table below keeps a control from being hooked twice.
    Private ReadOnly readableCaptions As New Runtime.CompilerServices.ConditionalWeakTable(Of ButtonBase, Object)()

    Public Sub KeepCaptionsReadable(root As Control)
        If root Is Nothing Then Return
        Dim b = TryCast(root, ButtonBase)
        If b IsNot Nothing Then KeepCaptionReadable(b)
        For Each c As Control In root.Controls
            KeepCaptionsReadable(c)
        Next
    End Sub

    Private Sub KeepCaptionReadable(b As ButtonBase)
        Dim hooked As Object = Nothing
        If b Is Nothing OrElse readableCaptions.TryGetValue(b, hooked) Then Return
        readableCaptions.Add(b, Nothing)
        AddHandler b.Paint, AddressOf PaintDisabledCaption
    End Sub

    Friend Function CaptionKeptReadable(b As ButtonBase) As Boolean
        Dim hooked As Object = Nothing
        Return b IsNot Nothing AndAlso readableCaptions.TryGetValue(b, hooked)
    End Function

    Private Sub PaintDisabledCaption(sender As Object, e As PaintEventArgs)
        Dim b = TryCast(sender, ButtonBase)
        If b Is Nothing OrElse b.Enabled OrElse String.IsNullOrEmpty(b.Text) Then Return
        Dim bounds As Rectangle
        Dim flags As TextFormatFlags
        If Not CaptionBounds(b, e.Graphics, bounds, flags) Then Return
        If b.BackColor.A = 255 Then
            Using fill As New SolidBrush(b.BackColor)
                e.Graphics.FillRectangle(fill, bounds)
            End Using
        End If
        TextRenderer.DrawText(e.Graphics, b.Text, b.Font, bounds, Theme.Current.TextDisabled, flags)
    End Sub

    ' Where WinForms (GDI text, visual styles) puts the caption of the kinds the shell uses: a flat
    ' button with its caption centred, and a check box or radio button with the box and the caption
    ' on the left. It follows the framework's own button layout - the border, two pixels of padding
    ' for a flat button, the glyph and one pixel for a check box, and a two-pixel inset around the
    ' text - and was measured equal to it, pixel for pixel, for all three kinds; the self-test's
    ' disabled-caption: rows hold that on every build. Any other kind - a system-drawn button,
    ' another alignment, right-to-left, an image, a high-contrast theme, where Windows's own
    ' GrayText is already readable - is left to WinForms.
    Private Function CaptionBounds(b As ButtonBase, g As Graphics, ByRef bounds As Rectangle, ByRef flags As TextFormatFlags) As Boolean
        If SystemInformation.HighContrast OrElse b.RightToLeft = RightToLeft.Yes OrElse b.Image IsNot Nothing Then Return False
        flags = TextFormatFlags.VerticalCenter Or TextFormatFlags.WordBreak Or TextFormatFlags.TextBoxControl Or
                If(b.UseMnemonic, TextFormatFlags.HidePrefix, TextFormatFlags.NoPrefix)
        Dim client As New Rectangle(b.Padding.Left, b.Padding.Top,
                                    b.ClientSize.Width - b.Padding.Horizontal, b.ClientSize.Height - b.Padding.Vertical)
        Dim field As Rectangle
        Dim centred = False
        Dim btn = TryCast(b, Button)
        Dim check = TryCast(b, CheckBox)
        Dim radio = TryCast(b, RadioButton)
        If btn IsNot Nothing Then
            If btn.FlatStyle <> FlatStyle.Flat OrElse btn.TextAlign <> ContentAlignment.MiddleCenter Then Return False
            Dim inset = btn.FlatAppearance.BorderSize + 2
            field = Rectangle.Inflate(client, -inset, -inset)
            flags = flags Or TextFormatFlags.HorizontalCenter
            centred = True
        ElseIf check IsNot Nothing Then
            If check.FlatStyle <> FlatStyle.Standard OrElse check.Appearance <> Appearance.Normal OrElse
               check.CheckAlign <> ContentAlignment.MiddleLeft OrElse check.TextAlign <> ContentAlignment.MiddleLeft Then Return False
            Dim glyph = CheckBoxRenderer.GetGlyphSize(g, VisualStyles.CheckBoxState.UncheckedDisabled).Width
            field = New Rectangle(client.X + glyph + 1, client.Y, client.Width - glyph - 1, client.Height)
        ElseIf radio IsNot Nothing Then
            If radio.FlatStyle <> FlatStyle.Standard OrElse radio.Appearance <> Appearance.Normal OrElse
               radio.CheckAlign <> ContentAlignment.MiddleLeft OrElse radio.TextAlign <> ContentAlignment.MiddleLeft Then Return False
            Dim glyph = RadioButtonRenderer.GetGlyphSize(g, VisualStyles.RadioButtonState.UncheckedDisabled).Width
            field = New Rectangle(client.X + glyph + 1, client.Y, client.Width - glyph - 1, client.Height)
        Else
            Return False
        End If
        Dim textArea = Rectangle.Inflate(field, -2, -2)
        Dim measured = TextRenderer.MeasureText(b.Text, b.Font, textArea.Size, flags)
        Dim x = If(centred, textArea.X + (textArea.Width - measured.Width) \ 2, textArea.X)
        Dim y = textArea.Y + (textArea.Height - measured.Height) \ 2
        ' A check box's caption sits one pixel higher than a radio button's (measured).
        If check IsNot Nothing Then y -= 1
        bounds = Rectangle.Intersect(New Rectangle(x, y, measured.Width, measured.Height), field)
        Return True
    End Function

    ' Puts text on the clipboard. Another program holding the clipboard open makes Windows refuse,
    ' and that refusal is a named cause with a retry rather than an exception dialog.
    Public Sub CopyText(owner As IWin32Window, text As String)
        If String.IsNullOrEmpty(text) Then Return
        Try
            Clipboard.SetText(text)
        Catch ex As Exception
            ShellLog.Write("copy to clipboard", ex)
            If ShellDialog.Problem(owner, Localization.Format(Localization.T("shell_copy_failed"), Problems.Cause(ex)),
                                   Localization.T("shell_btn_retry"), offerLogs:=False) Then
                CopyText(owner, text)
            End If
        End Try
    End Sub

    ' One duration format for every place that shows how long a run took (SHELL-11): minutes and
    ' seconds under an hour, the hours in front from one hour up - a 3 h 23 min test reads 3:23:00,
    ' never 23:00.
    Public Function FormatDuration(d As TimeSpan) As String
        If d < TimeSpan.Zero Then d = TimeSpan.Zero
        Dim hours = CLng(Math.Floor(d.TotalHours))
        If hours >= 1 Then
            Return hours.ToString(Globalization.CultureInfo.InvariantCulture) & ":" & d.ToString("mm\:ss")
        End If
        Return d.ToString("mm\:ss")
    End Function

    ' A number the CLI reads with strconv.Atoi: digits and nothing else (GUI-15). IsNumeric took
    ' "1,000", "1e3" and "2.5", which Atoi refuses - the run then silently used its default - and
    ' read "2,5" as a number in a locale whose decimal mark is a comma.
    Public Function IsWholeNumber(text As String) As Boolean
        Dim n As Long
        Return Long.TryParse(text, Globalization.NumberStyles.None, Globalization.CultureInfo.InvariantCulture, n)
    End Function

    ' A number the CLI reads with strconv.ParseFloat: digits with at most one "." - in every locale.
    Public Function IsDecimalNumber(text As String) As Boolean
        Dim d As Double
        Return Double.TryParse(text, Globalization.NumberStyles.AllowDecimalPoint,
                               Globalization.CultureInfo.InvariantCulture, d)
    End Function

    ' Opens a folder in Explorer. A folder that cannot be opened is said to be so, with where it is,
    ' so the user can go there by hand.
    Public Sub OpenFolder(owner As IWin32Window, folder As String)
        Try
            Process.Start("explorer.exe", """" & folder & """")
        Catch ex As Exception
            ShellLog.Write("open folder " & folder, ex)
            If ShellDialog.Problem(owner, Localization.Format(Localization.T("shell_folder_open_failed"), folder, Problems.Cause(ex)),
                                   Localization.T("shell_btn_copy_path")) Then
                CopyText(owner, folder)
            End If
        End Try
    End Sub

End Module

' How a run's progress is drawn, one rule for every page that runs something (APP-BEHAVIOUR rule 3,
' "reports real progress"): a bar that fills when the run has said how much there is to do - bytes
' first, then items - and a marquee when it has not, because a bar that fills on a guess is a lie.
Module RunProgress

    Public Sub Apply(bar As ProgressBar, p As EventStream.ProgressInfo)
        If bar Is Nothing OrElse p Is Nothing Then Return
        If p.TotalBytes > 0 Then
            bar.Style = ProgressBarStyle.Continuous
            bar.Value = Percent(p.DoneBytes, p.TotalBytes)
        ElseIf p.TotalItems > 0 Then
            bar.Style = ProgressBarStyle.Continuous
            bar.Value = Percent(p.DoneItems, p.TotalItems)
        Else
            bar.Style = ProgressBarStyle.Marquee
        End If
    End Sub

    ' The start of a run, before it has said anything about its size.
    Public Sub Begin(bar As ProgressBar)
        If bar Is Nothing Then Return
        bar.Value = 0
        bar.Style = ProgressBarStyle.Marquee
    End Sub

    Private Function Percent(done As Long, total As Long) As Integer
        Return CInt(Math.Min(100, Math.Max(0, (done * 100) \ total)))
    End Function

End Module

' A card: the flat surface every section of a page sits on. Owner-drawn rather than a GroupBox,
' because a system group box draws a 3D frame that no palette can hide (SP-0006 section 11 item 6).
'
' The accent bar on the left edge is how a destructive section stays distinguishable when the
' colour is ignored - a printed screenshot, a colour-blind user, a high-contrast theme (item 8).
Public Class ShellCard
    Inherits Panel

    ' Both colours come from the palette (Theme.vb); unset, they are empty and not drawn.
    Public Property BorderColour As Color
    Public Property Accented As Boolean = False
    Public Property AccentColour As Color

    Public Sub New()
        SetStyle(ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or
                 ControlStyles.ResizeRedraw Or
                 ControlStyles.UserPaint, True)
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        Dock = DockStyle.Top
    End Sub

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim g = e.Graphics
        Using b As New SolidBrush(BackColor)
            g.FillRectangle(b, ClientRectangle)
        End Using
        If Accented AndAlso Not AccentColour.IsEmpty Then
            Using b As New SolidBrush(AccentColour)
                g.FillRectangle(b, New Rectangle(0, 0, Ui.Px(Me, 3), Height))
            End Using
        End If
        If Not BorderColour.IsEmpty Then
            Using pen As New Pen(BorderColour, 1.0F)
                g.DrawRectangle(pen, New Rectangle(0, 0, Width - 1, Height - 1))
            End Using
        End If
        MyBase.OnPaint(e)
    End Sub

End Class

' A run's output drawer, fed from the runner's thread (SP-0029 GUI-14).
'
' A long run prints a line for every progress tick, and posting each one to the window and appending
' it to a TextBox cost the UI thread more with every line - 100 000 lines measured at 16 s, with the
' window frozen for most of it. Lines are queued here from any thread and appended in one piece
' every 100 ms, and the box keeps only the end of the output: the whole of it is in the run's report.
Public Class OutputPane

    Friend Const MaxChars As Integer = 1024 * 1024
    Private Const FlushMs As Integer = 100

    Private ReadOnly box As TextBox
    Private ReadOnly pending As New Concurrent.ConcurrentQueue(Of String)()
    Private ReadOnly timer As Windows.Forms.Timer

    Public Sub New(target As TextBox)
        box = target
        timer = New Windows.Forms.Timer With {.Interval = FlushMs}
        AddHandler timer.Tick, Sub() Flush()
    End Sub

    ' From any thread.
    Public Sub Add(line As String)
        pending.Enqueue(If(line, ""))
    End Sub

    ' On the UI thread, when a run starts and when it has ended.
    Public Sub Start()
        timer.Start()
    End Sub

    Public Sub [Stop]()
        timer.Stop()
        Flush()
    End Sub

    Public Sub Clear()
        Dim ignored As String = Nothing
        While pending.TryDequeue(ignored)
        End While
        box.Clear()
    End Sub

    ' Appends everything queued since the last flush, as one piece. When the box would pass its
    ' limit it is cut back to half of it, at a line start, so the cut happens once per half a
    ' megabyte of output rather than on every tick.
    Public Sub Flush()
        If box.IsDisposed OrElse pending.IsEmpty Then Return
        Dim sb As New System.Text.StringBuilder()
        Dim line As String = Nothing
        While pending.TryDequeue(line)
            sb.Append(line).Append(Environment.NewLine)
            If sb.Length > MaxChars * 2 Then sb.Remove(0, sb.Length - MaxChars)
        End While
        Dim chunk = sb.ToString()
        If box.TextLength + chunk.Length <= MaxChars Then
            box.AppendText(chunk)
            Return
        End If
        Dim combined = box.Text & chunk
        combined = combined.Substring(Math.Max(0, combined.Length - MaxChars \ 2))
        Dim nl = combined.IndexOf(ControlChars.Lf)
        If nl >= 0 AndAlso nl < combined.Length - 1 Then combined = combined.Substring(nl + 1)
        box.Text = combined
        box.SelectionStart = box.TextLength
        box.ScrollToCaret()
    End Sub

End Class
